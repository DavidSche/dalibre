// Package reader turns an e-book file into chapters of blocks, text,
// pictures, tables and notes, which a reader plugin lays out for the
// reader page. It reads the formats Dalibre can open by itself: EPUB
// (and KEPUB) and plain text. A reader plugin can't add a format: it
// decides how what it gets is set.
//
// The text is the book's, not the library's: the host reads only the file
// of a book the user opened, and hands it to the plugin the user chose to
// read with. Pictures stay in the host, which draws them.
package reader

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// Kinds of block a chapter holds.
const (
	Heading    = "heading"   // a title inside the text, Level 1–6
	Paragraph  = "paragraph" // body text
	Quote      = "quote"     // a quotation, set apart
	Divider    = "divider"   // a rule between passages
	Image      = "image"     // a picture from the book, Src names it
	TableBlock = "table"     // a table, rows of cells of text
	Code       = "code"      // preformatted text, as a book sets source code
)

// Limits on what is read of a book. A book over them is cut at a chapter
// boundary and Truncated is set: the page says so, rather than reading
// memory and frame time grow without end.
const (
	// MaxText is how many characters of text a book may give the reader.
	MaxText = 8 << 20
	// MaxChapterBlocks and MaxChapterRunes split a chapter longer than
	// this into parts, so the page never lays out thousands of paragraphs
	// in a frame.
	MaxChapterBlocks = 240
	MaxChapterRunes  = 24_000
	// MaxChapters bounds a book's chapters, table of contents included.
	MaxChapters = 2000
	// MaxImageBytes, MaxImages and MaxImageTotal bound the pictures a
	// book may put on the page: a page keeps each one's bytes while it
	// reads, so a book of huge plates can't use all the memory. The count
	// is generous because a converted book sets every formula's letter as
	// its own tiny picture; MaxImageTotal is what usually stops a book of
	// plates.
	MaxImageBytes = 8 << 20
	MaxImages     = 5000
	MaxImageTotal = 64 << 20
	// MaxTableRows, MaxTableCells and MaxTableCellRunes keep one table
	// readable: a book's table is a page element, not a spreadsheet.
	MaxTableRows      = 200
	MaxTableCells     = 2000
	MaxTableCellRunes = 2000
	// MaxNotes and MaxNoteRunes bound a chapter's footnotes, which the page
	// sets after the text.
	MaxNotes     = 500
	MaxNoteRunes = 8000
)

// Block is a piece of a chapter.
type Block struct {
	Kind  string `json:"kind"`
	Text  string `json:"text,omitempty"`
	Level int    `json:"level,omitempty"` // Heading's, 1–6
	// Runs is Text as the book sets it, when some of it is bold or italic.
	// It is nil when the text has one style; when it is set, the runs'
	// texts join to Text exactly.
	Runs []Run `json:"runs,omitempty"`
	// Src names an Image's bytes in Document.Images. Alt is what the
	// picture says for a reader who can't see it, and Caption is the
	// book's line under it, when the book has one.
	Src     string `json:"src,omitempty"`
	Alt     string `json:"alt,omitempty"`
	Caption string `json:"caption,omitempty"`
	// Table is a Table block's rows. Its cells hold text only, so a table
	// stays lightweight in the plugin sandbox and on the page.
	Table *Table `json:"table,omitempty"`
}

// Run is a stretch of a block's text in one style. Bold and Italic are
// the book's emphasis, not its font: the page sets them in its own face.
type Run struct {
	Text   string `json:"text"`
	Bold   bool   `json:"bold,omitempty"`
	Italic bool   `json:"italic,omitempty"`
}

// Table is a table in the text: a caption, if the book gives one, and
// rows of cells. A cell may span columns or rows; the page keeps the
// column proportions and leaves the merged space to the cell that starts
// there.
type Table struct {
	Caption string     `json:"caption,omitempty"`
	Rows    []TableRow `json:"rows"`
}

// TableRow is one row of a table.
type TableRow struct {
	Cells []TableCell `json:"cells"`
}

// TableCell is one cell: its text, whether the book set it as a heading
// cell, and how many columns and rows it spans.
type TableCell struct {
	Text    string `json:"text,omitempty"`
	Header  bool   `json:"header,omitempty"`
	ColSpan int    `json:"colspan,omitempty"`
	RowSpan int    `json:"rowspan,omitempty"`
}

// Chapter is a chapter, or a part of one long chapter. Continued marks a
// part that carries on the previous chapter: the page sets no heading for
// it, and the table of contents lists the chapter once.
type Chapter struct {
	Title string `json:"title,omitempty"`
	// Depth is the chapter's place in the book: 1 for a part or a top
	// chapter, 2 for one under it, and so on. It comes from the book's
	// contents, which is flat in some books.
	Depth     int     `json:"depth,omitempty"`
	Blocks    []Block `json:"blocks"`
	Continued bool    `json:"continued,omitempty"`
	// Notes are the chapter's footnotes or endnotes, in the order their
	// references appear in the text. A reference is marked [label] in the
	// text; the page sets the notes themselves after the chapter.
	Notes []Note `json:"notes,omitempty"`
}

// Note is one footnote or endnote. Label is the marker as the book shows
// it, usually a number; Text may hold more than one paragraph.
type Note struct {
	ID    string `json:"id,omitempty"`
	Label string `json:"label,omitempty"`
	Text  string `json:"text"`
}

// Document is a book's text as extracted.
type Document struct {
	Title     string    `json:"title,omitempty"`
	Language  string    `json:"language,omitempty"`
	Chapters  []Chapter `json:"chapters"`
	Truncated bool      `json:"truncated,omitempty"`
	// Images are the pictures the blocks name by Src. They aren't sent
	// to a reader plugin: a plugin decides where a picture goes, not
	// what its bytes are, and the page reads them from the host.
	Images map[string][]byte `json:"-"`
}

// UnsupportedError says Dalibre can't read the format by itself.
type UnsupportedError struct{ Format string }

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("reading %s books needs a reader plugin", e.Format)
}

// Formats are the formats Extract reads.
var Formats = []string{"EPUB", "KEPUB", "TXT"}

// Supports reports whether Extract reads format.
func Supports(format string) bool {
	for _, f := range Formats {
		if strings.EqualFold(f, format) {
			return true
		}
	}
	return false
}

// Extract reads the text, pictures, tables and notes of file, whose format is format
// ("EPUB", "KEPUB" or "TXT"), as chapters.
func Extract(file, format string) (*Document, error) {
	switch strings.ToUpper(format) {
	case "EPUB", "KEPUB":
		return readEPUB(file)
	case "TXT":
		return readTXT(file)
	case "":
		switch strings.ToLower(filepath.Ext(file)) {
		case ".epub", ".kepub":
			return readEPUB(file)
		case ".txt":
			return readTXT(file)
		}
	}
	if format == "" {
		format = strings.TrimPrefix(filepath.Ext(file), ".")
	}
	return nil, &UnsupportedError{Format: strings.ToUpper(format)}
}

// splitChapter cuts a long chapter into parts, so a page shows a few
// hundred paragraphs at most. Parts after the first are Continued.
func splitChapter(ch Chapter) []Chapter {
	if len(ch.Blocks) <= MaxChapterBlocks {
		n := 0
		for _, b := range ch.Blocks {
			n += blockRunes(b)
		}
		if n <= MaxChapterRunes {
			return []Chapter{ch}
		}
	}
	var out []Chapter
	for len(ch.Blocks) > 0 {
		n, runes := 0, 0
		for n < len(ch.Blocks) && n < MaxChapterBlocks {
			runes += blockRunes(ch.Blocks[n])
			n++
			if runes >= MaxChapterRunes {
				break
			}
		}
		part := Chapter{Title: ch.Title, Depth: ch.Depth, Blocks: ch.Blocks[:n], Continued: ch.Continued || len(out) > 0}
		ch.Blocks = ch.Blocks[n:]
		if len(ch.Blocks) == 0 {
			// The notes belong after the chapter's last part, not before
			// the rest of its text.
			part.Notes = ch.Notes
		}
		out = append(out, part)
	}
	return out
}

// addChapter appends ch, split, while the document has room; it reports
// whether every chapter fit.
func (d *Document) addChapter(ch Chapter) bool {
	ch.Blocks = cleanBlocks(ch.Blocks)
	ch.Notes = cleanNotes(ch.Notes)
	if len(ch.Blocks) == 0 {
		return true
	}
	for _, part := range splitChapter(ch) {
		if len(d.Chapters) >= MaxChapters {
			return false
		}
		d.Chapters = append(d.Chapters, part)
	}
	return true
}

// cleanBlocks trims the blocks and drops the empty ones.
func cleanBlocks(in []Block) []Block {
	out := in[:0]
	for _, b := range in {
		switch b.Kind {
		case Image:
			b.Text, b.Level, b.Runs = "", 0, nil
			b.Src = strings.TrimSpace(b.Src)
			b.Alt = strings.TrimSpace(b.Alt)
			b.Caption = strings.TrimSpace(b.Caption)
			if b.Src == "" {
				continue
			}
		case Divider:
			b.Text, b.Level, b.Runs = "", 0, nil
		case TableBlock:
			b.Text, b.Level, b.Src, b.Alt, b.Caption, b.Runs = "", 0, "", "", "", nil
			b.Table = cleanTable(b.Table)
			if b.Table == nil {
				continue
			}
		case Code:
			b.Text, b.Level, b.Src, b.Alt, b.Caption, b.Runs = cleanCode(b.Text), 0, "", "", "", nil
			if b.Text == "" {
				continue
			}
		default:
			b.Text = strings.TrimSpace(b.Text)
			if b.Text == "" {
				continue
			}
			b.Runs = CleanRuns(b.Runs, b.Text)
		}
		out = append(out, b)
	}
	return out
}

// RunsText is the text the runs spell, which is a block's Text when the
// runs are its own.
func RunsText(runs []Run) string {
	var b strings.Builder
	for _, r := range runs {
		b.WriteString(r.Text)
	}
	return b.String()
}

// CleanRuns makes runs fit text: it drops the empty ones, joins those
// with one style, and returns nil when the text has one style or the runs
// don't spell it. A plugin may change a block's text; the text is what
// the page shows, so runs that no longer match it are let go.
func CleanRuns(runs []Run, text string) []Run {
	if len(runs) == 0 || text == "" {
		return nil
	}
	out := make([]Run, 0, len(runs))
	for _, r := range runs {
		if r.Text == "" {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Bold == r.Bold && out[n-1].Italic == r.Italic {
			out[n-1].Text += r.Text
			continue
		}
		out = append(out, r)
	}
	if len(out) == 0 || RunsText(out) != text {
		return nil
	}
	styled := false
	for _, r := range out {
		if r.Bold || r.Italic {
			styled = true
			break
		}
	}
	if !styled {
		return nil
	}
	return out
}

// cleanCode trims a code block's blank ends and the spaces that trail
// its lines, but keeps the leading spaces that indent the code.
func cleanCode(s string) string {
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

// cleanNotes trims a chapter's notes, drops the empty ones, and gives a
// note without a label its place in the chapter.
func cleanNotes(in []Note) []Note {
	out := in[:0]
	for _, n := range in {
		n.ID = truncateText(strings.TrimSpace(n.ID), 200)
		n.Label = truncateText(strings.TrimSpace(n.Label), 16)
		n.Text = truncateText(strings.TrimSpace(n.Text), MaxNoteRunes)
		if n.Text == "" {
			continue
		}
		if n.Label == "" {
			n.Label = strconv.Itoa(len(out) + 1)
		}
		out = append(out, n)
		if len(out) >= MaxNotes {
			break
		}
	}
	return out
}

// cleanTable trims a table and bounds its size. A table with no text and
// no caption is nothing to show.
func cleanTable(t *Table) *Table {
	if t == nil {
		return nil
	}
	t.Caption = truncateText(strings.TrimSpace(t.Caption), 600)
	rows := make([]TableRow, 0, len(t.Rows))
	cells := 0
	hasText := false
	for _, row := range t.Rows {
		if len(rows) >= MaxTableRows {
			break
		}
		out := make([]TableCell, 0, len(row.Cells))
		for _, c := range row.Cells {
			if cells >= MaxTableCells {
				break
			}
			c.Text = truncateText(strings.TrimSpace(c.Text), MaxTableCellRunes)
			c.ColSpan = clampSpan(c.ColSpan)
			c.RowSpan = clampSpan(c.RowSpan)
			if c.Text != "" {
				hasText = true
			}
			out = append(out, c)
			cells++
		}
		if len(out) > 0 {
			rows = append(rows, TableRow{Cells: out})
		}
	}
	t.Rows = rows
	if len(rows) == 0 && t.Caption == "" {
		return nil
	}
	if !hasText && t.Caption == "" {
		return nil
	}
	return t
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

// blockRunes is how much of a book's text budget a block uses.
func blockRunes(b Block) int {
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

func truncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max])) + "…"
}

// errTooBig is a file larger than the reader reads.
var errTooBig = errors.New("the book is larger than Dalibre reads")
