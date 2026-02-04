# Workflow Node (Handoff)

> Call another workflow as a sub-process. Supports data and secrets passing.

**Type:** `workflow`
**Category:** Task
**Source:** `internal/engine/tasks/handoff.go`

---

## Config

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `uses` | string | Yes* | Workflow reference. Format: `user/workflow_name` or `tofi/component@v1` |
| `workflow` | string | No | Legacy: workflow ID |
| `action` | string | No | Legacy: action name |
| `file` | string | No | Direct file path to workflow YAML |
| `data` | object | No | Data payload passed to sub-workflow |
| `secrets` | object | No | Secrets payload passed to sub-workflow |

*One of `uses`, `workflow`, `action`, or `file` is required. Priority: `uses` > `workflow` > `action` > `file`.

## Recursion Limit

Maximum depth: **10**. Prevents infinite workflow loops.

## Output

JSON object containing all sub-workflow node outputs:
```json
{
  "node_1": "result_1",
  "node_2": {"key": "value"}
}
```

## Execution

1. Resolves the target workflow YAML
2. Creates an isolated child execution context (inherits DB connection, increments depth)
3. Passes `data` and `secrets` as payload
4. Waits for sub-workflow completion
5. Collects all node results and returns as JSON

## Examples

```yaml
# Call a shared component
send_notification:
  type: workflow
  config:
    uses: "tofi/telegram_notify@v2"
    data:
      message: "{{summary}}"
    secrets:
      bot_token: "{{secrets.telegram_token}}"

# Call by file path
run_processor:
  type: workflow
  config:
    file: "./processors/data_cleaner.yaml"
    data:
      raw_data: "{{fetch_data}}"
      format: "json"
```

## Errors

| Condition | Error |
|-----------|-------|
| Depth >= 10 | `exceeded maximum workflow recursion depth (10)` |
| No workflow reference | `failed to resolve workflow` |
| Workflow starter not initialized | `workflowStarter not initialized` |
