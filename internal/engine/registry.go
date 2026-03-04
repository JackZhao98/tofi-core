package engine

import (
	"fmt"
	"sync"
	"tofi-core/internal/engine/data"
	"tofi-core/internal/engine/logic"
	"tofi-core/internal/engine/tasks"
)

// ActionRegistry 动态管理节点类型到 Action 实现的映射
// 替代原有的硬编码 switch 语句，支持运行时注册新节点类型
type ActionRegistry struct {
	mu      sync.RWMutex
	actions map[string]Action
}

// globalRegistry 全局单例，所有 GetAction 调用通过此实例路由
var globalRegistry = NewRegistry()

// NewRegistry 创建一个空的 ActionRegistry
func NewRegistry() *ActionRegistry {
	return &ActionRegistry{
		actions: make(map[string]Action),
	}
}

// Register 注册一个节点类型到对应的 Action 实现
// 如果类型已存在会被覆盖（允许插件替换内置实现）
func (r *ActionRegistry) Register(nodeType string, action Action) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.actions[nodeType] = action
}

// Get 获取指定类型的 Action 实现
func (r *ActionRegistry) Get(nodeType string) (Action, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	action, ok := r.actions[nodeType]
	return action, ok
}

// List 返回所有已注册的节点类型
func (r *ActionRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]string, 0, len(r.actions))
	for t := range r.actions {
		types = append(types, t)
	}
	return types
}

// Has 检查是否已注册某个节点类型
func (r *ActionRegistry) Has(nodeType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.actions[nodeType]
	return ok
}

// RegisterBuiltins 注册所有内置节点类型
// 这是一对一映射原有 GetAction switch 的所有 case
func (r *ActionRegistry) RegisterBuiltins() {
	// Intelligence
	r.Register("ai", &tasks.AI{})

	// Tasks
	r.Register("shell", &tasks.Shell{})
	r.Register("hold", &tasks.Hold{})
	r.Register("file", &tasks.File{})
	r.Register("save", &tasks.Save{})
	r.Register("workflow", &tasks.Handoff{})

	// Integration
	r.Register("api", &tasks.API{})
	r.Register("notify", &tasks.Notify{})

	// Skills (Agent Skills ecosystem)
	r.Register("skill", &tasks.Skill{})

	// Logic
	r.Register("check", &logic.Check{})
	r.Register("compare", &logic.Compare{})
	r.Register("branch", &logic.Branch{})
	r.Register("loop", &logic.Loop{})

	// Data
	r.Register("var", &data.Var{})
	r.Register("const", &data.Var{}) // alias for var
	r.Register("dict", &data.Dict{})
	r.Register("secret", &data.Secret{})
}

// GlobalRegistry 返回全局 Registry 实例
// 供外部包（如 server）在运行时动态注册新节点类型
func GlobalRegistry() *ActionRegistry {
	return globalRegistry
}

func init() {
	// 启动时自动注册所有内置节点
	globalRegistry.RegisterBuiltins()
	fmt.Printf("[registry] %d built-in actions registered\n", len(globalRegistry.actions))
}
