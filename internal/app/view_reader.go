package app

import (
	"math"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"dalibre/cmd/dalibre/locales"
	"dalibre/internal/app/icons"
	"dalibre/internal/app/theme"
	"dalibre/internal/reader"
)

// The reader page draws a book over the whole window: a warm page, a wide
// measure of serif text, the chapter's name in the chrome, and the way back
// to the shelf. It is built like WeChat Reading's page, which a book should
// feel like: the text, and nothing else, until the reader asks for more.

const (
	readerHeaderH = 56
	readerFooterH = 48
	readerDrawerW = 340
	readerSheetW  = 440
)

func (a *App) readerView(c *ui.Context) {
	page := theme.ReadingPage(a.rd.theme)
	c.SetTheme(readerTheme(c.Theme(), page))
	c.SetMenuPresentation(ui.PopoverMenus)

	root := ui.Column(c).Key("reader").Fill().Background(page.Bg).TextColor(page.Text).Clip()
	root.Children(func() {
		a.readerHeader(c, page)
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			side := a.readerContentsSide()
			if a.rd.toc && side == "left" {
				a.readerContents(c, page)
			}
			ui.Column(c).Grow(1).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				switch {
				case a.rd.loading:
					a.readerLoading(c, page)
				case a.rd.err != nil:
					a.readerMessage(c, page)
				default:
					a.readerRestorePlace()
					a.readerBody(c, page)
					a.readerUpdateProgress()
					a.readerFooter(c, page)
				}
			})
			if a.rd.toc && side == "right" {
				a.readerContents(c, page)
			}
		})
	})
	if a.rd.typeSheet {
		a.readerTypeSheet(c, page)
	}
}

// readerTheme is the toolkit's theme on the reading page's colors, so the
// built-in pieces (scrollbars, menus, focus) match the page.
func readerTheme(base *ui.Theme, p theme.Reading) *ui.Theme {
	t := *base
	t.Dark = p.Dark
	t.Background, t.Surface, t.SurfaceHover, t.SurfacePressed = p.Bg, p.Chrome, p.Subtle, p.Subtle
	t.Border, t.Text, t.TextMuted = p.Border, p.Text, p.Secondary
	t.Accent, t.AccentHover, t.AccentPressed = p.Accent, p.Accent, p.Accent
	t.AccentText = p.Accent
	t.InactiveAccent, t.Selection, t.Focus = p.Accent, p.Subtle, p.Accent
	t.Inverse, t.InverseText = p.Text, p.Bg
	t.Danger = ui.Hex("#c0453b")
	t.Warning = ui.Hex("#b4802c")
	t.Success = ui.Hex("#4f7a3f")
	t.Font = theme.FontSerif
	return &t
}

// readerHeader is the top chrome: back to the library, the book and the
// chapter, and the page's controls.
func (a *App) readerHeader(c *ui.Context, p theme.Reading) {
	left := theme.Space16 + c.TitleBar().Left
	right := theme.Space16 + c.TitleBar().Right
	h := ui.Row(c).Height(readerHeaderH).Padding(0, right, 0, left).Gap(theme.Space8).
		Background(p.Chrome).BorderWidth(0, 0, 1, 0).BorderColor(p.Border).
		AlignItems(ui.Center).DragWindow()
	h.Children(func() {
		a.readerIconButton(c, p, icons.ChevronLeft, locales.ReaderBack(), false, a.readerClose)
		title := ""
		if a.rd.reading != nil {
			title = a.rd.reading.Title
		}
		if title == "" && a.rd.book != nil {
			title = a.rd.book.Title
		}
		sub := ""
		if a.rd.reading != nil && a.rd.err == nil && !a.rd.loading {
			sub = a.readerTitle(a.rd.chapter)
		}
		ui.Column(c).Grow(1).MinWidth(0).Padding(0, theme.Space8).Gap(1).Children(func() {
			ui.Text(c, title).FontSize(13).FontWeight(600).SingleLine().TextAlign(ui.Center).TextColor(p.Text)
			if sub != "" {
				ui.Text(c, sub).FontSize(11).SingleLine().TextAlign(ui.Center).TextColor(p.Tertiary)
			}
		})
		if a.rd.reading == nil || a.rd.err != nil {
			return
		}
		marked := a.readerBookmarked(a.rd.chapter)
		a.readerIconButton(c, p, icons.Bookmark, bookmarkLabel(marked), marked, a.readerToggleBookmark)
		a.readerIconButton(c, p, icons.List, locales.ReaderContents(), a.rd.toc, func() {
			a.rd.toc = true
			a.rd.tocTab = "contents"
		})
		a.readerIconButton(c, p, icons.Type, locales.ReaderTextSize(), a.rd.typeSheet, func() {
			a.rd.typeSheet = !a.rd.typeSheet
		})
	})
}

func bookmarkLabel(marked bool) string {
	if marked {
		return locales.ReaderRemoveBookmark()
	}
	return locales.ReaderAddBookmark()
}

// readerIconButton is a control in the chrome: quiet until hovered, and in
// the accent when it is on.
func (a *App) readerIconButton(c *ui.Context, p theme.Reading, icon *ui.SVG, label string, active bool, onClick func()) ui.Element {
	b := ui.ButtonBase(c).Size(34, 34).Radius(theme.RadiusFull).Center().Label(label).Tooltip(label)
	fg := p.Secondary
	switch {
	case onClick == nil:
		fg = p.Tertiary
	case active:
		b.Background(p.Subtle)
		fg = p.Accent
	case b.Hovered():
		b.Background(p.Subtle)
		fg = p.Text
	}
	if onClick != nil {
		b.OnClick(onClick)
	}
	return b.TextColor(fg).Children(func() { ui.Icon(c, icon).FontSize(17) })
}

// readerRestorePlace scrolls to the place kept for the book once the
// chapter has a height to scroll in.
func (a *App) readerRestorePlace() {
	if a.rd.pending > 0 && a.rd.scroll.MaxY > 0 {
		a.rd.scroll.Y = float32(a.rd.pending) * a.rd.scroll.MaxY
		a.rd.pending = 0
	}
}

// readerBody is the text: a column as wide as a book's page, centered.
func (a *App) readerBody(c *ui.Context, p theme.Reading) {
	ui.Scroll(c).Grow(1).MinHeight(0).TrackScroll(&a.rd.scroll).Scrollbars(false).Children(func() {
		col := ui.Column(c).WidthPercent(86).MaxWidth(theme.ReaderMeasure).MinWidth(0).
			AlignSelf(ui.Center).AlignItems(ui.Stretch).Selectable().
			Padding(theme.Space48, 0, theme.Space64, 0)
		col.Children(func() {
			ch := a.rd.chapters[a.rd.chapter]
			title := a.readerTitle(a.rd.chapter)
			if title != "" && !ch.Continued {
				ui.Text(c, title).Font(theme.FontSerif).FontSize(float32(a.rd.size+6)).FontWeight(600).
					LineHeight(1.4).TextAlign(ui.Center).Padding(0, theme.Space24, theme.Space8, theme.Space24)
			}
			labels := readerNoteLabels(ch.Notes)
			for _, b := range ch.Blocks {
				a.readerBlock(c, p, b, labels)
			}
			a.readerNotes(c, p, ch.Notes)
			a.readerEnd(c, p)
		})
	})
}

func (a *App) readerBlock(c *ui.Context, p theme.Reading, b reader.Block, labels map[string]bool) {
	size := float32(a.rd.size)
	switch b.Kind {
	case reader.Heading:
		lvl := b.Level
		if lvl < 1 || lvl > 6 {
			lvl = 2
		}
		fs := size + float32(6-lvl)
		pad := [2]float32{theme.Space24, theme.Space8}
		if lvl <= 2 {
			fs = size + 4
			pad = [2]float32{theme.Space32, theme.Space12}
		}
		ui.Text(c, b.Text).Font(theme.FontSerif).FontSize(fs).FontWeight(600).LineHeight(1.4).
			Padding(pad[0], theme.Space16, pad[1], theme.Space16)
	case reader.Quote:
		ui.Row(c).Padding(theme.Space8, 0, theme.Space16, 0).BorderWidth(0, 0, 0, 2).BorderColor(p.Accent).
			Children(func() {
				if spans, ok := readerSpans(b.Text, labels, size, p.Accent); ok {
					ui.RichText(c, spans...).Font(theme.FontSerif).FontSize(size).Italic().
						LineHeight(float32(a.rd.lineHeight)).TextColor(p.Secondary).Padding(0, theme.Space16)
					return
				}
				ui.Text(c, b.Text).Font(theme.FontSerif).FontSize(size).Italic().LineHeight(float32(a.rd.lineHeight)).
					TextColor(p.Secondary).Padding(0, theme.Space16)
			})
	case reader.Divider:
		ui.Row(c).Justify(ui.Center).Padding(theme.Space24, 0).Children(func() {
			ui.Box(c).Width(64).Height(1).Background(p.Border)
		})
	case reader.Image:
		a.readerImage(c, p, b)
	case reader.Code:
		a.readerCode(c, p, b.Text, size)
	case reader.TableBlock:
		a.readerTable(c, p, b.Table)
	default:
		text := b.Text
		if a.rd.indent {
			text = "　　" + text
		}
		if spans, ok := readerSpans(text, labels, size, p.Accent); ok {
			ui.RichText(c, spans...).Font(theme.FontSerif).FontSize(size).LineHeight(float32(a.rd.lineHeight)).
				Padding(0, theme.Space16, theme.Space12, theme.Space16)
			return
		}
		ui.Text(c, text).Font(theme.FontSerif).FontSize(size).LineHeight(float32(a.rd.lineHeight)).
			Padding(0, theme.Space16, theme.Space12, theme.Space16)
	}
}

// readerCode sets source code in a monospaced block: the lines and the
// spaces that indent them stay as the book wrote them, and a long line
// scrolls sideways rather than folding into the next.
func (a *App) readerCode(c *ui.Context, p theme.Reading, text string, size float32) {
	fs := size - 2
	if fs < 12 {
		fs = 12
	}
	ui.Column(c).Padding(0, theme.Space16, theme.Space12, theme.Space16).Children(func() {
		ui.ScrollHorizontal(c).WidthPercent(100).MinWidth(0).Border(1, p.Border).
			Radius(theme.RadiusSM).Background(p.Subtle).Children(func() {
			ui.Text(c, text).Font(theme.FontMono).FontSize(fs).LineHeight(1.55).
				PreserveSpaces().NoWrap().TextColor(p.Text).Padding(theme.Space12, theme.Space16)
		})
	})
}

// readerNoteLabels is the set of markers the chapter's notes answer.
func readerNoteLabels(notes []reader.Note) map[string]bool {
	if len(notes) == 0 {
		return nil
	}
	labels := make(map[string]bool, len(notes))
	for i, n := range notes {
		label := n.Label
		if label == "" {
			label = strconv.Itoa(i + 1)
		}
		labels[label] = true
	}
	return labels
}

// readerSpans splits a paragraph at [label] markers, so a note's marker
// reads as an aside in the accent, not as punctuation. Text without one
// is left to a plain Text, which is faster to lay out.
func readerSpans(text string, labels map[string]bool, size float32, accent ui.Color) ([]ui.Span, bool) {
	if len(labels) == 0 || !strings.ContainsRune(text, '[') {
		return nil, false
	}
	var spans []ui.Span
	rest := text
	found := false
	for {
		i := strings.IndexByte(rest, '[')
		if i < 0 {
			break
		}
		j := strings.IndexByte(rest[i+1:], ']')
		if j < 0 {
			break
		}
		j += i + 1
		label := rest[i+1 : j]
		if !labels[label] {
			// A bracket that isn't a note stays text; look on after it.
			spans = append(spans, ui.Span{Text: rest[:j+1]})
			rest = rest[j+1:]
			continue
		}
		if i > 0 {
			spans = append(spans, ui.Span{Text: rest[:i]})
		}
		spans = append(spans, ui.Span{Text: "[" + label + "]", Size: size - 3, Weight: 500, Color: accent})
		rest = rest[j+1:]
		found = true
	}
	if !found {
		return nil, false
	}
	if rest != "" {
		spans = append(spans, ui.Span{Text: rest})
	}
	return spans, true
}

// readerTable sets a table at the measure: a caption over it, a quiet
// grid, heading cells tinted, and a sideways scroll when the book has
// more columns than the page can hold. The rows keep the proportion of
// the columns they span; a rowspan cell is set where it starts.
func (a *App) readerTable(c *ui.Context, p theme.Reading, t *reader.Table) {
	if t == nil || (len(t.Rows) == 0 && t.Caption == "") {
		return
	}
	cols := 1
	for _, row := range t.Rows {
		n := 0
		for _, cell := range row.Cells {
			n += clampTableSpan(cell.ColSpan)
		}
		if n > cols {
			cols = n
		}
	}
	size := float32(a.rd.size - 1)
	if size < 12 {
		size = 12
	}
	draw := func() {
		ui.Column(c).WidthPercent(100).MinWidth(0).Gap(theme.Space6).Children(func() {
			if t.Caption != "" {
				ui.Text(c, t.Caption).Font(theme.FontSerif).FontSize(11).Italic().LineHeight(1.4).
					TextAlign(ui.Center).TextColor(p.Tertiary).Padding(0, theme.Space8)
			}
			ui.Column(c).WidthPercent(100).MinWidth(0).Border(1, p.Border).Radius(theme.RadiusSM).Clip().Children(func() {
				for ri, row := range t.Rows {
					used := 0
					r := ui.Row(c).AlignItems(ui.Stretch).BorderWidth(0, 0, 1, 0).BorderColor(p.Border)
					if ri == len(t.Rows)-1 {
						r.BorderWidth(0, 0, 0, 0)
					}
					r.Children(func() {
						for _, cell := range row.Cells {
							span := clampTableSpan(cell.ColSpan)
							used += span
							box := ui.Column(c).Grow(float32(span)).MinWidth(0).
								Padding(theme.Space6, theme.Space10, theme.Space8, theme.Space10).
								BorderWidth(0, 1, 0, 0).BorderColor(p.Border)
							if cell.Header {
								box.Background(p.Subtle)
							}
							box.Children(func() {
								text := cell.Text
								ui.Text(c, text).Font(theme.FontSerif).FontSize(size).LineHeight(1.45).
									TextColor(p.Text)
							})
						}
						if used < cols {
							ui.Box(c).Grow(float32(cols - used))
						}
					})
				}
			})
		})
	}
	if cols > 4 {
		ui.ScrollHorizontal(c).WidthPercent(100).Children(func() {
			ui.Column(c).WidthPercent(100).MinWidth(float32(cols * 132)).Children(draw)
		})
		return
	}
	draw()
}

func clampTableSpan(n int) int {
	if n < 1 {
		return 1
	}
	if n > 32 {
		return 32
	}
	return n
}

// readerNotes sets the chapter's footnotes and endnotes after its text,
// smaller than the body and marked with the same [label] as the text.
func (a *App) readerNotes(c *ui.Context, p theme.Reading, notes []reader.Note) {
	if len(notes) == 0 {
		return
	}
	size := float32(a.rd.size - 2)
	if size < 12 {
		size = 12
	}
	ui.Column(c).Padding(theme.Space40, theme.Space16, 0, theme.Space16).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Padding(0, 0, theme.Space12, 0).Children(func() {
			ui.Box(c).Width(24).Height(1).Background(p.Border)
			ui.Text(c, locales.ReaderNotes()).FontSize(11).FontWeight(600).LetterSpacing(0.6).TextColor(p.Tertiary)
		})
		for i, n := range notes {
			label := n.Label
			if label == "" {
				label = strconv.Itoa(i + 1)
			}
			ui.Row(c).AlignItems(ui.Start).Gap(theme.Space8).Padding(0, 0, theme.Space10, 0).Children(func() {
				ui.Text(c, "["+label+"]").Font(theme.FontMono).FontSize(11).TextColor(p.Accent).
					MinWidth(28).Padding(2, 0, 0, 0)
				ui.Text(c, n.Text).Font(theme.FontSerif).FontSize(size).LineHeight(1.6).
					TextColor(p.Secondary).Grow(1).MinWidth(0)
			})
		}
	})
}

// readerImage sets a picture on the page at the size it came, and no
// wider than the measure, with the book's caption under it. A small
// picture, as a converted book sets a formula's letters, stays small
// instead of being blown up and blurred; a plate scales down to fit. A
// picture the book names but the page couldn't decode is left out; the
// text doesn't stop for it.
func (a *App) readerImage(c *ui.Context, p theme.Reading, b reader.Block) {
	src := a.rd.images[b.Src]
	if src == nil {
		return
	}
	ui.Column(c).AlignItems(ui.Center).Padding(theme.Space16, theme.Space16, theme.Space8, theme.Space16).Children(func() {
		img := ui.Image(c, src).MaxWidthPercent(100).MaxWidth(theme.ReaderMeasure).Fit(ui.Contain).Radius(2)
		if b.Alt != "" {
			img.Label(b.Alt)
		}
		if b.Caption != "" {
			ui.Text(c, b.Caption).Font(theme.FontSerif).FontSize(12).Italic().
				LineHeight(1.5).TextAlign(ui.Center).TextColor(p.Tertiary).
				MaxWidth(theme.ReaderMeasure).Padding(0, theme.Space24, 0, theme.Space24)
		}
	})
}

// readerEnd closes the chapter: the next one's name, or the book's end.
func (a *App) readerEnd(c *ui.Context, p theme.Reading) {
	last := a.rd.chapter >= len(a.rd.chapters)-1
	ui.Column(c).AlignItems(ui.Center).Gap(theme.Space8).Padding(theme.Space48, theme.Space16, 0, theme.Space16).Children(func() {
		if a.rd.reading != nil && a.rd.reading.Truncated && last {
			ui.Text(c, locales.ReaderTruncated()).FontSize(12).TextColor(p.Tertiary).TextAlign(ui.Center).MaxWidth(360)
		}
		if last {
			ui.Text(c, locales.ReaderTheEnd()).Font(theme.FontSerif).FontSize(15).TextColor(p.Secondary)
			b := ui.ButtonBase(c).Height(32).Radius(theme.RadiusSM).Padding(0, theme.Space12).Label(locales.ReaderBack()).
				Border(1, p.Border).Background(p.Chrome)
			if b.Hovered() {
				b.Background(p.Subtle)
			}
			b.OnClick(a.readerClose).Children(func() {
				ui.Text(c, locales.ReaderBack()).FontSize(12).FontWeight(500).TextColor(p.Text)
			})
			return
		}
		next := a.rd.chapter + 1
		ui.Text(c, locales.ReaderEndOfChapter()).FontSize(11).LetterSpacing(0.6).TextColor(p.Tertiary)
		nextTitle := a.readerTitle(next)
		if nextTitle == "" {
			nextTitle = locales.ReaderChapterN(next + 1)
		}
		row := ui.ButtonBase(c).Gap(theme.Space8).AlignItems(ui.Center).Padding(theme.Space8, theme.Space12).
			Radius(theme.RadiusSM).Label(nextTitle).OnClick(func() { a.readerChapterTo(next) })
		if row.Hovered() {
			row.Background(p.Subtle)
		}
		row.Children(func() {
			ui.Text(c, nextTitle).Font(theme.FontSerif).FontSize(16).FontWeight(600).TextColor(p.Accent)
			ui.Icon(c, icons.ArrowRight).FontSize(16).TextColor(p.Accent)
		})
	})
}

// readerFooter is the bottom chrome: the book's progress and the way to the
// next chapter.
func (a *App) readerFooter(c *ui.Context, p theme.Reading) {
	ui.Column(c).AlignItems(ui.Stretch).Background(p.Chrome).BorderWidth(1, 0, 0, 0).BorderColor(p.Border).Children(func() {
		ui.Row(c).Height(3).Background(p.Border).Children(func() {
			w := float32(math.Max(0, math.Min(1, a.rd.percent))) * 100
			ui.Box(c).Height(3).WidthPercent(w).Background(p.Accent)
		})
		ui.Row(c).Height(readerFooterH).Padding(0, theme.Space20).Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.ReaderProgress(int(math.Round(a.rd.percent*100)))).Font(theme.FontMono).
				FontSize(12).TextColor(p.Secondary)
			ui.Text(c, locales.ReaderChapterOf(a.rd.chapter+1, len(a.rd.chapters))).Grow(1).
				FontSize(12).TextAlign(ui.Center).TextColor(p.Tertiary)
			var prev, next func()
			if a.rd.chapter > 0 {
				prev = func() { a.readerGo(-1) }
			}
			if a.rd.chapter < len(a.rd.chapters)-1 {
				next = func() { a.readerGo(1) }
			}
			a.readerIconButton(c, p, icons.ChevronLeft, locales.ReaderPrevChapter(), false, prev)
			a.readerIconButton(c, p, icons.ChevronRight, locales.ReaderNextChapter(), false, next)
		})
	})
}

func (a *App) readerLoading(c *ui.Context, p theme.Reading) {
	ui.Column(c).Grow(1).Center().Gap(theme.Space12).Children(func() {
		ui.Spinner(c).Size(20, 20).Label(locales.ReaderLoading())
		ui.Text(c, locales.ReaderLoading()).FontSize(12).TextColor(p.Tertiary)
	})
}

func (a *App) readerMessage(c *ui.Context, p theme.Reading) {
	ui.Column(c).Grow(1).Center().Gap(theme.Space12).Padding(theme.Space32).Children(func() {
		ui.Icon(c, icons.Alert).FontSize(22).TextColor(p.Tertiary)
		ui.Text(c, locales.ReaderFailed()).Font(theme.FontSerif).FontSize(17).FontWeight(600)
		if a.rd.err != nil {
			ui.Text(c, a.rd.err.Error()).FontSize(12).TextColor(p.Secondary).TextAlign(ui.Center).
				MaxWidth(460).Selectable()
		}
		ui.Row(c).Gap(theme.Space8).Padding(theme.Space8, 0, 0, 0).Children(func() {
			a.readerPlainButton(c, p, locales.ReaderRetry(), true, func() {
				if a.rd.book != nil {
					a.openReader(a.rd.book, a.rd.format)
				}
			})
			a.readerPlainButton(c, p, locales.ReaderBack(), false, a.readerClose)
		})
	})
}

func (a *App) readerPlainButton(c *ui.Context, p theme.Reading, label string, primary bool, onClick func()) ui.Element {
	b := ui.ButtonBase(c).Height(30).Radius(theme.RadiusSM).Padding(0, theme.Space12).Label(label).OnClick(onClick)
	fg := p.Text
	if primary {
		b.Background(p.Accent)
		fg = p.Chrome
	} else {
		b.Border(1, p.Border).Background(p.Chrome)
		if b.Hovered() {
			b.Background(p.Subtle)
		}
	}
	return b.Children(func() { ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(fg) })
}

// readerContents is the panel of chapters, or of the places marked. It
// takes its own column beside the page rather than covering the text: the
// reading measure narrows, so the contents stay a part of the book. The
// reader can dock it left or right, with the header's buttons or by
// dragging its inner edge to the other side.
func (a *App) readerContents(c *ui.Context, p theme.Reading) {
	side := a.readerContentsSide()
	panel := ui.Column(c).Width(readerDrawerW).MinWidth(0).HeightPercent(100).
		Background(p.Chrome).AlignItems(ui.Stretch)
	if side == "left" {
		panel.BorderWidth(0, 1, 0, 0)
	} else {
		panel.BorderWidth(0, 0, 0, 1)
	}
	panel.BorderColor(p.Border).Children(func() {
		ui.Row(c).Height(48).Padding(0, theme.Space8, 0, theme.Space16).Gap(theme.Space4).
			AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.ReaderContents()).FontSize(14).FontWeight(600).Grow(1)
			a.readerIconButton(c, p, icons.PanelLeft, locales.ReaderDockLeft(), side == "left",
				func() { a.readerSetContentsSide("left") })
			a.readerIconButton(c, p, icons.PanelRight, locales.ReaderDockRight(), side == "right",
				func() { a.readerSetContentsSide("right") })
			pinned := a.readerContentsPinned()
			pinLabel := locales.ReaderPinContents()
			if pinned {
				pinLabel = locales.ReaderUnpinContents()
			}
			a.readerIconButton(c, p, icons.Pin, pinLabel, pinned,
				func() { a.readerSetContentsPinned(!pinned) })
			a.readerIconButton(c, p, icons.X, locales.ReaderClose(), false, func() { a.rd.toc = false })
		})
		ui.Row(c).Padding(0, theme.Space12, theme.Space8, theme.Space12).Gap(theme.Space6).Children(func() {
			a.readerTab(c, p, locales.ReaderContents(), a.rd.tocTab == "contents", func() { a.rd.tocTab = "contents" })
			a.readerTab(c, p, locales.ReaderBookmarks(), a.rd.tocTab == "bookmarks", func() { a.rd.tocTab = "bookmarks" })
		})
		ui.Box(c).Height(1).Background(p.Border)
		if a.rd.tocTab == "bookmarks" {
			a.readerBookmarkList(c, p)
		} else {
			a.readerChapterList(c, p)
		}
		a.readerContentsGrip(c, p, side)
	})
}

// readerContentsGrip is the panel's inner edge: a drag toward the other
// side docks it there, as a sheet snaps to an edge. A press that doesn't
// move changes nothing.
func (a *App) readerContentsGrip(c *ui.Context, p theme.Reading, side string) {
	g := ui.Row(c).Absolute().Top(0).Bottom(0).Width(8).Center().Cursor(ui.CursorResizeEW).
		Label(locales.ReaderContents())
	if side == "left" {
		g.Right(0)
	} else {
		g.Left(0)
	}
	dx, _, held := g.Dragged()
	if held {
		a.rd.tocDrag += dx
	}
	if _, _, ended := g.DragEnded(); ended {
		const threshold = 48
		if a.rd.tocDrag >= threshold && side == "left" {
			a.readerSetContentsSide("right")
		} else if a.rd.tocDrag <= -threshold && side == "right" {
			a.readerSetContentsSide("left")
		}
		a.rd.tocDrag = 0
	}
	if g.PressCanceled() {
		a.rd.tocDrag = 0
	}
	g.Children(func() {
		grip := ui.Box(c).Width(3).Height(40).Radius(theme.RadiusFull).Background(p.Border)
		if g.Hovered() {
			grip.Background(p.Accent)
		}
	})
}

func (a *App) readerTab(c *ui.Context, p theme.Reading, label string, active bool, onClick func()) ui.Element {
	b := ui.ButtonBase(c).Height(28).Radius(theme.RadiusFull).Padding(0, theme.Space12).Label(label).OnClick(onClick)
	fg := p.Secondary
	if active {
		b.Background(p.Subtle)
		fg = p.Accent
	} else if b.Hovered() {
		b.Background(p.Subtle)
	}
	return b.Children(func() { ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(fg) })
}

func (a *App) readerChapterList(c *ui.Context, p theme.Reading) {
	entries := a.tocEntries()
	ui.List(c, &a.rd.tocList, len(entries), func(i int) {
		idx := entries[i]
		title := a.readerTitle(idx)
		if title == "" {
			title = locales.ReaderChapterN(idx + 1)
		}
		depth := a.readerDepth(idx)
		lead := theme.Space16 + float32(depth-1)*12
		size, weight, fg := float32(14), 500, p.Text
		if depth == 1 {
			size, weight = 15, 600
		} else if depth > 2 {
			size, weight, fg = 13, 400, p.Secondary
		}
		row := ui.ButtonBase(c).Padding(theme.Space10, theme.Space16, theme.Space10, lead).Gap(theme.Space8).
			AlignItems(ui.Center).Label(title).OnClick(func() { a.readerChapterTo(idx) })
		if idx == a.rd.chapter {
			row.Background(p.Subtle)
		} else if row.Hovered() {
			row.Background(p.Subtle)
		}
		row.Children(func() {
			ui.Text(c, title).Font(theme.FontSerif).FontSize(size).FontWeight(weight).TextColor(fg).
				MaxLines(2).Grow(1)
			if idx == a.rd.chapter {
				ui.Box(c).Width(3).Height(16).Radius(theme.RadiusFull).Background(p.Accent)
			}
		})
	}).Grow(1)
}

func (a *App) readerBookmarkList(c *ui.Context, p theme.Reading) {
	marks := a.rd.bookmarks
	ui.List(c, &a.rd.marksList, len(marks), func(i int) {
		idx := marks[i]
		title := a.readerTitle(idx)
		if title == "" {
			title = locales.ReaderChapterN(idx + 1)
		}
		row := ui.ButtonBase(c).Padding(theme.Space10, theme.Space16).Gap(theme.Space10).AlignItems(ui.Center).
			Label(title).OnClick(func() { a.readerChapterTo(idx) })
		if row.Hovered() {
			row.Background(p.Subtle)
		}
		row.Children(func() {
			ui.Icon(c, icons.Bookmark).FontSize(14).TextColor(p.Accent)
			ui.Text(c, title).Font(theme.FontSerif).FontSize(14).MaxLines(2).Grow(1)
		})
	}).Grow(1).Children(func() {
		if len(marks) == 0 {
			ui.Text(c, locales.ReaderNoBookmarks()).FontSize(12).TextColor(p.Tertiary).
				Padding(theme.Space20, theme.Space16).TextAlign(ui.Start)
		}
	})
}

// readerTypeSheet sets the page: its color, the text's size and leading.
func (a *App) readerTypeSheet(c *ui.Context, p theme.Reading) {
	ui.Row(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Children(func() {
		scrim := ui.ButtonBase(c).Fill().Background(ui.RGBA(20, 16, 10, 0.24)).Label(locales.ReaderClose())
		scrim.OnClick(func() { a.rd.typeSheet = false })
		ui.Column(c).Attach(ui.AnchorBottom, ui.AnchorBottom).Bottom(theme.Space24).Width(readerSheetW).
			MaxWidthPercent(92).Radius(theme.RadiusXL).Background(p.Chrome).Border(1, p.Border).
			Shadow(0, 24, 48, -8, p.Shadow).Padding(theme.Space20, theme.Space20).
			Gap(theme.Space20).AlignItems(ui.Stretch).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
				ui.Text(c, locales.ReaderTheme()).FontSize(12).FontWeight(600).TextColor(p.Secondary).Grow(1)
				ui.Row(c).Gap(theme.Space8).Children(func() {
					for _, name := range theme.ReadingThemes {
						a.readerSwatch(c, p, name)
					}
				})
			})
			a.readerStepRow(c, p, locales.ReaderTextSize(), strconv.Itoa(a.rd.size), func() { a.readerSetSize(-1) }, func() { a.readerSetSize(1) })
			a.readerStepRow(c, p, locales.ReaderLineSpacing(), strconv.FormatFloat(a.rd.lineHeight, 'f', -1, 64),
				func() { a.readerSetLineHeight(-0.1) }, func() { a.readerSetLineHeight(0.1) })
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, themeName(a.rd.theme)).FontSize(12).TextColor(p.Tertiary).Grow(1)
				ui.Textf(c, "%d%%", int(math.Round(a.rd.lineHeight*100))).Font(theme.FontMono).FontSize(11).TextColor(p.Tertiary)
			})
		})
	})
}

func (a *App) readerSwatch(c *ui.Context, p theme.Reading, name string) ui.Element {
	pg := theme.ReadingPage(name)
	b := ui.ButtonBase(c).Size(30, 30).Radius(theme.RadiusFull).Background(pg.Bg).
		Border(1, pg.Border).Label(themeName(name)).Tooltip(themeName(name)).OnClick(func() { a.readerSetTheme(name) })
	if name == a.rd.theme {
		b.Border(2, pg.Accent)
	}
	return b.Children(func() {
		if name == a.rd.theme {
			fg := pg.Text
			if pg.Dark {
				fg = pg.Accent
			}
			ui.Icon(c, icons.Check).FontSize(14).TextColor(fg)
		}
	})
}

func (a *App) readerStepRow(c *ui.Context, p theme.Reading, label, value string, less, more func()) {
	ui.Row(c).AlignItems(ui.Center).Gap(theme.Space12).Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(p.Secondary).Grow(1)
		a.readerStep(c, p, "−", label, less)
		ui.Text(c, value).Font(theme.FontMono).FontSize(13).Width(44).TextAlign(ui.Center).TextColor(p.Text)
		a.readerStep(c, p, "+", label, more)
	})
}

func (a *App) readerStep(c *ui.Context, p theme.Reading, mark, label string, onClick func()) ui.Element {
	b := ui.ButtonBase(c).Size(30, 30).Radius(theme.RadiusSM).Border(1, p.Border).Center().
		Label(label).OnClick(onClick)
	if b.Hovered() {
		b.Background(p.Subtle)
	}
	return b.Children(func() { ui.Text(c, mark).FontSize(15).TextColor(p.Text) })
}

func themeName(name string) string {
	switch name {
	case "sepia":
		return locales.ReaderThemeSepia()
	case "green":
		return locales.ReaderThemeGreen()
	case "night":
		return locales.ReaderThemeNight()
	default:
		return locales.ReaderThemePaper()
	}
}

// readerShortcuts are the page's keys: Escape leaves, B marks the chapter,
// T shows the contents, and + and - set the text's size.
func (a *App) readerShortcuts(c *ui.Context) {
	c.OnWindowShortcut(0, ui.KeyEscape, func() {
		switch {
		case a.rd.typeSheet:
			a.rd.typeSheet = false
		case a.rd.toc:
			a.rd.toc = false
		default:
			a.readerClose()
		}
	})
	if a.rd.reading == nil || a.rd.err != nil {
		return
	}
	if c.FocusTakesText() {
		return
	}
	c.OnWindowShortcut(0, ui.KeyB, a.readerToggleBookmark)
	c.OnWindowShortcut(0, ui.KeyT, func() {
		if a.rd.toc {
			a.rd.toc = false
			return
		}
		a.rd.toc = true
		a.rd.tocTab = "contents"
	})
	c.OnWindowShortcut(0, ui.KeyEqual, func() { a.readerSetSize(1) })
	c.OnWindowShortcut(0, ui.KeyMinus, func() { a.readerSetSize(-1) })
}
