---
name: app-edit
description: Edit an existing Tofi App — update prompt, model, skills, schedule, or notification targets
version: "4.0"
---

# Edit App

Modify an existing app's configuration. Only changed fields are updated.

## Workflow

1. Use `tofi_list_apps` to find the app ID (if user refers by name)
2. Confirm what the user wants to change
3. Call `tofi_update_app` with only the fields to change:
   - `name`, `description`, `prompt`, `model`, `skills`, `schedule`
4. If schedule was changed and app should be active → `tofi_activate_app`
5. If notification targets need updating:
   - `tofi_list_notify_targets` (no app_id) to show available receivers
   - `tofi_set_notify_targets` to configure

**Important**: `tofi_update_app` is a partial update — omitted fields stay unchanged. Only include fields the user wants to modify.
