package tasks

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"tofi-core/internal/models"
)

type Save struct{}

func (s *Save) Execute(config map[string]interface{}, ctx *models.ExecutionContext) (string, error) {
	content := fmt.Sprint(config["content"])

	filename, _ := config["filename"].(string)
	if filename == "" {
		filename = "output.txt"
	}

	// Ensure artifacts directory exists
	if err := os.MkdirAll(ctx.Paths.Artifacts, 0755); err != nil {
		return "", fmt.Errorf("failed to create artifacts dir: %v", err)
	}

	// Sanitize filename (only use base name to prevent path traversal)
	filePath := filepath.Join(ctx.Paths.Artifacts, filepath.Base(filename))

	// Write content: base64-prefixed content is decoded as binary
	var bytesWritten int
	if strings.HasPrefix(content, "base64:") {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(content, "base64:"))
		if err != nil {
			return "", fmt.Errorf("failed to decode base64 content: %v", err)
		}
		bytesWritten = len(decoded)
		if err := os.WriteFile(filePath, decoded, 0644); err != nil {
			return "", fmt.Errorf("failed to write file: %v", err)
		}
	} else {
		bytesWritten = len(content)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return "", fmt.Errorf("failed to write file: %v", err)
		}
	}

	ctx.Log("[Save] Wrote %s (%d bytes)", filepath.Base(filename), bytesWritten)
	return filePath, nil
}

func (s *Save) Validate(node *models.Node) error {
	return nil
}
