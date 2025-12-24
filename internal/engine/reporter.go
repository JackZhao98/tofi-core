package engine

import (
	"fmt"
	"strings"
	"tofi-core/internal/models"
)

func PrintSummary(ctx *models.ExecutionContext) {
	fmt.Println("\n" + strings.Repeat("=", 75))
	fmt.Printf("%-20s | %-10s | %-12s | %-15s\n", "NODE ID", "TYPE", "STATUS", "DURATION")
	fmt.Println(strings.Repeat("-", 75))

	for _, stat := range ctx.Stats {
		statusStr := stat.Status
		// 为终端增加一点颜色感
		switch stat.Status {
		case "SUCCESS":
			statusStr = "\033[32mSUCCESS\033[0m"
		case "ERROR":
			statusStr = "\033[31mERROR\033[0m"
		case "SKIP":
			statusStr = "\033[33mSKIP\033[0m"
		}

		fmt.Printf("%-20s | %-10s | %-22s | %-15s\n",
			stat.NodeID,
			stat.Type,
			statusStr,
			stat.Duration.String(),
		)
	}
	fmt.Println(strings.Repeat("=", 75))
}
