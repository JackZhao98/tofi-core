# Tofi App API Reference

Base URL: `http://localhost:8321/api/v1`

All endpoints require authentication via `Authorization: Bearer <token>` or `Authorization: Bearer tofi-sk-<api-key>`.

## App Lifecycle

```
Create App → Configure Schedule → Activate → Runs execute automatically
                                            → Trigger via webhook
                                            → View stats / runs / logs
```

Run status transitions: `pending → running → done | failed`
Manual overrides: `pending → skipped`, `running → failed (abort)`

---

## Endpoints

### Apps CRUD

#### List Apps
```
GET /apps
```

Response: `200 OK`
```json
[
  {
    "id": "daily-weather",
    "name": "Daily Weather",
    "description": "Fetch and summarize weather",
    "prompt": "Get weather for {{location}}...",
    "model": "claude-sonnet-4-20250514",
    "is_active": true,
    "pending_runs": 3,
    "next_run_at": "2026-04-03T09:00:00Z",
    ...
  }
]
```

#### Create App
```
POST /apps
Content-Type: application/json

{
  "id": "daily-weather",
  "name": "Daily Weather",
  "description": "Fetch and summarize weather",
  "prompt": "Get weather for {{location}} and summarize key points.",
  "system_prompt": "You are a weather analyst.",
  "model": "claude-sonnet-4-20250514",
  "skills": ["web-search", "web-fetch"],
  "parameters": {"location": "New York"},
  "schedule_rules": {
    "entries": [{"time": "09:00", "repeat": {"type": "daily"}, "enabled": true}],
    "timezone": "America/New_York"
  },
  "buffer_size": 20,
  "renewal_threshold": 5
}
```

Response: `201 Created` — returns the full `AppRecord`.

#### Get App
```
GET /apps/{id}
```

#### Update App
```
PUT /apps/{id}
Content-Type: application/json

{
  "prompt": "Updated prompt...",
  "model": "gpt-5-mini"
}
```

Partial update — only include fields you want to change.

#### Delete App
```
DELETE /apps/{id}
```

Response: `204 No Content`

---

### Schedule Control

#### Activate
```
POST /apps/{id}/activate
```

Starts the scheduler. App must have `schedule_rules` configured.

#### Deactivate
```
POST /apps/{id}/deactivate
```

Stops the scheduler and cancels all pending runs.

---

### Run Management

> All run dispatch is **asynchronous**: the server immediately returns a `run_id`,
> the actual LLM execution happens in the background. Poll
> `GET /apps/{id}/runs/{run_id}` to read status (`running` → `done` / `failed`)
> and the final `result`.

#### Template Substitution

The app's `prompt` field is a template. Two forms are supported:

- `{{var_name}}` — substitutes the parameter's value
- `{{#bool_var}}...{{/bool_var}}` — keeps the block when the parameter is truthy
  (`true` / `yes` / `1`); otherwise removes it

Value precedence (highest first):

1. `params` from this request body (per-run override)
2. `parameters` saved on the app (default values)
3. `default` from the matching `parameter_defs` entry

If a `parameter_defs` entry is `required: true` and no value is found through
any of the three layers, the request fails with **HTTP 400** and a hint listing
the missing names.

When the request body includes a non-empty `prompt`, template substitution is
**skipped** entirely — the override string is sent to the LLM as-is.

#### Trigger Run (Manual)
```
POST /apps/{id}/run
Content-Type: application/json

{
  "prompt": "Optional full prompt override (skips template)",
  "params": { "ticker": "MSFT" }
}
```

Both fields are optional. `params` is merged on top of the app's saved
parameter values for this single run.

Response: `201 Created`
```json
{
  "id": "uuid...",
  "app_id": "daily-weather",
  "status": "running",
  "trigger": "manual"
}
```

#### Trigger Run (Webhook)
```
POST /apps/{id}/trigger
Content-Type: application/json

{
  "prompt":  "Override prompt (optional, skips template)",
  "params":  { "ticker": "MSFT" },
  "payload": { "user": "alice", "free_text": "..." }
}
```

- `params` is merged into the template substitution (same as `/run`).
- `payload` is appended verbatim to the resolved prompt under a
  `## Webhook Payload` header — useful for free-form context that doesn't fit a
  parameter slot.

Response: `202 Accepted`
```json
{
  "run_id": "uuid...",
  "app_id": "daily-weather",
  "status": "running",
  "trigger": "webhook",
  "message": "App run triggered successfully. Poll GET /api/v1/agents/daily-weather/runs/{run_id} for status."
}
```

#### List Runs
```
GET /apps/{id}/runs?status=done&limit=10
```

Query params:
- `status` — filter by `pending`, `running`, `done`, `failed`, `cancelled`, `skipped`
- `limit` — max results (default 50)

#### Get Run
```
GET /apps/{id}/runs/{runId}
```

Response:
```json
{
  "id": "uuid...",
  "app_id": "daily-weather",
  "status": "done",
  "trigger": "scheduled",
  "scheduled_at": "2026-04-02T09:00:00Z",
  "started_at": "2026-04-02T09:01:23Z",
  "completed_at": "2026-04-02T09:05:30Z",
  "session_id": "s_abc123",
  "result": "Weather summary: Today in New York..."
}
```

#### Get Run Session (Full Chat History)
```
GET /apps/{id}/runs/{runId}/session
```

Returns the complete chat session with all messages, tool calls, and results.

#### Get Run Log
```
GET /apps/{id}/runs/{runId}/log
```

Returns plain text execution log. Content-Type: `text/plain`.

#### Abort Run
```
POST /apps/{id}/runs/{runId}/abort
```

Aborts a currently running run. Only works on runs with `status: "running"`.

Response:
```json
{
  "run_id": "uuid...",
  "status": "failed",
  "message": "Run aborted by user"
}
```

#### Skip Scheduled Run
```
POST /schedules/{runId}/skip
```

Skips a pending scheduled run. Only works on runs with `status: "pending"`.

---

### Statistics

#### Get App Stats
```
GET /apps/{id}/stats
```

Response:
```json
{
  "total_runs": 42,
  "done_runs": 38,
  "failed_runs": 4,
  "success_rate": 0.9047619047619048,
  "avg_duration_seconds": 45.2,
  "last_run_at": "2026-04-02 09:05:30",
  "last_status": "done"
}
```

---

### Connectors (Notification Targets)

#### List App Connectors
```
GET /apps/{id}/connectors
```

#### Link Connector to App
```
POST /apps/{id}/connectors
Content-Type: application/json

{"connector_id": "uuid..."}
```

#### Unlink Connector
```
DELETE /apps/{id}/connectors/{connectorId}
```

---

## Error Format

All errors follow:
```json
{
  "error": {
    "code": "APP_NOT_FOUND",
    "message": "app not found",
    "hint": "Check app ID is correct"
  }
}
```

Common codes: `BAD_REQUEST`, `UNAUTHORIZED`, `APP_NOT_FOUND`, `NOT_FOUND`, `CONFLICT`, `RATE_LIMITED`, `INTERNAL_ERROR`.

---

## curl Demo

```bash
# Set your auth
TOKEN="your-jwt-or-api-key"
BASE="http://localhost:8321/api/v1"
AUTH="Authorization: Bearer $TOKEN"

# 1. Create an app
curl -X POST "$BASE/apps" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d '{
    "id": "stock-monitor",
    "name": "Stock Monitor",
    "description": "Check AAPL stock price",
    "prompt": "Search the web for current AAPL stock price and give me a brief summary.",
    "model": "claude-sonnet-4-20250514",
    "skills": ["web-search"]
  }'

# 2. Trigger a run
curl -X POST "$BASE/apps/stock-monitor/run" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d '{}'

# 3. Trigger via webhook (with payload)
curl -X POST "$BASE/apps/stock-monitor/trigger" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"payload": {"ticker": "TSLA", "action": "check price"}}'

# 4. List runs
curl "$BASE/apps/stock-monitor/runs?limit=5" -H "$AUTH"

# 5. Get a specific run result
curl "$BASE/apps/stock-monitor/runs/RUN_ID" -H "$AUTH"

# 6. Get full chat session of a run
curl "$BASE/apps/stock-monitor/runs/RUN_ID/session" -H "$AUTH"

# 7. Get execution log
curl "$BASE/apps/stock-monitor/runs/RUN_ID/log" -H "$AUTH"

# 8. Abort a running run
curl -X POST "$BASE/apps/stock-monitor/runs/RUN_ID/abort" -H "$AUTH"

# 9. Get app statistics
curl "$BASE/apps/stock-monitor/stats" -H "$AUTH"

# 10. Set up a schedule and activate
curl -X PUT "$BASE/apps/stock-monitor" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d '{
    "schedule_rules": {
      "entries": [{"time": "09:30", "repeat": {"type": "weekly", "days": ["mon","tue","wed","thu","fri"]}, "enabled": true}],
      "timezone": "America/New_York"
    }
  }'

curl -X POST "$BASE/apps/stock-monitor/activate" -H "$AUTH"

# 11. Check upcoming scheduled runs
curl "$BASE/schedules/upcoming" -H "$AUTH"

# 12. Deactivate
curl -X POST "$BASE/apps/stock-monitor/deactivate" -H "$AUTH"

# 13. Delete
curl -X DELETE "$BASE/apps/stock-monitor" -H "$AUTH"
```
