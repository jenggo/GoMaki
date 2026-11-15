package main

import (
	"os"
	"path/filepath"
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

func TestDiscoverCommands(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCommand(t, filepath.Join(dir, ".maki", "commands", "foo.md"), "---\nname: foo\ndescription: Foo command\n---\nDo $ARGUMENTS now.\n")

	cmds := discoverCommandsIn(dir, "", "")
	if len(cmds) != 1 {
		t.Fatalf("commands = %+v", cmds)
	}
	cmd := cmds[0]
	if cmd.Name != "/project:foo" || cmd.Description != "Foo command" {
		t.Fatalf("command = %+v", cmd)
	}
	if !cmd.AcceptsArgs || cmd.render("bar") != "Do bar now." {
		t.Fatalf("render = %q", cmd.render("bar"))
	}
}

func TestProjectCommandOverridesUser(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "config")
	writeCommand(t, filepath.Join(config, "maki", "commands", "foo.md"), "user body\n")
	writeCommand(t, filepath.Join(dir, ".maki", "commands", "foo.md"), "project body\n")

	cmds := discoverCommandsIn(dir, "", config)
	if len(cmds) != 1 || cmds[0].Content != "project body" || cmds[0].Name != "/project:foo" {
		t.Fatalf("commands = %+v", cmds)
	}
}

func TestCommandPalette(t *testing.T) {
	a := newTestApp()
	a.client = &acpclient.Client{}
	a.cwd = "/test"
	a.commandsCwd = "/test"
	a.commands = []customCommand{{Name: "/project:foo", Description: "Foo command"}}
	a.prompt = "/"
	tt := ui.NewTester(a.view, 1000, 720)
	if !tt.HasText("/project:foo") || !tt.HasText("Foo command") {
		t.Fatalf("palette missing: %q", tt.Texts())
	}
	r, ok := tt.Find("/project:foo")
	if !ok {
		t.Fatal("command not found in palette")
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	if a.prompt != "/project:foo " {
		t.Fatalf("prompt = %q", a.prompt)
	}
	tt.Frame()
	tt.Type("x")
	if a.prompt != "/project:foo x" {
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
