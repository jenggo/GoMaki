# GoMaki

A native desktop client for [Maki](https://maki.sh), the AI coding agent. It starts `maki acp` as a child process, speaks ACP over stdio, and draws its own window with [mygo](https://github.com/egoist/mygo). There is no webview and no terminal UI in between.

Maki still does the work. GoMaki is a front end: sessions, prompts, tool calls, permissions, questions, models, modes, and usage all come from the ACP server.

## What you get

- A single-window chat view with streaming agent text, thoughts, and tool calls.
- A task panel above the composer: it summarizes progress and expands to the full list.
- A running-tasks window (Cmd+Shift+B, or click `N running` in the status bar) for tool calls still in progress, including delegated agents.
- Click a tool call to expand its raw input and output.
- Thoughts stay collapsed to their last five lines. Click the header to read the whole thing.
- A session picker on first open. Click a stored session to continue it, or type a prompt to start a new one.
- A model menu. The list fills in as providers answer.
- Build and plan modes. Click the mode in the status bar to switch.
- Permission dialogs for tool calls, and native forms for Maki's `question` tool.
- Notifications while the window is in the background: a permission request, or a finished turn.
- Attach an image or file to the next prompt (File > Attach Image or File…, or Cmd+Shift+A).
- Token usage in the status bar (`27.4k/200k (14%)`), with cost when Maki reports it.
- The status bar shows the workspace folder and its Git branch (`project (main)`), plus an icon for ready or working.
- Light and dark themes, a system theme, and a monospace-only font picker with font sizes.
- Custom slash commands from your user and project command folders.
- Copy from the transcript with right-click, or with Ctrl+C after selecting text.
- Reload the app from File > Reload (Cmd+R) or `/reload`, resuming the current session.

## Requirements

- Linux with OpenGL. This is the only target for now.
- Go 1.27 or newer to build.
- `maki` on `PATH`, with at least one provider authenticated. Run `maki auth status` if you are not sure.
- A project folder Maki can work in.

GoMaki starts Maki as:

```sh
maki --trust acp
```

`--trust` loads the project's `.maki` config without a trust prompt. Open only folders you trust.

## Run it

```sh
go run ./cmd/gomaki
```

Or build a binary:

```sh
go build -o gomaki ./cmd/gomaki
./gomaki
```

Add `-c` to continue the most recent session in the current folder:

```sh
./gomaki -c
```

Launch it from the project you want to work in. The session list starts there. File > New Session can point a new session at another folder.

## First open

If the folder has stored sessions, GoMaki lists them. Click one to load its transcript.

If it has none, the composer is ready. Type a prompt and press Enter. GoMaki creates a session in that folder and sends the prompt.

If `maki` is missing, the window stays open and shows the error at the top. Install Maki, then press Retry. The composer stays disabled until there is a connection.

## Keys

| Key | What it does |
| --- | --- |
| Enter | Send the prompt |
| Shift+Enter | New line in the composer |
| Esc twice | Stop the current turn, or restore the last prompt when the composer is empty |
| Ctrl+C / Cmd+C | Copy the selected transcript text, or clear the composer when nothing is selected |
| Cmd+Shift+A | Attach an image or file to the next prompt |
| Cmd+R | Reload the app and resume the current session |
| Cmd+Shift+B | Open the running-tasks window |
| `/` | Open the custom command palette |
| `/reload` | Reload the app and resume the current session |
| Click a tool call | Expand or collapse its input and output |

Esc twice on an empty composer removes the transcript from the last user message onward and puts that message back in the composer. This is a UI rewind. ACP has no history rewind, so Maki's own context still contains the turn you removed.

## Custom commands

GoMaki reads the same command folders as Maki:

- `~/.config/maki/commands/`
- `~/.claude/commands/`
- `.maki/commands/` and `.claude/commands/` from the working directory up to the Git root

Commands are Markdown files. Frontmatter can set `name` and `description`. `$ARGUMENTS` in the body is replaced with what you type after the command. Project commands override user commands, and the repo root wins over nested folders.

Type `/` in the composer and pick one. If you add a file while GoMaki is running, use File > Reload Commands. The transcript shows the command line; Maki receives the expanded prompt.

Maki's built-in TUI commands such as `/help`, `/model`, `/new`, `/theme`, and `/sessions` are not exposed over ACP today. Use GoMaki's model menu, the mode in the status bar, and the session picker instead.

### OpenSpec skills

GoMaki also exposes the OpenSpec skills installed in Maki's config dir as `/opsx:<id>` commands:

`/opsx:explore`, `/opsx:propose`, `/opsx:apply`, `/opsx:update`, `/opsx:sync`, `/opsx:archive`, `/opsx:new`, `/opsx:continue`, `/opsx:ff`, `/opsx:verify`, `/opsx:bulk-archive`, `/opsx:onboard`

Discovery reads `~/.config/maki/.agents/skills/<skill>/SKILL.md`. A command exists only when its file is on disk, so a machine without OpenSpec installed shows none. The command sends the skill body as the prompt; arguments are appended when the body has no `$ARGUMENTS` placeholder. Run `openspec init` in the project once so the workflows have an `openspec/` root to write into.

After a plan-mode turn writes its plan, a **Create change from plan** action appears above the composer when the fast-forward skill is installed. It switches the session to build mode and hands the plan file to `/opsx:ff`, so the change and its artifacts are generated in one pass.

## Limitations

- ACP is the boundary. Anything Maki's TUI does without sending a prompt is not available here. That includes built-in slash commands and MCP prompts in the Maki version this was built against.
- Background jobs and delegated agents are only visible as far as Maki reports them over ACP. A subagent arrives as a tool call; a background job that outlives its tool call may only show up again when its result lands in the conversation. The running-tasks window lists tool calls still marked in progress.
- The session list follows the current working folder. It starts as the folder you launched from; File > New Session can change it.
- A session that is already open in another Maki process cannot be loaded. GoMaki shows the error instead of closing silently.
- Esc rewind only changes the transcript and the composer. Maki's context is not rolled back.
- Permission and question dialogs depend on what the agent sends over ACP.
- Provider login and API keys stay in Maki. GoMaki never reads or stores them.
- Monospace fonts only. The font picker does not list proportional families.

## How the code is arranged

- `cmd/gomaki/` holds the app: view, state, settings, themes, menu, and command discovery.
- `internal/acpclient/` starts Maki, speaks ACP, and turns session updates into events for the UI.

The UI does not run the agent. It sends prompts and renders what ACP gives back.

## Development

```sh
go build ./...
go test ./...
golangci-lint run
```

Most tests run without Maki. The real ACP tests are opt-in and need a configured Maki with at least one stored session in this project:

```sh
MAKI_ACP_TEST=1 go test ./internal/acpclient -run TestSessionLifecycle -count=1
MAKI_ACP_TEST=1 go test ./cmd/gomaki -run TestResumeReplaysIntoTranscript -count=1
```

The first checks session creation, mode switching, listing, loading, and replay. The second checks that a loaded session reaches the transcript.

## Credits

GoMaki is a client, not a fork of Maki. Maki is the agent. The window and widgets come from mygo. The protocol side uses [coder/acp-go-sdk](https://github.com/coder/acp-go-sdk).
