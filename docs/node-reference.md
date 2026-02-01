# Tofi 节点配置速查手册

> 快速参考：所有节点类型的配置字段说明 (v2.0 规范)

---

## 节点通用字段

所有节点都支持以下字段：

```yaml
node_id:                           # 节点唯一标识符（必需）
  type: "节点类型"                  # 节点类型（必需）
  
  # 1. 行为配置 (Static)
  config:
    timeout: 30                    # 超时秒数
    retry_count: 3                 # 重试次数

  # 2. 动态输入 (Dynamic)
  input:
    key: "value"                   # 业务参数，支持变量替换 {{...}}

  # 3. 环境变量 (Process)
  env:
    ENV_VAR: "value"               # 注入到 Shell 进程的环境变量

  # 4. 静态数据 (Data Only)
  data:                            # 仅限 Var/Secret/Const 节点使用
    key: "value"

  # 5. 流程控制
  next: ["下一个节点"]              # 成功后执行的节点列表
  dependencies: ["依赖节点"]        # 前置依赖节点列表
  on_failure: ["失败处理节点"]      # 失败时执行的节点列表
```

---

## 任务类节点（Tasks）

### 1. shell - Shell 命令执行

执行 Shell 脚本，支持环境变量注入。

```yaml
run_build:
  type: "shell"
  env:
    NODE_ENV: "production"
    API_KEY: "{{secrets.api_key}}" # 安全注入
  input:
    script: "npm run build -- --key=$API_KEY"
  config:
    timeout: 300
```

### 2. ai - AI 大模型调用

调用 LLM 生成内容。

```yaml
translate_task:
  type: "ai"
  config:
    provider: "openai"             # openai | claude | gemini
    model: "gpt-4o"
    endpoint: "https://api.openai.com/v1/chat/completions"
    api_key: "{{secrets.openai_key}}"
  input:
    system: "你是一个翻译助手。"
    prompt: "请翻译这段话：{{text}}"
```

### 3. api - HTTP API 调用

发送 HTTP 请求。

```yaml
webhook_trigger:
  type: "api"
  config:
    method: "POST"
    url: "https://hooks.slack.com/services/xxx"
    api_key: "{{secrets.slack_token}}"
  input:
    body: '{"text": "部署完成"}'
```

### 4. workflow - 子工作流调用 (Handoff)

调用另一个 YAML 工作流文件，并传递参数。

```yaml
call_subprocess:
  type: "workflow"
  config:
    file: "./workflows/process_data.yaml"
  input:
    # 这里的参数会自动注入到子工作流中，可通过 {{inputs.user_id}} 访问
    user_id: "{{user.id}}"
    environment: "staging"
```

---

## 逻辑类节点（Logic）

### 5. if - 复杂表达式判断

使用表达式进行逻辑分支判断。

```yaml
check_condition:
  type: "if"
  input:
    if: "score > 60 && status == 'active'" # 直接写变量名，无需 {{}}
  next: ["success_flow"]
  on_failure: ["reject_flow"]
```

### 6. hold - 人工审批 (Human-in-the-Loop)

暂停工作流执行，等待外部人工审批（Approve/Reject）。
通常配合 UI 或 API 使用。

```yaml
wait_for_approval:
  type: "hold"
  input:
    # 传递给审批人的上下文数据
    request_id: "{{request.id}}"
    summary: "请审批部署请求"
  next: ["deploy"]
  on_failure: ["notify_rejection"]
```

### 7. check - 简单值检查

检查单个值的状态。

```yaml
is_enabled:
  type: "check"
  config:
    mode: "is_true"                # is_true | is_false | is_empty | exists
  input:
    value: "{{feature_flag}}"
```

### 8. text - 文本模式匹配

字符串包含、前缀匹配或正则匹配。

```yaml
validate_email:
  type: "text"
  config:
    mode: "matches"                # contains | starts_with | matches
  input:
    target: "{{email}}"
    value: "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"
```

### 9. math - 数值比较

数值大小比较。

```yaml
check_cpu:
  type: "math"
  config:
    operator: ">"                  # > | < | == | >= | <= | !=
  input:
    left: "{{cpu_usage}}"
    right: "80"
```

### 10. list - 列表操作

JSON 列表长度检查或包含检查。

```yaml
check_tags:
  type: "list"
  config:
    mode: "contains"               # length_is | contains
  input:
    list: "{{tags_json}}"          # 必须是 JSON 数组字符串
    value: "urgent"
```

---

## 数据类节点（Data）

### 11. var / const - 变量定义

定义流程中使用的静态数据。

```yaml
app_config:
  type: "var"
  data:
    app_name: "Tofi App"
    version: "1.0.0"
    max_retries: "5"
```

**使用方式**：`{{app_config.app_name}}`

### 12. secret - 机密存储

定义敏感数据，输出日志会自动脱敏（显示为 `********`）。

```yaml
secrets:
  type: "secret"
  data:
    github_token: "ghp_xxxxxxxx"
    db_password: "password123"
```

---

## 基础类节点（Base）

### 13. virtual - 虚拟占位节点

用于逻辑分组或作为汇聚点（等待多个并发分支完成）。

```yaml
join_point:
  type: "virtual"
  dependencies: ["branch_a", "branch_b"]
  next: ["final_step"]
```

```