# Var Node (Variable)

> Store a single value for use by downstream nodes.

**Type:** `var`
**Category:** Data
**Source:** `internal/engine/data/var.go`

---

## Config

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `value` | any | Yes | Value to store. Supports string, number, boolean, JSON object, or `{{}}` references. |

**Note:** In YAML, `var` nodes use a top-level `value` field, not inside `config`:

```yaml
my_var:
  type: var
  value: "hello world"
```

## Output

- If `value` is a string → returned as-is
- If `value` is any other type → JSON serialized
- If `value` is nil but `config` has other fields → entire config is JSON serialized
- If everything is empty → empty string `""`

## Examples

```yaml
# String value
greeting:
  type: var
  value: "Hello World"

# Number
max_retries:
  type: var
  value: 3

# Reference to another node
captured_output:
  type: var
  value: "{{ai_node}}"

# Used by downstream
consumer:
  type: ai
  config:
    model: gpt-4o
    use_system_key: true
    prompt: "Process this: {{captured_output}}"
```
