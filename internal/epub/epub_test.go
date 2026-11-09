package epub

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const opf = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
<manifest>
  <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
  <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
  <item id="t" href="Text/title.xhtml" media-type="application/xhtml+xml"/>
  <item id="c1" href="Text/chapter%201.xhtml" media-type="application/xhtml+xml"/>
  <item id="c2" href="Text/chapter2.xhtml" media-type="application/xhtml+xml"/>
  <item id="css" href="Styles/style.css" media-type="text/css"/>
</manifest>
<spine toc="ncx"><itemref idref="t"/><itemref idref="c1"/><itemref idref="c2"/></spine>
</package>`

const nav = `<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body>
<nav epub:type="toc"><ol><li><a href="Text/chapter%201.xhtml">Chapter <b>One</b></a></li></ol></nav></body></html>`

const ncx = `<ncx><navMap>
<navPoint><navLabel><text>From the NCX</text></navLabel><content src="Text/chapter%201.xhtml"/></navPoint>
<navPoint><navLabel><text>Second</text></navLabel><content src="Text/chapter2.xhtml#start"/></navPoint>
</navMap></ncx>`

func page(title, body string) string {
	return `<?xml version="1.0" encoding="utf-8"?><html xmlns="http://www.w3.org/1999/xhtml"><head><title>` + title +
		`</title></head><body>` + body + `</body></html>`
}

func writeBook(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "Book.epub")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, data string, method uint16) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, data)
	}
	add("mimetype", "application/epub+zip", zip.Store)
	add("META-INF/container.xml", `<container><rootfiles><rootfile full-path="OEBPS/content.opf"/></rootfiles></container>`, zip.Deflate)
	add("OEBPS/content.opf", opf, zip.Deflate)
	add("OEBPS/nav.xhtml", nav, zip.Deflate)
	add("OEBPS/toc.ncx", ncx, zip.Deflate)
	add("OEBPS/Text/title.xhtml", page("Title Page", "<h1>Pride and Prejudice</h1>"), zip.Deflate)
	add("OEBPS/Text/chapter 1.xhtml", page("x", "<p>It is a truth universally acknowledged.</p>"), zip.Deflate)
	add("OEBPS/Text/chapter2.xhtml", page("x", "<p>三体 is two words and one.</p>"), zip.Deflate)
	add("OEBPS/Styles/style.css", "p { margin: 0 }", zip.Deflate)
	zw.Close()
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOpen(t *testing.T) {
	b, err := Open(writeBook(t))
	if err != nil {
		t.Fatal(err)
	}
	sp := b.Spine()
	want := []Item{
		{Name: "OEBPS/Text/title.xhtml", Title: "Pride and Prejudice", Words: 3},
		{Name: "OEBPS/Text/chapter 1.xhtml", Title: "Chapter One", Words: 6},
		{Name: "OEBPS/Text/chapter2.xhtml", Title: "Second", Words: 7},
	}
	if len(sp) != len(want) {
		t.Fatalf("spine = %+v", sp)
	}
	for i := range want {
		if sp[i] != want[i] {
			t.Errorf("spine[%d] = %+v, want %+v", i, sp[i], want[i])
		}
	}
	if got := b.Type("OEBPS/Styles/style.css"); got != "text/css" {
		t.Errorf("css type = %q", got)
	}
	if got := len(b.Files()); got != 9 {
		t.Errorf("%d files", got)
	}
}

func TestSave(t *testing.T) {
	p := writeBook(t)
	b, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Set("OEBPS/Text/chapter2.xhtml", []byte(page("x", "<p>Edited here.</p>"))); err != nil {
		t.Fatal(err)
	}
	if !b.Changed() {
		t.Fatal("not changed")
	}
	if b.Spine()[2].Words != 2 {
		t.Errorf("words after edit = %d", b.Spine()[2].Words)
	}
	size, err := b.Save()
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Size() != size {
		t.Errorf("size %d, file %d", size, fi.Size())
	}
	zr, err := zip.OpenReader(p)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if f := zr.File[0]; f.Name != "mimetype" || f.Method != zip.Store {
		t.Errorf("first entry %q method %d", f.Name, f.Method)
	}
	for _, f := range zr.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		switch f.Name {
		case "OEBPS/Text/chapter2.xhtml":
			if !strings.Contains(string(data), "Edited here.") {
				t.Errorf("chapter2 = %s", data)
			}
		case "OEBPS/Styles/style.css":
			if string(data) != "p { margin: 0 }" {
				t.Errorf("css = %s", data)
			}
		}
	}
	// Saving again after a further edit still works from the new file.
	b.Set("OEBPS/Styles/style.css", []byte("p { margin: 1em }"))
	if _, err := b.Save(); err != nil {
		t.Fatal(err)
	}
	b2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if css, _ := b2.Read("OEBPS/Styles/style.css"); string(css) != "p { margin: 1em }" {
		t.Errorf("css after second save = %s", css)
	}
	if ch, _ := b2.Read("OEBPS/Text/chapter2.xhtml"); !strings.Contains(string(ch), "Edited here.") {
		t.Errorf("chapter2 lost its edit: %s", ch)
	}
	if _, err := os.Stat(p + ".dalibre-part"); !os.IsNotExist(err) {
		t.Error("temporary file left behind")
	}
}

func TestText(t *testing.T) {
	p := filepath.Join(t.TempDir(), "Notes.txt")
	os.WriteFile(p, []byte("one two three"), 0o644)
	b, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if sp := b.Spine(); len(sp) != 1 || sp[0].Words != 3 || sp[0].Title != "Notes" {
		t.Fatalf("spine = %+v", sp)
	}
	b.Set("Notes.txt", []byte("four"))
	if _, err := b.Save(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p); string(data) != "four" {
		t.Errorf("file = %q", data)
	}
}

func TestNotEPUB(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.epub")
	os.WriteFile(p, []byte("not a zip"), 0o644)
	if _, err := Open(p); err == nil {
		t.Fatal("opened a non-zip")
	}
}

func TestResolve(t *testing.T) {
	for _, c := range [][3]string{
		{"OEBPS/Styles", "../Text/ch1.xhtml#p3", "OEBPS/Text/ch1.xhtml"},
		{".", "OEBPS/a%20b.xhtml", "OEBPS/a b.xhtml"},
		{"OEBPS", "#top", ""},
	} {
		if got := Resolve(c[0], c[1]); got != c[2] {
			t.Errorf("Resolve(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}
