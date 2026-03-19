---
name: app-delete
description: Safely delete a Tofi App with user confirmation
version: "4.0"
---

# Delete App

## Workflow

1. Use `tofi_list_apps` to find the app ID (if user refers by name)
2. **Always confirm** before deleting — show the app name and description
3. Call `tofi_delete_app` with the app ID

Deletion is permanent: removes the app, its files, and cancels all pending runs.
