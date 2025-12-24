# Tofi 工作流引擎完整指南

> Trigger-based Operations & Flow Integration

---

## 目录

- [1. 快速开始](#1-快速开始)
- [2. 核心概念](#2-核心概念)
- [3. 节点配置规范](#3-节点配置规范)
- [4. 高级特性](#4-高级特性)
- [5. 最佳实践](#5-最佳实践)
- [6. 故障排查](#6-故障排查)

---

## 1. 快速开始

### 1.1 什么是 Tofi 工作流？

Tofi 是一个基于 Go 的轻量级工作流引擎，使用 YAML 定义任务编排。它支持 Shell、AI、HTTP API 等多种任务类型，具备并发执行、依赖管理和自动错误重试等特性。

### 1.2 第一个工作流

创建 `hello.yaml`：

```yaml
nodes:
  # 节点1: 打印问候
  greet:
    type: "shell"
    input:
      script: "echo 'Hello, Tofi!'"
    next: ["finish"]

  # 节点2: 完成标记
  finish:
    type: "shell"
    input:
      script: "echo 'Workflow completed!'"
    dependencies: ["greet"]
```

执行：
```bash
./tofi-core -workflow hello.yaml
```

输出：
```
[INFO] [START]   [greet] 类型: shell
[INFO] [SUCCESS] [greet] 输出: Hello, Tofi!
[INFO] [START]   [finish] 类型: shell
[INFO] [SUCCESS] [finish] 输出: Workflow completed!
```

---

## 2. 核心概念

### 2.1 节点（Node）

节点是工作流的最小执行单元。

| 字段 | 说明 | 必需 |
|------|------|------|
| `type` | 节点类型（如 `shell`, `ai`） | ✅ |
| `config` | **静态配置**：控制节点行为（如超时、重试、模式） | ❌ |
| `input` | **动态输入**：业务数据参数（如脚本、Prompt、URL） | ❌ |
| `env` | **环境变量**：注入到 Shell 进程的环境变量 | ❌ |
| `data` | **静态数据**：专用于 `var` / `secret` 节点的数据存储 | ❌ |
| `dependencies` | 必须先完成的前置节点列表 | ❌ |
| `next` | 当前节点成功后触发的后续节点列表 | ❌ |

### 2.2 执行流控制

*   **并发 (Fan-out)**：一个节点的 `next` 指向多个节点，这些节点会并行启动。
*   **汇聚 (Fan-in)**：一个节点有多个 `dependencies`，它会等待所有前置节点成功后才启动。
*   **跳过 (Skip)**：如果前置节点失败（且未被 `on_failure` 捕获），所有依赖它的节点会自动跳过。

---

## 3. 节点配置规范 (v2.0)

所有节点严格遵循以下字段分类：

### 3.1 任务类

#### Shell
```yaml
build:
  type: "shell"
  config:
    timeout: 300
  env:
    ENV: "prod"
  input:
    script: "./build.sh $ENV"
```

#### AI
```yaml
chat:
  type: "ai"
  config:
    model: "gpt-4o"
    api_key: "{{secrets.key}}"
  input:
    prompt: "Hello"
```

#### API
```yaml
ping:
  type: "api"
  config:
    method: "POST"
    url: "https://example.com"
  input:
    body: "{}"
```

### 3.2 逻辑类

#### If
```yaml
check:
  type: "if"
  input:
    if: "score > 60"
```

#### Check / Math / Text / List
所有的判断对象和值都放在 `input`，模式放在 `config`。
```yaml
is_valid:
  type: "check"
  config:
    mode: "is_true"
  input:
    value: "{{flag}}"
```

### 3.3 数据类

#### Var / Secret
数据放在 `data` 字段。
```yaml
config:
  type: "var"
  data:
    env: "prod"
    version: "1.0"
```

---

## 4. 高级特性

### 4.1 变量替换

使用 `{{node_id}}` 或 `{{node_id.field}}` 引用其他节点的输出。

### 4.2 子工作流 (Handoff)

使用 `workflow` 类型调用其他 YAML 文件。

```yaml
call_sub:
  type: "workflow"
  config:
    file: "./sub.yaml"
  input:
    user: "{{user_id}}"  # 注入到子工作流
```

### 4.3 环境变量安全注入

Shell 节点推荐使用 `env` 字段传递敏感数据，避免 Shell 注入风险。

```yaml
# ✅ 安全做法
deploy:
  type: "shell"
  env:
    TOKEN: "{{secrets.token}}"
  input:
    script: "./deploy.sh --token=$TOKEN"

# ❌ 危险做法 (不推荐)
# script: "./deploy.sh --token={{secrets.token}}"
```

---

## 5. 最佳实践

1.  **命名清晰**：使用 `fetch_data`, `check_status` 等动宾短语命名节点。
2.  **集中配置**：使用 `var` 和 `secret` 节点在开头定义所有配置。
3.  **拥抱并发**：利用 `next` 列表让独立的任务并行执行。
4.  **环境隔离**：不同的环境使用不同的 `secret` 配置文件。

---

## 6. 故障排查

*   **节点跳过**：检查上游节点日志，是否有 `ERROR`。
*   **变量未替换**：检查变量名是否正确，注意 JSON 路径区分大小写。
*   **Shell 报错**：检查 `env` 变量是否正确注入，脚本是否有语法错误。