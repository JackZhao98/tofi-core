# Tofi 节点配置速查手册

> 快速参考：所有节点类型的配置字段说明

---

## 节点通用字段

所有节点都支持以下字段：

```yaml
node_id:                           # 节点唯一标识符（必需）
  type: "节点类型"                  # 节点类型（必需），见下方类型列表
  config:                          # 配置参数（必需），键值对形式
    key: "value"

  # 以下字段均为可选
  next: ["下一个节点"]              # 成功后执行的节点列表
  dependencies: ["依赖节点"]        # 前置依赖节点列表
  retry_count: 3                   # 失败重试次数，默认 0
  timeout: 60                      # 超时秒数，默认 30
  on_failure: ["失败处理节点"]      # 失败时执行的节点列表
```

---

## 任务类节点（Tasks）

### 1. shell - Shell 命令执行

```yaml
node_id:
  type: "shell"
  config:
    script: "要执行的命令"          # 必需，支持变量替换和多行脚本
```

**示例**：
```yaml
run_build:
  type: "shell"
  config:
    script: "npm run build"
```

---

### 2. ai - AI 大模型调用

```yaml
node_id:
  type: "ai"
  config:
    provider: "openai"             # 可选，厂商：openai|claude|gemini|ollama，默认 openai
    endpoint: "API端点URL"          # 必需
    api_key: "API密钥"              # 可选（Ollama不需要）
    model: "模型名称"                # 必需
    system: "系统提示词"             # 可选
    prompt: "用户提示词"             # 必需，支持变量替换
```

**厂商配置示例**：

```yaml
# OpenAI
openai_task:
  type: "ai"
  config:
    provider: "openai"
    endpoint: "https://api.openai.com/v1/chat/completions"
    api_key: "{{secrets.openai_key}}"
    model: "gpt-4o-mini"
    prompt: "翻译：{{text}}"

# Claude
claude_task:
  type: "ai"
  config:
    provider: "claude"
    endpoint: "https://api.anthropic.com/v1/messages"
    api_key: "{{secrets.anthropic_key}}"
    model: "claude-3-5-sonnet-20241022"
    prompt: "分析：{{data}}"

# Gemini
gemini_task:
  type: "ai"
  config:
    provider: "gemini"
    endpoint: "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash-exp:generateContent"
    api_key: "{{secrets.gemini_key}}"
    prompt: "总结：{{content}}"

# Ollama
ollama_task:
  type: "ai"
  config:
    endpoint: "http://localhost:11434/v1/chat/completions"
    model: "qwen2.5:latest"
    prompt: "解释：{{topic}}"
```

---

### 3. api - HTTP API 调用

```yaml
node_id:
  type: "api"
  config:
    method: "POST"                 # 可选，POST|GET，默认 POST
    url: "目标URL"                  # 必需，支持变量替换
    api_key: "认证令牌"             # 可选，会添加到 Authorization: Bearer header
    body: "请求体JSON字符串"         # 可选，支持变量替换
```

**示例**：
```yaml
webhook:
  type: "api"
  config:
    method: "POST"
    url: "https://hooks.slack.com/services/xxx"
    body: '{"text": "部署完成：{{result}}"}'

github_api:
  type: "api"
  config:
    url: "https://api.github.com/repos/user/repo/issues"
    api_key: "{{secrets.github_token}}"
    body: '{"title": "Issue title"}'
```

---

## 逻辑类节点（Logic）

### 4. if - 复杂表达式判断

```yaml
node_id:
  type: "if"
  config:
    if: "布尔表达式"                # 必需，支持运算符和函数
```

**支持的语法**：
- 比较：`==`, `!=`, `>`, `<`, `>=`, `<=`
- 逻辑：`&&`, `||`, `!`
- 函数：`contains(str, substr)`, `len(str)`

**变量注入**：直接使用节点ID，不需要 `{{}}`

**示例**：
```yaml
check_condition:
  type: "if"
  config:
    if: "contains(result, 'SUCCESS') && len(errors) == 0"
  next: ["success_flow"]
  on_failure: ["error_flow"]

check_score:
  type: "if"
  config:
    if: "score >= 60"
```

---

### 5. check - 简单值检查

```yaml
node_id:
  type: "check"
  config:
    value: "待检查的值"             # 必需，支持变量替换
    mode: "检查模式"                # 必需，见下方模式说明
```

**模式**：
- `is_true`：值为 `"true"` 或 `"1"`
- `is_false`：值为 `"false"` 或 `"0"`
- `is_empty`：值为空或仅含空白
- `exists`：值非空

**示例**：
```yaml
check_flag:
  type: "check"
  config:
    value: "{{feature_enabled}}"
    mode: "is_true"

check_errors:
  type: "check"
  config:
    value: "{{error_list}}"
    mode: "is_empty"
```

---

### 6. text - 文本模式匹配

```yaml
node_id:
  type: "text"
  config:
    target: "待检查的文本"          # 必需，支持变量替换
    mode: "匹配模式"                # 必需，见下方模式说明
    value: "匹配值"                 # 必需
```

**模式**：
- `contains`：包含子串
- `starts_with`：以指定字符串开头
- `matches`：正则表达式匹配

**示例**：
```yaml
check_output:
  type: "text"
  config:
    target: "{{command_output}}"
    mode: "contains"
    value: "SUCCESS"

check_email:
  type: "text"
  config:
    target: "{{email}}"
    mode: "matches"
    value: "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"
```

---

### 7. math - 数值比较

```yaml
node_id:
  type: "math"
  config:
    left: "左值"                   # 必需，数字字符串，支持变量替换
    right: "右值"                  # 必需，数字字符串，支持变量替换
    operator: "运算符"             # 必需，见下方运算符列表
```

**运算符**：`>`, `<`, `==`, `>=`, `<=`, `!=`

**示例**：
```yaml
check_threshold:
  type: "math"
  config:
    left: "{{cpu_usage}}"
    operator: ">"
    right: "80"

compare_values:
  type: "math"
  config:
    left: "{{score_a}}"
    operator: ">="
    right: "{{score_b}}"
```

---

### 8. list - 列表操作

```yaml
node_id:
  type: "list"
  config:
    list: "JSON数组字符串"          # 必需，支持变量替换
    mode: "操作模式"                # 必需，见下方模式说明
    value: "预期值"                 # 必需
```

**模式**：
- `length_is`：数组长度等于 value
- `contains`：数组包含 value（字符串匹配）

**示例**：
```yaml
check_length:
  type: "list"
  config:
    list: '["a", "b", "c"]'
    mode: "length_is"
    value: "3"

check_contains:
  type: "list"
  config:
    list: "{{tags}}"
    mode: "contains"
    value: "latest"
```

---

## 数据类节点（Data）

### 9. var / const - 变量定义

```yaml
node_id:
  type: "var"  # 或 "const"，两者等价
  config:
    # 单值模式
    value: "单个值"

    # 或字典模式
    key1: "value1"
    key2: "value2"
```

**示例**：
```yaml
# 单值
user_name:
  type: "var"
  config:
    value: "Alice"

# 字典
config:
  type: "var"
  config:
    api_url: "https://api.example.com"
    timeout: "30"
    env: "production"
```

**使用**：
- 单值：`{{user_name}}` 或 `{{user_name.value}}`
- 字典：`{{config.api_url}}`、`{{config.timeout}}`

---

### 10. secret - 机密存储

```yaml
node_id:
  type: "secret"
  config:
    # 配置方式与 var 相同
    key1: "敏感值1"
    key2: "敏感值2"
```

**特性**：
- 配置方式与 `var` 完全相同
- 所有值会自动在日志中脱敏（显示为 `********`）
- 必须在使用密钥的节点**之前**执行（通过 `dependencies` 控制）

**示例**：
```yaml
secrets:
  type: "secret"
  config:
    github_token: "ghp_xxxxxxxxxxxx"
    api_key: "sk-proj-xxxxxxxxxxxx"
  next: ["use_secrets"]

use_secrets:
  type: "shell"
  config:
    script: "git clone https://{{secrets.github_token}}@github.com/user/repo.git"
  dependencies: ["secrets"]
```

---

## 基础类节点（Base）

### 11. virtual - 虚拟占位节点

```yaml
node_id:
  type: "virtual"
  # config 可省略，不执行任何操作
```

**用途**：
- 多分支汇聚点（等待多个并发分支完成）
- 流程占位符
- 逻辑分组

**示例**：
```yaml
# 汇聚点
merge:
  type: "virtual"
  dependencies: ["task_a", "task_b", "task_c"]
  next: ["final_step"]

# 也可以省略 type（默认为 virtual）
sync_point:
  dependencies: ["step1", "step2"]
  next: ["step3"]
```

---

## 变量替换语法

在任何 `config` 字段中都可以使用变量替换：

```yaml
# 基础替换
{{nodeID}}                  # 获取节点完整输出

# JSON 路径提取
{{nodeID.field}}            # 提取 JSON 字段
{{nodeID.a.b.c}}            # 嵌套路径
```

**示例**：
```yaml
get_data:
  type: "shell"
  config:
    script: 'echo ''{"user": {"name": "Alice", "age": 30}}'''

use_data:
  type: "shell"
  config:
    script: "echo 'Name: {{get_data.user.name}}, Age: {{get_data.user.age}}'"
  dependencies: ["get_data"]
```

---

## 快速参考表

### 节点类型总览

| 类型 | 分类 | 用途 | 关键字段 |
|------|------|------|---------|
| `shell` | 任务 | 执行命令 | `script` |
| `ai` | 任务 | AI调用 | `provider`, `endpoint`, `api_key`, `model`, `prompt` |
| `api` | 任务 | HTTP请求 | `method`, `url`, `api_key`, `body` |
| `if` | 逻辑 | 表达式判断 | `if` |
| `check` | 逻辑 | 值检查 | `value`, `mode` |
| `text` | 逻辑 | 文本匹配 | `target`, `mode`, `value` |
| `math` | 逻辑 | 数值比较 | `left`, `operator`, `right` |
| `list` | 逻辑 | 列表操作 | `list`, `mode`, `value` |
| `var`/`const` | 数据 | 变量定义 | `value` 或任意键值对 |
| `secret` | 数据 | 密钥存储 | 同 `var`（自动脱敏） |
| `virtual` | 基础 | 占位节点 | 无 |

### 默认超时时间

| 节点类型 | 默认超时 |
|---------|---------|
| `shell` | 30秒 |
| `ai` | 60秒 |
| `api` | 30秒 |
| 逻辑节点 | 无超时 |

### If 节点支持的运算符和函数

**运算符**：
- 比较：`==`, `!=`, `>`, `<`, `>=`, `<=`
- 逻辑：`&&`, `||`, `!`

**函数**：
- `contains(str, substr)`：字符串包含判断
- `len(str)`：字符串长度

---

## 完整示例

```yaml
nodes:
  # 1. 定义配置和密钥
  config:
    type: "var"
    config:
      api_url: "https://api.example.com"
      threshold: "80"
    next: ["secrets"]

  secrets:
    type: "secret"
    config:
      api_key: "sk-xxxxxxxxxxxx"
    dependencies: ["config"]
    next: ["fetch_data"]

  # 2. 获取数据
  fetch_data:
    type: "api"
    config:
      url: "{{config.api_url}}/data"
      api_key: "{{secrets.api_key}}"
    dependencies: ["secrets"]
    retry_count: 3
    timeout: 60
    next: ["validate_data"]

  # 3. 数据验证
  validate_data:
    type: "check"
    config:
      value: "{{fetch_data}}"
      mode: "exists"
    dependencies: ["fetch_data"]
    next: ["process_data"]
    on_failure: ["handle_error"]

  # 4. 并发处理
  process_data:
    type: "shell"
    config:
      script: "echo 'Processing...'"
    dependencies: ["validate_data"]
    next: ["ai_analyze", "upload_raw"]

  ai_analyze:
    type: "ai"
    config:
      endpoint: "https://api.openai.com/v1/chat/completions"
      api_key: "{{secrets.api_key}}"
      model: "gpt-4o-mini"
      prompt: "分析数据：{{fetch_data}}"
    dependencies: ["process_data"]
    next: ["check_result"]

  upload_raw:
    type: "shell"
    config:
      script: "aws s3 cp data.json s3://bucket/raw/"
    dependencies: ["process_data"]

  # 5. 结果检查
  check_result:
    type: "if"
    config:
      if: "contains(ai_analyze, '合格')"
    dependencies: ["ai_analyze"]
    next: ["notify_success"]
    on_failure: ["notify_fail"]

  # 6. 通知
  notify_success:
    type: "api"
    config:
      url: "https://hooks.slack.com/services/xxx"
      body: '{"text": "✅ 处理成功"}'

  notify_fail:
    type: "api"
    config:
      url: "https://hooks.slack.com/services/xxx"
      body: '{"text": "❌ 检查失败"}'

  handle_error:
    type: "shell"
    config:
      script: "echo 'Error: No data received'"
```

---

**更多详细信息和最佳实践，请参考：[workflow-guide.md](workflow-guide.md)**
