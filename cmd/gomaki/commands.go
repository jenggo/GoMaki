package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// customCommand is a Markdown prompt template from Maki's command folders.
// Name is the scoped palette name, /user:name or /project:name.
type customCommand struct {
	Name        string
	Description string
	Content     string
	AcceptsArgs bool
}

func (c customCommand) render(args string) string {
	return strings.ReplaceAll(c.Content, "$ARGUMENTS", args)
}

// discoverCommands reads the same folders as Maki: the user's config, the
// global Claude commands, and .maki/.claude commands from cwd up to the git
// root. Project commands override user ones; the repo root wins over nested
// folders, as Maki's own discovery does.
func discoverCommands(cwd string) []customCommand {
	home, _ := os.UserHomeDir()
	config, _ := os.UserConfigDir()
	return discoverCommandsIn(cwd, home, config)
}

func discoverCommandsIn(cwd, home, config string) []customCommand {
	byName := make(map[string]customCommand)

	addDir := func(dir string, project bool) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			cmd, ok := readCommand(filepath.Join(dir, entry.Name()), project)
			if !ok {
				continue
			}
			byName[bareName(cmd.Name)] = cmd
		}
	}

	if config != "" {
		addDir(filepath.Join(config, "maki", "commands"), false)
	}
	if home != "" {
		addDir(filepath.Join(home, ".claude", "commands"), false)
	}

	dir := cwd
	for {
		addDir(filepath.Join(dir, ".claude", "commands"), true)
		addDir(filepath.Join(dir, ".maki", "commands"), true)
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	commands := make([]customCommand, 0, len(byName))
	for _, cmd := range byName {
		commands = append(commands, cmd)
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })
	return commands
}

func bareName(display string) string {
	if _, bare, ok := strings.Cut(display, ":"); ok {
		return bare
	}
	return display
}

func readCommand(path string, project bool) (customCommand, bool) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return customCommand{}, false
	}
	content := strings.TrimSpace(string(blob))
	name, description := "", ""
	if rest, ok := strings.CutPrefix(content, "---"); ok {
		if fm, body, ok := strings.Cut(rest, "\n---"); ok {
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
			content = strings.TrimSpace(body)
		}
	}
	if content == "" {
		return customCommand{}, false
	}
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	scope := "/user:"
	if project {
		scope = "/project:"
	}
	return customCommand{
		Name:        scope + name,
		Description: description,
		Content:     content,
		AcceptsArgs: strings.Contains(content, "$ARGUMENTS"),
	}, true
}

func (a *app) loadCommands() {
	a.commands = discoverCommands(a.cwd)
	a.commandsCwd = a.cwd
}

func (a *app) commandMatches() []customCommand {
	text := a.prompt
	if !strings.HasPrefix(text, "/") || strings.ContainsAny(text, " \n\t") {
		return nil
	}
	query := strings.ToLower(strings.TrimPrefix(text, "/"))
	var matches []customCommand
	for _, cmd := range a.commands {
		if query != "" && !strings.Contains(strings.ToLower(cmd.Name), query) {
			continue
		}
		matches = append(matches, cmd)
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
	cmd, args, ok := a.matchCommand(text)
	if !ok {
		return text
	}
	return cmd.render(args)
}
