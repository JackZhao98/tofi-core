package server

import (
	"sync"
	"tofi-core/internal/models"
)

// ExecutionRegistry 管理当前正在运行的执行上下文
type ExecutionRegistry struct {
	activeExecutions map[string]*models.ExecutionContext
	mu               sync.RWMutex
}

func NewExecutionRegistry() *ExecutionRegistry {
	return &ExecutionRegistry{
		activeExecutions: make(map[string]*models.ExecutionContext),
	}
}

func (r *ExecutionRegistry) Register(id string, ctx *models.ExecutionContext) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.activeExecutions[id] = ctx
}

func (r *ExecutionRegistry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.activeExecutions, id)
}

func (r *ExecutionRegistry) Get(id string) (*models.ExecutionContext, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ctx, ok := r.activeExecutions[id]
	return ctx, ok
}
