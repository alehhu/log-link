# 🚀 LogLink

**Stop `grep`-ing for UUIDs. Start seeing the full picture.**

LogLink is a high-performance, terminal-native log aggregator for the distributed systems era. It correlates logs across services, parses structured JSON output, syncs with live metrics, clusters failure patterns, and remembers your debugging sessions.

> "LogLink is what happens when you give `tail -f` a brain and a heartbeat."

---

## ⚡️ 30-second Quickstart

```bash
# Install and run the interactive demo (Linux/macOS)
curl -fsSL https://raw.githubusercontent.com/alehhu/log-link/master/scripts/install.sh | sh
loglink --demo --incident-mode
```
*Launches a simulated environment with API, DB, and Worker logs, overlays live metrics, and clusters recurring errors.*

---

## 🧐 Why LogLink?

Distributed debugging usually means 5+ terminal tabs, manual UUID-grepping, and guessing whether a CPU spike caused the timeout. **LogLink solves this.**

### 🔗 Causal Correlation
LogLink automatically detects UUIDs, Request IDs, trace IDs, and IPs — including those embedded inside JSON log fields. Press `Enter` on any log line to **highlight the entire request flow** across every source simultaneously. Press `s` to filter to only those lines.

### 📈 Pulse Metrics (The Heartbeat)
Overlay any numeric metric as a **3-row live bar chart** above your logs. **Temporal Scrubbing** lets you drag the cursor along the metric timeline and see exactly which log lines were firing at that moment. Press `P` to expand the chart to fullscreen with Y-axis labels, gridlines, and timestamps.

### 🔍 Structured Log Parsing
LogLink automatically detects JSON log lines (from zap, logrus, slog, structlog, etc.) and renders them in a clean, human-readable format — `msg` field prominent, `level` color-coded, extra fields shown as `key=val`. Trace IDs and request IDs are extracted for causal correlation automatically.

### 🚨 Automatic Incident Clustering
LogLink clusters similar error signatures into "Incidents" and ranks them by frequency. The leaderboard shows the most noisy errors at a glance. With named sessions, **incident history accumulates across sessions** — so you can see if a failure has been recurring for days before you started investigating.

### 💾 Named Sessions
Your sources, pulse command, and incident history are saved per session. Type `loglink` with no arguments to resume exactly where you left off. Add `--session prod` to switch contexts.

---

## 🛠 Features

- **🔌 Native Integrations:** One-command streaming for **Docker, Kubernetes, systemd, and GitHub Actions.**
- **🏷 Named Sources:** Label any source for a readable source column: `api="kubectl logs -f deploy/api"`.
- **🔍 Text Search:** Press `/` to search across all log lines in real time.
- **📋 JSON Parsing:** Structured logs (zap, logrus, slog) are unpacked automatically.
- **💾 Sessions:** Named, persistent sessions with source memory, pulse config, and cross-session incident history.
- **⌨️ Keyboard-First:** Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea). Fast navigation, vi-style keys.
- **📦 Session Exports:** Export incidents to JSON or TXT for postmortems with `--export`.
- **🚀 Zero-Dependency Binary:** Single Go binary. No JVM, no config file, no daemon.

---

## 🔬 Deep Dive: Simulation Suite

```bash
python3 test/simulator.py
# In another terminal:
loglink api=api.log db=db.log worker=worker.log \
  --pulse "curl -fsS http://localhost:8080/load" \
  --incident-mode
```

---

## ⌨️ Essential Keybindings

| Key | Action |
| --- | --- |
| `f` | **Toggle Follow** (auto-scroll to latest) |
| `gg` / `G` | Jump to **Top** / Jump to **Latest** |
| `u` / `d` | **Page Up** / **Page Down** |
| `Enter` | **Highlight** ID under cursor across all sources |
| `s` | **Focus** — filter to only lines matching the ID |
| `/` | **Search** — real-time text search across all lines |
| `n` / `N` | Next / previous search match |
| `m` | **Bookmark** current line |
| `[` / `]` | Jump to previous / next bookmark |
| `Tab` | **Pulse Focus** — enter temporal scrubbing mode |
| `h` / `l` | Move pulse cursor back / forward through time |
| `=` / `-` | **Zoom out / in** the pulse time window |
| `P` | **Pulse fullscreen** — expand chart with axes |
| `a` | **Add source** at runtime (saved to session) |
| `d` | Toggle **Details Modal** |
| `v` | Open `file:line` reference in `$EDITOR` |
| `?` | Show full interactive help |
| `Esc` | Clear all active filters / modes |

---

## 📦 Installation & Setup

### Quick Install (macOS/Linux)
```bash
curl -fsSL https://raw.githubusercontent.com/alehhu/log-link/master/scripts/install.sh | sh
```

### From Source
```bash
git clone https://github.com/alehhu/log-link
cd log-link
go build -o loglink ./cmd/loglink
```

---

## 🗑 Uninstall
```bash
curl -fsSL https://raw.githubusercontent.com/alehhu/log-link/master/scripts/uninstall.sh | sh
```

---

## 📚 Documentation
- [**Development & Contributing**](docs/development.md)
- [**Integration Shortcuts** (K8s, Docker, SSH, etc.)](docs/integrations.md)
- [**Incident Mode & Session History**](docs/incidents.md)
- [**Architecture**](docs/architecture.md)

---

## 📜 License
GNU GPL v3.0 © 2026 Alessandro Hu. Built with ❤️ and [Bubble Tea](https://github.com/charmbracelet/bubbletea).
