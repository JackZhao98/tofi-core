# Dict Node

> Extract fields from JSON input and build structured key-value objects.

**Type:** `dict`
**Category:** Data
**Source:** `internal/engine/data/dict.go`

---

## Config

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `input` | string | No | Source JSON string. Supports `{{}}` references. |
| `fields` | array | Yes | Field definitions. Each element: `{key: string, value: string}` |

## Field Value Resolution

The `value` in each field is resolved in this order:

| Format | Behavior | Example |
|--------|----------|---------|
| `input.path` | Extract from input JSON via gjson path | `input.user.name` |
| `{{input.path}}` | Template syntax — extract from input JSON | `{{input.email}}` |
| `{{node_id}}` | Reference another node's output | `{{ai_action}}` |
| any other string | Literal value | `"fixed value"` |

**Mixed templates** are supported: `"Hello {{input.name}}, your ID is {{user_id}}"` resolves each `{{}}` independently.

## Output

JSON object: `{"key1": "value1", "key2": "value2"}`

Individual fields can be referenced downstream as `{{dict_node.key1}}`.

## JSON Extraction

The `input` string does not need to be pure JSON. The engine scans for the first valid JSON object or array within the string. This is useful when AI responses contain JSON embedded in natural language.

## Examples

```yaml
# Extract fields from AI response
parse_response:
  type: dict
  config:
    input: "{{api_response}}"
    fields:
      - key: user_id
        value: "input.data.user.id"
      - key: email
        value: "input.data.user.email"
      - key: status
        value: "active"

# Reference individual fields downstream
use_fields:
  type: ai
  config:
    model: gpt-4o
    use_system_key: true
    prompt: "Send email to {{parse_response.email}}"
```
