# Incident Mode

Incident mode provides an automated clustering and ranking system for recurring failures across multiple log streams.

## How It Works

### 1. Detection
When `--incident-mode` is enabled, LogLink scans every incoming log line for "error-like" keywords (case-insensitive):
- `error`
- `panic`
- `fatal`
- `timeout`

### 2. Normalization (Signature Generation)
Once an error-like line is detected, LogLink generates a "signature" for it. This signature is used to cluster similar errors together.

**The Normalization Process:**
-   **ID Masking:** Any string that looks like an ID (UUID, request-ID, IP address) is replaced with a generic `<id>` placeholder.
-   **Normalization:** The entire line is converted to lowercase.
-   **Truncation:** The signature is truncated to 120 characters to avoid excessive memory usage.

**Example:**
-   *Raw:* `2026-03-23T10:00:01 ERROR: Failed to process request 550e8400-e29b-41d4-a716-446655440000 after 1500ms`
-   *Signature:* `error: failed to process request <id> after 1500ms`

### 3. Ranking
LogLink maintains a map of these signatures. Each signature tracks:
-   **Count:** The total number of times it has occurred.
-   **Last Seen:** The timestamp of its most recent occurrence.
-   **Sources:** A breakdown of which log sources reported this error.

## The Details Sidebar
When you open the details sidebar (press `d`), the **INCIDENTS (top)** section displays the top 5 most frequent error signatures. This helps you quickly identify:
-   Which errors are the most "noisy" or frequent.
-   How many different services (sources) are reporting a specific error.

## Exporting Incidents
On exit, if you've provided the `--export <path>` flag, all detected incidents are exported into a JSON or TXT file. This is ideal for postmortem reports or sharing a snapshot of an outage with colleagues.

```bash
loglink ... --incident-mode --export run-summary.json
```
The export includes:
-   Total entries processed.
-   List of all sources.
-   Detailed list of all incidents with their counts and sources.
