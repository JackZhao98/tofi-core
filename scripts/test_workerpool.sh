#!/bin/bash

# Worker Pool 测试脚本

set -e

echo "=== Tofi Worker Pool 功能测试 ==="
echo ""

# 清理旧数据
echo "1. 清理旧的测试数据..."
pkill -f "tofi server" 2>/dev/null || true
sleep 1
rm -rf .tofi/jack 2>/dev/null || true
rm -f .tofi/tofi.db 2>/dev/null || true

# 生成测试 token
echo ""
echo "2. 生成测试 JWT Token..."
TOKEN=$(./tofi token -user jack | grep "eyJ" | head -1)
echo "Token: ${TOKEN:0:50}..."

# 启动服务器（并发数设置为 2）
echo ""
echo "3. 启动服务器 (workers=2)..."
./tofi server -port 8080 -workers 2 > server_pool.log 2>&1 &
SERVER_PID=$!
echo "服务器 PID: $SERVER_PID"

# 等待服务器启动
sleep 3

# 查看初始统计
echo ""
echo "4. 查看工作池初始统计..."
curl -s http://localhost:8080/api/v1/stats | jq '.'

# 创建一个延迟工作流用于测试
cat > workflows/delay_test.yaml << 'EOF'
name: delay_test

nodes:
  step1:
    type: shell
    config:
      script: |
        echo "Task started: ${TASK_ID}"
        sleep 5
        echo "Task completed: ${TASK_ID}"
    env:
      TASK_ID: "{{data.task_id}}"
EOF

# 快速提交 5 个任务（超过并发数）
echo ""
echo "5. 快速提交 5 个任务（并发限制为 2）..."
for i in {1..5}; do
  echo "提交任务 $i..."
  curl -s -X POST http://localhost:8080/api/v1/run \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"workflow\": \"delay_test\", \"inputs\": {\"data\": {\"task_id\": \"task-$i\"}}}" | jq '.execution_id, .status'

  sleep 0.2  # 避免太快提交
done

# 查看队列状态
echo ""
echo "6. 查看工作池统计（应该有任务排队）..."
sleep 2
curl -s http://localhost:8080/api/v1/stats | jq '.'

# 等待任务完成
echo ""
echo "7. 等待所有任务完成（约 15 秒）..."
sleep 16

# 查看最终统计
echo ""
echo "8. 查看最终工作池统计..."
curl -s http://localhost:8080/api/v1/stats | jq '.'

# 检查日志
echo ""
echo "9. 查看服务器日志摘要..."
echo ""
grep -E "(Worker|队列|工作池|任务)" server_pool.log | tail -30

# 清理
echo ""
echo "10. 清理测试环境..."
kill $SERVER_PID 2>/dev/null || true
rm -f workflows/delay_test.yaml

echo ""
echo "=== 测试完成 ==="
echo "预期结果:"
echo "  - max_workers: 2"
echo "  - 同时运行的任务不超过 2 个"
echo "  - 其他任务会在队列中等待"
echo "  - 所有 5 个任务最终都会完成"
