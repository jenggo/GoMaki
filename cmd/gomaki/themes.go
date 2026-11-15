package main

import (
	"slices"

	"github.com/egoist/mygo/ui"
)

// palette is the color set of one app theme. The rest of the theme comes
// from mygo's light or dark base.
type palette struct {
	background string
	surface    string
	border     string
	text       string
	muted      string
	accent     string
	accentText string
	dark       bool
}

const (
	themeSystem = "System"
	colorWhite  = "#ffffff"
	colorPaper  = "#fdf6e3"
)

var themePalettes = map[string]palette{
	"One Dark": {
		dark: true, background: "#282c34", surface: "#21252b", border: "#3e4451",
		text: "#abb2bf", muted: "#7f848e", accent: "#61afef", accentText: "#1e2227",
	},
	"One Light": {
		background: "#fafafa", surface: "#f0f0f0", border: "#d0d0d0",
		text: "#383a42", muted: "#696c77", accent: "#4078f2", accentText: colorWhite,
	},
	"GitHub Dark": {
		dark: true, background: "#0d1117", surface: "#161b22", border: "#30363d",
		text: "#e6edf3", muted: "#8b949e", accent: "#2f81f7", accentText: colorWhite,
	},
	"GitHub Light": {
		background: colorWhite, surface: "#f6f8fa", border: "#d0d7de",
		text: "#1f2328", muted: "#656d76", accent: "#0969da", accentText: colorWhite,
	},
	"Dracula": {
		dark: true, background: "#282a36", surface: "#21222c", border: "#44475a",
		text: "#f8f8f2", muted: "#7a86b8", accent: "#bd93f9", accentText: "#282a36",
	},
	"Nord": {
		dark: true, background: "#2e3440", surface: "#3b4252", border: "#4c566a",
		text: "#eceff4", muted: "#a3aab8", accent: "#88c0d0", accentText: "#2e3440",
	},
	"Gruvbox Dark": {
		dark: true, background: "#282828", surface: "#3c3836", border: "#504945",
		text: "#ebdbb2", muted: "#a89984", accent: "#fabd2f", accentText: "#282828",
	},
	"Gruvbox Light": {
		background: "#fbf1c7", surface: "#ebdbb2", border: "#d5c4a1",
		text: "#3c3836", muted: "#7c6f64", accent: "#b57614", accentText: "#fbf1c7",
	},
	"Tokyo Night": {
		dark: true, background: "#1a1b26", surface: "#16161e", border: "#292e42",
		text: "#c0caf5", muted: "#737aa2", accent: "#7aa2f7", accentText: "#1a1b26",
	},
	"Solarized Dark": {
		dark: true, background: "#002b36", surface: "#073642", border: "#586e75",
		text: "#93a1a1", muted: "#839496", accent: "#268bd2", accentText: colorPaper,
	},
	"Solarized Light": {
		background: colorPaper, surface: "#eee8d5", border: "#93a1a1",
		text: "#586e75", muted: "#839496", accent: "#268bd2", accentText: colorPaper,
	},
	"Monokai": {
		dark: true, background: "#272822", surface: "#1e1f1c", border: "#49483e",
		text: "#f8f8f2", muted: "#a5a193", accent: "#a6e22e", accentText: "#272822",
	},
	"Catppuccin Mocha": {
		dark: true, background: "#1e1e2e", surface: "#181825", border: "#313244",
		text: "#cdd6f4", muted: "#a6adc8", accent: "#89b4fa", accentText: "#1e1e2e",
	},
	"Catppuccin Latte": {
		background: "#eff1f5", surface: "#e6e9ef", border: "#ccd0da",
		text: "#4c4f69", muted: "#7c7f93", accent: "#1e66f5", accentText: "#eff1f5",
	},
}

// themeNames lists the picker's choices: follow the desktop, then the dark
// themes, then the light ones.
var themeNames = []string{
	themeSystem,
	"One Dark", "GitHub Dark", "Dracula", "Nord", "Gruvbox Dark", "Tokyo Night",
	"Solarized Dark", "Monokai", "Catppuccin Mocha",
	"One Light", "GitHub Light", "Gruvbox Light", "Solarized Light", "Catppuccin Latte",
}

var themeCache = map[string]*ui.Theme{}

func themeByName(name string) *ui.Theme {
	if name == themeSystem {
		return nil
	}
	if t, ok := themeCache[name]; ok {
		return t
	}
	p, ok := themePalettes[name]
	if !ok {
		return nil
	}
	base := ui.LightTheme()
	if p.dark {
		base = ui.DarkTheme()
	}
	base.Dark = p.dark
	base.Background = ui.Hex(p.background)
	base.Surface = ui.Hex(p.surface)
	base.Border = ui.Hex(p.border)
	base.Text = ui.Hex(p.text)
	base.TextMuted = ui.Hex(p.muted)

	accent := ui.Hex(p.accent)
	base.Accent = accent
	base.AccentText = ui.Hex(p.accentText)
	base.InactiveAccent = accent
	hover, pressed := 12, 24
	if !p.dark {
		hover, pressed = -12, -24
	}
	base.AccentHover = shiftColor(accent, hover)
	base.AccentPressed = shiftColor(accent, pressed)
	surfaceStep := 10
	if !p.dark {
		surfaceStep = -10
	}
	base.SurfaceHover = shiftColor(base.Surface, surfaceStep)
	base.SurfacePressed = shiftColor(base.Surface, 2*surfaceStep)
	base.Selection = ui.RGBA(accent.R, accent.G, accent.B, 0.3)
	base.Focus = ui.RGBA(accent.R, accent.G, accent.B, 0.55)
	if p.dark {
		base.Scrollbar = ui.RGBA(255, 255, 255, 0.32)
	} else {
		base.Scrollbar = ui.RGBA(0, 0, 0, 0.28)
	}
	themeCache[name] = base
	return base
}

func shiftColor(c ui.Color, d int) ui.Color {
	clamp := func(v int) uint8 {
		return uint8(max(0, min(255, v)))
	}
	return ui.RGB(clamp(int(c.R)+d), clamp(int(c.G)+d), clamp(int(c.B)+d))
}

func themeName(name string) string {
	if slices.Contains(themeNames, name) {
		return name
	}
	return themeSystem
}
