# maiku

insanely lightweight, efficient; a thinner take on [pi](https://github.com/earendil-works/pi), ported to golang, and optimized the fuck out of it

## Build

```bash
go build -o bin/maiku ./cmd/maiku

# Desktop UI (Wails)
export PATH="$(go env GOPATH)/bin:$PATH"
cd desktop && wails build
open build/bin/maiku.app
```

## CLI

GitHub releases include standalone `maiku-cli-<os>-<arch>` archives for macOS
(`darwin`), Linux, and Windows on amd64 and arm64, alongside the desktop app.
Extract the archive and run `maiku` (`maiku.exe` on Windows) from your project
folder. The CLI does not need the desktop app installed.

```bash
export ANTHROPIC_API_KEY=...
./bin/maiku
./bin/maiku --continue
./bin/maiku --provider anthropic --model MODEL_ID "explain this repo"
```

The CLI opens a full-screen terminal workspace with a streaming transcript,
multiline composer, model picker, saved sessions, tool activity, and MCP status.
It shares credentials, settings, skills, and session files with the desktop app.
An optional prompt pre-fills the composer for review before sending.

Enter sends; Alt+Enter (or Ctrl+J) inserts a newline. PgUp/PgDn and the mouse
wheel scroll. Ctrl+N starts a session, Ctrl+S opens saved sessions, and Ctrl+O
opens the loaded model catalog. Escape stops the current run. Ctrl+C stops an
active run or exits when idle. Use `--session PATH_OR_ID` to resume a specific
session in the current folder, and `--help` for startup options.

Slash commands work directly in the composer, including before any provider
has been configured. Type `/help` for the full list; Tab completes commands.

| Command | Action |
|---------|--------|
| `/sessions` | Open the session picker |
| `/sessions new` or `/new` | Start a conversation |
| `/sessions <path or ID>` | Resume a saved conversation |
| `/models` | Open the model picker |
| `/models <provider/model>` | Switch models in the current conversation |
| `/models refresh` | Refresh configured provider catalogs |
| `/providers` | List providers and credential status |
| `/provider add [ID]` | Guided API-key setup or a custom OpenAI-compatible provider |
| `/mcp` | List MCP connections |
| `/mcp add` | Guided stdio, HTTP, or SSE server setup |
| `/mcp reload` | Reconnect servers and refresh the current session's tools |
| `/stop`, `/quit` | Stop a run or exit |

Provider keys and MCP environment/header values use masked input fields.
Setup is saved globally in the same configuration as the desktop app; commands
and credentials are never sent as chat messages. Escape cancels setup before
anything is saved. MCP arguments use a JSON string array, such as
`["-y", "@example/server", "path with spaces"]`; environment variables and HTTP
headers use JSON objects. Prefix a prompt with `//` to send a literal leading `/`.

## Desktop

```bash
cd desktop && wails dev
# or:
open desktop/build/bin/maiku.app
```

## Config

| Path | Purpose |
|------|---------|
| `~/.maiku/agent/settings.json` | Defaults |
| `~/.maiku/agent/auth.json` | API keys |
| `~/.maiku/agent/sessions/` | Sessions |
| `.maiku/` | Project overrides |

Env: `MAIKU_AGENT_DIR`, `MAIKU_SESSION_DIR`

Shell commands use `$SHELL` (falling back to `sh`) on Unix and `%COMSPEC%`
(falling back to `cmd.exe`) on Windows. Override the executable or prepend setup
commands in `settings.json`:

```json
{
  "shellPath": "C:\\Program Files\\PowerShell\\7\\pwsh.exe",
  "shellCommandPrefix": ""
}
```

## Web tools

The default agent toolset includes:

- `web_search` — searches DuckDuckGo's HTML endpoint and returns structured titles, URLs, and snippets. Supports `max_results`, `region`, and day/week/month/year filters; no API key is required.
- `curl` — fetches HTTP(S) page content with a Chrome desktop user agent. It follows redirects and supports custom methods, headers, request bodies, and timeouts.

Both tools cap response output before returning it to the model.

## Subagents

Root Maiku sessions expose a `subagent` tool for delegating self-contained work. Each call runs an independent, ephemeral child with `read`, `bash`, `edit`, and `write`; children cannot delegate again. Independent calls emitted in the same turn run concurrently and return concise Markdown reports to the root orchestrator.

Subagents are enabled by default. Toggle them from the desktop composer; the setting is persisted in `~/.maiku/agent/settings.json` and applied immediately:

```text
/settings subagent false
/settings subagent true
```
