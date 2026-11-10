package theme

import "github.com/egoist/mygo/ui"

// Reading is a page of the reader: a background, inks and the chrome over
// them. The reader doesn't follow the app's light or dark theme: the page
// keeps its own, as WeChat Reading's does, so a night reader stays night
// in a light app.
type Reading struct {
	Dark bool

	// Bg is the page; Chrome the bars and sheets over it.
	Bg, Chrome ui.Color
	// Text, Secondary and Tertiary are body text and the lines around it.
	Text, Secondary, Tertiary ui.Color
	// Border separates the chrome from the page; Subtle fills a chosen row.
	Border, Subtle ui.Color
	// Accent marks the place read, the current chapter and controls.
	Accent ui.Color
	// Shadow is under the sheets.
	Shadow ui.Color
}

var readingThemes = map[string]Reading{
	// Paper: warm white, as a printed page.
	"paper": {
		Bg: ui.Hex("#faf7f1"), Chrome: ui.Hex("#fdfbf6"),
		Text: ui.Hex("#26231d"), Secondary: ui.Hex("#6d675c"), Tertiary: ui.Hex("#989186"),
		Border: ui.Hex("#e8e2d6"), Subtle: ui.Hex("#f1ece1"),
		Accent: ui.Hex("#b4552d"),
		Shadow: ui.RGBA(62, 50, 32, 0.14),
	},
	// Sepia: the tea-stained page.
	"sepia": {
		Bg: ui.Hex("#f0e4cc"), Chrome: ui.Hex("#f5ecd8"),
		Text: ui.Hex("#3b2f1e"), Secondary: ui.Hex("#74644c"), Tertiary: ui.Hex("#9b8a6f"),
		Border: ui.Hex("#ddcfb2"), Subtle: ui.Hex("#e8dcc3"),
		Accent: ui.Hex("#a4552f"),
		Shadow: ui.RGBA(60, 46, 24, 0.16),
	},
	// Green: the eye-comfort page.
	"green": {
		Bg: ui.Hex("#e7efe2"), Chrome: ui.Hex("#edf3e9"),
		Text: ui.Hex("#26301f"), Secondary: ui.Hex("#5f6b54"), Tertiary: ui.Hex("#8d977f"),
		Border: ui.Hex("#d3dfca"), Subtle: ui.Hex("#dfe9d8"),
		Accent: ui.Hex("#4f7a3f"),
		Shadow: ui.RGBA(34, 52, 26, 0.16),
	},
	// Night: not black, so it doesn't glare in the dark; gray-blue ink.
	"night": {
		Dark: true,
		Bg:   ui.Hex("#15161a"), Chrome: ui.Hex("#1b1d22"),
		Text: ui.Hex("#b9b6ae"), Secondary: ui.Hex("#8b8880"), Tertiary: ui.Hex("#66645d"),
		Border: ui.Hex("#2b2d33"), Subtle: ui.Hex("#23252b"),
		Accent: ui.Hex("#c98a63"),
		Shadow: ui.RGBA(0, 0, 0, 0.5),
	},
}

// ReaderStrongWeight is how much heavier the page sets a book's strong
// text: 600, the usual semibold, not the 700 of a heading.
const ReaderStrongWeight = 600

// ReaderMeasure is the width of the reader's text column: about 35 Han
// characters at the default size, as a printed page sets them, and narrow
// enough that the eye finds the next line without losing its place.
const ReaderMeasure = 720

// ReadingThemes are the pages the reader offers, in the order it shows them.
var ReadingThemes = []string{"paper", "sepia", "green", "night"}

// ReadingPage returns the page called name; paper when it isn't one.
func ReadingPage(name string) Reading {
	if p, ok := readingThemes[name]; ok {
		return p
	}
	return readingThemes["paper"]
}

// IsReadingTheme reports whether name is a page the reader offers.
func IsReadingTheme(name string) bool {
	_, ok := readingThemes[name]
	return ok
}
