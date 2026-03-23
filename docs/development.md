# Development Guide

LogLink is built with Go and the [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI library.

## Getting Started

1.  **Clone the Repo:**
    ```bash
    git clone https://github.com/alehhu/log-link
    cd log-link
    ```

2.  **Install Dependencies:**
    ```bash
    go mod download
    ```

3.  **Run Locally:**
    ```bash
    go run . --demo
    ```

## Local Testing & Simulation

The project includes two scripts in the `test/` directory to help with development and testing:

### 1. `test/simulator.py`
This script simulates a distributed system environment.
-   **Logs:** Generates `app.log` and `db.log` with correlated UUIDs.
-   **Metrics:** Runs a local HTTP server at `http://localhost:8080/load` providing a numeric load metric.

**Usage:**
```bash
python3 test/simulator.py
```
Then, in another terminal:
```bash
loglink --demo
```

### 2. `test/gen_logs.py`
A simpler script that only generates correlated log files (`app.log`, `db.log`) without the HTTP metric server.

**Usage:**
```bash
python3 test/gen_logs.py
```

For more details, see the [Test README](../test/README.md).

## Modifying the TUI

LogLink's TUI is entirely contained within `cmd/loglink/main.go`.

-   **`model` struct:** The central state container.
-   **`Update` function:** Where keyboard shortcuts and incoming log messages are handled.
-   **`View` function:** Where the UI layout and styling (Lip Gloss) are defined.

## Contributing

1.  Create a feature branch.
2.  Ensure your changes are consistent with the existing coding style.
3.  Test your changes using the `simulator.py` script.
4.  Open a Pull Request.

---

**Build Tooling:**
-   Go 1.26.1+
-   Bubble Tea (v1.3.10)
-   Lip Gloss (v1.1.0)
