// Package session owns the ACP client and the session state. Renderers read a
// consistent snapshot and subscribe to ordered updates; they never own a
// second copy of the session.
package session

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gomaki/internal/acpclient"
)

const (
	modePlan     = "plan"
	toolKindEdit = "edit"
)

// EntryKind is the kind of a transcript entry.
type EntryKind int

const (
	EntryUser EntryKind = iota
	EntryAgent
	EntryThought
	EntryTool
)

// Entry is one transcript item.
type Entry struct {
	Started    time.Time
	Text       string
	ToolID     string
	ToolName   string
	Status     string
	ToolInput  string
	ToolOutput string
	ToolKind   string
	Todos      []acpclient.PlanEntry
	TodoList   bool
	Kind       EntryKind
}

// QueuedPrompt is a prompt waiting for the current turn to end.
type QueuedPrompt struct {
	Att     *acpclient.Attachment
	Prompt  string
	Display string
	Mode    string
}

// State is the session state a renderer reads.
type State struct {
	Permission  *acpclient.PermissionRequest
	Elicitation *acpclient.ElicitationRequest

	PlanPath string
	Model    string
	Mode     string

	CostCurrency string

	Status     string
	Err        string
	Notice     string
	LastPrompt string

	SessionID  string
	Cwd        string
	Branch     string
	Title      string
	Transcript []Entry
	Plan       []acpclient.PlanEntry
	Todos      []acpclient.PlanEntry
	TodoToolID string

	Models   []string
	Modes    []string
	Commands []acpclient.Command
	Sessions []acpclient.SessionInfo

	Queue []QueuedPrompt

	ContextUsed int64
	ContextSize int64
	CostAmount  float64
	PlanReady   bool

	Busy       bool
	Stopping   bool
	ConfigSeen bool
	Resumed    bool
}

// UpdateKind tells a subscriber what changed.
type UpdateKind int

const (
	UpdateSnapshot UpdateKind = iota
	UpdateEvent
	UpdateState
	UpdateReset
	UpdateRestoreDraft
)

// Update is one ordered change. A state-carrying update is marked Dirty; its
// State is filled in by the consumer from Snapshot, after the publish lock is
// released, so a publish never builds a snapshot under the core mutex.
type Update struct {
	Text  string
	State State
	Event acpclient.Event
	Kind  UpdateKind
	Dirty bool
}

// Core owns the ACP client and the session state.
type Core struct {
	client *acpclient.Client
	stop   chan struct{}

	// titleSource is the first prompt the model saw, which is the text maki
	// names a session after. The transcript holds what the user typed instead.
	titleSource string
	subs        []chan Update
	state       State
	mu          sync.Mutex
	closed      bool
	loading     bool
}

// New returns a core with no agent process. Call Start to spawn one.
func New() *Core {
	return &Core{}
}

// Start spawns the agent in dir and begins draining its events.
func (c *Core) Start(dir string) error {
	client, err := acpclient.Start(dir)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.client = client
	c.state.Cwd = dir
	c.startPumpLocked(client)
	c.mu.Unlock()
	c.refreshBranch(dir)
	return nil
}

// Snapshot returns a consistent copy of the state.
func (c *Core) Snapshot() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotLocked()
}

// Subscribe returns the update stream. The first update is a snapshot.
func (c *Core) Subscribe() <-chan Update {
	ch := make(chan Update, 64)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		close(ch)
		return ch
	}
	first := Update{Kind: UpdateSnapshot, State: c.snapshotLocked()}
	c.subs = append(c.subs, ch)
	c.mu.Unlock()
	ch <- first
	return ch
}

// Unsubscribe ends one subscription.
func (c *Core) Unsubscribe(ch <-chan Update) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, s := range c.subs {
		if s == ch {
			close(s)
			c.subs = append(c.subs[:i], c.subs[i+1:]...)
			return
		}
	}
}

// Closed reports whether Close has run.
func (c *Core) Closed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// Close stops the agent process and ends every subscription.
func (c *Core) Close() error {
	c.mu.Lock()
	c.closed = true
	c.stopPumpLocked()
	client := c.client
	c.client = nil
	for _, ch := range c.subs {
		close(ch)
	}
	c.subs = nil
	c.mu.Unlock()
	if client != nil {
		return client.Close()
	}
	return nil
}

// Handle applies one agent event. The pump calls it for every event.
func (c *Core) Handle(e acpclient.Event) {
	c.mu.Lock()
	restore := c.applyEventLocked(e)
	// A loaded session replays its whole transcript as events. Publish those
	// as one state after the load instead of flooding every subscriber.
	quiet := c.loading &&
		e.Kind != acpclient.EventPermission &&
		e.Kind != acpclient.EventElicitation
	if !quiet {
		c.publishLocked(Update{Kind: UpdateEvent, Event: e})
	}
	if restore != "" {
		c.publishLocked(Update{Kind: UpdateRestoreDraft, Text: restore})
	}
	c.mu.Unlock()
	if e.Kind == acpclient.EventPromptDone {
		c.drainQueue()
	}
}

func (c *Core) applyEventLocked(e acpclient.Event) string {
	switch e.Kind {
	case acpclient.EventUserText:
		c.appendTextLocked(EntryUser, e.Text)
	case acpclient.EventText:
		c.appendTextLocked(EntryAgent, e.Text)
	case acpclient.EventThought:
		c.appendTextLocked(EntryThought, e.Text)
	case acpclient.EventToolCall:
		status := e.Status
		if status == "" {
			status = "pending"
		}
		c.capturePlanPathLocked(e.ToolKind, e.ToolInput)
		todos, todoList := parseTodos(e.ToolInput)
		c.pinTodosLocked(e.ToolCallID, todos, todoList)
		c.state.Transcript = append(c.state.Transcript, Entry{
			Kind:       EntryTool,
			Text:       e.Title,
			ToolID:     e.ToolCallID,
			ToolName:   e.Title,
			Status:     status,
			ToolInput:  e.ToolInput,
			ToolOutput: e.ToolOutput,
			ToolKind:   e.ToolKind,
			Todos:      todos,
			TodoList:   todoList,
			Started:    time.Now(),
		})
		if header := toolHeader(c.state.Cwd, e.Title, e.ToolInput); header != "" {
			// A live call sends its summary in a later update; a replayed one
			// never does, so rebuild it from the input the replay does carry.
			c.state.Transcript[len(c.state.Transcript)-1].Text = header
		}
	case acpclient.EventToolCallUpdate:
		c.updateToolCallLocked(e)
	case acpclient.EventPlan:
		c.state.Plan = e.Plan
	case acpclient.EventConfig:
		c.state.ConfigSeen = true
		c.state.Models = e.Models
		if e.Model != "" {
			c.state.Model = e.Model
		}
	case acpclient.EventMode:
		c.state.Mode = e.Mode
	case acpclient.EventCommands:
		c.state.Commands = e.Commands
	case acpclient.EventPermission:
		c.askPermissionLocked(e.Permission)
	case acpclient.EventElicitation:
		c.askElicitationLocked(e.Elicitation)
	case acpclient.EventUsage:
		c.state.ContextUsed, c.state.ContextSize = e.ContextUsed, e.ContextSize
		c.state.CostAmount, c.state.CostCurrency = e.CostAmount, e.CostCurrency
	case acpclient.EventPromptDone:
		return c.promptDoneLocked(e)
	}
	return ""
}

// Send starts a turn, or queues the prompt while the agent is busy.
func (c *Core) Send(prompt, display string, att *acpclient.Attachment) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		return false
	}
	if c.state.Busy {
		c.state.Queue = append(c.state.Queue, QueuedPrompt{
			Prompt: prompt, Display: display, Mode: c.state.Mode, Att: att,
		})
		c.publishLocked(Update{Kind: UpdateState})
		return true
	}
	ok := c.sendLocked(prompt, display, att)
	c.publishLocked(Update{Kind: UpdateState})
	return ok
}

// Cancel stops the running turn.
func (c *Core) Cancel() {
	c.mu.Lock()
	if c.client == nil || !c.state.Busy {
		c.mu.Unlock()
		return
	}
	c.state.Stopping = true
	c.state.Notice = "Stopping…"
	client := c.client
	c.publishLocked(Update{Kind: UpdateState})
	c.mu.Unlock()
	go func() { _ = client.Cancel() }()
}

// SetModel switches the session model.
func (c *Core) SetModel(model string) error {
	c.mu.Lock()
	if c.client == nil {
		c.mu.Unlock()
		return errors.New("no agent")
	}
	prev := c.state.Model
	c.state.Model = model
	client := c.client
	c.publishLocked(Update{Kind: UpdateState})
	c.mu.Unlock()
	if err := client.SetModel(model); err != nil {
		c.mu.Lock()
		c.state.Model = prev
		c.state.Err = err.Error()
		c.publishLocked(Update{Kind: UpdateState})
		c.mu.Unlock()
		return err
	}
	return nil
}

// SetMode switches the session mode.
func (c *Core) SetMode(mode string) error {
	c.mu.Lock()
	if c.client == nil {
		c.mu.Unlock()
		return errors.New("no agent")
	}
	prev := c.state.Mode
	c.state.Mode = mode
	if mode != modePlan {
		c.state.PlanPath = ""
		c.state.PlanReady = false
	}
	client := c.client
	c.publishLocked(Update{Kind: UpdateState})
	c.mu.Unlock()
	if err := client.SetMode(mode); err != nil {
		c.mu.Lock()
		c.state.Mode = prev
		c.state.Err = err.Error()
		c.publishLocked(Update{Kind: UpdateState})
		c.mu.Unlock()
		return err
	}
	return nil
}

// NewSession starts a session in dir. A different directory respawns the
// agent there; the same directory reuses the running process.
func (c *Core) NewSession(dir string) error {
	client, err := c.clientFor(dir)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.state.Cwd = dir
	c.resetLocked()
	c.state.Status = "Starting session…"
	c.publishLocked(Update{Kind: UpdateReset})
	c.mu.Unlock()
	c.refreshBranch(dir)
	s, err := client.NewSession(dir)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.Status = ""
	if err != nil {
		c.state.Err = err.Error()
		c.publishLocked(Update{Kind: UpdateState})
		return err
	}
	c.applySessionLocked(s)
	c.publishLocked(Update{Kind: UpdateState})
	return nil
}

// clientFor returns the agent for dir, respawning it when the directory
// changed.
func (c *Core) clientFor(dir string) (*acpclient.Client, error) {
	c.mu.Lock()
	client := c.client
	same := dir == c.state.Cwd
	c.mu.Unlock()
	if client == nil {
		return nil, errors.New("no agent")
	}
	if same {
		return client, nil
	}
	c.mu.Lock()
	c.stopPumpLocked()
	old := c.client
	c.client = nil
	c.mu.Unlock()
	_ = old.Close()
	next, err := acpclient.Start(dir)
	if err != nil {
		c.mu.Lock()
		c.state.Err = err.Error()
		c.publishLocked(Update{Kind: UpdateState})
		c.mu.Unlock()
		return nil, err
	}
	c.mu.Lock()
	c.client = next
	c.startPumpLocked(next)
	c.mu.Unlock()
	return next, nil
}

// LoadSession resumes a stored session.
func (c *Core) LoadSession(id string) error {
	c.mu.Lock()
	client := c.client
	cwd := c.state.Cwd
	contextSize := c.state.ContextSize
	if client == nil {
		c.mu.Unlock()
		return errors.New("no agent")
	}
	c.resetLocked()
	c.state.Status = "Loading session…"
	c.loading = true
	c.publishLocked(Update{Kind: UpdateReset})
	c.mu.Unlock()
	s, err := client.LoadSession(cwd, id)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loading = false
	c.state.Status = ""
	if err != nil {
		c.state.Err = err.Error()
		c.publishLocked(Update{Kind: UpdateState})
		return err
	}
	c.state.Resumed = true
	used, model := storedContext(id)
	window := catalogWindow(model)
	if window == 0 {
		window = contextSize
	}
	c.state.ContextUsed, c.state.ContextSize = used, window
	c.applySessionLocked(s)
	c.publishLocked(Update{Kind: UpdateState})
	return nil
}

// storedContext reads the gauge and the model from maki's session file. ACP
// sends a usage update only when a turn finishes, so a resumed session has
// none.
func storedContext(id string) (used int64, model string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, ""
	}
	f, err := os.Open(filepath.Join(home, ".local", "state", "maki", "sessions", id+".jsonl"))
	if err != nil {
		return 0, ""
	}
	defer func() { _ = f.Close() }()

	reader := bufio.NewReader(f)
	metaPrefix := []byte(`{"t":"meta"`)
	headerPrefix := []byte(`{"t":"header"`)
	for {
		line, err := reader.ReadBytes('\n')
		if bytes.HasPrefix(line, metaPrefix) {
			var meta struct {
				ContextSize int64 `json:"context_size"`
			}
			if json.Unmarshal(line, &meta) == nil {
				used = meta.ContextSize
			}
		} else if bytes.HasPrefix(line, headerPrefix) {
			var header struct {
				Model string `json:"model"`
			}
			if json.Unmarshal(line, &header) == nil {
				model = header.Model
			}
		}
		if err != nil {
			return used, model
		}
	}
}

// catalogWindow looks up the model's context window in maki's cached
// models.dev catalog. Zero means unknown; the caller keeps what it has.
func catalogWindow(model string) int64 {
	provider, id, ok := strings.Cut(model, "/")
	if !ok {
		return 0
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return 0
	}
	data, err := os.ReadFile(filepath.Join(home, ".cache", "maki", "models-dev-catalog.json"))
	if err != nil {
		return 0
	}
	var catalog map[string]struct {
		Models map[string]struct {
			Limit struct {
				Context int64 `json:"context"`
			} `json:"limit"`
		} `json:"models"`
	}
	if json.Unmarshal(data, &catalog) != nil {
		return 0
	}
	return catalog[provider].Models[id].Limit.Context
}

// ListSessions lists stored sessions for the working directory.
func (c *Core) ListSessions() ([]acpclient.SessionInfo, error) {
	c.mu.Lock()
	client := c.client
	cwd := c.state.Cwd
	c.mu.Unlock()
	if client == nil {
		return nil, errors.New("no agent")
	}
	sessions, err := client.ListSessions(cwd)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	filtered := make([]acpclient.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		if s.ID != c.state.SessionID {
			filtered = append(filtered, s)
		}
	}
	c.state.Sessions = filtered
	c.publishLocked(Update{Kind: UpdateState})
	return filtered, nil
}

// AnswerPermission sends the first answer and ignores later ones.
func (c *Core) AnswerPermission(option string) {
	c.mu.Lock()
	req := c.state.Permission
	c.state.Permission = nil
	c.publishLocked(Update{Kind: UpdateState})
	c.mu.Unlock()
	if req != nil {
		req.Reply <- option
	}
}

// AnswerElicitation sends the first answer and ignores later ones.
func (c *Core) AnswerElicitation(answers map[string]any, decline bool) {
	c.mu.Lock()
	req := c.state.Elicitation
	c.state.Elicitation = nil
	c.publishLocked(Update{Kind: UpdateState})
	c.mu.Unlock()
	if req != nil {
		req.Reply <- acpclient.ElicitationReply{Answers: answers, Decline: decline}
	}
}

// TranscriptPage returns up to limit entries before the absolute index before,
// the absolute index of the first returned entry, and whether older entries
// remain.
func (c *Core) TranscriptPage(before, limit int) ([]Entry, int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	total := len(c.state.Transcript)
	if before <= 0 || before > total {
		before = total
	}
	start := max(before-limit, 0)
	page := slices.Clone(c.state.Transcript[start:before])
	for i := range page {
		page[i].Todos = slices.Clone(page[i].Todos)
	}
	return page, start, start > 0
}

// RewindLastTurn removes the last user turn and returns its text.
func (c *Core) RewindLastTurn() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	idx := -1
	for i := range slices.Backward(c.state.Transcript) {
		if c.state.Transcript[i].Kind == EntryUser {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", false
	}
	text := c.state.Transcript[idx].Text
	c.state.Transcript = c.state.Transcript[:idx]
	c.state.Err = ""
	c.state.Notice = "Last prompt restored"
	c.publishLocked(Update{Kind: UpdateState})
	return text, true
}

// PopQueue removes the first queued prompt and returns its display text.
func (c *Core) PopQueue() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.state.Queue) == 0 {
		return "", false
	}
	q := c.state.Queue[0]
	c.state.Queue = c.state.Queue[1:]
	c.publishLocked(Update{Kind: UpdateState})
	return q.Display, true
}

func (c *Core) startPumpLocked(client *acpclient.Client) {
	stop := make(chan struct{})
	c.stop = stop
	go func() {
		for {
			select {
			case <-stop:
				return
			case e := <-client.Events():
				c.Handle(e)
			}
		}
	}()
}

func (c *Core) stopPumpLocked() {
	if c.stop != nil {
		close(c.stop)
		c.stop = nil
	}
}

func (c *Core) publishLocked(u Update) {
	if u.Kind != UpdateEvent || u.Event.Kind == acpclient.EventPromptDone {
		u.Dirty = true
	}
	for i := 0; i < len(c.subs); i++ {
		select {
		case c.subs[i] <- u:
		default:
			// A subscriber that fell behind loses its oldest buffered
			// update to make room for the newest one, so it converges on
			// the newest state instead of stalling; its stream stays open.
			select {
			case <-c.subs[i]:
			default:
			}
			select {
			case c.subs[i] <- u:
			default:
			}
		}
	}
}

func (c *Core) snapshotLocked() State {
	s := c.state
	s.Transcript = slices.Clone(c.state.Transcript)
	for i := range s.Transcript {
		s.Transcript[i].Todos = slices.Clone(s.Transcript[i].Todos)
	}
	s.Plan = slices.Clone(c.state.Plan)
	s.Todos = slices.Clone(c.state.Todos)
	s.Title = c.titleLocked()
	s.Models = slices.Clone(c.state.Models)
	s.Modes = slices.Clone(c.state.Modes)
	s.Sessions = slices.Clone(c.state.Sessions)
	s.Queue = slices.Clone(c.state.Queue)
	return s
}

// titleLocked derives the session title from the first prompt the model saw,
// the same text maki stores in session/list, so the label does not change once
// the session is saved.
func (c *Core) titleLocked() string {
	if c.titleSource != "" {
		return titleText(c.titleSource)
	}
	return titleFrom(c.state.Transcript)
}

// titleLimit is maki's session title cap, in bytes.
const titleLimit = 60

// titleFrom names a session the way maki does: the first user prompt,
// whitespace collapsed, cut at titleLimit bytes.
func titleFrom(transcript []Entry) string {
	for _, e := range transcript {
		if e.Kind == EntryUser {
			return titleText(e.Text)
		}
	}
	return ""
}

func titleText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= titleLimit {
		return text
	}
	cut := text[:titleLimit]
	for !utf8.RuneStart(text[len(cut)]) {
		cut = cut[:len(cut)-1]
	}
	if i := strings.LastIndexByte(cut, ' '); i > titleLimit/2 {
		cut = cut[:i]
	}
	return cut + "…"
}
func (c *Core) sendLocked(prompt, display string, att *acpclient.Attachment) bool {
	s := &c.state
	s.Err = ""
	s.Notice = ""
	s.PlanReady = false
	mark := len(s.Transcript)
	if c.titleSource == "" {
		c.titleSource = prompt
	}
	s.Transcript = append(s.Transcript, Entry{Kind: EntryUser, Text: display})
	s.Busy = true
	s.Status = "Working…"
	if s.SessionID == "" {
		c.startSessionAndPromptLocked(prompt, display, att, mark)
		return true
	}
	if err := c.client.Prompt(prompt, att); err != nil {
		c.failSendLocked(display, mark, err)
		return false
	}
	return true
}

func (c *Core) startSessionAndPromptLocked(prompt, display string, att *acpclient.Attachment, mark int) {
	client := c.client
	cwd := c.state.Cwd
	go func() {
		s, err := client.NewSession(cwd)
		c.mu.Lock()
		defer c.mu.Unlock()
		if err != nil {
			c.failSendLocked(display, mark, err)
			c.publishLocked(Update{Kind: UpdateState})
			return
		}
		c.applySessionLocked(s)
		if c.state.Stopping {
			c.abortSendLocked(display, mark)
			c.publishLocked(Update{Kind: UpdateState})
			return
		}
		if err := client.Prompt(prompt, att); err != nil {
			c.failSendLocked(display, mark, err)
			c.publishLocked(Update{Kind: UpdateState})
			return
		}
		c.publishLocked(Update{Kind: UpdateState})
	}()
}

func (c *Core) failSendLocked(text string, mark int, err error) {
	c.state.Transcript = c.state.Transcript[:min(mark, len(c.state.Transcript))]
	c.state.Busy = false
	c.state.Status = ""
	c.state.Err = err.Error()
	c.state.Notice = "Draft restored"
	c.publishLocked(Update{Kind: UpdateRestoreDraft, Text: text})
}

func (c *Core) abortSendLocked(text string, mark int) {
	c.state.Transcript = c.state.Transcript[:min(mark, len(c.state.Transcript))]
	c.state.Busy = false
	c.state.Status = ""
	c.state.Stopping = false
	c.state.Notice = "Stopped — prompt restored"
	c.publishLocked(Update{Kind: UpdateRestoreDraft, Text: text})
}

func (c *Core) promptDoneLocked(e acpclient.Event) string {
	s := &c.state
	s.Busy = false
	s.Status = ""
	restore := ""
	if s.Stopping || e.Err != nil {
		restore = s.LastPrompt
		switch {
		case s.Stopping && restore != "":
			s.Notice = "Stopped — prompt restored"
		case s.Stopping:
			s.Notice = "Stopped"
		case restore != "":
			s.Notice = "Draft restored"
		default:
			s.Notice = "Prompt failed"
		}
		s.Stopping = false
	} else {
		s.Notice = ""
		if s.Mode == modePlan && s.PlanPath != "" {
			s.PlanReady = true
		}
	}
	s.LastPrompt = ""
	if e.Err != nil {
		s.Err = e.Err.Error()
	}
	return restore
}

func (c *Core) drainQueue() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state.Busy || c.client == nil {
		return
	}
	prompt, display, att, ok := c.nextBatchLocked()
	if !ok {
		return
	}
	c.sendLocked(prompt, display, att)
	c.publishLocked(Update{Kind: UpdateState})
}

func (c *Core) nextBatchLocked() (string, string, *acpclient.Attachment, bool) {
	if len(c.state.Queue) == 0 {
		return "", "", nil, false
	}
	first := c.state.Queue[0]
	batch := []QueuedPrompt{first}
	rest := c.state.Queue[1:]
	for len(rest) > 0 && rest[0].Mode == first.Mode {
		batch = append(batch, rest[0])
		rest = rest[1:]
	}
	c.state.Queue = rest
	prompts := make([]string, len(batch))
	displays := make([]string, len(batch))
	var att *acpclient.Attachment
	for i, q := range batch {
		prompts[i] = q.Prompt
		displays[i] = q.Display
		if att == nil {
			att = q.Att
		}
	}
	return strings.Join(prompts, "\n\n"), strings.Join(displays, "\n\n"), att, true
}

func (c *Core) appendTextLocked(kind EntryKind, text string) {
	if n := len(c.state.Transcript); n > 0 && c.state.Transcript[n-1].Kind == kind {
		c.state.Transcript[n-1].Text += text
		return
	}
	c.state.Transcript = append(c.state.Transcript, Entry{Kind: kind, Text: text})
}

func (c *Core) updateToolCallLocked(e acpclient.Event) {
	for i := len(c.state.Transcript) - 1; i >= 0; i-- {
		entry := &c.state.Transcript[i]
		if entry.Kind != EntryTool || entry.ToolID != e.ToolCallID {
			continue
		}
		if e.Title != "" {
			entry.Text = e.Title
		}
		if e.Status != "" {
			entry.Status = e.Status
		}
		if e.ToolInput != "" {
			entry.ToolInput = e.ToolInput
			entry.Todos, entry.TodoList = parseTodos(e.ToolInput)
			c.pinTodosLocked(entry.ToolID, entry.Todos, entry.TodoList)
			c.capturePlanPathLocked(entry.ToolKind, e.ToolInput)
		}
		if e.ToolOutput != "" {
			entry.ToolOutput = e.ToolOutput
		}
		return
	}
}

// pinTodosLocked keeps the newest todo list at the session level so a renderer
// that only sees a window of the transcript still shows the task panel.
func (c *Core) pinTodosLocked(toolID string, todos []acpclient.PlanEntry, ok bool) {
	if !ok {
		return
	}
	c.state.TodoToolID = toolID
	c.state.Todos = todos
}

// toolHeader rebuilds the line a live tool call shows in its header, from the
// raw input a replayed call carries but never summarizes.
func toolHeader(cwd, name, input string) string {
	var in struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
		Include string `json:"include"`
		Offset  int    `json:"offset"`
		Limit   int    `json:"limit"`
	}
	if json.Unmarshal([]byte(input), &in) != nil {
		return ""
	}
	short := func(p string) string {
		if p == "" {
			return ""
		}
		if cwd != "" {
			if rel, ok := strings.CutPrefix(p, cwd+"/"); ok {
				return rel
			}
		}
		return p
	}
	switch name {
	case "read":
		start := in.Offset
		if start == 0 {
			start = 1
		}
		header := fmt.Sprintf("%s:%d", short(in.Path), start)
		if in.Limit > 0 {
			header += fmt.Sprintf("-%d", start+in.Limit-1)
		}
		return header
	case "edit", "write", "multiedit", "edit_lines", "insert_lines":
		return short(in.Path)
	case "grep":
		header := in.Pattern
		if in.Include != "" {
			header += " [" + in.Include + "]"
		}
		if in.Path != "" {
			header += " " + short(in.Path)
		}
		return header
	}
	return ""
}

func (c *Core) capturePlanPathLocked(kind, input string) {
	if c.state.Mode != modePlan || kind != toolKindEdit {
		return
	}
	var in struct {
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
	}
	if json.Unmarshal([]byte(input), &in) != nil {
		return
	}
	path := in.Path
	if path == "" {
		path = in.FilePath
	}
	if path != "" {
		c.state.PlanPath = path
	}
}

func (c *Core) askPermissionLocked(req *acpclient.PermissionRequest) {
	if c.state.Permission != nil {
		c.state.Permission.Reply <- ""
	}
	c.state.Permission = req
}

func (c *Core) askElicitationLocked(req *acpclient.ElicitationRequest) {
	if c.state.Elicitation != nil {
		c.state.Elicitation.Reply <- acpclient.ElicitationReply{Decline: true}
	}
	c.state.Elicitation = req
}

func (c *Core) resetLocked() {
	s := &c.state
	c.titleSource = ""
	s.Transcript = nil
	s.Plan = nil
	s.Todos = nil
	s.TodoToolID = ""
	s.PlanPath = ""
	s.PlanReady = false
	s.Busy = false
	s.Err = ""
	s.Notice = ""
	s.LastPrompt = ""
	s.Stopping = false
	s.ContextUsed, s.ContextSize = 0, 0
	s.CostAmount, s.CostCurrency = 0, ""
	s.ConfigSeen = false
	s.Resumed = false
	s.Commands = nil
	s.Queue = nil
	if s.Permission != nil {
		s.Permission.Reply <- ""
		s.Permission = nil
	}
	if s.Elicitation != nil {
		s.Elicitation.Reply <- acpclient.ElicitationReply{Decline: true}
		s.Elicitation = nil
	}
}

func (c *Core) applySessionLocked(s acpclient.Session) {
	c.state.SessionID = s.ID
	c.state.Sessions = slices.DeleteFunc(c.state.Sessions, func(item acpclient.SessionInfo) bool {
		return item.ID == s.ID
	})
	if !c.state.ConfigSeen {
		c.state.Models = s.Models
		c.state.Model = s.Model
	}
	c.state.Modes = s.Modes
	c.state.Mode = s.Mode
}

func (c *Core) refreshBranch(dir string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
		branch := ""
		if err == nil {
			branch = strings.TrimSpace(string(out))
			if branch == "HEAD" {
				branch = ""
			}
		}
		c.mu.Lock()
		c.state.Branch = branch
		c.publishLocked(Update{Kind: UpdateState})
		c.mu.Unlock()
	}()
}

// ParseTodos reads a todo tool call's input, which is a JSON object with a
// "todos" array. Other tool inputs return nil.
func ParseTodos(input string) []acpclient.PlanEntry {
	todos, _ := parseTodos(input)
	return todos
}

// ParseTodoList also reports whether the input carried a "todos" key, so an
// empty list can clear a pinned panel.
func ParseTodoList(input string) ([]acpclient.PlanEntry, bool) {
	return parseTodos(input)
}

// parseTodos also reports whether the input carried a "todos" key, so an
// empty list can clear the pinned panel.
func parseTodos(input string) ([]acpclient.PlanEntry, bool) {
	var v struct {
		Todos []struct {
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"todos"`
	}
	if json.Unmarshal([]byte(input), &v) != nil || v.Todos == nil {
		return nil, false
	}
	todos := make([]acpclient.PlanEntry, len(v.Todos))
	for i, t := range v.Todos {
		todos[i] = acpclient.PlanEntry{Content: t.Content, Status: t.Status}
	}
	return todos, true
}
