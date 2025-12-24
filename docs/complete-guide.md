# Tofi 工作流引擎完整指南

> 基于触发器的灵活工作流编排系统 - Trigger-based Operations with Flexible Interface

---

## 目录

- [1. 快速开始](#1-快速开始)
- [2. 核心概念](#2-核心概念)
- [3. 节点类型完整参考](#3-节点类型完整参考)
  - [3.1 任务类节点 (Tasks)](#31-任务类节点-tasks)
  - [3.2 逻辑类节点 (Logic)](#32-逻辑类节点-logic)
  - [3.3 数据类节点 (Data)](#33-数据类节点-data)
  - [3.4 基础类节点 (Base)](#34-基础类节点-base)
- [4. 高级特性](#4-高级特性)
- [5. 最佳实践](#5-最佳实践)
- [6. 完整示例](#6-完整示例)
- [7. 故障排查](#7-故障排查)

---

## 1. 快速开始

### 1.1 什么是 Tofi 工作流？

Tofi 工作流引擎是一个基于 YAML 配置的任务编排系统，支持：

- ✅ **并发执行**：基于依赖关系自动并发
- ✅ **错误处理**：自动重试、失败分支、错误传播
- ✅ **灵活集成**：Shell、AI、API 等多种任务类型
- ✅ **安全特性**：密钥自动脱敏、超时控制
- ✅ **逻辑控制**：条件判断、文本匹配、数值比较

### 1.2 第一个工作流

创建 `hello.yaml`：

```yaml
nodes:
  # 节点1: 打印问候
  greet:
    type: "shell"
    config:
      script: "echo 'Hello, Tofi!'"
    next: ["finish"]

  # 节点2: 完成标记
  finish:
    type: "shell"
    config:
      script: "echo 'Workflow completed!'"
    dependencies: ["greet"]
```

执行：
```bash
./tofi-core -workflow hello.yaml
```

输出：
```
[exec-xxx] [START]   [greet] 类型: shell
[exec-xxx] [SUCCESS] [greet] 输出: Hello, Tofi!
[exec-xxx] [START]   [finish] 类型: shell
[exec-xxx] [SUCCESS] [finish] 输出: Workflow completed!
```

### 1.3 YAML 结构说明

```yaml
nodes:                    # 必需：所有节点的容器
  node_id:               # 节点唯一标识符（任意字符串）
    type: "节点类型"      # 必需：见第3章节点类型列表
    config:              # 必需：节点配置参数（键值对）
      param1: "value1"
      param2: "value2"

    next: ["下一个节点"]  # 可选：成功后执行的节点ID列表
    dependencies: ["依赖节点"]  # 可选：前置依赖节点ID列表
    retry_count: 3       # 可选：失败重试次数，默认0
    timeout: 60          # 可选：超时秒数，默认30
    on_failure: ["失败处理节点"]  # 可选：失败时执行的节点ID列表
```

---

## 2. 核心概念

### 2.1 节点（Node）

节点是工作流的最小执行单元，每个节点包含：

| 字段 | 类型 | 必需 | 说明 |
|------|------|------|------|
| `type` | string | ✅ | 节点类型（如 `shell`、`ai`、`if` 等） |
| `config` | map[string]string | ✅ | 配置参数（键值对） |
| `next` | []string | ❌ | 成功后执行的节点ID列表（支持多个） |
| `dependencies` | []string | ❌ | 必须先完成的节点ID列表 |
| `retry_count` | int | ❌ | 失败重试次数（默认0） |
| `timeout` | int | ❌ | 超时秒数（默认30秒） |
| `on_failure` | []string | ❌ | 失败时执行的节点ID列表 |

### 2.2 执行顺序与依赖

#### 并发执行规则

```yaml
nodes:
  start:
    type: "shell"
    config:
      script: "echo 'Starting...'"
    next: ["task_a", "task_b"]  # task_a 和 task_b 会并发执行

  task_a:
    type: "shell"
    config:
      script: "sleep 2 && echo 'A done'"
    dependencies: ["start"]
    next: ["merge"]

  task_b:
    type: "shell"
    config:
      script: "sleep 1 && echo 'B done'"
    dependencies: ["start"]
    next: ["merge"]

  merge:
    type: "shell"
    config:
      script: "echo 'All tasks completed'"
    dependencies: ["task_a", "task_b"]  # 等待两个任务都完成
```

**执行流程**：
1. `start` 首先执行
2. `task_a` 和 `task_b` 同时启动（并发）
3. `merge` 等待 `task_a` 和 `task_b` **都完成**后才执行

#### 扇出/扇入模式（Fan-out/Fan-in）

```
    start
     /  \
   task_a  task_b   ← 扇出（Fan-out）：并发执行
     \  /
    merge            ← 扇入（Fan-in）：等待所有完成
```

### 2.3 变量系统

#### 基础变量替换

```yaml
nodes:
  set_name:
    type: "var"
    config:
      user_name: "Alice"
    next: ["greet"]

  greet:
    type: "shell"
    config:
      script: "echo 'Hello, {{set_name.user_name}}!'"  # 输出: Hello, Alice!
    dependencies: ["set_name"]
```

**语法规则**：
- `{{nodeID}}`：获取节点的完整输出
- `{{nodeID.field}}`：提取 JSON 字段（支持嵌套路径）

#### JSON 路径提取

```yaml
nodes:
  get_data:
    type: "shell"
    config:
      script: 'echo ''{"user": {"name": "Bob", "age": 30}}'''
    next: ["use_data"]

  use_data:
    type: "shell"
    config:
      # 提取嵌套字段
      script: "echo 'Name: {{get_data.user.name}}, Age: {{get_data.user.age}}'"
    dependencies: ["get_data"]
```

#### 变量作用域

```yaml
nodes:
  # 全局配置变量
  config:
    type: "var"
    config:
      api_endpoint: "https://api.example.com"
      max_retries: "3"
    next: ["task1", "task2"]

  # 多个任务共享配置
  task1:
    type: "api"
    config:
      url: "{{config.api_endpoint}}/users"
    dependencies: ["config"]

  task2:
    type: "shell"
    config:
      script: "echo 'Max retries: {{config.max_retries}}'"
    dependencies: ["config"]
```

### 2.4 错误处理

#### 2.4.1 重试机制

```yaml
nodes:
  unstable_task:
    type: "shell"
    config:
      script: "curl https://flaky-api.com/data"
    retry_count: 3  # 失败后最多重试3次
    timeout: 10     # 每次尝试超时10秒
```

**日志输出**：
```
[exec-xxx] [START]   [unstable_task] 类型: shell
[exec-xxx] [ERROR]   [unstable_task] 执行失败: exit status 1
[exec-xxx] [RETRY]   [unstable_task] 第 1 次重试...
[exec-xxx] [SUCCESS] [unstable_task] 输出: {"data": "..."}
```

#### 2.4.2 失败分支（on_failure）

```yaml
nodes:
  deploy:
    type: "shell"
    config:
      script: "./deploy.sh production"
    retry_count: 2
    on_failure: ["rollback", "notify_team"]  # 失败后执行这些节点
    next: ["celebrate"]                       # 成功后执行这个节点

  rollback:
    type: "shell"
    config:
      script: "./rollback.sh"

  notify_team:
    type: "api"
    config:
      url: "https://hooks.slack.com/services/xxx"
      body: '{"text": "部署失败，已回滚"}'
    dependencies: ["rollback"]

  celebrate:
    type: "shell"
    config:
      script: "echo '部署成功！'"
```

**执行路径**：
- ✅ 成功：`deploy` → `celebrate`
- ❌ 失败：`deploy` → `rollback` → `notify_team`

#### 2.4.3 错误传播

当一个节点失败时，其所有下游节点会自动跳过：

```yaml
nodes:
  step1:
    type: "shell"
    config:
      script: "exit 1"  # 模拟失败
    next: ["step2"]

  step2:
    type: "shell"
    config:
      script: "echo 'This will be skipped'"
    dependencies: ["step1"]
    next: ["step3"]

  step3:
    type: "shell"
    config:
      script: "echo 'This will also be skipped'"
    dependencies: ["step2"]
```

**日志输出**：
```
[exec-xxx] [ERROR]   [step1] 执行失败: exit status 1
[exec-xxx] [SKIP]    [step2] 由于上游失败自动跳过
[exec-xxx] [SKIP]    [step3] 由于上游失败自动跳过
```

### 2.5 超时控制

```yaml
nodes:
  long_task:
    type: "shell"
    config:
      script: "sleep 100"  # 模拟长时间任务
    timeout: 5  # 5秒后强制终止
```

**行为**：
- 超时后自动 Kill 子进程
- 触发 `on_failure` 分支（如果配置了）
- 下游节点被自动跳过

---

## 3. 节点类型完整参考

### 3.1 任务类节点 (Tasks)

#### 3.1.1 Shell - 命令执行器

**用途**：执行 Shell 脚本命令

**配置参数**：

| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| `script` | string | ✅ | 要执行的命令（支持变量替换） |

**示例**：

```yaml
# 基础用法
simple_command:
  type: "shell"
  config:
    script: "ls -la"

# 多行脚本
setup_env:
  type: "shell"
  config:
    script: |
      mkdir -p /tmp/workspace
      cd /tmp/workspace
      git clone https://github.com/user/repo.git
      echo "Setup complete"

# 变量注入
dynamic_command:
  type: "shell"
  config:
    script: "echo 'Processing: {{input_data}}' && ./process.sh {{config.target}}"
  dependencies: ["input_data", "config"]
```

**输出**：命令的 `stdout + stderr` 组合输出（自动 trim 空白）

**注意事项**：
- 底层使用 `sh -c` 执行
- 非零退出码视为失败
- 支持管道、重定向等 Shell 特性
- 默认超时30秒（可通过 `timeout` 字段覆盖）

---

#### 3.1.2 AI - 大语言模型调用

**用途**：调用 OpenAI、Claude、Gemini 或 Ollama 进行 AI 推理

**配置参数**：

| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| `provider` | string | ❌ | AI 厂商：`openai`（默认）、`claude`、`gemini`、`ollama` |
| `endpoint` | string | ✅ | API 端点 URL |
| `api_key` | string | ❌ | API 密钥（Ollama 不需要） |
| `model` | string | ✅ | 模型名称（如 `gpt-4o`、`claude-3-5-sonnet-20241022`） |
| `system` | string | ❌ | 系统提示词 |
| `prompt` | string | ✅ | 用户提示词（支持变量替换） |

**厂商配置对照表**：

| 厂商 | provider 值 | 认证方式 | 示例 endpoint | 示例 model |
|------|------------|---------|--------------|-----------|
| OpenAI | `openai` | `api_key` | `https://api.openai.com/v1/chat/completions` | `gpt-4o`, `gpt-4o-mini` |
| Claude | `claude` | `api_key` | `https://api.anthropic.com/v1/messages` | `claude-3-5-sonnet-20241022` |
| Gemini | `gemini` | `api_key` | `https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash-exp:generateContent` | 省略（URL中已包含） |
| Ollama | 省略或 `openai` | 不需要 | `http://localhost:11434/v1/chat/completions` | `qwen2.5:latest` |

**示例**：

```yaml
# OpenAI 示例
openai_translate:
  type: "ai"
  config:
    provider: "openai"
    endpoint: "https://api.openai.com/v1/chat/completions"
    api_key: "{{secrets.openai_key}}"
    model: "gpt-4o-mini"
    system: "你是一个专业的翻译助手。"
    prompt: "请将以下文本翻译成中文：\n{{input_text}}"
  dependencies: ["secrets", "input_text"]

# Claude 示例
claude_review:
  type: "ai"
  config:
    provider: "claude"
    endpoint: "https://api.anthropic.com/v1/messages"
    api_key: "{{secrets.anthropic_key}}"
    model: "claude-3-5-sonnet-20241022"
    system: "你是一个代码审查专家。"
    prompt: "请审查以下代码并给出改进建议：\n{{code_content}}"

# Gemini 示例
gemini_analyze:
  type: "ai"
  config:
    provider: "gemini"
    endpoint: "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash-exp:generateContent"
    api_key: "{{secrets.gemini_key}}"
    prompt: "分析这段日志：{{log_output}}"

# Ollama 本地模型示例
ollama_chat:
  type: "ai"
  config:
    endpoint: "http://localhost:11434/v1/chat/completions"
    model: "qwen2.5:latest"
    prompt: "解释什么是工作流引擎"
```

**输出**：AI 模型返回的文本内容（自动提取 `content` 字段）

**注意事项**：
- OpenAI 格式最通用（Ollama 也兼容）
- Claude 需要额外的 `anthropic-version: 2023-06-01` Header（自动添加）
- Gemini 的请求格式与其他厂商不同（自动适配）
- API 密钥建议使用 `secret` 节点存储（自动脱敏）
- 默认超时60秒

---

#### 3.1.3 API - HTTP 调用

**用途**：发送 HTTP 请求到外部 API

**配置参数**：

| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| `method` | string | ❌ | HTTP 方法（`POST` 或 `GET`，默认 `POST`） |
| `url` | string | ✅ | 目标 URL |
| `api_key` | string | ❌ | 认证令牌（会自动添加到 `Authorization: Bearer` Header） |
| `body` | string | ❌ | 请求体（JSON 字符串，支持变量替换） |

**示例**：

```yaml
# Webhook 通知
slack_notify:
  type: "api"
  config:
    method: "POST"
    url: "https://hooks.slack.com/services/T00/B00/XXX"
    body: |
      {
        "text": "部署完成",
        "blocks": [
          {
            "type": "section",
            "text": {
              "type": "mrkdwn",
              "text": "🚀 部署结果: {{deploy_result}}"
            }
          }
        ]
      }

# 带认证的 API 调用
github_api:
  type: "api"
  config:
    url: "https://api.github.com/repos/user/repo/issues"
    api_key: "{{secrets.github_token}}"
    body: |
      {
        "title": "Automated Issue",
        "body": "Issue created by Tofi workflow"
      }

# 动态 URL
fetch_user:
  type: "api"
  config:
    method: "GET"
    url: "https://api.example.com/users/{{user_id}}"
    api_key: "{{secrets.api_key}}"
```

**输出**：HTTP 响应体（原始字符串）

**注意事项**：
- 非 200 状态码会抛出错误
- `api_key` 会自动添加为 `Authorization: Bearer {api_key}` Header
- `Content-Type: application/json` 自动添加
- 支持变量替换（包括 URL 和 Body）

---

### 3.2 逻辑类节点 (Logic)

#### 3.2.1 If - 复杂表达式判断

**用途**：基于布尔表达式进行条件分支

**配置参数**：

| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| `if` | string | ✅ | 布尔表达式（支持运算符和函数） |

**支持的语法**：

| 类型 | 语法 | 示例 |
|------|------|------|
| 比较运算符 | `==`, `!=`, `>`, `<`, `>=`, `<=` | `count > 10` |
| 逻辑运算符 | `&&`, `||`, `!` | `success && !error` |
| 内置函数 | `contains(str, substr)` | `contains(result, 'PASS')` |
| 内置函数 | `len(str)` | `len(name) > 0` |

**变量注入规则**：
- 直接使用节点ID作为变量名（**不需要** `{{}}` 包裹）
- 示例：`contains(ai_result, '成功')` → `ai_result` 是节点ID

**示例**：

```yaml
# 基础条件判断
check_status:
  type: "if"
  config:
    if: "status_code == 200"
  dependencies: ["status_code"]
  next: ["process_success"]
  on_failure: ["handle_error"]

# 复杂逻辑组合
validate_result:
  type: "if"
  config:
    if: "contains(ai_response, '合格') && len(error_log) == 0"
  dependencies: ["ai_response", "error_log"]
  next: ["deploy"]
  on_failure: ["reject"]

# 数值范围检查
check_score:
  type: "if"
  config:
    if: "score >= 60 && score <= 100"
  dependencies: ["score"]

# 多条件或运算
check_keywords:
  type: "if"
  config:
    if: "contains(content, 'urgent') || contains(content, '紧急') || contains(content, 'critical')"
  dependencies: ["content"]
```

**输出**：
- 条件满足：返回空字符串，触发 `next` 分支
- 条件不满足：返回 `CONDITION_NOT_MET` 错误，触发 `on_failure` 分支

**注意事项**：
- 字符串比较区分大小写
- 所有变量值都会先进行参数替换（`{{nodeID}}`）
- 使用 [govaluate](https://github.com/Knetic/govaluate) 库进行安全求值
- 不支持自定义函数（仅支持 `contains` 和 `len`）

---

#### 3.2.2 Check - 简单值检查

**用途**：对单个值进行快速布尔判定

**配置参数**：

| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| `value` | string | ✅ | 待检查的值（支持变量替换） |
| `mode` | string | ✅ | 检查模式：`is_true`, `is_false`, `is_empty`, `exists` |

**模式说明**：

| 模式 | 判定条件 | 用途 |
|------|---------|------|
| `is_true` | 值为 `"true"` 或 `"1"` | 验证布尔标志 |
| `is_false` | 值为 `"false"` 或 `"0"` | 验证否定条件 |
| `is_empty` | 值为空字符串或仅含空白 | 检查是否为空 |
| `exists` | 值非空（Trim后长度>0） | 检查是否有值 |

**示例**：

```yaml
# 检查布尔标志
check_enabled:
  type: "check"
  config:
    value: "{{feature_flag}}"
    mode: "is_true"
  next: ["enable_feature"]
  on_failure: ["skip_feature"]

# 检查环境变量
check_env:
  type: "check"
  config:
    value: "{{env_var}}"
    mode: "exists"
  dependencies: ["env_var"]

# 验证空值
check_errors:
  type: "check"
  config:
    value: "{{error_list}}"
    mode: "is_empty"
  next: ["proceed"]
  on_failure: ["fix_errors"]

# 检查失败标志
check_failure:
  type: "check"
  config:
    value: "{{has_failed}}"
    mode: "is_false"
```

**输出**：成功时返回 `"CHECK_PASSED"`

**注意事项**：
- 值会先进行 `Trim()` 处理
- 大小写敏感（`"True"` 不等于 `"true"`）
- 条件不满足返回 `CONDITION_NOT_MET` 错误

---

#### 3.2.3 Text - 文本模式匹配

**用途**：对文本内容进行模式匹配判定

**配置参数**：

| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| `target` | string | ✅ | 待检查的文本（支持变量替换） |
| `mode` | string | ✅ | 匹配模式：`contains`, `starts_with`, `matches` |
| `value` | string | ✅ | 匹配模式值 |

**模式说明**：

| 模式 | 功能 | 示例 |
|------|------|------|
| `contains` | 包含子串（大小写敏感） | `target="Hello World"`, `value="World"` → ✅ |
| `starts_with` | 以指定字符串开头 | `target="Error: timeout"`, `value="Error"` → ✅ |
| `matches` | 正则表达式匹配 | `target="user@example.com"`, `value=".*@.*\\.com"` → ✅ |

**示例**：

```yaml
# 子串匹配
check_output:
  type: "text"
  config:
    target: "{{command_output}}"
    mode: "contains"
    value: "SUCCESS"
  next: ["continue"]
  on_failure: ["retry"]

# 前缀检查
check_error:
  type: "text"
  config:
    target: "{{log_line}}"
    mode: "starts_with"
    value: "ERROR"
  next: ["alert"]

# 正则匹配（邮箱验证）
validate_email:
  type: "text"
  config:
    target: "{{user_email}}"
    mode: "matches"
    value: "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"

# 检查环境
check_prod:
  type: "text"
  config:
    target: "{{deploy_env}}"
    mode: "contains"
    value: "production"
  next: ["extra_validation"]
  on_failure: ["direct_deploy"]
```

**输出**：
- 匹配成功：`"TEXT_MATCHED"`
- 匹配失败：`"TEXT_NOT_MATCH"` + `CONDITION_NOT_MET` 错误

**注意事项**：
- 所有模式都**区分大小写**
- 正则表达式使用 Go 的 `regexp` 包语法
- 正则中的反斜杠需要转义（如 `\\.` 匹配字面点）

---

#### 3.2.4 Math - 数值比较

**用途**：数值大小比较判定

**配置参数**：

| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| `left` | string | ✅ | 左值（数字字符串，支持变量替换） |
| `right` | string | ✅ | 右值（数字字符串，支持变量替换） |
| `operator` | string | ✅ | 比较运算符：`>`, `<`, `==`, `>=`, `<=`, `!=` |

**示例**：

```yaml
# 基础数值比较
check_threshold:
  type: "math"
  config:
    left: "{{cpu_usage}}"
    operator: ">"
    right: "80"
  next: ["scale_up"]
  on_failure: ["continue"]

# 比较两个节点的输出
compare_scores:
  type: "math"
  config:
    left: "{{score_a}}"
    operator: ">="
    right: "{{score_b}}"
  dependencies: ["score_a", "score_b"]

# 相等检查
check_count:
  type: "math"
  config:
    left: "{{item_count}}"
    operator: "=="
    right: "100"

# 范围检查（配合多个 math 节点）
lower_bound:
  type: "math"
  config:
    left: "{{temperature}}"
    operator: ">="
    right: "0"
  next: ["upper_bound"]

upper_bound:
  type: "math"
  config:
    left: "{{temperature}}"
    operator: "<="
    right: "100"
  dependencies: ["lower_bound"]
```

**输出**：成功时返回 `"MATH_PASSED"`

**注意事项**：
- 自动将字符串转换为 `float64`
- 转换失败会抛出错误（如 `"abc"` 无法转换）
- 支持整数和浮点数（如 `"3.14"`, `"42"`）
- 比较失败返回 `CONDITION_NOT_MET` 错误

---

#### 3.2.5 List - 列表操作

**用途**：对 JSON 数组进行判定

**配置参数**：

| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| `list` | string | ✅ | JSON 数组字符串（支持变量替换） |
| `mode` | string | ✅ | 操作模式：`length_is`, `contains` |
| `value` | string | ✅ | 预期值 |

**模式说明**：

| 模式 | 功能 | 示例 |
|------|------|------|
| `length_is` | 判断数组长度是否等于 `value` | `list=["a","b"]`, `value="2"` → ✅ |
| `contains` | 判断数组是否包含 `value`（字符串匹配） | `list=["apple","banana"]`, `value="apple"` → ✅ |

**示例**：

```yaml
# 长度检查
check_list_size:
  type: "list"
  config:
    list: '["item1", "item2", "item3"]'
    mode: "length_is"
    value: "3"

# 包含检查
check_fruits:
  type: "list"
  config:
    list: '["apple", "banana", "orange"]'
    mode: "contains"
    value: "banana"

# 动态列表（从其他节点获取）
validate_tags:
  type: "list"
  config:
    list: "{{get_tags}}"  # 假设 get_tags 输出: ["v1.0", "latest"]
    mode: "contains"
    value: "latest"
  dependencies: ["get_tags"]

# 空列表检查
check_empty:
  type: "list"
  config:
    list: "{{error_list}}"
    mode: "length_is"
    value: "0"
  next: ["success"]
  on_failure: ["has_errors"]
```

**输出**：成功时返回 `"LIST_OK"`

**注意事项**：
- `list` 必须是合法的 JSON 数组字符串
- `contains` 模式进行字符串匹配（`Sprint(element) == value`）
- 数组元素可以是字符串、数字、对象等
- JSON 解析失败会抛出错误

---

### 3.3 数据类节点 (Data)

#### 3.3.1 Var / Const - 变量定义

**用途**：定义可在整个工作流中复用的变量或配置

**配置参数**：

支持两种模式：

**模式1：单值模式**
```yaml
config:
  value: "单个值"
```

**模式2：字典模式**
```yaml
config:
  key1: "value1"
  key2: "value2"
  key3: "value3"
```

**示例**：

```yaml
# 单值变量
user_name:
  type: "var"
  config:
    value: "Alice"
  next: ["greet"]

greet:
  type: "shell"
  config:
    script: "echo 'Hello, {{user_name}}!'"  # 输出: Hello, Alice!

# 字典变量（配置集合）
config:
  type: "var"
  config:
    api_endpoint: "https://api.example.com"
    max_retries: "3"
    timeout: "30"
    environment: "production"
  next: ["task1", "task2"]

task1:
  type: "api"
  config:
    url: "{{config.api_endpoint}}/users"
  dependencies: ["config"]

task2:
  type: "shell"
  config:
    script: "echo 'Env: {{config.environment}}, Timeout: {{config.timeout}}'"
  dependencies: ["config"]

# 常量定义（type: "const" 与 "var" 行为相同）
constants:
  type: "const"
  config:
    pi: "3.14159"
    app_version: "1.2.3"
```

**输出**：
- 单值模式：返回 `value` 的值
- 字典模式：返回 JSON 序列化的字符串（如 `{"key1":"value1","key2":"value2"}`）

**使用方式**：
- 单值：`{{nodeID}}` 或 `{{nodeID.value}}`
- 字典：`{{nodeID.key1}}`、`{{nodeID.key2}}`

**注意事项**：
- `var` 和 `const` 类型完全等价（只是语义区分）
- 所有值都存储为字符串
- 数字需要在使用时转换（如 `math` 节点会自动转换）

---

#### 3.3.2 Secret - 机密存储

**用途**：存储敏感信息（API 密钥、密码、令牌等），并自动在日志中脱敏

**配置参数**：与 `Var` 完全相同

**示例**：

```yaml
# 密钥存储
secrets:
  type: "secret"
  config:
    openai_api_key: "sk-proj-abcdefghijklmnopqrstuvwxyz"
    github_token: "ghp_1234567890abcdefghijklmnopqrstuv"
    db_password: "MyS3cr3tP@ssw0rd!"
  next: ["ai_task", "deploy"]

# 使用密钥
ai_task:
  type: "ai"
  config:
    endpoint: "https://api.openai.com/v1/chat/completions"
    api_key: "{{secrets.openai_api_key}}"  # ← 自动脱敏
    model: "gpt-4o"
    prompt: "Hello, AI!"
  dependencies: ["secrets"]

deploy:
  type: "shell"
  config:
    script: "git clone https://{{secrets.github_token}}@github.com/user/repo.git"
  dependencies: ["secrets"]
```

**脱敏效果**：

```
# 原始日志（未使用 secret 节点）
[exec-xxx] [SUCCESS] [ai_task] 输出: {"api_key": "sk-proj-abcdefghijklmnopqrstuvwxyz"}

# 使用 secret 节点后的日志
[exec-xxx] [SUCCESS] [secrets] 输出: ********  # secret 节点输出被脱敏
[exec-xxx] [SUCCESS] [ai_task] 输出: {"api_key": "********"}  # 包含密钥的输出被脱敏
[exec-xxx] [SUCCESS] [deploy] 输出: Cloning into 'repo'...  # Token 被自动隐藏
```

**工作原理**：
1. `secret` 节点执行时，引擎提取所有 `config` 中的值
2. 这些值被添加到全局"黑名单"（`ExecutionContext.secretValues`）
3. 所有日志输出通过 `MaskLog()` 过滤，匹配的值替换为 `********`

**注意事项**：
- ⚠️ **脱敏仅影响日志输出**，不影响实际执行
- ⚠️ Secret 节点必须在使用密钥的节点**之前**执行（通过 `dependencies` 控制）
- ⚠️ 脱敏是字符串匹配，确保 secret 值足够独特（避免误脱敏）
- ✅ 推荐模式：将所有密钥放在一个 `secret` 节点中集中管理

---

### 3.4 基础类节点 (Base)

#### 3.4.1 Virtual - 虚拟节点

**用途**：占位节点，用于复杂流程控制（不执行任何实际操作）

**配置参数**：无（可以省略 `config` 字段）

**示例**：

```yaml
# 场景1: 多分支汇聚点
nodes:
  start:
    type: "shell"
    config:
      script: "echo 'Starting...'"
    next: ["branch_a", "branch_b", "branch_c"]

  branch_a:
    type: "shell"
    config:
      script: "echo 'Branch A'"
    next: ["merge"]

  branch_b:
    type: "shell"
    config:
      script: "echo 'Branch B'"
    next: ["merge"]

  branch_c:
    type: "shell"
    config:
      script: "echo 'Branch C'"
    next: ["merge"]

  # 汇聚点：等待所有分支完成
  merge:
    type: "virtual"  # 不执行任何操作，只等待
    dependencies: ["branch_a", "branch_b", "branch_c"]
    next: ["final_step"]

  final_step:
    type: "shell"
    config:
      script: "echo 'All branches completed'"
    dependencies: ["merge"]

# 场景2: 逻辑占位符
check_condition:
  type: "if"
  config:
    if: "status == 'ready'"
  next: ["placeholder"]
  on_failure: ["skip"]

placeholder:
  type: "virtual"  # 预留位置，未来扩展
  next: ["next_step"]

# 场景3: 默认类型（省略 type 字段）
sync_point:
  # type 省略，默认为 virtual
  dependencies: ["task1", "task2"]
  next: ["final"]
```

**输出**：始终返回 `"VIRTUAL_OK"`

**使用场景**：
- ✅ **扇入汇聚**：等待多个并发分支全部完成
- ✅ **流程占位**：预留未来功能的接入点
- ✅ **逻辑分组**：将多个节点组织成逻辑单元

**注意事项**：
- 几乎无性能开销（只返回常量字符串）
- 可以省略 `config` 字段
- 如果 `type` 字段省略或为未知类型，默认使用 `virtual`

---

## 4. 高级特性

### 4.1 并发控制

#### 4.1.1 扇出模式（Fan-out）

一个节点的 `next` 字段包含多个节点时，这些节点会**并发执行**：

```yaml
nodes:
  prepare:
    type: "shell"
    config:
      script: "echo 'Preparing...'"
    next: ["lint", "test", "build"]  # 三个任务并发执行

  lint:
    type: "shell"
    config:
      script: "eslint src/"
    dependencies: ["prepare"]

  test:
    type: "shell"
    config:
      script: "npm test"
    dependencies: ["prepare"]

  build:
    type: "shell"
    config:
      script: "npm run build"
    dependencies: ["prepare"]
```

**执行流程**：
```
prepare
  ├─→ lint  (并发)
  ├─→ test  (并发)
  └─→ build (并发)
```

#### 4.1.2 扇入模式（Fan-in）

多个节点的 `next` 指向同一个节点时，该节点会等待**所有依赖**完成：

```yaml
nodes:
  lint:
    type: "shell"
    config:
      script: "eslint src/"
    next: ["report"]

  test:
    type: "shell"
    config:
      script: "npm test"
    next: ["report"]

  build:
    type: "shell"
    config:
      script: "npm run build"
    next: ["report"]

  report:
    type: "shell"
    config:
      script: "echo 'All checks completed'"
    dependencies: ["lint", "test", "build"]  # 等待三个任务全部完成
```

**执行流程**：
```
  lint  ─┐
  test  ─┤ (全部完成后)
  build ─┘
    └─→ report
```

#### 4.1.3 钻石模式（Diamond）

扇出后再扇入：

```yaml
nodes:
  start:
    type: "shell"
    config:
      script: "git clone repo"
    next: ["test", "lint"]

  test:
    type: "shell"
    config:
      script: "npm test"
    dependencies: ["start"]
    next: ["merge"]

  lint:
    type: "shell"
    config:
      script: "eslint src/"
    dependencies: ["start"]
    next: ["merge"]

  merge:
    type: "virtual"
    dependencies: ["test", "lint"]
    next: ["deploy"]

  deploy:
    type: "shell"
    config:
      script: "./deploy.sh"
    dependencies: ["merge"]
```

**执行流程**：
```
      start
       / \
    test  lint (并发)
       \ /
      merge (等待)
        |
      deploy
```

### 4.2 条件分支

#### 4.2.1 基于 If 节点的分支

```yaml
nodes:
  check_env:
    type: "text"
    config:
      target: "{{config.environment}}"
      mode: "contains"
      value: "production"
    dependencies: ["config"]
    next: ["prod_flow"]
    on_failure: ["dev_flow"]

  prod_flow:
    type: "shell"
    config:
      script: "./deploy-prod.sh"
    dependencies: ["check_env"]

  dev_flow:
    type: "shell"
    config:
      script: "./deploy-dev.sh"
```

#### 4.2.2 多级条件

```yaml
nodes:
  check_score:
    type: "math"
    config:
      left: "{{score}}"
      operator: ">="
      right: "90"
    next: ["excellent"]
    on_failure: ["check_good"]

  excellent:
    type: "shell"
    config:
      script: "echo 'Excellent!'"

  check_good:
    type: "math"
    config:
      left: "{{score}}"
      operator: ">="
      right: "60"
    next: ["pass"]
    on_failure: ["fail"]

  pass:
    type: "shell"
    config:
      script: "echo 'Pass'"

  fail:
    type: "shell"
    config:
      script: "echo 'Fail'"
```

**执行路径**：
- score ≥ 90 → `excellent`
- 60 ≤ score < 90 → `pass`
- score < 60 → `fail`

### 4.3 错误传播机制

#### 4.3.1 自动跳过下游

```yaml
nodes:
  step1:
    type: "shell"
    config:
      script: "exit 1"  # 失败
    next: ["step2"]

  step2:
    type: "shell"
    config:
      script: "echo 'This will be skipped'"
    dependencies: ["step1"]
    next: ["step3"]

  step3:
    type: "shell"
    config:
      script: "echo 'This will also be skipped'"
    dependencies: ["step2"]
```

**日志输出**：
```
[exec-xxx] [ERROR]   [step1] 执行失败: exit status 1
[exec-xxx] [SKIP]    [step2] 由于上游失败自动跳过
[exec-xxx] [SKIP]    [step3] 由于上游失败自动跳过
```

#### 4.3.2 独立分支不受影响

```yaml
nodes:
  task_a:
    type: "shell"
    config:
      script: "exit 1"  # 失败
    next: ["task_a_child"]

  task_a_child:
    type: "shell"
    config:
      script: "echo 'Skipped'"
    dependencies: ["task_a"]

  task_b:
    type: "shell"
    config:
      script: "echo 'I will still run!'"  # 独立分支，正常执行
```

**执行结果**：
- `task_a` 失败
- `task_a_child` 被跳过
- `task_b` 正常执行（不依赖 `task_a`）

### 4.4 密钥脱敏系统

#### 4.4.1 完整脱敏示例

```yaml
nodes:
  # 1. 定义密钥
  vault:
    type: "secret"
    config:
      github_token: "ghp_SUPER_SECRET_TOKEN_12345"
      api_key: "sk-proj-ANOTHER_SECRET_67890"
    next: ["clone_repo", "call_api"]

  # 2. 使用密钥（会被自动脱敏）
  clone_repo:
    type: "shell"
    config:
      script: "git clone https://{{vault.github_token}}@github.com/user/repo.git"
    dependencies: ["vault"]

  call_api:
    type: "api"
    config:
      url: "https://api.example.com/data"
      api_key: "{{vault.api_key}}"
    dependencies: ["vault"]
```

**日志输出**：
```
[exec-xxx] [SUCCESS] [vault] 输出: ********
[exec-xxx] [SUCCESS] [clone_repo] 输出: Cloning into 'repo'...
# 注意：Token 不会出现在日志中
[exec-xxx] [SUCCESS] [call_api] 输出: ********
# 注意：API Key 被自动隐藏
```

#### 4.4.2 脱敏原理

```go
// 引擎在 secret 节点执行后：
if node.Type == "secret" {
    var secretData map[string]interface{}
    if err := json.Unmarshal([]byte(res), &secretData); err == nil {
        for _, v := range secretData {
            ctx.AddSecretValue(fmt.Sprint(v))  // 添加到黑名单
        }
    }
}

// 所有日志输出前：
func (ctx *ExecutionContext) MaskLog(s string) string {
    for _, secret := range ctx.secretValues {
        s = strings.ReplaceAll(s, secret, "********")
    }
    return s
}
```

### 4.5 超时控制

#### 4.5.1 节点级超时

```yaml
nodes:
  slow_task:
    type: "shell"
    config:
      script: "sleep 100"
    timeout: 5  # 5秒后强制终止
    retry_count: 2
    on_failure: ["handle_timeout"]
```

#### 4.5.2 不同任务的默认超时

| 任务类型 | 默认超时 |
|---------|---------|
| `shell` | 30秒 |
| `ai` | 60秒 |
| `api` | 30秒 |
| 逻辑节点 | 无超时 |

#### 4.5.3 超时行为

- Shell 任务：自动 Kill 子进程
- AI/API 任务：取消 HTTP 请求
- 触发 `on_failure` 分支（如果配置了）
- 下游节点自动跳过

---

## 5. 最佳实践

### 5.1 节点命名规范

#### 推荐命名方式

```yaml
# ✅ 好的命名：清晰、描述性强
nodes:
  init_config:        # 数据节点：init_*, set_*, load_*
    type: "var"

  fetch_user_data:    # 任务节点：fetch_*, run_*, exec_*
    type: "api"

  check_status:       # 逻辑节点：check_*, validate_*, verify_*
    type: "if"

  notify_team:        # 通知节点：notify_*, alert_*, send_*
    type: "api"

# ❌ 不好的命名：模糊、无意义
nodes:
  node1:
    type: "var"

  do_stuff:
    type: "shell"

  x:
    type: "if"
```

#### 分层命名

```yaml
# 复杂工作流：使用层级前缀
nodes:
  # 准备阶段
  prep_load_config:
    type: "var"

  prep_validate_env:
    type: "check"

  # 构建阶段
  build_compile_code:
    type: "shell"

  build_run_tests:
    type: "shell"

  # 部署阶段
  deploy_push_image:
    type: "shell"

  deploy_update_service:
    type: "api"

  # 通知阶段
  notify_slack:
    type: "api"
```

### 5.2 错误处理策略

#### 5.2.1 关键任务：重试 + 失败分支

```yaml
critical_deploy:
  type: "shell"
  config:
    script: "./deploy.sh production"
  retry_count: 3      # 最多重试3次
  timeout: 300        # 5分钟超时
  on_failure: ["rollback", "notify_oncall"]  # 失败后回滚并通知
  next: ["verify_deployment"]

rollback:
  type: "shell"
  config:
    script: "./rollback.sh"
  next: ["notify_oncall"]

notify_oncall:
  type: "api"
  config:
    url: "https://pagerduty.com/api/incidents"
    api_key: "{{secrets.pagerduty_key}}"
    body: '{"title": "Deployment failed", "urgency": "high"}'
```

#### 5.2.2 非关键任务：快速失败

```yaml
optional_cleanup:
  type: "shell"
  config:
    script: "rm -rf /tmp/cache"
  retry_count: 0     # 不重试
  timeout: 10        # 快速超时
  # 不配置 on_failure，失败后直接跳过
```

#### 5.2.3 探测任务：容忍失败

```yaml
health_check:
  type: "api"
  config:
    url: "https://api.example.com/health"
  on_failure: ["use_fallback"]  # 失败时使用备用方案
  next: ["use_primary"]

use_primary:
  type: "shell"
  config:
    script: "echo 'Using primary service'"

use_fallback:
  type: "shell"
  config:
    script: "echo 'Using fallback service'"
```

### 5.3 密钥管理

#### 5.3.1 集中管理密钥

```yaml
# ✅ 推荐：单一 secret 节点
nodes:
  secrets:
    type: "secret"
    config:
      openai_key: "sk-xxx"
      github_token: "ghp_xxx"
      db_password: "xxx"
      slack_webhook: "https://hooks.slack.com/xxx"
    next: ["all", "downstream", "tasks"]

# ❌ 不推荐：分散的 secret 节点
nodes:
  secret1:
    type: "secret"
    config:
      openai_key: "sk-xxx"

  secret2:
    type: "secret"
    config:
      github_token: "ghp_xxx"
```

#### 5.3.2 环境隔离

```yaml
# 开发环境：dev.yaml
nodes:
  secrets:
    type: "secret"
    config:
      api_key: "dev-key-12345"
      endpoint: "https://dev-api.example.com"

# 生产环境：prod.yaml
nodes:
  secrets:
    type: "secret"
    config:
      api_key: "prod-key-67890"
      endpoint: "https://api.example.com"
```

### 5.4 性能优化

#### 5.4.1 最大化并发

```yaml
# ✅ 好的设计：充分利用并发
nodes:
  start:
    type: "var"
    config:
      value: "ready"
    next: ["task1", "task2", "task3", "task4"]  # 4个任务并发

  task1:
    type: "shell"
    config:
      script: "./build-frontend.sh"

  task2:
    type: "shell"
    config:
      script: "./build-backend.sh"

  task3:
    type: "shell"
    config:
      script: "./run-tests.sh"

  task4:
    type: "shell"
    config:
      script: "./generate-docs.sh"

# ❌ 坏的设计：不必要的串行
nodes:
  task1:
    type: "shell"
    config:
      script: "./build-frontend.sh"
    next: ["task2"]  # 不必要的依赖

  task2:
    type: "shell"
    config:
      script: "./build-backend.sh"
    next: ["task3"]

  task3:
    type: "shell"
    config:
      script: "./run-tests.sh"
```

#### 5.4.2 合理设置超时

```yaml
# 根据任务特性设置超时
nodes:
  quick_check:
    type: "api"
    config:
      url: "https://api.example.com/ping"
    timeout: 5  # 快速任务：短超时

  compile_project:
    type: "shell"
    config:
      script: "make build"
    timeout: 600  # 慢任务：长超时（10分钟）

  ai_analysis:
    type: "ai"
    config:
      endpoint: "https://api.openai.com/v1/chat/completions"
      api_key: "{{secrets.openai_key}}"
      model: "gpt-4o"
      prompt: "Analyze this large document..."
    timeout: 120  # AI 任务：2分钟
```

### 5.5 调试技巧

#### 5.5.1 添加调试输出

```yaml
nodes:
  debug_vars:
    type: "shell"
    config:
      script: |
        echo "=== Debug Info ==="
        echo "Config: {{config}}"
        echo "User: {{user_data}}"
        echo "Status: {{status}}"
    dependencies: ["config", "user_data", "status"]
    next: ["actual_task"]
```

#### 5.5.2 使用虚拟节点跟踪进度

```yaml
nodes:
  stage1_complete:
    type: "virtual"
    dependencies: ["task1", "task2"]
    next: ["stage2_start"]

  stage2_start:
    type: "shell"
    config:
      script: "echo 'Stage 2 starting...'"
    next: ["task3", "task4"]

  stage2_complete:
    type: "virtual"
    dependencies: ["task3", "task4"]
```

#### 5.5.3 日志级别控制

```bash
# 查看详细日志
./tofi-core -workflow test.yaml 2>&1 | tee workflow.log

# 过滤关键信息
./tofi-core -workflow test.yaml 2>&1 | grep -E '\[(ERROR|SKIP|SUCCESS)\]'

# 追踪特定节点
./tofi-core -workflow test.yaml 2>&1 | grep '\[my_node\]'
```

---

## 6. 完整示例

### 6.1 CI/CD 流水线

```yaml
# ci-cd-pipeline.yaml
nodes:
  # 1. 配置与密钥
  config:
    type: "var"
    config:
      repo_url: "https://github.com/user/project.git"
      docker_registry: "registry.example.com"
      app_name: "my-app"
    next: ["secrets"]

  secrets:
    type: "secret"
    config:
      github_token: "ghp_xxxxxxxxxxxx"
      docker_password: "xxxxxxxxxxxx"
      slack_webhook: "https://hooks.slack.com/services/xxx"
    dependencies: ["config"]
    next: ["clone_repo"]

  # 2. 克隆代码
  clone_repo:
    type: "shell"
    config:
      script: |
        rm -rf /tmp/{{config.app_name}}
        git clone {{config.repo_url}} /tmp/{{config.app_name}}
        cd /tmp/{{config.app_name}} && git log -1 --oneline
    dependencies: ["secrets"]
    next: ["lint", "test", "build"]

  # 3. 并发执行：Lint、Test、Build
  lint:
    type: "shell"
    config:
      script: "cd /tmp/{{config.app_name}} && npm run lint"
    dependencies: ["clone_repo"]
    retry_count: 2
    next: ["quality_gate"]

  test:
    type: "shell"
    config:
      script: "cd /tmp/{{config.app_name}} && npm test"
    dependencies: ["clone_repo"]
    retry_count: 2
    next: ["quality_gate"]

  build:
    type: "shell"
    config:
      script: "cd /tmp/{{config.app_name}} && npm run build"
    dependencies: ["clone_repo"]
    timeout: 300
    next: ["quality_gate"]

  # 4. 质量门禁
  quality_gate:
    type: "virtual"
    dependencies: ["lint", "test", "build"]
    next: ["ai_code_review"]

  # 5. AI 代码审查
  ai_code_review:
    type: "ai"
    config:
      provider: "openai"
      endpoint: "https://api.openai.com/v1/chat/completions"
      api_key: "{{secrets.openai_key}}"
      model: "gpt-4o-mini"
      system: "你是一个资深的代码审查专家。"
      prompt: |
        请审查以下构建结果：
        Lint: {{lint}}
        Test: {{test}}
        Build: {{build}}

        评估代码质量，给出 "APPROVED" 或 "REJECTED"。
    dependencies: ["quality_gate"]
    next: ["check_review"]

  check_review:
    type: "text"
    config:
      target: "{{ai_code_review}}"
      mode: "contains"
      value: "APPROVED"
    dependencies: ["ai_code_review"]
    next: ["docker_build"]
    on_failure: ["reject_deployment"]

  # 6. Docker 构建
  docker_build:
    type: "shell"
    config:
      script: |
        cd /tmp/{{config.app_name}}
        docker build -t {{config.docker_registry}}/{{config.app_name}}:latest .
    dependencies: ["check_review"]
    timeout: 600
    retry_count: 1
    next: ["docker_push"]
    on_failure: ["notify_failure"]

  docker_push:
    type: "shell"
    config:
      script: |
        echo "{{secrets.docker_password}}" | docker login {{config.docker_registry}} -u admin --password-stdin
        docker push {{config.docker_registry}}/{{config.app_name}}:latest
    dependencies: ["docker_build"]
    next: ["deploy_production"]

  # 7. 生产部署
  deploy_production:
    type: "shell"
    config:
      script: |
        kubectl set image deployment/{{config.app_name}} \
          app={{config.docker_registry}}/{{config.app_name}}:latest
    dependencies: ["docker_push"]
    retry_count: 2
    timeout: 120
    next: ["verify_deployment"]
    on_failure: ["rollback_deployment"]

  # 8. 部署验证
  verify_deployment:
    type: "api"
    config:
      method: "GET"
      url: "https://{{config.app_name}}.example.com/health"
    dependencies: ["deploy_production"]
    retry_count: 5
    timeout: 10
    next: ["notify_success"]
    on_failure: ["rollback_deployment"]

  # 9. 成功通知
  notify_success:
    type: "api"
    config:
      url: "{{secrets.slack_webhook}}"
      body: |
        {
          "text": "🎉 部署成功！",
          "blocks": [
            {
              "type": "section",
              "text": {
                "type": "mrkdwn",
                "text": "*部署成功*\nApp: {{config.app_name}}\nStatus: ✅"
              }
            }
          ]
        }
    dependencies: ["verify_deployment"]

  # 10. 失败处理
  rollback_deployment:
    type: "shell"
    config:
      script: "kubectl rollout undo deployment/{{config.app_name}}"
    next: ["notify_failure"]

  notify_failure:
    type: "api"
    config:
      url: "{{secrets.slack_webhook}}"
      body: |
        {
          "text": "❌ 部署失败，已自动回滚",
          "blocks": [
            {
              "type": "section",
              "text": {
                "type": "mrkdwn",
                "text": "*部署失败*\nApp: {{config.app_name}}\nStatus: ❌ 已回滚"
              }
            }
          ]
        }

  reject_deployment:
    type: "shell"
    config:
      script: "echo 'AI Review rejected the deployment'"
    next: ["notify_failure"]
```

**执行流程图**：
```
config → secrets → clone_repo
                      ├─→ lint ─┐
                      ├─→ test ─┤→ quality_gate
                      └─→ build ┘
                                ↓
                          ai_code_review
                                ↓
                          check_review
                           ↓       ↓
                    (PASS)        (FAIL)
                      ↓             ↓
                docker_build    reject_deployment
                      ↓             ↓
                docker_push     notify_failure
                      ↓
              deploy_production
                 ↓       ↓
            (成功)     (失败)
              ↓           ↓
        verify_deployment  rollback_deployment
         ↓       ↓              ↓
    (成功)     (失败)      notify_failure
      ↓           ↓
notify_success  rollback_deployment
```

### 6.2 数据处理流水线

```yaml
# data-pipeline.yaml
nodes:
  # 1. 配置
  config:
    type: "var"
    config:
      data_source: "https://api.example.com/raw-data"
      output_bucket: "s3://my-bucket/processed"
      batch_size: "1000"
    next: ["secrets"]

  secrets:
    type: "secret"
    config:
      api_key: "api_key_here"
      openai_key: "sk-proj-xxx"
      aws_access_key: "AKIA..."
      aws_secret_key: "..."
    dependencies: ["config"]
    next: ["fetch_data"]

  # 2. 获取原始数据
  fetch_data:
    type: "api"
    config:
      url: "{{config.data_source}}"
      api_key: "{{secrets.api_key}}"
    dependencies: ["secrets"]
    retry_count: 3
    timeout: 60
    next: ["validate_data"]

  # 3. 数据验证
  validate_data:
    type: "list"
    config:
      list: "{{fetch_data}}"
      mode: "length_is"
      value: "{{config.batch_size}}"
    dependencies: ["fetch_data"]
    next: ["split_processing"]
    on_failure: ["log_warning"]

  log_warning:
    type: "shell"
    config:
      script: "echo 'Warning: Expected {{config.batch_size}} items, got different count'"
    next: ["split_processing"]

  # 4. 分批处理（假设拆分为3个批次）
  split_processing:
    type: "virtual"
    dependencies: ["validate_data"]
    next: ["process_batch_1", "process_batch_2", "process_batch_3"]

  process_batch_1:
    type: "ai"
    config:
      provider: "openai"
      endpoint: "https://api.openai.com/v1/chat/completions"
      api_key: "{{secrets.openai_key}}"
      model: "gpt-4o-mini"
      prompt: "Analyze batch 1: {{fetch_data}}"
    dependencies: ["split_processing"]
    next: ["merge_results"]

  process_batch_2:
    type: "ai"
    config:
      provider: "openai"
      endpoint: "https://api.openai.com/v1/chat/completions"
      api_key: "{{secrets.openai_key}}"
      model: "gpt-4o-mini"
      prompt: "Analyze batch 2: {{fetch_data}}"
    dependencies: ["split_processing"]
    next: ["merge_results"]

  process_batch_3:
    type: "ai"
    config:
      provider: "openai"
      endpoint: "https://api.openai.com/v1/chat/completions"
      api_key: "{{secrets.openai_key}}"
      model: "gpt-4o-mini"
      prompt: "Analyze batch 3: {{fetch_data}}"
    dependencies: ["split_processing"]
    next: ["merge_results"]

  # 5. 合并结果
  merge_results:
    type: "shell"
    config:
      script: |
        echo "Batch 1: {{process_batch_1}}"
        echo "Batch 2: {{process_batch_2}}"
        echo "Batch 3: {{process_batch_3}}"
        echo "All batches processed"
    dependencies: ["process_batch_1", "process_batch_2", "process_batch_3"]
    next: ["upload_results"]

  # 6. 上传到 S3
  upload_results:
    type: "shell"
    config:
      script: |
        export AWS_ACCESS_KEY_ID="{{secrets.aws_access_key}}"
        export AWS_SECRET_ACCESS_KEY="{{secrets.aws_secret_key}}"
        echo "{{merge_results}}" | aws s3 cp - {{config.output_bucket}}/result.txt
    dependencies: ["merge_results"]
    next: ["notify_complete"]

  # 7. 完成通知
  notify_complete:
    type: "shell"
    config:
      script: "echo 'Data pipeline completed successfully'"
    dependencies: ["upload_results"]
```

### 6.3 监控与告警

```yaml
# monitoring.yaml
nodes:
  config:
    type: "var"
    config:
      service_url: "https://api.myapp.com"
      metrics_endpoint: "https://api.myapp.com/metrics"
      alert_threshold: "90"
    next: ["secrets"]

  secrets:
    type: "secret"
    config:
      pagerduty_key: "xxx"
      slack_webhook: "https://hooks.slack.com/xxx"
    dependencies: ["config"]
    next: ["health_check", "metrics_check"]

  # 并发检查：健康状态和指标
  health_check:
    type: "api"
    config:
      method: "GET"
      url: "{{config.service_url}}/health"
    dependencies: ["secrets"]
    timeout: 10
    next: ["check_health_status"]
    on_failure: ["alert_service_down"]

  metrics_check:
    type: "api"
    config:
      method: "GET"
      url: "{{config.metrics_endpoint}}"
    dependencies: ["secrets"]
    next: ["parse_cpu_usage"]

  # 检查健康状态
  check_health_status:
    type: "text"
    config:
      target: "{{health_check}}"
      mode: "contains"
      value: "healthy"
    dependencies: ["health_check"]
    on_failure: ["alert_unhealthy"]

  # 解析 CPU 使用率（假设返回 JSON: {"cpu": "85.5"}）
  parse_cpu_usage:
    type: "shell"
    config:
      script: "echo '{{metrics_check}}' | jq -r '.cpu'"
    dependencies: ["metrics_check"]
    next: ["check_cpu_threshold"]

  check_cpu_threshold:
    type: "math"
    config:
      left: "{{parse_cpu_usage}}"
      operator: ">"
      right: "{{config.alert_threshold}}"
    dependencies: ["parse_cpu_usage"]
    next: ["alert_high_cpu"]
    on_failure: ["all_clear"]

  # 告警分支
  alert_service_down:
    type: "api"
    config:
      url: "{{secrets.pagerduty_key}}"
      body: '{"title": "Service Down", "urgency": "high"}'
    next: ["notify_slack_down"]

  notify_slack_down:
    type: "api"
    config:
      url: "{{secrets.slack_webhook}}"
      body: '{"text": "🚨 Service is DOWN!"}'

  alert_unhealthy:
    type: "api"
    config:
      url: "{{secrets.slack_webhook}}"
      body: '{"text": "⚠️ Service health check failed"}'

  alert_high_cpu:
    type: "api"
    config:
      url: "{{secrets.slack_webhook}}"
      body: '{"text": "⚠️ High CPU usage: {{parse_cpu_usage}}%"}'

  all_clear:
    type: "shell"
    config:
      script: "echo 'All checks passed'"
```

---

## 7. 故障排查

### 7.1 常见错误

#### 7.1.1 循环依赖

**错误现象**：工作流卡住，部分节点永远处于 `WAIT` 状态

**示例**：
```yaml
# ❌ 错误：循环依赖
nodes:
  task_a:
    type: "shell"
    config:
      script: "echo 'A'"
    dependencies: ["task_b"]  # A 依赖 B
    next: ["task_b"]

  task_b:
    type: "shell"
    config:
      script: "echo 'B'"
    dependencies: ["task_a"]  # B 依赖 A → 循环！
```

**日志**：
```
[exec-xxx] [WAIT] [task_a] Waiting for: task_b
[exec-xxx] [WAIT] [task_b] Waiting for: task_a
# 工作流永远无法完成
```

**解决方案**：
- 检查 `dependencies` 字段，确保无循环
- 使用 `next` 字段定义单向流程

#### 7.1.2 变量未定义

**错误现象**：变量替换失败，输出为空或原始 `{{nodeID}}`

**示例**：
```yaml
nodes:
  use_var:
    type: "shell"
    config:
      script: "echo {{undefined_var}}"  # undefined_var 不存在
```

**日志**：
```
[exec-xxx] [SUCCESS] [use_var] 输出: {{undefined_var}}
# 变量未被替换
```

**解决方案**：
- 确保被引用的节点已执行（通过 `dependencies` 声明）
- 检查节点ID拼写是否正确
- 使用 `var` 节点预定义变量

#### 7.1.3 JSON 路径错误

**错误现象**：`{{nodeID.field}}` 提取失败

**示例**：
```yaml
nodes:
  get_data:
    type: "shell"
    config:
      script: 'echo ''{"user": "Alice"}'''
    next: ["use_data"]

  use_data:
    type: "shell"
    config:
      script: "echo {{get_data.name}}"  # ❌ 字段不存在
    dependencies: ["get_data"]
```

**日志**：
```
[exec-xxx] [SUCCESS] [get_data] 输出: {"user": "Alice"}
[exec-xxx] [SUCCESS] [use_data] 输出:
# name 字段不存在，返回空
```

**解决方案**：
- 检查 JSON 结构是否正确
- 使用正确的字段名（`{{get_data.user}}`）
- 测试原始输出：`echo {{get_data}}`

#### 7.1.4 超时设置过短

**错误现象**：任务反复超时失败

**日志**：
```
[exec-xxx] [START]   [slow_task] 类型: shell
[exec-xxx] [ERROR]   [slow_task] 执行失败: context deadline exceeded
[exec-xxx] [RETRY]   [slow_task] 第 1 次重试...
[exec-xxx] [ERROR]   [slow_task] 执行失败: context deadline exceeded
```

**解决方案**：
- 增加 `timeout` 值
- 检查任务是否真的需要这么长时间
- 考虑优化任务本身

#### 7.1.5 密钥脱敏不生效

**错误现象**：密钥仍然出现在日志中

**原因**：`secret` 节点未在使用密钥的节点之前执行

**示例**：
```yaml
# ❌ 错误：缺少依赖关系
nodes:
  secrets:
    type: "secret"
    config:
      api_key: "sk-xxx"

  use_key:
    type: "api"
    config:
      api_key: "{{secrets.api_key}}"
    # ❌ 缺少 dependencies: ["secrets"]
```

**解决方案**：
```yaml
# ✅ 正确：显式声明依赖
use_key:
  type: "api"
  config:
    api_key: "{{secrets.api_key}}"
  dependencies: ["secrets"]  # 确保 secrets 先执行
```

### 7.2 日志解读

#### 7.2.1 日志格式

```
[执行ID] [状态] [节点ID] 详细信息
```

**状态码**：
| 状态 | 含义 |
|------|------|
| `START` | 节点开始执行 |
| `SUCCESS` | 节点执行成功 |
| `ERROR` | 节点执行失败 |
| `SKIP` | 节点被跳过（上游失败） |
| `WAIT` | 节点等待依赖完成 |
| `RETRY` | 节点正在重试 |

#### 7.2.2 调试流程

1. **查找失败节点**：
   ```bash
   cat workflow.log | grep '\[ERROR\]'
   ```

2. **追踪节点执行路径**：
   ```bash
   cat workflow.log | grep '\[my_node\]'
   ```

3. **检查跳过原因**：
   ```bash
   cat workflow.log | grep '\[SKIP\]'
   ```

4. **查看完整执行统计**：
   ```bash
   cat workflow.log | grep 'Status:'
   ```

### 7.3 调试技巧

#### 7.3.1 添加断点节点

```yaml
nodes:
  before_critical:
    type: "shell"
    config:
      script: |
        echo "=== DEBUG CHECKPOINT ==="
        echo "Var1: {{var1}}"
        echo "Var2: {{var2}}"
        echo "========================"
    dependencies: ["var1", "var2"]
    next: ["critical_task"]
```

#### 7.3.2 临时禁用节点

```yaml
# 方法1: 注释掉 next 字段
problematic_task:
  type: "shell"
  config:
    script: "./might-fail.sh"
  # next: ["downstream"]  # 临时禁用

# 方法2: 将失败节点替换为 virtual
problematic_task:
  type: "virtual"  # 跳过实际执行
  # type: "shell"
  # config:
  #   script: "./might-fail.sh"
  next: ["downstream"]
```

#### 7.3.3 单步执行

创建简化版工作流：
```yaml
# debug.yaml - 只测试一个节点
nodes:
  test_node:
    type: "shell"
    config:
      script: "echo 'Testing...'"
```

---

## 附录

### A. 完整配置参考

#### A.1 节点字段

```yaml
node_id:
  type: "节点类型"                # 必需
  config:                         # 必需
    param1: "value1"
  next: ["节点1", "节点2"]         # 可选
  dependencies: ["节点3", "节点4"] # 可选
  retry_count: 3                  # 可选，默认0
  timeout: 60                     # 可选，默认30
  on_failure: ["失败节点"]         # 可选
```

#### A.2 所有节点类型一览

| 类别 | 类型 | 用途 |
|------|------|------|
| **任务类** | `shell` | 执行 Shell 命令 |
| | `ai` | 调用 AI 大模型 |
| | `api` | 发送 HTTP 请求 |
| **逻辑类** | `if` | 复杂表达式判断 |
| | `check` | 简单值检查 |
| | `text` | 文本模式匹配 |
| | `math` | 数值比较 |
| | `list` | 列表操作 |
| **数据类** | `var` / `const` | 变量定义 |
| | `secret` | 机密存储 |
| **基础类** | `virtual` | 虚拟占位节点 |

### B. 变量替换语法

| 语法 | 说明 | 示例 |
|------|------|------|
| `{{nodeID}}` | 获取节点完整输出 | `{{user_data}}` |
| `{{nodeID.field}}` | 提取 JSON 字段 | `{{config.api_key}}` |
| `{{nodeID.a.b.c}}` | 嵌套路径 | `{{response.data.user.name}}` |

### C. 内置函数（仅 If 节点）

| 函数 | 签名 | 示例 |
|------|------|------|
| `contains` | `contains(str, substr) bool` | `contains(result, 'SUCCESS')` |
| `len` | `len(str) int` | `len(name) > 0` |

### D. 运算符（仅 If 节点）

| 类型 | 运算符 |
|------|--------|
| 比较 | `==`, `!=`, `>`, `<`, `>=`, `<=` |
| 逻辑 | `&&`, `||`, `!` |

---

## 结语

本文档涵盖了 Tofi 工作流引擎的所有核心功能。如有疑问或发现问题，请参考：

- **源码位置**：
  - 引擎核心：`/internal/engine/engine.go`
  - 任务节点：`/internal/engine/tasks/`
  - 逻辑节点：`/internal/engine/logic/`
  - 数据节点：`/internal/engine/data/`

- **示例工作流**：`/workflows/` 目录下的 YAML 文件

**Happy Workflow Orchestration! 🚀**
