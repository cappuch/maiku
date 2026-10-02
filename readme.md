# maiku

[pi](https://github.com/earendil-works/pi) but in golang and with better cache optimizations and tooling

## Build

To build the UI:
```bash
go build -o bin/maiku ./cmd/maiku

export PATH="$(go env GOPATH)/bin:$PATH"
cd desktop && wails build
open build/bin/maiku.app
```

# CLI

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
