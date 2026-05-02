# Integrations

LogLink is designed to be easily extensible. Instead of a complex plugin system, it leverages the shell's power to run arbitrary commands as log sources.

---

## Labeling Sources

Any source — file or command — can be given a human-readable label using the `label=source` syntax. The label appears in the source column instead of a truncated path or command string.

```bash
# Files
loglink api=/var/log/api.log db=/var/log/db.log

# Commands
loglink api="kubectl logs -f deploy/api" db="kubectl logs -f deploy/postgres"

# Mix
loglink api=/var/log/api.log worker="kubectl logs -f deploy/worker"
```

Labels must start with a letter and contain only `[a-zA-Z0-9_-]`. The `=` splits on the first occurrence, so `kubectl` selectors like `app=api` inside the command string are preserved correctly.

---

## Built-in Integration Shortcuts

These flags are convenient aliases for common shell commands. They also set a sensible default label automatically.

### 🐳 Docker
```bash
loglink --docker <container_name>
# Equivalent to: docker logs -f <container_name>
# Label: container name
```

### ⛵ Kubernetes
```bash
loglink --kube-selector <label_selector>
# Equivalent to: kubectl logs -f -l "<selector>" --all-containers=true --prefix=true
```
Aggregates logs from all pods matching the selector. For multi-namespace or multi-context setups, pass the full `kubectl` command directly as a labeled source:
```bash
loglink api="kubectl logs -f -l app=api -n production --context prod-cluster --all-containers"
```

### 📜 systemd / journald
```bash
loglink --journal-unit <unit_name>
# Equivalent to: journalctl -f -u <unit_name> -o short-iso
# Label: unit name
```

### 🐙 GitHub Actions
```bash
loglink --gha-run <run_id>
# Equivalent to: gh run view <run_id> --log
```

---

## Remote Hosts (SSH)

Pass a shell command as a labeled source to tail files on remote machines. The shell handles the SSH transport; LogLink handles the aggregation:

```bash
loglink \
  prod1="ssh prod-1 'tail -f /var/log/api.log'" \
  prod2="ssh prod-2 'tail -f /var/log/api.log'"
```

---

## 📈 Pulse Metrics

The `--pulse <cmd>` flag runs any shell command that prints a single number, once per `--pulse-interval` seconds (default: 2). The value is plotted as a live bar chart above the log view.

```bash
# Kubernetes pod CPU (millicores)
loglink --kube-selector app=api \
  --pulse "kubectl top pod -l app=api --no-headers | awk '{print \$3}' | tr -d 'm'"

# Docker container memory %
loglink --docker web \
  --pulse "docker stats web --no-stream --format '{{.MemPerc}}' | tr -d '%'"

# Custom API metric
loglink ... --pulse "curl -fsS http://localhost:8080/metrics/queue-depth"

# System memory (Linux)
loglink ... --pulse "free -m | awk 'NR==2{print \$3}'"
```

### Pulse flags

| Flag | Default | Description |
|---|---|---|
| `--pulse <cmd>` | — | Shell command returning a numeric value |
| `--pulse-interval <n>` | `2` | Seconds between samples. Lower = more detail, higher = better battery. |

### In-app pulse controls

| Key | Action |
|---|---|
| `Tab` | Enter/exit temporal scrubbing mode |
| `h` / `l` | Move cursor backward / forward along the metric timeline |
| `=` / `-` | Zoom out / in (show more / less history) |
| `P` | Toggle fullscreen pulse chart with Y-axis and timestamps |

---

## Adding Custom Integrations

Any shell command whose stdout is a stream of log lines works as a source:

```bash
# Stream from S3-backed log archive
loglink archive="aws s3 cp s3://my-logs/2026-05-01.log - | tail -f"

# Forward from a remote Kubernetes cluster
loglink remote="kubectl --context staging logs -f deploy/api"
```
