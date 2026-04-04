# Architecture

LogLink is designed to provide a unified, causal timeline of events from multiple, disparate sources.

## Core Components

### 1. Log Ingestion
LogLink can ingest logs from two types of sources:
-   **Static Files:** Using the `hpcloud/tail` package to tail local files in real-time.
-   **Shell Commands:** Using Go's `os/exec` to run commands (like `docker logs` or `kubectl logs`) and capture their `stdout/stderr`.

### 2. The Aggregation Engine
All log entries from all sources are funneled into a single Go channel. The engine then:
-   **Timestamp Extraction:** Uses regex to find and parse timestamps. If no timestamp is found, it uses the current time.
-   **Entity Extraction:** Automatically scans each line for "entities" like UUIDs, Request IDs (`req-123`), and IPv4 addresses.
-   **Timeline Sorting:** Log entries are inserted into an ordered slice based on their timestamp, ensuring a consistent causal timeline even if sources report events slightly out of sync.

### 3. TUI (Bubble Tea)
The user interface is built with the **Bubble Tea** framework, which uses an Elm-like architecture (Model-Update-View):
-   **Model:** Maintains the list of log entries, the current cursor position, filters, and active highlights.
-   **Update:** Handles keyboard input (scrolling, highlighting, filtering) and incoming log entries from the background goroutines.
-   **View:** Renders the terminal UI using **Lip Gloss** for styling, including the log timeline, the sparkline pulse graph, the incident leaderboard, and the detailed floating modal.

## Data Structures

```go
type LogEntry struct {
	Timestamp time.Time
	Source    string
	Content   string
	Entities  []string
}

type MetricEntry struct {
	Timestamp time.Time
	Value     float64
}
```

## Causal Linking Logic
When a user highlights an entity (like a UUID), LogLink scans the visible log entries for that exact string. If an entry contains the string, it is rendered with a distinct highlight style. This allows a developer to instantly see the path a single request took across multiple services (e.g., from an API container to a database log).
