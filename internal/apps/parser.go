package apps

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// nameRegex 验证 App 名称格式（与 Skill 一致）
var nameRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ParseDir 从目录解析完整 App 包（APP.md + SYSTEM_PROMPT.md + skills.txt + scripts/）
func ParseDir(dir string) (*AppFile, error) {
	appMDPath := filepath.Join(dir, "APP.md")
	if _, err := os.Stat(appMDPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("APP.md not found in %s", dir)
	}

	// 1. 解析 APP.md
	data, err := os.ReadFile(appMDPath)
	if err != nil {
		return nil, fmt.Errorf("read APP.md: %w", err)
	}

	app, err := ParseAppMD(data)
	if err != nil {
		return nil, fmt.Errorf("parse APP.md: %w", err)
	}
	app.Dir = dir

	// 2. 读取 SYSTEM_PROMPT.md（可选）
	spPath := filepath.Join(dir, "SYSTEM_PROMPT.md")
	if spData, err := os.ReadFile(spPath); err == nil {
		app.SystemPrompt = strings.TrimSpace(string(spData))
	}

	// 3. 读取 skills.txt（可选）
	skillsPath := filepath.Join(dir, "skills.txt")
	if skillsData, err := os.ReadFile(skillsPath); err == nil {
		app.Skills = parseSkillsTxt(string(skillsData))
	}

	// 4. 扫描 scripts/ 目录
	scriptsDir := filepath.Join(dir, "scripts")
	if entries, err := os.ReadDir(scriptsDir); err == nil {
		app.HasScripts = true
		for _, e := range entries {
			if !e.IsDir() {
				app.ScriptDirs = append(app.ScriptDirs, e.Name())
			}
		}
	}

	return app, nil
}

// ParseAppMD 从字节数据解析 APP.md（YAML frontmatter + Markdown body）
func ParseAppMD(data []byte) (*AppFile, error) {
	content := string(data)

	frontmatter, body, err := splitFrontmatter(content)
	if err != nil {
		return nil, err
	}

	var manifest AppManifest
	if err := yaml.Unmarshal([]byte(frontmatter), &manifest); err != nil {
		return nil, fmt.Errorf("invalid YAML frontmatter: %w", err)
	}

	if err := validateManifest(&manifest); err != nil {
		return nil, err
	}

	return &AppFile{
		Manifest: manifest,
		Prompt:   strings.TrimSpace(body),
	}, nil
}

// splitFrontmatter 将内容分为 YAML frontmatter 和 Markdown body
func splitFrontmatter(content string) (frontmatter, body string, err error) {
	content = strings.TrimSpace(content)

	if !strings.HasPrefix(content, "---") {
		return "", "", fmt.Errorf("APP.md must start with '---' (YAML frontmatter delimiter)")
	}

	rest := content[3:]
	if idx := strings.IndexByte(rest, '\n'); idx >= 0 {
		rest = rest[idx+1:]
	}

	endIdx := strings.Index(rest, "\n---")
	if endIdx < 0 {
		endIdx = strings.Index(rest, "\r\n---")
		if endIdx < 0 {
			return "", "", fmt.Errorf("APP.md missing closing '---' for YAML frontmatter")
		}
	}

	frontmatter = rest[:endIdx]
	afterDelimiter := rest[endIdx+4:] // skip \n---
	body = strings.TrimLeft(afterDelimiter, "\r\n")

	return frontmatter, body, nil
}

// validateManifest 验证 AppManifest 的必填字段
func validateManifest(m *AppManifest) error {
	if m.Name == "" {
		return fmt.Errorf("app 'name' is required")
	}
	if len(m.Name) > 64 {
		return fmt.Errorf("app 'name' must be at most 64 characters, got %d", len(m.Name))
	}
	if !nameRegex.MatchString(m.Name) {
		return fmt.Errorf("app 'name' must be lowercase alphanumeric with hyphens: %q", m.Name)
	}
	if strings.Contains(m.Name, "--") {
		return fmt.Errorf("app 'name' must not contain consecutive hyphens: %q", m.Name)
	}
	if m.Description == "" {
		return fmt.Errorf("app 'description' is required")
	}
	if len(m.Description) > 1024 {
		return fmt.Errorf("app 'description' must be at most 1024 characters, got %d", len(m.Description))
	}
	// 验证参数定义
	for name, param := range m.Parameters {
		if err := validateParameter(name, param); err != nil {
			return err
		}
	}
	return nil
}

// validateParameter 验证单个参数定义
func validateParameter(name string, p *AppParameter) error {
	validTypes := map[string]bool{"text": true, "number": true, "select": true, "boolean": true}
	if !validTypes[p.Type] {
		return fmt.Errorf("parameter %q has invalid type %q (must be text/number/select/boolean)", name, p.Type)
	}
	if p.Type == "select" && len(p.Options) == 0 {
		return fmt.Errorf("parameter %q of type 'select' must have options", name)
	}
	return nil
}

// parseSkillsTxt 解析 skills.txt 内容（每行一个，# 开头的为注释）
func parseSkillsTxt(content string) []string {
	var skills []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		skills = append(skills, line)
	}
	return skills
}
