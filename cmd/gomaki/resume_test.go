package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"gomaki/internal/session"
)

// TestResumeReplaysIntoTranscript loads a stored session through the real
// agent and checks the replayed events reach the core transcript. It needs
// MAKI_ACP_TEST=1 and a stored session in this project.
func TestResumeReplaysIntoTranscript(t *testing.T) {
	if os.Getenv("MAKI_ACP_TEST") == "" {
		t.Skip("set MAKI_ACP_TEST=1 to run against maki acp")
	}
	if _, err := exec.LookPath("maki"); err != nil {
		t.Skip("maki is not on PATH")
	}

	cwd, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	core := session.New()
	if err := core.Start(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Close() })

	sessions, err := core.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sessions {
		if err := core.LoadSession(s.ID); err != nil {
			continue
		}
		deadline := time.After(5 * time.Second)
		for {
			select {
			case <-deadline:
				t.Fatal("replayed session left the transcript empty")
			default:
				if len(core.Snapshot().Transcript) > 0 {
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
	}
	t.Skip("no loadable session with a transcript")
}
