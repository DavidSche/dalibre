package mcp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"dalibre/internal/plugin"
)

var sourceTools = []string{"find_books", "download_book", "find_metadata"}

// sourceTimeout bounds one source's answer.
const sourceTimeout = 30 * time.Second

// maxFound is how many of find_books' results download_book remembers.
const maxFound = 1000

func sourceTool(name, desc string, readOnly bool) *sdk.Tool {
	a := &sdk.ToolAnnotations{ReadOnlyHint: readOnly, OpenWorldHint: boolp(true)}
	if !readOnly {
		a.DestructiveHint = boolp(false)
	}
	return &sdk.Tool{Name: name, Description: desc, Annotations: a}
}

// Found is a book a source found.
type Found struct {
	Source      string            `json:"source" jsonschema:"the source's id, for download_book"`
	ID          string            `json:"id" jsonschema:"the book's id in the source, for download_book"`
	Title       string            `json:"title"`
	Authors     []string          `json:"authors,omitempty"`
	Publisher   string            `json:"publisher,omitempty"`
	Language    string            `json:"language,omitempty"`
	Published   string            `json:"published,omitempty"`
	Series      string            `json:"series,omitempty"`
	SeriesIndex float64           `json:"series_index,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Identifiers map[string]string `json:"identifiers,omitempty"`
	Description string            `json:"description,omitempty" jsonschema:"plain text"`
	Cover       string            `json:"cover,omitempty" jsonschema:"the cover image's URL"`
	URL         string            `json:"url,omitempty" jsonschema:"the book's page on the source's site"`
	Formats     []string          `json:"formats,omitempty" jsonschema:"what download_book can download, as EPUB"`
}

func found(source string, r plugin.Result) Found {
	f := Found{
		Source: source, ID: r.ID, Title: r.Title, Authors: r.Authors, Publisher: r.Publisher, Language: r.Language,
		Published: r.Pubdate, Series: r.Series, SeriesIndex: r.SeriesIndex, Tags: r.Tags, Identifiers: r.Identifiers,
		Description: htmlToText(r.Description), Cover: r.Cover, URL: r.URL,
	}
	for _, ff := range r.Formats {
		f.Formats = append(f.Formats, ff.Format)
	}
	return f
}

// SourceError is a source that didn't answer.
type SourceError struct {
	Source string `json:"source"`
	Error  string `json:"error"`
}

type findIn struct {
	Query  string `json:"query" jsonschema:"title, author or both"`
	Source string `json:"source,omitempty" jsonschema:"a source's id; all sources when not given"`
}

type findOut struct {
	Results []Found       `json:"results"`
	Failed  []SourceError `json:"failed,omitempty"`
}

type downloadIn struct {
	Source string `json:"source"`
	ID     string `json:"id" jsonschema:"a result's id from find_books"`
	Format string `json:"format,omitempty" jsonschema:"as EPUB; EPUB, AZW3, MOBI then PDF, as the result has them, when not given"`
}

type downloadOut struct {
	ID     int64  `json:"id" jsonschema:"the new book"`
	Title  string `json:"title"`
	Format string `json:"format"`
}

type metadataIn struct {
	ID     int64  `json:"id" jsonschema:"the book to look up"`
	Source string `json:"source,omitempty" jsonschema:"a metadata source's id; all when not given"`
}

type metadataOut struct {
	Candidates []Found       `json:"candidates" jsonschema:"best first, by source; apply one with update_books and set_cover"`
	Failed     []SourceError `json:"failed,omitempty"`
}

func (s *Server) addSourceTools() {
	sdk.AddTool(s.srv, sourceTool("find_books", "Searches the book sources of Get books, such as Project Gutenberg, for books to add.", true), s.findBooks)
	sdk.AddTool(s.srv, sourceTool("download_book", "Downloads a book find_books found and adds it to the library, as Get books does.", false), s.downloadBook)
	sdk.AddTool(s.srv, sourceTool("find_metadata", "Looks a book up in the metadata sources. Nothing changes: apply a candidate with update_books.", true), s.findMetadata)
}

func (s *Server) sources() (Sources, error) {
	if !s.allowsChanges() {
		return nil, errReadOnly
	}
	src, ok := s.host.(Sources)
	if !ok {
		return nil, errors.New("sources need Dalibre running")
	}
	return src, nil
}

// ask calls fn for each plugin at once and gathers what they found.
func ask(ctx context.Context, ps []*plugin.Plugin, fn func(context.Context, *plugin.Plugin) ([]plugin.Result, error)) ([][]plugin.Result, []SourceError) {
	results := make([][]plugin.Result, len(ps))
	errs := make([]error, len(ps))
	var wg sync.WaitGroup
	for i, p := range ps {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, sourceTimeout)
			defer cancel()
			results[i], errs[i] = fn(ctx, p)
		})
	}
	wg.Wait()
	var failed []SourceError
	for i, err := range errs {
		if err != nil {
			failed = append(failed, SourceError{Source: ps[i].Manifest.ID, Error: err.Error()})
		}
	}
	return results, failed
}

func pick(ps []*plugin.Plugin, id string) ([]*plugin.Plugin, error) {
	if id == "" {
		if len(ps) == 0 {
			return nil, errors.New("no sources")
		}
		return ps, nil
	}
	for _, p := range ps {
		if p.Manifest.ID == id {
			return []*plugin.Plugin{p}, nil
		}
	}
	var ids []string
	for _, p := range ps {
		ids = append(ids, p.Manifest.ID)
	}
	return nil, fmt.Errorf("no source %q; there are %s", id, strings.Join(ids, ", "))
}

func (s *Server) findBooks(ctx context.Context, _ *sdk.CallToolRequest, in findIn) (*sdk.CallToolResult, findOut, error) {
	src, err := s.sources()
	if err != nil {
		return nil, findOut{}, err
	}
	if strings.TrimSpace(in.Query) == "" {
		return nil, findOut{}, errors.New("no query")
	}
	ps, err := pick(src.BookSources(ctx), in.Source)
	if err != nil {
		return nil, findOut{}, err
	}
	results, failed := ask(ctx, ps, func(ctx context.Context, p *plugin.Plugin) ([]plugin.Result, error) {
		pg, err := p.Search(ctx, plugin.Query{Text: in.Query, Page: 1})
		return pg.Items, err
	})
	out := findOut{Results: []Found{}, Failed: failed}
	s.mu.Lock()
	for i, rs := range results {
		for _, r := range rs {
			id := ps[i].Manifest.ID
			k := id + "\x00" + r.ID
			if _, ok := s.found[k]; !ok {
				s.order = append(s.order, k)
			}
			s.found[k] = r
			out.Results = append(out.Results, found(id, r))
		}
	}
	for len(s.order) > maxFound {
		delete(s.found, s.order[0])
		s.order = s.order[1:]
	}
	s.mu.Unlock()
	s.noteBooks(ctx, len(out.Results))
	return nil, out, nil
}

func (s *Server) downloadBook(ctx context.Context, _ *sdk.CallToolRequest, in downloadIn) (*sdk.CallToolResult, downloadOut, error) {
	src, err := s.sources()
	if err != nil {
		return nil, downloadOut{}, err
	}
	if s.host.Library() == nil {
		return nil, downloadOut{}, errNoLibrary
	}
	s.mu.Lock()
	r, ok := s.found[in.Source+"\x00"+in.ID]
	s.mu.Unlock()
	if !ok {
		return nil, downloadOut{}, errors.New("no such result; find it with find_books first")
	}
	ps, err := pick(src.BookSources(ctx), in.Source)
	if err != nil {
		return nil, downloadOut{}, err
	}
	p := ps[0]
	if full, err := p.Get(ctx, r); err == nil {
		r = full
	}
	var have []string
	for _, f := range r.Formats {
		have = append(have, strings.ToUpper(f.Format))
	}
	format := strings.ToUpper(strings.TrimSpace(in.Format))
	if format == "" {
		for _, f := range append([]string{"EPUB", "AZW3", "MOBI", "PDF"}, have...) {
			if slices.Contains(have, f) {
				format = f
				break
			}
		}
	}
	if !slices.Contains(have, format) {
		return nil, downloadOut{}, fmt.Errorf("%q comes as %s", r.Title, strings.Join(have, ", "))
	}
	id, err := src.Download(ctx, p, r, format)
	if err != nil {
		return nil, downloadOut{}, err
	}
	title := r.Title
	if lib := s.host.Library(); lib != nil {
		if b, err := lib.Book(id); err == nil {
			title = b.Title
		}
	}
	s.noteBooks(ctx, 1, BookRef{id, title})
	return nil, downloadOut{ID: id, Title: title, Format: format}, nil
}

func (s *Server) findMetadata(ctx context.Context, _ *sdk.CallToolRequest, in metadataIn) (*sdk.CallToolResult, metadataOut, error) {
	src, err := s.sources()
	if err != nil {
		return nil, metadataOut{}, err
	}
	lib, err := s.library()
	if err != nil {
		return nil, metadataOut{}, err
	}
	b, err := s.book(lib, in.ID)
	if err != nil {
		return nil, metadataOut{}, err
	}
	d, err := lib.Details(b)
	if err != nil {
		return nil, metadataOut{}, err
	}
	ps, err := pick(src.MetadataSources(ctx), in.Source)
	if err != nil {
		return nil, metadataOut{}, err
	}
	book := plugin.Book{Title: b.Title, Authors: slices.Clone(b.Authors), Identifiers: d.Identifiers}
	if len(b.Languages) > 0 {
		book.Language = b.Languages[0]
	}
	results, failed := ask(ctx, ps, func(ctx context.Context, p *plugin.Plugin) ([]plugin.Result, error) {
		return p.Identify(ctx, book)
	})
	out := metadataOut{Candidates: []Found{}, Failed: failed}
	for i, rs := range results {
		for _, r := range rs {
			out.Candidates = append(out.Candidates, found(ps[i].Manifest.ID, r))
		}
	}
	return nil, out, nil
}
