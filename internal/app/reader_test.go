package app

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"dalibre/internal/app/theme"
	"dalibre/internal/calibre"
	"dalibre/internal/reader"
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
  <p>A hall of statues, and one of them was me.</p>
  <p>The tide came in through the lower doors.</p>
  <blockquote><p>Where are the others?</p></blockquote>
  <h2>The first hall</h2>
  <p>Its walls were painted with birds.<a epub:type="noteref" href="#n1">1</a></p>
  <table><caption>Measures of the halls</caption>
    <tr><th>Hall</th><th>Width</th></tr>
    <tr><td>First</td><td>12 paces</td></tr>
    <tr><td>Second</td><td>9 paces</td></tr>
  </table>
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
