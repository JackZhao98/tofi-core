# Tofi 快速上手指南

## 🎯 5分钟快速体验

### 步骤 1: 配置环境 (1分钟)

```bash
# 复制配置文件
cp .env.example .env

# 编辑 .env,添加你的 Telegram Chat ID
# TELEGRAM_CHAT_ID=your_chat_id_here
```

**如何获取 Chat ID?**

先给 Tofi Bot 发送一条消息,然后运行:

```bash
./tofi -workflow workflows/find_user_chat_id.yaml -env TELEGRAM_USERNAME=your_username
```

### 步骤 2: 运行第一个 AI 工作流 (30秒)

```bash
./tofi -workflow workflows/simple_ai_demo.yaml
```

这会:
1. AI 生成一条工作效率建议
2. 发送到你的 Telegram
3. 显示结果

### 步骤 3: 尝试更多功能 (3分钟)

#### 3.1 AI 新闻搜集与翻译

```bash
./tofi -workflow workflows/ai_news_translator.yaml
```

**你会收到:** 📰 今日科技新闻摘要 (中文)

---

#### 3.2 AI 代码审查

```bash
./tofi -workflow workflows/ai_code_reviewer.yaml -env CODE_FILE_PATH=internal/engine/engine.go
```

**你会收到:** 🔍 专业的代码审查报告

---

#### 3.3 AI 工作总结

```bash
./tofi -workflow workflows/ai_daily_summary.yaml -env DAILY_WORK="今天完成了用户认证模块,修复了3个bug"
```

**你会收到:** 📝 中英双语工作总结

---

## 🎨 自己创建工作流

### 最简单的示例

创建 `my_workflow.yaml`:

```yaml
name: 我的第一个工作流

nodes:
  ask_ai:
    type: workflow
    config:
      action: "tofi/ai_response"
    input:
      prompt: "给我讲个笑话"
    next: ["send_telegram"]

  send_telegram:
    type: workflow
    config:
      action: "tofi/telegram_notify"
    input:
      chat_id: "${TELEGRAM_CHAT_ID}"
      message: "{{ask_ai.generate_response}}"
    dependencies: ["ask_ai"]
```

运行:

```bash
./tofi -workflow my_workflow.yaml
```

---

## 📋 常用命令速查

### 运行工作流

```bash
# 基本运行
./tofi -workflow path/to/workflow.yaml

# 传递环境变量
./tofi -workflow workflow.yaml -env VAR_NAME=value

# 传递多个变量
./tofi -workflow workflow.yaml -env VAR1=val1 -env VAR2=val2
```

### 查看帮助

```bash
./tofi -help
```

---

## 🧩 可用的 Actions

### AI
```yaml
type: workflow
config:
  action: "tofi/ai_response"
input:
  prompt: "你的问题"
  system_prompt: "你是一个..." # 可选
  model: "gpt-5.1" # 可选
```

### Telegram 通知
```yaml
type: workflow
config:
  action: "tofi/telegram_notify"
input:
  chat_id: "${TELEGRAM_CHAT_ID}"
  message: "你的消息"
  parse_mode: "Markdown" # 可选
```

### 读取文件
```yaml
type: workflow
config:
  action: "tofi/read_file"
input:
  path: "path/to/file.txt"
```

### 写入文件
```yaml
type: workflow
config:
  action: "tofi/write_file"
input:
  path: "output.txt"
  content: "文件内容"
```

---

## 💡 实用技巧

### 1. 变量引用

```yaml
# 引用上一步的输出
message: "{{previous_step.output}}"

# 引用环境变量
chat_id: "${TELEGRAM_CHAT_ID}"

# 引用 input 参数 (在 Action 内部)
prompt: "{{inputs.user_question}}"
```

### 2. 并行执行

```yaml
nodes:
  task1:
    type: workflow
    config:
      action: "tofi/ai_response"
    input:
      prompt: "任务1"
    next: ["combine"]

  task2:
    type: workflow
    config:
      action: "tofi/ai_response"
    input:
      prompt: "任务2"
    next: ["combine"]

  combine:
    type: shell
    input:
      script: |
        echo "结果1: $R1"
        echo "结果2: $R2"
    env:
      R1: "{{task1.generate_response}}"
      R2: "{{task2.generate_response}}"
    dependencies: ["task1", "task2"]
```

### 3. 条件执行

```yaml
nodes:
  check_status:
    type: if
    input:
      condition: "{{result}} == 'success'"
      then: "✅ 成功"
      else: "❌ 失败"
    next: ["notify"]
```

---

## 🔗 进阶学习

- [AI Workflows 详细指南](docs/AI_WORKFLOWS_GUIDE.md)
- [所有示例工作流](workflows/README.md)
- [Toolbox 完整文档](internal/toolbox/README.md)

---

## ❓ 常见问题

### Q: AI Response 需要我自己的 OpenAI API Key 吗?

**A:** 不需要! Tofi 官方提供 `TOFI_OPENAI_API_KEY`,用户可以直接使用,无需配置。

### Q: Telegram 通知需要我自己的 Bot 吗?

**A:** 不需要! Tofi 官方提供 `TOFI_TELEGRAM_BOT_TOKEN`,你只需要配置 `TELEGRAM_CHAT_ID` 即可。

### Q: 如何获取 Chat ID?

**A:** 运行以下命令 (记得先给 Tofi Bot 发消息):
```bash
./tofi -workflow workflows/find_user_chat_id.yaml -env TELEGRAM_USERNAME=your_username
```

### Q: 支持哪些 AI 模型?

**A:**
- `gpt-5.1` (默认) - 最新旗舰模型
- `gpt-5.1-codex-max` - 编程专用
- `gpt-4.1-2025-04-14` - GPT-4.1
- `gpt-4-turbo` - GPT-4 Turbo

### Q: 如何查看执行日志?

**A:** Tofi 会自动输出详细的执行日志,包括每个节点的状态和输出。

---

## 🎉 开始使用吧!

现在你已经掌握了 Tofi 的基础用法,开始创建你的第一个 AI 工作流吧!

**推荐路线:**
1. ✅ 运行 `simple_ai_demo.yaml` 体验基础功能
2. ✅ 尝试 `ai_news_translator.yaml` 看看 AI 如何处理实际任务
3. ✅ 参考示例创建你自己的工作流
4. ✅ 查看详细文档学习高级功能

有问题? 查看 [完整文档](docs/AI_WORKFLOWS_GUIDE.md) 或提交 Issue!
