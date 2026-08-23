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

```bash
export ANTHROPIC_API_KEY=...
./bin/maiku -p "list Go files under ./ai"
./bin/maiku --mode json -p "summarize agent/loop.go"
./bin/maiku --list-models
```

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
