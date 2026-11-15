package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"gomaki/internal/acpclient"
)

// TestResumeReplaysIntoTranscript loads a stored session through the real
// agent and checks the replayed events reach the app's transcript. It needs
// MAKI_ACP_TEST=1 and a stored session in this project.
func TestResumeReplaysIntoTranscript(t *testing.T) {
	if os.Getenv("MAKI_ACP_TEST") == "" {
		t.Skip("set MAKI_ACP_TEST=1 to run against maki acp")
	}
	if _, err := exec.LookPath("maki"); err != nil {
		t.Skip("maki is not on PATH")
	}

	c, err := acpclient.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	cwd, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := c.ListSessions(cwd)
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range sessions {
		if _, err := c.LoadSession(cwd, s.ID); err != nil {
			continue
		}
		a := newTestApp()
		a.events = c.Events()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case e := <-c.Events():
				a.apply(e)
			case <-deadline:
				t.Fatal("replayed session left the transcript empty")
			default:
				if len(a.transcript) > 0 {
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
	}
	t.Skip("no loadable session with a transcript")
}
