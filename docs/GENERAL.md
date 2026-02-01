# Tofi - Workflow Engine

> **T**rigger-based **O**perations & **F**low **I**ntegration

Tofi is a production-grade workflow engine written in Go, designed for task automation, AI integration, and multi-tenancy support.

---

## Architecture Overview

```
tofi-core/
├── cmd/tofi/main.go           # CLI entry point
├── internal/
│   ├── engine/                # Core execution engine
│   │   ├── tasks/             # Task nodes (shell, ai, api, file, hold)
│   │   ├── logic/             # Logic nodes (if, loop, check, math, text, list)
│   │   ├── data/              # Data nodes (var, secret, dict)
│   │   └── base/              # Base nodes (virtual)
│   ├── server/                # HTTP API server
│   ├── models/                # Data models and resolvers
│   ├── storage/               # SQLite persistence
│   ├── parser/                # YAML parser
│   └── toolbox/               # Built-in workflow components
├── workflows/                 # Example workflows
└── docs/                      # Documentation
```

---

## Execution Model

### DAG-based Execution

Workflows are executed as **Directed Acyclic Graphs (DAGs)**. Each node represents a task or logic unit.

```
[start] --> [task_a] --> [if_check] --> [task_b] --> [end]
                             |
                             +--> [on_failure]
```

### Node Lifecycle

1. **Pending**: Node waiting for dependencies
2. **Running**: Node is executing
3. **Success**: Node completed successfully
4. **Failed**: Node execution failed
5. **Skipped**: Node skipped due to upstream failure

### Concurrency

- Multiple independent nodes execute in parallel
- `dependencies` field enforces sequential execution
- `next` field triggers downstream nodes on success
- `on_failure` handles error paths

---

## Workflow Structure

A workflow is a YAML file with the following structure:

```yaml
# Metadata
id: workflow_id           # Unique identifier
name: "My Workflow"       # Display name
description: "..."        # Optional description
timeout: 300              # Global timeout in seconds

# Input Schema
data:
  key: "default_value"    # Workflow inputs

# Secret References
secrets:
  api_key: "ref:OPENAI_KEY"

# Node Definitions
nodes:
  node_id:
    type: "node_type"
    config: {}            # Static configuration
    input: []             # Dynamic inputs with variable references
    env: {}               # Environment variables (shell only)
    next: []              # Success path
    on_failure: []        # Failure path
    dependencies: []      # Explicit dependencies
    timeout: 60           # Node-level timeout
```

---

## Variable System

### Reference Syntax

```yaml
# Reference another node's output
prompt: "Process this: {{fetch_data}}"

# Reference a specific field (JSON path)
user_email: "{{get_user.email}}"

# Reference workflow input
api_key: "{{secrets.api_key}}"
message: "Hello, {{data.username}}"

# Escape literal braces
template: "Use \\{{ for literal braces"
```

### Resolution Order

1. **Local Context** (node inputs) - highest priority
2. **Global Context** (upstream node outputs)
3. **Workflow Data** (workflow-level data block)
4. **Workflow Secrets** (workflow-level secrets block)

---

## Dual Execution Modes

### CLI Mode

Direct workflow execution for scripting and testing:

```bash
./tofi run -workflow workflows/demo.yaml -home .tofi
```

### Server Mode

HTTP API for integration with web applications:

```bash
./tofi server -port 8080 -workers 10 -home .tofi
```

---

## API Reference

### Authentication

All protected endpoints require JWT token:

```
Authorization: Bearer <token>
```

Generate test token:
```bash
./tofi token -user jack -secret "your-secret"
```

### Core Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/v1/run` | Execute a workflow |
| `GET` | `/api/v1/executions` | List executions |
| `GET` | `/api/v1/executions/{id}` | Get execution details |
| `GET` | `/api/v1/executions/{id}/logs` | Get execution logs |
| `POST` | `/api/v1/executions/{id}/cancel` | Cancel execution |
| `POST` | `/api/v1/executions/{id}/nodes/{node}/approve` | Approve hold node |

### Workflow Management

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/workflows` | List workflows |
| `GET` | `/api/v1/workflows/{id}` | Get workflow |
| `POST` | `/api/v1/workflows` | Save workflow |
| `DELETE` | `/api/v1/workflows/{id}` | Delete workflow |
| `GET` | `/api/v1/workflows/{id}/schema` | Get input schema |

### File Management

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/files` | List user files |
| `POST` | `/api/v1/files` | Upload file |
| `DELETE` | `/api/v1/files/{id}` | Delete file |

### Artifacts

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/artifacts` | List all artifacts |
| `GET` | `/api/v1/executions/{id}/artifacts` | List execution artifacts |
| `GET` | `/api/v1/executions/{id}/artifacts/{file}` | Download artifact |

### Secrets

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/secrets` | List secrets (names only) |
| `POST` | `/api/v1/secrets` | Create secret |
| `DELETE` | `/api/v1/secrets/{name}` | Delete secret |

---

## Storage Layout

```
.tofi/
├── admin/
│   └── tofi.db              # SQLite database
└── {user}/
    ├── workflows/           # User workflow files
    │   ├── my_workflow.yaml
    │   └── my_workflow.json # Metadata (positions, etc.)
    ├── storage/
    │   └── files/           # Uploaded files (by UUID)
    ├── logs/                # Execution logs
    └── reports/             # Execution reports
```

---

## Features

### Human-in-the-Loop (Hold Node)

Pause workflow execution for manual approval:

```yaml
approval:
  type: "hold"
  next: ["deploy"]
  on_failure: ["notify_rejection"]
```

Approve via API:
```bash
curl -X POST /api/v1/executions/{id}/nodes/approval/approve \
  -d '{"action": "approve"}'
```

### Global File Library

Upload files for use across workflows:

```bash
curl -X POST /api/v1/files \
  -F "file=@data.csv" \
  -F "file_id=sales_data_2024"
```

Reference in workflow:
```yaml
load_file:
  type: "file"
  config:
    file_id: "sales_data_2024"
```

### Execution Artifacts

Files created during execution are stored as artifacts:

```yaml
save_report:
  type: "shell"
  config:
    script: "echo 'Report' > $TOFI_ARTIFACTS_DIR/report.txt"
```

### Component Versioning

Call versioned workflow components:

```yaml
call_component:
  type: "workflow"
  config:
    uses: "tofi/ai_response@v2"  # Version syntax
```

---

## Multi-tenancy

- Each user has isolated storage namespace
- JWT tokens carry user identity
- All operations are scoped to authenticated user
- Admin endpoints for cross-user management

---

## Timeout Control

### Node-level Timeout

```yaml
slow_task:
  type: "shell"
  config:
    script: "sleep 100"
  timeout: 30  # Fail after 30 seconds
```

### Workflow-level Timeout

```yaml
timeout: 300  # Entire workflow must complete in 5 minutes

nodes:
  # ...
```

---

## Error Handling

### On Failure Path

```yaml
risky_task:
  type: "api"
  config:
    url: "https://unstable-api.com"
  next: ["success_path"]
  on_failure: ["error_handler"]

error_handler:
  type: "shell"
  config:
    script: "echo 'Task failed, notifying...'"
```

### Retry Configuration

```yaml
flaky_api:
  type: "api"
  config:
    url: "https://api.example.com"
  retry_count: 3
```

---

## Environment Variables

Shell nodes automatically receive:

| Variable | Description |
|----------|-------------|
| `TOFI_ARTIFACTS_DIR` | Path to execution artifacts directory |
| `TOFI_EXECUTION_ID` | Current execution ID |

Custom environment variables:

```yaml
build:
  type: "shell"
  env:
    NODE_ENV: "production"
    API_KEY: "{{secrets.api_key}}"
  config:
    script: "npm run build"
```

---

## Worker Pool

Configurable concurrent execution limit:

```bash
./tofi server -workers 10  # Max 10 concurrent workflows (default)
```

**Defaults:**
| Parameter | Default | Description |
|-----------|---------|-------------|
| `-workers` | 10 | Maximum concurrent workflows |
| Queue buffer | 100 | Pending workflow capacity |

Monitor via stats endpoint:
```bash
curl /api/v1/stats
```

Response:
```json
{
  "running": 3,
  "queued": 5,
  "max_workers": 10
}
```

---

## Zombie Recovery

On server restart, incomplete executions are automatically resumed:

1. Scans for `RUNNING` status executions
2. Resumes execution from last checkpoint
3. Maintains execution state consistency

---

## Quick Start

1. **Build**
   ```bash
   cd tofi-core
   go build -o tofi ./cmd/tofi
   ```

2. **Initialize**
   ```bash
   ./tofi server -port 8080 -home .tofi
   # First run creates admin setup endpoint
   ```

3. **Setup Admin**
   ```bash
   curl -X POST http://localhost:8080/api/v1/auth/setup \
     -H "Content-Type: application/json" \
     -d '{"username": "admin", "password": "secure_password"}'
   ```

4. **Login**
   ```bash
   curl -X POST http://localhost:8080/api/v1/auth/login \
     -H "Content-Type: application/json" \
     -d '{"username": "admin", "password": "secure_password"}'
   # Returns JWT token
   ```

5. **Run Workflow**
   ```bash
   curl -X POST http://localhost:8080/api/v1/run \
     -H "Authorization: Bearer <token>" \
     -H "Content-Type: application/json" \
     -d '{"workflow_id": "demo_basic"}'
   ```

---

## Related Documentation

- [NODE_REFERENCE.md](./NODE_REFERENCE.md) - Complete node type reference
- [API.md](./API.md) - API endpoint details
- [TIMEOUT_GUIDE.md](./TIMEOUT_GUIDE.md) - Timeout configuration
- [SECRET_STORE_GUIDE.md](./SECRET_STORE_GUIDE.md) - Secret management
