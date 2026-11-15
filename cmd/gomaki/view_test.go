package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"gomaki/internal/acpclient"
)

func newTestApp() *app {
	return &app{planIndex: -1, sessionPick: -1, settings: defaultSettings}
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
	a := newTestApp()
	reply := make(chan string, 1)
	a.permission = &acpclient.PermissionRequest{
		Title:   "Run ls",
		Options: []acpclient.PermissionOption{{ID: "allow_once", Name: "Allow once"}},
		Reply:   reply,
	}
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
	a := newTestApp()
	reply := make(chan acpclient.ElicitationReply, 1)
	a.startElicitation(&acpclient.ElicitationRequest{
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
	})
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
	a.client = &acpclient.Client{}
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
	a.client = &acpclient.Client{}
	a.resumeOpen = true
	tt := ui.NewTester(a.view, 900, 700)
	if !tt.HasText("No stored sessions yet") {
		t.Fatalf("missing empty picker message: %q", tt.Texts())
	}
}

func TestFirstScreenListsSessions(t *testing.T) {
	a := newTestApp()
	a.client = &acpclient.Client{}
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

func TestPromptDoneRestoresDraft(t *testing.T) {
	a := newTestApp()
	a.busy = true
	a.stopping = true
	a.lastPrompt = "hello"
	a.promptDone(acpclient.Event{StopReason: "cancelled"})
	if a.busy || a.prompt != "hello" || a.notice != "Stopped — prompt restored" {
		t.Fatalf("busy=%v prompt=%q notice=%q", a.busy, a.prompt, a.notice)
	}
}

func TestPromptDoneKeepsDraftOnSuccess(t *testing.T) {
	a := newTestApp()
	a.busy = true
	a.lastPrompt = "hello"
	a.promptDone(acpclient.Event{StopReason: "end_turn"})
	if a.busy || a.prompt != "" || a.lastPrompt != "" {
		t.Fatalf("busy=%v prompt=%q last=%q", a.busy, a.prompt, a.lastPrompt)
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

func TestRewindLastTurn(t *testing.T) {
	a := newTestApp()
	a.transcript = []entry{
		{Kind: entryUser, Text: "first"},
		{Kind: entryAgent, Text: "answer one"},
		{Kind: entryPlan, Plan: []acpclient.PlanEntry{{Content: "step", Status: toolCompleted}}},
		{Kind: entryUser, Text: "lorem ipsum"},
		{Kind: entryAgent, Text: "answer two"},
		{Kind: entryTool, Text: "bash", Status: toolCompleted},
	}
	a.rewindLastTurn()
	if len(a.transcript) != 3 {
		t.Fatalf("transcript len = %d", len(a.transcript))
	}
	if a.prompt != "lorem ipsum" || a.notice != "Last prompt restored" {
		t.Fatalf("prompt = %q notice = %q", a.prompt, a.notice)
	}
	if a.planIndex != 2 {
		t.Fatalf("planIndex = %d", a.planIndex)
	}
}

func TestCtrlCClearsComposer(t *testing.T) {
	a := newTestApp()
	a.client = &acpclient.Client{}
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
	a.client = &acpclient.Client{}
	a.sessionID = "s"
	a.contextUsed = 27394
	a.contextSize = 200000
	tt := ui.NewTester(a.view, 1000, 720)
	if !tt.HasText("ctx 27.4k/200k") {
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
