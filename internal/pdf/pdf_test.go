package pdf

import (
	"bytes"
	"errors"
	"image"
	"path/filepath"
	"testing"
)

func TestPageSources(t *testing.T) {
	if got := PageSrc(7); got != "pdf:7" {
		t.Fatalf("PageSrc(7) = %q", got)
	}
	if !IsPageSrc(PageSrc(0)) {
		t.Fatal("a page source is not recognized")
	}
	if IsPageSrc("image.png") {
		t.Fatal("a picture was mistaken for a page")
	}
}

func TestRendersAPage(t *testing.T) {
	if testing.Short() {
		t.Skip("the PDF engine takes a second to start")
	}
	book, err := Open(filepath.Join("testdata", "one-page.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	if book.Pages() != 1 {
		t.Fatalf("pages = %d, want 1", book.Pages())
	}
	data, err := book.Render(0)
	if err != nil {
		t.Fatal(err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if format != "jpeg" {
		t.Fatalf("format = %q, want jpeg", format)
	}
	if cfg.Width != RenderWidth {
		t.Fatalf("width = %d, want %d", cfg.Width, RenderWidth)
	}
	if cfg.Height <= 0 || cfg.Height > maxPageSide {
		t.Fatalf("height = %d", cfg.Height)
	}
	if _, err := book.Render(1); err == nil {
		t.Fatal("a page past the end rendered")
	}
}

func TestClosedBookDoesNotRender(t *testing.T) {
	if testing.Short() {
		t.Skip("the PDF engine takes a second to start")
	}
	book, err := Open(filepath.Join("testdata", "one-page.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Close(); err != nil {
		t.Fatal(err)
	}
	if err := book.Close(); err != nil {
		t.Fatalf("closing twice: %v", err)
	}
	if _, err := book.Render(0); !errors.Is(err, ErrClosed) {
		t.Fatalf("render after close = %v, want ErrClosed", err)
	}
}
