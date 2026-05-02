# Architecture

LogLink provides a unified, causal timeline of events from multiple disparate sources, with structured log parsing, live metrics, and persistent session state.

## Core Components

### 1. Log Ingestion & Batching

LogLink ingests from two source types:
- **Static files:** `hpcloud/tail` with inotify/kqueue for event-driven file watching
- **Shell commands:** `os/exec` streaming stdout/stderr (Docker, kubectl, SSH, etc.)

Both paths use a **leading-edge + cooldown batching** strategy to cap TUI render rate at ~20 fps regardless of log volume:
- The first line of any burst is sent immediately (0 ms added latency)
- Subsequent lines within a 50 ms cooldown window are accumulated
- When the window closes, accumulated lines are sent as a single `logEntryBatch`
- An idle source consumes zero CPU between events

Scanner buffers are set to 1 MB to handle long JSON log lines without silent drops.

### 2. Structured Log Parsing

Every incoming line passes through `newLogEntry`, which:

1. **Attempts JSON detection:** Scans for `{` anywhere in the line (handles `kubectl --prefix=true` prefixes like `[pod/name] {"level":"info",...}`). If found, attempts `json.Unmarshal`.
2. **Field extraction:** Pulls well-known fields by category:
   - Message: `msg`, `message`, `log`, `body`
   - Level: `level`, `severity`, `lvl` → normalized to canonical lowercase
   - Timestamp: `ts`, `time`, `timestamp`, `@timestamp` (string RFC3339 or Unix float)
   - Error: `error`, `err` → appended as `err=<value>`
   - Trace entities: `trace_id`, `traceID`, `request_id`, `requestId`, `span_id`, etc. → promoted to `LogEntry.Entities` for causal correlation
   - Remaining fields: rendered as sorted `key=val` pairs
3. **Plain-text fallback:** For non-JSON lines, `detectLogLevel` checks common patterns (`[ERROR]`, `level=warn`, etc.) and the result is stored on `LogEntry.Level`. This runs once at parse time, never during rendering.

### 3. The Aggregation Engine

All log entries are funneled into the Bubble Tea program via `p.Send()`. The engine:

- **Timestamp extraction:** Regex parses common formats; falls back to `time.Now()`.
- **Entity extraction:** Regex detects UUIDs, `req-*` patterns, and IPv4 addresses.
- **Timeline insertion:** `insertEntry` uses binary search (`sort.Search`) to find the insertion position in the sorted `m.entries` slice — O(log n) position finding. Entries from multiple sources are merged into one chronological timeline even if they arrive slightly out of order.
- **Incremental filter maintenance:** When a `focusID` is active, `filteredEntries` indices are maintained incrementally inside `insertEntry` (O(filtered) per insertion), not rebuilt from scratch (O(n)) on every batch.

### 4. Session Persistence

Sessions are stored at `~/.config/loglink/sessions.json` as a JSON map of named sessions. Each session stores:

- `sources` — labeled source list (restored on startup)
- `incident_mode` — boolean flag
- `pulse_cmd` — pulse shell command (restored, starts automatically)
- `pulse_interval` — polling interval in seconds
- `description` — human-readable label shown in the header
- `last_used` — timestamp of last session
- `incident_history` — map of error signatures to `{total_count, session_count, first_seen, last_seen}`

On quit, the current session's incidents are merged into `incident_history` (counts accumulated, timestamps updated). On startup, the session is fully restored — sources start streaming, pulse starts ticking, and incident history is loaded for display in the Details Modal.

### 5. TUI (Bubble Tea)

The interface is built with the **Bubble Tea** framework (Elm-like Model-Update-View):

- **Model:** Central state — entries, cursor, filters, metrics, session info, UI modes.
- **Update:** Handles keyboard input and incoming messages (`logEntryBatch`, `MetricEntry`, etc.).
- **View:** Renders the full terminal frame using **Lip Gloss**. At most ~20 fps due to batching.

**Layout (normal mode):**
```
┌─ Compact pulse chart (4 lines: stats + 3 chart rows) ─────────────────────┐
│ Pulse  cur:68.42  min:32.1  max:91.3  avg:54.7 [10m]  =/-:zoom  P:expand  │
│ ▄▄▅▅▆▆▇▇████▇▇▆▆▅▅▄▄▃▃▄▄▅▆▇▇█████████▇▇▆▅▄▃▂▃▄▅▆▇█████                  │
│ ████████████████████████████████████████████████████████████████████       │
│ ████████████████████████████████████████████████████████████████████       │
├─ Log timeline ────────────────────────────────────────────────────────────┤
│  14:32:11  api    upstream timeout  service=payments  latency_ms=1502      │
│  14:32:11  db     connection pool exhausted  err=max connections reached   │
│  ...                                                                        │
├─ Incident leaderboard (--incident-mode) ───────────────────────────────────┤
│ TOP INCIDENTS: 🔥 47× connection pool exhausted | 🔥 12× upstream timeout  │
└─ Footer (keybindings / search bar / add-source prompt) ───────────────────┘
```

**Pulse fullscreen mode** (`P`): replaces the full viewport with a chart, Y-axis labels, gridlines at 33%/67%, and X-axis timestamps.

## Data Structures

```go
type LogEntry struct {
    Timestamp time.Time
    Source    string    // human-readable label
    Content   string    // parsed/rendered content (JSON unpacked or raw text)
    Entities  []string  // UUIDs, IPs, trace IDs — used for causal correlation
    Level     string    // canonical: "error","warn","info","debug","fatal","panic","critical","trace"
}

type MetricEntry struct {
    Timestamp time.Time
    Value     float64
}

type Incident struct {
    Signature string
    Count     int
    FirstSeen time.Time
    LastSeen  time.Time
    Sources   map[string]int
}

type logEntryBatch []LogEntry  // sent as one message to cap render rate at 20 fps
```

## Causal Linking Logic

When a user highlights an entity (UUID, trace ID, etc.), LogLink scans the visible log entries for that exact string. Matching entries are rendered with a distinct highlight style. For JSON-parsed entries, trace entities extracted from `trace_id`, `requestId`, etc. fields are automatically added to `Entities`, making them available for correlation without the user needing to manually select the field.
