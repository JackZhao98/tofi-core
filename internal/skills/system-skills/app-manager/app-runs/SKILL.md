---
name: app-runs
description: View run history for a Tofi App — status, timestamps, trigger type
version: "4.0"
---

# App Run History

## Workflow

1. Use `tofi_list_apps` to find the app ID (if needed)
2. Call `tofi_list_app_runs` with the app ID
   - Default: 5 most recent runs (max: 20)
   - Pass `limit` to control how many

Each run shows: status, creation time, trigger type (scheduled/manual), completion time, and associated session ID.
