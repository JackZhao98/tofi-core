package tasks

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tofi-core/internal/models"
	"tofi-core/internal/storage"
)

type File struct{}

// FileOutput represents the structured output of a File node
// Note: content is NOT included in the output - it's resolved on-demand via ReplaceParams
// when downstream nodes reference {{file_node.content}}
type FileOutput struct {
	Path     string `json:"path"`
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
	FileID   string `json:"file_id"`
}

// Execute handles both upstream input and user-uploaded files.
// Behavior:
//   - If config["_input"] exists: data comes from upstream node
//   - Otherwise: data comes from user-uploaded file (config["file_path"])
//
// For upstream input, if save_to_disk is true, the content is written to artifacts directory.
// Output is always a JSON object: {"path": "...", "mime_type": "..."}
func (f *File) Execute(config map[string]interface{}, ctx *models.ExecutionContext) (string, error) {
	// Check if we have upstream input
	if upstreamData, ok := config["_input"]; ok && upstreamData != nil {
		return f.handleUpstreamInput(upstreamData, config, ctx)
	}

	// Otherwise, handle user-uploaded file
	return f.handleUserUpload(config, ctx)
}

// handleUpstreamInput processes data from an upstream node
// Always returns JSON with file metadata; content is resolved on-demand via {{node.content}}
func (f *File) handleUpstreamInput(data interface{}, config map[string]interface{}, ctx *models.ExecutionContext) (string, error) {
	content := fmt.Sprint(data)
	nodeID, _ := config["_node_id"].(string)
	if nodeID == "" {
		nodeID = "file"
	}

	// Check if we should save to disk
	saveToDisk, _ := config["save_to_disk"].(bool)

	if saveToDisk {
		// Ensure artifacts directory exists
		if err := os.MkdirAll(ctx.Paths.Artifacts, 0755); err != nil {
			return "", fmt.Errorf("failed to create artifacts dir: %v", err)
		}

		// Generate filename: {nodeId}_{timestamp}.{ext}
		ext := ".txt"
		var fileContent []byte
		mimeType := "text/plain"

		// Check for base64-encoded binary content
		if strings.HasPrefix(content, "base64:") {
			decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(content, "base64:"))
			if err != nil {
				return "", fmt.Errorf("failed to decode base64 content: %v", err)
			}
			fileContent = decoded
			// Detect extension from content
			mimeType = http.DetectContentType(decoded)
			ext = mimeToExt(mimeType)
		} else {
			fileContent = []byte(content)
		}

		filename := fmt.Sprintf("%s_%d%s", nodeID, time.Now().Unix(), ext)
		filePath := filepath.Join(ctx.Paths.Artifacts, filename)

		if err := os.WriteFile(filePath, fileContent, 0644); err != nil {
			return "", fmt.Errorf("failed to write file: %v", err)
		}

		mimeType = detectMimeType(filePath)
		ctx.Log("[File] Saved upstream data to %s (%d bytes, %s)", filename, len(fileContent), mimeType)

		// Return structured output as JSON (no content - resolved on demand)
		output := FileOutput{
			Path:     filePath,
			Filename: filename,
			MimeType: mimeType,
			Size:     int64(len(fileContent)),
			FileID:   "", // Artifacts don't have file_id
		}
		result, _ := json.Marshal(output)
		return string(result), nil
	}

	// Not saving to disk - return metadata with empty path
	// The actual content will be resolved via {{node.content}} special parsing
	ctx.Log("[File] Upstream data received (%d bytes, not saved to disk)", len(content))

	// Store the upstream content in execution context for later .content resolution
	// We use a special key pattern: _upstream_content_{nodeID}
	if ctx.UpstreamContent == nil {
		ctx.UpstreamContent = make(map[string]string)
	}
	ctx.UpstreamContent[nodeID] = content

	output := FileOutput{
		Path:     "",            // No path when not saved
		Filename: "",            // No filename
		MimeType: "text/plain",  // Assume text for upstream data
		Size:     int64(len(content)),
		FileID:   "",            // No file_id for upstream
	}
	result, _ := json.Marshal(output)
	return string(result), nil
}

// handleUserUpload processes a user-uploaded file
func (f *File) handleUserUpload(config map[string]interface{}, ctx *models.ExecutionContext) (string, error) {
	var realPath string
	var mimeType string
	var err error

	// Priority 1: Use file_id (new File ID system)
	fileID, hasFileID := config["file_id"].(string)
	if hasFileID && fileID != "" {
		realPath, mimeType, err = f.resolveFileID(fileID, ctx)
		if err != nil {
			return "", err
		}
		ctx.Log("[File] Resolved file_id %s -> %s (%s)", fileID, realPath, mimeType)
	} else {
		// Priority 2: Use file_path (legacy symlink approach)
		filePath, _ := config["file_path"].(string)

		// Validation
		if filePath == "" {
			return "", fmt.Errorf("no file linked (config.file_id or config.file_path is missing)")
		}

		// Build full symlink path
		// Path convention: $HOME/$USER/workflows/$WORKFLOW_ID/files/$SYMLINK_NAME
		symlinkPath := filepath.Join(
			ctx.Paths.Home, ctx.User, "workflows",
			ctx.WorkflowID, "files", filepath.Base(filePath),
		)

		// Verify symlink exists
		info, err := os.Lstat(symlinkPath)
		if err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("file link not found: %s", filePath)
			}
			return "", fmt.Errorf("failed to access file link: %v", err)
		}

		// Verify it's actually a symlink (or regular file for backward compat)
		if info.Mode()&os.ModeSymlink == 0 && !info.Mode().IsRegular() {
			return "", fmt.Errorf("file path is not a valid file or symlink: %s", filePath)
		}

		// Resolve symlink to get real path
		realPath = symlinkPath
		if info.Mode()&os.ModeSymlink != 0 {
			realPath, err = filepath.EvalSymlinks(symlinkPath)
			if err != nil {
				return "", fmt.Errorf("broken symlink (source file moved or deleted?): %v", err)
			}
		}

		// Verify target file exists
		if _, err := os.Stat(realPath); err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("source file no longer exists: %s", realPath)
			}
			return "", fmt.Errorf("failed to access source file: %v", err)
		}

		mimeType = detectMimeType(realPath)
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
			// Priority 1: Get extension from original filename (stored in config)
			// Priority 2: Get extension from realPath (works for new naming format)
			// Priority 3: Infer from MIME type
			var ext string
			if origFilename, ok := config["filename"].(string); ok && origFilename != "" {
				ext = strings.ToLower(filepath.Ext(origFilename))
			}
			if ext == "" {
				ext = strings.ToLower(filepath.Ext(realPath))
			}
			if ext == "" && mimeType != "" {
				ext = mimeToExt(mimeType)
			}

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

	// Get file info
	fileInfo, err := os.Stat(realPath)
	if err != nil {
		return "", fmt.Errorf("failed to get file info: %v", err)
	}

	// Get filename from config or extract from path
	filename, _ := config["filename"].(string)
	if filename == "" {
		filename = filepath.Base(realPath)
	}

	ctx.Log("[File] Resolved file -> %s (%s, %d bytes)", realPath, mimeType, fileInfo.Size())

	// Return structured output as JSON (no content - resolved on demand via {{node.content}})
	output := FileOutput{
		Path:     realPath,
		Filename: filename,
		MimeType: mimeType,
		Size:     fileInfo.Size(),
		FileID:   fileID, // May be empty for legacy file_path
	}
	result, _ := json.Marshal(output)
	return string(result), nil
}

// resolveFileID looks up a file by its file_id in the database
func (f *File) resolveFileID(fileID string, ctx *models.ExecutionContext) (string, string, error) {
	if ctx.DB == nil {
		return "", "", fmt.Errorf("database not available for file_id lookup")
	}

	db, ok := ctx.DB.(*storage.DB)
	if !ok {
		return "", "", fmt.Errorf("invalid database connection")
	}

	record, err := db.GetUserFile(ctx.User, fileID)
	if err != nil {
		return "", "", fmt.Errorf("file not found: %s", fileID)
	}

	// Build absolute path: $HOME/{user}/storage/files/{uuid}
	absPath := filepath.Join(ctx.Paths.Home, ctx.User, "storage", "files", record.UUID)

	// Verify file exists
	if _, err := os.Stat(absPath); err != nil {
		return "", "", fmt.Errorf("file storage corrupted, UUID %s not found on disk", record.UUID)
	}

	return absPath, record.MimeType, nil
}

func (f *File) Validate(node *models.Node) error {
	// We allow empty file_path during validation (it might be filled later or user forgot)
	// But we could warn. For now, strict validation isn't necessary as Execute handles missing path nicely.
	return nil
}

// detectMimeType detects the MIME type of a file by reading its first 512 bytes
func detectMimeType(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer file.Close()

	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil {
		return "application/octet-stream"
	}

	return http.DetectContentType(buffer[:n])
}

// mimeToExt converts a MIME type to a file extension
func mimeToExt(mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "image/png"):
		return ".png"
	case strings.HasPrefix(mimeType, "image/jpeg"):
		return ".jpg"
	case strings.HasPrefix(mimeType, "image/gif"):
		return ".gif"
	case strings.HasPrefix(mimeType, "image/webp"):
		return ".webp"
	case strings.HasPrefix(mimeType, "application/pdf"):
		return ".pdf"
	case strings.HasPrefix(mimeType, "application/json"):
		return ".json"
	case strings.HasPrefix(mimeType, "text/html"):
		return ".html"
	case strings.HasPrefix(mimeType, "text/plain"):
		return ".txt"
	case strings.HasPrefix(mimeType, "text/markdown"):
		return ".md"
	case strings.HasPrefix(mimeType, "text/csv"):
		return ".csv"
	case strings.HasPrefix(mimeType, "application/xml"), strings.HasPrefix(mimeType, "text/xml"):
		return ".xml"
	case strings.HasPrefix(mimeType, "application/yaml"), strings.HasPrefix(mimeType, "text/yaml"):
		return ".yaml"
	default:
		return ""
	}
}

// isTextExtension checks if a file extension is a text format
func isTextExtension(ext string) bool {
	textExtensions := map[string]bool{
		".txt": true, ".md": true, ".json": true, ".yaml": true, ".yml": true,
		".xml": true, ".html": true, ".htm": true, ".css": true, ".js": true,
		".ts": true, ".tsx": true, ".jsx": true, ".go": true, ".py": true,
		".java": true, ".c": true, ".cpp": true, ".h": true, ".rs": true,
		".sh": true, ".bash": true, ".zsh": true, ".csv": true, ".tsv": true,
		".log": true, ".ini": true, ".conf": true, ".cfg": true, ".toml": true,
		".sql": true, ".graphql": true, ".proto": true, ".env": true,
	}
	return textExtensions[ext]
}
