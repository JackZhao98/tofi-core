# Loop Node

> Iterate over a list or numeric range, executing a task template for each item. Supports concurrency.

**Type:** `loop`
**Category:** Logic
**Source:** `internal/engine/logic/loop.go`

---

## Config

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `mode` | string | Yes | — | `"list"` or `"range"` |
| `items` | array/string | If mode=list | — | Items to iterate. JSON array or `{{}}` reference to one. |
| `start` | int | If mode=range | — | Range start (inclusive) |
| `end` | int | If mode=range | — | Range end (inclusive) |
| `step` | int | No | `1` | Range step. 0 is treated as 1. |
| `iterator` | string | No | `"item"` | Variable name for current item. Use `{{item}}` in task template. |
| `max_concurrency` | int | No | `1` | Max parallel iterations. 0 = unlimited. |
| `fail_fast` | bool | No | `false` | Stop all iterations on first error. |
| `task` | object | Yes | — | Task definition to execute per iteration. JSON object with `type` and `config`. |

## Iterator Variable

Each iteration registers the current item as a variable accessible via `{{iterator_name}}` (default `{{item}}`).

## Output

JSON array of all iteration results, ordered by index:
```json
["result_0", "result_1", {"key": "value"}, ...]
```

If an iteration fails, its result is:
```json
{"index": 2, "error": "error message"}
```

## Concurrency

- `max_concurrency: 1` — sequential execution (default)
- `max_concurrency: 3` — up to 3 iterations run in parallel
- `max_concurrency: 0` — all iterations run in parallel (unlimited)

Each iteration gets its own isolated execution context and artifacts directory.

## Examples

```yaml
# Iterate over a list
process_users:
  type: loop
  config:
    mode: list
    items: ["alice", "bob", "charlie"]
    iterator: username
    max_concurrency: 3
    task:
      type: ai
      config:
        model: gpt-4o
        use_system_key: true
        prompt: "Generate a greeting for {{username}}"

# Iterate over a range
batch_pages:
  type: loop
  config:
    mode: range
    start: 1
    end: 10
    step: 1
    iterator: page
    task:
      type: shell
      config:
        script: "curl https://api.example.com/data?page={{page}}"

# Dynamic items from another node
process_results:
  type: loop
  config:
    mode: list
    items: "{{fetch_list}}"
    iterator: item
    fail_fast: true
    task:
      type: shell
      config:
        script: "echo Processing: {{item}}"
```

## Errors

| Condition | Error |
|-----------|-------|
| Empty items list | `items list is empty` |
| Invalid mode | No items or range generated |
| `fail_fast` + iteration error | Remaining iterations skipped |
