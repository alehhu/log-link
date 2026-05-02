# Incident Mode & Session History

## Incident Mode

Enable with `--incident-mode`. LogLink scans every incoming log line for error signals and clusters similar failures together so you see patterns instead of noise.

### Detection

LogLink detects errors via two paths:

1. **Structured logs (JSON):** The `level` field is read directly. Lines with `level=error`, `level=fatal`, `level=panic`, or `level=critical` are always detected, regardless of message content.
2. **Plain-text logs:** A keyword scan checks for `error`, `panic`, `fatal`, and `timeout` (case-insensitive).

### Normalization (Signature Generation)

Detected error lines are normalized into a **signature** used to cluster similar errors together:

- All IDs (UUIDs, request IDs, IPs, hex strings) are replaced with `<id>`
- The line is lowercased
- The result is truncated to 120 characters

**Example:**
```
# Raw line:
{"level":"error","msg":"connection pool exhausted","request_id":"abc-123","attempt":3}

# After JSON parsing:
connection pool exhausted  err=... request_id=abc-123  attempt=3

# Signature:
connection pool exhausted  err=... request_id=<id>  attempt=<id>
```

### Ranking

Each signature tracks:
- **Count:** total occurrences in the current session
- **FirstSeen / LastSeen:** timestamps for the current session
- **Sources:** which log sources reported this error

The **Incident Leaderboard** at the bottom of the screen shows the top 3 most frequent signatures at all times.

---

## Cross-Session Incident History

When you quit LogLink, the current session's incidents are **merged into the session's persistent history** stored in `~/.config/loglink/sessions.json`. Each signature accumulates:

- `total_count` — total hits across all sessions
- `session_count` — how many sessions this signature appeared in
- `first_seen` — when it was first ever detected
- `last_seen` — most recent occurrence

This turns LogLink into a lightweight recurring-failure tracker. When you open the **Details Modal** (`d`) while incident mode is active, the **INCIDENT HISTORY** section shows the top 5 all-time failures for the current session, with cross-session counts and recency.

**Example modal output:**
```
INCIDENT HISTORY (all sessions):
▸  47× across 12 sessions  last: 2h ago
    connection pool exhausted  request_id=<id>

▸  23× across 5 sessions  last: 3d ago
    upstream timeout  service=<id>
```

---

## Exporting Incidents

On exit, if you provided `--export <path>`, all detected incidents from the current session are written to a JSON or TXT file:

```bash
loglink --incident-mode --export incident-2026-05-01.json
```

The export includes:
- Total entries processed
- List of all sources
- All incident signatures with counts, sources, and timestamps

Useful for postmortem reports or sharing a session snapshot with colleagues.

---

## Sessions

Named sessions persist sources, incident history, pulse configuration, and description across restarts.

```bash
# First run — sources and config are saved to "prod" on quit
loglink --session prod --desc "payments k8s" \
  api="kubectl logs -f deploy/api" \
  --incident-mode \
  --pulse "kubectl top pod -l app=api --no-headers | awk '{print \$3}' | tr -d 'm'"

# Resume exactly where you left off
loglink --session prod
```

Session files are stored at `~/.config/loglink/sessions.json`. Use `--session <name>` to switch between contexts (e.g., `prod`, `staging`, `local-dev`). The default session name is `default`.

### Flags

| Flag | Description |
|---|---|
| `--session <name>` | Load/save a named session (default: `"default"`) |
| `--desc <text>` | Set a description for the session (shown in header) |

### Adding sources at runtime

Press `a` inside the app to open a source prompt. Type a labeled source (`worker="kubectl logs -f deploy/worker"`) or a plain path. The new source starts streaming immediately and is saved to the session on quit.
