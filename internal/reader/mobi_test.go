package reader

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildMOBI writes a small Palm database with one text record and one
// picture, enough to exercise the container, the contents and the shared
// HTML-to-blocks path.
func buildMOBI(t *testing.T, html string, compression, encryption uint16) []byte {
	t.Helper()
	const headerLen = 0xE4
	name := "Test Book"
	image := []byte{0xff, 0xd8, 0xff, 0xe0, 0, 0, 0, 0}

	records := [][]byte{make([]byte, 400), []byte(html), image}
	n := len(records)
	total := 78 + 8*n
	for _, rec := range records {
		total += len(rec)
	}
	data := make([]byte, total)
	copy(data[60:68], "BOOKMOBI")
	binary.BigEndian.PutUint16(data[76:78], uint16(n))

	at := 78 + 8*n
	for i, rec := range records {
		binary.BigEndian.PutUint32(data[78+8*i:82+8*i], uint32(at))
		copy(data[at:], rec)
		at += len(rec)
	}

	r0 := records[0]
	binary.BigEndian.PutUint16(r0[0:2], compression)
	binary.BigEndian.PutUint32(r0[4:8], uint32(len(html)))
	binary.BigEndian.PutUint16(r0[8:10], 1)
	binary.BigEndian.PutUint16(r0[12:14], encryption)
	copy(r0[16:20], "MOBI")
	binary.BigEndian.PutUint32(r0[20:24], headerLen)
	binary.BigEndian.PutUint32(r0[28:32], 65001)
	binary.BigEndian.PutUint32(r0[36:40], 6)
	binary.BigEndian.PutUint32(r0[84:88], 300)
	binary.BigEndian.PutUint32(r0[88:92], uint32(len(name)))
	binary.BigEndian.PutUint32(r0[108:112], 2) // record 2 is the first picture
	copy(r0[300:300+len(name)], name)
	copy(data[78+8*n:], r0)
	return data
}

func testMOBIHTML() string {
	prefix := "<html><head><guide><reference type=\"toc\" filepos=0000000000 /></guide></head><body>"
	front := "<p>front matter</p><mbp:pagebreak/>"
	tocTemplate := "<p><a filepos=0000000000>Chapter One</a></p>" +
		"<p><a filepos=0000000000>Chapter Two</a></p><mbp:pagebreak/>"
	first := len(prefix) + len(front) + len(tocTemplate)
	one := "<h1>Chapter One</h1><p>Hello <img recindex=\"00001\"> world.</p><mbp:pagebreak/>"
	second := first + len(one)
	tocStart := len(prefix) + len(front)
	prefix = strings.Replace(prefix, "0000000000", fmt.Sprintf("%010d", tocStart), 1)
	toc := fmt.Sprintf("<p><a filepos=%010d>Chapter One</a></p>"+
		"<p><a filepos=%010d>Chapter Two</a></p><mbp:pagebreak/>", first, second)
	if len(toc) != len(tocTemplate) {
		panic("contents offsets changed the page's length")
	}
	return prefix + front + toc + one + "<h1>Chapter Two</h1><p>Goodbye.</p></body></html>"
}

func TestMOBIReadsChaptersAndPictures(t *testing.T) {
	data := buildMOBI(t, testMOBIHTML(), mobiCompressionNone, 0)
	file := filepath.Join(t.TempDir(), "test.mobi")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := readMOBI(file)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Test Book" {
		t.Errorf("title = %q", doc.Title)
	}
	if len(doc.Chapters) != 3 {
		t.Fatalf("chapters = %d, want 3", len(doc.Chapters))
	}
	if doc.Chapters[0].Title != "Test Book" {
		t.Errorf("front title = %q", doc.Chapters[0].Title)
	}
	if doc.Chapters[1].Title != "Chapter One" || doc.Chapters[2].Title != "Chapter Two" {
		t.Errorf("titles = %q, %q", doc.Chapters[1].Title, doc.Chapters[2].Title)
	}
	var src string
	for _, b := range doc.Chapters[1].Blocks {
		if b.Kind == Image {
			src = b.Src
		}
		for _, r := range b.Runs {
			if r.Src != "" {
				src = r.Src
			}
		}
	}
	if src != "images/1" {
		t.Fatalf("picture src = %q", src)
	}
	if got := doc.Chapters[1].Blocks[1].Text; got != "Hello \uFFFC world." {
		t.Fatalf("text = %q", got)
	}
	if !bytes.HasPrefix(doc.Images[src], []byte{0xff, 0xd8, 0xff}) {
		t.Errorf("picture bytes = % x", doc.Images[src])
	}
}

func TestMOBIFallsBackToHeadings(t *testing.T) {
	html := "<html><body><h1>One</h1><p>first</p><h2>Two</h2><p>second</p></body></html>"
	book, err := parseMOBI(buildMOBI(t, html, mobiCompressionNone, 0))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := book.document()
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Chapters) != 2 {
		t.Fatalf("chapters = %d, want 2", len(doc.Chapters))
	}
	if doc.Chapters[0].Title != "One" || doc.Chapters[0].Depth != 1 {
		t.Errorf("first = %q at %d", doc.Chapters[0].Title, doc.Chapters[0].Depth)
	}
	if doc.Chapters[1].Title != "Two" || doc.Chapters[1].Depth != 2 {
		t.Errorf("second = %q at %d", doc.Chapters[1].Title, doc.Chapters[1].Depth)
	}
}

func TestMOBIPalmDOC(t *testing.T) {
	// "abc", a back-reference of three bytes, a space plus "a", and a
	// two-byte literal run.
	in := []byte{0x03, 'a', 'b', 'c', 0x80, 0x18, 0xe1, 0x02, 'd', 'e'}
	out, cut, err := mobiPalmDOC(in, 100)
	if err != nil {
		t.Fatal(err)
	}
	if cut || string(out) != "abcabc ade" {
		t.Errorf("out = %q, cut = %v", out, cut)
	}
	out, cut, err = mobiPalmDOC([]byte{0x03, 'a', 'b', 'c'}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !cut || string(out) != "ab" {
		t.Errorf("limited out = %q, cut = %v", out, cut)
	}
}

func TestMOBIRejectsUnsupportedBooks(t *testing.T) {
	for _, tc := range []struct {
		name        string
		compression uint16
		encryption  uint16
		want        string
	}{
		{"huff", mobiCompressionHUFF, 0, "HUFF/CDIC"},
		{"encrypted", mobiCompressionNone, 1, "encrypted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseMOBI(buildMOBI(t, "<html><body>x</body></html>", tc.compression, tc.encryption))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}
