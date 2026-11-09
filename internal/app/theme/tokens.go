// Package theme holds Dalibre's design tokens, mirroring the variables of
// the Figma file: Radix Colors primitives, semantic colors aliased to the
// same step in light and dark, a 4px spacing scale, radii, sizes and the
// type families.
package theme

import "github.com/egoist/mygo/ui"

type scale [13]ui.Color // index 1–12, as Radix numbers its steps

func mk(hexes ...string) scale {
	var s scale
	for i, h := range hexes {
		s[i+1] = ui.Hex(h)
	}
	return s
}

// Radix Colors scales (MIT): https://www.radix-ui.com/colors
var (
	sand       = mk("#fdfdfc", "#f9f9f8", "#f1f0ef", "#e9e8e6", "#e2e1de", "#dad9d6", "#cfceca", "#bcbbb5", "#8d8d86", "#82827c", "#63635e", "#21201c")
	sandDark   = mk("#111110", "#191918", "#222221", "#2a2a28", "#31312e", "#3b3a37", "#494844", "#62605b", "#6f6d66", "#7c7b74", "#b5b3ad", "#eeeeec")
	indigo     = mk("#fdfdfe", "#f7f9ff", "#edf2fe", "#e1e9ff", "#d2deff", "#c1d0ff", "#abbdf9", "#8da4ef", "#3e63dd", "#3358d4", "#3a5bc7", "#1f2d5c")
	indigoDark = mk("#11131f", "#141726", "#182449", "#1d2e62", "#253974", "#304384", "#3a4f97", "#435db1", "#3e63dd", "#5472e4", "#9eb1ff", "#d6e1ff")
	green      = mk("#fbfefc", "#f4fbf6", "#e6f6eb", "#d6f1df", "#c4e8d1", "#adddc0", "#8eceaa", "#5bb98b", "#30a46c", "#2b9a66", "#218358", "#193b2d")
	greenDark  = mk("#0e1512", "#121b17", "#132d21", "#113b29", "#174933", "#20573e", "#28684a", "#2f7c57", "#30a46c", "#33b074", "#3dd68c", "#b1f1cb")
	red        = mk("#fffcfc", "#fff7f7", "#feebec", "#ffdbdc", "#ffcdce", "#fdbdbe", "#f4a9aa", "#eb8e90", "#e5484d", "#dc3e42", "#ce2c31", "#641723")
	redDark    = mk("#191111", "#201314", "#3b1219", "#500f1c", "#611623", "#72232d", "#8c333a", "#b54548", "#e5484d", "#ec5d5e", "#ff9592", "#ffd1d9")
	amber      = mk("#fefdfb", "#fefbe9", "#fff7c2", "#ffee9c", "#fbe577", "#f3d673", "#e9c162", "#e2a336", "#ffc53d", "#ffba18", "#ab6400", "#4f3422")
	amberDark  = mk("#16120c", "#1d180f", "#302008", "#3f2700", "#4d3000", "#5c3d05", "#714f19", "#8f6424", "#ffc53d", "#ffd60a", "#ffca16", "#ffe7b3")
	violet     = mk("#fdfcfe", "#faf8ff", "#f4f0fe", "#ebe4ff", "#e1d9ff", "#d4cafe", "#c2b5f5", "#aa99ec", "#6e56cf", "#654dc4", "#6550b9", "#2f265f")
	violetDark = mk("#14121f", "#1b1525", "#291f43", "#33255b", "#3c2e69", "#473876", "#56468b", "#6958ad", "#6e56cf", "#7d66d9", "#baa7ff", "#e2ddfe")
)

// Pair is a foreground and a background, as a format badge's.
type Pair struct{ FG, BG ui.Color }

// Palette is the semantic color layer (Figma collection "Color").
type Palette struct {
	Dark bool

	BgApp, BgSidebar, Surface, SurfaceRaised, Subtle, Hover, Pressed, Selected, Inverse, Scrim ui.Color
	BorderSubtle, Border, BorderStrong, Focus, BorderSelected                                  ui.Color
	Text, TextSecondary, TextTertiary, TextDisabled, TextAccent, TextOnAccent, TextOnInverse   ui.Color
	Icon, IconMuted                                                                            ui.Color
	Accent, AccentHover, AccentSubtle                                                          ui.Color
	Success, Warning, Danger                                                                   Pair
	DangerSolid, WarningSolid                                                                  ui.Color
	// Chrome (bg/chrome) is a dialog's frame around its content: its
	// footer and section sidebar, one step below SurfaceRaised in both
	// modes.
	Chrome ui.Color
	// The grid's shelf boards (shelf/*): lit top, front face, bottom lip.
	PlankTop, PlankFace, PlankEdge ui.Color
	Formats                        map[string]Pair
}

// NewPalette returns the light or the dark palette.
func NewPalette(dark bool) *Palette {
	s, in, g, r, a, v := sand, indigo, green, red, amber, violet
	if dark {
		s, in, g, r, a, v = sandDark, indigoDark, greenDark, redDark, amberDark, violetDark
	}
	white := ui.Hex("#ffffff")
	p := &Palette{
		Dark:  dark,
		BgApp: s[2], BgSidebar: s[3], Chrome: s[2], Surface: white, SurfaceRaised: white,
		Subtle: s[3], Hover: s[4], Pressed: s[5], Selected: in[3], Inverse: s[12], Scrim: ui.RGBA(0, 0, 0, 0.32),
		BorderSubtle: s[5], Border: s[6], BorderStrong: s[8], Focus: in[8], BorderSelected: in[9],
		Text: s[12], TextSecondary: s[11], TextTertiary: s[9], TextDisabled: s[8], TextAccent: in[11],
		TextOnAccent: white, TextOnInverse: s[1],
		Icon: s[11], IconMuted: s[9],
		Accent: in[9], AccentHover: in[10], AccentSubtle: in[4],
		Success: Pair{g[11], g[3]}, Warning: Pair{a[11], a[3]}, Danger: Pair{r[11], r[3]},
		DangerSolid: r[9], WarningSolid: a[9],
		PlankTop: white, PlankFace: s[5], PlankEdge: s[7],
		Formats: map[string]Pair{
			"EPUB": {g[11], g[3]}, "PDF": {r[11], r[3]}, "MOBI": {a[11], a[3]}, "AZW3": {v[11], v[3]},
		},
	}
	if dark {
		p.BgApp, p.BgSidebar, p.Surface, p.SurfaceRaised = s[1], s[2], s[2], s[3]
		p.Scrim = ui.RGBA(0, 0, 0, 0.6)
		p.PlankTop, p.PlankFace, p.PlankEdge = s[7], s[4], s[2]
	}
	return p
}

// Format returns a format's badge colors, gray for the uncommon ones.
func (p *Palette) Format(f string) Pair {
	if c, ok := p.Formats[f]; ok {
		return c
	}
	return Pair{p.TextSecondary, p.Subtle}
}

var (
	light, dark = NewPalette(false), NewPalette(true)
	current     = light
)

// Use makes the light or dark palette current for the frame being built;
// the view calls it first, on the main thread.
func Use(isDark bool) *Palette {
	current = light
	if isDark {
		current = dark
	}
	return current
}

// P is the palette of the frame being built.
func P() *Palette { return current }

// Spacing (Figma collection "Spacing"): the only values for padding and gaps.
const (
	Space2  = 2
	Space4  = 4
	Space6  = 6
	Space8  = 8
	Space10 = 10
	Space12 = 12
	Space16 = 16
	Space20 = 20
	Space24 = 24
	Space32 = 32
	Space40 = 40
	Space48 = 48
	Space64 = 64
)

// Radius (Figma collection "Radius").
const (
	RadiusXS   = 4
	RadiusSM   = 6
	RadiusMD   = 8
	RadiusLG   = 12
	RadiusXL   = 16
	RadiusFull = 999
)

// Size (Figma collection "Size").
const (
	ControlS      = 28
	ControlM      = 32
	SidebarWidth  = 240
	DetailWidth   = 340
	ToolbarHeight = 64
	CoverW        = 132
	CoverH        = 198
	CardW         = 156
)

// Type families (Figma text styles): sans throughout. Each names a Chinese
// sans after the bundled Latin face, so Han characters are drawn in a sans
// that suits Inter (YaHei on Windows, PingFang on macOS, Noto/Source Han
// elsewhere) rather than whatever the system falls back to, often a serif
// such as SimSun or Songti.
const (
	cjkSans  = "Microsoft YaHei UI, Microsoft YaHei, PingFang SC, Hiragino Sans GB, Noto Sans CJK SC, Source Han Sans SC, Noto Sans SC"
	FontUI   = "Inter, " + cjkSans
	FontMono = "Geist Mono, " + cjkSans
)

// Theme builds the toolkit's theme from the palette, so the built-in
// widgets (inputs, menus, tables, dialogs) match the design.
func (p *Palette) Theme(base *ui.Theme) *ui.Theme {
	t := *base
	t.Dark = p.Dark
	t.Background, t.Surface, t.SurfaceHover, t.SurfacePressed = p.BgApp, p.Surface, p.Hover, p.Pressed
	t.Border, t.Text, t.TextMuted = p.Border, p.Text, p.TextSecondary
	t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = p.Accent, p.AccentHover, p.AccentHover, p.TextOnAccent
	t.InactiveAccent = p.Accent
	t.Danger, t.Warning, t.Success = p.DangerSolid, p.WarningSolid, p.Success.FG
	t.Selection = p.AccentSubtle
	t.Focus = p.Focus
	t.Inverse, t.InverseText = p.Inverse, p.TextOnInverse
	t.Radius = RadiusSM
	t.Spacing = 4
	t.FontSize = 13
	t.Font = FontUI
	return &t
}
