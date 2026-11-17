package main

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"gomaki/internal/acpclient"
	"gomaki/internal/version"
)

func newApp() *app {
	return &app{
		sessionPick: -1,
		settings:    defaultSettings,
		md:          markdown.Options{ShareSelection: true},
	}
}

type entryKind int

const (
	entryUser entryKind = iota
	entryAgent
	entryThought
	entryTool
)

type entry struct {
	Started    time.Time
	Text       string
	ToolID     string
	Status     string
	ToolInput  string
	ToolOutput string
	Plan       []acpclient.PlanEntry
	Kind       entryKind
	Expanded   bool
}

type app struct {
	scroll ui.ScrollState
	escAt  time.Time

	win    *mygo.Window
	client *acpclient.Client
	events <-chan acpclient.Event

	permission *acpclient.PermissionRequest

	elicitation  *acpclient.ElicitationRequest
	answers      map[string]string
	multiAnswers map[string]map[string]bool

	attach *acpclient.Attachment

	md markdown.Options

	notice      string
	lastPrompt  string
	commandsCwd string

	cwd       string
	branch    string
	sessionID string
	prompt    string
	status    string
	err       string

	attachName string

	model     string
	mode      string
	resumeErr string

	costCurrency string

	selectedText string

	copyPending string

	sessionsList ui.ListState

	settings settings

	commands []customCommand

	transcript []entry

	models []string
	modes  []string

	sessions []acpclient.SessionInfo
	families []string
	plan     []acpclient.PlanEntry

	sessionPick int

	contextUsed  int
	contextSize  int
	costAmount   float64
	planExpanded bool

	continueLast bool
	focusInput   bool

	sessionsLoading bool

	stopping       bool
	busy           bool
	resumed        bool
	configSeen     bool
	permissionOpen bool
	menuQueued     bool
	resumeOpen     bool
	resumeLoading  bool

	elicitationOpen bool
	settingsOpen    bool
	runningOpen     bool
	quitting        bool
}

func (a *app) startup() {
	a.refreshBranch()
	go func() {
		c, err := acpclient.Start()
		if err != nil {
			a.win.Update(func() {
				a.err = "Cannot start Maki. Check that it is installed and on PATH.\n" + err.Error()
			})
			return
		}
		a.win.Update(func() {
			a.client = c
			a.events = c.Events()
			a.status = ""
			a.syncMenu()
			if a.continueLast {
				a.resumeLast()
			} else {
				a.loadSessions(false)
				// Open a session right away: ACP only reports models and modes
				// for a session, so this fills the menus before the first prompt.
				a.startNewSession(a.cwd)
			}
		})
		go a.pump(c)
	}()
}

func (a *app) pump(c *acpclient.Client) {
	for range c.Wake() {
		a.win.Invalidate()
	}
}

func (a *app) drain() {
	for {
		select {
		case e := <-a.events:
			a.apply(e)
		default:
			return
		}
	}
}

// reload restarts the app and resumes the newest session.
func (a *app) reload() {
	mygo.App.Relaunch("-c")
}

func (a *app) showAbout() {
	mygo.App.ShowAboutPanel(mygo.AboutPanelOptions{
		ApplicationName:    appName,
		ApplicationVersion: version.String(),
		Credits:            "A desktop client for Maki's ACP.",
	})
}

// confirmQuit asks before quitting while the agent is working.
func (a *app) confirmQuit() {
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:             a.win,
			Type:               mygo.MessageWarning,
			Title:              "Maki is still working",
			Message:            "Quit GoMaki and stop the current turn?",
			Detail:             "The agent is still working. Quitting stops it.",
			Buttons:            []string{"Cancel", "Quit"},
			DefaultButton:      0,
			CancelButton:       0,
			DestructiveButtons: []int{1},
		})
		if err != nil || res.Button != 1 {
			return
		}
		a.quitting = true
		mygo.App.Quit()
	}()
}

// refreshBranch reads the workspace's Git branch for the status bar.
func (a *app) refreshBranch() {
	cwd := a.cwd
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD").Output()
		branch := ""
		if err == nil {
			branch = strings.TrimSpace(string(out))
			if branch == "HEAD" {
				branch = ""
			}
		}
		a.win.Update(func() { a.branch = branch })
	}()
}

func (a *app) askPermission(p *acpclient.PermissionRequest) {
	if a.permission != nil {
		a.permission.Reply <- ""
	}
	a.permission = p
	a.permissionOpen = true
	title := p.Title
	if title == "" {
		title = "Permission required"
	}
	a.notify(appName, title)
}

// attachLimit keeps a chosen file from filling memory; ACP carries images
// inline as base64.
const attachLimit = 20 << 20

// attachFile asks for a file to send with the next prompt.
func (a *app) attachFile() {
	if a.client == nil {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:  a.win,
			Title:   "Attach a File",
			Filters: []mygo.FileFilter{{Name: "Images and files", Extensions: []string{"*"}}},
		})
		a.win.Update(func() {
			if err != nil || len(paths) == 0 {
				return
			}
			a.loadAttachment(paths[0])
		})
	}()
}

func (a *app) loadAttachment(path string) {
	info, err := os.Stat(path)
	if err != nil {
		a.err = err.Error()
		return
	}
	if info.Size() > attachLimit {
		a.notice = "File is larger than 20 MB"
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		a.err = err.Error()
		return
	}
	mime := http.DetectContentType(data)
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = mime[:i]
	}
	a.attach = &acpclient.Attachment{Path: path, MimeType: mime, Data: data}
	a.attachName = filepath.Base(path)
}

func (a *app) clearAttachment() {
	a.attach = nil
	a.attachName = ""
}

func (a *app) apply(e acpclient.Event) {
	switch e.Kind {
	case acpclient.EventUserText:
		a.appendText(entryUser, e.Text)
	case acpclient.EventText:
		a.appendText(entryAgent, e.Text)
	case acpclient.EventThought:
		a.appendText(entryThought, e.Text)
	case acpclient.EventToolCall:
		status := e.Status
		if status == "" {
			status = toolPending
		}
		a.transcript = append(a.transcript, entry{
			Kind:       entryTool,
			Text:       e.Title,
			ToolID:     e.ToolCallID,
			Status:     status,
			ToolInput:  e.ToolInput,
			ToolOutput: e.ToolOutput,
			Started:    time.Now(),
		})
	case acpclient.EventToolCallUpdate:
		a.updateToolCall(e)
	case acpclient.EventPlan:
		a.updatePlan(e.Plan)
	case acpclient.EventConfig:
		a.configSeen = true
		a.models = e.Models
		if e.Model != "" {
			a.model = e.Model
		}
		a.syncMenu()
	case acpclient.EventMode:
		a.mode = e.Mode
		a.syncMenu()
	case acpclient.EventPermission:
		a.askPermission(e.Permission)
	case acpclient.EventElicitation:
		a.startElicitation(e.Elicitation)
	case acpclient.EventUsage:
		a.contextUsed, a.contextSize = e.ContextUsed, e.ContextSize
		a.costAmount, a.costCurrency = e.CostAmount, e.CostCurrency
	case acpclient.EventPromptDone:
		a.promptDone(e)
	}
}

func (a *app) promptDone(e acpclient.Event) {
	a.busy = false
	a.status = ""
	a.escAt = time.Time{}
	if a.stopping || e.Err != nil {
		restored := false
		if a.prompt == "" && a.lastPrompt != "" {
			a.prompt = a.lastPrompt
			restored = true
		}
		switch {
		case a.stopping && restored:
			a.notice = "Stopped — prompt restored"
		case a.stopping:
			a.notice = "Stopped"
		case restored:
			a.notice = "Draft restored"
		default:
			a.notice = "Prompt failed"
		}
		a.stopping = false
	} else {
		a.notice = ""
		a.notify(appName, "Turn finished.")
	}
	a.lastPrompt = ""
	if e.Err != nil {
		a.err = e.Err.Error()
	}
}

// notify shows a desktop notification while the window is in the background,
// so a permission request or a finished turn is not missed.
func (a *app) notify(title, body string) {
	if a.win == nil || a.win.IsFocused() || !mygo.NotificationsSupported() {
		return
	}
	go func() {
		_ = mygo.NewNotification(mygo.NotificationOptions{Title: title, Body: body}).Show()
	}()
}

func (a *app) appendText(kind entryKind, text string) {
	if n := len(a.transcript); n > 0 && a.transcript[n-1].Kind == kind {
		a.transcript[n-1].Text += text
		return
	}
	a.transcript = append(a.transcript, entry{Kind: kind, Text: text})
}

func (a *app) updateToolCall(e acpclient.Event) {
	for i := len(a.transcript) - 1; i >= 0; i-- {
		if a.transcript[i].Kind != entryTool || a.transcript[i].ToolID != e.ToolCallID {
			continue
		}
		if e.Title != "" {
			a.transcript[i].Text = e.Title
		}
		if e.Status != "" {
			a.transcript[i].Status = e.Status
		}
		if e.ToolInput != "" {
			a.transcript[i].ToolInput = e.ToolInput
		}
		if e.ToolOutput != "" {
			a.transcript[i].ToolOutput = e.ToolOutput
		}
		return
	}
}

// runningTools returns the transcript indexes of tool calls still running.
func (a *app) runningTools() []int {
	var idx []int
	for i := range a.transcript {
		if a.transcript[i].Kind == entryTool && a.transcript[i].Status == toolRunning {
			idx = append(idx, i)
		}
	}
	return idx
}

func (a *app) updatePlan(plan []acpclient.PlanEntry) {
	a.plan = plan
}

func (a *app) startNewSession(cwd string) {
	if a.client == nil {
		return
	}
	a.cwd = cwd
	a.refreshBranch()
	a.reset()
	a.status = "Starting session…"
	go func() {
		s, err := a.client.NewSession(cwd)
		a.win.Update(func() {
			if err != nil {
				a.status = ""
				a.err = err.Error()
				return
			}
			a.applySession(s)
			a.status = ""
		})
	}()
}

func (a *app) resumeLast() {
	cwd := a.cwd
	a.status = "Loading session…"
	go func() {
		sessions, err := a.client.ListSessions(cwd)
		a.win.Update(func() {
			a.status = ""
			if err != nil || len(sessions) == 0 {
				a.startNewSession(cwd)
				return
			}
			a.resume(sessions[0].ID)
		})
	}()
}

func (a *app) resume(id string) {
	if a.client == nil || a.resumeLoading {
		return
	}
	cwd := a.cwd
	a.reset()
	a.status = "Loading session…"
	a.resumeLoading = true
	a.resumeErr = ""
	go func() {
		s, err := a.client.LoadSession(cwd, id)
		a.win.Update(func() {
			a.resumeLoading = false
			a.status = ""
			if err != nil {
				a.resumeErr = err.Error()
				a.err = err.Error()
				return
			}
			a.resumeOpen = false
			a.resumed = true
			a.applySession(s)
		})
	}()
}

func (a *app) reset() {
	a.transcript = nil
	a.plan = nil
	a.planExpanded = false
	a.prompt = ""
	a.busy = false
	a.err = ""
	a.notice = ""
	a.lastPrompt = ""
	a.stopping = false
	a.escAt = time.Time{}
	a.contextUsed, a.contextSize = 0, 0
	a.costAmount, a.costCurrency = 0, ""
	a.configSeen = false
	a.resumed = false
	a.sessionPick = -1
	a.scroll = ui.ScrollState{}
	a.clearAttachment()
}

func (a *app) applySession(s acpclient.Session) {
	a.sessionID = s.ID
	if !a.configSeen {
		a.models = s.Models
		a.model = s.Model
	}
	a.modes = s.Modes
	a.mode = s.Mode
	a.syncMenu()
}

func (a *app) pickDirectory() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      a.win,
			Title:       "Choose a working directory",
			DefaultPath: a.cwd,
			Directory:   true,
		})
		if err != nil || len(paths) == 0 {
			return
		}
		a.win.Update(func() { a.startNewSession(paths[0]) })
	}()
}

func (a *app) openSessions() {
	a.loadSessions(true)
}

func (a *app) loadSessions(open bool) {
	if a.client == nil {
		return
	}
	cwd := a.cwd
	a.sessionsLoading = true
	if open {
		a.status = "Loading sessions…"
	}
	go func() {
		sessions, err := a.client.ListSessions(cwd)
		a.win.Update(func() {
			a.sessionsLoading = false
			if open {
				a.status = ""
			}
			if err != nil {
				a.err = err.Error()
				return
			}
			filtered := make([]acpclient.SessionInfo, 0, len(sessions))
			for _, s := range sessions {
				if s.ID != a.sessionID {
					filtered = append(filtered, s)
				}
			}
			a.sessions = filtered
			a.sessionsList = ui.ListState{}
			a.sessionPick = -1
			a.resumeLoading = false
			a.resumeErr = ""
			if open {
				a.resumeOpen = true
			}
		})
	}()
}

func (a *app) rewindLastTurn() {
	idx := -1
	for i := range slices.Backward(a.transcript) {
		if a.transcript[i].Kind == entryUser {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	text := a.transcript[idx].Text
	a.transcript = a.transcript[:idx]
	a.prompt = text
	a.lastPrompt = ""
	a.err = ""
	a.notice = "Last prompt restored"
}

func (a *app) stopPrompt() {
	if a.client == nil || !a.busy {
		return
	}
	a.stopping = true
	a.notice = "Stopping…"
	go func() { _ = a.client.Cancel() }()
	if a.win != nil {
		a.win.Invalidate()
	}
}

func (a *app) send(text string) bool {
	text = strings.TrimSpace(text)
	if text == "/reload" {
		a.prompt = ""
		a.reload()
		return true
	}
	if text == "" || a.busy || a.client == nil {
		return false
	}
	a.prompt = ""
	a.err = ""
	a.notice = ""
	a.lastPrompt = text
	a.escAt = time.Time{}
	att := a.attach
	display := text
	if a.attachName != "" {
		display += "\n\n[" + a.attachName + "]"
	}
	mark := len(a.transcript)
	a.transcript = append(a.transcript, entry{Kind: entryUser, Text: display})
	a.busy = true
	a.status = "Working…"
	agentText := a.expandedPrompt(text)
	if a.sessionID == "" {
		a.startSessionAndPrompt(agentText, text, att, mark)
		return true
	}
	if err := a.client.Prompt(agentText, att); err != nil {
		a.failSend(text, mark, err)
		return false
	}
	a.clearAttachment()
	return true
}

func (a *app) startSessionAndPrompt(prompt, display string, att *acpclient.Attachment, mark int) {
	cwd := a.cwd
	go func() {
		s, err := a.client.NewSession(cwd)
		a.win.Update(func() {
			if err != nil {
				a.failSend(display, mark, err)
				return
			}
			a.applySession(s)
			if a.stopping {
				a.abortSend(display, mark)
				return
			}
			if err := a.client.Prompt(prompt, att); err != nil {
				a.failSend(display, mark, err)
				return
			}
			a.clearAttachment()
		})
	}()
}

func (a *app) failSend(text string, mark int, err error) {
	a.transcript = a.transcript[:min(mark, len(a.transcript))]
	a.busy = false
	a.status = ""
	a.err = err.Error()
	a.notice = "Draft restored"
	a.prompt = text
	a.lastPrompt = ""
}

func (a *app) abortSend(text string, mark int) {
	a.transcript = a.transcript[:min(mark, len(a.transcript))]
	a.busy = false
	a.status = ""
	a.stopping = false
	a.notice = "Stopped — prompt restored"
	a.prompt = text
	a.lastPrompt = ""
}

func (a *app) switchModel(model string) {
	prev := a.model
	a.model = model
	a.syncMenu()
	a.win.Invalidate()
	go func() {
		if err := a.client.SetModel(model); err != nil {
			a.win.Update(func() {
				a.model = prev
				a.err = err.Error()
				a.syncMenu()
			})
		}
	}()
}

// nextMode returns the mode after the current one, or "" when there is
// nothing to switch to.
func (a *app) nextMode() string {
	if len(a.modes) < 2 {
		return ""
	}
	return a.modes[(slices.Index(a.modes, a.mode)+1)%len(a.modes)]
}

func (a *app) switchMode(mode string) {
	prev := a.mode
	a.mode = mode
	a.syncMenu()
	a.win.Invalidate()
	go func() {
		if err := a.client.SetMode(mode); err != nil {
			a.win.Update(func() {
				a.mode = prev
				a.err = err.Error()
				a.syncMenu()
			})
		}
	}()
}

func (a *app) answerPermission(option string) {
	req := a.permission
	a.permission = nil
	a.permissionOpen = false
	if req != nil {
		req.Reply <- option
	}
}

func (a *app) startElicitation(req *acpclient.ElicitationRequest) {
	if a.elicitation != nil {
		a.elicitation.Reply <- acpclient.ElicitationReply{Decline: true}
	}
	a.elicitation = req
	a.elicitationOpen = true
	a.answers = make(map[string]string)
	a.multiAnswers = make(map[string]map[string]bool)
	for _, f := range req.Fields {
		if len(f.Options) > 0 && !f.Multi {
			a.answers[f.Name] = f.Options[0].Label
		}
	}
}

func (a *app) answerElicitation(decline bool) {
	req := a.elicitation
	a.elicitation = nil
	a.elicitationOpen = false
	if req == nil {
		return
	}
	if decline {
		req.Reply <- acpclient.ElicitationReply{Decline: true}
		return
	}
	req.Reply <- acpclient.ElicitationReply{Answers: a.elicitationAnswers(req)}
}

func (a *app) elicitationAnswers(req *acpclient.ElicitationRequest) map[string]any {
	answers := make(map[string]any)
	for _, f := range req.Fields {
		if f.Multi {
			if chosen := a.multiValue(f); len(chosen) > 0 {
				answers[f.Name] = chosen
			}
			continue
		}
		if value, ok := a.singleValue(f); ok {
			answers[f.Name] = value
		}
	}
	return answers
}

func (a *app) multiValue(f acpclient.ElicitationField) []string {
	var chosen []string
	for _, opt := range f.Options {
		if a.multiAnswers[f.Name][opt.Value] {
			chosen = append(chosen, opt.Value)
		}
	}
	return chosen
}

func (a *app) singleValue(f acpclient.ElicitationField) (string, bool) {
	label := a.answers[f.Name]
	if label == "" {
		return "", false
	}
	for _, opt := range f.Options {
		if opt.Label == label {
			return opt.Value, true
		}
	}
	return label, true
}
