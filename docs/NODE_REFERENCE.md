# Tofi Node Reference

> Complete reference for all node types in Tofi workflow engine.
> For detailed documentation on each node, see the [nodes/](nodes/) directory.

---

## Node Schema

Every node follows this structure:

```yaml
<node_id>:                    # Unique identifier (required)
  type: "<node_type>"         # Node type (required)
  label: "Display Name"       # Optional display name
  config:                     # Static configuration
    <key>: <value>
  next: ["<node_id>"]         # Nodes to execute on success
  on_failure: ["<node_id>"]   # Nodes to execute on failure
  dependencies: ["<node_id>"] # Wait for these nodes before starting
  timeout: <seconds>          # Node-level timeout
```

**Data References:**
- `{{node_id}}` — Reference another node's output
- `{{node_id.field}}` — Reference a specific field (backend resolves via gjson)
- `{{secrets.key}}` — Reference a secret value
- `{{data.key}}` — Reference workflow-level data
- `{{env.VAR}}` — Reference an environment variable

---

## Registered Nodes

These nodes are registered in the engine and fully functional:

| Type | Category | Purpose | Key Config | Doc |
|------|----------|---------|------------|-----|
| `ai` | Task | LLM text generation | `model`, `prompt` | [ai.md](nodes/ai.md) |
| `shell` | Task | Execute shell scripts | `script` | [shell.md](nodes/shell.md) |
| `hold` | Task | Wait for manual approval | `input` | [hold.md](nodes/hold.md) |
| `file` | Task | Load file from library | `file_id` | [file.md](nodes/file.md) |
| `workflow` | Task | Call sub-workflow | `uses` | [workflow.md](nodes/workflow.md) |
| `compare` | Logic | Compare two values → true/false | `left`, `operator`, `right` | [compare.md](nodes/compare.md) |
| `check` | Logic | Check single value → true/false | `value`, `operator` | [check.md](nodes/check.md) |
| `branch` | Logic | Route based on boolean | `condition`, `on_true`, `on_false` | [branch.md](nodes/branch.md) |
| `loop` | Logic | Iterate over items | `mode`, `task` | [loop.md](nodes/loop.md) |
| `var` | Data | Store a value | `value` | [var.md](nodes/var.md) |
| `dict` | Data | Build structured JSON | `input`, `fields` | [dict.md](nodes/dict.md) |
| `secret` | Data | Sensitive values with masking | key-value pairs | [secret.md](nodes/secret.md) |

**Not registered** (code exists but not wired into engine):
- `api` — HTTP request node (`api.go` exists but not in `GetAction()`)

**Internal / Fallback:**
- `virtual` — Default handler for unknown types. Returns `"VIRTUAL_OK"`.

---

## Edge & Connection Model

Edges represent execution flow. Two systems:

### Reference-Driven Edges (Normal Nodes)
Normal nodes do **not** have manual `next`. Edges are auto-computed from `{{}}` references:
- If node B's fields contain `{{A}}`, edge A→B appears automatically.
- Removing the reference removes the edge.
- `next` and `dependencies` are auto-computed by the frontend.

### Boolean Trigger Edges (Compare/Check Only)
Only `compare` and `check` nodes can actively trigger downstream via `on_true`/`on_false`.
These are the only nodes with T/F branching capability.

---

## Frontend Serialization Notes

- **Compare/Check** nodes auto-generate a `_branch` suffix node in YAML. The frontend merges them back into one node on load.
- **Secret** references use `ref:SECRET_NAME` format in YAML config, which the engine resolves via database lookup and decryption.
- **Var** nodes use a top-level `value` field (not inside `config`) in YAML.

---

## Defaults

| Node | Parameter | Default |
|------|-----------|---------|
| `shell` | timeout | 60 seconds |
| `loop` | iterator | `"item"` |
| `loop` | max_concurrency | 1 |
| `loop` | step | 1 |
| `loop` | fail_fast | false |
| `ai` | use_system_key | false |
| `ai` | provider | Auto-detected from model name |
| `workflow` | max recursion depth | 10 |
