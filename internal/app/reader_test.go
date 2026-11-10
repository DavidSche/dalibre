package app

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"dalibre/internal/app/theme"
	"dalibre/internal/calibre"
	"dalibre/internal/reader"
	"dalibre/internal/settings"
)

// testEPUB builds a small book with two chapters and a navigation document.
func testEPUB(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	addBytes := func(name string, body []byte) {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	add := func(name, body string) {
		addBytes(name, []byte(body))
	}
	add("mimetype", "application/epub+zip")
	add("META-INF/container.xml", `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`)
	add("OEBPS/content.opf", `<?xml version="1.0"?>
<package version="3.0" xmlns="http://www.idpf.org/2007/opf" unique-identifier="id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Piranesi</dc:title><dc:language>eng</dc:language>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="ch1" href="ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="ch2" href="ch2.xhtml" media-type="application/xhtml+xml"/>
    <item id="plate" href="img/plate.png" media-type="image/png"/>
    <item id="plate2" href="img/plate2.png" media-type="image/png"/>
  </manifest>
  <spine><itemref idref="ch1"/><itemref idref="ch2"/></spine>
</package>`)
	add("OEBPS/nav.xhtml", `<?xml version="1.0"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<body><nav epub:type="toc"><ol>
  <li><a href="ch1.xhtml">The Halls</a><ol>
    <li><a href="ch2.xhtml">The Statues</a></li>
  </ol></li>
</ol></nav></body></html>`)
	add("OEBPS/ch1.xhtml", `<?xml version="1.0"?>
<html xmlns="http://www.w3.org/1999/xhtml"><body>
  <h1>The Halls</h1>
  <p>A hall of statues, and one of them was <strong>me</strong>.</p>
  <p>The tide came in through the lower doors.</p>
  <blockquote><p>Where are the others?</p></blockquote>
  <h2>The first hall</h2>
  <p>Its walls were painted with birds.<a epub:type="noteref" href="#n1">1</a></p>
  <table><caption>Measures of the halls</caption>
    <tr><th>Hall</th><th>Width</th></tr>
    <tr><td>First</td><td>12 paces</td></tr>
    <tr><td>Second</td><td>9 paces</td></tr>
  </table>
  <pre><code><div>def hall():</div><div>&nbsp;&nbsp;&nbsp;&nbsp;return 12</div></code></pre>
  <aside id="n1" epub:type="footnote"><p>Measured by the tide.</p>
    <a href="#ref1" epub:type="backlink">↩</a></aside>
  <figure><img src="img/plate.png" alt="A painted hall"/><figcaption>The western wall</figcaption></figure>
</body></html>`)
	add("OEBPS/ch2.xhtml", `<?xml version="1.0"?>
<html xmlns="http://www.w3.org/1999/xhtml"><body><p>Second chapter text.</p>
  <figure><img src="img/plate2.png" alt="A second plate"/></figure>
</body></html>`)
	addBytes("OEBPS/img/plate.png", testPNG(t))
	addBytes("OEBPS/img/plate2.png", testPNGSize(t, 120, 80))
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestReaderSpansEmphasis(t *testing.T) {
	accent := ui.Hex("#b4552d")
	runs := []reader.Run{
		{Text: "A "},
		{Text: "bold", Bold: true},
		{Text: " and "},
		{Text: "italic", Italic: true},
	}
	spans, ok := readerSpans("A bold and italic", runs, nil, nil, 0, 18, accent, false)
	if !ok || len(spans) != 4 {
		t.Fatalf("spans %+v, ok %v", spans, ok)
	}
	if spans[1].Text != "bold" || spans[1].Weight != theme.ReaderStrongWeight {
		t.Fatalf("bold span %+v", spans[1])
	}
	if spans[3].Text != "italic" || !spans[3].Italic {
		t.Fatalf("italic span %+v", spans[3])
	}

	labels := map[string]bool{"1": true}
	runs = []reader.Run{{Text: "A "}, {Text: "bold", Bold: true}, {Text: " note[1]"}}
	spans, ok = readerSpans("A bold note[1]", runs, labels, nil, 0, 18, accent, false)
	if !ok || len(spans) != 4 {
		t.Fatalf("spans %+v, ok %v", spans, ok)
	}
	if got := spans[3]; got.Text != "[1]" || got.Weight != 500 || got.Size != 15 || got.Color != accent {
		t.Fatalf("note span %+v", got)
	}
}

func TestReaderHighlightSpans(t *testing.T) {
	accent := ui.Hex("#b4552d")
	marks := []settings.Highlight{{Start: 2, End: 7, Color: "yellow"}}
	spans, ok := readerSpans("hello world", nil, nil, marks, 0, 18, accent, false)
	if !ok || len(spans) != 3 {
		t.Fatalf("spans %+v, ok %v", spans, ok)
	}
	if spans[0].Text != "he" || spans[2].Text != "orld" {
		t.Fatalf("cut %+v", spans)
	}
	if got := spans[1]; got.Text != "llo w" || got.Background != theme.ReadingHighlight("yellow", false) || got.Radius != 3 {
		t.Fatalf("mark %+v", got)
	}

	// The page's indentation is not part of the marked runes.
	marks = []settings.Highlight{{Start: 0, End: 5, Color: "blue"}}
	spans, ok = readerSpans("　　hello", nil, nil, marks, 2, 18, accent, false)
	if !ok || len(spans) != 2 || spans[0].Text != "　　" || spans[1].Text != "hello" {
		t.Fatalf("indented spans %+v, ok %v", spans, ok)
	}
	if spans[1].Background != theme.ReadingHighlight("blue", false) {
		t.Fatalf("indented mark %+v", spans[1])
	}

	// A mark keeps the emphasis of the run it falls in.
	runs := []reader.Run{{Text: "A "}, {Text: "bold", Bold: true}}
	marks = []settings.Highlight{{Start: 2, End: 5, Color: "pink"}}
	spans, ok = readerSpans("A bold", runs, nil, marks, 0, 18, accent, false)
	if !ok || len(spans) != 3 || spans[1].Text != "bol" || spans[2].Text != "d" {
		t.Fatalf("bold spans %+v, ok %v", spans, ok)
	}
	if spans[1].Weight != theme.ReaderStrongWeight || spans[1].Background != theme.ReadingHighlight("pink", false) {
		t.Fatalf("bold mark %+v", spans[1])
	}
}

func TestReaderHighlightsFollowTheBook(t *testing.T) {
	chapters := []reader.Chapter{{Blocks: []reader.Block{
		{Kind: reader.Paragraph, Text: "A hall of statues."},
	}}}
	in := []settings.Highlight{
		{Chapter: 0, Block: 0, Start: 2, End: 6, Color: "yellow", Text: "hall"},
		{Chapter: 0, Block: 0, Start: 2, End: 6, Color: "yellow", Text: "door"}, // words moved
		{Chapter: 1, Block: 0, Start: 0, End: 1, Color: "green"},                // chapter gone
		{Chapter: 0, Block: 3, Start: 0, End: 1, Color: "green"},                // block gone
		{Chapter: 0, Block: 0, Start: 2, End: 6, Color: "grey", Text: "hall"},   // no such color
	}
	out := validReaderHighlights(in, chapters)
	if len(out) != 1 || out[0].Text != "hall" {
		t.Fatalf("kept %+v", out)
	}
}

func TestReaderMarksSelection(t *testing.T) {
	a := newTestApp(t)
	a.rd.open = true
	a.rd.chapters = []reader.Chapter{{Blocks: []reader.Block{
		{Kind: reader.Paragraph, Text: "A hall of statues."},
	}}}
	a.rd.sel.parts = []readerSelectionPart{{block: 0, start: 2, end: 6, text: "hall"}}
	a.readerMarkSelection("yellow")
	if len(a.rd.highlights) != 1 || !a.readerSelectionMarked() {
		t.Fatalf("marks %+v", a.rd.highlights)
	}
	// Marking the same words again replaces the mark instead of stacking.
	a.readerMarkSelection("green")
	if len(a.rd.highlights) != 1 || a.rd.highlights[0].Color != "green" {
		t.Fatalf("marks %+v", a.rd.highlights)
	}
	a.readerRemoveSelectionMarks()
	if len(a.rd.highlights) != 0 || a.readerSelectionMarked() {
		t.Fatalf("marks %+v", a.rd.highlights)
	}
}

func TestReaderKeepsMarks(t *testing.T) {
	a := newTestApp(t)
	a.builtins = true
	a.pluginTransport = fakeWeb{http.NotFoundHandler()}
	a.loadPlugins()
	book := a.bookByTitle("Dune")
	d, _ := a.lib.Details(book)
	if err := os.WriteFile(d.Files[0].Path, testEPUB(t), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openReader(book, "EPUB")
	if a.rd.err != nil {
		t.Fatal(a.rd.err)
	}
	block := -1
	for i, b := range a.rd.chapters[0].Blocks {
		if strings.HasPrefix(b.Text, "A hall of statues") {
			block = i
			break
		}
	}
	if block < 0 {
		t.Fatal("no passage to mark")
	}
	a.rd.sel.parts = []readerSelectionPart{{block: block, start: 2, end: 6, text: "hall"}}
	a.readerMarkSelection("yellow")
	a.readerClose()
	if a.settings.Reading.Book(readerKey(book)).Highlights == nil {
		t.Fatal("mark not saved")
	}

	a.openReader(book, "EPUB")
	if a.rd.err != nil {
		t.Fatal(a.rd.err)
	}
	if len(a.rd.highlights) != 1 || a.rd.highlights[0].Color != "yellow" || a.rd.highlights[0].Text != "hall" {
		t.Fatalf("marks %+v", a.rd.highlights)
	}
}

func TestReaderDropsSelectionWhenContentsOpens(t *testing.T) {
	a := newTestApp(t)
	a.builtins = true
	a.pluginTransport = fakeWeb{http.NotFoundHandler()}
	a.loadPlugins()
	book := a.bookByTitle("Dune")
	d, _ := a.lib.Details(book)
	if err := os.WriteFile(d.Files[0].Path, testEPUB(t), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openReader(book, "EPUB")
	if a.rd.err != nil {
		t.Fatal(a.rd.err)
	}
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	line := "The tide came in through the lower doors."
	r, ok := tt.Find(line)
	if !ok {
		t.Fatalf("no line to select: %q", tt.Texts())
	}
	tt.Press(r.X+8, r.Y+r.H/2)
	tt.Frame()
	tt.Move(r.X+160, r.Y+r.H/2)
	tt.Frame()
	tt.Release(r.X+160, r.Y+r.H/2)
	tt.Frame()
	a.rd.sel.since = time.Now().Add(-time.Second)
	tt.Frame()
	if !a.readerSelectionShown() {
		t.Fatal("no menu before the contents opened")
	}

	if err := tt.Click("Contents"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.rd.toc {
		t.Fatal("contents did not open")
	}
	a.rd.toc = false
	tt.Frame()
	if a.rd.sel.text != "" || a.rd.sel.ready {
		t.Fatalf("selection survived the contents: %+v", a.rd.sel)
	}
	if a.readerSelectionShown() {
		t.Fatal("menu came back after the contents closed")
	}

	// A fresh selection still opens the menu.
	if !tt.HasText(line) {
		t.Fatalf("line gone after the contents: %q", tt.Texts())
	}
	r, _ = tt.Find(line)
	tt.Press(r.X+8, r.Y+r.H/2)
	tt.Frame()
	tt.Move(r.X+160, r.Y+r.H/2)
	tt.Frame()
	tt.Release(r.X+160, r.Y+r.H/2)
	tt.Frame()
	a.rd.sel.since = time.Now().Add(-time.Second)
	tt.Frame()
	if !a.readerSelectionShown() {
		t.Fatal("menu did not open for a fresh selection")
	}
}

func TestReaderSelectsAndCopies(t *testing.T) {
	a := newTestApp(t)
	a.builtins = true
	a.pluginTransport = fakeWeb{http.NotFoundHandler()}
	a.loadPlugins()
	book := a.bookByTitle("Dune")
	d, _ := a.lib.Details(book)
	if err := os.WriteFile(d.Files[0].Path, testEPUB(t), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openReader(book, "EPUB")
	if a.rd.err != nil {
		t.Fatal(a.rd.err)
	}
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	line := "The tide came in through the lower doors."
	r, ok := tt.Find(line)
	if !ok {
		t.Fatalf("no line to select: %q", tt.Texts())
	}
	tt.Press(r.X+8, r.Y+r.H/2)
	tt.Frame()
	tt.Move(r.X+160, r.Y+r.H/2)
	tt.Frame()
	tt.Release(r.X+160, r.Y+r.H/2)
	tt.Frame()
	if a.rd.sel.text == "" {
		t.Fatalf("nothing selected; line %+v", r)
	}
	// The menu waits for the selection to rest; the test's clock stands
	// still, so age the selection by hand.
	a.rd.sel.since = time.Now().Add(-time.Second)
	tt.Frame()
	if !a.readerSelectionShown() || !tt.HasText("Copy") {
		t.Fatalf("no menu over %q: %q", a.rd.sel.text, tt.Texts())
	}
	if err := tt.Click("Copy"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := tt.Clipboard(); got == "" || !strings.Contains(line, got) {
		t.Fatalf("copied %q from %q", got, line)
	}
	if a.readerSelectionShown() {
		t.Fatal("menu stayed after copying")
	}
}

func TestReaderOpensEPUB(t *testing.T) {
	a := newTestApp(t)
	a.builtins = true
	a.pluginTransport = fakeWeb{http.NotFoundHandler()}
	a.loadPlugins()
	var book = a.bookByTitle("Piranesi")
	if book == nil {
		t.Fatal("no Piranesi")
	}
	d, err := a.lib.Details(book)
	if err != nil || len(d.Files) == 0 {
		t.Fatalf("details: %v", err)
	}
	if err := os.WriteFile(d.Files[0].Path, testEPUB(t), 0o644); err != nil {
		t.Fatal(err)
	}

	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	if err := tt.Click("Piranesi"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Read") {
		t.Fatalf("no Read button; texts %q", tt.Texts())
	}
	if err := tt.Click("Read"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.rd.err != nil {
		t.Fatalf("reader: %v", a.rd.err)
	}
	if a.rd.loading {
		t.Fatal("still loading")
	}
	if !tt.HasText("A hall of statues, and one of them was me.") {
		t.Fatalf("text: %q", tt.Texts())
	}
	var emphasized bool
	for _, b := range a.rd.chapters[0].Blocks {
		if b.Text == "A hall of statues, and one of them was me." && len(b.Runs) > 0 {
			emphasized = true
		}
	}
	if !emphasized {
		t.Fatal("the paragraph's emphasis was lost")
	}
	if !tt.HasText("The Halls") {
		t.Fatalf("chapter title missing: %q", tt.Texts())
	}
	if !tt.HasText("Where are the others?") {
		t.Fatal("quote missing")
	}
	if !tt.HasText("The western wall") {
		t.Fatalf("caption missing: %q", tt.Texts())
	}
	if !tt.HasText("Measures of the halls") || !tt.HasText("12 paces") {
		t.Fatalf("table missing: %q", tt.Texts())
	}
	if !tt.HasText("Measured by the tide.") {
		t.Fatalf("note missing: %q", tt.Texts())
	}
	if !tt.HasText("def hall():\n    return 12") {
		t.Fatalf("code missing: %q", tt.Texts())
	}
	if r, ok := tt.Find("def hall():\n    return 12"); !ok || r.H < 20 {
		t.Fatalf("code box %+v, %v", r, ok)
	}
	if notes := a.rd.chapters[0].Notes; len(notes) != 1 || notes[0].Label != "1" {
		t.Fatalf("notes %+v", notes)
	}
	if len(a.rd.imageData) != 2 || len(a.rd.images) != 1 {
		t.Fatalf("images %d of %d bytes", len(a.rd.images), len(a.rd.imageData))
	}
	// The plate is 240×150: it keeps that size instead of growing to the
	// measure, as a small picture must not be blown up.
	if r, ok := tt.Find("A painted hall"); !ok || r.W < 235 || r.W > 245 || r.H < 145 || r.H > 155 {
		t.Fatalf("image box %+v, %v", r, ok)
	}
	if len(a.rd.chapters) != 2 {
		t.Fatalf("chapters %d", len(a.rd.chapters))
	}
	if a.rd.chapters[0].Depth != 1 || a.rd.chapters[1].Depth != 2 {
		t.Fatalf("depths %d, %d", a.rd.chapters[0].Depth, a.rd.chapters[1].Depth)
	}
	if a.readerDepth(1) != 2 {
		t.Fatalf("contents depth %d", a.readerDepth(1))
	}
	shot(t, tt, "reader-paper")

	// The next chapter, and the way back.
	if err := tt.Click("Next chapter"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.rd.chapter != 1 || !tt.HasText("Second chapter text.") {
		t.Fatalf("chapter %d, texts %q", a.rd.chapter, tt.Texts())
	}
	// Only the chapter on the page keeps decoded pictures.
	if len(a.rd.images) != 1 {
		t.Fatalf("images %d", len(a.rd.images))
	}
	if _, ok := a.rd.images["OEBPS/img/plate.png"]; ok {
		t.Fatal("the first chapter's picture was kept")
	}
	if _, ok := a.rd.images["OEBPS/img/plate2.png"]; !ok {
		t.Fatalf("second chapter's picture missing: %v", a.rd.images)
	}
	// A bookmark, and the page kept for the book.
	if err := tt.Click("Bookmark this chapter"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.readerBookmarked(1) {
		t.Fatal("not bookmarked")
	}
	if err := tt.Click("Back to library"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.rd.open {
		t.Fatal("reader still open")
	}
	p := a.settings.Reading.Book(readerKey(book))
	if p.Chapter != 1 || len(p.Bookmarks) != 1 {
		t.Fatalf("progress %+v", p)
	}
	// The shelf now offers to carry on.
	carryOn := false
	for _, txt := range tt.Texts() {
		if strings.HasPrefix(txt, "Continue · ") {
			carryOn = true
		}
	}
	if !carryOn {
		t.Fatalf("continue label missing: %q", tt.Texts())
	}
}

func TestReaderThemeAndSize(t *testing.T) {
	a := newTestApp(t)
	a.builtins = true
	a.pluginTransport = fakeWeb{http.NotFoundHandler()}
	a.loadPlugins()
	book := a.bookByTitle("Dune")
	d, _ := a.lib.Details(book)
	os.WriteFile(d.Files[0].Path, testEPUB(t), 0o644)

	a.openReader(book, "EPUB")
	if a.rd.err != nil {
		t.Fatal(a.rd.err)
	}
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	if err := tt.Click("Text size"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Night"); err != nil {
		t.Fatalf("theme: %v", err)
	}
	tt.Frame()
	if a.rd.theme != "night" {
		t.Fatalf("theme %q", a.rd.theme)
	}
	shot(t, tt, "reader-night")
	tt.TypeKey(0, ui.KeyEscape, "")
	tt.Frame()
	for _, name := range []string{"sepia", "green"} {
		a.readerSetTheme(name)
		tt.Frame()
		shot(t, tt, "reader-"+name)
	}
	a.readerSetTheme("night")
	before := a.rd.size
	tt.TypeKey(0, ui.KeyEqual, "+")
	if a.rd.size != before+1 {
		t.Fatalf("size %d, before %d", a.rd.size, before)
	}
	if a.settings.Reading.Theme != "night" || a.settings.Reading.Size != a.rd.size {
		t.Fatalf("styles not kept: %+v", a.settings.Reading)
	}
}

func TestReaderContentsDocks(t *testing.T) {
	a := newTestApp(t)
	a.builtins = true
	a.pluginTransport = fakeWeb{http.NotFoundHandler()}
	a.loadPlugins()
	book := a.bookByTitle("Dune")
	d, _ := a.lib.Details(book)
	os.WriteFile(d.Files[0].Path, testEPUB(t), 0o644)

	a.openReader(book, "EPUB")
	if a.rd.err != nil {
		t.Fatal(a.rd.err)
	}
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	if err := tt.Click("Contents"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Dock contents on the left") || !tt.HasText("Dock contents on the right") {
		t.Fatalf("no dock buttons: %q", tt.Texts())
	}
	if err := tt.Click("Dock contents on the left"); err != nil {
		t.Fatal(err)
	}
	if a.readerContentsSide() != "left" || a.settings.Reading.TOCSide != "left" {
		t.Fatalf("side %q, settings %q", a.readerContentsSide(), a.settings.Reading.TOCSide)
	}
	tt.Frame()
	if !tt.HasText("The Halls") || !tt.HasText("A hall of statues, and one of them was me.") {
		t.Fatalf("contents replaced the page: %q", tt.Texts())
	}
	shot(t, tt, "reader-contents-left")
	// The inner edge moves it back with a drag, as a sheet snaps to an edge.
	tt.Press(336, 450)
	tt.Frame()
	tt.Move(420, 450)
	tt.Frame()
	tt.Release(420, 450)
	tt.Frame()
	if a.readerContentsSide() != "right" {
		t.Fatalf("side %q after the drag", a.readerContentsSide())
	}
	shot(t, tt, "reader-contents-right")
	if !tt.HasText("Keep contents open") {
		t.Fatalf("no pin button: %q", tt.Texts())
	}
	if err := tt.Click("Keep contents open"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.readerContentsPinned() || !a.settings.Reading.TOCPinned {
		t.Fatal("contents not pinned")
	}
	a.readerChapterTo(1)
	tt.Frame()
	if a.rd.chapter != 1 || !a.rd.toc {
		t.Fatalf("chapter %d, contents open %v after a pinned choice", a.rd.chapter, a.rd.toc)
	}
	if err := tt.Click("Let contents close"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.readerContentsPinned() {
		t.Fatal("contents still pinned")
	}
	a.readerChapterTo(0)
	tt.Frame()
	if a.rd.toc {
		t.Fatal("contents did not close after an unpinned choice")
	}
}

func (a *App) bookByTitle(title string) *calibre.Book {
	for _, b := range a.books {
		if b.Title == title {
			return b
		}
	}
	return nil
}

func TestReaderReopensWhereLeft(t *testing.T) {
	a := newTestApp(t)
	a.builtins = true
	a.pluginTransport = fakeWeb{http.NotFoundHandler()}
	a.loadPlugins()
	book := a.bookByTitle("Dune")
	d, _ := a.lib.Details(book)
	os.WriteFile(d.Files[0].Path, testEPUB(t), 0o644)

	a.openReader(book, "EPUB")
	if a.rd.err != nil {
		t.Fatal(a.rd.err)
	}
	a.rd.chapter = 1
	a.readerToggleBookmark()
	a.readerClose()

	a.openReader(book, "EPUB")
	if a.rd.err != nil {
		t.Fatal(a.rd.err)
	}
	if a.rd.chapter != 1 {
		t.Fatalf("chapter %d", a.rd.chapter)
	}
	if !a.readerBookmarked(1) {
		t.Fatal("bookmark lost")
	}
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	if !tt.HasText("Second chapter text.") {
		t.Fatalf("not at the kept place: %q", tt.Texts())
	}
	if err := tt.Click("Contents"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Bookmarks") || !tt.HasText("The Halls") {
		t.Fatalf("contents: %q", tt.Texts())
	}
	if err := tt.Click("The Halls"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.rd.chapter != 0 {
		t.Fatalf("chapter %d after the contents", a.rd.chapter)
	}
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	return testPNGSize(t, 240, 150)
}

func testPNGSize(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8(80 + x*100/max(w, 1)),
				G: uint8(110 + y*80/max(h, 1)),
				B: uint8(150 + (x+y)*60/max(w+h, 1)),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestReaderImageSizes checks that a picture keeps its own size and only
// a picture wider than the measure shrinks: a formula's letters stay as
// small as the book set them, and a plate fits the page.
func TestReaderImageSizes(t *testing.T) {
	small, err := ui.DecodeImage(testPNGSize(t, 14, 13))
	if err != nil {
		t.Fatal(err)
	}
	large, err := ui.DecodeImage(testPNGSize(t, 1600, 600))
	if err != nil {
		t.Fatal(err)
	}
	a := &App{}
	a.rd.images = map[string]ui.ImageSource{"small": small, "large": large}
	tt := ui.NewTester(func(c *ui.Context) {
		p := theme.ReadingPage("paper")
		a.readerImage(c, p, reader.Block{Kind: reader.Image, Src: "small", Alt: "small formula"})
		a.readerImage(c, p, reader.Block{Kind: reader.Image, Src: "large", Alt: "large plate"})
	}, 1000, 800)
	tt.Frame()
	if r, ok := tt.Find("small formula"); !ok || r.W < 13 || r.W > 15 || r.H < 12 || r.H > 14 {
		t.Fatalf("small image box %+v, %v", r, ok)
	}
	if r, ok := tt.Find("large plate"); !ok || r.W < 715 || r.W > 725 {
		t.Fatalf("large image box %+v, %v", r, ok)
	}
}

// TestReaderOpensPDF checks that a PDF takes the reader's own path: each
// page is a chapter, the page is drawn when the reader lands on it, and
// the page's bitmap reaches the page like a picture would.
func TestReaderOpensPDF(t *testing.T) {
	if testing.Short() {
		t.Skip("the PDF engine takes a second to start")
	}
	a := newTestApp(t)
	data, err := os.ReadFile(filepath.Join("..", "pdf", "testdata", "one-page.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "one-page.pdf")
	if err := os.WriteFile(f, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.lib.AddBook(calibre.NewBook{File: f, Metadata: calibre.Metadata{
		Title: "Scanned Book", Authors: []string{"A. Printer"}, Languages: []string{"eng"},
	}}); err != nil {
		t.Fatal(err)
	}
	a.reload(nil)
	book := a.bookByTitle("Scanned Book")
	if book == nil {
		t.Fatal("the PDF is not in the library")
	}
	if format, ok := a.readableFormat(book); !ok || format != "PDF" {
		t.Fatalf("format = %q, %v", format, ok)
	}
	a.openReader(book, "PDF")
	if a.rd.err != nil {
		t.Fatal(a.rd.err)
	}
	if a.rd.pdf == nil {
		t.Fatal("the PDF was not kept")
	}
	if len(a.rd.chapters) != 1 || a.rd.chapters[0].Title != "Page 1" {
		t.Fatalf("chapters = %+v", a.rd.chapters)
	}
	if len(a.rd.images) != 1 {
		t.Fatalf("images = %d, want 1", len(a.rd.images))
	}
	a.rd.chapters[0].Blocks[0].Alt = "Rendered page"
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	if !tt.HasText("Scanned Book") {
		t.Fatalf("the reader is not showing the book: %q", tt.Texts())
	}
	if r, ok := tt.Find("Rendered page"); !ok || r.W < 600 || r.H < 600 {
		t.Fatalf("page box %+v, %v", r, ok)
	}
	a.readerClose()
	if a.rd.pdf != nil {
		t.Fatal("the PDF was left open")
	}
}
