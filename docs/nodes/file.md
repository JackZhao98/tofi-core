# File Node

> File container — accepts upstream data or user-uploaded files. Returns structured JSON metadata; content resolved on-demand via `{{node.content}}`.

**Type:** `file`
**Category:** Data
**Source:** `internal/engine/tasks/file.go`

---

## Dual Input Mode

The File node operates in two modes depending on whether it receives upstream data:

### Mode 1: Upstream Input (Container)

When connected to an upstream node via edge, the engine injects the upstream output as `_input`:

- `engine.go` checks `node.Dependencies[0]` and injects its output into `resolvedConfig["_input"]`
- If `save_to_disk` is true, content is written to the artifacts directory
- If `save_to_disk` is false, content is stored in `ExecutionContext.UpstreamContent` for on-demand resolution

### Mode 2: User Upload

When no upstream input exists, the node resolves a user-uploaded file:

1. **File ID** (preferred): Looks up `file_id` in the database via `storage.DB.GetUserFile()`
2. **File Path** (legacy): Resolves `file_path` as a symlink under `{home}/{user}/workflows/{workflow_id}/files/`

---

## Config

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `file_id` | string | No | File ID for database lookup (new system) |
| `file_path` | string | No | Relative path to symlinked file (legacy) |
| `filename` | string | No | Original filename for display and extension checks |
| `accept` | string/array | No | Allowed file extensions (e.g. `.csv,.json`) |
| `save_to_disk` | boolean | No | Save upstream data to artifacts directory (default: false) |

> At least one of `file_id`, `file_path`, or upstream input (`_input`) must be present.

---

## Output

JSON object (always):

```json
{
  "path": "/absolute/path/to/file",
  "filename": "data.csv",
  "mime_type": "text/csv",
  "size": 1024,
  "file_id": "sales_2024"
}
```

- `path` is empty when upstream data is not saved to disk
- `file_id` is empty for upstream data and legacy file_path files

### On-Demand Content Resolution

Downstream nodes can access file content via `{{file_node.content}}`:

1. If `path` exists → reads file from disk (text files only; binary returns error)
2. If no path → looks up `ExecutionContext.UpstreamContent[nodeID]`
3. Handled by `resolveFileContent()` in `models.go`

---

## Extension Validation

If `accept` is specified, the file's extension is checked (case-insensitive):

- Extensions can include or omit leading dot: `.csv` or `csv`
- Comma-separated string or array: `".csv,.json"` or `["csv", "json"]`
- Extension is resolved from: config `filename` → file path → MIME type inference

---

## Frontend Behavior

- File node is a **container node** — edges use `dependencies` instead of `{{}}` references
- `edgeSync.ts`: Dragging a connection to a File node adds source to `dependencies` (not field injection)
- MentionInput provides three-level submenu: `content`/`filename` (common) → `path`/`mime_type`/`size`/`file_id` (advanced)
- Category: **Data** (alongside var, dict)

---

## Examples

```yaml
# User-uploaded file (File ID system)
load_data:
  type: file
  config:
    file_id: "sales_2024"
    filename: "sales_2024.csv"
    accept: ".csv,.xlsx"

# User-uploaded file (legacy symlink)
load_config:
  type: file
  config:
    file_path: "config.yaml"

# Container mode: receive upstream data and save to disk
save_result:
  type: file
  config:
    save_to_disk: true
  dependencies: ["ai_generate"]

# Use file content in downstream node
process:
  type: shell
  config:
    script: "echo '{{load_data.content}}' | python analyze.py"
  dependencies: ["load_data"]

# Use file metadata
log_info:
  type: shell
  config:
    script: "echo 'File: {{load_data.filename}} ({{load_data.mime_type}}, {{load_data.size}} bytes)'"
  dependencies: ["load_data"]
```

## Errors

| Condition | Error |
|-----------|-------|
| No input source | `no file linked (config.file_id or config.file_path is missing)` |
| File ID not in DB | `file not found: {id}` |
| Symlink broken | `broken symlink (source file moved or deleted?)` |
| Extension not allowed | `file type not allowed: {ext}` |
| Binary content access | `cannot read binary file content, use .path instead` |
| Artifacts dir creation fails | `failed to create artifacts dir` |
