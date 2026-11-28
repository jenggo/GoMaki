package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// customCommand is a slash command gomaki expands itself. Only OpenSpec
// workflows qualify: Maki advertises its file commands, MCP prompts and
// /compact over ACP, but commands a Lua plugin registers (opsx.lua's /opsx:*)
// stay inside the TUI.
type customCommand struct {
	Name        string
	Description string
	Content     string
	AcceptsArgs bool
	InjectArgs  bool
	Router      bool
}

func (c customCommand) render(args string) string {
	if c.Router {
		if args == "" {
			args = "(ask the user what they want)"
		}
		// The router body is not inlined: the agent loads it, so paying for it
		// once is enough.
		return "Load the `openspec` skill with the `skill` tool and follow its routing steps. Request: " + args
	}
	if c.AcceptsArgs {
		return strings.ReplaceAll(c.Content, "$ARGUMENTS", args)
	}
	if c.InjectArgs && args != "" {
		return c.Content + "\n\n**Provided arguments**: " + args +
			"\n\nTreat these arguments as the input to this workflow (e.g. the change name after " + c.Name +
			"); if a name is given, use it exactly instead of picking a change yourself."
	}
	return c.Content
}

// opsxRoot is the dir name OpenSpec installs its router skill under.
const opsxRoot = "openspec"

// opsxAliases gives OpenSpec's known skill dirs their short /opsx ids. A new
// openspec-* dir without an alias uses its name minus the prefix.
var opsxAliases = map[string]string{
	"openspec-explore":             "explore",
	"openspec-propose":             "propose",
	"openspec-new-change":          "new",
	"openspec-continue-change":     "continue",
	"openspec-ff-change":           "ff",
	"openspec-update-change":       "update",
	"openspec-apply-change":        "apply",
	"openspec-verify-change":       "verify",
	"openspec-sync-specs":          "sync",
	"openspec-archive-change":      "archive",
	"openspec-bulk-archive-change": "bulk-archive",
	"openspec-onboard":             "onboard",
}

// opsxCommands lists the globally installed OpenSpec skills as /opsx commands,
// the same set opsx.lua registers: the router, then one command per
// openspec-* skill dir. Global only, so `openspec update` is run once and every
// project sees the same commands. A command exists only while its SKILL.md is
// on disk.
func opsxCommands() []customCommand {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	skills := filepath.Join(home, ".agents", "skills")
	entries, err := os.ReadDir(skills)
	if err != nil {
		return nil
	}
	var commands []customCommand
	used := make(map[string]bool)
	for _, entry := range entries {
		cmd, ok := opsxCommand(entry.Name(), skills)
		if !ok || used[cmd.Name] {
			continue
		}
		used[cmd.Name] = true
		commands = append(commands, cmd)
	}
	sort.Slice(commands, func(i, j int) bool {
		if commands[i].Router != commands[j].Router {
			return commands[i].Router
		}
		return commands[i].Name < commands[j].Name
	})
	return commands
}

// opsxCommand turns one skill dir into its /opsx command.
func opsxCommand(dir, skills string) (customCommand, bool) {
	if !strings.HasPrefix(dir, opsxRoot) {
		return customCommand{}, false
	}
	name, description, content, ok := readSkill(filepath.Join(skills, dir, "SKILL.md"))
	if !ok {
		return customCommand{}, false
	}
	if dir == opsxRoot || name == opsxRoot {
		return customCommand{Name: "/opsx", Description: description, Router: true}, true
	}
	id := opsxAliases[dir]
	if id == "" {
		id = strings.TrimPrefix(dir, opsxRoot+"-")
	}
	if id == "" {
		return customCommand{}, false
	}
	acceptsArgs := strings.Contains(content, "$ARGUMENTS")
	return customCommand{
		Name:        "/opsx:" + id,
		Description: description,
		Content:     "The " + id + " workflow skill follows.\n\n" + content,
		AcceptsArgs: acceptsArgs,
		InjectArgs:  !acceptsArgs,
	}, true
}

// readSkill reads a SKILL.md: its frontmatter name and description, and the
// body with the frontmatter removed. The dir name stands in for a missing name.
func readSkill(path string) (name, description, body string, ok bool) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return "", "", "", false
	}
	content := strings.TrimSpace(string(blob))
	if rest, ok := strings.CutPrefix(content, "---"); ok {
		if fm, text, ok := strings.Cut(rest, "\n---"); ok {
			for line := range strings.SplitSeq(fm, "\n") {
				key, value, ok := strings.Cut(line, ":")
				if !ok {
					continue
				}
				value = strings.Trim(strings.TrimSpace(value), "\"'")
				switch strings.TrimSpace(key) {
				case "name":
					name = value
				case "description":
					description = value
				}
			}
			content = strings.TrimSpace(text)
		}
	}
	if content == "" {
		return "", "", "", false
	}
	if name == "" {
		name = filepath.Base(filepath.Dir(path))
	}
	return name, description, content, true
}

func (a *app) loadCommands() {
	a.commands = opsxCommands()
}

func (a *app) commandMatches() []customCommand {
	text := a.prompt
	if !strings.HasPrefix(text, "/") || strings.ContainsAny(text, " \n\t") {
		return nil
	}
	query := strings.ToLower(strings.TrimPrefix(text, "/"))
	var matches []customCommand
	seen := make(map[string]bool)
	for _, cmd := range a.commands {
		if query != "" && !strings.Contains(strings.ToLower(cmd.Name), query) {
			continue
		}
		matches = append(matches, cmd)
		seen[strings.ToLower(cmd.Name)] = true
		if len(matches) == 8 {
			break
		}
	}
	for _, cmd := range a.acpCommands {
		if query != "" && !strings.Contains(strings.ToLower(cmd.Name), query) {
			continue
		}
		name := "/" + cmd.Name
		if seen[strings.ToLower(name)] {
			continue
		}
		matches = append(matches, customCommand{Name: name, Description: cmd.Description})
		if len(matches) == 8 {
			break
		}
	}
	return matches
}

func (a *app) matchCommand(text string) (customCommand, string, bool) {
	head, args, _ := strings.Cut(text, " ")
	for _, cmd := range a.commands {
		if cmd.Name == head {
			return cmd, strings.TrimSpace(args), true
		}
	}
	return customCommand{}, "", false
}

func (a *app) expandedPrompt(text string) string {
	head, _, _ := strings.Cut(text, " ")
	for _, cmd := range a.acpCommands {
		if "/"+cmd.Name == head {
			return text
		}
	}
	cmd, args, ok := a.matchCommand(text)
	if !ok {
		return text
	}
	return cmd.render(args)
}
