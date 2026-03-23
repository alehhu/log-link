# 🛠 LogLink Scripts

This directory contains scripts for automating LogLink setup, installation, and uninstallation.

## Setup Scripts

### 1. `install.sh`
Automates the installation of LogLink. It builds the project and moves the binary to `~/.local/bin`.

**Usage:**
```bash
sh scripts/install.sh
```

### 2. `uninstall.sh`
Removes the LogLink binary from the installation directory.

**Usage:**
```bash
sh scripts/uninstall.sh
```

---
**Note:** For testing, simulation, and log generation scripts, please see the [Test Tools](../test/README.md) directory.
