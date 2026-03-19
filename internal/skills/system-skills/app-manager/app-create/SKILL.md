---
name: app-create
description: Create a new Tofi App from natural language — decompose user intent into app definition with prompt, model, schedule, skills, and notifications
version: "4.0"
---

# Create App

When the user describes an automation they want, decompose it into a complete app definition.

## Step 1: Understand

Ask clarifying questions ONLY if too vague. Determine:
1. **What** — core purpose
2. **When** — schedule or manual-only
3. **Who** — notification targets (optional)

## Step 2: Decompose

- **name**: kebab-case (e.g., `daily-weather-report`)
- **description**: one-line summary (< 80 chars)
- **prompt**: clear, actionable instruction for the AI — include steps, output format, tone, error handling
- **model**: match complexity to model tier:
  - Simple tasks: `gpt-4o-mini`, `deepseek-chat`, `gemini-2.0-flash`
  - Analysis/writing: `gpt-4o`, `claude-sonnet-4`, `gemini-2.5-flash`
  - Deep reasoning: `claude-opus-4`, `gpt-5`, `gemini-2.5-pro`
- **skills**: search for relevant skills if the task needs tools (web search, etc.)
- **schedule** (if timed): JSON array, e.g. `[{"time":"09:00","repeat":{"type":"daily"}}]`
  - Repeat types: `daily`, `weekdays`, `weekly` (+ `day_of_week`), `monthly` (+ `day_of_month`)

## Step 3: Confirm

Present a summary in the user's language before creating:
```
名称: daily-weather-report
描述: 每天早上查询天气并推送
Prompt: [前 50 字...]
模型: gpt-4o-mini
调度: 每天 08:00
```

## Step 4: Execute

1. `tofi_create_app` with all fields
2. If schedule provided → `tofi_activate_app` with `active: true`
3. If user wants notifications → `tofi_list_notify_targets` (no app_id) to show receivers, then `tofi_set_notify_targets`
4. Optionally `tofi_run_app` to test
