# Tofi Node Reference

> Complete reference for all node types in Tofi workflow engine.
> Format designed for AI agents to generate valid workflow YAML.

---

## Node Schema

Every node follows this structure:

```yaml
<node_id>:                    # Unique identifier (required)
  type: "<node_type>"         # Node type (required)
  config:                     # Static configuration
    <key>: <value>
  input:                      # Dynamic inputs (supports {{}} references)
    - var:
        id: "<local_name>"
        from: "{{node_id.path}}"
  env:                        # Environment variables (shell nodes only)
    <KEY>: "<value>"
  next: ["<node_id>"]         # Nodes to execute on success
  on_failure: ["<node_id>"]   # Nodes to execute on failure
  dependencies: ["<node_id>"] # Wait for these nodes before starting
  timeout: <seconds>          # Node-level timeout
  retry_count: <number>       # Retry attempts on failure
```

---

## Task Nodes

### shell

Execute shell commands with environment variable injection.

**Config:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `script` | string | Yes | Shell script to execute |

**Env:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `<KEY>` | string | No | Custom environment variables |

**Magic Variables (auto-injected):**
| Variable | Description |
|----------|-------------|
| `TOFI_ARTIFACTS_DIR` | Path to write output files |
| `TOFI_EXECUTION_ID` | Current execution ID |

**Output:** stdout of the script

**Example:**
```yaml
build_project:
  type: "shell"
  env:
    NODE_ENV: "production"
    API_KEY: "{{secrets.api_key}}"
  config:
    script: |
      npm install
      npm run build
      echo "Build complete" > $TOFI_ARTIFACTS_DIR/build.log
  timeout: 300
  next: ["deploy"]
```

---

### ai

Call LLM APIs (OpenAI, Claude, Gemini) for text generation.

**Config:**
| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `model` | string | Yes | - | Model name (e.g., "gpt-4o", "claude-3-5-sonnet-20241022", "gemini-1.5-pro") |
| `provider` | string | No | Auto-detected | Provider: "openai", "claude", "gemini" |
| `api_key` | string | Yes* | - | API key (or use `use_system_key`) |
| `use_system_key` | boolean | No | false | Use system-configured API key |
| `endpoint` | string | No | Provider default | Custom API endpoint |
| `system` | string | No | "" | System prompt |
| `prompt` | string | Yes | - | User prompt |
| `mcp_servers` | array | No | - | MCP server IDs for agent mode |

**Auto-detection rules:**
- Model starts with `claude` -> provider = "claude"
- Model starts with `gemini` -> provider = "gemini"
- Model starts with `gpt-`, `o1-`, `o3-` -> provider = "openai"

**Output:** Generated text response

**Example (Standard):**
```yaml
summarize:
  type: "ai"
  config:
    model: "gpt-4o"
    api_key: "{{secrets.openai_key}}"
    system: "You are a helpful assistant."
    prompt: "Summarize this text: {{fetch_content}}"
  next: ["save_summary"]
```

**Example (Claude):**
```yaml
analyze:
  type: "ai"
  config:
    model: "claude-3-5-sonnet-20241022"
    api_key: "{{secrets.anthropic_key}}"
    prompt: "Analyze this data: {{data.input}}"
```

**Example (Agent with MCP):**
```yaml
research_agent:
  type: "ai"
  config:
    model: "gpt-4o"
    use_system_key: true
    mcp_servers: ["web_search", "calculator"]
    system: "You are a research assistant with tools."
    prompt: "Research the latest AI news"
```

---

### api

Make HTTP requests to external APIs.

**Config:**
| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `url` | string | Yes | - | Request URL |
| `method` | string | No | "POST" | HTTP method (GET, POST, PUT, DELETE, etc.) |
| `headers` | object | No | {} | Request headers |
| `params` | object | No | {} | URL query parameters |
| `body` | string/object | No | - | Request body (auto-serialized if object) |

**Output:** Response body as string

**Example:**
```yaml
send_notification:
  type: "api"
  config:
    method: "POST"
    url: "https://api.slack.com/api/chat.postMessage"
    headers:
      Authorization: "Bearer {{secrets.slack_token}}"
      Content-Type: "application/json"
    body:
      channel: "#alerts"
      text: "Workflow completed: {{workflow_name}}"
```

---

### file

Select a file from the global file library for use in workflow.

**Config:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `file_id` | string | Yes | User-defined file ID from file library |
| `accept` | string/array | No | Allowed file extensions (e.g., ".csv,.json") |

**Output:** Absolute path to the file

**Example:**
```yaml
load_dataset:
  type: "file"
  config:
    file_id: "sales_data_2024"
    accept: ".csv,.xlsx"
  next: ["process_data"]

process_data:
  type: "shell"
  config:
    script: "python analyze.py {{load_dataset}}"
```

---

### workflow

Call another workflow as a sub-process (handoff).

**Config:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `file` | string | No | Path to workflow YAML file |
| `uses` | string | No | Component reference (e.g., "tofi/ai_response@v2") |

**Input:** Parameters passed to sub-workflow (accessible via `{{inputs.key}}`)

**Output:** Sub-workflow's final output

**Example:**
```yaml
call_processor:
  type: "workflow"
  config:
    file: "./processors/data_cleaner.yaml"
  input:
    - var:
        id: "raw_data"
        from: "{{fetch_data}}"
    - var:
        id: "output_format"
        from: "json"
```

**Example (Toolbox Component):**
```yaml
send_telegram:
  type: "workflow"
  config:
    uses: "tofi/telegram_notify"
  input:
    - var:
        id: "message"
        from: "{{summary}}"
    - secret:
        id: "bot_token"
        from: "{{secrets.telegram_token}}"
```

---

### hold

Pause workflow execution until manual approval via API.

**Config:** None required

**Input:**
| Field | Type | Description |
|-------|------|-------------|
| Any | Any | Context data passed to approver |

**Output:** Input data (pass-through on approve)

**Failure:** Returns error on reject

**Example:**
```yaml
approval_gate:
  type: "hold"
  input:
    - var:
        id: "deploy_target"
        from: "{{data.environment}}"
    - var:
        id: "changes_summary"
        from: "{{analyze.summary}}"
  next: ["deploy"]
  on_failure: ["notify_rejection"]
```

**Approval API:**
```bash
# Approve
POST /api/v1/executions/{exec_id}/nodes/approval_gate/approve
{"action": "approve"}

# Reject
POST /api/v1/executions/{exec_id}/nodes/approval_gate/approve
{"action": "reject"}
```

---

## Logic Nodes

### if

Evaluate boolean expression for conditional branching.

**Config:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `if` | string | Yes | Expression to evaluate |
| `output_bool` | string | No | If "true", output "true"/"false" instead of error |

**Built-in Functions:**
| Function | Description |
|----------|-------------|
| `contains(str, substr)` | Check if string contains substring |
| `len(str)` | Get string length |

**Variables:** All upstream node outputs are available as variables.

**Output:**
- On true: "EXPR_MATCHED" (or "true" if output_bool)
- On false: Error "CONDITION_NOT_MET" (or "false" if output_bool)

**Example:**
```yaml
check_threshold:
  type: "if"
  config:
    if: "score > 80 && status == 'active'"
  next: ["high_priority_path"]
  on_failure: ["normal_path"]

check_content:
  type: "if"
  config:
    if: "contains(response, 'success')"
  next: ["success_handler"]
```

---

### loop

Iterate over a list or range, executing a task for each item.

**Config:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `mode` | string | Yes | "list" or "range" |
| `items` | array/string | If mode=list | Items to iterate (or JSON string) |
| `start` | number | If mode=range | Range start |
| `end` | number | If mode=range | Range end |
| `step` | number | No | Range step (default: 1) |
| `iterator` | string | No | Variable name for current item (default: "item") |
| `max_concurrency` | number | No | Max parallel iterations (default: 1) |
| `fail_fast` | boolean | No | Stop on first error (default: false) |
| `task` | object | Yes | Task definition to execute per item |

**Output:** JSON array of all iteration results

**Example (List):**
```yaml
process_users:
  type: "loop"
  config:
    mode: "list"
    items: ["alice", "bob", "charlie"]
    iterator: "username"
    max_concurrency: 3
    task:
      type: "api"
      config:
        url: "https://api.example.com/users/{{username}}"
        method: "GET"
```

**Example (Range):**
```yaml
batch_process:
  type: "loop"
  config:
    mode: "range"
    start: 1
    end: 10
    step: 1
    iterator: "page"
    task:
      type: "api"
      config:
        url: "https://api.example.com/data?page={{page}}"
```

**Example (Dynamic Items):**
```yaml
process_results:
  type: "loop"
  config:
    mode: "list"
    items: "{{fetch_list}}"  # JSON array from previous node
    iterator: "item"
    task:
      type: "shell"
      config:
        script: "echo Processing: {{item}}"
```

---

### check

Simple value checking for boolean conditions.

**Config:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `mode` | string | Yes | Check mode |
| `value` | string | Yes | Value to check |

**Modes:**
| Mode | Description |
|------|-------------|
| `is_true` | Value equals "true" (case-insensitive) |
| `is_false` | Value equals "false" (case-insensitive) |
| `is_empty` | Value is empty string or null |
| `exists` | Value is not empty |

**Example:**
```yaml
check_enabled:
  type: "check"
  config:
    mode: "is_true"
    value: "{{feature_flag}}"
  next: ["feature_enabled"]
  on_failure: ["feature_disabled"]
```

---

### text

String pattern matching and validation.

**Config:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `mode` | string | Yes | Match mode |
| `target` | string | Yes | String to check |
| `value` | string | Yes | Pattern or substring |

**Modes:**
| Mode | Description |
|------|-------------|
| `contains` | Target contains value |
| `starts_with` | Target starts with value |
| `ends_with` | Target ends with value |
| `matches` | Target matches regex pattern |

**Example:**
```yaml
validate_email:
  type: "text"
  config:
    mode: "matches"
    target: "{{user.email}}"
    value: "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"
  next: ["valid_email"]
  on_failure: ["invalid_email"]
```

---

### math

Numeric comparison operations.

**Config:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `operator` | string | Yes | Comparison operator |
| `left` | string | Yes | Left operand |
| `right` | string | Yes | Right operand |

**Operators:** `>`, `<`, `==`, `>=`, `<=`, `!=`

**Example:**
```yaml
check_cpu:
  type: "math"
  config:
    operator: ">"
    left: "{{metrics.cpu_usage}}"
    right: "80"
  next: ["alert_high_cpu"]
```

---

### list

JSON array operations.

**Config:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `mode` | string | Yes | Operation mode |
| `list` | string | Yes | JSON array string |
| `value` | string | If mode=contains | Value to check |
| `length` | number | If mode=length_is | Expected length |

**Modes:**
| Mode | Description |
|------|-------------|
| `length_is` | Check if array length equals value |
| `contains` | Check if array contains value |

**Example:**
```yaml
check_results:
  type: "list"
  config:
    mode: "contains"
    list: "{{fetch_tags}}"
    value: "urgent"
  next: ["handle_urgent"]
```

---

## Data Nodes

### var

Define static or computed values.

**Config:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `value` | any | No | Single value |
| `<key>` | any | No | Multiple key-value pairs |

**Output:** Value or JSON object of all config fields

**Example (Single Value):**
```yaml
api_endpoint:
  type: "var"
  config:
    value: "https://api.example.com/v1"
```

**Example (Multiple Values):**
```yaml
app_config:
  type: "var"
  config:
    app_name: "MyApp"
    version: "1.0.0"
    max_retries: 3
```

**Usage:**
```yaml
call_api:
  type: "api"
  config:
    url: "{{api_endpoint}}"

log_name:
  type: "shell"
  config:
    script: "echo {{app_config.app_name}}"
```

---

### secret

Define sensitive values with automatic log masking.

**Config:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `value` | string | No | Single secret |
| `<key>` | string | No | Multiple secrets |

**Output:** Secret value (masked as `********` in logs)

**Example:**
```yaml
api_secrets:
  type: "secret"
  config:
    openai_key: "sk-xxx"
    db_password: "super_secret"

call_ai:
  type: "ai"
  config:
    api_key: "{{api_secrets.openai_key}}"
    # Log shows: api_key: ********
```

---

### dict

Extract fields from JSON and build structured objects.

**Config:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `input` | string | No | Source JSON string |
| `fields` | array | Yes | Field definitions |

**Field Definition:**
| Field | Type | Description |
|-------|------|-------------|
| `key` | string | Output field name |
| `value` | string | Value expression |

**Value Expressions:**
- `"input.path.to.field"` - Extract from input JSON
- `"{{node_id}}"` - Reference another node
- `"literal"` - Literal string

**Output:** JSON object with extracted fields

**Example:**
```yaml
parse_response:
  type: "dict"
  config:
    input: "{{api_response}}"
    fields:
      - key: "user_id"
        value: "input.data.user.id"
      - key: "email"
        value: "input.data.user.email"
      - key: "status"
        value: "active"
      - key: "timestamp"
        value: "{{current_time}}"
```

---

## Base Nodes

### virtual

Logical grouping or synchronization point with no execution.

**Config:** None

**Output:** Empty string

**Use Cases:**
- Wait for multiple parallel branches
- Logical workflow organization
- Fan-in synchronization

**Example:**
```yaml
# Parallel branches
task_a:
  type: "shell"
  config:
    script: "echo A"
  next: ["sync_point"]

task_b:
  type: "shell"
  config:
    script: "echo B"
  next: ["sync_point"]

# Synchronization
sync_point:
  type: "virtual"
  dependencies: ["task_a", "task_b"]
  next: ["final_step"]
```

---

## Complete Workflow Example

```yaml
id: data_pipeline
name: "Data Processing Pipeline"
description: "Fetch, process, and report on data"
timeout: 600

data:
  source_url: "https://api.example.com/data"

secrets:
  api_key: "{{env.API_KEY}}"
  slack_webhook: "{{env.SLACK_WEBHOOK}}"

nodes:
  # 1. Fetch data from API
  fetch_data:
    type: "api"
    config:
      method: "GET"
      url: "{{data.source_url}}"
      headers:
        Authorization: "Bearer {{secrets.api_key}}"
    timeout: 30
    next: ["validate_data"]
    on_failure: ["notify_error"]

  # 2. Validate data structure
  validate_data:
    type: "check"
    config:
      mode: "exists"
      value: "{{fetch_data}}"
    next: ["process_items"]
    on_failure: ["notify_error"]

  # 3. Process each item
  process_items:
    type: "loop"
    config:
      mode: "list"
      items: "{{fetch_data}}"
      iterator: "item"
      max_concurrency: 5
      task:
        type: "ai"
        config:
          model: "gpt-4o-mini"
          use_system_key: true
          prompt: "Summarize: {{item}}"
    next: ["generate_report"]

  # 4. Generate final report
  generate_report:
    type: "shell"
    config:
      script: |
        echo "# Report" > $TOFI_ARTIFACTS_DIR/report.md
        echo "Processed items: {{process_items}}" >> $TOFI_ARTIFACTS_DIR/report.md
    next: ["approval_gate"]

  # 5. Wait for approval
  approval_gate:
    type: "hold"
    input:
      - var:
          id: "report_preview"
          from: "{{generate_report}}"
    next: ["notify_success"]
    on_failure: ["notify_rejection"]

  # 6. Success notification
  notify_success:
    type: "api"
    config:
      method: "POST"
      url: "{{secrets.slack_webhook}}"
      body:
        text: "Pipeline completed successfully!"

  # Error handlers
  notify_error:
    type: "api"
    config:
      method: "POST"
      url: "{{secrets.slack_webhook}}"
      body:
        text: "Pipeline failed!"

  notify_rejection:
    type: "api"
    config:
      method: "POST"
      url: "{{secrets.slack_webhook}}"
      body:
        text: "Pipeline rejected by approver"
```

---

## Quick Reference Table

| Type | Category | Purpose | Key Config |
|------|----------|---------|------------|
| `shell` | Task | Execute shell commands | `script` |
| `ai` | Task | LLM text generation | `model`, `prompt` |
| `api` | Task | HTTP requests | `url`, `method` |
| `file` | Task | Load file from library | `file_id` |
| `workflow` | Task | Call sub-workflow | `file` or `uses` |
| `hold` | Task | Wait for approval | (input data) |
| `if` | Logic | Expression branching | `if` |
| `loop` | Logic | Iterate items | `mode`, `items`, `task` |
| `check` | Logic | Simple value check | `mode`, `value` |
| `text` | Logic | String matching | `mode`, `target`, `value` |
| `math` | Logic | Numeric comparison | `operator`, `left`, `right` |
| `list` | Logic | Array operations | `mode`, `list` |
| `var` | Data | Define values | `value` or key-values |
| `secret` | Data | Sensitive values | `value` or key-values |
| `dict` | Data | JSON extraction | `input`, `fields` |
| `virtual` | Base | Sync point | (none) |
