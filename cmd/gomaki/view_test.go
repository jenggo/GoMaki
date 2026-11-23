package main

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gomaki/internal/acpclient"
	"gomaki/internal/session"
)

func newTestApp() *app {
	return newApp()
}

func TestViewRendersTranscript(t *testing.T) {
	a := newTestApp()
	a.transcript = []entry{
		{Kind: entryUser, Text: "hello"},
		{Kind: entryAgent, Text: "hi there"},
		{Kind: entryTool, Text: "read", Status: toolCompleted},
	}
	tt := ui.NewTester(a.view, 900, 700)
	for _, want := range []string{"hello", "hi there", "read", "Done", "No model", "Connecting…"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestPermissionDialogAnswers(t *testing.T) {
	reply := make(chan string, 1)
	req := &acpclient.PermissionRequest{
		Title:   "Run ls",
		Options: []acpclient.PermissionOption{{ID: "allow_once", Name: "Allow once"}},
		Reply:   reply,
	}
	core := session.New()
	core.Handle(acpclient.Event{Kind: acpclient.EventPermission, Permission: req})
	a := newTestApp()
	a.core = core
	a.permission = req
	a.permissionOpen = true
	tt := ui.NewTester(a.view, 900, 700)
	if err := tt.Click("Allow once"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-reply:
		if got != "allow_once" {
			t.Fatalf("answer = %q", got)
		}
	default:
		t.Fatal("no permission answer sent")
	}
}

func TestSettingsDialogOpens(t *testing.T) {
	a := newTestApp()
	a.settingsOpen = true
	tt := ui.NewTester(a.view, 900, 700)
	if !tt.HasText("Theme") {
		t.Fatalf("settings did not open: %q", tt.Texts())
	}
}

func TestElicitationAnswers(t *testing.T) {
	reply := make(chan acpclient.ElicitationReply, 1)
	req := &acpclient.ElicitationRequest{
		Fields: []acpclient.ElicitationField{
			{Name: "q1", Title: "Pick one", Options: []acpclient.ElicitationOption{
				{Value: "A", Label: "A - first"},
				{Value: "B", Label: "B - second"},
			}},
			{Name: "q2", Title: "Pick many", Multi: true, Options: []acpclient.ElicitationOption{
				{Value: "X", Label: "X"},
				{Value: "Y", Label: "Y"},
			}},
			{Name: "q3", Title: "Type"},
		},
		Reply: reply,
	}
	core := session.New()
	core.Handle(acpclient.Event{Kind: acpclient.EventElicitation, Elicitation: req})
	a := newTestApp()
	a.core = core
	a.startElicitation(req)
	a.multiAnswers["q2"] = map[string]bool{"Y": true}
	a.answers["q3"] = "hello"
	a.answerElicitation(false)

	got := <-reply
	if got.Answers["q1"] != "A" {
		t.Errorf("q1 = %v", got.Answers["q1"])
	}
	if chosen, ok := got.Answers["q2"].([]string); !ok || len(chosen) != 1 || chosen[0] != "Y" {
		t.Errorf("q2 = %v", got.Answers["q2"])
	}
	if got.Answers["q3"] != "hello" {
		t.Errorf("q3 = %v", got.Answers["q3"])
	}
}

func TestElicitationDialogOpens(t *testing.T) {
	a := newTestApp()
	a.startElicitation(&acpclient.ElicitationRequest{
		Fields: []acpclient.ElicitationField{
			{Name: "q1", Title: "Pick one", Options: []acpclient.ElicitationOption{
				{Value: "A", Label: "A - first"},
			}},
			{Name: "q2", Title: "Type"},
		},
		Reply: make(chan acpclient.ElicitationReply, 1),
	})
	tt := ui.NewTester(a.view, 900, 700)
	for _, want := range []string{"Pick one", "A - first", "Type", "Submit", "Decline"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestEmptyState(t *testing.T) {
	a := newTestApp()
	tt := ui.NewTester(a.view, 900, 700)
	if !tt.HasText("Connecting to Maki…") {
		t.Fatalf("missing connecting state: %q", tt.Texts())
	}

	a = newTestApp()
	a.connected = true
	tt = ui.NewTester(a.view, 900, 700)
	if !tt.HasText("No stored sessions yet") {
		t.Fatalf("missing empty sessions message: %q", tt.Texts())
	}
	if tt.HasText("Previous sessions") {
		t.Fatalf("empty slate still shows a session list: %q", tt.Texts())
	}
}

func TestSessionPickerEmpty(t *testing.T) {
	a := newTestApp()
	a.connected = true
	a.resumeOpen = true
	tt := ui.NewTester(a.view, 900, 700)
	if !tt.HasText("No stored sessions yet") {
		t.Fatalf("missing empty picker message: %q", tt.Texts())
	}
}

func TestFirstScreenListsSessions(t *testing.T) {
	a := newTestApp()
	a.connected = true
	a.sessions = []acpclient.SessionInfo{{
		ID: "s1", Title: "Previous session", UpdatedAt: "2026-11-14T09:28:59Z",
	}}
	tt := ui.NewTester(a.view, 1000, 720)
	for _, want := range []string{"Previous sessions", "Previous session"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestSessionClickChoosesRow(t *testing.T) {
	a := newTestApp()
	a.sessions = []acpclient.SessionInfo{
		{ID: "s1", Title: "First session", UpdatedAt: "2026-11-14T09:28:59Z"},
	}
	a.resumeOpen = true
	tt := ui.NewTester(a.view, 1000, 720)
	if err := tt.Click("First session"); err != nil {
		t.Fatal(err)
	}
	if a.sessionPick != 0 {
		t.Fatalf("click did not choose the session: pick = %d", a.sessionPick)
	}
}

func TestTranscriptTextIsCopyable(t *testing.T) {
	a := newTestApp()
	a.transcript = []entry{
		{Kind: entryAgent, Text: "copy this sentence"},
		{Kind: entryAgent, Text: "other message"},
	}
	tt := ui.NewTester(a.view, 1000, 720)
	r, ok := tt.Find("copy this sentence")
	if !ok {
		t.Fatal("agent text not found")
	}
	tt.Press(r.X+2, r.Y+r.H/2)
	tt.Move(r.X+r.W-2, r.Y+r.H/2)
	tt.Release(r.X+r.W-2, r.Y+r.H/2)
	tt.Command("copy")
	if got := tt.Clipboard(); !strings.Contains(got, "copy this sentence") {
		t.Fatalf("clipboard = %q", got)
	}
}

func TestRightClickCopiesSelection(t *testing.T) {
	a := newTestApp()
	a.transcript = []entry{
		{Kind: entryAgent, Text: "copy this sentence"},
		{Kind: entryAgent, Text: "other message"},
	}
	tt := ui.NewTester(a.view, 1000, 720)
	r, ok := tt.Find("copy this sentence")
	if !ok {
		t.Fatal("agent text not found")
	}
	tt.Press(r.X+2, r.Y+r.H/2)
	tt.Move(r.X+r.W-2, r.Y+r.H/2)
	tt.Release(r.X+r.W-2, r.Y+r.H/2)
	tt.Frame()

	if err := tt.RightClick("copy this sentence"); err != nil {
		t.Fatal(err)
	}
	if got := tt.Clipboard(); got != "copy this sentence" {
		t.Fatalf("clipboard after right-click = %q", got)
	}
	if a.notice != "Copied selection" {
		t.Fatalf("notice = %q", a.notice)
	}
}

func TestRightClickCopiesAllWithoutSelection(t *testing.T) {
	a := newTestApp()
	a.transcript = []entry{
		{Kind: entryAgent, Text: "first message"},
		{Kind: entryAgent, Text: "second message"},
	}
	tt := ui.NewTester(a.view, 1000, 720)
	if err := tt.RightClick("first message"); err != nil {
		t.Fatal(err)
	}
	if got := tt.Clipboard(); got != "first message\nsecond message" {
		t.Fatalf("clipboard = %q", got)
	}
	if a.notice != "Copied all text" {
		t.Fatalf("notice = %q", a.notice)
	}
}

func TestToolCallExpands(t *testing.T) {
	a := newTestApp()
	a.transcript = []entry{{
		Kind:       entryTool,
		Text:       "read go.mod",
		Status:     toolCompleted,
		ToolInput:  `{"path":"go.mod"}`,
		ToolOutput: "module gomaki",
	}}
	tt := ui.NewTester(a.view, 1000, 720)
	if tt.HasText("module gomaki") {
		t.Fatal("tool output showed before the click")
	}
	r, ok := tt.Find("read go.mod")
	if !ok {
		t.Fatal("tool title not found")
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	if !a.transcript[0].Expanded {
		t.Fatal("click did not expand the tool call")
	}
	if !tt.HasText("module gomaki") {
		t.Fatalf("output missing after expand: %q", tt.Texts())
	}
}

func TestTodoToolCallRendersList(t *testing.T) {
	a := newTestApp()
	input := `{"todos":[{"content":"write tests","status":"completed"},{"content":"ship it","status":"pending"}]}`
	a.transcript = []entry{{
		Kind:      entryTool,
		Text:      "2 todos",
		Status:    toolCompleted,
		ToolInput: input,
		Todos:     session.ParseTodos(input),
	}}
	tt := ui.NewTester(a.view, 1000, 720)
	tt.Frame()
	if tt.HasText("write tests") || tt.HasText("ship it") {
		t.Fatal("todo list was expanded by default")
	}
	r, ok := tt.Find("2 todos")
	if !ok {
		t.Fatal("todo tool header not found")
	}
	tt.ClickAt(r.X+2, r.Y+r.H/2)
	if !tt.HasText("write tests") || !tt.HasText("ship it") {
		t.Fatalf("todo items missing: %q", tt.Texts())
	}
	if tt.HasText(`"todos"`) {
		t.Fatal("raw JSON shown for a todo tool call")
	}
}

func TestParseTodos(t *testing.T) {
	todos := session.ParseTodos(`{"todos":[{"content":"a","status":"pending"}]}`)
	if len(todos) != 1 || todos[0].Content != "a" || todos[0].Status != "pending" {
		t.Fatalf("todos = %+v", todos)
	}
	if session.ParseTodos(`{"path":"go.mod"}`) != nil {
		t.Fatal("non-todo input parsed as todos")
	}
}

func TestToolCallPointerCursor(t *testing.T) {
	a := newTestApp()
	a.transcript = []entry{{
		Kind:      entryTool,
		Text:      "read go.mod",
		Status:    toolCompleted,
		ToolInput: `{"path":"go.mod"}`,
	}}
	tt := ui.NewTester(a.view, 1000, 720)
	r, ok := tt.Find("read go.mod")
	if !ok {
		t.Fatal("tool title not found")
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	if got := tt.Cursor(); got != ui.CursorPointer {
		t.Fatalf("cursor = %v, want pointer", got)
	}
}

func TestRightClickCopiesToolCall(t *testing.T) {
	a := newTestApp()
	a.transcript = []entry{{
		Kind:      entryTool,
		Text:      "read go.mod",
		Status:    toolCompleted,
		ToolInput: `{"path":"go.mod"}`,
	}}
	tt := ui.NewTester(a.view, 1000, 720)
	r, ok := tt.Find("read go.mod")
	if !ok {
		t.Fatal("tool title not found")
	}
	tt.RightClickAt(r.X+r.W/2, r.Y+r.H/2)
	if got := tt.Clipboard(); got != "read go.mod" {
		t.Fatalf("clipboard = %q", got)
	}
}

func TestMissingMakiError(t *testing.T) {
	a := newTestApp()
	a.err = "Cannot start Maki. Check that it is installed and on PATH."
	tt := ui.NewTester(a.view, 1000, 720)
	for _, want := range []string{"Cannot start Maki", "Maki unavailable", "Retry"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestCtrlCClearsComposer(t *testing.T) {
	a := newTestApp()
	a.connected = true
	tt := ui.NewTester(a.view, 1000, 720)
	tt.Frame()
	tt.Type("draft")
	if a.prompt != "draft" {
		t.Fatalf("prompt = %q", a.prompt)
	}
	tt.Key(ui.Cmd, ui.KeyC)
	if a.prompt != "" {
		t.Fatalf("prompt after Ctrl+C = %q", a.prompt)
	}
}

func TestUsageInStatusBar(t *testing.T) {
	a := newTestApp()
	a.connected = true
	a.sessionID = "s"
	a.contextUsed = 27394
	a.contextSize = 200000
	tt := ui.NewTester(a.view, 1000, 720)
	if !tt.HasText("27.4k/200k (14%)") {
		t.Fatalf("missing usage in %q", tt.Texts())
	}
}

func TestThoughtCollapsesAndExpands(t *testing.T) {
	a := newTestApp()
	a.transcript = []entry{{Kind: entryThought, Text: "line1\nline2\nline3\nline4\nline5\nline6"}}
	tt := ui.NewTester(a.view, 1000, 720)
	if tt.HasText("line1") {
		t.Fatalf("collapsed thought showed its first line: %q", tt.Texts())
	}
	if !tt.HasText("line6") {
		t.Fatalf("collapsed thought missed its tail: %q", tt.Texts())
	}
	r, ok := tt.Find("Thinking")
	if !ok {
		t.Fatal("thinking header not found")
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	if !a.transcript[0].Expanded {
		t.Fatal("thought did not expand")
	}
	if !tt.HasText("line1") {
		t.Fatalf("expanded thought missed its start: %q", tt.Texts())
	}
}

func TestEagerSessionFillsMenusAndKeepsSessionList(t *testing.T) {
	a := newTestApp()
	a.connected = true
	a.sessions = []acpclient.SessionInfo{{ID: "old", Title: "Earlier work"}}
	a.sessionID = "new"
	a.models = []string{"acme/fast"}
	a.model = "acme/fast"
	a.modes = []string{"build", "plan"}
	a.mode = "build"

	tt := ui.NewTester(a.view, 1000, 720)
	if !tt.HasText("Previous sessions") {
		t.Fatalf("first screen lost its session list: %q", tt.Texts())
	}
	models := a.modelItems()
	if len(models) != 1 || models[0].Label != "acme/fast" {
		t.Fatalf("models = %+v", models)
	}
	if next := a.nextMode(); next != "plan" {
		t.Fatalf("next mode = %q, want plan", next)
	}
	if !tt.HasText("Build") {
		t.Fatalf("status bar lost the mode: %q", tt.Texts())
	}
}

func TestElicitationShowsWholeOptionText(t *testing.T) {
	a := newTestApp()
	long := "Just assessing — no current pain - Exploring the option space. Then the honest answer is: don't do it yet, the embedded mode is fine."
	a.elicitation = &acpclient.ElicitationRequest{
		Reply: make(chan acpclient.ElicitationReply, 1),
		Fields: []acpclient.ElicitationField{{
			Name:  "driver",
			Title: "What's the actual driver for horizontal scale here?",
			Options: []acpclient.ElicitationOption{
				{Value: "fleet", Label: "Fleet size — one emak can't hold all agent connections"},
				{Value: "assess", Label: long},
			},
		}},
	}
	a.elicitationOpen = true
	a.answers = map[string]string{}
	a.multiAnswers = map[string]map[string]bool{}

	tt := ui.NewTester(a.view, 1000, 760)
	if !tt.HasText(long) {
		t.Fatalf("option text was cut: %q", tt.Texts())
	}
	r, ok := tt.Find(long)
	if !ok {
		t.Fatal("option row not found")
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	if a.answers["driver"] != long {
		t.Fatalf("answer = %q", a.answers["driver"])
	}
}

func TestModeColorsAreDistinct(t *testing.T) {
	build, plan := modeColor("build"), modeColor("plan")
	if build == plan {
		t.Fatal("build and plan share a color")
	}
	extra := modeColor("review")
	if extra == build || extra == plan {
		t.Fatalf("extra mode color %v clashes with a known mode", extra)
	}
	if modeColor("review") != extra {
		t.Fatal("unknown mode color is not stable")
	}
}

func TestCtrlCCopiesSelection(t *testing.T) {
	a := newTestApp()
	a.connected = true
	a.transcript = []entry{{Kind: entryAgent, Text: "copy this sentence"}}
	tt := ui.NewTester(a.view, 1000, 720)
	tt.Frame()

	r, ok := tt.Find("copy this sentence")
	if !ok {
		t.Fatalf("transcript text not found: %q", tt.Texts())
	}
	tt.Press(r.X+2, r.Y+r.H/2)
	tt.Move(r.X+r.W-2, r.Y+r.H/2)
	tt.Release(r.X+r.W-2, r.Y+r.H/2)
	tt.Frame()
	if a.selectedText == "" {
		t.Fatal("selection was not captured")
	}

	tt.Key(ui.Cmd, ui.KeyC)
	tt.Frame()
	if got := tt.Clipboard(); got != "copy this sentence" {
		t.Fatalf("clipboard = %q", got)
	}
}

func TestCtrlCWithComposerFocusedCopiesSelection(t *testing.T) {
	a := newTestApp()
	a.connected = true
	a.transcript = []entry{{Kind: entryAgent, Text: "copy this sentence"}}
	tt := ui.NewTester(a.view, 1000, 720)
	tt.Frame()
	tt.Type("draft")
	a.selectedText = "copy this sentence"

	tt.Key(ui.Cmd, ui.KeyC)
	tt.Frame()
	if a.prompt != "draft" {
		t.Fatalf("composer cleared despite a selection: %q", a.prompt)
	}
	if got := tt.Clipboard(); got != "copy this sentence" {
		t.Fatalf("clipboard = %q", got)
	}
}

func TestToolDetailsWrapCode(t *testing.T) {
	a := newTestApp()
	if !a.toolMD.WrapCode {
		t.Fatal("tool markdown must wrap code blocks")
	}
	if a.md.WrapCode {
		t.Fatal("agent markdown must keep its existing code layout")
	}
}

func TestKindLabelHidesDuplicates(t *testing.T) {
	if got := kindLabel("read", "read"); got != "" {
		t.Fatalf("duplicate label = %q, want empty", got)
	}
	if got := kindLabel("read", "File ~/x"); got != "read" {
		t.Fatalf("label = %q, want read", got)
	}
	if got := kindLabel("", "read"); got != "" {
		t.Fatalf("empty kind = %q, want empty", got)
	}
}

func TestCodeFence(t *testing.T) {
	if got := codeFence(`{"a": 1}`); !strings.HasPrefix(got, "```json\n") {
		t.Fatalf("json fence = %q", got)
	}
	if got := codeFence("a ``` b"); !strings.HasPrefix(got, "````\n") {
		t.Fatalf("backtick fence = %q", got)
	}
}

func TestPlanPanelShowsProgressAndExpands(t *testing.T) {
	a := newTestApp()
	a.plan = []acpclient.PlanEntry{
		{Content: "first task", Status: toolCompleted},
		{Content: "second task", Status: toolRunning},
		{Content: "third task", Status: toolPending},
	}
	tt := ui.NewTester(a.view, 1000, 720)
	tt.Frame()
	if !tt.HasText("Tasks 1 of 3") {
		t.Fatalf("progress header missing: %q", tt.Texts())
	}
	if tt.HasText("first task") || tt.HasText("third task") {
		t.Fatal("collapsed panel showed the full list")
	}
	r, ok := tt.Find("Tasks 1 of 3")
	if !ok {
		t.Fatal("header not found")
	}
	tt.ClickAt(r.X+2, r.Y+r.H/2)
	if !tt.HasText("first task") || !tt.HasText("third task") {
		t.Fatalf("expanded panel missing tasks: %q", tt.Texts())
	}
}

func TestPlanActionRequiresFFCommand(t *testing.T) {
	a := newTestApp()
	a.planReady = true
	tt := ui.NewTester(a.view, 1000, 720)
	tt.Frame()
	if tt.HasText("Create change from plan") {
		t.Fatal("plan action shown without the ff command")
	}
	a.commands = []customCommand{{Name: "/opsx:ff", Content: "FF body."}}
	tt.Frame()
	if !tt.HasText("Create change from plan") {
		t.Fatalf("plan action not rendered: %q", tt.Texts())
	}
}

func TestPlanChangePrompt(t *testing.T) {
	prompt := planChangePrompt(customCommand{Name: "/opsx:ff", Content: "FF body."}, "/state/plans/x.md")
	for _, want := range []string{"FF body.", "/state/plans/x.md", "read this file first"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %q", want, prompt)
		}
	}
}

func TestRunningDialogListsRunningTools(t *testing.T) {
	a := newTestApp()
	a.transcript = []entry{
		{Kind: entryTool, Text: "delegate research", Status: toolRunning, Started: time.Now()},
		{Kind: entryTool, Text: "read go.mod", Status: toolCompleted},
	}
	if got := len(a.runningTools()); got != 1 {
		t.Fatalf("runningTools = %d, want 1", got)
	}
	tt := ui.NewTester(a.view, 1000, 720)
	tt.Frame()
	if !tt.HasText("1 running") {
		t.Fatalf("status bar has no running count: %q", tt.Texts())
	}
	r, ok := tt.Find("1 running")
	if !ok {
		t.Fatal("running count not found")
	}
	tt.ClickAt(r.X+2, r.Y+r.H/2)
	if !tt.HasText("Tool calls still in progress.") {
		t.Fatal("running dialog did not open")
	}
}

func TestCommandPaletteKeyboard(t *testing.T) {
	a := newTestApp()
	a.connected = true
	a.cwd = "/test"
	a.commandsCwd = "/test"
	a.commands = []customCommand{
		{Name: "/project:alpha", Description: "Alpha"},
		{Name: "/project:beta", Description: "Beta"},
	}
	tt := ui.NewTester(a.view, 1000, 720)
	tt.Frame()
	tt.Type("/")
	if a.prompt != "/" {
		t.Fatalf("prompt = %q", a.prompt)
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if a.paletteIndex != 1 {
		t.Fatalf("palette index = %d, want 1", a.paletteIndex)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.prompt != "/project:beta " {
		t.Fatalf("prompt after Enter = %q", a.prompt)
	}
}

func TestTodoPanelPinsAndCollapses(t *testing.T) {
	a := newTestApp()
	a.transcript = []entry{{
		Kind:     entryTool,
		Text:     "todo_write",
		ToolID:   "todo-1",
		Status:   toolRunning,
		TodoList: true,
		Todos: []acpclient.PlanEntry{
			{Content: "write tests", Status: toolCompleted},
			{Content: "ship it", Status: toolRunning},
		},
	}}
	tt := ui.NewTester(a.view, 1000, 720)
	tt.Frame()
	if !tt.HasText("Tasks 1 of 2") {
		t.Fatalf("todo header missing: %q", tt.Texts())
	}
	if tt.HasText("write tests") {
		t.Fatalf("todo list was expanded by default: %q", tt.Texts())
	}
	r, ok := tt.Find("Tasks 1 of 2")
	if !ok {
		t.Fatal("todo header not found")
	}
	tt.ClickAt(r.X+2, r.Y+r.H/2)
	if !tt.HasText("write tests") || !tt.HasText("ship it") {
		t.Fatalf("todo list did not expand: %q", tt.Texts())
	}
}

func TestTodoPanelClosesWhenDone(t *testing.T) {
	a := newTestApp()
	a.transcript = []entry{{
		Kind:     entryTool,
		Text:     "todo_write",
		ToolID:   "todo-1",
		Status:   toolCompleted,
		TodoList: true,
		Todos: []acpclient.PlanEntry{
			{Content: "write tests", Status: toolCompleted},
			{Content: "ship it", Status: toolCompleted},
		},
	}}
	tt := ui.NewTester(a.view, 1000, 720)
	tt.Frame()
	if !tt.HasText("Tasks 2 of 2") {
		t.Fatalf("todo header missing: %q", tt.Texts())
	}
	if err := tt.Click("×"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("Tasks 2 of 2") {
		t.Fatalf("todo panel did not close: %q", tt.Texts())
	}
}
