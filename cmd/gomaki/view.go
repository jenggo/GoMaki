package main

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"gomaki/internal/acpclient"
)

func (a *app) view(c *ui.Context) {
	a.drain()
	if a.commandsCwd != a.cwd {
		a.loadCommands()
	}
	c.SetTheme(a.windowTheme(c))
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		if a.err != "" {
			a.errorBanner(c, t)
		}
		if len(a.transcript) == 0 {
			a.emptyState(c, t)
		} else {
			a.transcriptView(c, t)
		}
		a.composer(c, t)
		a.statusBar(c, t)
	})
	a.permissionDialog(c)
	a.elicitationDialog(c, t)
	a.resumeDialog(c, t)
	a.settingsDialog(c, t)
}

func (a *app) statusBar(c *ui.Context, t *ui.Theme) {
	workspace := a.cwd
	if workspace == "" {
		workspace = "No workspace"
	}
	model := a.model
	if model == "" {
		model = "No model"
	}
	mode := modeLabel(a.mode)
	if mode == "" {
		mode = "No mode"
	}
	status := a.status
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
	stColor := t.TextMuted
	if a.status != "" {
		stColor = t.Accent
	}
	if a.notice != "" {
		status = a.notice
		stColor = t.Success
	}
	if a.client == nil && a.err != "" {
		stColor = t.Danger
	}
	ui.Row(c).Padding(t.Space(1.5), t.Space(4)).Gap(t.Space(2)).AlignItems(ui.Center).
		Background(t.Surface).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		ui.Text(c, workspace).FontSize(t.Rem(0.75)).TextColor(t.TextMuted).Grow(1).MaxLines(1)
		ui.Text(c, mode).FontSize(t.Rem(0.75)).TextColor(t.TextMuted)
		ui.Text(c, "·").FontSize(t.Rem(0.75)).TextColor(t.TextMuted)
		ui.Text(c, model).FontSize(t.Rem(0.75)).TextColor(t.TextMuted).MaxWidth(340).MaxLines(1)
		if a.contextSize > 0 {
			ui.Text(c, "·").FontSize(t.Rem(0.75)).TextColor(t.TextMuted)
			ui.Text(c, "ctx "+tokenCount(a.contextUsed)+"/"+tokenCount(a.contextSize)).
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
		ui.Text(c, "·").FontSize(t.Rem(0.75)).TextColor(t.TextMuted)
		ui.Text(c, status).FontSize(t.Rem(0.75)).TextColor(stColor)
	})
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
		ui.Text(c, "A native desktop client for Maki.").TextColor(t.TextMuted)
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
		if a.sessionID != "" {
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

func (a *app) transcriptView(c *ui.Context, t *ui.Theme) {
	if a.scroll.Y >= a.scroll.MaxY {
		a.scroll.Y = math.MaxFloat32
	}
	ui.Scroll(c).TrackScroll(&a.scroll).Grow(1).Children(func() {
		col := ui.Column(c).WidthPercent(100).Padding(t.Space(3), t.Space(4)).
			Gap(t.Space(2.5)).Selectable()
		col.Children(func() {
			for i := range a.transcript {
				a.entryView(c, col, &a.transcript[i], t)
			}
		})
	})
}

func (a *app) entryView(c *ui.Context, col ui.Element, e *entry, t *ui.Theme) {
	switch e.Kind {
	case entryUser:
		ui.Row(c).Children(func() {
			ui.Spacer(c)
			a.copyable(c, col, ui.Text(c, e.Text).MaxWidth(620).Padding(t.Space(2.5), t.Space(3)).
				Radius(t.Space(3)).Background(t.Accent).TextColor(t.AccentText).
				SelectionColor(ui.RGBA(255, 255, 255, 0.35)))
		})
	case entryAgent:
		a.copyable(c, col, ui.Text(c, e.Text).FontSize(t.Rem(1)))
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
	case entryPlan:
		ui.Column(c).Padding(t.Space(3)).Radius(t.Space(2.5)).Gap(t.Space(1)).
			Background(t.Surface).Border(1, t.Border).Children(func() {
			a.copyable(c, col, ui.Text(c, "Plan").Bold().FontSize(t.Rem(0.8)).TextColor(t.TextMuted))
			for _, p := range e.Plan {
				ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
					ui.Badge(c, statusLabel(p.Status)).Background(statusColor(t, p.Status)).
						TextColor(statusTextColor(t, p.Status))
					a.copyable(c, col, ui.Text(c, p.Content).FontSize(t.Rem(0.9)).Grow(1))
				})
			}
		})
	case entryTool:
		a.toolEntry(c, col, e, t)
	}
}

func (a *app) toolEntry(c *ui.Context, col ui.Element, e *entry, t *ui.Theme) {
	expanded := e.Expanded
	expandable := e.ToolInput != "" || e.ToolOutput != ""
	ui.Column(c).Gap(t.Space(1)).Padding(t.Space(2), t.Space(2.5)).Radius(t.Space(2)).Background(t.Surface).Border(1, t.Border).Children(func() {
		row := ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center)
		if expandable {
			row = row.Cursor(ui.CursorPointer)
		}
		row = row.ContextMenu(func(*ui.Menu) { a.copyTranscript(c, col) })
		row.OnClick(func() { e.Expanded = !e.Expanded })
		row.Children(func() {
			if expandable {
				ui.Text(c, chevron(expanded)).FontSize(t.Rem(0.8)).TextColor(t.TextMuted).IgnoreInput(true)
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

// copyable turns a right-click into a copy: the selection when there is
// one, else the whole transcript. The menu stays empty, so no panel takes
// the focus and the selection stays.
func (a *app) copyable(c *ui.Context, col, el ui.Element) {
	el.ContextMenu(func(*ui.Menu) { a.copyTranscript(c, col) })
}

func (a *app) copyTranscript(c *ui.Context, col ui.Element) {
	text := selectionText(col)
	msg := "Copied selection"
	if text == "" {
		text = a.transcriptText()
		msg = "Copied all text"
	}
	if text == "" {
		return
	}
	c.WriteClipboard(text)
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
		if e.Kind == entryPlan {
			for _, p := range e.Plan {
				if p.Content != "" {
					b.WriteString(p.Content)
					b.WriteString("\n")
				}
			}
			continue
		}
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
		a.copyable(c, col, ui.Text(c, text).FontSize(t.Rem(0.8)))
	})
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

func (a *app) composer(c *ui.Context, t *ui.Theme) {
	a.commandPalette(c, t)
	ui.Row(c).Padding(t.Space(1.5), t.Space(3)).Gap(t.Space(2)).AlignItems(ui.End).
		BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		input := ui.TextAreaBase(c, &a.prompt).Lines(2, 7).Grow(1).AutoFocus().
			Padding(t.Space(1), t.Space(2)).Radius(t.Space(2)).
			Background(t.Surface).Border(1, t.Border).Disabled(a.client == nil)
		if input.Focused() {
			input = input.BorderColor(t.Accent)
		}
		if a.focusInput {
			a.focusInput = false
			input.Focus()
		}
		input.Placeholder("Ask Maki… (Enter to send, Shift+Enter for a newline)").OnTextKey(func(k ui.TextKey) bool {
			if a.handleEsc(c, k) {
				return true
			}
			if k.Key == ui.KeyC && k.Mods == ui.Cmd {
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
		})
	})
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
		ui.Text(c, f.Title).Bold()
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
			labels := make([]string, len(f.Options))
			for i, opt := range f.Options {
				labels[i] = opt.Label
			}
			v := a.answers[f.Name]
			if v == "" {
				v = f.Options[0].Label
			}
			ui.Select(c, &v, labels)
			a.answers[f.Name] = v
		}
	})
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
