#!/bin/bash

# 僵尸恢复测试脚本

set -e

echo "=== Tofi 僵尸恢复功能测试 ==="
echo ""

# 清理旧数据
echo "1. 清理旧的测试数据..."
rm -rf .tofi/jack 2>/dev/null || true
rm -f .tofi/tofi.db 2>/dev/null || true

# 生成测试 token
echo ""
echo "2. 生成测试 JWT Token..."
TOKEN=$(./tofi token -user jack)
echo "Token: $TOKEN"

# 启动服务器（后台）
echo ""
echo "3. 启动服务器..."
./tofi server -port 8080 > server.log 2>&1 &
SERVER_PID=$!
echo "服务器 PID: $SERVER_PID"

# 等待服务器启动
sleep 2

# 触发工作流
echo ""
echo "4. 触发测试工作流..."
EXEC_ID=$(curl -s -X POST http://localhost:8080/api/v1/run \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "workflow": "zombie_recovery_test",
    "inputs": {}
  }' | grep -o '"execution_id":"[^"]*"' | cut -d'"' -f4)

echo "Execution ID: $EXEC_ID"

# 等待部分节点完成
echo ""
echo "5. 等待 5 秒让部分节点完成..."
sleep 5

# 强制终止服务器（模拟崩溃）
echo ""
echo "6. 强制终止服务器（模拟崩溃）..."
kill -9 $SERVER_PID
sleep 1

# 检查数据库状态
echo ""
echo "7. 检查数据库中的任务状态..."
sqlite3 .tofi/tofi.db "SELECT id, workflow_name, status, created_at FROM executions WHERE id='$EXEC_ID';"

# 重新启动服务器
echo ""
echo "8. 重新启动服务器（应该自动恢复僵尸任务）..."
./tofi server -port 8080 > server_restart.log 2>&1 &
NEW_SERVER_PID=$!
echo "新服务器 PID: $NEW_SERVER_PID"

# 等待恢复和执行完成
echo ""
echo "9. 等待 10 秒让任务恢复并完成..."
sleep 10

# 查看最终状态
echo ""
echo "10. 查看最终执行状态..."
curl -s http://localhost:8080/api/v1/executions/$EXEC_ID \
  -H "Authorization: Bearer $TOKEN" | jq '.'

# 查看日志
echo ""
echo "11. 查看执行日志..."
curl -s http://localhost:8080/api/v1/executions/$EXEC_ID/logs \
  -H "Authorization: Bearer $TOKEN"

# 清理
echo ""
echo "12. 清理测试环境..."
kill $NEW_SERVER_PID 2>/dev/null || true

echo ""
echo "=== 测试完成 ==="
echo "请检查日志中是否有 '♻️  从僵尸状态恢复执行' 的提示"
