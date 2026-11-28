package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"gomaki/internal/acpclient"
)

func writeCommand(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOpsxCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	skills := filepath.Join(home, ".agents", "skills")
	writeCommand(t, filepath.Join(skills, "openspec", "SKILL.md"),
		"---\nname: openspec\ndescription: Route OpenSpec work\n---\nRouter body.\n")
	writeCommand(t, filepath.Join(skills, "openspec-propose", "SKILL.md"),
		"---\nname: openspec-propose\ndescription: Propose a change\n---\nPropose body.\n")
	writeCommand(t, filepath.Join(skills, "openspec-fix-thing", "SKILL.md"),
		"---\nname: openspec-fix-thing\ndescription: Fix a thing\n---\nFix body with $ARGUMENTS.\n")

	cmds := opsxCommands()
	if len(cmds) != 3 {
		t.Fatalf("commands = %+v", cmds)
	}
	if cmds[0].Name != "/opsx" || !cmds[0].Router || cmds[0].Description != "Route OpenSpec work" {
		t.Fatalf("router = %+v", cmds[0])
	}
	if got := cmds[0].render("where do things stand"); got !=
		"Load the `openspec` skill with the `skill` tool and follow its routing steps. Request: where do things stand" {
		t.Fatalf("router render = %q", got)
	}

	byName := make(map[string]customCommand)
	for _, cmd := range cmds {
		byName[cmd.Name] = cmd
	}

	propose, ok := byName["/opsx:propose"]
	if !ok || propose.Description != "Propose a change" {
		t.Fatalf("propose = %+v", propose)
	}
	if got := propose.render("my-change"); !strings.Contains(got, "**Provided arguments**: my-change") {
		t.Fatalf("render with args = %q", got)
	}

	custom, ok := byName["/opsx:fix-thing"]
	if !ok || !custom.AcceptsArgs {
		t.Fatalf("custom = %+v", custom)
	}
	if got := custom.render("abc"); got != "The fix-thing workflow skill follows.\n\nFix body with abc." {
		t.Fatalf("custom render = %q", got)
	}
}

func TestCommandPalette(t *testing.T) {
	a := newTestApp()
	a.connected = true
	a.cwd = "/test"
	a.commands = []customCommand{{Name: "/opsx:foo", Description: "Foo command"}}
	a.prompt = "/"
	tt := ui.NewTester(a.view, 1000, 720)
	if !tt.HasText("/opsx:foo") || !tt.HasText("Foo command") {
		t.Fatalf("palette missing: %q", tt.Texts())
	}
	r, ok := tt.Find("/opsx:foo")
	if !ok {
		t.Fatal("command not found in palette")
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	if a.prompt != "/opsx:foo " {
		t.Fatalf("prompt = %q", a.prompt)
	}
	tt.Frame()
	tt.Type("x")
	if a.prompt != "/opsx:foo x" {
		t.Fatalf("composer text = %q", a.prompt)
	}
}

func TestExpandedPrompt(t *testing.T) {
	a := newTestApp()
	a.commands = []customCommand{{Name: "/user:foo", Content: "Do $ARGUMENTS.", AcceptsArgs: true}}
	if got := a.expandedPrompt("/user:foo bar"); got != "Do bar." {
		t.Fatalf("expanded = %q", got)
	}
	if got := a.expandedPrompt("plain text"); got != "plain text" {
		t.Fatalf("plain = %q", got)
	}
}

func TestAcpCommandsInPalette(t *testing.T) {
	a := newTestApp()
	a.prompt = "/comp"
	a.acpCommands = []acpclient.Command{{Name: "compact", Description: "Summarize history"}}
	matches := a.commandMatches()
	if len(matches) != 1 || matches[0].Name != "/compact" {
		t.Fatalf("matches = %+v", matches)
	}
	if got := a.expandedPrompt("/compact some guidance"); got != "/compact some guidance" {
		t.Fatalf("acp command must pass through raw, got %q", got)
	}
}
