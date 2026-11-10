package reader

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func epubBytes(t *testing.T) []byte {
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
	add("META-INF/container.xml", `<container><rootfiles>
		<rootfile full-path="OPS/book.opf" media-type="application/oebps-package+xml"/>
	</rootfiles></container>`)
	add("OPS/book.opf", `<package><metadata>
		<dc:title xmlns:dc="http://purl.org/dc/elements/1.1/">A Book</dc:title>
		<dc:language xmlns:dc="http://purl.org/dc/elements/1.1/">eng</dc:language>
	</metadata><manifest>
		<item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
		<item id="a" href="a.xhtml" media-type="application/xhtml+xml"/>
		<item id="b" href="sub/b.xhtml" media-type="application/xhtml+xml"/>
		<item id="plate" href="img/plate.png" media-type="image/png"/>
	</manifest><spine toc="ncx"><itemref idref="a"/><itemref idref="b"/></spine></package>`)
	add("OPS/toc.ncx", `<ncx><navMap>
		<navPoint><navLabel><text>First Chapter</text></navLabel><content src="a.xhtml"/></navPoint>
		<navPoint><navLabel><text>Second Chapter</text></navLabel><content src="sub/b.xhtml"/></navPoint>
	</navMap></ncx>`)
	add("OPS/a.xhtml", `<html><body>
		<h1>First Chapter</h1>
		<p>One line
		continues here.</p>
		<blockquote><p>A quote.</p></blockquote>
		<h3>Sub</h3>
		<pre>code   stays</pre>
		<figure><img src="img/plate.png" alt="A plate"/><figcaption>The plate</figcaption></figure>
	</body></html>`)
	add("OPS/sub/b.xhtml", `<html><body><p>Second body.</p>
		<figure><img src="../img/plate.png" alt="Again"/></figure>
		<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 12 8">
			<image xlink:href="../img/plate.png" width="12" height="8"/>
		</svg></body></html>`)
	addBytes("OPS/img/plate.png", testPNG(t))
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPreformattedLines(t *testing.T) {
	h := parseHTML([]byte(`<html><body>
		<pre><code><div>def f():</div><div>&nbsp;&nbsp;&nbsp;&nbsp;return 1</div><div>&nbsp;&nbsp;&nbsp;&nbsp;return 2</div></code></pre>
		<p>After.</p>
	</body></html>`))
	blocks, _ := h.blocks("", nil, nil, nil, "", "")
	if len(blocks) != 2 {
		t.Fatalf("blocks %+v", blocks)
	}
	if blocks[0].Kind != Code {
		t.Fatalf("kind %q", blocks[0].Kind)
	}
	if want := "def f():\n    return 1\n    return 2"; blocks[0].Text != want {
		t.Fatalf("code %q, want %q", blocks[0].Text, want)
	}
	if blocks[1].Kind != Paragraph || blocks[1].Text != "After." {
		t.Fatalf("after %+v", blocks[1])
	}
}

func TestMonospacedParagraphLines(t *testing.T) {
	// Converters often drop <pre> and set each source line in a monospaced
	// font, paragraph by paragraph. Those lines are still one code block.
	doc := parseHTML([]byte(`<html><body>
		<p><span style="font-family:'Source Code Pro'">import numpy as np</span></p>
		<p><span style="font-family:'Source Code Pro'">x = np.arange(3)</span></p>
		<p><span style="font-family:'PingFang SC'">A paragraph.</span></p>
		<p><span style="font-family:'PingFang SC'"><span style="font-family:'Source Code Pro'"># </span>a comment in the prose face</span></p>
		<p><span style="font-family:'Source Code Pro'">print(x)</span></p>
		<p><span style="font-family:'PingFang SC'"><span style="font-family:'Source Code Pro'">torch</span> is a library.</span></p>
	</body></html>`))
	blocks, _ := doc.blocks("", nil, nil, nil, "", "")
	var got []string
	for _, b := range blocks {
		got = append(got, b.Kind+": "+b.Text)
	}
	want := []string{
		Code + ": import numpy as np\nx = np.arange(3)",
		Paragraph + ": A paragraph.",
		Code + ": # a comment in the prose face\nprint(x)",
		Paragraph + ": torch is a library.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("blocks %#v, want %#v", got, want)
	}
}

func TestInlineEmphasis(t *testing.T) {
	doc := parseHTML([]byte(`<html><body>
		<p>A <strong>bold</strong> word, <em>italic</em> and <strong><em>both</em></strong>, plus <b>b</b> and <i>i</i>.</p>
		<h2>Title <em>here</em></h2>
	</body></html>`))
	blocks, _ := doc.blocks("", nil, nil, nil, "", "")
	if len(blocks) != 2 {
		t.Fatalf("blocks %+v", blocks)
	}
	p := blocks[0]
	wantText := "A bold word, italic and both, plus b and i."
	if p.Kind != Paragraph || p.Text != wantText {
		t.Fatalf("paragraph %+v", p)
	}
	wantRuns := []Run{
		{Text: "A "},
		{Text: "bold ", Bold: true},
		{Text: "word, "},
		{Text: "italic ", Italic: true},
		{Text: "and "},
		{Text: "both", Bold: true, Italic: true},
		{Text: ", plus "},
		{Text: "b ", Bold: true},
		{Text: "and "},
		{Text: "i", Italic: true},
		{Text: "."},
	}
	if !reflect.DeepEqual(p.Runs, wantRuns) {
		t.Fatalf("runs %#v, want %#v", p.Runs, wantRuns)
	}
	h := blocks[1]
	if h.Kind != Heading || h.Level != 2 || h.Text != "Title here" {
		t.Fatalf("heading %+v", h)
	}
	if want := []Run{{Text: "Title "}, {Text: "here", Italic: true}}; !reflect.DeepEqual(h.Runs, want) {
		t.Fatalf("heading runs %#v, want %#v", h.Runs, want)
	}
}

func TestCleanRunsDropsMismatch(t *testing.T) {
	runs := []Run{{Text: "bold", Bold: true}}
	if got := CleanRuns(runs, "other"); got != nil {
		t.Fatalf("mismatched runs %#v", got)
	}
	if got := CleanRuns([]Run{{Text: "plain"}}, "plain"); got != nil {
		t.Fatalf("plain runs %#v", got)
	}
	if got := CleanRuns([]Run{{Text: "bo", Bold: true}, {Text: "ld", Bold: true}}, "bold"); len(got) != 1 || got[0].Text != "bold" {
		t.Fatalf("joined runs %#v", got)
	}
}

func TestExtractEPUB(t *testing.T) {
	d, err := Extract(writeFile(t, "book.epub", epubBytes(t)), "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "A Book" || d.Language != "eng" {
		t.Fatalf("document %+v", d)
	}
	if len(d.Chapters) != 2 {
		t.Fatalf("chapters %d", len(d.Chapters))
	}
	ch := d.Chapters[0]
	if ch.Title != "First Chapter" {
		t.Fatalf("title %q", ch.Title)
	}
	var kinds []string
	var texts []string
	for _, b := range ch.Blocks {
		kinds = append(kinds, b.Kind)
		texts = append(texts, b.Text)
	}
	want := []string{Heading, Paragraph, Quote, Heading, Code, Image}
	if len(kinds) != len(want) {
		t.Fatalf("kinds %v", kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds %v", kinds)
		}
	}
	if !strings.Contains(texts[1], "One line continues here.") {
		t.Fatalf("paragraph %q", texts[1])
	}
	if ch.Blocks[3].Level != 3 {
		t.Fatalf("level %d", ch.Blocks[3].Level)
	}
	if ch.Blocks[4].Text != "code   stays" {
		t.Fatalf("code %q", ch.Blocks[4].Text)
	}
	img := ch.Blocks[5]
	if img.Src == "" || img.Alt != "A plate" || img.Caption != "The plate" {
		t.Fatalf("image %+v", img)
	}
	if len(d.Images[img.Src]) == 0 {
		t.Fatalf("image bytes missing for %q", img.Src)
	}
	if d.Chapters[1].Title != "Second Chapter" {
		t.Fatalf("second title %q", d.Chapters[1].Title)
	}
	if got := d.Chapters[1].Blocks[1]; got.Kind != Image || got.Src != img.Src {
		t.Fatalf("relative image %+v", got)
	}
	if got := d.Chapters[1].Blocks[2]; got.Kind != Image || got.Src != img.Src {
		t.Fatalf("inline SVG image %+v", got)
	}
}

func TestExtractInlinePicturesAndSizes(t *testing.T) {
	data := "data:image/png;base64," + base64.StdEncoding.EncodeToString(testPNG(t))
	doc := parseHTML([]byte(`<html><body>
		<p>Before <img src="` + data + `" width="16%" style="width: 16%"> after.</p>
		<p><img src="` + data + `" width="120" height="80"></p>
		<p><img src="` + data + `" style="width: 50%"></p>
	</body></html>`))
	blocks, _ := doc.blocks("", nil, nil, nil, "", "")
	if len(blocks) != 3 {
		t.Fatalf("blocks %+v", blocks)
	}
	p := blocks[0]
	want := "Before " + string(ObjectRune) + " after."
	if p.Kind != Paragraph || p.Text != want {
		t.Fatalf("paragraph %+v", p)
	}
	if len(p.Runs) != 3 || p.Runs[1].Src == "" || p.Runs[1].WidthPercent != 16 || p.Runs[1].Text != string(ObjectRune) {
		t.Fatalf("inline runs %#v", p.Runs)
	}
	plate := blocks[1]
	if plate.Kind != Image || plate.Width != 120 || plate.Height != 80 {
		t.Fatalf("sized picture %+v", plate)
	}
	half := blocks[2]
	if half.Kind != Image || half.WidthPercent != 50 {
		t.Fatalf("half-width picture %+v", half)
	}
}

func TestExtractEPUBJoinsNavlessFiles(t *testing.T) {
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
	add := func(name, body string) { addBytes(name, []byte(body)) }
	add("mimetype", "application/epub+zip")
	add("META-INF/container.xml", `<container><rootfiles><rootfile full-path="OPS/book.opf"/></rootfiles></container>`)
	add("OPS/book.opf", `<package><metadata><dc:title>Split</dc:title></metadata><manifest>
		<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
		<item id="a" href="a.xhtml" media-type="application/xhtml+xml"/>
		<item id="b" href="b.xhtml" media-type="application/xhtml+xml"/>
		<item id="c" href="c.xhtml" media-type="application/xhtml+xml"/>
		<item id="d" href="d.xhtml" media-type="application/xhtml+xml"/>
		<item id="plate" href="img/plate.png" media-type="image/png"/>
	</manifest><spine><itemref idref="a"/><itemref idref="b"/><itemref idref="c"/><itemref idref="d"/></spine></package>`)
	add("OPS/nav.xhtml", `<html xmlns:epub="http://www.idpf.org/2007/ops"><body><nav epub:type="toc">
		<ol><li><a href="a.xhtml">First</a></li></ol></nav></body></html>`)
	add("OPS/a.xhtml", `<html><body><h1>First</h1><p>The first file.</p></body></html>`)
	// The next two files aren't named by the contents: the rest of the
	// chapter, then a plate. They belong with it.
	add("OPS/b.xhtml", `<html><body><p>The rest of the first.</p></body></html>`)
	add("OPS/c.xhtml", `<html><body><figure><img src="img/plate.png" alt="A plate"/></figure></body></html>`)
	// A heading of its own keeps a file a chapter, even unnamed.
	add("OPS/d.xhtml", `<html><body><h2>Its own</h2><p>Another chapter.</p></body></html>`)
	addBytes("OPS/img/plate.png", testPNG(t))
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	d, err := Extract(writeFile(t, "split.epub", buf.Bytes()), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Chapters) != 2 {
		t.Fatalf("chapters %d: %+v", len(d.Chapters), d.Chapters)
	}
	var texts []string
	plate := false
	for _, b := range d.Chapters[0].Blocks {
		texts = append(texts, b.Text)
		if b.Kind == Image && b.Alt == "A plate" {
			plate = true
		}
	}
	joined := strings.Join(texts, "|")
	if !strings.Contains(joined, "The first file.") || !strings.Contains(joined, "The rest of the first.") {
		t.Fatalf("first chapter %q", joined)
	}
	if !plate {
		t.Fatalf("plate missing from the first chapter: %+v", d.Chapters[0].Blocks)
	}
	if d.Chapters[1].Blocks[0].Text != "Its own" {
		t.Fatalf("second chapter %+v", d.Chapters[1])
	}
}

// TestExtractEPUBManyPictures makes sure the picture count is generous
// enough for a converted book that sets every formula's letter as its own
// picture: two hundred used to be the limit, and a maths book has more.
func TestExtractEPUBManyPictures(t *testing.T) {
	const count = 201
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
	add := func(name, body string) { addBytes(name, []byte(body)) }
	add("mimetype", "application/epub+zip")
	add("META-INF/container.xml", `<container><rootfiles>
		<rootfile full-path="OPS/book.opf" media-type="application/oebps-package+xml"/>
	</rootfiles></container>`)
	add("OPS/book.opf", `<package><metadata>
		<dc:title xmlns:dc="http://purl.org/dc/elements/1.1/">Many Pictures</dc:title>
	</metadata><manifest>
		<item id="a" href="a.xhtml" media-type="application/xhtml+xml"/>
	</manifest><spine><itemref idref="a"/></spine></package>`)
	var body strings.Builder
	body.WriteString("<html><body>")
	pic := testPNG(t)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("img/p%03d.png", i)
		body.WriteString(`<img src="` + name + `" alt="formula"/>`)
		addBytes("OPS/"+name, pic)
	}
	body.WriteString("</body></html>")
	add("OPS/a.xhtml", body.String())
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	d, err := Extract(writeFile(t, "many.epub", buf.Bytes()), "EPUB")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Images) != count {
		t.Fatalf("pictures %d, want %d", len(d.Images), count)
	}
}

func hierarchyEPUB(t *testing.T) []byte {
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
	add := func(name, body string) { addBytes(name, []byte(body)) }
	add("mimetype", "application/epub+zip")
	add("META-INF/container.xml", `<container><rootfiles>
		<rootfile full-path="OPS/book.opf" media-type="application/oebps-package+xml"/>
	</rootfiles></container>`)
	add("OPS/book.opf", `<package><metadata>
		<dc:title xmlns:dc="http://purl.org/dc/elements/1.1/">A Parted Book</dc:title>
	</metadata><manifest>
		<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
		<item id="one" href="text/one.xhtml" media-type="application/xhtml+xml"/>
	</manifest><spine><itemref idref="one"/></spine></package>`)
	add("OPS/nav.xhtml", `<html xmlns:epub="http://www.idpf.org/2007/ops"><body>
		<nav epub:type="toc"><ol>
			<li><a href="text/one.xhtml#part1">Part One</a><ol>
				<li><a href="text/one.xhtml#ch1">Chapter One</a></li>
				<li><a href="text/one.xhtml#ch2">Chapter Two</a></li>
			</ol></li>
			<li><a href="text/one.xhtml#part2">Part Two</a><ol>
				<li><a href="text/one.xhtml#ch3">Chapter Three</a></li>
			</ol></li>
		</ol></nav>
	</body></html>`)
	add("OPS/text/one.xhtml", `<html><body>
		<h1 id="part1">Part One</h1><p>Part one opens.</p>
		<h2 id="ch1">Chapter One</h2><p>One text.</p>
		<h2 id="ch2">Chapter Two</h2><p>Two text.</p>
		<h1 id="part2">Part Two</h1><p>Part two opens.</p>
		<h2 id="ch3">Chapter Three</h2><p>Three text.</p>
	</body></html>`)
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractEPUBHierarchy(t *testing.T) {
	d, err := Extract(writeFile(t, "parts.epub", hierarchyEPUB(t)), "")
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		title string
		depth int
	}{
		{"Part One", 1},
		{"Chapter One", 2},
		{"Chapter Two", 2},
		{"Part Two", 1},
		{"Chapter Three", 2},
	}
	if len(d.Chapters) != len(want) {
		t.Fatalf("chapters %d: %+v", len(d.Chapters), d.Chapters)
	}
	for i, w := range want {
		ch := d.Chapters[i]
		if ch.Title != w.title || ch.Depth != w.depth {
			t.Fatalf("chapter %d: %q depth %d, want %q depth %d", i, ch.Title, ch.Depth, w.title, w.depth)
		}
	}
	if got := chapterText(d.Chapters[1]); strings.Contains(got, "Two text.") || !strings.Contains(got, "One text.") {
		t.Fatalf("chapter one leaked: %q", got)
	}
	if got := chapterText(d.Chapters[0]); strings.Contains(got, "One text.") || !strings.Contains(got, "Part one opens.") {
		t.Fatalf("part one leaked: %q", got)
	}
	if got := chapterText(d.Chapters[4]); strings.Contains(got, "Part two opens.") || !strings.Contains(got, "Three text.") {
		t.Fatalf("chapter three leaked: %q", got)
	}
}

func chapterText(ch Chapter) string {
	var b strings.Builder
	for _, block := range ch.Blocks {
		b.WriteString(block.Text)
		b.WriteString(" ")
	}
	return b.String()
}

func TestNavigationSelfClosingScript(t *testing.T) {
	// Kobo writes its navigation document with <script .../>. HTML parsing
	// takes that as an opening tag, so the lists after it vanished.
	nav := []byte(`<html xmlns:epub="http://www.idpf.org/2007/ops"><head>
		<script xmlns="http://www.w3.org/1999/xhtml" type="text/javascript" src="../js/kobo.js"/>
	</head><body><nav epub:type="toc"><ol>
		<li><a href="one.xhtml">Chapter One</a></li>
		<li><a href="two.xhtml">Chapter Two</a></li>
	</ol></nav></body></html>`)
	entries := navEntries(nav)
	if len(entries) != 2 || entries[0].Title != "Chapter One" || entries[1].Href != "two.xhtml" {
		t.Fatalf("nav entries %+v", entries)
	}
}

func TestNavigationDepth(t *testing.T) {
	nav := []byte(`<html><body><nav epub:type="toc"><ol>
		<li><span>Part One</span><ol><li><a href="one.xhtml">Chapter One</a></li></ol></li>
	</ol></nav></body></html>`)
	entries := navEntries(nav)
	if len(entries) != 1 || entries[0].Depth != 2 || entries[0].Title != "Chapter One" {
		t.Fatalf("nav entries %+v", entries)
	}
	ncx := []byte(`<ncx><navMap>
		<navPoint><navLabel><text>Part One</text></navLabel><content src="one.xhtml"/>
			<navPoint><navLabel><text>Chapter One</text></navLabel><content src="one.xhtml#ch1"/></navPoint>
		</navPoint>
	</navMap></ncx>`)
	points := ncxEntries(ncx)
	if len(points) != 2 || points[1].Depth != 2 || points[1].Title != "Chapter One" {
		t.Fatalf("ncx entries %+v", points)
	}
}

func TestExtractEPUB3FallsBackToNCX(t *testing.T) {
	// The navigation document is broken and the spine doesn't name the
	// NCX, as some EPUB 3 books leave it: the chapters still get names.
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	add("mimetype", "application/epub+zip")
	add("META-INF/container.xml", `<container><rootfiles>
		<rootfile full-path="OPS/book.opf"/></rootfiles></container>`)
	add("OPS/book.opf", `<package><metadata>
		<dc:title xmlns:dc="http://purl.org/dc/elements/1.1/">A Fallback</dc:title>
	</metadata><manifest>
		<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
		<item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
		<item id="one" href="one.xhtml" media-type="application/xhtml+xml"/>
	</manifest><spine><itemref idref="one"/></spine></package>`)
	// nav.xhtml is named but absent, so only the NCX can name the chapters.
	add("OPS/toc.ncx", `<ncx><navMap><navPoint><navLabel><text>From the NCX</text></navLabel>
		<content src="one.xhtml"/></navPoint></navMap></ncx>`)
	add("OPS/one.xhtml", `<html><body><p>Body.</p></body></html>`)
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	d, err := Extract(writeFile(t, "fallback.epub", buf.Bytes()), "EPUB")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Chapters) != 1 || d.Chapters[0].Title != "From the NCX" {
		t.Fatalf("chapters %+v", d.Chapters)
	}
}

// testPNG is a small picture, to check that a book's plates reach the page.
func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 12, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 12; x++ {
			img.Set(x, y, color.RGBA{R: uint8(40 + x*12), G: uint8(90 + y*10), B: 170, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractTXT(t *testing.T) {
	text := "\ufeffChapter 1\r\n\r\nThe first line\r\nand its second.\r\n\r\nChapter 2\r\n\r\nAnother.\r\n"
	d, err := Extract(writeFile(t, "book.txt", []byte(text)), "TXT")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Chapters) != 2 {
		t.Fatalf("chapters %d: %+v", len(d.Chapters), d.Chapters)
	}
	if d.Chapters[0].Title != "Chapter 1" {
		t.Fatalf("title %q", d.Chapters[0].Title)
	}
	if got := d.Chapters[0].Blocks[0].Text; got != "The first line and its second." {
		t.Fatalf("text %q", got)
	}
}

func TestExtractCJK(t *testing.T) {
	text := "第一章 初见\n\n他说：\n你好。\n\n第二章 再见\n\n后来。\n"
	d, err := Extract(writeFile(t, "book.txt", []byte(text)), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Chapters) != 2 || d.Chapters[0].Title != "第一章 初见" {
		t.Fatalf("chapters %+v", d.Chapters)
	}
	if got := d.Chapters[0].Blocks[0].Text; got != "他说：你好。" {
		t.Fatalf("text %q", got)
	}
}

func TestExtractRefuses(t *testing.T) {
	if _, err := Extract(writeFile(t, "book.pdf", []byte("x")), ""); err == nil {
		t.Fatal("a PDF read")
	}
	if _, err := Extract(writeFile(t, "book.epub", []byte("not a zip")), "EPUB"); err == nil {
		t.Fatal("a broken EPUB read")
	}
	if _, err := Extract(writeFile(t, "x.epub", []byte("x")), "PDF"); err == nil {
		t.Fatal("an unsupported format read")
	}
}

func TestExtractEPUB3NavSubfolder(t *testing.T) {
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
	add("META-INF/container.xml", `<container><rootfiles><rootfile full-path="OPS/book.opf"/></rootfiles></container>`)
	add("OPS/book.opf", `<package><metadata><dc:title xmlns:dc="http://purl.org/dc/elements/1.1/">Nested</dc:title></metadata>
		<manifest>
			<item id="nav" href="nav/toc.xhtml" media-type="application/xhtml+xml" properties="nav"/>
			<item id="a" href="text/a.xhtml" media-type="application/xhtml+xml"/>
		</manifest>
		<spine><itemref idref="a"/></spine></package>`)
	add("OPS/nav/toc.xhtml", `<html><body><nav epub:type="toc"><ol>
		<li><a href="../text/a.xhtml">From the nav</a></li>
	</ol></nav></body></html>`)
	add("OPS/text/a.xhtml", `<html><body><p>Body.</p></body></html>`)
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	d, err := Extract(writeFile(t, "nested.epub", buf.Bytes()), "EPUB")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Chapters) != 1 || d.Chapters[0].Title != "From the nav" {
		t.Fatalf("chapters %+v", d.Chapters)
	}
}

func TestExtractTXTEncodings(t *testing.T) {
	text := "第一章 初见\n\n他说：你好。\n"
	gb, err := simplifiedchinese.GB18030.NewEncoder().String(text)
	if err != nil {
		t.Fatal(err)
	}
	le, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().String(text)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"gb18030.txt": gb, "utf16.txt": le} {
		d, err := Extract(writeFile(t, name, []byte(data)), "TXT")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(d.Chapters) != 1 || d.Chapters[0].Title != "第一章 初见" {
			t.Fatalf("%s: %+v", name, d.Chapters)
		}
		if got := d.Chapters[0].Blocks[0].Text; got != "他说：你好。" {
			t.Fatalf("%s: %q", name, got)
		}
	}
}

func TestExtractEPUBTablesAndNotes(t *testing.T) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	add("mimetype", "application/epub+zip")
	add("META-INF/container.xml", `<container><rootfiles><rootfile full-path="OPS/book.opf"/></rootfiles></container>`)
	add("OPS/book.opf", `<package><metadata><dc:title xmlns:dc="http://purl.org/dc/elements/1.1/">Marks</dc:title></metadata>
		<manifest>
			<item id="a" href="a.xhtml" media-type="application/xhtml+xml"/>
			<item id="notes" href="notes.xhtml" media-type="application/xhtml+xml"/>
		</manifest><spine><itemref idref="a"/></spine></package>`)
	add("OPS/a.xhtml", `<html xmlns:epub="http://www.idpf.org/2007/ops"><body>
		<h1>Measures</h1>
		<p>The hall was wide.<a epub:type="noteref" href="notes.xhtml#n1">1</a></p>
		<table><caption>Measures of the halls</caption>
			<thead><tr><th>Hall</th><th colspan="2">Size</th></tr></thead>
			<tbody><tr><td>First</td><td>12</td><td>paces</td></tr>
			<tr><td>Second</td><td>9</td><td>paces</td></tr></tbody>
		</table>
		<p>A second mark.<a href="#fn2"><sup>2</sup></a></p>
		<div class="footnotes"><ol><li id="fn2">2. The second mark was a star.
			<a href="#ref2" epub:type="backlink">↩</a></li></ol></div>
	</body></html>`)
	add("OPS/notes.xhtml", `<html xmlns:epub="http://www.idpf.org/2007/ops"><body>
		<section epub:type="endnotes"><aside id="n1" epub:type="endnote">
			<p>The hall was wide.</p><p>It held the tide.</p>
			<a href="a.xhtml#ref1">↩</a>
		</aside></section></body></html>`)
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	d, err := Extract(writeFile(t, "marks.epub", buf.Bytes()), "EPUB")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Chapters) != 1 {
		t.Fatalf("chapters %d: %+v", len(d.Chapters), d.Chapters)
	}
	ch := d.Chapters[0]
	if len(ch.Blocks) != 4 {
		t.Fatalf("blocks %+v", ch.Blocks)
	}
	if got := ch.Blocks[1].Text; !strings.Contains(got, "The hall was wide.[1]") {
		t.Fatalf("reference %q", got)
	}
	tb := ch.Blocks[2]
	if tb.Kind != TableBlock || tb.Table == nil {
		t.Fatalf("table block %+v", tb)
	}
	if tb.Table.Caption != "Measures of the halls" || len(tb.Table.Rows) != 3 {
		t.Fatalf("table %+v", tb.Table)
	}
	if got := tb.Table.Rows[0].Cells; len(got) != 2 || !got[0].Header || got[1].ColSpan != 2 {
		t.Fatalf("header cells %+v", got)
	}
	if got := tb.Table.Rows[1].Cells[0].Text; got != "First" {
		t.Fatalf("cell %q", got)
	}
	if got := ch.Blocks[3].Text; !strings.Contains(got, "A second mark.[2]") {
		t.Fatalf("second reference %q", got)
	}
	if len(ch.Notes) != 2 {
		t.Fatalf("notes %+v", ch.Notes)
	}
	if n := ch.Notes[0]; n.Label != "1" || !strings.Contains(n.Text, "It held the tide.") || strings.Contains(n.Text, "↩") {
		t.Fatalf("endnote %+v", n)
	}
	if n := ch.Notes[1]; n.Label != "2" || n.Text != "The second mark was a star." {
		t.Fatalf("footnote %+v", n)
	}
}

func TestPictureLength(t *testing.T) {
	cases := []struct {
		in     string
		w, pct float32
	}{
		{"240", 240, 0},
		{"240px", 240, 0},
		{" 24.5px ", 24.5, 0},
		{"16%", 0, 16},
		{"16.5%", 0, 16.5},
		{"12em", 0, 0},
		{"12rem", 0, 0},
		{"9pt", 0, 0},
		{"auto", 0, 0},
	}
	for _, c := range cases {
		w, pct := imageLength(c.in)
		if w != c.w || pct != c.pct {
			t.Errorf("imageLength(%q) = %v, %v; want %v, %v", c.in, w, pct, c.w, c.pct)
		}
	}
	if w, pct := styleLength("height: 10px; width: 50% !important", "width"); w != 0 || pct != 50 {
		t.Errorf("style width = %v, %v", w, pct)
	}
	if w, pct := styleLength("height: 10px; width: 320px", "width"); w != 320 || pct != 0 {
		t.Errorf("style width = %v, %v", w, pct)
	}
}
