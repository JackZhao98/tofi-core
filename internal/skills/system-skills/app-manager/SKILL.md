---
name: app-manager
description: Guide for creating, managing, and configuring Tofi Apps. Decomposes natural language descriptions into complete app definitions with identity, soul, skills, schedule, and notify targets.
version: "3.0"
---

# App Manager

Guide the AI through creating and managing Tofi Apps using built-in `tofi_*` tools.

## Available Tools

| Tool | Purpose |
|------|---------|
| `tofi_list_apps` | List all apps with status |
| `tofi_create_app` | Create a new app |
| `tofi_update_app` | Update app configuration |
| `tofi_delete_app` | Delete an app |
| `tofi_run_app` | Trigger manual run |
| `tofi_list_app_runs` | View run history |
| `tofi_activate_app` | Enable/disable schedule |
| `tofi_list_notify_targets` | List available receivers |
| `tofi_set_notify_targets` | Set push notification targets |

---

## Creating Apps from Natural Language

When the user describes what they want, decompose into a complete app definition.

### Step 1: Understand the Request

Ask clarifying questions ONLY if too vague to determine:
1. **What** the app does (core purpose)
2. **When** it runs (schedule or manual)
3. **Who** gets notified (push targets)

If clear enough, skip to decomposition.

### Step 2: Decompose into Components

#### A. Identity
- `name`: kebab-case name (e.g., `daily-weather-report`)
- `description`: One-line summary (< 80 chars)

#### B. Prompt (the core instruction)
Write a clear, actionable prompt that tells the AI exactly what to do each run.
Include:
- Step-by-step operational instructions
- Output format expectations
- Error handling behavior
- Tone and language preferences

#### C. Model Selection
| Task Complexity | Recommended Models |
|----------------|-------------------|
| Simple fetching, formatting | `gpt-4o-mini`, `deepseek-chat`, `gemini-2.0-flash` |
| Analysis, writing, reasoning | `gpt-4o`, `claude-sonnet-4`, `gemini-2.5-flash` |
| Deep reasoning, research | `claude-opus-4`, `gpt-5`, `gemini-2.5-pro` |

Always prefer the user's enabled models. Default to cost-effective for simple tasks.

#### D. Skills
Search for relevant skills using `tofi_search` if the task needs specialized tools (web search, data export, etc.).

#### E. Schedule
If the user mentions timing, format as JSON:
```json
[{"time":"09:00","repeat":{"type":"daily"},"enabled":true}]
```

Repeat types: `daily`, `weekdays`, `weekly` (+ `day_of_week`), `monthly` (+ `day_of_month`).

If no timing mentioned, leave empty (manual trigger only).

#### F. Notify Targets
After creating the app, ask if the user wants push notifications.
- Use `tofi_list_notify_targets` to show available receivers
- Use `tofi_set_notify_targets` to configure who gets notified

### Step 3: Confirm Before Creating

Present a summary in the user's language:
```
App 名称: daily-weather-report
描述: 每天早上查询天气并推送
Prompt: [前 50 字...]
模型: gpt-4o-mini
调度: 每天 08:00
通知: Jack (Telegram)
```

Wait for user confirmation.

### Step 4: Execute

1. Call `tofi_create_app` with all fields
2. If schedule provided, call `tofi_activate_app`
3. If notify targets requested, call `tofi_set_notify_targets`
4. Optionally run once with `tofi_run_app` to test

---

## Workflow (all operations)

1. **Understand** — ask only if truly unclear
2. **Plan** — describe what you will do
3. **Confirm** — wait for user approval
4. **Execute** — use `tofi_*` tools
5. **Verify** — call `tofi_list_apps` or `tofi_list_app_runs` to confirm

**Important**: For destructive actions (delete, deactivate), always double-check with the user.
