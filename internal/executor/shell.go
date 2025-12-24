package executor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ExecuteShell 现在接受一个 timeout 参数
func ExecuteShell(script string, timeout int) (string, error) {
	// 如果没设置超时，默认给 30 秒，防止永久挂起
	if timeout <= 0 {
		timeout = 30
	}

	// 创建一个带超时的 Context
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	// 使用 CommandContext，这样超时后 Go 会自动 Kill 掉这个子进程
	cmd := exec.CommandContext(ctx, "sh", "-c", script)

	output, err := cmd.CombinedOutput() // 同时获取 stdout 和 stderr
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("执行超时 (%d秒)", timeout)
		}
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}
