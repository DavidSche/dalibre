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

	tocList   ui.ListState
	marksList ui.ListState
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

// readableFormat is the format the reader would open b in, if a plugin
// reads one: the one Dalibre reads best first.
func (a *App) readableFormat(b *calibre.Book) (string, bool) {
	if b == nil {
		return "", false
	}
	order := append(slices.Clone(reader.Formats), b.Formats...)
	for _, f := range order {
		if slices.Contains(b.Formats, f) && a.readerPlugin(f) != nil {
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

// openReader shows b in the reader page, laid out by a reader plugin.
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
	p := a.readerPlugin(format)
	if p == nil {
		a.toast("%s", locales.ReaderNoPlugin(format))
		return
	}
	name, size, lineHeight := a.readerStyles()
	prev := a.settings.Reading.Books[readerKey(b)]
	marks := []int{}
	chapter, fraction := 0, 0.0
	if prev != nil {
		marks = slices.Clone(prev.Bookmarks)
		chapter, fraction = prev.Chapter, prev.Percent
	}
	gen := a.rd.gen + 1
	a.rd = readerState{
		open: true, loading: true, gen: gen, book: b, format: format,
		tocTab: "contents", pending: fraction, chapter: chapter,
		theme: name, size: size, lineHeight: lineHeight, bookmarks: marks,
	}
	lib := a.lib
	info := plugin.ReaderBook{
		ID: strconv.FormatInt(b.ID, 10), Title: b.Title, Authors: slices.Clone(b.Authors),
		Language: firstLanguage(b.Languages), Format: format,
	}
	at := a.rd.chapter
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

// A picture's pixels are bounded too: a small file can decode to a plate
// far larger than any screen, and a chapter has room for many.
const (
	maxReaderImagePixels = 24 << 20
	maxReaderImageSide   = 12_000
	// maxReaderChapterPixels bounds the bitmaps one chapter decodes, so a
	// chapter of plates can't fill memory when the reader turns to it.
	maxReaderChapterPixels = 32 << 20
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

// readerLoadImages decodes the current chapter's pictures in the
// background and lets the previous chapter's go when they are ready. A
// quicker turn makes an earlier decode's result stale, so it is dropped.
func (a *App) readerLoadImages() {
	r := &a.rd
	if !r.open || len(r.chapters) == 0 {
		return
	}
	r.imageGen++
	gen, imgGen, at := r.gen, r.imageGen, r.chapter
	chapters, raw := r.chapters, r.imageData
	if len(raw) == 0 {
		r.images = nil
		return
	}
	a.run(func() {
		images := decodeReaderChapter(chapters, at, raw)
		a.post(func() {
			if a.rd.open && a.rd.gen == gen && a.rd.imageGen == imgGen && a.rd.chapter == at {
				a.rd.images = images
			}
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

// openDocument reads the book's file in format and extracts its text.
func openDocument(lib *calibre.Library, b *calibre.Book, format string) (*reader.Document, error) {
	d, err := lib.Details(b)
	if err != nil {
		return nil, err
	}
	for _, f := range d.Files {
		if f.Format != format {
			continue
		}
		return reader.Extract(f.Path, format)
	}
	return nil, errNoFormat{book: b.Title, format: format}
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

// readerSetTheme changes the page's color, keeping it.
func (a *App) readerSetTheme(name string) {
	if !theme.IsReadingTheme(name) {
		return
	}
	a.rd.theme = name
	a.saveReading()
}

// readerSetSize steps the text's size by delta DIPs.
func (a *App) readerSetSize(delta int) {
	a.rd.size = min(max(a.rd.size+delta, 14), 30)
	a.saveReading()
}

// readerSetLineHeight steps the line height by delta.
func (a *App) readerSetLineHeight(delta float64) {
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
