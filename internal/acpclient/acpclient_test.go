package acpclient

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRealTurn drives one prompt through the local `maki acp` binary. It
// needs a working model in the normal Maki config, so it only runs when
// MAKI_ACP_TEST=1.
func TestRealTurn(t *testing.T) {
	if os.Getenv("MAKI_ACP_TEST") == "" {
		t.Skip("set MAKI_ACP_TEST=1 to run a real turn against maki acp")
	}
	if _, err := exec.LookPath("maki"); err != nil {
		t.Skip("maki is not on PATH")
	}

	c, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.NewSession(cwd); err != nil {
		t.Fatal(err)
	}
	if err := c.Prompt("Use the read tool to read go.mod, then reply with the module name only."); err != nil {
		t.Fatal(err)
	}

	var text strings.Builder
	var sawTool bool
	deadline := time.After(3 * time.Minute)
	for {
		select {
		case e := <-c.Events():
			switch e.Kind {
			case EventText:
				text.WriteString(e.Text)
			case EventToolCall:
				sawTool = true
			case EventUserText, EventThought, EventToolCallUpdate, EventPlan, EventConfig, EventMode, EventPermission, EventElicitation, EventUsage:
			case EventPromptDone:
				if e.Err != nil {
					t.Fatalf("prompt failed: %v", e.Err)
				}
				if text.Len() == 0 {
					t.Fatal("no agent text arrived")
				}
				if !sawTool {
					t.Fatal("no tool call arrived")
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for the turn")
		}
	}
}

// TestSessionLifecycle exercises the session methods that do not need a
// model: new, mode switching, list, and load.
func TestSessionLifecycle(t *testing.T) {
	if os.Getenv("MAKI_ACP_TEST") == "" {
		t.Skip("set MAKI_ACP_TEST=1 to run against maki acp")
	}
	if _, err := exec.LookPath("maki"); err != nil {
		t.Skip("maki is not on PATH")
	}

	c, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	cwd, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.NewSession(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Modes) == 0 || s.Model == "" {
		t.Fatalf("session = %+v", s)
	}
	if err := c.SetMode("plan"); err != nil {
		t.Fatalf("plan mode: %v", err)
	}
	if err := c.SetMode("build"); err != nil {
		t.Fatalf("build mode: %v", err)
	}
	infos, err := c.ListSessions(cwd)
	if err != nil {
		t.Fatal(err)
	}
	// Try the stored sessions that are not open in this process until one
	// replays its transcript.
	loaded := 0
	for _, info := range infos {
		if info.ID == s.ID {
			continue
		}
		if _, err := c.LoadSession(cwd, info.ID); err != nil {
			continue
		}
		loaded++
		if replayed(c) {
			return
		}
		if loaded == 3 {
			break
		}
	}
	t.Fatal("loading a session replayed no transcript updates")
}

func replayed(c *Client) bool {
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-c.Events():
			switch e.Kind {
			case EventUserText, EventText, EventThought, EventToolCall, EventPlan:
				return true
			case EventToolCallUpdate, EventConfig, EventMode, EventPermission, EventElicitation, EventUsage, EventPromptDone:
			}
		case <-deadline:
			return false
		}
	}
}

func TestStartMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Start(); err == nil {
		t.Fatal("Start succeeded without maki on PATH")
	}
}
