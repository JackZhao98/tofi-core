package tasks

import (
	"fmt"
	"path/filepath"
	"strings"
	"tofi-core/internal/models"
)

type File struct{}

// DBInterface defines the subset of DB methods we need
// This avoids importing storage directly if we want to keep dependencies clean,
// but since we need the models anyway, we can just define the interface here.
type DBInterface interface {
	GetUserFile(user, fileID string) (*models.UserFileRecord, error)
}

func (f *File) Execute(config map[string]interface{}, ctx *models.ExecutionContext) (string, error) {
	fileID, _ := config["file_id"].(string)

	// Validation
	if fileID == "" {
		return "", fmt.Errorf("no file uploaded (config.file_id is missing)")
	}

	// Resolve DB
	db, ok := ctx.DB.(DBInterface)
	if !ok {
		return "", fmt.Errorf("database connection not available in execution context")
	}

	// Lookup file
	fileRecord, err := db.GetUserFile(ctx.User, fileID)
	if err != nil {
		return "", fmt.Errorf("file not found: %s (error: %v)", fileID, err)
	}

	// Check Accepted Extensions
	if acceptRaw, ok := config["accept"]; ok {
		var acceptedExts []string

		switch v := acceptRaw.(type) {
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok {
					acceptedExts = append(acceptedExts, strings.ToLower(s))
				}
			}
		case []string:
			for _, s := range v {
				acceptedExts = append(acceptedExts, strings.ToLower(s))
			}
		case string:
			parts := strings.Split(v, ",")
			for _, part := range parts {
				if trimmed := strings.TrimSpace(part); trimmed != "" {
					acceptedExts = append(acceptedExts, strings.ToLower(trimmed))
				}
			}
		}

		if len(acceptedExts) > 0 {
			ext := strings.ToLower(filepath.Ext(fileRecord.OriginalFilename))
			matched := false
			for _, acc := range acceptedExts {
				if ext == acc {
					matched = true
					break
				}
			}
			if !matched {
				return "", fmt.Errorf("file type not allowed: %s (expected: %v)", ext, acceptedExts)
			}
		}
	}

	// Construct Absolute Path
	// Path convention: $HOME/$USER/storage/files/$UUID
	absPath := filepath.Join(ctx.Paths.Home, ctx.User, "storage", "files", fileRecord.UUID)

	ctx.Log("[File] Resolved file '%s' (%s) -> %s", fileID, fileRecord.OriginalFilename, absPath)
	return absPath, nil
}

func (f *File) Validate(node *models.Node) error {
	// We allow empty file_id during validation (it might be filled later or user forgot)
	// But we could warn. For now, strict validation isn't necessary as Execute handles missing ID nicely.
	return nil
}
