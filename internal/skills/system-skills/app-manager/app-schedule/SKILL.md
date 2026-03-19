---
name: app-schedule
description: View and manage Tofi App schedules — enable, disable, or modify run timing
version: "4.0"
---

# App Schedule

## View Schedule

Call `tofi_list_apps` — active apps show their next run time.

## Modify Schedule

Use `tofi_update_app` with the `schedule` field to change timing:

```json
[{"time":"09:00","repeat":{"type":"daily"},"enabled":true}]
```

Repeat types:
- `daily` — every day
- `weekdays` — Monday to Friday
- `weekly` + `day_of_week` (0=Sun, 1=Mon, ..., 6=Sat)
- `monthly` + `day_of_month` (1-31)

Multiple rules are supported in the array.

## Enable / Disable

- `tofi_activate_app` with `active: true` — start scheduled runs
- `tofi_activate_app` with `active: false` — stop and cancel pending runs

An app must have schedule rules configured before it can be activated.
