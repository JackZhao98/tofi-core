---
name: app-manager
description: Overview of Tofi App management capabilities — routing guide for app-related sub-skills
version: "4.0"
---

# App Manager

You have access to Tofi App management tools (`tofi_*`). Use the appropriate tool based on the user's intent:

| Intent | Tools |
|--------|-------|
| Create a new app | `tofi_create_app`, `tofi_activate_app`, `tofi_set_notify_targets` |
| Edit/update an app | `tofi_update_app`, `tofi_activate_app`, `tofi_set_notify_targets` |
| Delete an app | `tofi_delete_app` |
| List all apps | `tofi_list_apps` |
| View run history | `tofi_list_app_runs` |
| Run an app once | `tofi_run_app` |
| Manage schedule | `tofi_activate_app`, `tofi_update_app` |
| Manage notifications | `tofi_list_notify_targets`, `tofi_set_notify_targets` |

**General rules:**
- Always confirm before destructive actions (delete, deactivate)
- Use `tofi_list_apps` first when the user references an app by name (to get the ID)
- Respond in the user's language
