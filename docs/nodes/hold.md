# Hold Node

> Pause workflow execution until manual approval or rejection via API.

**Type:** `hold`
**Category:** Task (Flow Control)
**Source:** `internal/engine/tasks/hold.go`

---

## Config

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `input` | string | No | Data to pass through on approval. Supports `{{}}` references. |

## Behavior

1. Logs waiting status and pauses execution
2. Polls every 1 second for user decision via `ctx.GetApproval(nodeID)`
3. On **approve** → returns `input` data (pass-through)
4. On **reject** → returns error `"execution rejected by user"`
5. On context cancellation → returns cancellation error

## Output

The `input` value, passed through unchanged on approval.

## Approval API

```bash
# Approve
POST /api/v1/executions/{exec_id}/nodes/{node_id}/approve
{"action": "approve"}

# Reject
POST /api/v1/executions/{exec_id}/nodes/{node_id}/approve
{"action": "reject"}
```

## Example

```yaml
review_gate:
  type: hold
  config:
    input: "{{ai_analysis}}"

# On approval, downstream nodes receive the AI analysis output
post_approval:
  type: ai
  config:
    model: gpt-4o
    use_system_key: true
    prompt: "Format this approved analysis: {{review_gate}}"
```
