package reader

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

// Plain text reading: chapters are the lines that look like headings, as
// "Chapter 12", "PART TWO" or "第十二章"; the rest of the text is
// paragraphs, one per blank-line-separated block of lines.

var (
	txtChapter = regexp.MustCompile(`(?i)^\s*(?:chapter|part|book|section)\s+(?:[0-9]{1,4}|[ivxlcdm]{1,10})\b`)
	txtCJK     = regexp.MustCompile(`^\s*第\s*[0-9０-９一二三四五六七八九十百千零两〇]+\s*[章节回卷篇部]`)
)

func readTXT(file string) (*Document, error) {
	st, err := os.Stat(file)
	if err != nil {
		return nil, err
	}
	if st.Size() > MaxText*2 {
		return nil, errTooBig
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	body, err := decodeTXT(data)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")

	doc := &Document{}
	var ch Chapter
	var para []string
	flushPara := func() {
		if len(para) == 0 {
			return
		}
		ch.Blocks = append(ch.Blocks, Block{Kind: Paragraph, Text: joinLines(para)})
		para = para[:0]
	}
	flushChapter := func() bool {
		flushPara()
		if len(ch.Blocks) == 0 {
			ch = Chapter{}
			return true
		}
		ok := doc.addChapter(ch)
		ch = Chapter{}
		return ok
	}
	read := 0
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if isTXTHeading(trimmed) {
			if !flushChapter() {
				doc.Truncated = true
				break
			}
			ch.Title = trimmed
			continue
		}
		if trimmed == "" {
			flushPara()
			continue
		}
		para = append(para, trimmed)
		read += len(trimmed)
		if read >= MaxText {
			doc.Truncated = true
			break
		}
	}
	if !doc.Truncated {
		if !flushChapter() {
			doc.Truncated = true
		}
	} else {
		flushChapter()
	}
	if len(doc.Chapters) == 0 {
		return nil, fmt.Errorf("txt: the file has no text to read")
	}
	return doc, nil
}

// decodeTXT reads a text file as UTF-8, UTF-16 (Notepad's Unicode) or, as
// most Chinese ebooks on the web are, GB18030.
func decodeTXT(data []byte) (string, error) {
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	switch {
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
		out, err := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewDecoder().Bytes(data[2:])
		if err != nil {
			return "", fmt.Errorf("txt: %w", err)
		}
		return string(out), nil
	case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		out, err := unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM).NewDecoder().Bytes(data[2:])
		if err != nil {
			return "", fmt.Errorf("txt: %w", err)
		}
		return string(out), nil
	}
	if utf8.Valid(data) {
		return string(data), nil
	}
	out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data)
	if err == nil && !bytes.ContainsRune(out, utf8.RuneError) {
		return string(out), nil
	}
	return "", fmt.Errorf("txt: the text is in an encoding Dalibre doesn't read")
}

func isTXTHeading(line string) bool {
	if line == "" || len([]rune(line)) > 80 {
		return false
	}
	return txtChapter.MatchString(line) || txtCJK.MatchString(line)
}

// joinLines joins a paragraph's lines: no space where the lines meet in
// Han characters or kana, as Chinese and Japanese wrap without one, and
// one space otherwise.
func joinLines(lines []string) string {
	var b strings.Builder
	for i, line := range lines {
		if i > 0 && needsSpace(lines[i-1], line) {
			b.WriteByte(' ')
		}
		b.WriteString(line)
	}
	return b.String()
}

func needsSpace(prev, next string) bool {
	pr, _ := utf8.DecodeLastRuneInString(prev)
	nr, _ := utf8.DecodeRuneInString(next)
	if isCJK(pr) || isCJK(nr) {
		return false
	}
	return true
}

func isCJK(r rune) bool {
	return r >= 0x2e80 && r <= 0x9fff || r >= 0xf900 && r <= 0xfaff || r >= 0x3040 && r <= 0x30ff || r >= 0xac00 && r <= 0xd7af
}
