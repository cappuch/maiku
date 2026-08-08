# maiku desktop

Wails + React + Tailwind UI for the maiku coding agent.

## Dev

```bash
# from repo root
export PATH="$(go env GOPATH)/bin:$PATH"
cd desktop
wails dev
```

## Build

```bash
cd desktop
wails build
open build/bin/maiku.app
```

## Features

- Collapsible session sidebar
- Streaming token transcript + expandable tool calls
- Model + thinking selector
- API key settings (`~/.maiku/agent/auth.json`)
- Open folder (workspace cwd)
- Status bar: input / output / cache rate / total tokens / cost
