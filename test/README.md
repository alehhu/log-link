# 🧪 LogLink Test & Simulation Tools

This directory contains scripts for generating synthetic logs and metrics to test LogLink's features.

## Tools

### 1. `simulator.py`
The primary simulation engine for LogLink. It generates correlated logs across multiple services and starts a local metric server.
- **Used by:** The `--demo` flag in `loglink`.
- **Logs:** `api.log`, `db.log`, `worker.log`.
- **Features:** Realistic HTTP methods, status codes, user IDs, and IPs.
- **Incidents:** Randomly triggers connection pool errors and middleware panics.
- **Metrics:** `http://localhost:8080/load`.

**Manual Usage:**
```bash
python3 test/simulator.py
```

### 2. `gen_logs.py`
A lightweight log generator (no metrics) for testing basic aggregation and entity linking.

**Usage:**
```bash
python3 test/gen_logs.py
```

## How to test LogLink
1. **Start the simulator:** `python3 test/simulator.py`
2. **Launch LogLink:** `go run cmd/loglink/main.go --demo`
3. **Observe:** You should see logs from both `app.log` and `db.log` appearing in the TUI, with shared UUIDs highlighted.
