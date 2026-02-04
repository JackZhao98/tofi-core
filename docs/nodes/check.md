# Check Node

> Validate a single value and output `"true"` or `"false"`.

**Type:** `check`
**Category:** Logic
**Source:** `internal/engine/logic/check.go`

---

## Config

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `value` | string | Yes | Value to check. Supports `{{}}` references. |
| `operator` | string | Yes | Check type (see table below) |

## Operators

| Operator | Description | Logic |
|----------|-------------|-------|
| `is_empty` | Value is empty or whitespace-only | `len(strings.TrimSpace(value)) == 0` |
| `not_empty` | Value is not empty | `len(strings.TrimSpace(value)) > 0` |
| `is_true` | Value is truthy | `strings.ToLower(value) == "true"` or `value == "1"` |
| `is_false` | Value is falsy | `strings.ToLower(value) == "false"` or `value == "0"` |
| `is_number` | Value is a valid number | `strconv.ParseFloat(value, 64)` succeeds |
| `is_json` | Value is valid JSON | `json.Unmarshal` succeeds |

## Output

`"true"` or `"false"` (string)

## Frontend Behavior

Same as Compare — when `on_true`/`on_false` are configured, the serializer generates a `_branch` node automatically.

## Examples

```yaml
# Check if data exists
has_data:
  type: check
  config:
    value: "{{optional_input}}"
    operator: not_empty
  next: ["has_data_branch"]

has_data_branch:
  type: branch
  config:
    condition: "{{has_data}}"
    on_true: ["process_data"]
    on_false: ["use_default"]

# Validate JSON response
valid_json:
  type: check
  config:
    value: "{{api_response}}"
    operator: is_json

# Check boolean flag
is_enabled:
  type: check
  config:
    value: "{{feature_flag}}"
    operator: is_true
```

## Errors

| Condition | Error |
|-----------|-------|
| `operator` missing | `config.operator is required` |
| Unknown operator | `unsupported check operator` |
