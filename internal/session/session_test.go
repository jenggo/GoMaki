package session

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"gomaki/internal/acpclient"
)

func TestEventSequenceToSnapshot(t *testing.T) {
	c := New()
	c.Handle(acpclient.Event{Kind: acpclient.EventUserText, Text: "hello"})
	c.Handle(acpclient.Event{Kind: acpclient.EventText, Text: "hi "})
	c.Handle(acpclient.Event{Kind: acpclient.EventText, Text: "there"})
	c.Handle(acpclient.Event{
		Kind: acpclient.EventToolCall, ToolCallID: "t1", Title: "read", Status: "running",
		ToolInput: `{"path":"go.mod"}`,
	})
	c.Handle(acpclient.Event{Kind: acpclient.EventToolCallUpdate, ToolCallID: "t1", Status: "completed", ToolOutput: "module gomaki"})
	c.Handle(acpclient.Event{Kind: acpclient.EventPlan, Plan: []acpclient.PlanEntry{{Content: "step", Status: "pending"}}})
	c.Handle(acpclient.Event{Kind: acpclient.EventUsage, ContextUsed: 10, ContextSize: 100, CostAmount: 0.5, CostCurrency: "USD"})

	s := c.Snapshot()
	if len(s.Transcript) != 3 {
		t.Fatalf("transcript len = %d", len(s.Transcript))
	}
	if s.Transcript[1].Text != "hi there" {
		t.Fatalf("agent text = %q", s.Transcript[1].Text)
	}
	if s.Transcript[2].Status != "completed" || s.Transcript[2].ToolOutput != "module gomaki" {
		t.Fatalf("tool = %+v", s.Transcript[2])
	}
	if s.Transcript[2].ToolName != "read" {
		t.Fatalf("tool name = %q, want read", s.Transcript[2].ToolName)
	}
	if len(s.Plan) != 1 || s.Plan[0].Content != "step" {
		t.Fatalf("plan = %+v", s.Plan)
	}
	if s.ContextUsed != 10 || s.ContextSize != 100 || s.CostAmount != 0.5 {
		t.Fatalf("usage = %d/%d %v", s.ContextUsed, s.ContextSize, s.CostAmount)
	}
}

func TestReplayedToolUsesInputForTitle(t *testing.T) {
	c := New()
	c.Handle(acpclient.Event{Kind: acpclient.EventToolCall, ToolCallID: "t1", Title: "edit",
		ToolInput: `{"path":"/repo/internal/session/session.go"}`})
	c.state.Cwd = "/repo"

	if got := c.Snapshot().Transcript[0].Text; got != "/repo/internal/session/session.go" {
		t.Fatalf("replayed title = %q", got)
	}

	c.state.Cwd = "/repo"
	c.Handle(acpclient.Event{Kind: acpclient.EventToolCall, ToolCallID: "t2", Title: "grep",
		ToolInput: `{"pattern":"toolHeader","include":"*.go","path":"/repo/internal"}`})
	if got := c.Snapshot().Transcript[1].Text; got != "toolHeader [*.go] internal" {
		t.Fatalf("grep title = %q", got)
	}
}

func TestLiveToolTitleComesFromUpdate(t *testing.T) {
	c := New()
	c.Handle(acpclient.Event{Kind: acpclient.EventToolCall, ToolCallID: "t1", Title: "bash",
		ToolInput: `{"command":"ls","description":"list files"}`})
	c.Handle(acpclient.Event{Kind: acpclient.EventToolCallUpdate, ToolCallID: "t1", Title: "list files"})

	if got := c.Snapshot().Transcript[0].Text; got != "list files" {
		t.Fatalf("live title = %q, want the agent's summary", got)
	}
}

func TestToolNameSurvivesTitleUpdate(t *testing.T) {
	c := New()
	c.Handle(acpclient.Event{Kind: acpclient.EventToolCall, ToolCallID: "t1", Title: "bash", Status: "running"})
	c.Handle(acpclient.Event{Kind: acpclient.EventToolCallUpdate, ToolCallID: "t1", Title: "list files", Status: "completed"})

	entry := c.Snapshot().Transcript[0]
	if entry.ToolName != "bash" {
		t.Fatalf("tool name = %q, want bash", entry.ToolName)
	}
	if entry.Text != "list files" {
		t.Fatalf("title = %q, want the summary", entry.Text)
	}
}

func TestSnapshotCopiesSlices(t *testing.T) {
	c := New()
	c.Handle(acpclient.Event{Kind: acpclient.EventPlan, Plan: []acpclient.PlanEntry{{Content: "a"}}})
	s := c.Snapshot()
	s.Plan[0].Content = "mutated"
	if got := c.Snapshot().Plan[0].Content; got != "a" {
		t.Fatalf("snapshot shares the plan slice: %q", got)
	}
}

func TestLateSubscriberGetsSnapshotFirst(t *testing.T) {
	c := New()
	c.Handle(acpclient.Event{Kind: acpclient.EventText, Text: "before"})
	ch := c.Subscribe()
	first := <-ch
	if first.Kind != UpdateSnapshot || len(first.State.Transcript) != 1 {
		t.Fatalf("first update = %+v", first)
	}
	c.Handle(acpclient.Event{Kind: acpclient.EventText, Text: " after"})
	next := <-ch
	if next.Kind != UpdateEvent || next.Event.Text != " after" {
		t.Fatalf("next update = %+v", next)
	}
}

func TestDoubleAnswerDoesNotBlock(t *testing.T) {
	c := New()
	reply := make(chan string, 1)
	c.Handle(acpclient.Event{Kind: acpclient.EventPermission, Permission: &acpclient.PermissionRequest{Reply: reply}})

	done := make(chan struct{})
	go func() {
		c.AnswerPermission("allow_once")
		c.AnswerPermission("reject")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second answer blocked")
	}
	if got := <-reply; got != "allow_once" {
		t.Fatalf("answer = %q", got)
	}
	if c.Snapshot().Permission != nil {
		t.Fatal("permission still pending")
	}
}

func TestNewPermissionReplacesPending(t *testing.T) {
	c := New()
	first := make(chan string, 1)
	second := make(chan string, 1)
	c.Handle(acpclient.Event{Kind: acpclient.EventPermission, Permission: &acpclient.PermissionRequest{Reply: first}})
	c.Handle(acpclient.Event{Kind: acpclient.EventPermission, Permission: &acpclient.PermissionRequest{Reply: second}})
	if got := <-first; got != "" {
		t.Fatalf("old request reply = %q, want cancel", got)
	}
	if c.Snapshot().Permission == nil {
		t.Fatal("new request is not pending")
	}
}

func TestResetClearsSessionState(t *testing.T) {
	c := New()
	c.Handle(acpclient.Event{Kind: acpclient.EventText, Text: "hello"})
	c.Handle(acpclient.Event{Kind: acpclient.EventPlan, Plan: []acpclient.PlanEntry{{Content: "a"}}})
	c.Handle(acpclient.Event{Kind: acpclient.EventUsage, ContextUsed: 5, ContextSize: 10})
	c.Handle(acpclient.Event{Kind: acpclient.EventPermission, Permission: &acpclient.PermissionRequest{Reply: make(chan string, 1)}})
	c.mu.Lock()
	c.state.Busy = true
	c.state.PlanPath = "/plan.md"
	c.state.PlanReady = true
	c.state.Queue = []QueuedPrompt{{Prompt: "q"}}
	c.resetLocked()
	c.mu.Unlock()

	s := c.Snapshot()
	if len(s.Transcript) != 0 || len(s.Plan) != 0 || s.ContextUsed != 0 || s.Busy || s.PlanPath != "" || s.PlanReady || len(s.Queue) != 0 || s.Permission != nil {
		t.Fatalf("reset left state: %+v", s)
	}
}

func TestQueueWhileBusy(t *testing.T) {
	c := New()
	c.client = &acpclient.Client{}
	c.state.Busy = true
	if !c.Send("one", "one", nil) {
		t.Fatal("send while busy was rejected")
	}
	if !c.Send("two", "two", nil) {
		t.Fatal("second send while busy was rejected")
	}
	if got := len(c.Snapshot().Queue); got != 2 {
		t.Fatalf("queue len = %d", got)
	}
	if text, ok := c.PopQueue(); !ok || text != "one" {
		t.Fatalf("pop = %q %v", text, ok)
	}
	if got := len(c.Snapshot().Queue); got != 1 {
		t.Fatalf("queue len after pop = %d", got)
	}
}

func TestQueueBatchesByMode(t *testing.T) {
	c := New()
	c.state.Queue = []QueuedPrompt{
		{Prompt: "a", Display: "a", Mode: "build"},
		{Prompt: "b", Display: "b", Mode: "build"},
		{Prompt: "c", Display: "c", Mode: "plan"},
	}
	c.mu.Lock()
	prompt, display, _, ok := c.nextBatchLocked()
	c.mu.Unlock()
	if !ok || prompt != "a\n\nb" || display != "a\n\nb" {
		t.Fatalf("batch = %q %q %v", prompt, display, ok)
	}
	if got := len(c.Snapshot().Queue); got != 1 {
		t.Fatalf("queue len = %d", got)
	}
}

func TestRewindLastTurn(t *testing.T) {
	c := New()
	c.Handle(acpclient.Event{Kind: acpclient.EventUserText, Text: "first"})
	c.Handle(acpclient.Event{Kind: acpclient.EventText, Text: "answer"})
	c.Handle(acpclient.Event{Kind: acpclient.EventUserText, Text: "second"})
	text, ok := c.RewindLastTurn()
	if !ok || text != "second" {
		t.Fatalf("rewind = %q %v", text, ok)
	}
	if got := len(c.Snapshot().Transcript); got != 2 {
		t.Fatalf("transcript len = %d", got)
	}
}

func TestPlanPathCapturedInPlanMode(t *testing.T) {
	c := New()
	c.state.Mode = modePlan
	c.Handle(acpclient.Event{Kind: acpclient.EventToolCall, ToolCallID: "t1", Title: "write plan", ToolKind: toolKindEdit})
	c.Handle(acpclient.Event{Kind: acpclient.EventToolCallUpdate, ToolCallID: "t1", ToolInput: `{"path":"/state/plans/x.md"}`})
	if got := c.Snapshot().PlanPath; got != "/state/plans/x.md" {
		t.Fatalf("planPath = %q", got)
	}
	c.Handle(acpclient.Event{Kind: acpclient.EventToolCall, ToolCallID: "t2", Title: "read", ToolKind: "read", ToolInput: `{"path":"/repo/x.go"}`})
	if got := c.Snapshot().PlanPath; got != "/state/plans/x.md" {
		t.Fatalf("read changed planPath to %q", got)
	}
}

func TestPromptDoneRestoresDraft(t *testing.T) {
	c := New()
	c.state.Busy = true
	c.state.Stopping = true
	c.state.LastPrompt = "hello"
	c.Handle(acpclient.Event{Kind: acpclient.EventPromptDone, StopReason: "cancelled"})
	s := c.Snapshot()
	if s.Busy || s.Notice != "Stopped — prompt restored" || s.LastPrompt != "" {
		t.Fatalf("state = %+v", s)
	}
}

func TestPromptDoneKeepsDraftOnSuccess(t *testing.T) {
	c := New()
	c.state.Busy = true
	c.state.LastPrompt = "hello"
	c.Handle(acpclient.Event{Kind: acpclient.EventPromptDone, StopReason: "end_turn"})
	s := c.Snapshot()
	if s.Busy || s.LastPrompt != "" || s.Notice != "" {
		t.Fatalf("state = %+v", s)
	}
}

func TestPlanReadyAfterPlanTurn(t *testing.T) {
	c := New()
	c.state.Mode = modePlan
	c.state.PlanPath = "/state/plans/x.md"
	c.Handle(acpclient.Event{Kind: acpclient.EventPromptDone, StopReason: "end_turn"})
	if !c.Snapshot().PlanReady {
		t.Fatal("plan not ready after a plan turn")
	}
}

func TestTodoListFlag(t *testing.T) {
	c := New()
	c.Handle(acpclient.Event{Kind: acpclient.EventToolCall, ToolCallID: "t1", ToolInput: `{"todos":[]}`})
	c.Handle(acpclient.Event{Kind: acpclient.EventToolCall, ToolCallID: "t2", ToolInput: `{"path":"x"}`})
	s := c.Snapshot()
	if !s.Transcript[0].TodoList || len(s.Transcript[0].Todos) != 0 {
		t.Fatalf("empty todo list not flagged: %+v", s.Transcript[0])
	}
	if s.Transcript[1].TodoList {
		t.Fatalf("non-todo input flagged: %+v", s.Transcript[1])
	}
}

func TestLoadReplayIsQuiet(t *testing.T) {
	c := New()
	ch := c.Subscribe()
	<-ch // initial snapshot

	c.mu.Lock()
	c.loading = true
	c.mu.Unlock()
	c.Handle(acpclient.Event{Kind: acpclient.EventText, Text: "one"})
	c.Handle(acpclient.Event{Kind: acpclient.EventText, Text: " two"})

	select {
	case u := <-ch:
		t.Fatalf("update during load replay: %+v", u)
	default:
	}
	if got := c.Snapshot().Transcript; len(got) != 1 || got[0].Text != "one two" {
		t.Fatalf("transcript = %+v", got)
	}

	c.mu.Lock()
	c.loading = false
	c.publishLocked(Update{Kind: UpdateState})
	c.mu.Unlock()
	u := <-ch
	if u.Kind != UpdateState || !u.Dirty || len(u.State.Transcript) != 0 {
		t.Fatalf("final update = %+v", u)
	}
	if got := c.Snapshot().Transcript; len(got) != 1 || got[0].Text != "one two" {
		t.Fatalf("snapshot after dirty update = %+v", got)
	}
}

func TestLoadSessionPublishesOneState(t *testing.T) {
	c := New()
	ch := c.Subscribe()
	<-ch // initial snapshot

	c.mu.Lock()
	c.loading = true
	c.mu.Unlock()
	for i := range 50 {
		c.Handle(acpclient.Event{Kind: acpclient.EventUserText, Text: fmt.Sprintf("turn %d", i)})
		c.Handle(acpclient.Event{Kind: acpclient.EventText, Text: fmt.Sprintf("answer %d", i)})
		c.Handle(acpclient.Event{Kind: acpclient.EventPromptDone, StopReason: "end_turn"})
	}

	select {
	case u := <-ch:
		t.Fatalf("replayed turn published: %+v", u)
	default:
	}

	c.mu.Lock()
	c.loading = false
	c.publishLocked(Update{Kind: UpdateState})
	c.mu.Unlock()
	u := <-ch
	if u.Kind != UpdateState || !u.Dirty {
		t.Fatalf("final update = %+v", u)
	}
	select {
	case u := <-ch:
		t.Fatalf("more than one state update: %+v", u)
	default:
	}
	if got := len(c.Snapshot().Transcript); got != 100 {
		t.Fatalf("transcript len = %d, want 100", got)
	}
}

func TestSnapshotKeepsUpDuringPublishStorm(t *testing.T) {
	c := New()
	ch := c.Subscribe()
	<-ch // initial snapshot

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			c.Snapshot()
		}
	})
	for i := range 200 {
		c.Handle(acpclient.Event{Kind: acpclient.EventToolCall, ToolCallID: fmt.Sprintf("t%d", i), Title: "x", Status: "running"})
		c.Handle(acpclient.Event{Kind: acpclient.EventToolCallUpdate, ToolCallID: fmt.Sprintf("t%d", i), Status: "completed"})
	}
	close(stop)
	wg.Wait()

	for range 200 {
		select {
		case u := <-ch:
			if u.Kind == UpdateState && !u.Dirty {
				t.Fatalf("state update not marked dirty: %+v", u)
			}
		default:
		}
	}
}

func TestSubscribeDoesNotHoldLock(t *testing.T) {
	c := New()
	ch := c.Subscribe()
	<-ch // initial snapshot
	c.Handle(acpclient.Event{Kind: acpclient.EventText, Text: "one"})

	// An unread subscriber must not stop Snapshot from returning.
	done := make(chan struct{})
	go func() {
		c.Snapshot()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Snapshot blocked by an unread subscriber")
	}

	// The initial send happens after the mutex is released: a publish from
	// another goroutine is not blocked by a fresh Subscribe either.
	done2 := make(chan struct{})
	go func() {
		c.Subscribe()
		close(done2)
	}()
	select {
	case <-done2:
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe blocked on send")
	}
}

func TestSlowSubscriberIsNotClosed(t *testing.T) {
	c := New()
	slow := c.Subscribe()
	<-slow // initial snapshot

	// Overflow the buffer while it is never read.
	for i := range 200 {
		c.Handle(acpclient.Event{Kind: acpclient.EventToolCall, ToolCallID: fmt.Sprintf("t%d", i), Title: "x"})
	}

	// The stream is still open and holds the updates it did receive.
	got := 0
	for {
		select {
		case _, ok := <-slow:
			if !ok {
				t.Fatal("slow subscriber was closed after buffer overflow")
			}
			got++
			continue
		default:
		}
		break
	}
	if got == 0 {
		t.Fatal("slow subscriber received nothing")
	}

	// A later update still arrives: the slow subscriber converges to the
	// newest state.
	c.mu.Lock()
	c.publishLocked(Update{Kind: UpdateState})
	c.mu.Unlock()
	select {
	case u, ok := <-slow:
		if !ok || !u.Dirty {
			t.Fatalf("convergence update = %+v ok=%v", u, ok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no convergence update")
	}
}

func TestUnsubscribeClosesExactlyOnce(t *testing.T) {
	c := New()
	ch := c.Subscribe()
	<-ch
	c.Unsubscribe(ch)
	c.Unsubscribe(ch) // second call is a no-op

	if _, ok := <-ch; ok {
		t.Fatal("channel not closed by Unsubscribe")
	}
}

func TestTranscriptPage(t *testing.T) {
	c := New()
	for i := range 5 {
		c.Handle(acpclient.Event{Kind: acpclient.EventToolCall, ToolCallID: fmt.Sprintf("t%d", i), Title: "x"})
	}
	page, start, more := c.TranscriptPage(5, 2)
	if len(page) != 2 || start != 3 || !more {
		t.Fatalf("page = %d start = %d more = %v", len(page), start, more)
	}
	page, start, more = c.TranscriptPage(3, 2)
	if len(page) != 2 || start != 1 || !more {
		t.Fatalf("page = %d start = %d more = %v", len(page), start, more)
	}
	page, start, more = c.TranscriptPage(1, 2)
	if len(page) != 1 || start != 0 || more {
		t.Fatalf("page = %d start = %d more = %v", len(page), start, more)
	}
}

func TestApplyingSessionDropsItFromSessionList(t *testing.T) {
	c := New()
	c.state.Sessions = []acpclient.SessionInfo{
		{ID: "s1", Title: "current"},
		{ID: "s2", Title: "other"},
	}
	c.applySessionLocked(acpclient.Session{ID: "s1"})
	sessions := c.Snapshot().Sessions
	if len(sessions) != 1 || sessions[0].ID != "s2" {
		t.Fatalf("session list = %+v", sessions)
	}
}
