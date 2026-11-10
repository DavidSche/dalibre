package reader

// The HTML side of a MOBI: its few non-HTML references, its contents
// page, and the chapter boundaries that page or its headings give.

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// normalizeMOBIHTML turns the few MOBI-only references into the markup
// htmlDoc reads: a picture's recindex becomes an src naming its record.
func normalizeMOBIHTML(root *html.Node) {
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "img" || n.Data == "image") {
			src := attr(n, "src")
			if src == "" {
				src = attr(n, "xlink:href")
			}
			if src == "" {
				if index := mobiImageIndex(attr(n, "recindex")); index > 0 {
					src = mobiImageName(index)
				}
			} else if index := mobiImageIndex(src); index > 0 && strings.Contains(strings.ToLower(src), "embed:") {
				src = mobiImageName(index)
			}
			if src != "" {
				setAttr(n, "src", src)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
}

// setAttr sets an element's attribute, replacing an existing one.
func setAttr(n *html.Node, name, value string) {
	for i := range n.Attr {
		if strings.EqualFold(n.Attr[i].Key, name) {
			n.Attr[i].Val = value
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: name, Val: value})
}

// mobiTOCBounds finds the guide's table of contents and the page it
// occupies. The page ends at the next page break; a TOC at the end of
// the book has no break after it.
func mobiTOCBounds(text []byte) (start, end int, ok bool) {
	head := text
	if len(head) > 1<<20 {
		head = head[:1<<20]
	}
	root, err := html.Parse(bytes.NewReader(head))
	if err != nil {
		return 0, 0, false
	}
	var at int
	found := false
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found {
			return
		}
		if n.Type == html.ElementNode && n.Data == "reference" &&
			strings.EqualFold(attr(n, "type"), "toc") {
			if v, err := strconv.Atoi(strings.TrimSpace(attr(n, "filepos"))); err == nil && v > 0 && v < len(text) {
				at, found = v, true
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if !found {
		return mobiInlineTOC(text)
	}
	end = len(text)
	if i := bytes.Index(text[at:], []byte("<mbp:pagebreak")); i >= 0 {
		end = at + i
	}
	if end <= at {
		end = len(text)
	}
	return at, end, true
}

// mobiAnchor is a link with a filepos target in the text.
type mobiAnchor struct {
	start, end, target int
}

// mobiInlineTOC finds a contents page that the book didn't put in its
// guide. Converted books often write the links as plain paragraphs, so
// the links themselves are the signal: a dense run of them, all pointing
// forward across the book, is a contents page, not footnotes.
func mobiInlineTOC(text []byte) (start, end int, ok bool) {
	const maxLinks = 10_000
	anchors := make([]mobiAnchor, 0, 64)
	for pos := 0; pos < len(text); {
		i := bytes.Index(text[pos:], []byte("<a"))
		if i < 0 {
			break
		}
		at := pos + i
		if at+2 < len(text) {
			c := text[at+2]
			if c != ' ' && c != 0x09 && c != 0x0d && c != 0x0a && c != '>' {
				pos = at + 2
				continue
			}
		}
		openEnd := at + bytes.IndexByte(text[at:], '>') + 1
		if openEnd <= at || openEnd > len(text) {
			break
		}
		open := text[at:openEnd]
		key := bytes.Index(open, []byte("filepos="))
		if key < 0 {
			pos = openEnd
			continue
		}
		value := bytes.TrimSpace(open[key+len("filepos="):])
		if len(value) > 0 && (value[0] == '"' || value[0] == 0x27) {
			value = value[1:]
		}
		n := 0
		for n < len(value) && value[n] >= '0' && value[n] <= '9' {
			n++
		}
		if n == 0 {
			pos = openEnd
			continue
		}
		target, err := strconv.Atoi(string(value[:n]))
		if err != nil {
			pos = openEnd
			continue
		}
		close := bytes.Index(text[openEnd:], []byte("</a>"))
		if close < 0 {
			break
		}
		anchors = append(anchors, mobiAnchor{start: at, end: openEnd + close + 4, target: target})
		if len(anchors) >= maxLinks {
			break
		}
		pos = openEnd
	}
	for i := 0; i < len(anchors); {
		j := i + 1
		for j < len(anchors) && anchors[j].start-anchors[j-1].start <= 4096 {
			j++
		}
		if mobiContentsRun(anchors[i:j], text, &start, &end) {
			return start, end, true
		}
		i = j
	}
	return 0, 0, false
}

// mobiContentsRun reports whether a run of links is a contents page: its
// links point ahead of it, and their targets span much of the book. A
// short run of footnotes fails both tests.
func mobiContentsRun(group []mobiAnchor, text []byte, start, end *int) bool {
	if len(group) < 4 {
		return false
	}
	last := group[len(group)-1]
	first := group[0]
	if first.target <= 0 || first.target <= last.end {
		return false
	}
	lo, hi := first.target, first.target
	for _, a := range group {
		if a.target <= last.end {
			return false
		}
		if a.target < lo {
			lo = a.target
		}
		if a.target > hi {
			hi = a.target
		}
	}
	need := len(text) / 8
	if need < 4096 {
		need = 4096
	}
	if hi-lo < need {
		return false
	}
	*start, *end = first.start, last.end
	return true
}

// mobiTOCEntry is one link in the guide's contents: the title a reader
// sees, where its section starts, and how deep it sits.
type mobiTOCEntry struct {
	Title  string
	Offset int
	Depth  int
}

// mobiTOCEntries reads the links in the contents page. The page uses
// filepos offsets, and indents subsections in a blockquote.
func mobiTOCEntries(text []byte, start, end int) []mobiTOCEntry {
	if start < 0 || end <= start || end > len(text) {
		return nil
	}
	root, err := html.Parse(bytes.NewReader(text[start:end]))
	if err != nil {
		return nil
	}
	var out []mobiTOCEntry
	seen := map[int]bool{}
	var walk func(*html.Node, int)
	walk = func(n *html.Node, depth int) {
		if n.Type == html.ElementNode && n.Data == "a" {
			if raw := strings.TrimSpace(attr(n, "filepos")); raw != "" {
				at, err := strconv.Atoi(raw)
				title := collapse(nodeText(n), false)
				if err == nil && at > 0 && at < len(text) && title != "" &&
					(at < start || at >= end) && !seen[at] {
					seen[at] = true
					out = append(out, mobiTOCEntry{
						Title: truncateRunes(title, 300), Offset: at, Depth: clampDepth(depth),
					})
				}
				return
			}
		}
		childDepth := depth
		if n.Type == html.ElementNode && n.Data == "blockquote" {
			childDepth++
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, childDepth)
		}
	}
	walk(root, 1)
	for i := 1; i < len(out); i++ {
		if out[i].Offset <= out[i-1].Offset {
			// A broken contents page isn't a reading order; headings are
			// a safer guide than slices that overlap.
			return nil
		}
	}
	return out
}

// clampDepth keeps a contents entry inside the page's six levels.
func clampDepth(n int) int {
	if n < 1 {
		return 1
	}
	if n > 6 {
		return 6
	}
	return n
}

// truncateRunes cuts s to n runes, for a title a page can show.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

// content returns the part of the book between two offsets, leaving out
// the contents page itself: a chapter that starts before the contents
// would otherwise carry the table of contents in its text.
func (b *mobiBook) content(start, end int) []byte {
	if start < 0 {
		start = 0
	}
	if end > len(b.text) {
		end = len(b.text)
	}
	if start >= end {
		return nil
	}
	if !b.hasTOC || b.tocStart < start || b.tocStart >= end {
		return b.text[start:end]
	}
	cutEnd := min(end, b.tocEnd)
	out := make([]byte, 0, (b.tocStart-start)+max(end-cutEnd, 0))
	out = append(out, b.text[start:b.tocStart]...)
	out = append(out, b.text[cutEnd:end]...)
	return out
}

// document turns the text into chapters. The contents page is the best
// guide; a book without one is read by its headings. A book with neither
// is one chapter, which the page splits when it is long.
func (b *mobiBook) document() (*Document, error) {
	doc := &Document{Title: b.title, Language: b.language, Images: map[string][]byte{}}
	if b.hasTOC {
		// One link is not a reading order; headings say more than a
		// single slice would.
		if entries := mobiTOCEntries(b.text, b.tocStart, b.tocEnd); len(entries) > 1 {
			b.tocChapters(doc, entries)
		}
	}
	if len(doc.Chapters) == 0 {
		b.headingChapters(doc)
	}
	if len(doc.Chapters) == 0 {
		b.wholeChapter(doc)
	}
	if len(doc.Chapters) == 0 {
		return nil, fmt.Errorf("mobi: the book has no text to read")
	}
	normalizeMobiDepths(doc)
	doc.Truncated = doc.Truncated || b.textTruncated
	return doc, nil
}

// normalizeMobiDepths keeps a contents tree from skipping a level. A
// book may set a part as h1 and its chapters as h3; the chapters still
// sit one level under the part, not two.
func normalizeMobiDepths(doc *Document) {
	prev := 1
	for i := range doc.Chapters {
		d := clampDepth(doc.Chapters[i].Depth)
		if d > prev+1 {
			d = prev + 1
		}
		doc.Chapters[i].Depth = d
		prev = d
	}
}

// tocChapters reads each section the contents page names. Several
// sections can share a text record; each is parsed from its own slice so
// a chapter never depends on where a record happened to end.
func (b *mobiBook) tocChapters(doc *Document, entries []mobiTOCEntry) {
	notes := &noteIndex{docs: map[string]*noteDoc{}, notes: map[string]Note{}}
	if entries[0].Offset > 0 {
		b.addMobiChapter(doc, notes, b.content(0, entries[0].Offset), "mobi-front", b.title, 1)
	}
	for i, e := range entries {
		end := len(b.text)
		if i+1 < len(entries) {
			end = entries[i+1].Offset
		}
		fragment := b.content(e.Offset, end)
		if len(fragment) == 0 {
			continue
		}
		h := parseHTML(fragment)
		if h == nil {
			continue
		}
		normalizeMOBIHTML(h.root)
		base := fmt.Sprintf("mobi-%d.xhtml", i)
		blocks, chNotes := h.blocks(base, b, doc.Images, notes, "", "")
		if len(blocks) == 0 {
			continue
		}
		depth := mobiBlocksDepth(blocks, e.Depth)
		if !doc.addChapter(Chapter{Title: e.Title, Depth: depth, Blocks: blocks, Notes: chNotes}) {
			doc.Truncated = true
			return
		}
	}
}

// addMobiChapter parses one slice and appends it, taking its title from
// its first heading when the slice has one.
func (b *mobiBook) addMobiChapter(doc *Document, notes *noteIndex, fragment []byte, base, title string, depth int) {
	if len(fragment) == 0 {
		return
	}
	h := parseHTML(fragment)
	if h == nil {
		return
	}
	normalizeMOBIHTML(h.root)
	blocks, chNotes := h.blocks(base, b, doc.Images, notes, "", "")
	if len(blocks) == 0 {
		return
	}
	if heading := mobiFirstHeading(blocks); heading != "" {
		title = heading
	}
	doc.addChapter(Chapter{Title: title, Depth: mobiBlocksDepth(blocks, depth), Blocks: blocks, Notes: chNotes})
}

// mobiFirstHeading names a slice by its first heading, if it has one.
func mobiFirstHeading(blocks []Block) string {
	for i, b := range blocks {
		if b.Kind == Heading && b.Text != "" {
			return truncateRunes(b.Text, 300)
		}
		if i == 2 {
			break
		}
	}
	return ""
}

// mobiBlocksDepth takes a section's depth from its first heading. A MOBI
// contents page often lists every entry flat; the headings still say
// which sections sit under which.
func mobiBlocksDepth(blocks []Block, fallback int) int {
	for i, b := range blocks {
		if b.Kind == Heading && b.Level > 0 {
			return clampDepth(b.Level)
		}
		if i == 2 {
			break
		}
	}
	return clampDepth(fallback)
}

// headingChapters follows the book's headings, for a MOBI that has no
// contents page. Each heading starts a chapter at its own level.
func (b *mobiBook) headingChapters(doc *Document) {
	h := parseHTML(b.text)
	if h == nil {
		return
	}
	normalizeMOBIHTML(h.root)
	var heads []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && headingLevel(n.Data) > 0 && collapse(nodeText(n), false) != "" {
			heads = append(heads, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(h.root)
	if len(heads) == 0 {
		return
	}
	for i, n := range heads {
		id := strings.TrimSpace(attr(n, "id"))
		if id == "" {
			id = fmt.Sprintf("mobi-heading-%d", i)
			setAttr(n, "id", id)
		}
		h.ids[id] = n
	}
	notes := &noteIndex{docs: map[string]*noteDoc{}, notes: map[string]Note{}}
	first := attr(heads[0], "id")
	pre, preNotes := h.blocks(mobiBase, b, doc.Images, notes, "", first)
	for i, n := range heads {
		from := attr(n, "id")
		to := ""
		if i+1 < len(heads) {
			to = attr(heads[i+1], "id")
		}
		blocks, chNotes := h.blocks(mobiBase, b, doc.Images, notes, from, to)
		if i == 0 && len(pre) > 0 {
			blocks = append(pre, blocks...)
			chNotes = append(preNotes, chNotes...)
		}
		title := truncateRunes(collapse(nodeText(n), false), 300)
		if !doc.addChapter(Chapter{Title: title, Depth: clampDepth(headingLevel(n.Data)), Blocks: blocks, Notes: chNotes}) {
			doc.Truncated = true
			return
		}
	}
}

// wholeChapter reads a book that gives neither contents nor headings as
// one chapter; addChapter splits it at the page's limits.
func (b *mobiBook) wholeChapter(doc *Document) {
	h := parseHTML(b.text)
	if h == nil {
		return
	}
	normalizeMOBIHTML(h.root)
	notes := &noteIndex{docs: map[string]*noteDoc{}, notes: map[string]Note{}}
	if blocks, chNotes := h.blocks(mobiBase, b, doc.Images, notes, "", ""); len(blocks) > 0 {
		doc.addChapter(Chapter{Title: b.title, Blocks: blocks, Notes: chNotes})
	}
}
