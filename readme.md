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
