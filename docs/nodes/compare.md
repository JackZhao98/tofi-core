# Compare Node

> Compare two values and output `"true"` or `"false"`. Supports numeric, string, and list comparisons.

**Type:** `compare`
**Category:** Logic
**Source:** `internal/engine/logic/compare.go`

---

## Config

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `left` | string | Yes | Left operand. Supports `{{}}` references. |
| `operator` | string | Yes | Comparison operator (see table below) |
| `right` | string | Yes | Right operand. Supports `{{}}` references. |

## Operators

| Category | Operator | Description | Type Requirement |
|----------|----------|-------------|------------------|
| Universal | `==` | Equal (try numeric first, fallback string) | Any |
| Universal | `!=` | Not equal | Any |
| Numeric | `>` | Greater than | Both must be valid numbers |
| Numeric | `<` | Less than | Both must be valid numbers |
| Numeric | `>=` | Greater than or equal | Both must be valid numbers |
| Numeric | `<=` | Less than or equal | Both must be valid numbers |
| Numeric | `between` | Value in range | left=number, right=JSON `[min, max]` |
| String | `contains` | Left contains right | Converted to string |
| String | `not_contains` | Left does not contain right | Converted to string |
| String | `starts_with` | Left starts with right | Converted to string |
| String | `ends_with` | Left ends with right | Converted to string |
| String | `matches` | Regex match | left=string, right=regex pattern |
| List | `in` | Left is in right array | right must be JSON array |
| List | `not_in` | Left is not in right array | right must be JSON array |

## Output

`"true"` or `"false"` (string)

## Frontend Behavior

In the UI, Compare nodes with `on_true`/`on_false` configured are serialized as **two** backend nodes:
1. `{node_id}` — the compare node itself
2. `{node_id}_branch` — an auto-generated branch node with `condition: "{{node_id}}"`

The deserializer merges them back into a single UI node.

## Examples

```yaml
# Numeric comparison
check_score:
  type: compare
  config:
    left: "{{metrics.score}}"
    operator: ">"
    right: "80"
  next: ["score_branch"]

score_branch:
  type: branch
  config:
    condition: "{{check_score}}"
    on_true: ["high_handler"]
    on_false: ["low_handler"]

# Range check
in_range:
  type: compare
  config:
    left: "{{value}}"
    operator: between
    right: "[10, 100]"

# String contains
has_keyword:
  type: compare
  config:
    left: "{{response}}"
    operator: contains
    right: "success"

# List membership
valid_status:
  type: compare
  config:
    left: "{{status}}"
    operator: in
    right: '["active", "pending", "review"]'

# Regex match
email_format:
  type: compare
  config:
    left: "{{user_input}}"
    operator: matches
    right: "^[a-zA-Z0-9+_.-]+@[a-zA-Z0-9.-]+$"
```

## Errors

| Condition | Error |
|-----------|-------|
| Non-numeric value with `>`, `<`, `>=`, `<=` | `not a valid number` |
| `between` right is not JSON array | `must be a JSON array [min, max]` |
| `between` array length != 2 | `must have exactly 2 elements` |
| `in`/`not_in` right is not JSON array | `must be a JSON array` |
| Unknown operator | `unsupported operator` |
