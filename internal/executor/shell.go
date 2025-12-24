package executor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ExecuteShell 现在接受 timeout 和 env 参数
func ExecuteShell(script string, env map[string]string, timeout int) (string, error) {
	// 如果没设置超时，默认给 30 秒，防止永久挂起
	if timeout <= 0 {
		timeout = 30
	}

	// 创建一个带超时的 Context
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	// 使用 CommandContext，这样超时后 Go 会自动 Kill 掉这个子进程
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	
		// 注入环境变量
	
		if len(env) > 0 {
	
			cmd.Env = append(cmd.Environ(), toEnvList(env)...)
	
		}
	
	
	
		// 0. 安全检查 (Static Analysis)
	
		if err := CheckShellSafety(script); err != nil {
	
			return "", fmt.Errorf("安全检查未通过: %v", err)
	
		}
	
	
	
		output, err := cmd.CombinedOutput() // 同时获取 stdout 和 stderr
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("执行超时 (%d秒)", timeout)
		}
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

// toEnvList 将 map 转换为 KEY=VALUE 格式的切片
func toEnvList(env map[string]string) []string {
	var list []string
	for k, v := range env {
		list = append(list, fmt.Sprintf("%s=%s", k, v))
	}
	return list
}