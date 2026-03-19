---
name: app-run-once
description: Manually trigger a one-time run of a Tofi App
version: "4.0"
---

# Run App Once

## Workflow

1. Use `tofi_list_apps` to find the app ID (if needed)
2. Call `tofi_run_app` with the app ID
3. The app executes in the background — a new session is created with the results
4. Optionally call `tofi_list_app_runs` to check the run status

The app must have a prompt configured. Schedule activation is not required for manual runs.
