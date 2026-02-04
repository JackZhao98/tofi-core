# Secret Node

> Define sensitive values with automatic log masking. Values are never exposed in logs.

**Type:** `secret`
**Category:** Data
**Source:** `internal/engine/data/secret.go`

---

## Config

Key-value pairs where each key is a secret name and each value is a string:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `<key>` | string | Yes | Secret name → value expression |

## Value Formats

| Format | Description | Example |
|--------|-------------|---------|
| `env.VAR_NAME` | Read from environment variable | `env.OPENAI_API_KEY` |
| `{{env.VAR_NAME}}` | Template syntax for env var | `{{env.DB_PASSWORD}}` |
| literal string | Direct value | `sk-xxx-literal-key` |

## Output

JSON object with resolved secret values:
```json
{"openai_key": "sk-abc123", "db_password": "mypassword"}
```

## Log Masking

All non-empty secret values are automatically registered for masking. Any occurrence of a secret value in logs is replaced with `****`.

## Usage

Downstream nodes reference secrets via `{{secrets_node.key_name}}`:

```yaml
api_secrets:
  type: secret
  config:
    openai_key: "env.OPENAI_API_KEY"
    db_password: "{{env.DB_PASSWORD}}"
    static_token: "sk-xxx-literal"

call_ai:
  type: ai
  config:
    api_key: "{{api_secrets.openai_key}}"
    model: gpt-4o
    prompt: "Hello"
```

## Frontend Serialization

The frontend serializer converts `{{secrets.KEY}}` references in node configs to `ref:KEY` format in YAML. The engine resolves `ref:KEY` by looking up the encrypted secret in the database and decrypting it at runtime.

## Errors

| Condition | Error |
|-----------|-------|
| Value is not a string | `secret node config values must be strings` |
| Config is empty | `secret node requires config` |
| Serialization failure | `failed to marshal secrets` |
