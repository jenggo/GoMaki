package acpclient

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

func TestPermissionHandoff(t *testing.T) {
	c := &Client{events: make(chan Event, 1)}
	go func() {
		e := <-c.events
		e.Permission.Reply <- "allow_once"
	}()

	resp, err := c.RequestPermission(context.Background(), acp.RequestPermissionRequest{
		ToolCall: acp.ToolCallUpdate{ToolCallId: "t1"},
		Options: []acp.PermissionOption{
			{OptionId: "allow_once", Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Outcome.Selected == nil || resp.Outcome.Selected.OptionId != "allow_once" {
		t.Fatalf("outcome = %+v", resp.Outcome)
	}
}

func TestElicitationHandoff(t *testing.T) {
	c := &Client{events: make(chan Event, 1)}
	go func() {
		e := <-c.events
		e.Elicitation.Reply <- ElicitationReply{Answers: map[string]any{"q1": "A"}}
	}()

	resp, err := c.UnstableCreateElicitation(context.Background(), acp.UnstableCreateElicitationRequest{
		Form: &acp.UnstableCreateElicitationForm{
			Message: "Pick",
			RequestedSchema: acp.UnstableElicitationSchema{
				Properties: map[string]any{
					"q1": map[string]any{"type": "string", "title": "Choose"},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Accept == nil || resp.Accept.Content["q1"] != "A" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestEmitWakesWithoutConsuming(t *testing.T) {
	c := &Client{
		events: make(chan Event, 1),
		wake:   make(chan struct{}, 1),
	}
	c.emit(Event{Kind: EventText, Text: "hello"})

	select {
	case <-c.Wake():
	default:
		t.Fatal("emit did not wake the frame loop")
	}
	select {
	case e := <-c.Events():
		if e.Kind != EventText || e.Text != "hello" {
			t.Fatalf("event = %+v", e)
		}
	default:
		t.Fatal("waking consumed the event")
	}
}

func TestUsageUpdate(t *testing.T) {
	c := &Client{events: make(chan Event, 1)}
	err := c.SessionUpdate(context.Background(), acp.SessionNotification{
		Update: acp.SessionUpdate{
			UsageUpdate: &acp.SessionUsageUpdate{Used: 27394, Size: 200000},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-c.Events():
		if e.Kind != EventUsage || e.ContextUsed != 27394 || e.ContextSize != 200000 {
			t.Fatalf("event = %+v", e)
		}
	default:
		t.Fatal("no usage event")
	}
}
