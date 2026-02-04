# Shell Node

> Execute bash scripts with environment variable injection.

**Type:** `shell`
**Category:** Task
**Source:** `internal/engine/tasks/shell.go`

---

## Config

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `script` | string | Yes | Bash script content. Supports multi-line. |
| `env` | object | No | Custom environment variables as key-value pairs. |

## Auto-Injected Environment Variables

| Variable | Description |
|----------|-------------|
| `TOFI_ARTIFACTS_DIR` | Directory path for writing output files |
| `TOFI_EXECUTION_ID` | Current execution ID |

These are always available in addition to any user-defined `env` variables.

## Output

Standard output (stdout) of the script, trimmed.

## Timeout

60 seconds (hardcoded). The script is killed if it exceeds this limit.

## Important Notes

- Scripts do **not** support `{{}}` template syntax directly. Use `env` to pass node references as environment variables.
- Supports context cancellation (workflow abort).

## Examples

```yaml
# Simple script
build:
  type: shell
  config:
    script: |
      npm install
      npm run build
      echo "Build complete"

# With environment variables
deploy:
  type: shell
  config:
    script: |
      echo "Deploying to $DEPLOY_TARGET"
      echo "Using API key: $API_KEY"
      echo "Artifacts at: $TOFI_ARTIFACTS_DIR"
    env:
      DEPLOY_TARGET: production
      API_KEY: "{{secrets.deploy_key}}"

# Writing output files
generate_report:
  type: shell
  config:
    script: |
      echo "# Report" > $TOFI_ARTIFACTS_DIR/report.md
      echo "Generated at $(date)" >> $TOFI_ARTIFACTS_DIR/report.md
```

## Errors

| Condition | Error |
|-----------|-------|
| `script` missing | Validation error |
| Script timeout (>60s) | Process killed |
| Non-zero exit code | Error with stderr output |
