# 🚀 LogLink

**Stop `grep`-ing for UUIDs. Start seeing the full picture.**

LogLink is a high-performance, terminal-native log aggregator designed for the distributed systems era. It doesn't just collect logs; it **correlates** them across services, **syncs** them with system metrics, and **clusters** failure patterns automatically.

> "LogLink is what happens when you give `tail -f` a brain and a heartbeat."

---

## ⚡️ The 30-Second "Magic Moment"

The best way to understand LogLink is to see it in action. No configuration required:

```bash
# Install and run the interactive demo in one go
curl -fsSL https://raw.githubusercontent.com/alehhu/log-link/main/scripts/install.sh | sh
loglink --demo --incident-mode
```
*This launches a simulated environment with API, DB, and Worker logs, overlays live CPU/Memory metrics, and clusters recurring errors.*

---

## 🧐 Why LogLink?

Distributed debugging usually means having 5 terminal tabs open, manually matching UUIDs, and guessing if a CPU spike caused that timeout. **LogLink solves this.**

### 🔗 Causal Correlation
LogLink automatically detects UUIDs, Request IDs, and IPs. Select one, and it **highlights the entire flow** across every log source simultaneously. Filter the noise with one keypress (`s`).

### 📈 Pulse Metrics (The Heartbeat)
Overlay live system metrics (CPU, Mem, or custom API stats) as a sparkline. **Temporal Scrubbing** allows you to move the cursor through the logs and see exactly what the metrics were at that millisecond.

### 🚨 Automatic Incident Clustering
Stop chasing individual error lines. LogLink clusters similar failure signatures into "Incidents," allowing you to see the **top recurring issues** at a glance in the sidebar.

---

## 🛠 Features

- **🔌 Native Integrations:** One-command streaming for **Docker, Kubernetes, systemd, and GitHub Actions.**
- **⌨️ Keyboard-First:** Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea). Blistering fast navigation and filtering.
- **📦 Session Exports:** Wrap up a debugging session by exporting a JSON or TXT summary of all detected incidents—perfect for postmortems.
- **🚀 Zero-Dependency Binary:** Single Go binary. No heavy JVM, no complex config.

---

## 🔬 Deep Dive: Simulation Suite

LogLink includes a full simulation environment in the `test/` directory to help you explore its power without touching your production logs.

- **Correlated Flows:** See how `request_id` flows through `api.log` → `db.log` → `worker.log`.
- **Failure Modes:** Simulated "connection pool exhausted" and "middleware panics" to test clustering.
- **Metrics:** A noisy sine-wave metric server to practice temporal scrubbing.

```bash
python3 test/simulator.py
# Then in another terminal:
loglink api.log db.log worker.log --pulse "curl -fsS http://localhost:8080/load" --incident-mode
```

---

## ⌨️ Essential Keybindings

| Key | Action |
| --- | --- |
| `f` | **Toggle Follow** (auto-scroll) |
| `Enter` | **Highlight** ID under cursor across all files |
| `s` | **Focus** (filter) only on the ID under cursor |
| `Tab` | **Pulse Focus** (start scrubbing through time) |
| `h` / `l` | Move pulse cursor (and log view) back/forward |
| `d` | Toggle **Details Sidebar** (Incidents & Raw JSON) |
| `v` | Open `file:line` in your **$EDITOR** |
| `?` | Show full interactive help |

---

## 📦 Installation & Setup

### Quick Install (macOS/Linux)
```bash
curl -fsSL https://raw.githubusercontent.com/alehhu/log-link/main/scripts/install.sh | sh
```

### From Source
```bash
git clone https://github.com/alehhu/log-link
cd log-link
go build -o loglink ./cmd/loglink
```

---

## 📚 Documentation
- [**Development & Contributing**](docs/development.md)
- [**Integration Shortcuts** (K8s, Docker, etc.)](docs/integrations.md)
- [**Incident Mode Internals**](docs/incidents.md)
- [**Architecture**](docs/architecture.md)

---

## 📜 License
GNU GPL v3.0 © 2026 Alessandro Hu. Built with ❤️ and [Bubble Tea](https://github.com/charmbracelet/bubbletea).
