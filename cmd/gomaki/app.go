package main

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"gomaki/internal/acpclient"
	"gomaki/internal/session"
	"gomaki/internal/version"
)

func newApp() *app {
	return &app{
		sessionPick: -1,
		settings:    defaultSettings,
		md:          markdown.Options{ShareSelection: true},
		toolMD:      markdown.Options{ShareSelection: true, WrapCode: true},
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
	ToolName   string
	Status     string
	ToolInput  string
	ToolOutput string
	ToolKind   string
	Todos      []acpclient.PlanEntry
	Kind       entryKind
	TodoList   bool
	Expanded   bool
}

type app struct {
	scroll        ui.ScrollState
	thoughtScroll ui.ScrollState
	escAt         time.Time

	win  *mygo.Window
	core *session.Core

	permission *acpclient.PermissionRequest

	elicitation  *acpclient.ElicitationRequest
	answers      map[string]string
	multiAnswers map[string]map[string]bool

	attach *acpclient.Attachment

	webServer *webServer

	md     markdown.Options
	toolMD markdown.Options

	notice string

	cwd       string
	branch    string
	sessionID string
	prompt    string
	status    string
	err       string

	attachName string

	model     string
	mode      string
	title     string
	resumeErr string

	costCurrency string

	selectedText string

	copyPending  string
	toastPending string

	planPath string

	password string
	listen   string
	httpErr  string

	todoToolID    string
	todoDismissed string

	sessionsList ui.ListState

	paletteList ui.ListState

	settings settings

	commands    []customCommand
	acpCommands []acpclient.Command

	transcript []entry

	models []string
	modes  []string

	sessions []acpclient.SessionInfo
	families []string
	plan     []acpclient.PlanEntry
	queue    []session.QueuedPrompt

	httpURLs []string

	sessionPick  int
	paletteIndex int

	contextUsed int64
	contextSize int64
	costAmount  float64
	port        int

	connected bool

	planReady bool

	planExpanded bool
	todoExpanded bool

	continueLast bool
	focusInput   bool

	sessionsLoading bool

	busy           bool
	resumed        bool
	permissionOpen bool
	menuQueued     bool
	resumeOpen     bool
	resumeLoading  bool

	elicitationOpen bool
	settingsOpen    bool
	runningOpen     bool
	quitting        bool

	httpMode bool
}

func (a *app) startup() {
	go func() {
		core := session.New()
		if err := core.Start(a.cwd); err != nil {
			a.win.Update(func() {
				a.err = "Cannot start Maki. Check that it is installed and on PATH.\n" + err.Error()
			})
			return
		}
		a.win.Update(func() {
			a.core = core
			a.connected = true
			a.status = ""
			a.loadCommands()
			a.syncMenu()
			if a.continueLast {
				a.resumeLast()
			} else {
				a.loadSessions(false)
				// Open a session right away: ACP only reports models and modes
				// for a session, so this fills the menus before the first prompt.
				a.startNewSession(a.cwd)
			}
			if a.password != "" {
				a.startHTTP()
			}
		})
		go a.watch(core)
	}()
}

// watch applies core updates to the app's mirror and runs the side effects
// that need the main thread.
func (a *app) watch(core *session.Core) {
	for {
		ch := core.Subscribe()
		for u := range ch {
			if u.Dirty {
				u.State = core.Snapshot()
			}
			a.handleUpdate(u)
		}
		if core.Closed() {
			return
		}
	}
}

func (a *app) handleUpdate(u session.Update) {
	switch u.Kind {
	case session.UpdateReset:
		a.win.Update(func() {
			a.clearUI()
			a.syncTitle("")
		})
	case session.UpdateRestoreDraft:
		a.win.Update(func() { a.prompt = u.Text })
	case session.UpdateEvent:
		a.handleEvent(u)
	case session.UpdateState:
		a.win.Update(func() {
			a.models = u.State.Models
			a.modes = u.State.Modes
			a.model = u.State.Model
			a.mode = u.State.Mode
			a.syncTitle(u.State.Title)
			if u.State.Err != "" {
				a.err = u.State.Err
			}
			a.syncMenu()
		})
	case session.UpdateSnapshot:
	}
	a.win.Invalidate()
}

func (a *app) handleEvent(u session.Update) {
	switch u.Event.Kind {
	case acpclient.EventPermission:
		a.win.Update(func() {
			a.permissionOpen = true
			title := u.Event.Permission.Title
			if title == "" {
				title = "Permission required"
			}
			a.notify(appName, title)
		})
	case acpclient.EventElicitation:
		a.win.Update(func() { a.startElicitation(u.Event.Elicitation) })
	case acpclient.EventConfig:
		a.win.Update(func() {
			a.models = u.Event.Models
			if u.Event.Model != "" {
				a.model = u.Event.Model
			}
			a.syncMenu()
		})
	case acpclient.EventMode:
		a.win.Update(func() {
			a.mode = u.Event.Mode
			a.syncMenu()
		})
	case acpclient.EventPromptDone:
		a.win.Update(func() {
			a.notice = u.State.Notice
			a.escAt = time.Time{}
			if u.State.Err != "" {
				a.err = u.State.Err
			}
		})
		if u.State.Notice == "" {
			a.notify(appName, "Turn finished.")
		}
	case acpclient.EventUserText, acpclient.EventText, acpclient.EventThought,
		acpclient.EventToolCall, acpclient.EventToolCallUpdate, acpclient.EventPlan,
		acpclient.EventUsage, acpclient.EventCommands:
	}
}

// clearUI drops the renderer-local state a session reset invalidates.
func (a *app) clearUI() {
	a.prompt = ""
	a.err = ""
	a.notice = ""
	a.escAt = time.Time{}
	a.scroll = ui.ScrollState{}
	a.thoughtScroll = ui.ScrollState{}
	a.sessionPick = -1
	a.paletteIndex = 0
	a.planExpanded = false
	a.todoToolID = ""
	a.todoExpanded = false
	a.todoDismissed = ""
	a.permissionOpen = false
	a.elicitationOpen = false
	a.answers = nil
	a.multiAnswers = nil
	a.clearAttachment()
}

// mirror copies the core snapshot into the app's fields. The core is the
// source of truth; these fields are a per-frame cache for the renderers.
func (a *app) mirror() {
	if a.core == nil {
		return
	}
	s := a.core.Snapshot()
	a.transcript = a.mergeTranscript(s.Transcript)
	a.plan = s.Plan
	a.models = s.Models
	a.modes = s.Modes
	a.model = s.Model
	a.mode = s.Mode
	a.busy = s.Busy
	a.status = s.Status
	a.sessionID = s.SessionID
	a.cwd = s.Cwd
	a.branch = s.Branch
	a.contextUsed = s.ContextUsed
	a.contextSize = s.ContextSize
	a.costAmount = s.CostAmount
	a.costCurrency = s.CostCurrency
	a.planPath = s.PlanPath
	a.planReady = s.PlanReady
	a.sessions = s.Sessions
	a.acpCommands = s.Commands
	a.permission = s.Permission
	a.elicitation = s.Elicitation
	a.resumed = s.Resumed
	a.queue = s.Queue
}

// mergeTranscript converts the core transcript and keeps the renderer-local
// expansion state.
func (a *app) mergeTranscript(src []session.Entry) []entry {
	expanded := make(map[string]bool)
	for _, e := range a.transcript {
		if e.Kind == entryTool && e.Expanded {
			expanded[e.ToolID] = true
		}
	}
	out := make([]entry, len(src))
	for i, e := range src {
		out[i] = entry{
			Started:    e.Started,
			Text:       e.Text,
			ToolID:     e.ToolID,
			ToolName:   e.ToolName,
			Status:     e.Status,
			ToolInput:  e.ToolInput,
			ToolOutput: e.ToolOutput,
			ToolKind:   e.ToolKind,
			Todos:      e.Todos,
			TodoList:   e.TodoList,
			Kind:       entryKind(e.Kind),
		}
		if e.Kind == session.EntryTool {
			out[i].Expanded = expanded[e.ToolID]
		} else if i < len(a.transcript) && a.transcript[i].Kind == out[i].Kind {
			out[i].Expanded = a.transcript[i].Expanded
		}
	}
	return out
}

// syncTodo pins the newest todo list and resets the panel when a new list
// replaces it.
func (a *app) syncTodo() {
	id, _, ok := a.latestTodos()
	if !ok || id == "" || id == a.todoToolID {
		return
	}
	a.todoToolID = id
	a.todoExpanded = false
	a.todoDismissed = ""
}

// latestTodos returns the newest todo tool call and its list.
func (a *app) latestTodos() (string, []acpclient.PlanEntry, bool) {
	for i := len(a.transcript) - 1; i >= 0; i-- {
		e := &a.transcript[i]
		if e.Kind == entryTool && e.TodoList {
			return e.ToolID, e.Todos, true
		}
	}
	return "", nil, false
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

// attachLimit keeps a chosen file from filling memory; ACP carries images
// inline as base64.
const attachLimit = 20 << 20

// attachFile asks for a file to send with the next prompt.
func (a *app) attachFile() {
	if a.core == nil {
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

// syncTitle puts the session title in the window title bar. Call it on the
// main thread; it skips the native call when nothing changed.
func (a *app) syncTitle(title string) {
	if title == a.title || a.win == nil {
		return
	}
	a.title = title
	if title == "" {
		a.win.SetTitle(appName)
		return
	}
	a.win.SetTitle(appName + " (" + title + ")")
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

func (a *app) startNewSession(cwd string) {
	if a.core == nil {
		return
	}
	go func() { _ = a.core.NewSession(cwd) }()
}

func (a *app) resumeLast() {
	if a.core == nil {
		return
	}
	go func() {
		sessions, err := a.core.ListSessions()
		if err != nil || len(sessions) == 0 {
			a.startNewSession(a.cwd)
			return
		}
		a.resume(sessions[0].ID)
	}()
}

func (a *app) resume(id string) {
	if a.core == nil || a.resumeLoading {
		return
	}
	a.resumeLoading = true
	a.resumeErr = ""
	go func() {
		err := a.core.LoadSession(id)
		a.win.Update(func() {
			a.resumeLoading = false
			if err != nil {
				a.resumeErr = err.Error()
				return
			}
			a.resumeOpen = false
		})
	}()
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
	if a.core == nil {
		return
	}
	a.sessionsLoading = true
	go func() {
		_, err := a.core.ListSessions()
		a.win.Update(func() {
			a.sessionsLoading = false
			a.sessionsList = ui.ListState{}
			a.sessionPick = -1
			a.resumeLoading = false
			a.resumeErr = ""
			if err != nil {
				a.err = err.Error()
				return
			}
			if open {
				a.resumeOpen = true
			}
		})
	}()
}

func (a *app) rewindLastTurn() {
	if a.core == nil {
		return
	}
	if text, ok := a.core.RewindLastTurn(); ok {
		a.prompt = text
		a.notice = "Last prompt restored"
	}
}

func (a *app) stopPrompt() {
	if a.core == nil {
		return
	}
	a.notice = "Stopping…"
	a.core.Cancel()
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
	if text == "/new" {
		a.prompt = ""
		a.startNewSession(a.cwd)
		return true
	}
	if text == "" || a.core == nil {
		return false
	}
	att := a.attach
	display := text
	if a.attachName != "" {
		display += "\n\n[" + a.attachName + "]"
	}
	if !a.core.Send(a.expandedPrompt(text), display, att) {
		return false
	}
	a.prompt = ""
	a.clearAttachment()
	return true
}

// sendPlanChange hands the accepted plan to the OpenSpec fast-forward
// workflow. The mode switch is awaited so the agent may write the artifacts.
func (a *app) sendPlanChange() {
	path := a.planPath
	cmd, _, ok := a.matchCommand("/opsx:ff")
	if !ok || path == "" || a.busy || a.core == nil {
		return
	}
	prompt := planChangePrompt(cmd, path)
	a.planReady = false
	go func() {
		if err := a.core.SetMode("build"); err != nil {
			return
		}
		a.core.Send(prompt, "/opsx:ff (from plan)", nil)
	}()
}

func planChangePrompt(cmd customCommand, path string) string {
	return cmd.render("(derive the change name from the accepted plan at "+path+")") +
		"\n\n**The accepted plan**: " + path +
		" — read this file first; its contents are the plan to turn into an OpenSpec change.\n" +
		"Do not re-litigate the plan's decisions: generate the artifacts from it."
}

func (a *app) switchModel(model string) {
	if a.core == nil {
		return
	}
	go func() { _ = a.core.SetModel(model) }()
	a.syncMenu()
	a.win.Invalidate()
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
	if a.core == nil {
		return
	}
	go func() { _ = a.core.SetMode(mode) }()
	a.syncMenu()
	a.win.Invalidate()
}

func (a *app) answerPermission(option string) {
	a.permissionOpen = false
	if a.core != nil {
		a.core.AnswerPermission(option)
	}
}

func (a *app) startElicitation(req *acpclient.ElicitationRequest) {
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
	a.elicitationOpen = false
	if req == nil || a.core == nil {
		return
	}
	if decline {
		a.core.AnswerElicitation(nil, true)
		return
	}
	a.core.AnswerElicitation(a.elicitationAnswers(req), false)
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

// popQueue pulls the first queued prompt back into the composer.
func (a *app) popQueue() {
	if a.core == nil {
		return
	}
	if text, ok := a.core.PopQueue(); ok {
		a.prompt = text
		a.focusInput = true
	}
}
