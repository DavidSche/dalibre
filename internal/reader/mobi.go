package reader

// MOBI reading. A MOBI file is a Palm database: a record table holds the
// header, the text split over numbered records, and the pictures. The
// text is HTML, so this file only unwraps the container and says where
// its parts are; htmlDoc turns the markup into blocks, the same way it
// does for an EPUB. AZW3 is the same container with a newer text format;
// the pieces it needs are kept apart here rather than spread through the
// reading page.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/charmap"
)

const (
	mobiCompressionNone    = 1
	mobiCompressionPalmDOC = 2
	mobiCompressionHUFF    = 17480

	mobiEncodingCP1252 = 1252

	// maxMOBIBytes keeps a malformed or enormous database from being read
	// into memory whole. The text and pictures have their own, tighter,
	// limits; this is the last guard before os.ReadFile.
	maxMOBIBytes = 512 << 20

	// maxMOBITextBytes is MaxText in bytes at UTF-8's widest. The text is
	// cut there before decoding, so a compressed record can't expand past
	// what the reader would ever show.
	maxMOBITextBytes = MaxText * 4

	mobiBase = "mobi.xhtml"
)

// mobiRecord is one record's place in the database.
type mobiRecord struct {
	start, end int
}

// mobiBook is a MOBI's text and pictures, before the text is turned into
// chapters. It keeps the database because pictures are read lazily, by
// the name a block gives them.
var _ htmlSource = (*mobiBook)(nil)

type mobiBook struct {
	data          []byte
	records       []mobiRecord
	text          []byte
	title         string
	language      string
	firstImage    int
	extraFlags    uint16
	tocStart      int
	tocEnd        int
	hasTOC        bool
	textTruncated bool
}

// readMOBI reads a MOBI file's text, pictures and chapters.
func readMOBI(file string) (*Document, error) {
	st, err := os.Stat(file)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxMOBIBytes {
		return nil, errTooBig
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	b, err := parseMOBI(data)
	if err != nil {
		return nil, err
	}
	return b.document()
}

// parseMOBI reads the database's header and text. It does not parse the
// markup: that is htmlDoc's job, once the chapters know their boundaries.
func parseMOBI(data []byte) (*mobiBook, error) {
	records, err := mobiRecords(data)
	if err != nil {
		return nil, err
	}
	r0 := data[records[0].start:records[0].end]
	if len(r0) < 132 || string(r0[16:20]) != "MOBI" {
		return nil, errors.New("mobi: no MOBI header")
	}
	compression := binary.BigEndian.Uint16(r0[0:2])
	switch compression {
	case mobiCompressionNone, mobiCompressionPalmDOC:
	case mobiCompressionHUFF:
		return nil, errors.New("mobi: HUFF/CDIC compression isn't supported yet")
	default:
		return nil, fmt.Errorf("mobi: unknown compression %d", compression)
	}
	if crypto := binary.BigEndian.Uint16(r0[12:14]); crypto != 0 {
		return nil, errors.New("mobi: this book is encrypted")
	}
	headerLen := int(binary.BigEndian.Uint32(r0[20:24]))
	version := int(binary.BigEndian.Uint32(r0[36:40]))
	if headerLen < 24 || headerLen > len(r0)-16 {
		return nil, errors.New("mobi: the MOBI header is malformed")
	}
	encoding := binary.BigEndian.Uint32(r0[28:32])
	textRecords := int(binary.BigEndian.Uint16(r0[8:10]))
	if textRecords < 1 || textRecords+1 > len(records) {
		return nil, errors.New("mobi: the book has no text records")
	}
	textLength := int(binary.BigEndian.Uint32(r0[4:8]))
	firstImage := int(binary.BigEndian.Uint32(r0[108:112]))
	if firstImage < 1 || firstImage >= len(records) {
		firstImage = -1
	}
	var extraFlags uint16
	if headerLen >= 0xE4 && version >= 5 && len(r0) >= 0xF4 {
		extraFlags = binary.BigEndian.Uint16(r0[0xF2:0xF4])
	}

	b := &mobiBook{
		data: data, records: records,
		firstImage: firstImage, extraFlags: extraFlags,
	}
	b.title, b.language = mobiMetadata(r0, headerLen, encoding)
	var text bytes.Buffer
	truncated := false
	for i := 1; i <= textRecords; i++ {
		rec := data[records[i].start:records[i].end]
		rec = mobiTrimTrailing(rec, extraFlags)
		if compression == mobiCompressionPalmDOC {
			out, cut, err := mobiPalmDOC(rec, maxMOBITextBytes-text.Len())
			if err != nil {
				return nil, fmt.Errorf("mobi: text record %d: %w", i, err)
			}
			rec = out
			truncated = truncated || cut
		}
		if text.Len()+len(rec) > maxMOBITextBytes {
			rec = rec[:maxMOBITextBytes-text.Len()]
			truncated = true
		}
		text.Write(rec)
		if truncated {
			break
		}
	}
	body := text.Bytes()
	if textLength > 0 && textLength < len(body) {
		body = body[:textLength]
	}
	if textLength > maxMOBITextBytes {
		truncated = true
	}
	body = mobiText(body, encoding)
	b.text = body
	b.textTruncated = truncated

	if start, end, ok := mobiTOCBounds(body); ok {
		b.tocStart, b.tocEnd, b.hasTOC = start, end, true
	}
	return b, nil
}

// mobiRecords reads the Palm database's record table. A record's end is
// the next record's start; the last ends at the file's end.
func mobiRecords(data []byte) ([]mobiRecord, error) {
	if len(data) < 78 || string(data[60:68]) != "BOOKMOBI" {
		return nil, errors.New("mobi: not a MOBI file")
	}
	n := int(binary.BigEndian.Uint16(data[76:78]))
	if n < 2 {
		return nil, errors.New("mobi: the book has no records")
	}
	if max := (len(data) - 78) / 8; n > max {
		n = max
	}
	if n < 2 {
		return nil, errors.New("mobi: the record table is truncated")
	}
	out := make([]mobiRecord, n)
	for i := 0; i < n; i++ {
		start := int(binary.BigEndian.Uint32(data[78+8*i:]))
		end := len(data)
		if i+1 < n {
			end = int(binary.BigEndian.Uint32(data[78+8*(i+1):]))
		}
		if start < 0 || start > len(data) || end < start || end > len(data) {
			out[i] = mobiRecord{}
			continue
		}
		out[i] = mobiRecord{start: start, end: end}
	}
	if out[0].end <= out[0].start {
		return nil, errors.New("mobi: the first record is missing")
	}
	return out, nil
}

// mobiText makes the text valid UTF-8 for the HTML parser. MOBI's two
// encodings are UTF-8 and Windows-1252; an unknown byte becomes the
// replacement character instead of failing the book.
func mobiText(data []byte, encoding uint32) []byte {
	if encoding == mobiEncodingCP1252 {
		if out, err := charmap.Windows1252.NewDecoder().Bytes(data); err == nil {
			return out
		}
	}
	return bytes.ToValidUTF8(data, []byte("\uFFFD"))
}

// mobiMetadata reads the book's name from the MOBI header and its title
// and language from EXTH, when it has them.
func mobiMetadata(r0 []byte, headerLen int, encoding uint32) (title, language string) {
	if len(r0) >= 92 {
		off := int(binary.BigEndian.Uint32(r0[84:88]))
		n := int(binary.BigEndian.Uint32(r0[88:92]))
		if off > 0 && off+n <= len(r0) {
			title = mobiDecode(r0[off:off+n], encoding)
		}
	}
	if len(r0) < 132 || binary.BigEndian.Uint32(r0[128:132])&0x40 == 0 {
		return collapse(title, false), ""
	}
	pos := 16 + headerLen
	if pos < 0 || pos+12 > len(r0) || string(r0[pos:pos+4]) != "EXTH" {
		return collapse(title, false), ""
	}
	count := int(binary.BigEndian.Uint32(r0[pos+8 : pos+12]))
	pos += 12
	for i := 0; i < count && pos+8 <= len(r0); i++ {
		typ := binary.BigEndian.Uint32(r0[pos:])
		size := int(binary.BigEndian.Uint32(r0[pos+4:]))
		if size < 8 || pos+size > len(r0) {
			break
		}
		val := r0[pos+8 : pos+size]
		switch typ {
		case 503:
			if s := collapse(mobiDecode(val, encoding), false); s != "" {
				title = s
			}
		case 524:
			language = collapse(mobiDecode(val, encoding), false)
		}
		pos += size
	}
	return collapse(title, false), language
}

// mobiDecode reads text in the encoding the header names. MOBI files are
// either UTF-8 or Windows-1252; a broken byte becomes the replacement
// character rather than failing the whole book.
func mobiDecode(data []byte, encoding uint32) string {
	if encoding == mobiEncodingCP1252 {
		if out, err := charmap.Windows1252.NewDecoder().Bytes(data); err == nil {
			return string(out)
		}
	}
	return strings.ToValidUTF8(string(data), "\uFFFD")
}

// mobiTrimTrailing removes the size-prefixed entries a MOBI text record
// carries after its text. The flags are a bit set: bit 0 is the
// multibyte trailer, and each bit above it is another trailer.
func mobiTrimTrailing(data []byte, flags uint16) []byte {
	trailers := 0
	for f := flags; f > 1; f >>= 1 {
		if f&2 != 0 {
			trailers++
		}
	}
	for i := 0; i < trailers && len(data) > 0; i++ {
		n := mobiTrailingSize(data)
		if n <= 0 || n > len(data) {
			return data
		}
		data = data[:len(data)-n]
	}
	if flags&1 != 0 && len(data) > 0 {
		n := int(data[len(data)-1]&3) + 1
		if n <= len(data) {
			data = data[:len(data)-n]
		}
	}
	return data
}

// mobiTrailingSize reads a trailer's own length from the last four bytes,
// as MOBI writes it: seven bits per byte, most significant last.
func mobiTrailingSize(data []byte) int {
	start := len(data) - 4
	if start < 0 {
		start = 0
	}
	n := 0
	for _, v := range data[start:] {
		if v&0x80 != 0 {
			n = 0
		}
		n = n<<7 | int(v&0x7f)
	}
	return n
}

// mobiPalmDOC expands one PalmDOC record. A record is a byte stream of
// literals, short literal runs, space-plus-character pairs, and
// back-references of three to ten bytes. limit is how much the book still
// has room for; cut says the record was longer than that.
func mobiPalmDOC(data []byte, limit int) (out []byte, cut bool, err error) {
	if limit < 0 {
		limit = 0
	}
	out = make([]byte, 0, min(len(data)*2, limit))
	appendByte := func(v byte) bool {
		if len(out) >= limit {
			return false
		}
		out = append(out, v)
		return true
	}
	for i := 0; i < len(data); {
		c := data[i]
		i++
		switch {
		case c == 0 || (c >= 0x09 && c <= 0x7f):
			if !appendByte(c) {
				return out, true, nil
			}
		case c >= 0x01 && c <= 0x08:
			n := int(c)
			if i+n > len(data) {
				return nil, false, errors.New("a literal run runs past the record")
			}
			if limit-len(out) < n {
				out = append(out, data[i:i+max(limit-len(out), 0)]...)
				return out, true, nil
			}
			out = append(out, data[i:i+n]...)
			i += n
		case c >= 0xc0:
			if !appendByte(' ') || !appendByte(c^0x80) {
				return out, true, nil
			}
		default:
			if i >= len(data) {
				return nil, false, errors.New("a back-reference is cut short")
			}
			pair := uint16(c)<<8 | uint16(data[i])
			i++
			dist := int(pair>>3) & 0x7ff
			n := int(pair&7) + 3
			if dist == 0 || dist > len(out) {
				return nil, false, errors.New("a back-reference points outside the record")
			}
			for j := 0; j < n; j++ {
				if !appendByte(out[len(out)-dist]) {
					return out, true, nil
				}
			}
		}
	}
	return out, false, nil
}

// mobiImageName is the name a normalized <img> gives a picture record.
// The number is the recindex the book uses, one-based.
func mobiImageName(index int) string {
	return "images/" + strconv.Itoa(index)
}

// mobiImageIndex reads a recindex, or a kindle:embed: reference. The
// former is decimal; the latter is usually decimal too, but a few books
// write it in hex.
func mobiImageIndex(s string) int {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(strings.ToLower(s), "kindle:embed:"); i >= 0 {
		s = s[i+len("kindle:embed:"):]
	} else if i := strings.LastIndex(strings.ToLower(s), "embed:"); i >= 0 {
		s = s[i+len("embed:"):]
	}
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	if n, err := strconv.ParseInt(s, 16, 32); err == nil && n > 0 {
		return int(n)
	}
	return 0
}

// mobiImage reports whether a record is a picture Dalibre can hand to the
// page. Other records in the image range are indexes and metadata.
func mobiImage(data []byte) bool {
	return bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}) ||
		bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) ||
		bytes.HasPrefix(data, []byte("GIF87a")) ||
		bytes.HasPrefix(data, []byte("GIF89a"))
}

// read returns a resource the HTML names. Only pictures are resources in
// a MOBI; the text is already in memory. The bytes are copied so the
// picture doesn't keep the whole database alive.
func (b *mobiBook) read(name string, max int64) ([]byte, error) {
	name = path.Clean(strings.TrimSpace(name))
	index, ok := strings.CutPrefix(name, "images/")
	if !ok || b.firstImage < 1 {
		return nil, os.ErrNotExist
	}
	n, err := strconv.Atoi(index)
	if err != nil || n < 1 {
		return nil, os.ErrNotExist
	}
	i := b.firstImage + n - 1
	if i < 1 || i >= len(b.records) {
		return nil, os.ErrNotExist
	}
	r := b.records[i]
	if r.end <= r.start || r.start < 0 || r.end > len(b.data) {
		return nil, os.ErrNotExist
	}
	if int64(r.end-r.start) > max {
		return nil, errTooBig
	}
	data := b.data[r.start:r.end]
	if !mobiImage(data) {
		return nil, os.ErrNotExist
	}
	return bytes.Clone(data), nil
}
