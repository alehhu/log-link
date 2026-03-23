# Integrations

LogLink is designed to be easily extensible. Instead of a complex plugin system, it leverages the shell's power to run external commands.

## Built-in Integration Shortcuts

These flags are simply convenient aliases for common shell commands.

### 🐳 Docker
Flag: `--docker <container_name>`

This runs:
```bash
docker logs -f <container_name>
```

### ⛵ Kubernetes
Flag: `--kube-selector <label_selector>`

This runs:
```bash
kubectl logs -f -l "<label_selector>" --all-containers=true --prefix=true
```
It's specifically designed to aggregate logs from all pods matching the selector, showing each source with a clear prefix.

### 📜 systemd / journald
Flag: `--journal-unit <unit_name>`

This runs:
```bash
journalctl -f -u <unit_name> -o short-iso
```
The `short-iso` format ensures consistent timestamp parsing.

### 🐙 GitHub Actions
Flag: `--gha-run <run_id>`

This runs:
```bash
gh run view <run_id> --log
```
Useful for local triage of CI failures.

## 📈 Custom Pulse Metrics

The `--pulse` flag allows you to run any shell command that returns a numeric value once per second. This numeric value is then plotted as a sparkline overlaying your logs.

### Examples:

**CPU Usage (macOS):**
```bash
loglink ... --pulse "top -l 1 | grep 'CPU usage' | awk '{print \$3}' | sed 's/%//'"
```

**Memory Usage (Linux):**
```bash
loglink ... --pulse "free -m | awk 'NR==2{print \$3}'"
```

**Custom API Metric:**
```bash
loglink ... --pulse "curl -fsS http://localhost:8080/metrics/active-requests"
```

## Adding Custom Integrations

Since LogLink can take any command as a positional argument, you can build your own custom integrations directly in the shell:

```bash
loglink "ssh prod-1 'tail -f /var/log/app.log'" "ssh prod-2 'tail -f /var/log/app.log'"
```
The command output will be streamed into the TUI just like a local file.
