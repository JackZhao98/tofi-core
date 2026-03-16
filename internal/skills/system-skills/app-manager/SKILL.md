---
name: app-manager
description: Create, update, delete, and manage Tofi Apps via API. When creating apps, decomposes natural language descriptions into complete app definitions with soul, identity, skills, schedule, and capabilities.
version: "2.0"
required_secrets: ["TOFI_TOKEN"]
---

# App Manager Toolkit

Manage Tofi Apps directly via API scripts. **Always use these scripts** to create, update, or delete apps — never propose changes that require frontend execution.

Environment variables `TOFI_API_URL` and `TOFI_TOKEN` are automatically injected.

## Tools

### `manage.py list` — List all apps
```bash
python3 skills/app-manager/scripts/manage.py list
```
Returns JSON array of all apps with id, name, description, is_active, schedule, model.

### `manage.py get <app_id>` — Get app details
```bash
python3 skills/app-manager/scripts/manage.py get <app_id>
```
Returns full JSON of a single app.

### `manage.py create` — Create a new app
```bash
python3 skills/app-manager/scripts/manage.py create --name "App Name" --prompt "What the app does..." [options]
```
Options:
- `--name NAME` (required): App display name
- `--prompt PROMPT` (required): What the app should do each run
- `--description DESC`: Brief description
- `--model MODEL`: Model ID (e.g., claude-sonnet-4-20250514, gpt-4o)
- `--skills SKILL1,SKILL2`: Comma-separated skill IDs
- `--schedule JSON`: Schedule rules as JSON string
- `--system-prompt TEXT`: Custom system prompt that defines the app's personality and behavior
- `--capabilities JSON`: Capabilities config as JSON (e.g., `'{"web_search":{"enabled":true}}'`)

Returns the created app as JSON.

### `manage.py update <app_id>` — Update an existing app
```bash
python3 skills/app-manager/scripts/manage.py update <app_id> [options]
```
Same options as `create`. Only specified fields are updated.

### `manage.py delete <app_id>` — Delete an app
```bash
python3 skills/app-manager/scripts/manage.py delete <app_id>
```

### `manage.py activate <app_id>` — Enable scheduling
```bash
python3 skills/app-manager/scripts/manage.py activate <app_id>
```

### `manage.py deactivate <app_id>` — Disable scheduling
```bash
python3 skills/app-manager/scripts/manage.py deactivate <app_id>
```

### `manage.py run <app_id>` — Run app immediately
```bash
python3 skills/app-manager/scripts/manage.py run <app_id>
```

---

## Creating Apps from Natural Language

When the user describes what they want an app to do in natural language, decompose their description into a complete app definition before calling `create`.

### Step 1: Understand the Request

Ask clarifying questions ONLY if the description is too vague to determine:
1. **What** the app does (core purpose)
2. **How** it behaves (personality/tone)

If clear enough, skip straight to decomposition.

### Step 2: Decompose into Components

#### A. Identity
- `name`: Display name for the app
- `description`: One-line summary (< 80 chars)

#### B. Soul (becomes the `--system-prompt`)
- `role`: Core role definition (1-2 sentences)
- `personality`: Communication style and tone
- `principles`: 3-5 behavioral rules
- `boundaries`: What the app refuses to do

#### C. Capabilities
- `skills`: Skills from the Tofi registry the app needs
- `capabilities`: Built-in Tofi capabilities:
  - `web_search` — search the web
  - `web_fetch` — fetch and read web pages
  - `file_read` — read workspace files
  - `file_write` — write workspace files
- `model`: Pick from the user's enabled models. Default to cost-effective for simple tasks.

#### D. Operations
- `schedule`: When the app runs (see Schedule Format below)
- If the user doesn't mention scheduling, default to manual trigger (no schedule).

### Step 3: Build the System Prompt

Compose a `--system-prompt` that encodes the Soul:

```
You are {role}.

## Personality
{personality description}

## Principles
- {principle 1}
- {principle 2}
- {principle 3}

## Boundaries
- {boundary 1}
- {boundary 2}

## Instructions
{Step-by-step operational instructions}
```

### Step 4: Create via API

Call `manage.py create` with all the decomposed fields.

### Step 5: Review with User

Present a summary and confirm. Adjust with `manage.py update` if needed.

---

## Model Selection Guide

| Task Complexity | Recommended Models |
|----------------|-------------------|
| Simple fetching, formatting, summarizing | `gpt-4o-mini`, `deepseek-chat`, `gemini-2.0-flash` |
| Analysis, writing, multi-step reasoning | `gpt-4o`, `claude-sonnet-4`, `gemini-2.5-flash` |
| Deep reasoning, research, architecture | `claude-opus-4`, `gpt-5`, `gemini-2.5-pro` |

Always prefer the user's enabled models.

## Schedule Format

Tofi uses structured JSON for schedules:

```json
{
  "entries": [
    {"time": "09:00", "repeat": {"type": "daily"}, "enabled": true}
  ],
  "timezone": "Asia/Shanghai"
}
```

Repeat types: `daily`, `weekdays`, `weekly` (with `day_of_week`), `monthly` (with `day_of_month`), `custom` (with cron expression).

### Examples

| Need | Schedule JSON |
|------|--------------|
| Every morning at 8am | `{"entries":[{"time":"08:00","repeat":{"type":"daily"},"enabled":true}],"timezone":"Asia/Shanghai"}` |
| Weekdays 9am and 5pm | `{"entries":[{"time":"09:00","repeat":{"type":"weekdays"},"enabled":true},{"time":"17:00","repeat":{"type":"weekdays"},"enabled":true}],"timezone":"Asia/Shanghai"}` |
| Every Monday at 10am | `{"entries":[{"time":"10:00","repeat":{"type":"weekly","day_of_week":1},"enabled":true}],"timezone":"Asia/Shanghai"}` |

---

## Workflow (for all operations)

1. **Understand** the user's request — ask clarifying questions if needed
2. **Research** if needed (web search, skill search)
3. **Describe** your plan to the user in text, listing what you will create/change
4. **Wait for confirmation** — only proceed after the user says yes / approves
5. **Execute** using the scripts above
6. **Verify** by running `list` or `get` to confirm the changes

**Important**: Always describe what you plan to do BEFORE executing. For destructive actions (delete, deactivate), double-check with the user.
