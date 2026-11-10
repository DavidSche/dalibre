package reader

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// EPUB reading: the container names the OPF package, the package its
// spine (the reading order) and a navigation document or NCX (the table of
// contents), and each spine document is XHTML whose block elements become
// paragraphs.

type opfPackage struct {
	Metadata struct {
		Titles    []string `xml:"title"`
		Languages []string `xml:"language"`
	} `xml:"metadata"`
	Manifest struct {
		Items []struct {
			ID         string `xml:"id,attr"`
			Href       string `xml:"href,attr"`
			MediaType  string `xml:"media-type,attr"`
			Properties string `xml:"properties,attr"`
		} `xml:"item"`
	} `xml:"manifest"`
	Spine struct {
		Toc   string `xml:"toc,attr"`
		Items []struct {
			IDRef  string `xml:"idref,attr"`
			Linear string `xml:"linear,attr"`
		} `xml:"itemref"`
	} `xml:"spine"`
}

type epubFile struct {
	zr    *zip.ReadCloser
	files []*zip.File
}

func (e *epubFile) find(name string) *zip.File {
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	for _, f := range e.files {
		if f.Name == name {
			return f
		}
	}
	// Some books write names with another case or escaped.
	if u, err := url.PathUnescape(name); err == nil && u != name {
		for _, f := range e.files {
			if f.Name == u {
				return f
			}
		}
	}
	for _, f := range e.files {
		if strings.EqualFold(f.Name, name) {
			return f
		}
	}
	return nil
}

func (e *epubFile) read(name string, max int64) ([]byte, error) {
	f := e.find(name)
	if f == nil {
		return nil, os.ErrNotExist
	}
	if int64(f.UncompressedSize64) > max {
		return nil, errTooBig
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errTooBig
	}
	return data, nil
}

func readEPUB(file string) (*Document, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return nil, fmt.Errorf("epub: %w", err)
	}
	defer zr.Close()
	e := &epubFile{zr: zr, files: zr.File}

	container, err := e.read("META-INF/container.xml", 1<<20)
	if err != nil {
		return nil, errors.New("epub: no META-INF/container.xml")
	}
	var c struct {
		Rootfiles []struct {
			Path string `xml:"full-path,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if err := xml.Unmarshal(container, &c); err != nil || len(c.Rootfiles) == 0 {
		return nil, errors.New("epub: container names no package")
	}
	opfPath := strings.TrimPrefix(path.Clean("/"+c.Rootfiles[0].Path), "/")
	data, err := e.read(opfPath, 4<<20)
	if err != nil {
		return nil, fmt.Errorf("epub: reading the package: %w", err)
	}
	var pkg opfPackage
	if err := xml.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("epub: %w", err)
	}
	doc := &Document{Images: map[string][]byte{}}
	if len(pkg.Metadata.Titles) > 0 {
		doc.Title = collapse(pkg.Metadata.Titles[0], false)
	}
	if len(pkg.Metadata.Languages) > 0 {
		doc.Language = strings.TrimSpace(pkg.Metadata.Languages[0])
	}

	// The navigation document (EPUB 3) or the NCX (EPUB 2) names the
	// chapters and their nesting. A chapter that came without a name is
	// named by the reader plugin from its first heading.
	var sections []navSection
	var navPath, ncxPath string
	items := map[string]struct{ href, mediaType, properties string }{}
	for _, it := range pkg.Manifest.Items {
		items[it.ID] = struct{ href, mediaType, properties string }{it.Href, it.MediaType, it.Properties}
		if strings.Contains(" "+it.Properties+" ", " nav ") {
			navPath = resolve(opfPath, it.Href)
		}
	}
	if pkg.Spine.Toc != "" {
		if it, ok := items[pkg.Spine.Toc]; ok {
			ncxPath = resolve(opfPath, it.href)
		}
	}
	if ncxPath == "" {
		// EPUB 3 books may keep an NCX without naming it in the spine,
		// as a fallback for readers that read EPUB 2. Use it when the
		// navigation document is missing or broken.
		for _, it := range pkg.Manifest.Items {
			if strings.EqualFold(it.MediaType, "application/x-dtbncx+xml") ||
				strings.EqualFold(path.Ext(it.Href), ".ncx") {
				ncxPath = resolve(opfPath, it.Href)
				break
			}
		}
	}
	if navPath != "" {
		if data, err := e.read(navPath, 4<<20); err == nil {
			sections = rebaseSections(navEntries(data), navPath, opfPath, e)
		}
	}
	if len(sections) == 0 && ncxPath != "" {
		if data, err := e.read(ncxPath, 4<<20); err == nil {
			sections = rebaseSections(ncxEntries(data), ncxPath, opfPath, e)
		}
	}
	byFile := map[string][]navSection{}
	for _, s := range sections {
		byFile[s.File] = append(byFile[s.File], s)
	}

	text := 0
	notes := &noteIndex{docs: map[string]*noteDoc{}, notes: map[string]Note{}}
	for _, ref := range pkg.Spine.Items {
		it, ok := items[ref.IDRef]
		if !ok || it.href == "" {
			continue
		}
		if strings.EqualFold(ref.Linear, "no") {
			// Notes and other auxiliary documents aren't part of the
			// reading order.
			continue
		}
		if strings.Contains(" "+it.properties+" ", " nav ") {
			continue
		}
		name := resolve(opfPath, it.href)
		if !isHTML(it.mediaType, name) {
			continue
		}
		body, err := e.read(name, MaxText)
		if errors.Is(err, errTooBig) {
			doc.Truncated = true
			break
		}
		if err != nil {
			continue
		}
		h := parseHTML(body)
		if h == nil {
			continue
		}
		if len(byFile[name]) == 0 && len(doc.Chapters) > 0 && !h.hasHeading() {
			// A file the table doesn't name, after a chapter: usually the
			// rest of that chapter when a book splits one across files,
			// or a plate set beside it. Join it to the chapter rather
			// than give the contents another unnamed entry.
			blocks, chNotes := h.blocks(name, e, doc.Images, notes, "", "")
			if len(blocks) == 0 {
				continue
			}
			prev := &doc.Chapters[len(doc.Chapters)-1]
			prev.Blocks = append(prev.Blocks, cleanBlocks(blocks)...)
			prev.Notes = append(prev.Notes, cleanNotes(chNotes)...)
			for _, b := range blocks {
				text += blockRunes(b)
			}
			if text >= MaxText {
				doc.Truncated = true
				break
			}
			continue
		}
		parts := sectionsFor(h, byFile[name])
		for i, part := range parts {
			to := ""
			for j := i + 1; j < len(parts); j++ {
				if parts[j].ID != "" {
					to = parts[j].ID
					break
				}
			}
			blocks, chNotes := h.blocks(name, e, doc.Images, notes, part.ID, to)
			if i == 0 && part.ID != "" {
				// A title page or preface before the first named section
				// belongs with it, not in a chapter of its own.
				pre, preNotes := h.blocks(name, e, doc.Images, notes, "", part.ID)
				blocks = append(pre, blocks...)
				chNotes = append(preNotes, chNotes...)
			}
			if len(blocks) == 0 {
				continue
			}
			ch := Chapter{Title: part.Title, Depth: part.Depth, Blocks: blocks, Notes: chNotes}
			for _, b := range ch.Blocks {
				text += blockRunes(b)
			}
			if !doc.addChapter(ch) {
				doc.Truncated = true
				break
			}
			if text >= MaxText {
				doc.Truncated = true
				break
			}
		}
		if doc.Truncated {
			break
		}
	}
	if len(doc.Chapters) == 0 {
		return nil, errors.New("epub: the book has no text to read")
	}
	return doc, nil
}

// sectionsFor picks the table of contents' sections for one content file.
// A file without an entry is one chapter; a link to an id the file doesn't
// have is skipped, so a broken table can't hide the text around it.
func sectionsFor(h *htmlDoc, all []navSection) []navSection {
	out := make([]navSection, 0, len(all))
	whole := false
	for _, s := range all {
		if s.ID != "" && h.ids[s.ID] == nil {
			continue
		}
		if s.ID == "" {
			if whole {
				continue
			}
			whole = true
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return []navSection{{Depth: 1}}
	}
	return out
}

func isHTML(mediaType, name string) bool {
	if mediaType != "" {
		return mediaType == "application/xhtml+xml" || mediaType == "text/html"
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".xhtml", ".html", ".htm":
		return true
	}
	return false
}

// resolve resolves href, relative to the package at base, to a name in the
// zip, without its fragment or query.
func resolve(base, href string) string {
	href = strings.TrimSpace(href)
	if i := strings.IndexAny(href, "#?"); i >= 0 {
		href = href[:i]
	}
	if u, err := url.PathUnescape(href); err == nil {
		href = u
	}
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "/") {
		return strings.TrimPrefix(path.Clean(href), "/")
	}
	return path.Clean(path.Join(path.Dir(base), href))
}

// navPoint is one NCX navigation point.
type navPoint struct {
	Label   string `xml:"navLabel>text"`
	Content struct {
		Src string `xml:"src,attr"`
	} `xml:"content"`
	Children []navPoint `xml:"navPoint"`
}

// cleanHref is a navigation href without its fragment or query, for a key.
func cleanHref(href string) string {
	if i := strings.IndexAny(href, "#?"); i >= 0 {
		href = href[:i]
	}
	if u, err := url.PathUnescape(href); err == nil {
		href = u
	}
	return path.Clean(href)
}

// withoutScripts drops script and style elements before HTML parsing. An
// XHTML document may write one as <script src="..."/>, which HTML takes
// as an opening tag whose content runs to the end of the document,
// leaving no navigation lists or text after it.
func withoutScripts(data []byte) []byte {
	lower := bytes.ToLower(data)
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); {
		j, closeTag := -1, []byte("</script")
		for _, tag := range []string{"<script", "<style"} {
			if k := bytes.Index(lower[i:], []byte(tag)); k >= 0 && (j < 0 || k < j) {
				j = k
				if tag == "<style" {
					closeTag = []byte("</style")
				}
			}
		}
		if j < 0 {
			out = append(out, data[i:]...)
			break
		}
		j += i
		out = append(out, data[i:j]...)
		gt := bytes.IndexByte(data[j:], '>')
		if gt < 0 {
			break // malformed: drop the rest rather than parse it as text
		}
		gt += j
		if bytes.HasSuffix(bytes.TrimSpace(data[j:gt]), []byte("/")) {
			i = gt + 1 // empty, as XHTML writes it
			continue
		}
		end := bytes.Index(lower[gt+1:], closeTag)
		if end < 0 {
			i = len(data)
			break
		}
		end += gt + 1
		endGT := bytes.IndexByte(data[end:], '>')
		if endGT < 0 {
			i = len(data)
			break
		}
		i = end + endGT + 1
	}
	return out
}

// navEntry is one link in a book's table of contents. Depth is the
// nesting the book gives it: a part is 1, a chapter under it 2.
type navEntry struct {
	Title string
	Href  string
	Depth int
}

// navSection is a navEntry resolved to a file in the zip and an id in it.
type navSection struct {
	File  string
	ID    string
	Title string
	Depth int
}

// navEntries reads an EPUB 3 navigation document: the toc nav's nested
// lists, flattened in reading order with their depth.
func navEntries(data []byte) []navEntry {
	doc, err := html.Parse(bytes.NewReader(withoutScripts(data)))
	if err != nil {
		return nil
	}
	var toc, first *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "nav" {
			if first == nil {
				first = n
			}
			if strings.Contains(" "+strings.ToLower(attr(n, "epub:type"))+" ", " toc ") {
				toc = n
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	nav := toc
	if nav == nil {
		nav = first
	}
	if nav == nil {
		return nil
	}

	var out []navEntry
	var list func(*html.Node, int)
	list = func(ol *html.Node, depth int) {
		for li := ol.FirstChild; li != nil; li = li.NextSibling {
			if li.Type != html.ElementNode || li.Data != "li" {
				continue
			}
			var label, sub *html.Node
			for k := li.FirstChild; k != nil; k = k.NextSibling {
				if k.Type != html.ElementNode {
					continue
				}
				switch k.Data {
				case "ol", "ul":
					if sub == nil {
						sub = k
					}
				case "a", "span":
					if label == nil {
						label = k
					}
				}
			}
			if label != nil {
				href := attr(label, "href")
				if title := collapse(nodeText(label), false); href != "" && title != "" {
					out = append(out, navEntry{Title: title, Href: href, Depth: depth})
				}
			}
			if sub != nil {
				list(sub, depth+1)
			}
		}
	}
	for c := nav.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "ol" || c.Data == "ul") {
			list(c, 1)
		}
	}
	if len(out) == 0 {
		// A nav without lists: its links still name the chapters, if not
		// their places in a hierarchy.
		var links func(*html.Node)
		links = func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "a" {
				if href := attr(n, "href"); href != "" {
					if title := collapse(nodeText(n), false); title != "" {
						out = append(out, navEntry{Title: title, Href: href, Depth: 1})
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				links(c)
			}
		}
		links(nav)
	}
	return out
}

// ncxEntries reads an EPUB 2 NCX's navigation map, keeping its depth.
func ncxEntries(data []byte) []navEntry {
	var ncx struct {
		NavMap struct {
			Points []navPoint `xml:"navPoint"`
		} `xml:"navMap"`
	}
	if err := xml.Unmarshal(data, &ncx); err != nil {
		return nil
	}
	var out []navEntry
	var walk func(ps []navPoint, depth int)
	walk = func(ps []navPoint, depth int) {
		for _, p := range ps {
			if title := collapse(p.Label, false); p.Content.Src != "" && title != "" {
				out = append(out, navEntry{Title: title, Href: p.Content.Src, Depth: depth})
			}
			walk(p.Children, depth+1)
		}
	}
	walk(ncx.NavMap.Points, 1)
	return out
}

// rebaseSections resolves the navigation's hrefs, relative to the
// navigation document or the package, to files in the zip. Books disagree
// about which of the two they are relative to, so both are tried, and then
// the zip's root.
func rebaseSections(entries []navEntry, nav, pkg string, e *epubFile) []navSection {
	out := make([]navSection, 0, len(entries))
	seen := map[string]bool{}
	for _, en := range entries {
		href, id := splitFragment(en.Href)
		key := resolve(nav, href)
		if e.find(key) == nil {
			key = resolve(pkg, href)
		}
		if e.find(key) == nil {
			// Some books write the path from the zip's root, not from
			// the navigation or the package.
			if alt := cleanHref(href); e.find(alt) != nil {
				key = alt
			}
		}
		if key == "" || e.find(key) == nil {
			continue
		}
		seenKey := key + "#" + id
		if seen[seenKey] {
			continue
		}
		seen[seenKey] = true
		depth := en.Depth
		if depth < 1 {
			depth = 1
		}
		out = append(out, navSection{File: key, ID: id, Title: en.Title, Depth: depth})
	}
	return out
}

// splitFragment splits an href into its file and its percent-decoded id.
func splitFragment(href string) (file, id string) {
	href = strings.TrimSpace(href)
	if i := strings.IndexByte(href, '#'); i >= 0 {
		href, id = href[:i], href[i+1:]
	}
	if i := strings.IndexByte(id, '?'); i >= 0 {
		id = id[:i]
	}
	if u, err := url.PathUnescape(id); err == nil {
		id = u
	}
	return strings.TrimSpace(href), strings.TrimSpace(id)
}

// htmlSource reads a content document's resources: pictures, and the
// notes file an EPUB keeps beside its text. It keeps what a book is made
// of out of the text walker: the walker reads the same markup whether it
// came from a zip or a Palm database.
type htmlSource interface {
	read(name string, max int64) ([]byte, error)
}

var _ htmlSource = (*epubFile)(nil)

// htmlDoc is a parsed content document, kept once while its sections are
// read. A table of contents may put several sections in one file.
type htmlDoc struct {
	root  *html.Node
	ids   map[string]*html.Node
	order map[*html.Node]int
}

func parseHTML(data []byte) *htmlDoc {
	root, err := html.Parse(bytes.NewReader(withoutScripts(data)))
	if err != nil {
		return nil
	}
	order := map[*html.Node]int{}
	n := 0
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		order[node] = n
		n++
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return &htmlDoc{root: root, ids: idsOf(root), order: order}
}

// hasHeading reports whether the document has a heading with text, which
// starts a section of its own even when the table doesn't name the file.
func (h *htmlDoc) hasHeading() bool {
	found := false
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found {
			return
		}
		if n.Type == html.ElementNode && headingLevel(n.Data) > 0 && strings.TrimSpace(nodeText(n)) != "" {
			found = true
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(h.root)
	return found
}

// blocks reads one section of the document: from id from to id to, which
// may be empty for the document's start or end.
func (h *htmlDoc) blocks(base string, src htmlSource, images map[string][]byte, notes *noteIndex, from, to string) ([]Block, []Note) {
	if h == nil {
		return nil, nil
	}
	if from != "" && h.ids[from] == nil {
		return nil, nil
	}
	if images == nil {
		images = map[string][]byte{}
	}
	if notes == nil {
		notes = &noteIndex{docs: map[string]*noteDoc{}, notes: map[string]Note{}}
	}
	total := 0
	for _, img := range images {
		total += len(img)
	}
	b := &blockReader{base: base, source: src, images: images, imageTotal: total,
		index: notes, ids: h.ids, noteSeen: map[string]bool{}}
	b.start, b.stop = h.bounds(from, to)
	if b.start == nil {
		b.walk(h.root)
	} else {
		// Start at the section's first block and walk on from there, not
		// from the document's start: a file can hold a whole book.
		for n := b.start; n != nil && !b.stopped; n = nextAfter(n) {
			b.walk(n)
		}
	}
	b.flush()
	b.finishCode()
	return b.blocks, b.notes
}

// nextAfter is the node after n's subtree, in reading order.
func nextAfter(n *html.Node) *html.Node {
	for p := n; p != nil; p = p.Parent {
		if p.NextSibling != nil {
			return p.NextSibling
		}
	}
	return nil
}

// bounds finds the block elements that begin and end a section. A
// navigation link often sits in an empty anchor before a heading or inside
// one; the section starts at the block, not the inline anchor.
func (h *htmlDoc) bounds(from, to string) (*html.Node, *html.Node) {
	var start, stop *html.Node
	if n := h.ids[from]; n != nil {
		start = sectionBlock(n)
	}
	if n := h.ids[to]; n != nil {
		stop = sectionBlock(n)
	}
	if start != nil && stop != nil {
		if start == stop {
			// Two anchors in one block: use the anchors themselves, so
			// the text between them is still a section.
			start, stop = h.ids[from], h.ids[to]
			if start == stop || h.order[stop] <= h.order[start] {
				stop = nil
			}
		} else if h.order[stop] <= h.order[start] {
			stop = nil
		}
	}
	return start, stop
}

// sectionBlock is the block a section boundary names: the nearest block
// around an anchor, or the block after an empty anchor.
func sectionBlock(n *html.Node) *html.Node {
	if n == nil {
		return nil
	}
	for p := n; p != nil; p = p.Parent {
		if isSectionBlock(p) {
			return p
		}
		if p.Parent == nil {
			break
		}
		if p.Parent.Data == "body" || p.Parent.Data == "html" {
			if strings.TrimSpace(nodeText(n)) == "" {
				for s := p.NextSibling; s != nil; s = s.NextSibling {
					if s.Type == html.ElementNode && isSectionBlock(s) {
						return s
					}
				}
			}
			return p
		}
	}
	return n
}

// isSectionBlock marks the elements a table of contents can start a
// section at. Inline anchors climb to one of these.
func isSectionBlock(n *html.Node) bool {
	if n == nil || n.Type != html.ElementNode {
		return false
	}
	switch n.Data {
	case "h1", "h2", "h3", "h4", "h5", "h6", "p", "div", "section", "article",
		"header", "footer", "main", "aside", "blockquote", "pre", "figure",
		"table", "ul", "ol", "li", "dl", "dt", "dd", "hr", "form", "center", "address":
		return true
	}
	return false
}

// noteIndex keeps the note definitions read from a book: a reference may
// point into the same document or, as endnotes often do, into a file that
// isn't part of the reading order.
type noteIndex struct {
	docs  map[string]*noteDoc
	notes map[string]Note
}

type noteDoc struct {
	base string
	ids  map[string]*html.Node
}

// idsOf maps the ids in a document to their elements, first one wins.
func idsOf(root *html.Node) map[string]*html.Node {
	out := map[string]*html.Node{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if id := strings.TrimSpace(attr(n, "id")); id != "" {
				if _, ok := out[id]; !ok {
					out[id] = n
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}

// noteRef turns an anchor that points at a note into its marker in the
// text, and remembers the note for the end of the chapter. An ordinary
// link is left for the text walker to read.
func (b *blockReader) noteRef(n *html.Node) bool {
	href := strings.TrimSpace(attr(n, "href"))
	if href == "" || !strings.Contains(href, "#") {
		return false
	}
	doc, id := noteHref(b.base, href)
	if id == "" {
		return false
	}
	note, ok := b.noteAt(doc, id, n)
	if !ok {
		return false
	}
	key := doc + "#" + id
	if !b.addNote(key, note) {
		return false
	}
	b.write("[" + note.Label + "]")
	return true
}

// noteHref splits a reference into the document it names and its id.
// A bare fragment names the current document.
func noteHref(base, href string) (doc, id string) {
	i := strings.IndexByte(href, '#')
	if i < 0 {
		return "", ""
	}
	doc, id = href[:i], href[i+1:]
	if u, err := url.PathUnescape(id); err == nil {
		id = u
	}
	if doc == "" {
		return base, id
	}
	return resolve(base, doc), id
}

// noteAt finds the note id names, reading another document when the
// reference goes there. A definition is cached with the label its first
// reference shows.
func (b *blockReader) noteAt(doc, id string, ref *html.Node) (Note, bool) {
	if b.index == nil {
		return Note{}, false
	}
	key := doc + "#" + id
	if n, ok := b.index.notes[key]; ok {
		return n, true
	}
	if doc != b.base && !isNoteRef(ref) && !isMarkerText(ref) {
		// A link from one chapter to a section of another isn't a note
		// unless it says so, or reads like a note's number.
		return Note{}, false
	}
	ids, ok := b.noteDoc(doc)
	if !ok {
		return Note{}, false
	}
	target := ids[id]
	if target == nil {
		return Note{}, false
	}
	if !isNoteRef(ref) && !looksLikeNote(target) {
		return Note{}, false
	}
	node := noteNode(target)
	if node == nil {
		return Note{}, false
	}
	label := noteLabel(ref, len(b.index.notes))
	text := trimNoteLabel(noteText(node), label)
	if text == "" {
		return Note{}, false
	}
	note := Note{ID: id, Label: label, Text: text}
	b.index.notes[key] = note
	return note, true
}

// noteDoc is the ids of doc, parsed once. The current document already
// has them.
func (b *blockReader) noteDoc(doc string) (map[string]*html.Node, bool) {
	if doc == "" || doc == b.base {
		return b.ids, b.ids != nil
	}
	if d := b.index.docs[doc]; d != nil {
		return d.ids, true
	}
	if b.source == nil {
		return nil, false
	}
	data, err := b.source.read(doc, MaxText)
	if err != nil || len(data) == 0 {
		return nil, false
	}
	root, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}
	d := &noteDoc{base: doc, ids: idsOf(root)}
	b.index.docs[doc] = d
	return d.ids, true
}

// addNote keeps a note at the end of the chapter, once; it reports
// whether the note is there, so a reference past the limit isn't marked
// with no note to read.
func (b *blockReader) addNote(key string, n Note) bool {
	if b.noteSeen == nil {
		b.noteSeen = map[string]bool{}
	}
	if b.noteSeen[key] {
		return true
	}
	if len(b.notes) >= MaxNotes {
		return false
	}
	b.noteSeen[key] = true
	b.notes = append(b.notes, n)
	return true
}

// trimNoteLabel takes the marker a note repeats from its first line, as
// "1. The hall" does, so the page doesn't set the number twice.
func trimNoteLabel(text, label string) string {
	text = strings.TrimSpace(text)
	if label == "" {
		return text
	}
	for _, sep := range []string{".", ")", ":", "]", "、", "．"} {
		if strings.HasPrefix(text, label+sep) {
			return strings.TrimSpace(text[len(label+sep):])
		}
	}
	if strings.HasPrefix(text, "["+label+"]") {
		return strings.TrimSpace(text[len(label)+2:])
	}
	return text
}

// noteLabel is the marker a reference shows, as a book writes it: 1, *,
// ¹, or a short word. A reference with no text gets the next place.
func noteLabel(ref *html.Node, n int) string {
	label := collapse(nodeText(ref), false)
	label = strings.TrimSpace(strings.Trim(label, "[]()（）"))
	if r := []rune(label); len(r) > 16 {
		label = strings.TrimSpace(string(r[:16]))
	}
	if label == "" {
		label = strconv.Itoa(n + 1)
	}
	return label
}

// noteNode picks the element that holds a note's text. A definition may
// be an aside, a list item, or an anchor inside one; a whole footnotes
// section is not one note.
func noteNode(target *html.Node) *html.Node {
	if target == nil {
		return nil
	}
	if isNoteContainer(target) {
		return target
	}
	switch target.Data {
	case "li", "dd", "dt", "p", "div", "section", "article", "aside":
		return target
	}
	for p := target.Parent; p != nil; p = p.Parent {
		switch p.Data {
		case "li", "dd", "dt", "p":
			return p
		}
		if isNoteContainer(p) {
			return p
		}
	}
	return target
}

// noteText is a note's text, with a blank line between its paragraphs
// and its backlink left out.
func noteText(n *html.Node) string {
	root := n
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n != root && isNoteContainer(n) {
				return
			}
			switch n.Data {
			case "script", "style", "head", "nav":
				return
			case "a":
				if isBacklink(n) {
					return
				}
			case "br":
				b.WriteByte('\n')
				return
			case "img", "image":
				if alt := collapse(attr(n, "alt"), false); alt != "" {
					b.WriteString(" ")
					b.WriteString(alt)
					b.WriteString(" ")
				}
				return
			case "p", "div", "li", "dd", "dt", "blockquote", "section", "article", "figcaption":
				b.WriteString("\n\n")
			}
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return normalizeBreaks(collapse(b.String(), true), 2)
}

// isNoteRef says a reference calls itself a note reference.
func isNoteRef(n *html.Node) bool {
	for _, name := range []string{"epub:type", "role", "class", "rel"} {
		s := strings.ToLower(attr(n, name))
		if strings.Contains(s, "noteref") || strings.Contains(s, "doc-noteref") ||
			strings.Contains(s, "footnote-ref") || strings.Contains(s, "endnote-ref") {
			return true
		}
	}
	return n.Parent != nil && n.Parent.Data == "sup"
}

// isMarkerText says a link's text looks like a note's marker, which is
// enough for a reference to an endnotes file without epub:type.
func isMarkerText(n *html.Node) bool {
	t := strings.TrimSpace(collapse(nodeText(n), false))
	if t == "" || len([]rune(t)) > 8 {
		return false
	}
	for _, r := range t {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '*' || r == '†' || r == '‡' ||
			r == '[' || r == ']' || r == '(' || r == ')' || r == '#' || r == ' ' {
			continue
		}
		return false
	}
	return true
}

// isNoteContainer says an element is a footnote, endnote or a section
// that holds them.
func isNoteContainer(n *html.Node) bool {
	if n == nil || n.Type != html.ElementNode {
		return false
	}
	fields := strings.Fields(strings.ToLower(
		attr(n, "epub:type") + " " + attr(n, "role") + " " + attr(n, "class")))
	for _, f := range fields {
		switch f {
		case "footnote", "footnotes", "endnote", "endnotes", "rearnote", "rearnotes",
			"doc-footnote", "doc-endnote", "doc-rearnote":
			return true
		}
	}
	return false
}

// looksLikeNote says a target is a note or inside one.
func looksLikeNote(n *html.Node) bool {
	if isNoteContainer(n) {
		return true
	}
	for p := n; p != nil; p = p.Parent {
		if isNoteContainer(p) {
			return true
		}
	}
	return false
}

// isBacklink is the return link a note carries to its reference.
func isBacklink(n *html.Node) bool {
	if n == nil || n.Type != html.ElementNode || n.Data != "a" {
		return false
	}
	for _, name := range []string{"epub:type", "role", "class", "rel"} {
		if strings.Contains(strings.ToLower(attr(n, name)), "backlink") {
			return true
		}
	}
	switch strings.TrimSpace(nodeText(n)) {
	case "↩", "↵", "⏎":
		return true
	case "return", "Return", "back", "Back":
		return strings.Contains(attr(n, "href"), "#")
	}
	return false
}

// normalizeBreaks keeps at most max consecutive line breaks. collapse
// leaves them between paragraphs; the page needs no blank runs.
func normalizeBreaks(s string, max int) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	b.Grow(len(s))
	n := 0
	for _, r := range s {
		if r == '\n' {
			n++
			if n <= max {
				b.WriteRune(r)
			}
			continue
		}
		n = 0
		b.WriteRune(r)
	}
	return b.String()
}

type blockReader struct {
	blocks                []Block
	buf                   strings.Builder
	runs                  []Run
	style                 textStyle
	monoRunes, plainRunes int
	firstMono             bool
	firstText             string
	codeBuf               strings.Builder
	quote                 int

	// base is the content document's name, which the pictures' hrefs
	// are relative to. source reads them, into images; it hides whether
	// the book is a zip of XHTML files or a Palm database of text records.
	base       string
	source     htmlSource
	images     map[string][]byte
	imageTotal int

	// notes are the chapter's notes, in the order their references
	// appear; index keeps definitions read from this and other documents,
	// so a reference to a shared notes file reads it once.
	index    *noteIndex
	ids      map[string]*html.Node
	notes    []Note
	noteSeen map[string]bool

	// start and stop bound a section of one document; the reader starts
	// walking at start and stops at stop.
	start   *html.Node
	stop    *html.Node
	stopped bool
}

func (b *blockReader) flush() {
	raw, runs := b.buf.String(), b.runs
	code := b.monoRunes > b.plainRunes || b.firstMono && codeComment(b.firstText)
	b.buf.Reset()
	b.runs = nil
	b.monoRunes, b.plainRunes, b.firstMono, b.firstText = 0, 0, false, ""
	if strings.TrimSpace(raw) == "" {
		return
	}
	if code {
		b.addCodeLine(raw)
		return
	}
	b.finishCode()
	runs = collapseRuns(runs, false)
	text := RunsText(runs)
	if text == "" {
		return
	}
	runs = CleanRuns(runs, text)
	if b.quote > 0 {
		b.blocks = append(b.blocks, Block{Kind: Quote, Text: text, Runs: runs})
		return
	}
	b.blocks = append(b.blocks, Block{Kind: Paragraph, Text: text, Runs: runs})
}

// codeComment reports whether text starts like a comment, the clearest
// sign of a code line whose explanatory words are set in the prose face.
func codeComment(text string) bool {
	for _, mark := range []string{"#", "//", "/*", "* ", "-- ", ";;", "%"} {
		if strings.HasPrefix(text, mark) {
			return true
		}
	}
	return false
}

// addCodeLine joins a paragraph that the book sets in a monospaced font
// to the code block being read. Converters often make each source line a
// paragraph of its own; the line breaks are what make code readable.
func (b *blockReader) addCodeLine(raw string) {
	text := cleanCode(textMarks.Replace(raw))
	if text == "" {
		return
	}
	if b.codeBuf.Len() > 0 {
		b.codeBuf.WriteByte('\n')
	}
	b.codeBuf.WriteString(text)
}

// finishCode adds the code block gathered from monospaced paragraphs.
func (b *blockReader) finishCode() {
	if b.codeBuf.Len() == 0 {
		return
	}
	b.blocks = append(b.blocks, Block{Kind: Code, Text: b.codeBuf.String()})
	b.codeBuf.Reset()
}

// textStyle is the emphasis over the text being read: a strong or em
// element inside a paragraph turns it on for what it holds. Mono marks
// text the book sets in a monospaced font, which is usually source code.
type textStyle struct {
	bold, italic, mono bool
}

// write adds text to the block being read, in the emphasis over it.
func (b *blockReader) write(s string) {
	if s == "" {
		return
	}
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	if b.monoRunes+b.plainRunes == 0 && n > 0 {
		b.firstMono = b.style.mono
		b.firstText = strings.TrimLeftFunc(s, unicode.IsSpace)
	}
	if b.style.mono {
		b.monoRunes += n
	} else {
		b.plainRunes += n
	}
	b.buf.WriteString(s)
	b.runs = appendRun(b.runs, Run{Text: s, Bold: b.style.bold, Italic: b.style.italic})
}

// writeBreak ends a line without deciding whether the block is code: a
// br outside a monospaced span still belongs to a code line.
func (b *blockReader) writeBreak() {
	b.buf.WriteByte('\n')
	b.runs = appendRun(b.runs, Run{Text: "\n", Bold: b.style.bold, Italic: b.style.italic})
}

// walkStyled reads n's children with bold, italic and mono over them.
func (b *blockReader) walkStyled(n *html.Node, bold, italic, mono bool) {
	prev := b.style
	b.style = textStyle{bold: bold, italic: italic, mono: mono}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.walk(c)
	}
	b.style = prev
}

// The families that mean source code rather than prose. A book says so
// inline when its converter drops the <pre> and sets each line in one of
// these, which is the only clue left that a paragraph is a code line.
var monospaceFonts = []string{
	"source code pro", "monospace", "courier", "consolas", "menlo",
	"monaco", "fira code", "fira mono", "jetbrains mono", "roboto mono",
	"ubuntu mono", "inconsolata", "dejavu sans mono", "liberation mono",
	"lucida console", "andale mono", "sf mono", "cascadia code",
	"cascadia mono", "ibm plex mono", "noto sans mono", "pt mono",
	"space mono", "anonymous pro", "iosevka",
}

// monoStyled reports whether n's own style sets a monospaced family.
func monoStyled(n *html.Node) bool {
	if face := strings.ToLower(attr(n, "face")); face != "" && monospaceFont(face) {
		return true
	}
	style := strings.ToLower(attr(n, "style"))
	for _, decl := range strings.Split(style, ";") {
		name, value, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(name) {
		case "font-family", "font":
			if monospaceFont(value) {
				return true
			}
		}
	}
	return false
}

// monospaceFont reports whether a font-family value names a monospace face.
func monospaceFont(value string) bool {
	value = strings.ToLower(value)
	for _, name := range monospaceFonts {
		if strings.Contains(value, name) {
			return true
		}
	}
	return false
}

// blockElement reports whether a tag starts a new block of text.
func blockElement(name string) bool {
	switch name {
	case "p", "div", "section", "article", "header", "footer", "main", "aside",
		"li", "dd", "dt", "figcaption", "tr", "td", "th", "center", "address", "form":
		return true
	}
	return false
}

func (b *blockReader) walk(n *html.Node) {
	if b.stopped {
		return
	}
	if b.stop != nil && n == b.stop {
		b.flush()
		b.stopped = true
		return
	}
	if n.Type == html.ElementNode {
		if isNoteContainer(n) {
			// A note's own place in the text is after the chapter; its
			// marker is where the reference is.
			return
		}
		if n.Data == "a" && isBacklink(n) {
			return
		}
		switch n.Data {
		case "head", "script", "style", "nav", "audio", "video", "iframe", "noscript", "template", "object", "canvas":
			return
		case "title", "meta", "link":
			return
		case "strong", "b":
			b.walkStyled(n, true, b.style.italic, b.style.mono || monoStyled(n))
			return
		case "em", "i":
			b.walkStyled(n, b.style.bold, true, b.style.mono || monoStyled(n))
			return
		}
		if lvl := headingLevel(n.Data); lvl > 0 {
			b.flush()
			b.finishCode()
			runs := collapseRuns(inlineRuns(n, b.style), false)
			if text := RunsText(runs); text != "" {
				b.blocks = append(b.blocks, Block{Kind: Heading, Text: text, Level: lvl, Runs: CleanRuns(runs, text)})
			}
			return
		}
		if n.Data != "pre" && n.Data != "table" && n.Data != "a" && monoStyled(n) {
			if blockElement(n.Data) {
				b.flush()
			}
			b.walkStyled(n, b.style.bold, b.style.italic, true)
			return
		}
		switch n.Data {
		case "table":
			b.table(n)
			return
		case "a":
			if b.noteRef(n) {
				return
			}
		case "p", "div", "section", "article", "header", "footer", "main", "aside",
			"li", "dd", "dt", "figcaption", "tr", "td", "th", "center", "address", "form":
			b.flush()
		case "figure":
			b.figure(n)
			return
		case "svg":
			// A cover or a plate often comes as an SVG that holds one
			// image; its text isn't the book's reading text.
			if img := firstImage(n); img != nil {
				b.addImage(img, "")
			}
			return
		case "blockquote":
			b.flush()
			b.finishCode()
			b.quote++
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				b.walk(c)
			}
			b.flush()
			b.quote--
			return
		case "pre":
			b.code(n)
			return
		case "br":
			b.writeBreak()
			return
		case "hr":
			b.flush()
			b.finishCode()
			b.blocks = append(b.blocks, Block{Kind: Divider})
			return
		case "img":
			b.addImage(n, "")
			return
		}
	}
	if n.Type == html.TextNode {
		b.write(n.Data)
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.walk(c)
	}
}

// inlineRuns reads n's text as runs, with the emphasis of the elements
// around it, for a heading: its own text and no block structure.
func inlineRuns(root *html.Node, style textStyle) []Run {
	var out []Run
	var walk func(*html.Node, textStyle)
	walk = func(n *html.Node, st textStyle) {
		switch n.Type {
		case html.TextNode:
			out = appendRun(out, Run{Text: n.Data, Bold: st.bold, Italic: st.italic})
			return
		case html.ElementNode:
			switch n.Data {
			case "script", "style", "head", "nav", "noscript", "template":
				return
			case "br":
				out = appendRun(out, Run{Text: "\n", Bold: st.bold, Italic: st.italic})
				return
			case "strong", "b":
				st.bold = true
			case "em", "i":
				st.italic = true
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, st)
		}
	}
	walk(root, style)
	return out
}

// appendRun adds a run, joining it to the last when both have one style.
func appendRun(out []Run, r Run) []Run {
	if r.Text == "" {
		return out
	}
	if n := len(out); n > 0 && out[n-1].Bold == r.Bold && out[n-1].Italic == r.Italic {
		out[n-1].Text += r.Text
		return out
	}
	return append(out, r)
}

// code reads a preformatted block as one block. A converter often sets
// each source line in a div instead of writing a line break; the line
// boundaries are put back, and the leading spaces, which are what makes
// source code readable, are kept.
func (b *blockReader) code(n *html.Node) {
	b.flush()
	b.finishCode()
	text := codeText(n)
	if text == "" {
		return
	}
	b.blocks = append(b.blocks, Block{Kind: Code, Text: text})
}

// codeText reads an element's text as code: block children end a line,
// br ends a line, and every other character stays as the book wrote it.
func codeText(root *html.Node) string {
	var b strings.Builder
	line := func() {
		if b.Len() == 0 {
			return
		}
		s := b.String()
		if s[len(s)-1] != '\n' {
			b.WriteByte('\n')
		}
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
			return
		case html.ElementNode:
			switch n.Data {
			case "script", "style", "head", "nav", "noscript", "template":
				return
			case "br":
				b.WriteByte('\n')
				return
			case "div", "p", "li", "tr", "section", "article", "blockquote",
				"pre", "h1", "h2", "h3", "h4", "h5", "h6":
				line()
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c)
				}
				line()
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return cleanCode(b.String())
}

// table reads a table as one block: its caption, rows and cells. Cells
// keep only their text, which is what a reading page can set; a nested
// table's text is flattened into its cell.
func (b *blockReader) table(n *html.Node) {
	b.flush()
	b.finishCode()
	t := &Table{}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "caption" {
			t.Caption = collapse(nodeText(c), false)
			break
		}
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.Data {
			case "table":
				continue
			case "tr":
				if row := tableRow(c); len(row.Cells) > 0 {
					t.Rows = append(t.Rows, row)
				}
			default:
				walk(c)
			}
		}
	}
	walk(n)
	if t.Caption == "" && len(t.Rows) == 0 {
		return
	}
	b.blocks = append(b.blocks, Block{Kind: TableBlock, Table: t})
}

// tableRow reads a row's cells, including those a browser would put in a
// tbody or thead.
func tableRow(n *html.Node) TableRow {
	var row TableRow
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if c.Data == "table" {
				continue
			}
			if c.Data == "td" || c.Data == "th" {
				row.Cells = append(row.Cells, TableCell{
					Text:    cellText(c),
					Header:  c.Data == "th",
					ColSpan: spanAttr(c, "colspan"),
					RowSpan: spanAttr(c, "rowspan"),
				})
				continue
			}
			walk(c)
		}
	}
	walk(n)
	return row
}

// cellText is a cell's text, with a line break where the book has a
// block or a br, and with a picture's alt standing in for it.
func cellText(n *html.Node) string {
	root := n
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "head":
				return
			case "br":
				b.WriteByte('\n')
				return
			case "img", "image":
				if alt := collapse(attr(n, "alt"), false); alt != "" {
					b.WriteString(" ")
					b.WriteString(alt)
					b.WriteString(" ")
				}
				return
			case "p", "div", "li", "dd", "dt", "blockquote", "section", "article", "tr":
				if n != root {
					b.WriteByte('\n')
				}
			}
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return normalizeBreaks(collapse(b.String(), true), 1)
}

// spanAttr reads a cell's colspan or rowspan, within what a page sets.
func spanAttr(n *html.Node, name string) int {
	v, err := strconv.Atoi(strings.TrimSpace(attr(n, name)))
	if err != nil || v < 1 {
		return 1
	}
	if v > 32 {
		return 32
	}
	return v
}

// figure takes a figure's picture and caption, rather than reading the
// caption as body text and losing where it belongs.
func (b *blockReader) figure(n *html.Node) {
	var img *html.Node
	caption := ""
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "img", "image":
				if img == nil {
					img = n
				}
				return
			case "figcaption":
				if caption == "" {
					caption = collapse(nodeText(n), false)
				}
				return
			case "script", "style":
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	if img != nil {
		b.addImage(img, caption)
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.walk(c)
	}
}

// firstImage finds the picture an inline SVG holds. Cover pages often wrap
// one in an svg element.
func firstImage(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.Data == "image" {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if img := firstImage(c); img != nil {
			return img
		}
	}
	return nil
}

// addImage puts a picture in the text, if the book has its bytes. The
// picture's alt says what it is; the figure's caption is the book's line
// under it.
func (b *blockReader) addImage(n *html.Node, caption string) {
	src := attr(n, "src")
	if src == "" {
		src = attr(n, "xlink:href")
	}
	if src == "" {
		src = attr(n, "href")
	}
	if src == "" {
		return
	}
	key, ok := b.loadImage(src)
	if !ok {
		return
	}
	b.flush()
	b.finishCode()
	alt := collapse(attr(n, "alt"), false)
	if alt == "" {
		alt = collapse(attr(n, "title"), false)
	}
	b.blocks = append(b.blocks, Block{Kind: Image, Src: key, Alt: alt, Caption: collapse(caption, false)})
}

// loadImage reads the picture src names, relative to the content document,
// once: the page keeps its bytes by key. A data: URL is read too, as some
// converted books put small pictures inline.
func (b *blockReader) loadImage(src string) (string, bool) {
	src = strings.TrimSpace(src)
	if data, ok := dataImage(src); ok {
		if b.imageTotal+len(data) > MaxImageTotal || len(b.images) >= MaxImages {
			return "", false
		}
		key := fmt.Sprintf("data:%d", len(b.images))
		b.images[key] = data
		b.imageTotal += len(data)
		return key, true
	}
	if u, err := url.Parse(src); err == nil && (u.Scheme != "" || u.Host != "") {
		// A remote picture isn't the book's file, and the page doesn't
		// fetch the web to read a book.
		return "", false
	}
	if b.source == nil {
		return "", false
	}
	name := resolve(b.base, src)
	if name == "" {
		return "", false
	}
	if _, ok := b.images[name]; ok {
		return name, true
	}
	if len(b.images) >= MaxImages || b.imageTotal >= MaxImageTotal {
		return "", false
	}
	data, err := b.source.read(name, MaxImageBytes)
	if err != nil || len(data) == 0 || b.imageTotal+len(data) > MaxImageTotal {
		return "", false
	}
	b.images[name] = data
	b.imageTotal += len(data)
	return name, true
}

// dataImage decodes a base64 data: URL holding a picture.
func dataImage(src string) ([]byte, bool) {
	header, payload, ok := strings.Cut(src, ",")
	if !ok || !strings.HasPrefix(header, "data:image/") || !strings.Contains(header, ";base64") {
		return nil, false
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(data) == 0 || len(data) > MaxImageBytes {
		return nil, false
	}
	return data, true
}

func headingLevel(tag string) int {
	if len(tag) == 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '6' {
		return int(tag[1] - '0')
	}
	return 0
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) || strings.EqualFold(a.Namespace+":"+a.Key, name) {
			return a.Val
		}
	}
	return ""
}

// nodeText is the text of n's subtree, as it reads.
func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
		case html.ElementNode:
			if n.Data == "br" {
				b.WriteByte('\n')
				return
			}
			if n.Data == "script" || n.Data == "style" {
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// textMarks are the spaces and marks converters leave: a no-break space
// reads as a space, and the zero-width marks go.
var textMarks = strings.NewReplacer("\u00a0", " ", "\u200b", "", "\ufeff", "", "\u2028", "\n", "\u2029", "\n")

// collapseRuns collapses whitespace and drops the marks in each run, as
// collapse does for plain text, keeping each run's emphasis.
func collapseRuns(runs []Run, lines bool) []Run {
	var out []Run
	space := false
	for _, r := range runs {
		var b strings.Builder
		for _, ch := range textMarks.Replace(r.Text) {
			if ch == '\n' && lines {
				b.WriteByte('\n')
				space = false
				continue
			}
			if unicode.IsSpace(ch) {
				space = true
				continue
			}
			if space {
				if b.Len() > 0 {
					b.WriteByte(' ')
				} else if len(out) > 0 {
					// The space is between runs, so it keeps the style of
					// the run it follows: emphasis should cover words, not
					// the gap after them.
					out[len(out)-1].Text += " "
				}
			}
			space = false
			b.WriteRune(ch)
		}
		out = appendRun(out, Run{Text: b.String(), Bold: r.Bold, Italic: r.Italic})
	}
	return out
}

// collapse makes text read as one paragraph: whitespace runs become one
// space, and the zero-width marks some converters leave go. With lines,
// line breaks stay.
func collapse(s string, lines bool) string {
	s = textMarks.Replace(s)
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if r == '\n' && lines {
			b.WriteByte('\n')
			space = false
			continue
		}
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}
