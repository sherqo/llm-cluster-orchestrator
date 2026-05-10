# LLM Cluster Chat Client — TUI

A terminal chat interface for the [llm-cluster-orchestrator](../README.md) built with
[Bubble Tea](https://github.com/charmbracelet/bubbletea) +
[Lipgloss](https://github.com/charmbracelet/lipgloss) +
[Bubbles](https://github.com/charmbracelet/bubbles).

Styled after [OpenCode](https://github.com/opencode/opencode) with a dark
Catppuccin Mocha palette.

---

## Layout

```
┌────────────────────────┬────────────────────────────────────────┐
│  LLM Chat              │  Session title            [tier] user  │
│  ▶ New Session         │                                        │
│    Session 2           │  ╔══════════════════════════════════╗  │
│    Session 3           │  ║   chat messages scroll here      ║  │
│                        │  ╚══════════════════════════════════╝  │
│                        │ ┌──────────────────────────────────┐   │
│                        │ │ Type a message…                  │   │
│  Tab ⇄  ^N new  ^D del │ └──────────────────────────────────┘   │
│                        │  ↑↓ scroll  100%          Enter→send  │
└────────────────────────┴────────────────────────────────────────┘
```

## Usage

```bash
# defaults: MASTER_URL=http://localhost:8080  USER_ID=cli-user  TIER=standard
go run .

# custom endpoint
MASTER_URL=http://mymaster:8080 USER_ID=alice TIER=premium go run .
```

## Key Bindings

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `Shift+Enter` | Insert newline |
| `Tab` | Switch focus: input ↔ sidebar |
| `↑ / k` | Navigate sessions (sidebar focused) |
| `↓ / j` | Navigate sessions (sidebar focused) |
| `Ctrl+N` | New session |
| `Ctrl+D` | Delete current session |
| `Ctrl+C` | Quit |

## Build

```bash
go build -o llm-chat .
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MASTER_URL` | `http://localhost:8080` | Master server base URL |
| `USER_ID` | `cli-user` | User ID sent with each request |
| `TIER` | `standard` | Request tier (`standard` / `premium`) |
