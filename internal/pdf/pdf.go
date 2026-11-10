// Package pdf turns a PDF's pages into pictures.
//
// A PDF is a page, not a stream of text: its words, plates and margins are
// placed in points. Rendering each page keeps that placement instead of
// guessing at a reading order, and it is the only way to show a scanned
// book, which has no text to extract. PDFium, the engine Chrome uses, runs
// here as WebAssembly, so the build stays free of cgo and one binary serves
// every platform.
package pdf

import (
	"bytes"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
)

const (
	// RenderWidth is the page's width in pixels. The reader shows a page at
	// 720 DIPs, so twice that keeps small type sharp on a retina display
	// without making every page a large bitmap.
	RenderWidth = 1440

	// maxPageSide bounds a page whose shape is extreme; a page is fitted
	// into RenderWidth and this height, never cropped.
	maxPageSide = 12_000

	// MaxFileBytes bounds the PDF held in memory while it is open.
	MaxFileBytes = 512 << 20

	// MaxPages bounds the contents; a reader cannot use more page entries
	// than it can turn to.
	MaxPages = 5000

	pagePrefix = "pdf:"

	// jpegQuality keeps a page's text readable while a rendered page stays
	// around a few hundred kilobytes.
	jpegQuality = 86
)

// ErrClosed is a render asked of a book that was already closed.
var ErrClosed = errors.New("the PDF is closed")

// PageSrc is the block Src of page i (0-based).
func PageSrc(i int) string { return pagePrefix + strconv.Itoa(i) }

// IsPageSrc says whether src names a rendered page.
func IsPageSrc(src string) bool { return strings.HasPrefix(src, pagePrefix) }

// Book is an open PDF: its bytes, its document handle and its page count.
// Renders are serialized because the PDFium instance is shared and a
// document's handle is only safe to use from one call at a time.
type Book struct {
	inst  pdfium.Pdfium
	doc   references.FPDF_DOCUMENT
	data  []byte
	pages int

	mu     sync.Mutex
	closed bool
}

// engine is one PDFium instance for the life of the app. Starting it takes
// a second or two, and a second instance would only take memory; a book is
// opened and closed on this one.
var (
	engineOnce sync.Once
	enginePool pdfium.Pool
	engineInst pdfium.Pdfium
	engineErr  error
)

func engine() (pdfium.Pdfium, error) {
	engineOnce.Do(func() {
		enginePool, engineErr = webassembly.Init(webassembly.Config{
			MinIdle:  1,
			MaxIdle:  1,
			MaxTotal: 1,
		})
		if engineErr != nil {
			engineErr = fmt.Errorf("start the PDF engine: %w", engineErr)
			return
		}
		engineInst, engineErr = enginePool.GetInstance(60 * time.Second)
		if engineErr != nil {
			engineErr = fmt.Errorf("start the PDF engine: %w", engineErr)
		}
	})
	return engineInst, engineErr
}

// Open reads the PDF at path and counts its pages.
func Open(path string) (*Book, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() > MaxFileBytes {
		return nil, fmt.Errorf("the PDF is larger than %d MB", MaxFileBytes>>20)
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > MaxFileBytes {
		return nil, fmt.Errorf("the PDF is larger than %d MB", MaxFileBytes>>20)
	}
	inst, err := engine()
	if err != nil {
		return nil, err
	}
	doc, err := inst.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		return nil, fmt.Errorf("read the PDF: %w", err)
	}
	count, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		_, _ = inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})
		return nil, fmt.Errorf("count the PDF's pages: %w", err)
	}
	if count.PageCount < 1 {
		_, _ = inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})
		return nil, errors.New("the PDF has no pages")
	}
	if count.PageCount > MaxPages {
		_, _ = inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})
		return nil, fmt.Errorf("the PDF has %d pages; the reader opens at most %d", count.PageCount, MaxPages)
	}
	return &Book{inst: inst, doc: doc.Document, data: data, pages: count.PageCount}, nil
}

// Pages is how many pages the book has.
func (b *Book) Pages() int { return b.pages }

// Render draws page i (0-based) as a JPEG and returns its bytes. The
// picture is only valid until it is encoded; PDFium's bitmap is released
// before this returns.
func (b *Book) Render(i int) ([]byte, error) {
	if b == nil {
		return nil, ErrClosed
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrClosed
	}
	if i < 0 || i >= b.pages {
		return nil, fmt.Errorf("page %d is outside the PDF's %d pages", i+1, b.pages)
	}
	rendered, err := b.inst.RenderPageInPixels(&requests.RenderPageInPixels{
		Page: requests.Page{ByIndex: &requests.PageByIndex{Document: b.doc, Index: i}},
		// Annotations are the book's own marks, notes and links; they are
		// part of the page a reader sees in any PDF viewer.
		RenderFlags: enums.FPDF_RENDER_FLAG_ANNOT,
		Width:       RenderWidth,
		Height:      maxPageSide,
	})
	if err != nil {
		return nil, fmt.Errorf("render page %d: %w", i+1, err)
	}
	defer rendered.Cleanup()
	img := rendered.Result.RenderedImage
	if img == nil {
		return nil, fmt.Errorf("render page %d: the engine returned no picture", i+1)
	}
	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 ||
		bounds.Dx() > maxPageSide || bounds.Dy() > maxPageSide {
		return nil, fmt.Errorf("render page %d: the page is too large", i+1)
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, fmt.Errorf("encode page %d: %w", i+1, err)
	}
	return out.Bytes(), nil
}

// Close releases the document. A render already running finishes first, so
// a turn that raced the close is not left reading freed memory.
func (b *Book) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	_, err := b.inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: b.doc})
	b.data = nil
	return err
}
