# Tofi Core

**T**rigger-based **O**perations & **F**low **I**ntegration

一个强大的工作流引擎,支持 AI 集成、多节点编排和自动化任务执行。

## ✨ 特性

- 🤖 **AI 集成**: 内置 GPT-5.1 支持,开箱即用
- 🔄 **工作流编排**: DAG (有向无环图) 任务调度
- 📦 **Action Library**: 官方内置常用功能
- 🔌 **多服务集成**: Telegram, Slack, Discord, Email, GitHub 等
- 🎯 **类型丰富**: API, Shell, AI, 条件判断, 循环, 数学运算等
- 🔐 **安全管理**: 环境变量和 Secret 节点支持

## 🚀 快速开始

### 1. 安装

```bash
# 克隆仓库
git clone https://github.com/yourorg/tofi-core.git
cd tofi-core

# 编译
go build -o tofi cmd/tofi/main.go

# 配置环境变量
cp .env.example .env
```

### 2. 配置 .env

```bash
# AI 服务 (Tofi 官方提供,无需用户配置)
TOFI_OPENAI_API_KEY=your_key_here

# Telegram (用户配置)
TELEGRAM_CHAT_ID=your_chat_id_here

# Tofi 官方 Telegram Bot (已提供)
TOFI_TELEGRAM_BOT_TOKEN=your_bot_token_here
```

### 3. 运行第一个 AI 工作流

```bash
./tofi -workflow workflows/simple_ai_demo.yaml
```

## 📖 使用示例

### 示例 1: AI 新闻搜集与推送

```bash
./tofi -workflow workflows/ai_news_translator.yaml
```

**功能:**
- AI 自动搜集今日科技新闻
- AI 翻译成中文
- 自动推送到 Telegram

### 示例 2: AI 代码审查

```bash
./tofi -workflow workflows/ai_code_reviewer.yaml -env CODE_FILE_PATH=path/to/code.go
```

**功能:**
- 使用 GPT-5.1 Codex Max 深度审查代码
- 分析代码质量、安全性、性能
- 生成中文审查报告
- 保存报告并发送 Telegram 通知

### 示例 3: AI 每日工作总结

```bash
./tofi -workflow workflows/ai_daily_summary.yaml -env DAILY_WORK="今天完成了XX功能..."
```

**功能:**
- AI 生成专业的工作总结
- 自动翻译成中英双语
- 保存并推送到 Telegram

### 示例 4: AI 会议助手

```bash
./tofi -workflow workflows/ai_meeting_assistant.yaml -env RAW_MEETING_NOTES="会议内容..."
```

**功能:**
- AI 提取会议行动项
- AI 生成会议摘要
- 创建 TODO 清单
- 团队 Telegram 通知

## 🧩 Action Library

Tofi 内置了丰富的 Action Library,开箱即用:

### AI Actions
- `tofi/ai_response` - AI 响应生成 (支持 GPT-5.1)

### 消息通知
- `tofi/telegram_notify` - Telegram 通知
- `tofi/slack_notify` - Slack 通知
- `tofi/discord_notify` - Discord 通知
- `tofi/send_email` - SMTP 邮件发送

### 文件操作
- `tofi/read_file` - 读取文件
- `tofi/write_file` - 写入文件

### 其他
- `tofi/webhook_notify` - Webhook 通知
- `tofi/github_create_issue` - 创建 GitHub Issue

查看完整文档: [Action Library 文档](action_library/README.md)

## 📝 Workflow 语法

### 基础结构

```yaml
name: 我的工作流

nodes:
  # 节点 1: AI 任务
  ai_task:
    type: workflow
    config:
      action: "tofi/ai_response"
    input:
      prompt: "你的问题..."
      system_prompt: "你是一个..."
    next: ["notify"]

  # 节点 2: 通知
  notify:
    type: workflow
    config:
      action: "tofi/telegram_notify"
    input:
      chat_id: "${TELEGRAM_CHAT_ID}"
      message: "{{ai_task.generate_response}}"
    dependencies: ["ai_task"]
```

### 支持的节点类型

| 类型 | 说明 | 示例 |
|------|------|------|
| `api` | HTTP API 调用 | REST API 请求 |
| `shell` | Shell 命令执行 | 运行脚本 |
| `ai` | AI 模型调用 | GPT-5.1, Claude |
| `workflow` | 调用 Action | `tofi/ai_response` |
| `if` | 条件判断 | if-else 逻辑 |
| `list` | 列表操作 | map, filter, join |
| `loop` | 循环执行 | for-each 循环 |
| `math` | 数学运算 | 加减乘除 |
| `text` | 文本处理 | 替换、拼接 |
| `secret` | 密钥管理 | 环境变量 |

## 🔧 高级功能

### 1. 并行执行

```yaml
nodes:
  task1:
    type: workflow
    config:
      action: "tofi/ai_response"
    input:
      prompt: "任务 1..."
    next: ["combine"]

  task2:
    type: workflow
    config:
      action: "tofi/ai_response"
    input:
      prompt: "任务 2..."
    next: ["combine"]

  combine:
    type: shell
    input:
      script: |
        echo "结果 1: $R1"
        echo "结果 2: $R2"
    env:
      R1: "{{task1.generate_response}}"
      R2: "{{task2.generate_response}}"
    dependencies: ["task1", "task2"]
```

### 2. 条件执行

```yaml
nodes:
  check:
    type: if
    input:
      condition: "{{status}} == 'success'"
      then: "任务成功"
      else: "任务失败"
    next: ["notify"]
```

### 3. 循环处理

```yaml
nodes:
  process_list:
    type: loop
    input:
      list: ["item1", "item2", "item3"]
      task:
        type: workflow
        config:
          action: "tofi/ai_response"
        input:
          prompt: "处理: {{item}}"
```

## 📚 文档

- [AI Workflows 使用指南](docs/AI_WORKFLOWS_GUIDE.md)
- [Action Library 文档](action_library/README.md)
- [Telegram 设置指南](docs/TELEGRAM_SETUP.md)
- [Workflow 示例集合](workflows/README.md)

## 🛠️ 开发

### 编译

```bash
go build -o tofi cmd/tofi/main.go
```

### 测试

```bash
go test ./...
```

### 添加新的 Action

1. 在 `action_library/` 创建 `.yaml` 文件
2. 使用 `{{inputs.xxx}}` 引用参数
3. 重新编译 (embed 会自动包含)
4. 更新文档

## 🎯 使用场景

### 1. 自动化新闻推送
每天自动获取科技新闻、翻译并推送到团队

### 2. 代码审查自动化
Git commit 前自动 AI 审查代码质量

### 3. 工作总结生成
每日自动生成中英双语工作总结

### 4. 会议记录整理
会议后自动提取行动项和 TODO

### 5. 监控告警
服务异常时自动分析日志并通知

### 6. 内容翻译
批量翻译文档、评论等内容

## 🤝 贡献

欢迎贡献代码、提交 Issue 或分享你的工作流示例!

## 📄 许可

MIT License

---

**快速链接:**
- [开始使用](docs/AI_WORKFLOWS_GUIDE.md)
- [示例工作流](workflows/README.md)
- [API 文档](docs/API.md)
