# AI Node

> Call LLM APIs for text generation. Supports OpenAI, Anthropic, Google Gemini, and any OpenAI-compatible endpoint.

**Type:** `ai`
**Category:** Task
**Source:** `internal/engine/tasks/ai.go`

---

## Config

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `model` | string | Yes | — | Model name or `"openai-compatible"` |
| `prompt` | string | Yes | — | User prompt. Supports `{{}}` references. |
| `system` | string | No | `""` | System prompt |
| `use_system_key` | bool | No | `false` | Use system-configured API key from env vars |
| `api_key` | string | Conditional | — | Required if `use_system_key` is false |
| `endpoint` | string | Conditional | — | Required if `model` is `"openai-compatible"` |
| `mcp_servers` | string[] | No | — | MCP server IDs to enable agent mode |

## Provider Detection

The provider is auto-detected from the model name:

| Model Pattern | Provider | Default Endpoint |
|---------------|----------|------------------|
| `claude*` | Anthropic | `https://api.anthropic.com/v1` |
| `gemini*` | Google Gemini | `https://generativelanguage.googleapis.com/v1beta` |
| `gpt-*`, `o1-*`, `o3-*` | OpenAI (Chat Completions) | `https://api.openai.com/v1` |
| `gpt-5*` | OpenAI (Responses API) | `https://api.openai.com/v1` |
| `openai-compatible` | Custom | User-provided `endpoint` |

## System Key Environment Variables

When `use_system_key: true`:

| Provider | Env Var |
|----------|---------|
| OpenAI | `TOFI_OPENAI_API_KEY` |
| Anthropic / Claude | `TOFI_ANTHROPIC_API_KEY` |
| Gemini | `TOFI_GEMINI_API_KEY` |

## Output

Generated text response (string).

Response is extracted from the first matching path:
1. `output[type=="message"].content[0].text` (OpenAI Responses)
2. `choices[0].message.content` (OpenAI Chat)
3. `candidates[0].content.parts[0].text` (Gemini)
4. `content[0].text` (Anthropic)

## Examples

```yaml
# Standard OpenAI
summarize:
  type: ai
  config:
    model: gpt-4o
    use_system_key: true
    system: You are a helpful assistant.
    prompt: "Summarize: {{fetch_content}}"

# Anthropic Claude
analyze:
  type: ai
  config:
    model: claude-sonnet-4-20250514
    api_key: "{{secrets.anthropic_key}}"
    prompt: "Analyze: {{data_input}}"

# OpenAI-compatible (Ollama, vLLM, etc.)
local_llm:
  type: ai
  config:
    model: openai-compatible
    endpoint: "http://localhost:11434/v1/chat/completions"
    prompt: "Explain quantum computing"

# Agent mode with MCP servers
research_agent:
  type: ai
  config:
    model: gpt-4o
    use_system_key: true
    mcp_servers: ["web_search", "calculator"]
    system: You are a research assistant with tools.
    prompt: "Research the latest AI news"
```

## Errors

| Condition | Error |
|-----------|-------|
| `model` missing | `config.model is required` |
| `model=openai-compatible` without `endpoint` | `config.endpoint is required` |
| `prompt` empty | `AI prompt cannot be empty` |
| System key not in env | `system API key not configured` |
| Response parse failure | `AI response parsing failed` |
