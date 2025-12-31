#!/bin/bash

# 并发 AI 调用测试脚本

set -e

echo "=== Tofi 并发 AI 调用测试 ==="
echo ""

# 检查环境变量
if [ -z "$TOFI_OPENAI_API_KEY" ]; then
  echo "⚠️  警告: TOFI_OPENAI_API_KEY 未设置"
  echo "   请在 .env 文件中设置或导出环境变量"
  echo ""
  echo "选择测试模式："
  echo "1. 跳过 AI 测试（仅测试并发控制）"
  echo "2. 继续测试（可能会失败）"
  read -p "请选择 [1/2]: " choice

  if [ "$choice" = "2" ]; then
    echo ""
    echo "使用 delay_test.yaml 代替..."
    WORKFLOW="delay_test"
  else
    echo ""
    echo "继续 AI 测试（可能因为 API key 问题失败）..."
    WORKFLOW="concurrent_ai_test"
  fi
else
  echo "✅ TOFI_OPENAI_API_KEY 已设置"
  WORKFLOW="concurrent_ai_test"
fi

echo ""
echo "测试工作流: $WORKFLOW"
echo ""

# 清理旧数据
echo "1. 清理旧的测试数据..."
pkill -f "tofi server" 2>/dev/null || true
sleep 1

# 生成测试 token
echo ""
echo "2. 生成测试 JWT Token..."
TOKEN=$(./tofi token -user jack | grep "eyJ" | head -1)
echo "Token: ${TOKEN:0:50}..."

# 启动服务器（并发数设置为 2，观察队列）
echo ""
echo "3. 启动服务器 (workers=2)..."
./tofi server -port 8080 -workers 2 > server_ai.log 2>&1 &
SERVER_PID=$!
echo "服务器 PID: $SERVER_PID"

# 等待服务器启动
sleep 3

# 查看初始统计
echo ""
echo "4. 查看工作池初始统计..."
curl -s http://localhost:8080/api/v1/stats | jq '.'

# 提交 AI 测试工作流
echo ""
echo "5. 提交并发 AI 测试工作流..."
EXEC_ID=$(curl -s -X POST http://localhost:8080/api/v1/run \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"workflow\": \"$WORKFLOW\", \"inputs\": {}}" | jq -r '.execution_id')

echo "Execution ID: $EXEC_ID"
echo "Status: queued"

# 监控队列状态
echo ""
echo "6. 监控工作池状态（5 个 AI 调用并发执行）..."
for i in {1..10}; do
  echo ""
  echo "--- 第 $i 次检查 ($(date +%H:%M:%S)) ---"
  curl -s http://localhost:8080/api/v1/stats | jq '{running_jobs, queued_jobs, queue_length}'
  sleep 2
done

# 等待任务完成
echo ""
echo "7. 等待任务完成..."
sleep 10

# 查看最终结果
echo ""
echo "8. 查看执行结果..."
curl -s http://localhost:8080/api/v1/executions/$EXEC_ID \
  -H "Authorization: Bearer $TOKEN" | jq '.status, .outputs'

# 查看日志
echo ""
echo "9. 查看执行日志..."
curl -s http://localhost:8080/api/v1/executions/$EXEC_ID/logs \
  -H "Authorization: Bearer $TOKEN" | tail -50

# 查看服务器日志中的并发信息
echo ""
echo "10. 服务器并发日志摘要..."
echo ""
grep -E "(Worker|开始执行|完成任务|队列)" server_ai.log | tail -40

# 清理
echo ""
echo "11. 清理测试环境..."
kill $SERVER_PID 2>/dev/null || true

echo ""
echo "=== 测试完成 ==="
echo "预期结果:"
echo "  - 5 个 AI 任务并发提交"
echo "  - 由于 workers=2，同时最多运行 2 个任务"
echo "  - 其他任务在队列中等待"
echo "  - 所有任务最终完成并返回 AI 响应"
