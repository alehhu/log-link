# Development Guide

LogLink is built with Go and the [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI library. The entire application lives in a single file: `cmd/loglink/main.go`.

## Getting Started

1. **Clone the Repo:**
   ```bash
   git clone https://github.com/alehhu/log-link
   cd log-link
   ```

2. **Install Dependencies:**
   ```bash
   go mod download
   ```

3. **Run Locally:**
   ```bash
   go run ./cmd/loglink --demo --incident-mode
   ```

4. **Build:**
   ```bash
   go build -o loglink ./cmd/loglink
   ```

---

## Local Testing & Simulation

The `test/` directory contains scripts for development and testing without touching production logs.

### `test/simulator.py`
Simulates a distributed system:
- Generates `api.log`, `db.log`, `worker.log` with correlated request IDs
- Runs an HTTP server at `http://localhost:8080/load` serving a numeric load metric

```bash
python3 test/simulator.py
# In another terminal:
loglink api=api.log db=db.log worker=worker.log \
  --pulse "curl -fsS http://localhost:8080/load" \
  --incident-mode
```

### `test/gen_logs.py`
Generates correlated log files only (no HTTP server).

```bash
python3 test/gen_logs.py
```

### Built-in demo
The fastest way to test the UI without any external setup:

```bash
./loglink --demo --incident-mode --pulse "echo 50"
```

---

## Modifying the TUI

LogLink's TUI is entirely contained within `cmd/loglink/main.go`. Key entry points:

| Component | Description |
|---|---|
| `model` struct | Central state: entries, cursor, filters, metrics, session info, UI mode flags |
| `initialModel()` | Constructs the initial model from CLI config and session data |
| `Update()` | Handles all keyboard input and incoming messages (`logEntryBatch`, `MetricEntry`, etc.) |
| `View()` | Renders the full terminal frame; dispatches to sub-renderers |
| `renderSparklineCompact()` | 4-line compact pulse chart (stats header + 3 bar rows) |
| `renderPulseFullscreen()` | Full-viewport pulse chart with Y-axis, gridlines, and X-axis timestamps |
| `renderLogs()` | Main log timeline with highlight, focus, search, and bookmark rendering |
| `renderModal()` | Details modal with entry info, incident history, and content |
| `insertEntry()` | Binary-search insertion into the sorted `m.entries` slice; maintains `filteredEntries` incrementally |
| `newLogEntry()` | Parses a raw log line: JSON detection, entity extraction, level pre-computation |
| `tryParseJSON()` | Detects and unpacks structured JSON logs; extracts trace entities |
| `batchSource()` | Leading-edge + 50 ms cooldown batching; feeds `logEntryBatch` messages to the TUI |
| `saveSession()` | Merges current incidents into session history and writes `sessions.json` |

### Bubble Tea message types

| Type | Direction | Purpose |
|---|---|---|
| `logEntryBatch` | source goroutine → Update | Batched log lines (20 fps cap) |
| `LogEntry` | demo goroutine → Update | Single log entry (demo mode) |
| `MetricEntry` | pulse goroutine → Update | One numeric metric sample |

### Session file

`~/.config/loglink/sessions.json` — JSON map of named sessions. Each session stores sources, flags, pulse config, description, last-used timestamp, and accumulated incident history.

---

## Adding a New Integration Shortcut

Integration shortcuts are shell command aliases. To add one:

1. Add a field to `cliConfig` (e.g., `nomadAlloc string`)
2. Add a `--nomad-alloc` case in `parseCLI`
3. Add the expansion in `integrationSources`:
   ```go
   if cfg.nomadAlloc != "" {
       sources = append(sources, logSource{
           label: cfg.nomadAlloc,
           arg:   fmt.Sprintf("nomad alloc logs -follow %s", cfg.nomadAlloc),
       })
   }
   ```
4. Document in `docs/integrations.md` and the `--help` block in `main()`

---

## Contributing

1. Create a feature branch from `master`.
2. Follow the existing patterns: no global state beyond `program *tea.Program`, all UI in `main.go`.
3. Test with `simulator.py` and the `--demo` flag.
4. Open a Pull Request.

---

## Build Tooling

- Go 1.21+ (uses `min`/`max` builtins; tested on 1.26.1)
- Bubble Tea v1.3.10
- Lip Gloss v1.1.0
- hpcloud/tail v1.0.0
