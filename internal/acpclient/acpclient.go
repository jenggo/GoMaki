package acpclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// Event kinds delivered by a Client.
type EventKind int

const (
	EventUserText EventKind = iota
	EventText
	EventThought
	EventToolCall
	EventToolCallUpdate
	EventPlan
	EventConfig
	EventMode
	EventPermission
	EventElicitation
	EventUsage
	EventCommands
	EventPromptDone
)

// Event is a state change from the agent. Permission and elicitation events
// carry a request whose Reply channel the UI answers on.
type Event struct {
	Err error

	Permission  *PermissionRequest
	Elicitation *ElicitationRequest

	Text         string
	ToolCallID   string
	Title        string
	ToolKind     string
	Status       string
	ToolInput    string
	ToolOutput   string
	CostCurrency string
	Model        string
	Mode         string
	StopReason   string
	Plan         []PlanEntry
	Models       []string
	Commands     []Command
	ContextUsed  int64
	ContextSize  int64
	CostAmount   float64
	Kind         EventKind
}

// PlanEntry is one task in the agent's plan.
type PlanEntry struct {
	Content string
	Status  string
}

// Command is a slash command the agent advertised over ACP.
type Command struct {
	Name        string
	Description string
}

// PermissionRequest asks the user to choose one of the agent's options. An
// empty reply cancels the request.
type PermissionRequest struct {
	Reply   chan string
	Title   string
	Options []PermissionOption
}

// PermissionOption is one choice of a permission request.
type PermissionOption struct {
	ID   string
	Name string
	Kind string
}

// ElicitationRequest is a form the agent wants the user to fill in. Fields
// are ordered as the agent sent them.
type ElicitationRequest struct {
	Reply   chan ElicitationReply
	Message string
	Fields  []ElicitationField
}

// ElicitationField is one question of an elicitation form. Options presents
// predefined choices; empty Options means free text. Multi allows several
// options to be selected.
type ElicitationField struct {
	Name        string
	Title       string
	Description string
	Options     []ElicitationOption
	Multi       bool
}

// ElicitationOption is one predefined answer. Value is what the agent
// receives; Label is what the user reads.
type ElicitationOption struct {
	Value string
	Label string
}

// ElicitationReply is the user's answer. Answers maps field names to a string
// or, for multi-select fields, a []string.
type ElicitationReply struct {
	Answers map[string]any
	Decline bool
}

// Session is the state a new or loaded session starts with.
type Session struct {
	ID     string
	Model  string
	Mode   string
	Models []string
	Modes  []string
}

// SessionInfo describes a stored session for the resume picker.
type SessionInfo struct {
	ID        string
	Title     string
	UpdatedAt string
}

const modelConfigID acp.SessionConfigId = "model"

// Attachment is a file sent with a prompt: an image if MimeType is an
// image type, otherwise a link the agent can open itself.
type Attachment struct {
	Path     string
	MimeType string
	Data     []byte
}

// Client spawns `maki acp` and speaks ACP to it.
type Client struct {
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	conn         *acp.ClientSideConnection
	events       chan Event
	wake         chan struct{}
	sessionID    acp.SessionId
	modelID      acp.SessionConfigId
	imageSupport bool
}

// Start spawns and initializes the agent process in dir.
func Start(dir string) (*Client, error) {
	cmd := exec.CommandContext(context.Background(), "maki", "--trust", "acp")
	cmd.Dir = dir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start maki: %w", err)
	}

	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		events:  make(chan Event, 512),
		wake:    make(chan struct{}, 1),
		modelID: modelConfigID,
	}
	c.conn = acp.NewClientSideConnection(c, stdin, stdout)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := c.conn.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientInfo:      &acp.Implementation{Name: "gomaki", Version: "0.1.0"},
		ClientCapabilities: acp.ClientCapabilities{
			Elicitation: &acp.ElicitationCapabilities{Form: &acp.ElicitationFormCapabilities{}},
		},
	})
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("initialize maki acp: %w", err)
	}
	c.imageSupport = resp.AgentCapabilities.PromptCapabilities.Image
	return c, nil
}

// Events returns the channel of agent updates. The channel is not closed.
func (c *Client) Events() <-chan Event { return c.events }

// Wake signals that an event is ready. Reading it never consumes the event,
// so one goroutine can ask for a frame while another drains the updates.
func (c *Client) Wake() <-chan struct{} { return c.wake }

// closeGrace is how long Close waits for the agent to exit after its stdin
// ends. Maki saves the running session on shutdown, even mid-turn, so the
// wait is what keeps a force-exited turn from being lost.
const closeGrace = 5 * time.Second

// Close ends the agent. Closing stdin lets the agent save the session before
// it exits; an agent still running after the grace is killed.
func (c *Client) Close() error {
	if c.cmd.Process == nil {
		return nil
	}
	_ = c.stdin.Close()
	done := make(chan struct{})
	go func() {
		_, _ = c.cmd.Process.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(closeGrace):
		_ = c.cmd.Process.Kill()
		<-done
		return nil
	}
}

// SupportsImages reports whether the agent takes images in a prompt.
func (c *Client) SupportsImages() bool { return c.imageSupport }

// NewSession creates a session for cwd.
func (c *Client) NewSession(cwd string) (Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := c.conn.NewSession(ctx, acp.NewSessionRequest{Cwd: cwd, McpServers: []acp.McpServer{}})
	if err != nil {
		return Session{}, err
	}
	c.sessionID = resp.SessionId
	return c.sessionState(resp.SessionId, resp.ConfigOptions, resp.Modes), nil
}

// LoadSession loads an existing session and replays its transcript through
// the event channel.
func (c *Client) LoadSession(cwd, id string) (Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := c.conn.LoadSession(ctx, acp.LoadSessionRequest{
		Cwd:        cwd,
		SessionId:  acp.SessionId(id),
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		return Session{}, err
	}
	c.sessionID = acp.SessionId(id)
	return c.sessionState(c.sessionID, resp.ConfigOptions, resp.Modes), nil
}

// ListSessions lists stored sessions for cwd, newest first.
func (c *Client) ListSessions(cwd string) ([]SessionInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := c.conn.ListSessions(ctx, acp.ListSessionsRequest{Cwd: &cwd})
	if err != nil {
		return nil, err
	}
	infos := make([]SessionInfo, 0, len(resp.Sessions))
	for _, s := range resp.Sessions {
		info := SessionInfo{ID: string(s.SessionId)}
		if s.Title != nil {
			info.Title = *s.Title
		}
		if s.UpdatedAt != nil {
			info.UpdatedAt = *s.UpdatedAt
		}
		infos = append(infos, info)
	}
	sort.SliceStable(infos, func(i, j int) bool {
		ti, ei := time.Parse(time.RFC3339, infos[i].UpdatedAt)
		tj, ej := time.Parse(time.RFC3339, infos[j].UpdatedAt)
		if ei != nil || ej != nil {
			return false
		}
		return ti.After(tj)
	})
	return infos, nil
}

// Prompt sends a user prompt and streams the turn through the event channel.
// It returns once the prompt is accepted; the turn ends with EventPromptDone.
func (c *Client) Prompt(text string, att *Attachment) error {
	if c.sessionID == "" {
		return errors.New("no active session")
	}
	blocks := []acp.ContentBlock{acp.TextBlock(text)}
	if att != nil {
		blocks = append(blocks, c.attachmentBlock(att))
	}
	go func() {
		resp, err := c.conn.Prompt(context.Background(), acp.PromptRequest{
			SessionId: c.sessionID,
			Prompt:    blocks,
		})
		c.emit(Event{Kind: EventPromptDone, Err: err, StopReason: string(resp.StopReason)})
	}()
	return nil
}

func (c *Client) attachmentBlock(att *Attachment) acp.ContentBlock {
	if strings.HasPrefix(att.MimeType, "image/") && c.imageSupport {
		return acp.ImageBlock(base64.StdEncoding.EncodeToString(att.Data), att.MimeType)
	}
	link := acp.ResourceLinkBlock(filepath.Base(att.Path), (&url.URL{Scheme: "file", Path: att.Path}).String())
	link.ResourceLink.MimeType = &att.MimeType
	size := int64(len(att.Data))
	link.ResourceLink.Size = &size
	return link
}

// Cancel stops the running turn.
func (c *Client) Cancel() error {
	if c.sessionID == "" {
		return errors.New("no active session")
	}
	return c.conn.Cancel(context.Background(), acp.CancelNotification{SessionId: c.sessionID})
}

// SetModel switches the session model.
func (c *Client) SetModel(model string) error {
	if c.sessionID == "" {
		return errors.New("no active session")
	}
	_, err := c.conn.SetSessionConfigOption(context.Background(), acp.SetSessionConfigOptionRequest{
		ValueId: &acp.SetSessionConfigOptionValueId{
			SessionId: c.sessionID,
			ConfigId:  c.modelID,
			Value:     acp.SessionConfigValueId(model),
		},
	})
	return err
}

// SetMode switches the session mode.
func (c *Client) SetMode(mode string) error {
	if c.sessionID == "" {
		return errors.New("no active session")
	}
	_, err := c.conn.SetSessionMode(context.Background(), acp.SetSessionModeRequest{
		SessionId: c.sessionID,
		ModeId:    acp.SessionModeId(mode),
	})
	return err
}

func (c *Client) emit(e Event) {
	c.events <- e
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Client) sessionState(id acp.SessionId, opts []acp.SessionConfigOption, modes *acp.SessionModeState) Session {
	s := Session{ID: string(id)}
	s.Models, s.Model, c.modelID = modelOptions(opts)
	if modes != nil {
		s.Mode = string(modes.CurrentModeId)
		for _, m := range modes.AvailableModes {
			s.Modes = append(s.Modes, string(m.Id))
		}
	}
	return s
}

func renderValue(v any) string {
	if v == nil {
		return ""
	}
	if raw, ok := v.(json.RawMessage); ok && len(raw) == 0 {
		return ""
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func modelOptions(opts []acp.SessionConfigOption) ([]string, string, acp.SessionConfigId) {
	for _, o := range opts {
		sel := o.Select
		if sel == nil {
			continue
		}
		if sel.Id != "model" && (sel.Category == nil || *sel.Category != acp.SessionConfigOptionCategoryModel) {
			continue
		}
		return collectModels(sel.Options), string(sel.CurrentValue), sel.Id
	}
	return nil, "", modelConfigID
}

func toolCallUpdateEvent(u *acp.SessionToolCallUpdate) Event {
	e := Event{Kind: EventToolCallUpdate, ToolCallID: string(u.ToolCallId)}
	if title, ok := u.Title.Value(); ok {
		e.Title = title
	}
	if status, ok := u.Status.Value(); ok {
		e.Status = string(status)
	}
	if raw, ok := u.RawInput.Value(); ok {
		e.ToolInput = renderValue(raw)
	}
	if raw, ok := u.RawOutput.Value(); ok {
		e.ToolOutput = renderValue(raw)
	}
	return e
}

func commandList(list []acp.AvailableCommand) []Command {
	commands := make([]Command, 0, len(list))
	for _, cmd := range list {
		commands = append(commands, Command{Name: cmd.Name, Description: cmd.Description})
	}
	return commands
}

func collectModels(options acp.SessionConfigSelectOptions) []string {
	var models []string
	if options.Ungrouped != nil {
		for _, opt := range *options.Ungrouped {
			models = append(models, string(opt.Value))
		}
	}
	if options.Grouped != nil {
		for _, g := range *options.Grouped {
			for _, opt := range g.Options {
				models = append(models, string(opt.Value))
			}
		}
	}
	return models
}

// SessionUpdate implements acp.Client.
func (c *Client) SessionUpdate(_ context.Context, n acp.SessionNotification) error {
	u := n.Update
	switch {
	case u.UserMessageChunk != nil:
		if t := u.UserMessageChunk.Content.Text; t != nil {
			c.emit(Event{Kind: EventUserText, Text: t.Text})
		}
	case u.AgentMessageChunk != nil:
		if t := u.AgentMessageChunk.Content.Text; t != nil {
			c.emit(Event{Kind: EventText, Text: t.Text})
		}
	case u.AgentThoughtChunk != nil:
		if t := u.AgentThoughtChunk.Content.Text; t != nil {
			c.emit(Event{Kind: EventThought, Text: t.Text})
		}
	case u.ToolCall != nil:
		c.emit(Event{
			Kind:       EventToolCall,
			ToolCallID: string(u.ToolCall.ToolCallId),
			Title:      u.ToolCall.Title,
			ToolKind:   string(u.ToolCall.Kind),
			Status:     string(u.ToolCall.Status),
			ToolInput:  renderValue(u.ToolCall.RawInput),
			ToolOutput: renderValue(u.ToolCall.RawOutput),
		})
	case u.ToolCallUpdate != nil:
		c.emit(toolCallUpdateEvent(u.ToolCallUpdate))
	case u.Plan != nil:
		entries := make([]PlanEntry, 0, len(u.Plan.Entries))
		for _, p := range u.Plan.Entries {
			entries = append(entries, PlanEntry{Content: p.Content, Status: string(p.Status)})
		}
		c.emit(Event{Kind: EventPlan, Plan: entries})
	case u.ConfigOptionUpdate != nil:
		models, model, _ := modelOptions(u.ConfigOptionUpdate.ConfigOptions)
		if models != nil {
			c.emit(Event{Kind: EventConfig, Models: models, Model: model})
		}
	case u.CurrentModeUpdate != nil:
		c.emit(Event{Kind: EventMode, Mode: string(u.CurrentModeUpdate.CurrentModeId)})
	case u.AvailableCommandsUpdate != nil:
		c.emit(Event{Kind: EventCommands, Commands: commandList(u.AvailableCommandsUpdate.AvailableCommands)})
	case u.UsageUpdate != nil:
		e := Event{Kind: EventUsage, ContextUsed: u.UsageUpdate.Used, ContextSize: u.UsageUpdate.Size}
		if u.UsageUpdate.Cost != nil {
			e.CostAmount = u.UsageUpdate.Cost.Amount
			e.CostCurrency = u.UsageUpdate.Cost.Currency
		}
		c.emit(e)
	}
	return nil
}

// RequestPermission implements acp.Client. It parks the agent on a reply
// channel while the UI shows the options.
func (c *Client) RequestPermission(ctx context.Context, p acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	req := &PermissionRequest{
		Reply: make(chan string, 1),
	}
	if title, ok := p.ToolCall.Title.Value(); ok {
		req.Title = title
	}
	for _, o := range p.Options {
		req.Options = append(req.Options, PermissionOption{
			ID:   string(o.OptionId),
			Name: o.Name,
			Kind: string(o.Kind),
		})
	}
	c.emit(Event{Kind: EventPermission, Permission: req})

	select {
	case id := <-req.Reply:
		if id == "" {
			return cancelledPermission(), nil
		}
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{
				Selected: &acp.RequestPermissionOutcomeSelected{OptionId: acp.PermissionOptionId(id)},
			},
		}, nil
	case <-ctx.Done():
		return cancelledPermission(), nil
	}
}

func cancelledPermission() acp.RequestPermissionResponse {
	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{Cancelled: &acp.RequestPermissionOutcomeCancelled{}},
	}
}

// CreateElicitation implements acp.Client. Only form elicitations are
// rendered; URL elicitations are declined.
func (c *Client) CreateElicitation(ctx context.Context, p acp.CreateElicitationRequest) (acp.CreateElicitationResponse, error) {
	if p.Form == nil {
		return declinedElicitation(), nil
	}
	req := &ElicitationRequest{
		Message: p.Form.Message,
		Fields:  parseFields(p.Form.RequestedSchema.Properties),
		Reply:   make(chan ElicitationReply, 1),
	}
	c.emit(Event{Kind: EventElicitation, Elicitation: req})

	select {
	case reply := <-req.Reply:
		if reply.Decline {
			return declinedElicitation(), nil
		}
		return acp.CreateElicitationResponse{
			Accept: &acp.CreateElicitationAccept{Action: "accept", Content: elicitationContent(reply.Answers)},
		}, nil
	case <-ctx.Done():
		return declinedElicitation(), nil
	}
}

// CompleteElicitation implements acp.Client. URL elicitations are declined,
// so there is nothing to complete.
func (c *Client) CompleteElicitation(context.Context, acp.CompleteElicitationNotification) error {
	return nil
}

func elicitationContent(answers map[string]any) map[string]acp.ElicitationContentValue {
	if len(answers) == 0 {
		return nil
	}
	content := make(map[string]acp.ElicitationContentValue, len(answers))
	for name, answer := range answers {
		switch v := answer.(type) {
		case string:
			content[name] = acp.NewElicitationContentValueString(v)
		case []string:
			content[name] = acp.NewElicitationContentValueStringArray(v...)
		}
	}
	return content
}

func declinedElicitation() acp.CreateElicitationResponse {
	return acp.CreateElicitationResponse{
		Decline: &acp.CreateElicitationDecline{Action: "decline"},
	}
}

func parseFields(props map[string]acp.ElicitationPropertySchema) []ElicitationField {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return fieldKeyLess(keys[i], keys[j]) })

	fields := make([]ElicitationField, 0, len(keys))
	for _, k := range keys {
		prop := props[k]
		f := ElicitationField{Name: k}
		switch {
		case prop.String != nil:
			f.Title = stringValue(prop.String.Title)
			f.Description = stringValue(prop.String.Description)
			f.Options = enumOptions(prop.String.Enum, prop.String.OneOf)
		case prop.Number != nil:
			f.Title = stringValue(prop.Number.Title)
			f.Description = stringValue(prop.Number.Description)
		case prop.Integer != nil:
			f.Title = stringValue(prop.Integer.Title)
			f.Description = stringValue(prop.Integer.Description)
		case prop.Boolean != nil:
			f.Title = stringValue(prop.Boolean.Title)
			f.Description = stringValue(prop.Boolean.Description)
		case prop.Array != nil:
			f.Title = stringValue(prop.Array.Title)
			f.Description = stringValue(prop.Array.Description)
			f.Multi = true
			f.Options = itemOptions(prop.Array.Items)
		}
		if f.Title == "" {
			f.Title = k
		}
		fields = append(fields, f)
	}
	return fields
}

func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func fieldKeyLess(a, b string) bool {
	na, oka := strconv.Atoi(strings.TrimPrefix(a, "q"))
	nb, okb := strconv.Atoi(strings.TrimPrefix(b, "q"))
	if oka == nil && okb == nil {
		return na < nb
	}
	return a < b
}

func itemOptions(items acp.MultiSelectItems) []ElicitationOption {
	switch {
	case items.String != nil:
		return enumOptions(items.String.Enum, nil)
	case items.Titled != nil:
		return enumOptions(nil, items.Titled.AnyOf)
	}
	return nil
}

func enumOptions(enum []string, oneOf []acp.EnumOption) []ElicitationOption {
	if len(oneOf) > 0 {
		out := make([]ElicitationOption, 0, len(oneOf))
		for _, o := range oneOf {
			out = append(out, ElicitationOption{Value: o.Const, Label: o.Title})
		}
		return out
	}
	if len(enum) == 0 {
		return nil
	}
	out := make([]ElicitationOption, 0, len(enum))
	for _, v := range enum {
		out = append(out, ElicitationOption{Value: v, Label: v})
	}
	return out
}

// ReadTextFile implements acp.Client. The client advertises no filesystem
// capability, so the agent should not call it.
func (c *Client) ReadTextFile(context.Context, acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	return acp.ReadTextFileResponse{}, errors.New("filesystem access is not supported")
}

// WriteTextFile implements acp.Client.
func (c *Client) WriteTextFile(context.Context, acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	return acp.WriteTextFileResponse{}, errors.New("filesystem access is not supported")
}

// CreateTerminal implements acp.Client.
func (c *Client) CreateTerminal(context.Context, acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, errors.New("terminals are not supported")
}

// KillTerminal implements acp.Client.
func (c *Client) KillTerminal(context.Context, acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, errors.New("terminals are not supported")
}

// ReleaseTerminal implements acp.Client.
func (c *Client) ReleaseTerminal(context.Context, acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, errors.New("terminals are not supported")
}

// TerminalOutput implements acp.Client.
func (c *Client) TerminalOutput(context.Context, acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, errors.New("terminals are not supported")
}

// WaitForTerminalExit implements acp.Client.
func (c *Client) WaitForTerminalExit(context.Context, acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, errors.New("terminals are not supported")
}
