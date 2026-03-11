package apps

// manifest.go — App 数据模型（APP.md manifest 相关类型）

// AppManifest 表示解析后的 APP.md YAML frontmatter
type AppManifest struct {
	// === Meta ===
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	Version     string `yaml:"version,omitempty" json:"version,omitempty"`
	Author      string `yaml:"author,omitempty" json:"author,omitempty"`

	// === 运行时 ===
	Model string `yaml:"model,omitempty" json:"model,omitempty"`

	// === 用户参数 ===
	Parameters map[string]*AppParameter `yaml:"parameters,omitempty" json:"parameters,omitempty"`

	// === 依赖 ===
	RequiredSecrets []string               `yaml:"required_secrets,omitempty" json:"required_secrets,omitempty"`
	Capabilities    map[string]any `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`

	// === 调度 ===
	Schedule         *AppSchedule `yaml:"schedule,omitempty" json:"schedule,omitempty"`
	BufferSize       int          `yaml:"buffer_size,omitempty" json:"buffer_size,omitempty"`
	RenewalThreshold int          `yaml:"renewal_threshold,omitempty" json:"renewal_threshold,omitempty"`
}

// AppParameter 定义 App 的一个用户参数
type AppParameter struct {
	Type        string   `yaml:"type" json:"type"`                                   // text | number | select | boolean
	Description string   `yaml:"description" json:"description"`
	Required    bool     `yaml:"required" json:"required"`
	Default     string   `yaml:"default,omitempty" json:"default,omitempty"`
	Options     []string `yaml:"options,omitempty" json:"options,omitempty"` // type=select 时的选项
}

// AppSchedule 定义 App 的调度规则
type AppSchedule struct {
	Rules    []AppScheduleRule `yaml:"rules" json:"rules"`
	Timezone string            `yaml:"timezone,omitempty" json:"timezone,omitempty"`
}

// AppScheduleRule 单条调度规则
type AppScheduleRule struct {
	Days    []string         `yaml:"days" json:"days"`
	Windows []AppTimeWindow  `yaml:"windows" json:"windows"`
}

// AppTimeWindow 时间窗口
type AppTimeWindow struct {
	Start       string `yaml:"start" json:"start"`
	End         string `yaml:"end" json:"end"`
	IntervalMin int    `yaml:"interval_min" json:"interval_min"`
}

// AppFile 表示完整的解析后 App 包
type AppFile struct {
	Manifest     AppManifest `json:"manifest"`
	Prompt       string      `json:"prompt"`        // APP.md body（任务 prompt 模板）
	SystemPrompt string      `json:"system_prompt"` // SYSTEM_PROMPT.md 内容
	Skills       []string    `json:"skills"`        // skills.txt 中的依赖列表

	// 文件系统信息（运行时填充）
	Dir        string   `json:"dir,omitempty"`
	HasScripts bool     `json:"has_scripts,omitempty"`
	ScriptDirs []string `json:"scripts,omitempty"`
}
