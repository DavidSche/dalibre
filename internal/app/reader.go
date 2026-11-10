package app

import (
	"bytes"
	"context"
	"image"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"dalibre/cmd/dalibre/locales"
	"dalibre/internal/app/theme"
	"dalibre/internal/calibre"
	"dalibre/internal/pdf"
	"dalibre/internal/plugin"
	"dalibre/internal/reader"
	"dalibre/internal/settings"
)

// The reader page: a book's text, full window, as WeChat Reading sets it —
// a warm page, a wide measure, chapter by chapter, with the contents, the
// page color and text size a tap away. The text comes from a reader
// plugin; the page is Dalibre's.

// readerState is the open book, or the last one while it loads.
type readerState struct {
	open    bool
	loading bool
	err     error
	gen     int // set a load's result aside when another was asked for

	book     *calibre.Book
	format   string
	reading  *plugin.Reading
	chapters []reader.Chapter
	// imageData is the book's picture bytes, by the block's Src, read once
	// with the book. images is what the current chapter shows: pictures of
	// other chapters are decoded away, so a book of thousands of formulas
	// costs what one chapter sets, not the book.
	imageData map[string][]byte
	images    map[string]ui.ImageSource
	imageGen  int // set a chapter's decode aside when another was asked for
	// pdf is the book when it is a PDF. Its pages are drawn when they are
	// shown, not read once with the book, and the document is closed when
	// the reader leaves.
	pdf *pdf.Book

	chapter   int
	scroll    ui.ScrollState
	percent   float64   // the whole book, 0–1
	percents  []float64 // each chapter's share of the book, by characters
	toc       bool
	tocTab    string  // "contents" or "bookmarks"
	tocDrag   float32 // how far the contents' grip was dragged
	typeSheet bool
	pending   float64 // how far into the chapter to scroll once laid out

	theme      string
	size       int
	lineHeight float64
	indent     bool
	bookmarks  []int
	highlights []settings.Highlight

	// blockRefs map the text elements a book's blocks built to their
	// block, so a selection can become a passage of the text rather than
	// of the page. They are rebuilt every frame.
	blockRefs  map[uint64]readerBlockRef
	blockElems map[int]ui.Element
	sel        readerSelection
	selHidden  bool
	selDrop    bool
	menuX      float32
	menuY      float32
	menuOn     bool

	tocList   ui.ListState
	marksList ui.ListState
}

// readerBlockRef is where a built text belongs: the block of the chapter
// on the page, and how many runes the page put before the block's text,
// as the two full-width spaces of an indented paragraph.
type readerBlockRef struct {
	index  int
	prefix int
}

// readerSelection is what the reader selected: the text Copy puts on the
// clipboard and the passages a mark covers, with where the menu opens.
type readerSelection struct {
	sig   string
	text  string
	parts []readerSelectionPart
	x, y  float32
	since time.Time
	ready bool
}

// readerSelectionPart is one block's part of the selection, in the block
// text's own offsets so a mark survives the page's indentation.
type readerSelectionPart struct {
	block      int
	start, end int
	text       string
}

// readerStyles are the chosen typography, with the defaults filled in.
func (a *App) readerStyles() (name string, size int, lineHeight float64) {
	r := a.settings.Reading
	name = r.Theme
	if !theme.IsReadingTheme(name) {
		name = settings.DefaultReadingTheme
	}
	size = r.Size
	if size < 14 || size > 30 {
		size = settings.DefaultReadingSize
	}
	lineHeight = r.LineHeight
	if lineHeight < 1.4 || lineHeight > 2.6 {
		lineHeight = settings.DefaultReadingLineHeight
	}
	return name, size, lineHeight
}

// readerContentsSide is the edge the contents rest on, "left" or "right".
// The right is the default, as the page first set it.
func (a *App) readerContentsSide() string {
	if a.settings.Reading.TOCSide == "left" {
		return "left"
	}
	return "right"
}

// readerSetContentsSide docks the contents to side, keeping it.
func (a *App) readerSetContentsSide(side string) {
	if side != "left" && side != "right" {
		return
	}
	if a.settings.Reading.TOCSide == side {
		return
	}
	a.settings.Reading.TOCSide = side
	_ = a.settings.Save()
}

// readerContentsPinned keeps the contents open when a chapter is chosen,
// as a table of contents left beside the page.
func (a *App) readerContentsPinned() bool { return a.settings.Reading.TOCPinned }

// readerSetContentsPinned pins or unpins the contents, keeping the choice.
func (a *App) readerSetContentsPinned(pinned bool) {
	if a.settings.Reading.TOCPinned == pinned {
		return
	}
	a.settings.Reading.TOCPinned = pinned
	_ = a.settings.Save()
}

// readerKey is the settings key for a book's place: its UUID, which
// survives edits, else its id.
func readerKey(b *calibre.Book) string {
	if b.UUID != "" {
		return b.UUID
	}
	return strconv.FormatInt(b.ID, 10)
}

// bookProgress is how far the user read b, 0–1.
func (a *App) bookProgress(b *calibre.Book) float64 {
	if b == nil {
		return 0
	}
	p := a.settings.Reading.Books[readerKey(b)]
	if p == nil || p.Chapters <= 0 {
		return 0
	}
	v := (float64(p.Chapter) + p.Percent) / float64(p.Chapters)
	return math.Max(0, math.Min(1, v))
}

// pdfFormat is the format Dalibre draws itself, page by page, without a
// reader plugin: a PDF has already been laid out.
const pdfFormat = "PDF"

// readerPlugin returns the plugin that reads format: the first the user
// installed, which may lay a book out better than Dalibre's own, or else the
// built-in reader.
func (a *App) readerPlugin(format string) *plugin.Plugin {
	format = strings.ToUpper(format)
	var fallback *plugin.Plugin
	for _, p := range a.plugins {
		r := p.Manifest.Capabilities.Reader
		if r == nil || !r.ReadsFormat(format) {
			continue
		}
		if p.Builtin {
			if fallback == nil {
				fallback = p
			}
			continue
		}
		return p
	}
	return fallback
}

// readableFormat is the format the reader would open b in: one of the
// formats Dalibre extracts, a PDF, or a format a reader plugin brings.
// Dalibre's own formats come first because it reads them best; a PDF is
// next because its pages keep the book's own layout; a plugin's extra
// format is last.
func (a *App) readableFormat(b *calibre.Book) (string, bool) {
	if b == nil {
		return "", false
	}
	for _, f := range reader.Formats {
		if slices.Contains(b.Formats, f) && a.readerPlugin(f) != nil {
			return f, true
		}
	}
	if slices.Contains(b.Formats, pdfFormat) {
		return pdfFormat, true
	}
	for _, f := range b.Formats {
		if a.readerPlugin(f) != nil {
			return f, true
		}
	}
	return "", false
}

// openBookOrRead reads b in Dalibre when a reader plugin reads it, else
// opens its file in the system's reader.
func (a *App) openBookOrRead(b *calibre.Book) {
	if b == nil {
		return
	}
	if f, ok := a.readableFormat(b); ok {
		a.openReader(b, f)
		return
	}
	a.openBook(b, "")
}

// openReader shows b in the reader page, laid out by a reader plugin, or
// drawn page by page when the book is a PDF.
func (a *App) openReader(b *calibre.Book, format string) {
	if b == nil || a.lib == nil {
		return
	}
	format = strings.ToUpper(format)
	if format == "" {
		if f, ok := a.readableFormat(b); ok {
			format = f
		}
	}
	if format == pdfFormat {
		a.openPDFReader(b)
		return
	}
	p := a.readerPlugin(format)
	if p == nil {
		a.toast("%s", locales.ReaderNoPlugin(format))
		return
	}
	gen, at := a.readerBegin(b, format)
	lib := a.lib
	info := plugin.ReaderBook{
		ID: strconv.FormatInt(b.ID, 10), Title: b.Title, Authors: slices.Clone(b.Authors),
		Language: firstLanguage(b.Languages), Format: format,
	}
	a.run(func() {
		doc, err := openDocument(lib, b, format)
		var reading *plugin.Reading
		if err == nil {
			reading, err = p.OpenReader(context.Background(), info, *doc)
		}
		var raw map[string][]byte
		var images map[string]ui.ImageSource
		if err == nil {
			raw = usedReaderImages(reading, doc.Images)
			images = decodeReaderChapter(reading.Chapters, at, raw)
		}
		a.post(func() {
			if a.rd.gen != gen || !a.rd.open {
				return
			}
			a.rd.loading = false
			if err != nil {
				a.rd.err = err
				return
			}
			a.rd.imageData = raw
			a.rd.images = images
			a.adoptReading(reading)
			// The kept place may have moved when the reading's chapters
			// changed; decode that chapter instead.
			if a.rd.chapter != at {
				a.readerLoadImages()
			}
		})
	})
}

// readerBegin resets the page for b in format, at the place kept for the
// book, and returns the load's generation and that place. It puts a PDF
// from the last book away first, so its document is not left open.
func (a *App) readerBegin(b *calibre.Book, format string) (gen, at int) {
	name, size, lineHeight := a.readerStyles()
	prev := a.settings.Reading.Books[readerKey(b)]
	marks := []int{}
	highlights := []settings.Highlight{}
	chapter, fraction := 0, 0.0
	if prev != nil {
		marks = slices.Clone(prev.Bookmarks)
		highlights = slices.Clone(prev.Highlights)
		chapter, fraction = prev.Chapter, prev.Percent
	}
	a.readerDiscardPDF()
	gen = a.rd.gen + 1
	a.rd = readerState{
		open: true, loading: true, gen: gen, book: b, format: format,
		tocTab: "contents", pending: fraction, chapter: chapter,
		theme: name, size: size, lineHeight: lineHeight, bookmarks: marks,
		highlights: highlights,
	}
	return gen, a.rd.chapter
}

// readerDiscardPDF closes the PDF the reader has open, in the background:
// a close waits for a page already being drawn, and that wait must not hold
// the window.
func (a *App) readerDiscardPDF() {
	if a.rd.pdf == nil {
		return
	}
	book := a.rd.pdf
	a.run(func() { _ = book.Close() })
}

// openPDFReader draws a PDF's pages. A PDF has no text to lay out, so each
// page is one image block; the page is rendered when the reader turns to it,
// not all at once, and only that page's bitmap is held.
func (a *App) openPDFReader(b *calibre.Book) {
	if b == nil || a.lib == nil {
		return
	}
	gen, at := a.readerBegin(b, pdfFormat)
	lib := a.lib
	a.run(func() {
		var book *pdf.Book
		var reading *plugin.Reading
		raw := map[string][]byte{}
		var images map[string]ui.ImageSource
		path, err := bookFile(lib, b, pdfFormat)
		if err == nil {
			book, err = pdf.Open(path)
		}
		if err == nil {
			reading = pdfReading(b, book.Pages())
			if at >= 0 && at < book.Pages() {
				data, renderErr := book.Render(at)
				if renderErr != nil {
					err = renderErr
				} else {
					raw[pdf.PageSrc(at)] = data
					images = decodeReaderChapter(reading.Chapters, at, raw)
				}
			}
		}
		a.post(func() {
			if a.rd.gen != gen || !a.rd.open {
				if book != nil {
					_ = book.Close()
				}
				return
			}
			a.rd.loading = false
			if err != nil {
				a.rd.err = err
				if book != nil {
					_ = book.Close()
				}
				return
			}
			a.rd.pdf = book
			a.rd.imageData = raw
			a.rd.images = images
			a.adoptReading(reading)
			// The kept page may have moved when the book was counted; draw
			// the page the reader actually landed on.
			if a.rd.chapter != at {
				a.readerLoadImages()
			}
		})
	})
}

// pdfReading is a PDF as the reader's chapters: one page each, in the
// order the document has them. The contents is the page list; a PDF's own
// outline can name pages, but the pages are still what the reader turns.
func pdfReading(b *calibre.Book, pages int) *plugin.Reading {
	chapters := make([]reader.Chapter, pages)
	for i := range chapters {
		title := locales.ReaderPageN(i + 1)
		chapters[i] = reader.Chapter{
			Title: title,
			Blocks: []reader.Block{{
				Kind: reader.Image,
				Src:  pdf.PageSrc(i),
				Alt:  title,
			}},
		}
	}
	return &plugin.Reading{
		Title:    b.Title,
		Author:   strings.Join(b.Authors, ", "),
		Chapters: chapters,
	}
}

// A picture's pixels are bounded too: a small file can decode to a plate
// far larger than any screen, and a chapter has room for many.
const (
	maxReaderImagePixels = 24 << 20
	maxReaderImageSide   = 12_000
	// maxReaderChapterPixels bounds the bitmaps one chapter decodes, so a
	// chapter of plates can't fill memory when the reader turns to it.
	maxReaderChapterPixels = 32 << 20

	// maxReaderHighlights bounds a book's marks, so the settings file
	// stays small however long a reader marks passages.
	maxReaderHighlights = 2000
)

// usedReaderImages keeps the bytes of the pictures the reading actually
// places, so a picture the plugin dropped costs nothing.
func usedReaderImages(r *plugin.Reading, raw map[string][]byte) map[string][]byte {
	out := map[string][]byte{}
	if r == nil {
		return out
	}
	for _, ch := range r.Chapters {
		for _, b := range ch.Blocks {
			if b.Kind != reader.Image || b.Src == "" {
				continue
			}
			if data, ok := raw[b.Src]; ok && len(data) > 0 {
				out[b.Src] = data
			}
		}
	}
	return out
}

// decodeReaderChapter decodes the pictures chapter at holds, and only
// those: turning to another chapter decodes its pictures and lets the
// previous ones go. Decoding here, off the main thread, keeps the first
// frame of a chapter from hitching on a plate.
func decodeReaderChapter(chapters []reader.Chapter, at int, raw map[string][]byte) map[string]ui.ImageSource {
	out := map[string]ui.ImageSource{}
	if at < 0 || at >= len(chapters) {
		return out
	}
	seen := map[string]bool{}
	var pixels int64
	for _, b := range chapters[at].Blocks {
		if b.Kind != reader.Image || b.Src == "" || seen[b.Src] {
			continue
		}
		seen[b.Src] = true
		data := raw[b.Src]
		if len(data) == 0 || !readerImageFits(data) {
			continue
		}
		img, err := ui.DecodeImage(data)
		if err != nil {
			continue
		}
		w, h := ui.ImageSize(img)
		area := int64(w) * int64(h)
		if w > maxReaderImageSide || h > maxReaderImageSide || area > maxReaderImagePixels {
			continue
		}
		if pixels+area > maxReaderChapterPixels {
			continue
		}
		pixels += area
		out[b.Src] = img
	}
	return out
}

// readerLoadImages draws or decodes the current chapter's pictures in the
// background and lets the previous chapter's go when they are ready. A
// quicker turn makes an earlier decode's result stale, so it is dropped.
func (a *App) readerLoadImages() {
	r := &a.rd
	if !r.open || len(r.chapters) == 0 {
		return
	}
	r.imageGen++
	gen, imgGen, at := r.gen, r.imageGen, r.chapter
	chapters, raw, book := r.chapters, r.imageData, r.pdf
	if book == nil && len(raw) == 0 {
		r.images = nil
		return
	}
	a.run(func() {
		var renderErr error
		if book != nil {
			// A PDF's page is drawn now, at the size the page shows it;
			// its bytes take the place the book's pictures would.
			raw = map[string][]byte{}
			data, err := book.Render(at)
			if err != nil {
				renderErr = err
			} else {
				raw[pdf.PageSrc(at)] = data
			}
		}
		images := decodeReaderChapter(chapters, at, raw)
		a.post(func() {
			if !a.rd.open || a.rd.gen != gen || a.rd.imageGen != imgGen || a.rd.chapter != at {
				return
			}
			if renderErr != nil {
				a.toastError(renderErr)
				return
			}
			a.rd.imageData = raw
			a.rd.images = images
		})
	})
}

// readerImageFits checks a bitmap's size before decoding it, so a
// decompression bomb is turned away before it takes any memory. An SVG
// has no pixels until it is drawn, so it is checked after decoding.
func readerImageFits(data []byte) bool {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return true
	}
	return cfg.Width > 0 && cfg.Height > 0 &&
		cfg.Width <= maxReaderImageSide && cfg.Height <= maxReaderImageSide &&
		int64(cfg.Width)*int64(cfg.Height) <= maxReaderImagePixels
}

// bookFile is the path of b's file in format.
func bookFile(lib *calibre.Library, b *calibre.Book, format string) (string, error) {
	d, err := lib.Details(b)
	if err != nil {
		return "", err
	}
	for _, f := range d.Files {
		if f.Format == format {
			return f.Path, nil
		}
	}
	return "", errNoFormat{book: b.Title, format: format}
}

// openDocument reads the book's file in format and extracts its text.
func openDocument(lib *calibre.Library, b *calibre.Book, format string) (*reader.Document, error) {
	path, err := bookFile(lib, b, format)
	if err != nil {
		return nil, err
	}
	return reader.Extract(path, format)
}

type errNoFormat struct{ book, format string }

func (e errNoFormat) Error() string {
	return e.book + " has no " + e.format + " file"
}

// adoptReading shows a plugin's reading, at the place kept for the book.
func (a *App) adoptReading(r *plugin.Reading) {
	a.rd.reading = r
	a.rd.chapters = r.Chapters
	a.rd.indent = r.Typography.Indent || isCJKLanguage(firstLanguage(a.rd.book.Languages))
	a.rd.percents = chapterShares(r.Chapters)
	if len(a.rd.chapters) == 0 {
		a.rd.err = errEmptyReading{}
		return
	}
	// The book may have been laid out with other chapters since; keep the
	// reader where they were, in the book's own measure.
	if p := a.settings.Reading.Books[readerKey(a.rd.book)]; p != nil && p.Chapters > 0 && p.Chapters != len(a.rd.chapters) {
		pos := (float64(p.Chapter) + p.Percent) / float64(p.Chapters) * float64(len(a.rd.chapters))
		if pos >= float64(len(a.rd.chapters)) {
			pos = math.Nextafter(float64(len(a.rd.chapters)), 0)
		}
		i := int(math.Max(0, pos))
		a.rd.chapter, a.rd.pending = i, pos-float64(i)
	}
	a.rd.chapter = min(max(a.rd.chapter, 0), len(a.rd.chapters)-1)
	a.rd.bookmarks = slices.DeleteFunc(a.rd.bookmarks, func(i int) bool { return i < 0 || i >= len(a.rd.chapters) })
	sort.Ints(a.rd.bookmarks)
	a.rd.highlights = validReaderHighlights(a.rd.highlights, a.rd.chapters)
}

type errEmptyReading struct{}

func (errEmptyReading) Error() string { return "the reader plugin returned no chapters" }

// chapterShares are each chapter's part of the book by its characters, so
// the progress bar moves as the text is read, not by chapter count.
func chapterShares(chs []reader.Chapter) []float64 {
	n := make([]float64, len(chs))
	total := 0.0
	for i, ch := range chs {
		for _, b := range ch.Blocks {
			n[i] += float64(len([]rune(b.Text)) + len([]rune(b.Caption)))
			if b.Table != nil {
				n[i] += float64(len([]rune(b.Table.Caption)))
				for _, row := range b.Table.Rows {
					for _, c := range row.Cells {
						n[i] += float64(len([]rune(c.Text)))
					}
				}
			}
		}
		for _, note := range ch.Notes {
			n[i] += float64(len([]rune(note.Text)))
		}
		total += n[i]
	}
	if total <= 0 {
		for i := range n {
			n[i] = 1 / float64(len(n))
		}
		return n
	}
	for i := range n {
		n[i] /= total
	}
	return n
}

// readerFraction is how far into the current chapter the page is.
func (a *App) readerFraction() float64 {
	if a.rd.scroll.MaxY <= 0 {
		return a.rd.pending
	}
	return math.Max(0, math.Min(1, float64(a.rd.scroll.Y/a.rd.scroll.MaxY)))
}

// readerUpdateProgress sets the book's progress from the page, every frame,
// and keeps the reader's own copy of each chapter's start.
func (a *App) readerUpdateProgress() {
	r := &a.rd
	if !r.open || len(r.chapters) == 0 || r.loading || r.err != nil {
		return
	}
	before := 0.0
	for i := 0; i < r.chapter && i < len(r.percents); i++ {
		before += r.percents[i]
	}
	share := 1.0
	if r.chapter < len(r.percents) {
		share = r.percents[r.chapter]
	}
	r.percent = math.Max(0, math.Min(1, before+share*a.readerFraction()))
}

// saveReading keeps the place in the book and the typography.
func (a *App) saveReading() {
	r := &a.rd
	if !r.open || r.book == nil {
		return
	}
	if r.reading != nil && len(r.chapters) > 0 {
		p := a.settings.Reading.Book(readerKey(r.book))
		p.Chapter = r.chapter
		p.Chapters = len(r.chapters)
		p.Percent = a.readerFraction()
		p.Bookmarks = slices.Clone(r.bookmarks)
		p.Highlights = slices.Clone(r.highlights)
		p.Updated = time.Now()
	}
	a.saveReadingStyles()
	_ = a.settings.Save()
}

func (a *App) saveReadingStyles() {
	if a.rd.theme == "" || a.rd.size <= 0 || a.rd.lineHeight <= 0 {
		// A book that failed to open left no styles to keep.
		return
	}
	a.settings.Reading.Theme = a.rd.theme
	a.settings.Reading.Size = a.rd.size
	a.settings.Reading.LineHeight = a.rd.lineHeight
}

// readerClose leaves the page, keeping the place.
func (a *App) readerClose() {
	if !a.rd.open {
		return
	}
	a.saveReading()
	a.readerDiscardPDF()
	gen := a.rd.gen + 1
	a.rd = readerState{gen: gen}
}

// readerChapterTo turns to chapter i (0-based), closing the contents
// unless the reader has pinned them open.
func (a *App) readerChapterTo(i int) {
	r := &a.rd
	if !r.open || len(r.chapters) == 0 {
		return
	}
	i = min(max(i, 0), len(r.chapters)-1)
	if i == r.chapter {
		if !a.readerContentsPinned() {
			r.toc = false
		}
		return
	}
	a.saveReading()
	r.chapter = i
	r.pending = 0
	r.scroll.Y = 0
	if !a.readerContentsPinned() {
		r.toc = false
	}
	a.readerLoadImages()
	a.readerUpdateProgress()
}

// readerGo turns delta chapters.
func (a *App) readerGo(delta int) { a.readerChapterTo(a.rd.chapter + delta) }

// readerToggleBookmark marks or unmarks the current chapter.
func (a *App) readerToggleBookmark() {
	r := &a.rd
	if !r.open || len(r.chapters) == 0 {
		return
	}
	if slices.Contains(r.bookmarks, r.chapter) {
		r.bookmarks = slices.DeleteFunc(r.bookmarks, func(i int) bool { return i == r.chapter })
	} else {
		r.bookmarks = append(r.bookmarks, r.chapter)
		sort.Ints(r.bookmarks)
	}
	a.saveReading()
}

func (a *App) readerBookmarked(i int) bool { return slices.Contains(a.rd.bookmarks, i) }

// readerSelectionDelay is how long a selection rests before the menu
// opens over it: a drag passes through many selections on its way.
const readerSelectionDelay = 140 * time.Millisecond

// readerTrackSelection turns the page's last selection into the text Copy
// reads and the passages a mark covers, and puts the menu where the
// pointer let go. It runs once the chapter's blocks are built, so their
// elements are known.
func (a *App) readerTrackSelection(c *ui.Context, col ui.Element) {
	r := &a.rd
	if r.selDrop {
		r.sel = readerSelection{}
		r.selHidden = false
		r.selDrop = false
		return
	}
	x, y, over := col.PointerPosition()
	parts := col.SelectedTexts()
	if len(parts) == 0 {
		// Pressing the menu takes the selection away before the button's
		// click arrives; keep the card until the press outside it says
		// the reader is done with the passage.
		if r.sel.text == "" || r.selHidden || r.toc || r.typeSheet {
			r.sel = readerSelection{}
			r.selHidden = false
		}
		return
	}
	var sel readerSelection
	var sig, copied strings.Builder
	for _, p := range parts {
		runes := []rune(p.Text)
		from := min(max(p.Start, 0), len(runes))
		to := min(max(p.End, 0), len(runes))
		if to <= from {
			continue
		}
		text := string(runes[from:to])
		blockIndex, start, end := -1, from, to
		if ref, ok := r.blockRefs[p.ID]; ok && ref.index >= 0 && ref.index < len(r.chapters[r.chapter].Blocks) {
			block := []rune(r.chapters[r.chapter].Blocks[ref.index].Text)
			start = min(max(from-ref.prefix, 0), len(block))
			end = min(max(to-ref.prefix, 0), len(block))
			if end > start {
				text = string(block[start:end])
				blockIndex = ref.index
				sel.parts = append(sel.parts, readerSelectionPart{
					block: ref.index, start: start, end: end, text: text,
				})
			}
		}
		// The menu is built while its own press takes the selection's
		// element IDs away, so the signature names the block, not the
		// element, and the card doesn't move under the pointer.
		if sig.Len() > 0 {
			sig.WriteByte(0x1f)
		}
		sig.WriteString(strconv.Itoa(blockIndex))
		sig.WriteByte(':')
		sig.WriteString(strconv.Itoa(start))
		sig.WriteByte(':')
		sig.WriteString(strconv.Itoa(end))
		sig.WriteByte(':')
		sig.WriteString(text)
		if text != "" {
			if copied.Len() > 0 {
				copied.WriteByte(10)
			}
			copied.WriteString(text)
		}
	}
	sel.sig, sel.text = sig.String(), copied.String()
	if sel.sig == "" {
		r.sel = readerSelection{}
		r.selHidden = false
		return
	}
	same := sel.sig == r.sel.sig
	placed := false
	onMenu := false
	if over {
		bounds := col.Bounds()
		px, py := bounds.X+x, bounds.Y+y
		onMenu = r.menuOn && px >= r.menuX && px <= r.menuX+readerMenuW &&
			py >= r.menuY && py <= r.menuY+readerMenuH
		if !onMenu && col.Pressed() {
			// A drag names its place as it goes; a selection made with
			// the keyboard falls back to the block it starts in.
			sel.x, sel.y = px, py
			placed = bounds.W > 0
		}
	}
	if !placed && r.sel.text == sel.text && (r.sel.x != 0 || r.sel.y != 0) {
		// The pointer went to the menu; the card stays where the
		// selection put it instead of jumping to the paragraph.
		sel.x, sel.y = r.sel.x, r.sel.y
		placed = true
	}
	if !placed {
		for _, part := range sel.parts {
			el, ok := r.blockElems[part.block]
			if !ok {
				continue
			}
			bounds := el.Bounds()
			if bounds.W <= 0 {
				continue
			}
			sel.x, sel.y = bounds.X+bounds.W/2, bounds.Y
			placed = true
			break
		}
	}
	if !placed {
		bounds := col.Bounds()
		sel.x, sel.y = bounds.X+bounds.W/2, bounds.Y+120
	}
	if !same {
		sel.since = time.Now()
		r.sel = sel
		r.selHidden = false
		c.After(readerSelectionDelay)
		return
	}
	sel.since, sel.ready = r.sel.since, r.sel.ready
	if !sel.ready && time.Since(sel.since) >= readerSelectionDelay {
		sel.ready = true
	}
	r.sel = sel
}

// readerDropSelection puts the page's selection away for good: opening
// the contents or the type sheet, or pressing Escape, is the reader
// leaving the passage, not stepping away from it. The next frame rebuilds
// the text without a selection scope, which also clears the toolkit's
// selection; selDrop keeps the card from taking the stale one back.
func (a *App) readerDropSelection() {
	a.rd.sel = readerSelection{}
	a.rd.selHidden = false
	a.rd.menuOn = false
	a.rd.selDrop = true
}

// readerSelectionShown reports whether the menu should open over the
// page, not in the contents or the type sheet.
func (a *App) readerSelectionShown() bool {
	r := &a.rd
	return r.open && !r.loading && r.err == nil && !r.toc && !r.typeSheet &&
		r.sel.ready && !r.selHidden && r.sel.text != ""
}

// readerCopySelection puts the page's selection on the clipboard.
func (a *App) readerCopySelection(c *ui.Context) {
	if a.rd.sel.text == "" {
		return
	}
	c.WriteClipboard(a.rd.sel.text)
	a.rd.selHidden = true
	a.toast("%s", locales.ReaderCopied())
}

// readerMarkSelection paints the page's selection in color, replacing the
// marks it overlaps. A selection across paragraphs becomes one mark per
// block.
func (a *App) readerMarkSelection(color string) {
	r := &a.rd
	if !theme.IsReadingHighlight(color) || len(r.sel.parts) == 0 {
		return
	}
	for _, part := range r.sel.parts {
		r.highlights = slices.DeleteFunc(r.highlights, func(h settings.Highlight) bool {
			return h.Chapter == r.chapter && h.Block == part.block && h.Start < part.end && part.start < h.End
		})
		r.highlights = append(r.highlights, settings.Highlight{
			Chapter: r.chapter, Block: part.block, Start: part.start, End: part.end,
			Color: color, Text: part.text,
		})
	}
	if len(r.highlights) > maxReaderHighlights {
		r.highlights = slices.Delete(r.highlights, 0, len(r.highlights)-maxReaderHighlights)
	}
	r.selHidden = true
	a.saveReading()
}

// readerSelectionMarked reports whether the page's selection already
// carries a mark, which the menu then offers to remove.
func (a *App) readerSelectionMarked() bool {
	r := &a.rd
	for _, part := range r.sel.parts {
		for _, h := range r.highlights {
			if h.Chapter == r.chapter && h.Block == part.block && h.Start < part.end && part.start < h.End {
				return true
			}
		}
	}
	return false
}

// readerRemoveSelectionMarks takes away the marks the selection touches.
func (a *App) readerRemoveSelectionMarks() {
	r := &a.rd
	if len(r.sel.parts) == 0 {
		return
	}
	for _, part := range r.sel.parts {
		r.highlights = slices.DeleteFunc(r.highlights, func(h settings.Highlight) bool {
			return h.Chapter == r.chapter && h.Block == part.block && h.Start < part.end && part.start < h.End
		})
	}
	r.selHidden = true
	a.saveReading()
}

// validReaderHighlights drops marks a new layout can't place: another
// chapter, a block that is gone, runes past its text, a color that isn't
// one of the page's, or words other than those that were marked.
func validReaderHighlights(highlights []settings.Highlight, chapters []reader.Chapter) []settings.Highlight {
	out := highlights[:0]
	for _, h := range highlights {
		if h.Chapter < 0 || h.Chapter >= len(chapters) {
			continue
		}
		blocks := chapters[h.Chapter].Blocks
		if h.Block < 0 || h.Block >= len(blocks) {
			continue
		}
		text := []rune(blocks[h.Block].Text)
		if h.Start < 0 || h.End > len(text) || h.Start >= h.End || !theme.IsReadingHighlight(h.Color) {
			continue
		}
		if h.Text != "" && string(text[h.Start:h.End]) != h.Text {
			continue
		}
		out = append(out, h)
	}
	if len(out) > maxReaderHighlights {
		out = out[len(out)-maxReaderHighlights:]
	}
	return out
}

// readerSetTheme changes the page's color, keeping it.
func (a *App) readerSetTheme(name string) {
	if !theme.IsReadingTheme(name) {
		return
	}
	a.rd.theme = name
	a.saveReading()
}

// readerSetSize steps the text's size by delta DIPs. A PDF page is a
// picture, so its size is the page's, not the reader's.
func (a *App) readerSetSize(delta int) {
	if a.rd.format == pdfFormat {
		return
	}
	a.rd.size = min(max(a.rd.size+delta, 14), 30)
	a.saveReading()
}

// readerSetLineHeight steps the line height by delta. A PDF page keeps the
// leading its typesetter gave it.
func (a *App) readerSetLineHeight(delta float64) {
	if a.rd.format == pdfFormat {
		return
	}
	h := math.Round((a.rd.lineHeight+delta)*10) / 10
	a.rd.lineHeight = min(max(h, 1.4), 2.6)
	a.saveReading()
}

// readerTitle is chapter i's name, as the page shows it: the plugin's, or
// Dalibre's number for a chapter that came without one.
func (a *App) readerTitle(i int) string {
	if i < 0 || i >= len(a.rd.chapters) {
		return ""
	}
	ch := a.rd.chapters[i]
	if t := strings.TrimSpace(ch.Title); t != "" {
		return t
	}
	if ch.Continued {
		return ""
	}
	if a.rd.format == pdfFormat {
		return locales.ReaderPageN(i + 1)
	}
	return locales.ReaderChapterN(i + 1)
}

// readerDepth is chapter i's place in the book: 1 for a part, 2 for a
// chapter under it. A plugin that doesn't say gets a flat list.
func (a *App) readerDepth(i int) int {
	if i < 0 || i >= len(a.rd.chapters) {
		return 1
	}
	d := a.rd.chapters[i].Depth
	if d < 1 {
		return 1
	}
	if d > 6 {
		return 6
	}
	return d
}

// tocEntries are the chapters the contents lists: a long chapter's parts
// are listed once, at its first part.
func (a *App) tocEntries() []int {
	var out []int
	for i, ch := range a.rd.chapters {
		if ch.Continued {
			continue
		}
		out = append(out, i)
	}
	if len(out) == 0 && len(a.rd.chapters) > 0 {
		out = []int{0}
	}
	return out
}

func firstLanguage(langs []string) string {
	if len(langs) == 0 {
		return ""
	}
	return langs[0]
}

func isCJKLanguage(lang string) bool {
	l := strings.TrimPrefix(strings.ToLower(lang), "x-")
	return strings.HasPrefix(l, "zh") || strings.HasPrefix(l, "zho") || strings.HasPrefix(l, "chi") ||
		strings.HasPrefix(l, "ja") || strings.HasPrefix(l, "jpn") ||
		strings.HasPrefix(l, "ko") || strings.HasPrefix(l, "kor")
}
