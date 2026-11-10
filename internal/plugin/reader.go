package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"dalibre/internal/reader"
)

// Limits on what a reader plugin may return.
const (
	// MaxReadingChapters bounds the chapters a book may have on the page.
	MaxReadingChapters = 2000
	// MaxReadingBlocks bounds the blocks of a whole book.
	MaxReadingBlocks = 60_000
	// MaxReadingBlockRunes bounds one paragraph; longer ones are cut.
	MaxReadingBlockRunes = 20_000
	// MaxReadingRuns bounds the styled stretches in one block; a block
	// longer than this has lost its emphasis, not its text.
	MaxReadingRuns = 10_000
	// MaxReadingTitleRunes bounds a chapter's or the book's title.
	MaxReadingTitleRunes = 300
	// MaxReadingNotes bounds a chapter's notes, and MaxReadingNoteRunes
	// one note's text; longer notes are cut.
	MaxReadingNotes     = reader.MaxNotes
	MaxReadingNoteRunes = reader.MaxNoteRunes
)

// Reader is a plugin that lays out the books the user opens in Dalibre:
// it turns the text and pictures the host extracted from a book into
// chapters and blocks, with the book's title, author and typography. The
// page itself is Dalibre's, as every page is; the plugin decides what it
// says, not how it looks.
type Reader struct {
	// Formats are those the plugin reads, from those the host extracts.
	Formats []string `json:"formats"`
}

// ReadsFormat reports whether the plugin reads format.
func (r *Reader) ReadsFormat(format string) bool {
	return r != nil && slices.Contains(r.Formats, strings.ToUpper(format))
}

// ReaderBook is what a reader plugin is told about the book it lays out:
// what the library knows of it, and the format being read.
type ReaderBook struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Authors  []string `json:"authors,omitempty"`
	Language string   `json:"language,omitempty"`
	Format   string   `json:"format"`
}

// OpenInput is what open receives: the book, and the text and picture
// names Dalibre extracted from its file.
type OpenInput struct {
	Book     ReaderBook      `json:"book"`
	Document reader.Document `json:"document"`
}

// Reading is what open returns: the book as the page shows it.
type Reading struct {
	Title      string           `json:"title,omitempty"`
	Author     string           `json:"author,omitempty"`
	Chapters   []reader.Chapter `json:"chapters"`
	Typography Typography       `json:"typography,omitzero"`
	// Truncated says the book was cut to what the page reads.
	Truncated bool `json:"truncated,omitempty"`
	// Warnings say what was wrong with what the plugin returned; logged
	// for its author, not shown to the reader.
	Warnings []string `json:"-"`
}

// Typography is how the plugin asks the page to set the text.
type Typography struct {
	// Indent indents the first line of each paragraph, as Chinese and
	// Japanese books do. The page also does for those languages when the
	// plugin doesn't say.
	Indent bool `json:"indent,omitempty"`
}

// OpenReader lays out a book with a reader plugin: it calls the plugin's
// open({book, document}) and checks what it returns. A malformed reading
// is an error, as the page has nothing to show; bad single blocks are
// dropped with a warning.
func (p *Plugin) OpenReader(ctx context.Context, book ReaderBook, doc reader.Document) (*Reading, error) {
	m := p.Manifest
	if m.Capabilities.Reader == nil {
		return nil, &Error{Plugin: m.ID, Call: "open", Kind: KindInvalid, Msg: "isn’t a reader"}
	}
	book.Format = strings.ToUpper(strings.TrimSpace(book.Format))
	if !m.Capabilities.Reader.ReadsFormat(book.Format) {
		return nil, &Error{Plugin: m.ID, Call: "open", Kind: KindInvalid,
			Msg: fmt.Sprintf("doesn’t read %s books", book.Format)}
	}
	book.Title = strings.TrimSpace(book.Title)
	book.Authors = slices.DeleteFunc(slices.Clone(book.Authors), func(a string) bool { return strings.TrimSpace(a) == "" })
	if book.Authors == nil {
		book.Authors = []string{}
	}
	if len(doc.Chapters) == 0 {
		return nil, &Error{Plugin: m.ID, Call: "open", Kind: KindInvalid, Msg: "the book has no text to read"}
	}
	raw, err := p.call(ctx, "open", OpenInput{Book: book, Document: doc})
	if err != nil {
		return nil, err
	}
	out, err := parseReading(raw)
	if err != nil {
		return nil, &Error{Plugin: m.ID, Call: "open", Kind: KindInvalid, Msg: err.Error()}
	}
	// The host cut the text; the page says so even if the plugin forgot.
	out.Truncated = out.Truncated || doc.Truncated
	for _, w := range out.Warnings {
		p.logf("[%s] warning: open returned %s", m.ID, w)
	}
	out.Warnings = nil
	return out, nil
}

// cleanCodeText trims a code block's blank ends and trailing spaces, but
// keeps the indentation on its lines.
func cleanCodeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\u00a0", " ")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// fitReadingRuns checks that runs spell text and keeps their emphasis.
// A plugin that changed the text without changing the runs has left
// emphasis pointing at the wrong words, so the text wins and the runs go.
func fitReadingRuns(runs []reader.Run, text string) ([]reader.Run, string) {
	if len(runs) == 0 {
		return nil, ""
	}
	if len(runs) > MaxReadingRuns {
		return nil, fmt.Sprintf("more than %d runs; emphasis dropped", MaxReadingRuns)
	}
	total := 0
	for _, r := range runs {
		total += len(r.Text)
		if total > 4*MaxReadingBlockRunes {
			return nil, "runs are longer than the text; emphasis dropped"
		}
	}
	if reader.RunsText(runs) != text {
		return nil, "runs don’t spell the text; emphasis dropped"
	}
	return reader.CleanRuns(runs, text), ""
}

// parseReading reads what open returned. Unknown fields are ignored: a
// reading may carry more than this Dalibre shows.
func parseReading(raw json.RawMessage) (*Reading, error) {
	var r Reading
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("returned %s; want a reading, an object with chapters", jsonProblem(err))
	}
	var warnings []string
	r.Title = truncateRunes(strings.TrimSpace(r.Title), MaxReadingTitleRunes)
	r.Author = truncateRunes(strings.TrimSpace(r.Author), 300)
	if len(r.Chapters) > MaxReadingChapters {
		warnings = append(warnings, fmt.Sprintf("%d chapters; only the first %d are kept", len(r.Chapters), MaxReadingChapters))
		r.Chapters = r.Chapters[:MaxReadingChapters]
		r.Truncated = true
	}
	out := make([]reader.Chapter, 0, len(r.Chapters))
	blocks, runes := 0, 0
	for i, ch := range r.Chapters {
		at := fmt.Sprintf("chapters[%d]", i)
		ch.Title = truncateRunes(strings.TrimSpace(ch.Title), MaxReadingTitleRunes)
		ch.Depth = clampDepth(ch.Depth)
		kept := make([]reader.Block, 0, len(ch.Blocks))
		for j, b := range ch.Blocks {
			b.Kind = strings.ToLower(strings.TrimSpace(b.Kind))
			if b.Kind != reader.Code {
				b.Text = strings.TrimSpace(b.Text)
			}
			switch b.Kind {
			case reader.Code:
				b.Text = cleanCodeText(b.Text)
				b.Level, b.Src, b.Alt, b.Caption, b.Runs = 0, "", "", "", nil
				b.Table = nil
				if b.Text == "" {
					warnings = append(warnings, fmt.Sprintf("%s.blocks[%d]: empty code; dropped", at, j))
					continue
				}
				if n := len([]rune(b.Text)); n > MaxReadingBlockRunes {
					warnings = append(warnings, fmt.Sprintf("%s.blocks[%d]: %d characters; cut to %d", at, j, n, MaxReadingBlockRunes))
					b.Text = truncateRunes(b.Text, MaxReadingBlockRunes)
				}
			case reader.Paragraph, reader.Quote:
				if b.Text == "" {
					warnings = append(warnings, fmt.Sprintf("%s.blocks[%d]: empty %s; dropped", at, j, b.Kind))
					continue
				}
				if n := len([]rune(b.Text)); n > MaxReadingBlockRunes {
					warnings = append(warnings, fmt.Sprintf("%s.blocks[%d]: %d characters; cut to %d", at, j, n, MaxReadingBlockRunes))
					b.Text = truncateRunes(b.Text, MaxReadingBlockRunes)
				}
				if runs, problem := fitReadingRuns(b.Runs, b.Text); problem != "" {
					warnings = append(warnings, fmt.Sprintf("%s.blocks[%d]: %s", at, j, problem))
					b.Runs = nil
				} else {
					b.Runs = runs
				}
			case reader.Heading:
				if b.Text == "" {
					warnings = append(warnings, fmt.Sprintf("%s.blocks[%d]: empty heading; dropped", at, j))
					continue
				}
				if b.Level < 1 || b.Level > 6 {
					b.Level = 2
				}
				b.Text = truncateRunes(b.Text, MaxReadingTitleRunes)
				if runs, problem := fitReadingRuns(b.Runs, b.Text); problem != "" {
					warnings = append(warnings, fmt.Sprintf("%s.blocks[%d]: %s", at, j, problem))
					b.Runs = nil
				} else {
					b.Runs = runs
				}
			case reader.Divider:
				b.Text, b.Runs = "", nil
			case reader.Image:
				b.Text, b.Level, b.Runs = "", 0, nil
				b.Src = strings.TrimSpace(b.Src)
				b.Alt = truncateRunes(strings.TrimSpace(b.Alt), MaxReadingTitleRunes)
				b.Caption = truncateRunes(strings.TrimSpace(b.Caption), 600)
				if b.Src == "" {
					warnings = append(warnings, fmt.Sprintf("%s.blocks[%d]: image with no src; dropped", at, j))
					continue
				}
				if len(b.Src) > 2_000 {
					warnings = append(warnings, fmt.Sprintf("%s.blocks[%d].src: too long; dropped", at, j))
					continue
				}
			case reader.TableBlock:
				b.Text, b.Level, b.Src, b.Alt, b.Caption, b.Runs = "", 0, "", "", "", nil
				b.Table = parseTable(b.Table)
				if b.Table == nil {
					warnings = append(warnings, fmt.Sprintf("%s.blocks[%d]: table with no text; dropped", at, j))
					continue
				}
			default:
				warnings = append(warnings, fmt.Sprintf("%s.blocks[%d].kind: %q isn’t %q, %q, %q, %q, %q, %q or %q; dropped", at, j, b.Kind, reader.Heading, reader.Paragraph, reader.Quote, reader.Divider, reader.Image, reader.TableBlock, reader.Code))
				continue
			}
			if blocks++; blocks > MaxReadingBlocks {
				warnings = append(warnings, fmt.Sprintf("more than %d blocks; the rest is dropped", MaxReadingBlocks))
				r.Truncated = true
				break
			}
			runes += readingBlockRunes(b)
			if runes > reader.MaxText {
				warnings = append(warnings, fmt.Sprintf("more than %d characters; the rest is dropped", reader.MaxText))
				r.Truncated = true
				break
			}
			kept = append(kept, b)
		}
		if len(kept) == 0 {
			warnings = append(warnings, at+": no blocks; dropped")
			continue
		}
		ch.Blocks = kept
		notes := make([]reader.Note, 0, len(ch.Notes))
		for k, n := range ch.Notes {
			if len(notes) >= MaxReadingNotes {
				warnings = append(warnings, fmt.Sprintf("%s.notes: more than %d; the rest is dropped", at, MaxReadingNotes))
				r.Truncated = true
				break
			}
			n.ID = strings.TrimSpace(n.ID)
			n.Label = truncateRunes(strings.TrimSpace(n.Label), 16)
			n.Text = truncateRunes(strings.TrimSpace(n.Text), MaxReadingNoteRunes)
			if n.Text == "" {
				warnings = append(warnings, fmt.Sprintf("%s.notes[%d]: empty note; dropped", at, k))
				continue
			}
			if n.Label == "" {
				n.Label = fmt.Sprint(len(notes) + 1)
			}
			if len(n.ID) > 200 {
				n.ID = ""
			}
			nr := len([]rune(n.Text))
			if runes+nr > reader.MaxText {
				warnings = append(warnings, fmt.Sprintf("more than %d characters; the rest is dropped", reader.MaxText))
				r.Truncated = true
				break
			}
			runes += nr
			notes = append(notes, n)
		}
		ch.Notes = notes
		if len(out) == 0 {
			ch.Continued = false
		}
		out = append(out, ch)
		if blocks > MaxReadingBlocks || runes > reader.MaxText {
			break
		}
	}
	if len(out) == 0 {
		return &Reading{Warnings: warnings}, errors.New("returned no chapters with text; want at least one")
	}
	r.Chapters = out
	r.Warnings = warnings
	return &r, nil
}

// clampDepth keeps a chapter's nesting on the page's six levels.
func clampDepth(d int) int {
	if d < 1 {
		return 1
	}
	if d > 6 {
		return 6
	}
	return d
}

// parseTable trims a table a plugin returned, bounds its cells and says
// whether anything is left to show.
func parseTable(t *reader.Table) *reader.Table {
	if t == nil {
		return nil
	}
	out := &reader.Table{Caption: truncateRunes(strings.TrimSpace(t.Caption), 600)}
	cells := 0
	hasText := false
	for _, row := range t.Rows {
		if len(out.Rows) >= reader.MaxTableRows {
			break
		}
		cellsOut := make([]reader.TableCell, 0, len(row.Cells))
		for _, c := range row.Cells {
			if cells >= reader.MaxTableCells {
				break
			}
			c.Text = truncateRunes(strings.TrimSpace(c.Text), reader.MaxTableCellRunes)
			c.ColSpan = clampSpan(c.ColSpan)
			c.RowSpan = clampSpan(c.RowSpan)
			if c.Text != "" {
				hasText = true
			}
			cellsOut = append(cellsOut, c)
			cells++
		}
		if len(cellsOut) > 0 {
			out.Rows = append(out.Rows, reader.TableRow{Cells: cellsOut})
		}
	}
	if len(out.Rows) == 0 && out.Caption == "" {
		return nil
	}
	if !hasText && out.Caption == "" {
		return nil
	}
	return out
}

func clampSpan(n int) int {
	if n < 1 {
		return 1
	}
	if n > 32 {
		return 32
	}
	return n
}

// readingBlockRunes is the text budget a block uses, cells included.
func readingBlockRunes(b reader.Block) int {
	n := len([]rune(b.Text)) + len([]rune(b.Alt)) + len([]rune(b.Caption))
	if b.Table != nil {
		n += len([]rune(b.Table.Caption))
		for _, row := range b.Table.Rows {
			for _, c := range row.Cells {
				n += len([]rune(c.Text))
			}
		}
	}
	return n
}

func truncateRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max])) + "…"
}
