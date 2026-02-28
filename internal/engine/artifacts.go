package engine

import (
	"net/http"
	"os"
	"path/filepath"
	"tofi-core/internal/models"
	"tofi-core/internal/pkg/logger"
	"tofi-core/internal/storage"
)

// ScanArtifacts walks the artifacts directory after workflow completion
// and records all found files to the database.
func ScanArtifacts(ctx *models.ExecutionContext) {
	artDir := ctx.Paths.Artifacts
	if artDir == "" {
		return
	}
	if _, err := os.Stat(artDir); os.IsNotExist(err) {
		return
	}

	db, ok := ctx.DB.(*storage.DB)
	if !ok || db == nil {
		return
	}

	count := 0
	filepath.Walk(artDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Log the error instead of silently swallowing it
			logger.Printf("[%s] [ARTIFACTS] Error accessing %s: %v", ctx.ExecutionID, path, err)
			return nil // Continue walking other files
		}
		if info.IsDir() {
			return nil
		}

		// Detect MIME type from file header
		mimeType := "application/octet-stream"
		f, ferr := os.Open(path)
		if ferr == nil {
			buf := make([]byte, 512)
			n, _ := f.Read(buf)
			f.Close()
			if n > 0 {
				mimeType = http.DetectContentType(buf[:n])
			}
		}

		// Relative path from artifacts dir
		relPath, _ := filepath.Rel(artDir, path)

		if err := db.RecordArtifact(ctx.ExecutionID, info.Name(), relPath, mimeType, info.Size()); err != nil {
			logger.Printf("[%s] [ARTIFACTS] Failed to record %s: %v", ctx.ExecutionID, info.Name(), err)
		} else {
			count++
		}
		return nil
	})

	if count > 0 {
		logger.Printf("[%s] [ARTIFACTS] Recorded %d artifact(s)", ctx.ExecutionID, count)
	}
}
