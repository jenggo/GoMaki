package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const settingsFile = "settings.json"

// settings are the appearance preferences kept across runs. Font is a
// monospaced family; Size is in DIPs; Theme is "System" or a palette name.
type settings struct {
	Font  string  `json:"font"`
	Theme string  `json:"theme"`
	Size  float32 `json:"size"`
}

var defaultSettings = settings{Size: 14, Theme: themeSystem}

var fontSizes = []float32{10, 11, 12, 13, 14, 15, 16, 18, 20, 22, 24, 28}

func (a *app) loadSettings() {
	a.settings = defaultSettings
	path, err := a.dataPath(settingsFile)
	if err == nil {
		if blob, readErr := os.ReadFile(path); readErr == nil {
			var s settings
			if json.Unmarshal(blob, &s) == nil {
				if s.Size > 0 {
					a.settings.Size = s.Size
				}
				a.settings.Theme = themeName(s.Theme)
				a.settings.Font = s.Font
			}
		}
	}

	fonts := a.fontNames()
	if len(fonts) > 0 && !slices.Contains(fonts, a.settings.Font) {
		a.settings.Font = fonts[0]
	}
	a.applyAppearance()
}

func (a *app) saveSettings() {
	path, err := a.dataPath(settingsFile)
	if err != nil {
		return
	}
	blob, err := json.Marshal(a.settings)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func (a *app) dataPath(name string) (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// applyAppearance gives the native window chrome the palette's light or
// dark look. The palette itself is applied in windowTheme.
func (a *app) applyAppearance() {
	if p, ok := themePalettes[a.settings.Theme]; ok {
		if p.dark {
			mygo.Theme.SetSource(mygo.ThemeDark)
		} else {
			mygo.Theme.SetSource(mygo.ThemeLight)
		}
		return
	}
	mygo.Theme.SetSource(mygo.ThemeSystem)
}

func (a *app) windowTheme(c *ui.Context) *ui.Theme {
	base := c.Theme()
	if themed := themeByName(a.settings.Theme); themed != nil {
		base = themed
	}
	t := *base
	if a.settings.Font != "" {
		t.Font = a.settings.Font
	}
	if a.settings.Size > 0 {
		t.FontSize = a.settings.Size
	}
	return &t
}

// fontNames lists the monospaced families, sorted: the coding agent's
// interface only offers monospace.
func (a *app) fontNames() []string {
	if a.families == nil {
		for _, f := range ui.FontFamilies() {
			if f.Monospaced {
				a.families = append(a.families, f.Name)
			}
		}
		slices.Sort(a.families)
	}
	return a.families
}

func (a *app) settingsDialog(c *ui.Context, t *ui.Theme) {
	if !a.settingsOpen {
		return
	}
	ui.Modal(c, &a.settingsOpen, func() {
		ui.Column(c).Width(400).Gap(t.Space(2.5)).Children(func() {
			ui.Text(c, "Settings").FontSize(t.Rem(1.4)).Bold()
			ui.Divider(c)
			a.fontPicker(c, t)
			a.sizePicker(c, t)
			a.themePicker(c, t)
			ui.Row(c).Justify(ui.End).Children(func() {
				if ui.Button(c, "Done").Clicked() {
					a.settingsOpen = false
				}
			})
		})
	})
}

func (a *app) fontPicker(c *ui.Context, t *ui.Theme) {
	ui.Text(c, "Monospace font").FontSize(t.Rem(0.85)).TextColor(t.TextMuted)
	fonts := a.fontNames()
	if len(fonts) == 0 {
		ui.Text(c, "No monospaced fonts found.").TextColor(t.Danger)
		return
	}
	font := a.settings.Font
	if !slices.Contains(fonts, font) {
		font = fonts[0]
	}
	if !ui.Select(c, &font, fonts).Changed() {
		return
	}
	a.settings.Font = font
	a.saveSettings()
	c.Invalidate()
}

func (a *app) sizePicker(c *ui.Context, t *ui.Theme) {
	ui.Text(c, "Font size").FontSize(t.Rem(0.85)).TextColor(t.TextMuted)
	size := strconv.FormatFloat(float64(a.settings.Size), 'g', -1, 32)
	sizes := make([]string, len(fontSizes))
	for i, s := range fontSizes {
		sizes[i] = strconv.FormatFloat(float64(s), 'g', -1, 32)
	}
	if !ui.Select(c, &size, sizes).Changed() {
		return
	}
	if v, err := strconv.ParseFloat(size, 32); err == nil {
		a.settings.Size = float32(v)
		a.saveSettings()
		c.Invalidate()
	}
}

func (a *app) themePicker(c *ui.Context, t *ui.Theme) {
	ui.Text(c, "Theme").FontSize(t.Rem(0.85)).TextColor(t.TextMuted)
	theme := a.settings.Theme
	if !ui.Select(c, &theme, themeNames).Changed() {
		return
	}
	a.settings.Theme = theme
	a.applyAppearance()
	a.saveSettings()
	c.Invalidate()
}
