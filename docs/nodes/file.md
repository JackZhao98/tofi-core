# File Node

> Load a user-uploaded file from the file library and return its absolute path.

**Type:** `file`
**Category:** Task
**Source:** `internal/engine/tasks/file.go`

---

## Config

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `file_id` | string | Yes | File ID from the user's file library |
| `accept` | string/array | No | Allowed file extensions. Comma-separated string or array. |

## Output

Absolute file path: `{home}/{user}/storage/files/{uuid}`

The path includes the original filename from the upload.

## Extension Validation

If `accept` is specified, the file's extension is checked:
- Extensions are case-insensitive
- Can be specified with or without leading dot
- `accept: ".csv,.json,.txt"` or `accept: ["csv", "json", "txt"]`

## Examples

```yaml
# Load a CSV file
load_data:
  type: file
  config:
    file_id: "sales_2024"
    accept: ".csv,.xlsx"

# Use file path in shell
process:
  type: shell
  config:
    script: "python analyze.py {{load_data}}"
```

## Errors

| Condition | Error |
|-----------|-------|
| `file_id` missing | `no file uploaded` |
| Database unavailable | `database connection not available` |
| File not found in DB | `file not found` |
| Extension not in `accept` list | `file type not allowed` |
