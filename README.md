# GoMaki

A native desktop client for [Maki](https://maki.sh), the AI coding agent. It starts `maki acp` as a child process, speaks ACP over stdio, and draws its own window with [mygo](https://github.com/egoist/mygo). There is no webview and no terminal UI in between.

Maki still does the work. GoMaki is a front end: sessions, prompts, tool calls, permissions, questions, models, modes, and usage all come from the ACP server.

## What you get

- A single-window chat view with streaming agent text, thoughts, plans, and tool calls.
- Click a tool call to expand its raw input and output.
- Thoughts stay collapsed to their last five lines. Click the header to read the whole thing.
- A session picker on first open. Click a stored session to continue it, or type a prompt to start a new one.
- Model and build/plan mode menus. Model lists fill in as providers answer.
- Permission dialogs for tool calls, and native forms for Maki's `question` tool.
- Token usage in the status bar (`ctx 27.4k/200k`), with cost when Maki reports it.
- Light and dark themes, a system theme, and a monospace-only font picker with font sizes.
- Custom slash commands from your user and project command folders.
- Copy from the transcript with right-click, or with Ctrl+C after selecting text.

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
| Ctrl+C / Cmd+C | Clear the composer |
| `/` | Open the custom command palette |
| Click a tool call | Expand or collapse its input and output |

Esc twice on an empty composer removes the transcript from the last user message onward and puts that message back in the composer. This is a UI rewind. ACP has no history rewind, so Maki's own context still contains the turn you removed.

## Custom commands

GoMaki reads the same command folders as Maki:

- `~/.config/maki/commands/`
- `~/.claude/commands/`
- `.maki/commands/` and `.claude/commands/` from the working directory up to the Git root

Commands are Markdown files. Frontmatter can set `name` and `description`. `$ARGUMENTS` in the body is replaced with what you type after the command. Project commands override user commands, and the repo root wins over nested folders.

Type `/` in the composer and pick one. If you add a file while GoMaki is running, use File > Reload Commands. The transcript shows the command line; Maki receives the expanded prompt.

Maki's built-in TUI commands such as `/help`, `/model`, `/new`, `/theme`, and `/sessions` are not exposed over ACP today. Use GoMaki's menus for model, mode, sessions, and settings instead.

## Limitations

- ACP is the boundary. Anything Maki's TUI does without sending a prompt is not available here. That includes built-in slash commands and MCP prompts in the Maki version this was built against.
- No markdown rendering. Agent text is shown as plain text, including code blocks.
- No image or file attachments. Prompts are text only.
- The session list follows the current working folder. It starts as the folder you launched from; File > New Session can change it.
- A session that is already open in another Maki process cannot be loaded. GoMaki shows the error instead of closing silently.
- Esc rewind only changes the transcript and the composer. Maki's context is not rolled back.
- Permission and question dialogs depend on what the agent sends over ACP.
- Provider login and API keys stay in Maki. GoMaki never reads or stores them.
- Monospace fonts only. The font picker does not list proportional families.

## How the code is arranged

- `cmd/gomaki/` holds the app: view, state, settings, themes, menu, and command discovery.
- `internal/acpclient/` starts Maki, speaks ACP, and turns session updates into events for the UI.
- `openspec/` holds the change proposal and specs used to build this.

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
