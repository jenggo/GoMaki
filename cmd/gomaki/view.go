package main

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"gomaki/internal/acpclient"
)

func (a *app) view(c *ui.Context) {
	a.drain()
	if a.commandsCwd != a.cwd {
		a.loadCommands()
	}
	c.SetTheme(a.windowTheme(c))
	t := c.Theme()
	if a.copyPending != "" {
		c.WriteClipboard(a.copyPending)
		a.copyPending = ""
	}
	if c.Shortcut(ui.Cmd, ui.KeyC) {
		a.copyOrClear(c)
	}
	if len(a.runningTools()) > 0 && c.Shortcut(ui.Cmd|ui.Shift, ui.KeyB) {
		a.runningOpen = !a.runningOpen
	}
	var col ui.Element
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		if a.err != "" {
			a.errorBanner(c, t)
		}
		if len(a.transcript) == 0 {
			a.emptyState(c, t)
		} else {
			col = a.transcriptView(c, t)
		}
		a.planPanel(c, t)
		a.planAction(c, t)
		a.composer(c, t)
		a.statusBar(c, t)
	})
	a.permissionDialog(c)
	a.elicitationDialog(c, t)
	a.resumeDialog(c, t)
	a.settingsDialog(c, t)
	a.runningDialog(c, t)
	a.selectedText = selectionText(col)
}

// copyOrClear copies the transcript selection, or clears the composer when
// nothing is selected. The clipboard write waits for the next frame: a key
// callback runs between frames, where the context cannot reach the clipboard.
func (a *app) copyOrClear(c *ui.Context) {
	if a.selectedText != "" {
		a.copyPending = a.selectedText
		a.notice = "Copied selection"
		c.Invalidate()
		return
	}
	a.prompt = ""
}

// modeColors gives each mode its own color. Known modes keep a fixed color;
// any other mode takes one from the rest of the palette, picked by name so it
// stays the same across runs.
var modeColors = map[string]ui.Color{
	"build":  ui.RGBA(217, 163, 0, 1),
	modePlan: ui.RGBA(59, 130, 246, 1),
}

var extraModeColors = []ui.Color{
	ui.RGBA(168, 85, 247, 1),
	ui.RGBA(16, 185, 129, 1),
	ui.RGBA(249, 115, 22, 1),
	ui.RGBA(20, 184, 166, 1),
	ui.RGBA(236, 72, 153, 1),
}

func modeColor(mode string) ui.Color {
	if c, ok := modeColors[mode]; ok {
		return c
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(mode))
	return extraModeColors[int(h.Sum32())%len(extraModeColors)]
}

func (a *app) modeChip(c *ui.Context, t *ui.Theme, label string) {
	chip := ui.Row(c).Padding(t.Space(0.5), t.Space(1.5)).Radius(t.Space(0.75)).AlignItems(ui.Center)
	if a.nextMode() != "" {
		chip = chip.Cursor(ui.CursorPointer).OnClick(func() { a.switchMode(a.nextMode()) })
		if chip.Hovered() {
			chip = chip.Background(t.SurfaceHover)
		}
		chip = chip.Tooltip("Click to switch mode")
	}
	chip.Children(func() {
		ui.Text(c, label).FontSize(t.Rem(0.75)).TextColor(modeColor(a.mode)).IgnoreInput(true)
	})
}

func (a *app) statusBar(c *ui.Context, t *ui.Theme) {
	workspace, model, mode, status, stColor := a.statusValues(t)
	ui.Row(c).Padding(t.Space(1.5), t.Space(4)).Gap(t.Space(2)).AlignItems(ui.Center).
		Background(t.Surface).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		ui.Text(c, workspace).FontSize(t.Rem(0.75)).TextColor(t.TextMuted).Grow(1).MaxLines(1)
		a.modeChip(c, t, mode)
		ui.Text(c, "·").FontSize(t.Rem(0.75)).TextColor(t.TextMuted)
		ui.Text(c, model).FontSize(t.Rem(0.75)).TextColor(t.TextMuted).MaxWidth(340).MaxLines(1)
		a.statusMetrics(c, t)
		if !a.statusIconOnly() {
			ui.Text(c, status).FontSize(t.Rem(0.75)).TextColor(stColor)
		}
	})
}

func (a *app) statusValues(t *ui.Theme) (workspace, model, mode, status string, color ui.Color) {
	workspace = a.cwd
	if workspace == "" {
		workspace = "No workspace"
	}
	if a.branch != "" {
		workspace += " (" + a.branch + ")"
	}
	model = a.model
	if model == "" {
		model = "No model"
	}
	mode = modeLabel(a.mode)
	if mode == "" {
		mode = "No mode"
	}
	status = a.status
	if status == "" {
		switch {
		case a.client == nil && a.err != "":
			status = "Maki unavailable"
		case a.client == nil:
			status = "Connecting…"
		case a.sessionID == "":
			status = "No session"
		default:
			status = "Ready"
		}
	}
	color = t.TextMuted
	if a.status != "" {
		color = t.Accent
	}
	if a.notice != "" {
		status = a.notice
		color = t.Success
	}
	if a.client == nil && a.err != "" {
		color = t.Danger
	}
	return workspace, model, mode, status, color
}

func (a *app) statusMetrics(c *ui.Context, t *ui.Theme) {
	if a.contextSize > 0 {
		ui.Text(c, "·").FontSize(t.Rem(0.75)).TextColor(t.TextMuted)
		pct := int(float64(a.contextUsed)/float64(a.contextSize)*100 + 0.5)
		ui.Text(c, fmt.Sprintf("%s/%s (%d%%)", tokenCount(a.contextUsed), tokenCount(a.contextSize), pct)).
			FontSize(t.Rem(0.75)).TextColor(t.TextMuted).FontFeatures("tnum").
			Tooltip(fmt.Sprintf("Context: %d of %d tokens", a.contextUsed, a.contextSize))
	}
	if a.costAmount > 0 {
		ui.Text(c, "·").FontSize(t.Rem(0.75)).TextColor(t.TextMuted)
		label := fmt.Sprintf("%.2f %s", a.costAmount, a.costCurrency)
		if a.costCurrency == "USD" {
			label = fmt.Sprintf("$%.2f", a.costAmount)
		}
		ui.Text(c, label).FontSize(t.Rem(0.75)).TextColor(t.TextMuted).FontFeatures("tnum")
	}
	if n := len(a.runningTools()); n > 0 {
		ui.Text(c, "·").FontSize(t.Rem(0.75)).TextColor(t.TextMuted)
		ui.Text(c, fmt.Sprintf("%d running", n)).FontSize(t.Rem(0.75)).TextColor(t.Accent).
			Cursor(ui.CursorPointer).OnClick(func() { a.runningOpen = true })
	}
	switch {
	case a.busy && a.statusIconOnly():
		ui.Spinner(c).Size(12, 12).TextColor(t.Accent)
	case a.statusIconOnly():
		ui.Text(c, "●").FontSize(t.Rem(0.55)).TextColor(t.TextMuted).IgnoreInput(true)
	default:
		ui.Text(c, "·").FontSize(t.Rem(0.75)).TextColor(t.TextMuted)
	}
}

// statusIconOnly is true for the bare ready and working states, which the
// icon alone conveys.
func (a *app) statusIconOnly() bool {
	if a.notice != "" || a.err != "" {
		return false
	}
	if a.busy {
		return true
	}
	return a.client != nil && a.sessionID != "" && a.status == ""
}

func (a *app) errorBanner(c *ui.Context, t *ui.Theme) {
	ui.Row(c).Padding(t.Space(2), t.Space(4)).Gap(t.Space(2)).AlignItems(ui.Center).Background(ui.RGBA(180, 40, 40, 0.16)).Children(func() {
		ui.Text(c, a.err).TextColor(t.Danger).Grow(1)
		if a.client == nil {
			if ui.Button(c, "Retry").Clicked() {
				a.err = ""
				a.startup()
			}
		} else if ui.Button(c, "Dismiss").Clicked() {
			a.err = ""
		}
	})
}

func (a *app) emptyState(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).Center().Gap(t.Space(2)).Children(func() {
		ui.Text(c, appName).FontSize(t.Rem(1.6)).Bold()
		ui.Text(c, "A desktop client for Maki's ACP.").TextColor(t.TextMuted)
		if a.client == nil {
			if a.err == "" {
				ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(16, 16)
					ui.Text(c, "Connecting to Maki…").FontSize(t.Rem(0.85)).TextColor(t.TextMuted)
				})
			}
			return
		}
		if a.resumeLoading {
			ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(16, 16)
				ui.Text(c, "Loading session…").FontSize(t.Rem(0.85)).TextColor(t.TextMuted)
			})
			return
		}
		if a.sessionsLoading {
			ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(16, 16)
				ui.Text(c, "Loading previous sessions…").FontSize(t.Rem(0.85)).TextColor(t.TextMuted)
			})
			return
		}
		if a.sessionID != "" && a.resumed {
			ui.Text(c, "No messages yet. Type below to continue this session.").
				FontSize(t.Rem(0.8)).TextColor(t.TextMuted)
			return
		}
		if len(a.sessions) > 0 && !a.resumeOpen {
			ui.Column(c).WidthPercent(80).MaxWidth(560).Gap(t.Space(1)).Padding(t.Space(2)).Radius(t.Space(2)).
				Background(t.Surface).Border(1, t.Border).Children(func() {
				ui.Text(c, "Previous sessions").FontSize(t.Rem(0.8)).Bold().TextColor(t.TextMuted)
				a.sessionList(c, t)
			})
			ui.Text(c, "or type below to start a new session in this folder.").
				FontSize(t.Rem(0.8)).TextColor(t.TextMuted)
			return
		}
		ui.Text(c, "No stored sessions yet. Type below to start a new one in this folder.").
			FontSize(t.Rem(0.8)).TextColor(t.TextMuted)
	})
}

func (a *app) transcriptView(c *ui.Context, t *ui.Theme) ui.Element {
	if a.scroll.Y >= a.scroll.MaxY {
		a.scroll.Y = math.MaxFloat32
	}
	var col ui.Element
	ui.Scroll(c).TrackScroll(&a.scroll).Grow(1).Children(func() {
		col = ui.Column(c).WidthPercent(100).Padding(t.Space(3), t.Space(4)).Gap(t.Space(2.5)).Selectable().
			ContextMenu(func(*ui.Menu) { a.copyTranscript(c, col) }).
			HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind != ui.InputKeyDown || ev.Key != ui.KeyC || ev.Mods != ui.Cmd {
					return false
				}
				a.copyOrClear(c)
				return true
			})
		col.Children(func() {
			for i := range a.transcript {
				streaming := a.busy && i == len(a.transcript)-1
				a.entryView(c, col, &a.transcript[i], t, streaming)
			}
		})
	})
	return col
}

func (a *app) entryView(c *ui.Context, col ui.Element, e *entry, t *ui.Theme, streaming bool) {
	switch e.Kind {
	case entryUser:
		ui.Row(c).Children(func() {
			ui.Spacer(c)
			a.copyable(c, col, ui.Text(c, e.Text).MaxWidth(620).Padding(t.Space(2.5), t.Space(3)).
				Radius(t.Space(3)).Background(t.Accent).TextColor(t.AccentText).
				SelectionColor(ui.RGBA(255, 255, 255, 0.35)))
		})
	case entryAgent:
		if streaming {
			markdown.Stream(c, e.Text, &a.md)
		} else {
			markdown.View(c, e.Text, &a.md)
		}
	case entryThought:
		expanded := e.Expanded
		ui.Column(c).Gap(t.Space(1)).Children(func() {
			row := ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			row.OnClick(func() { e.Expanded = !e.Expanded })
			row.Children(func() {
				ui.Text(c, chevron(expanded)).FontSize(t.Rem(0.8)).TextColor(t.TextMuted).IgnoreInput(true)
				ui.Text(c, "Thinking").Bold().FontSize(t.Rem(0.85)).TextColor(t.TextMuted).IgnoreInput(true)
			})
			text := e.Text
			if !expanded {
				text = thoughtPreview(text)
			}
			body := ui.Text(c, text).FontSize(t.Rem(0.9)).TextColor(t.TextMuted)
			if !expanded {
				body = body.MaxLines(5)
			}
			a.copyable(c, col, body)
		})
	case entryTool:
		a.toolEntry(c, col, e, t)
	}
}

func (a *app) toolEntry(c *ui.Context, col ui.Element, e *entry, t *ui.Theme) {
	todos := len(e.Todos) > 0
	expandable := !todos && (e.ToolInput != "" || e.ToolOutput != "")
	ui.Column(c).Gap(t.Space(1)).Padding(t.Space(2), t.Space(2.5)).Radius(t.Space(2)).Background(t.Surface).Border(1, t.Border).Children(func() {
		a.toolHeader(c, col, e, t, expandable)
		if todos {
			ui.Divider(c)
			a.todoList(c, e.Todos, t)
			return
		}
		if !e.Expanded {
			return
		}
		ui.Divider(c)
		if e.ToolInput != "" {
			a.toolBlock(c, col, "Input", e.ToolInput, t)
		}
		if e.ToolOutput != "" {
			a.toolBlock(c, col, "Output", e.ToolOutput, t)
		}
	})
}

func (a *app) toolHeader(c *ui.Context, col ui.Element, e *entry, t *ui.Theme, expandable bool) {
	row := ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center)
	if expandable {
		row = row.Cursor(ui.CursorPointer)
	}
	row = row.ContextMenu(func(*ui.Menu) { a.copyTranscript(c, col) })
	if expandable {
		row.OnClick(func() { e.Expanded = !e.Expanded })
	}
	row.Children(func() {
		if expandable {
			ui.Text(c, chevron(e.Expanded)).FontSize(t.Rem(0.8)).TextColor(t.TextMuted).IgnoreInput(true)
		}
		title := ui.Text(c, e.Text).Bold().FontSize(t.Rem(0.9))
		badge := ui.Badge(c, statusLabel(e.Status)).Background(statusColor(t, e.Status)).
			TextColor(statusTextColor(t, e.Status))
		if expandable {
			title.IgnoreInput(true)
			badge.IgnoreInput(true)
		} else {
			a.copyable(c, col, title)
			a.copyable(c, col, badge)
		}
	})
}

func (a *app) todoList(c *ui.Context, todos []acpclient.PlanEntry, t *ui.Theme) {
	ui.Column(c).Gap(t.Space(0.75)).Children(func() {
		for _, p := range todos {
			ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, planIcon(p.Status)).FontSize(t.Rem(0.8)).TextColor(planColor(t, p.Status)).IgnoreInput(true)
				ui.Text(c, p.Content).FontSize(t.Rem(0.85)).TextColor(planTextColor(t, p.Status)).Grow(1).MaxLines(2)
			})
		}
	})
}

// copyable turns a right-click into a copy: the selection when there is
// one, else the whole transcript. The menu stays empty, so no panel takes
// the focus and the selection stays.
func (a *app) copyable(c *ui.Context, col, el ui.Element) {
	el.ContextMenu(func(*ui.Menu) { a.copyTranscript(c, col) })
}

func (a *app) copyTranscript(c *ui.Context, col ui.Element) {
	text := a.selectedText
	if text == "" {
		text = selectionText(col)
	}
	msg := "Copied selection"
	if text == "" {
		text = a.transcriptText()
		msg = "Copied all text"
	}
	if text == "" {
		return
	}
	a.copyPending = text
	a.notice = msg
	c.Invalidate()
}

func selectionText(el ui.Element) string {
	parts := el.SelectedTexts()
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteString(" ")
		}
		runes := []rune(p.Text)
		start := min(max(p.Start, 0), len(runes))
		end := min(max(p.End, start), len(runes))
		b.WriteString(string(runes[start:end]))
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func (a *app) transcriptText() string {
	var b strings.Builder
	for _, e := range a.transcript {
		if e.Text != "" {
			b.WriteString(e.Text)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func thoughtPreview(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	truncated := false
	lines := strings.Split(text, "\n")
	if len(lines) > 5 {
		lines = lines[len(lines)-5:]
		truncated = true
	}
	text = strings.Join(lines, "\n")
	const maxRunes = 480
	if r := []rune(text); len(r) > maxRunes {
		text = string(r[len(r)-maxRunes:])
		truncated = true
	}
	if truncated {
		text = "…" + text
	}
	return text
}

func tokenCount(n int) string {
	switch {
	case n >= 1_000_000:
		v := float64(n) / 1_000_000
		if v == math.Trunc(v) {
			return fmt.Sprintf("%.0fM", v)
		}
		return fmt.Sprintf("%.1fM", v)
	case n >= 1000:
		v := float64(n) / 1000
		if v == math.Trunc(v) {
			return fmt.Sprintf("%.0fk", v)
		}
		return fmt.Sprintf("%.1fk", v)
	default:
		return fmt.Sprint(n)
	}
}

func chevron(expanded bool) string {
	if expanded {
		return "▾"
	}
	return "▸"
}

func (a *app) toolBlock(c *ui.Context, col ui.Element, label, text string, t *ui.Theme) {
	ui.Column(c).Gap(2).Children(func() {
		ui.Text(c, label).FontSize(t.Rem(0.75)).Bold().TextColor(t.TextMuted)
		if len(text) > toolHighlightLimit {
			a.copyable(c, col, ui.Text(c, text).FontSize(t.Rem(0.8)))
			return
		}
		markdown.View(c, codeFence(text), &a.md)
	})
}

// toolHighlightLimit keeps large tool outputs plain: highlighting them costs
// a parse and a tokenize per frame.
const toolHighlightLimit = 20000

// codeFence wraps text in a fenced code block, with a language when the text
// is JSON, and a fence longer than any backtick run inside it.
func codeFence(text string) string {
	lang := ""
	if trimmed := strings.TrimSpace(text); json.Valid([]byte(trimmed)) &&
		(strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) {
		lang = "json"
	}
	fence := strings.Repeat("`", max(3, longestBacktickRun(text)+1))
	return fence + lang + "\n" + text + "\n" + fence
}

func longestBacktickRun(s string) int {
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
			continue
		}
		run = 0
	}
	return longest
}

func workspaceName(dir string) string {
	if dir == "" {
		return appName
	}
	name := filepath.Base(dir)
	if name == "." || name == string(filepath.Separator) {
		return appName
	}
	return name
}

func modeLabel(mode string) string {
	if mode == "" {
		return mode
	}
	return strings.ToUpper(mode[:1]) + mode[1:]
}

const (
	toolPending   = "pending"
	toolRunning   = "in_progress"
	toolCompleted = "completed"
	toolFailed    = "failed"
	toolKindEdit  = "edit"
	modePlan      = "plan"
)

func statusLabel(status string) string {
	switch status {
	case toolCompleted:
		return "Done"
	case toolFailed:
		return "Failed"
	case toolRunning:
		return "Running"
	default:
		return "Pending"
	}
}

func statusColor(t *ui.Theme, status string) ui.Color {
	switch status {
	case toolCompleted:
		return t.Success
	case toolFailed:
		return t.Danger
	case toolRunning:
		return t.Warning
	default:
		return t.Surface
	}
}

func statusTextColor(t *ui.Theme, status string) ui.Color {
	switch status {
	case toolCompleted, toolFailed, toolRunning:
		return t.AccentText
	default:
		return t.TextMuted
	}
}

func (a *app) handleEsc(c *ui.Context, k ui.TextKey) bool {
	if k.Key != ui.KeyEscape {
		return false
	}
	now := time.Now()
	double := !a.escAt.IsZero() && now.Sub(a.escAt) <= time.Second
	if !a.busy {
		if strings.TrimSpace(a.prompt) != "" {
			a.escAt = time.Time{}
			return false
		}
		a.escAt = time.Time{}
		if double {
			a.rewindLastTurn()
		} else {
			a.escAt = now
			a.notice = "Press Esc again to edit the last prompt"
		}
		c.Invalidate()
		return true
	}
	a.escAt = time.Time{}
	if double {
		a.stopPrompt()
	} else {
		a.escAt = now
		a.notice = "Press Esc again to stop"
	}
	c.Invalidate()
	return true
}

func (a *app) commandPalette(c *ui.Context, t *ui.Theme) {
	cmds := a.commandMatches()
	if len(cmds) == 0 {
		return
	}
	ui.Column(c).Padding(t.Space(2), t.Space(3)).Gap(t.Space(0.5)).Background(t.Surface).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		ui.Text(c, "Commands").FontSize(t.Rem(0.75)).Bold().TextColor(t.TextMuted)
		ui.Scroll(c).MaxHeight(t.Rem(16)).Children(func() {
			ui.Column(c).Children(func() {
				for _, cmd := range cmds {
					row := ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).
						Padding(t.Space(1.5), t.Space(2)).Radius(t.Space(1)).
						Cursor(ui.CursorPointer)
					if row.Hovered() {
						row = row.Background(t.SurfaceHover)
					}
					row.OnClick(func() {
						a.prompt = cmd.Name + " "
						a.focusInput = true
					})
					row.Children(func() {
						ui.Text(c, cmd.Name).Bold().FontSize(t.Rem(0.85))
						if cmd.Description != "" {
							ui.Text(c, cmd.Description).FontSize(t.Rem(0.8)).
								TextColor(t.TextMuted).Grow(1)
						}
					})
				}
			})
		})
	})
}

// planPanel docks the agent's task list above the composer, so progress stays
// visible without scrolling back. The header summarizes and toggles it.
func (a *app) planPanel(c *ui.Context, t *ui.Theme) {
	if len(a.plan) == 0 {
		return
	}
	done := 0
	current := ""
	for _, p := range a.plan {
		switch p.Status {
		case toolCompleted:
			done++
		case toolRunning:
			if current == "" {
				current = p.Content
			}
		}
	}
	ui.Column(c).Padding(t.Space(1), t.Space(3)).Gap(t.Space(1)).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		header := ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Cursor(ui.CursorPointer)
		header.OnClick(func() { a.planExpanded = !a.planExpanded })
		header.Children(func() {
			ui.Text(c, chevron(a.planExpanded)).FontSize(t.Rem(0.75)).TextColor(t.TextMuted).IgnoreInput(true)
			ui.Text(c, fmt.Sprintf("Tasks %d of %d", done, len(a.plan))).
				FontSize(t.Rem(0.75)).Bold().TextColor(t.TextMuted).IgnoreInput(true)
			if current != "" {
				ui.Text(c, "·").FontSize(t.Rem(0.75)).TextColor(t.TextMuted).IgnoreInput(true)
				ui.Text(c, current).FontSize(t.Rem(0.75)).TextColor(t.TextMuted).
					Grow(1).MaxLines(1).IgnoreInput(true)
			}
		})
		if !a.planExpanded {
			return
		}
		for _, p := range a.plan {
			ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, planIcon(p.Status)).FontSize(t.Rem(0.75)).TextColor(planColor(t, p.Status)).IgnoreInput(true)
				ui.Text(c, p.Content).FontSize(t.Rem(0.8)).TextColor(planTextColor(t, p.Status)).
					Grow(1).MaxLines(2).IgnoreInput(true)
			})
		}
	})
}

// planAction offers the accepted plan to the OpenSpec fast-forward workflow.
// It appears only after a plan-mode turn wrote a plan and the ff skill exists.
func (a *app) planAction(c *ui.Context, t *ui.Theme) {
	if !a.planReady {
		return
	}
	ui.Row(c).Padding(t.Space(1), t.Space(3)).Gap(t.Space(2)).AlignItems(ui.Center).
		BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		ui.Text(c, "Plan ready").FontSize(t.Rem(0.75)).Bold().TextColor(t.TextMuted).IgnoreInput(true)
		row := ui.Row(c).Gap(t.Space(1)).Padding(t.Space(0.75), t.Space(1.5)).
			Radius(t.Space(0.75)).Cursor(ui.CursorPointer).OnClick(a.sendPlanChange)
		if row.Hovered() {
			row = row.Background(t.SurfaceHover)
		}
		row.Children(func() {
			ui.Text(c, "Create change from plan").FontSize(t.Rem(0.8)).IgnoreInput(true)
		})
	})
}

func planIcon(status string) string {
	switch status {
	case toolCompleted:
		return "✓"
	case toolRunning:
		return "●"
	default:
		return "○"
	}
}

func planColor(t *ui.Theme, status string) ui.Color {
	switch status {
	case toolCompleted:
		return t.Success
	case toolRunning:
		return t.Warning
	default:
		return t.TextMuted
	}
}

func planTextColor(t *ui.Theme, status string) ui.Color {
	if status == toolCompleted {
		return t.TextMuted
	}
	return t.Text
}

func (a *app) composer(c *ui.Context, t *ui.Theme) {
	a.commandPalette(c, t)
	ui.Column(c).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		if a.attachName != "" {
			ui.Row(c).Padding(t.Space(1), t.Space(3)).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Attached: "+a.attachName).FontSize(t.Rem(0.75)).TextColor(t.TextMuted).Grow(1).MaxLines(1)
				if ui.Button(c, "Remove").Clicked() {
					a.clearAttachment()
				}
			})
		}
		ui.Row(c).Padding(t.Space(1.5), t.Space(3)).Gap(t.Space(2)).AlignItems(ui.End).Children(func() {
			input := ui.TextAreaBase(c, &a.prompt).Lines(1, 7).Grow(1).AutoFocus().
				Padding(t.Space(1), t.Space(2)).Radius(t.Space(2)).
				Background(t.Surface).Border(1, t.Border).Disabled(a.client == nil).
				HandleInput(func(ev ui.InputEvent) bool {
					// The Edit menu's Copy reaches the focused field, so take it
					// here while the transcript holds the selection.
					if ev.Kind == ui.InputCommand && ev.Text == "copy" && a.selectedText != "" {
						a.copyOrClear(c)
						return true
					}
					return false
				})
			if input.Focused() {
				input = input.BorderColor(t.Accent)
			}
			if a.focusInput {
				a.focusInput = false
				input.Focus()
			}
			input.Placeholder("Ask Maki… (Enter to send, Shift+Enter for a newline)").
				OnTextKey(func(k ui.TextKey) bool { return a.composerKey(c, k) })
		})
	})
}

// composerKey handles the keys the composer takes before the text area does.
func (a *app) composerKey(c *ui.Context, k ui.TextKey) bool {
	if a.handleEsc(c, k) {
		return true
	}
	if k.Key == ui.KeyC && k.Mods == ui.Cmd {
		if a.selectedText != "" {
			a.copyOrClear(c)
			return true
		}
		k.SetText("")
		return true
	}
	if k.Key != ui.KeyEnter || k.Mods&ui.Shift != 0 {
		return false
	}
	if a.send(k.Text()) {
		k.SetText("")
	}
	return true
}

// runningDialog lists tool calls still running. ACP reports delegated agents
// and long commands as tool calls, so this is the window onto background work.
func (a *app) runningDialog(c *ui.Context, t *ui.Theme) {
	if !a.runningOpen {
		return
	}
	idx := a.runningTools()
	if len(idx) == 0 {
		a.runningOpen = false
		return
	}
	ui.Modal(c, &a.runningOpen, func() {
		ui.Column(c).Width(560).Gap(t.Space(2)).Children(func() {
			ui.Text(c, "Running").FontSize(t.Rem(1.2)).Bold()
			ui.Text(c, "Tool calls still in progress. Results arrive in the conversation.").
				FontSize(t.Rem(0.8)).TextColor(t.TextMuted)
			ui.Divider(c)
			for _, i := range idx {
				e := &a.transcript[i]
				ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
					ui.Badge(c, statusLabel(e.Status)).Background(statusColor(t, e.Status)).
						TextColor(statusTextColor(t, e.Status))
					ui.Text(c, e.Text).Grow(1).MaxLines(2)
					ui.Text(c, elapsed(e.Started)).FontSize(t.Rem(0.75)).
						TextColor(t.TextMuted).FontFeatures("tnum")
				})
			}
			ui.Row(c).Justify(ui.End).Children(func() {
				if ui.Button(c, "Close").Clicked() {
					a.runningOpen = false
				}
			})
		})
	})
}

func elapsed(start time.Time) string {
	if start.IsZero() {
		return ""
	}
	d := time.Since(start)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

func (a *app) permissionDialog(c *ui.Context) {
	if a.permission == nil {
		return
	}
	t := c.Theme()
	ui.Modal(c, &a.permissionOpen, func() {
		ui.Column(c).Width(440).Gap(t.Space(2.5)).Children(func() {
			ui.Text(c, "Permission required").FontSize(t.Rem(1.3)).Bold()
			title := a.permission.Title
			if title == "" {
				title = "Maki wants to run a tool."
			}
			ui.Text(c, title)
			ui.Divider(c)
			for _, opt := range a.permission.Options {
				if a.permissionOption(c, t, opt) {
					a.answerPermission(opt.ID)
				}
			}
			ui.Row(c).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					a.answerPermission("")
				}
			})
		})
	})
	if !a.permissionOpen && a.permission != nil {
		a.answerPermission("")
	}
}

func (a *app) permissionOption(c *ui.Context, t *ui.Theme, opt acpclient.PermissionOption) bool {
	if opt.Kind == "allow_once" {
		return ui.PrimaryButton(c, opt.Name).Clicked()
	}
	btn := ui.Button(c, opt.Name)
	if strings.HasPrefix(opt.Kind, "reject") {
		btn = btn.TextColor(t.Danger)
	}
	return btn.Clicked()
}

func (a *app) elicitationDialog(c *ui.Context, t *ui.Theme) {
	if a.elicitation == nil {
		return
	}
	ui.Modal(c, &a.elicitationOpen, func() {
		ui.Column(c).Width(520).MaxHeight(520).Gap(t.Space(2.5)).Children(func() {
			ui.Text(c, "Question").FontSize(t.Rem(1.3)).Bold()
			if a.elicitation.Message != "" {
				ui.Text(c, a.elicitation.Message).TextColor(t.TextMuted)
			}
			ui.Divider(c)
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Gap(t.Space(3)).Children(func() {
					for _, f := range a.elicitation.Fields {
						a.fieldView(c, f, t)
					}
				})
			})
			ui.Row(c).Gap(t.Space(2)).Justify(ui.End).Children(func() {
				if ui.Button(c, "Decline").Clicked() {
					a.answerElicitation(true)
				}
				if ui.PrimaryButton(c, "Submit").Clicked() {
					a.answerElicitation(false)
				}
			})
		})
	})
	if !a.elicitationOpen && a.elicitation != nil {
		a.answerElicitation(true)
	}
}

func (a *app) fieldView(c *ui.Context, f acpclient.ElicitationField, t *ui.Theme) {
	ui.Column(c).Gap(t.Space(1)).Children(func() {
		if f.Title != "" && f.Title != a.elicitation.Message {
			ui.Text(c, f.Title).Bold()
		}
		if f.Description != "" {
			ui.Text(c, f.Description).FontSize(t.Rem(0.85)).TextColor(t.TextMuted)
		}
		switch {
		case len(f.Options) == 0:
			v := a.answers[f.Name]
			ui.TextInput(c, &v).Placeholder("Answer")
			a.answers[f.Name] = v
		case f.Multi:
			if a.multiAnswers[f.Name] == nil {
				a.multiAnswers[f.Name] = make(map[string]bool)
			}
			for _, opt := range f.Options {
				checked := a.multiAnswers[f.Name][opt.Value]
				ui.Checkbox(c, &checked, opt.Label)
				a.multiAnswers[f.Name][opt.Value] = checked
			}
		default:
			a.choiceRows(c, f, t)
		}
	})
}

// choiceRows draws one wrapped row per option. A dropdown cuts long answers
// to a single line, and agents write whole sentences as options.
func (a *app) choiceRows(c *ui.Context, f acpclient.ElicitationField, t *ui.Theme) {
	if a.answers[f.Name] == "" {
		a.answers[f.Name] = f.Options[0].Label
	}
	for _, opt := range f.Options {
		chosen := a.answers[f.Name] == opt.Label
		row := ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Start).Padding(t.Space(2), t.Space(2.5)).Radius(t.Space(1.5)).
			Border(1, t.Border).Cursor(ui.CursorPointer)
		if chosen {
			row = row.BorderColor(t.Accent).Background(t.SurfaceHover)
		}
		row.OnClick(func() {
			a.answers[f.Name] = opt.Label
		})
		row.Children(func() {
			mark := "○"
			if chosen {
				mark = "●"
			}
			ui.Text(c, mark).TextColor(t.Accent).IgnoreInput(true)
			ui.Text(c, opt.Label).FontSize(t.Rem(0.9)).Grow(1).MaxLines(6).IgnoreInput(true)
		})
	}
}

func (a *app) resumeDialog(c *ui.Context, t *ui.Theme) {
	if !a.resumeOpen {
		return
	}
	ui.Modal(c, &a.resumeOpen, func() {
		ui.Column(c).Width(560).MaxHeight(560).Gap(t.Space(2.5)).Children(func() {
			ui.Text(c, "Open session").FontSize(t.Rem(1.3)).Bold()
			ui.Text(c, "Stored sessions for "+workspaceName(a.cwd)).FontSize(t.Rem(0.85)).TextColor(t.TextMuted)
			if a.resumeLoading {
				ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(16, 16)
					ui.Text(c, "Loading session…").FontSize(t.Rem(0.85)).TextColor(t.TextMuted)
				})
			}
			if a.resumeErr != "" {
				ui.Text(c, a.resumeErr).FontSize(t.Rem(0.8)).TextColor(t.Danger)
			}
			ui.Divider(c)
			a.sessionList(c, t)
			ui.Row(c).Justify(ui.End).Children(func() {
				if ui.Button(c, "Close").Clicked() {
					a.resumeOpen = false
				}
			})
		})
	})
}

func (a *app) sessionList(c *ui.Context, t *ui.Theme) {
	if a.sessionPick >= len(a.sessions) {
		a.sessionPick = -1
	}
	a.sessionsList.Key = func(i int) any { return a.sessions[i].ID }
	a.sessionsList.Label = func(i int) string { return sessionLabel(a.sessions[i]) }
	a.sessionsList.Selected = &a.sessionPick

	list := ui.List(c, &a.sessionsList, len(a.sessions), func(i int) {
		s := a.sessions[i]
		title, meta := t.Text, t.TextMuted
		if a.sessionPick == i {
			title, meta = t.AccentText, t.AccentText
		}
		ui.Column(c).Gap(2).Padding(t.Space(2), t.Space(2.5)).Children(func() {
			ui.Text(c, sessionLabel(s)).FontSize(t.Rem(0.85)).TextColor(title).MaxLines(2)
			ui.Text(c, sessionDate(s.UpdatedAt)).FontSize(t.Rem(0.75)).TextColor(meta).MaxLines(1)
		})
	}).MaxHeight(t.Rem(22))
	list.Children(func() {
		if len(a.sessions) == 0 {
			ui.Text(c, "No stored sessions yet.").FontSize(t.Rem(0.8)).TextColor(t.TextMuted)
		}
	})
	if !a.resumeLoading && list.Changed() && a.sessionPick >= 0 && a.sessionPick < len(a.sessions) {
		a.resume(a.sessions[a.sessionPick].ID)
	}
}

func sessionLabel(s acpclient.SessionInfo) string {
	if s.Title != "" {
		return s.Title
	}
	return s.ID
}

func sessionDate(updatedAt string) string {
	ts, err := time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return updatedAt
	}
	return ts.Local().Format("2 Jan 15:04")
}
