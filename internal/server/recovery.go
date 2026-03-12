package server

import (
	"log"
	"path/filepath"
	"tofi-core/internal/engine"
	"tofi-core/internal/parser"
)

// recoverAll is the unified recovery entry point called once at server startup.
// It handles all zombie/orphan states caused by server restarts.
func (s *Server) recoverAll() {
	log.Println("🔄 Starting unified recovery...")

	// 1. Kanban zombie cards: working → failed
	if n, err := s.db.RecoverZombieKanbanCards(); err != nil {
		log.Printf("⚠️  Kanban zombie recovery failed: %v", err)
	} else if n > 0 {
		log.Printf("🔄 Recovered %d kanban zombie cards (working → failed)", n)
	}

	// 2. Hold cards: hold → failed (hold channel lost after restart)
	s.recoverHoldCards()

	// 3. App runs: running → failed (dispatchRun goroutines killed by restart)
	if n, err := s.db.RecoverRunningAppRuns(); err != nil {
		log.Printf("⚠️  App runs zombie recovery failed: %v", err)
	} else if n > 0 {
		log.Printf("🔄 Recovered %d zombie app_runs (running → failed)", n)
	}

	// 4. Workflow executions: RUNNING → resume via workerPool
	s.recoverWorkflowExecutions()

	log.Println("✅ Unified recovery complete")
}

// recoverHoldCards marks orphaned hold cards as failed (channel lost after restart)
func (s *Server) recoverHoldCards() {
	rows, err := s.db.QueryHoldCards()
	if err != nil {
		log.Printf("⚠️  Hold card recovery query failed: %v", err)
		return
	}
	for _, id := range rows {
		log.Printf("🔄 Recovering hold card %s → failed", id)
		s.db.UpdateKanbanCardStatus(id, "failed")
	}
}

// recoverWorkflowExecutions resumes interrupted workflow executions via worker pool
func (s *Server) recoverWorkflowExecutions() {
	zombies, err := s.db.ListRunningExecutions()
	if err != nil {
		log.Printf("⚠️  Workflow zombie scan failed: %v", err)
		return
	}

	if len(zombies) == 0 {
		return
	}

	log.Printf("⚠️  Found %d zombie workflow executions, recovering...", len(zombies))

	for _, record := range zombies {
		execID := record.ID
		log.Printf("🔄 Recovering execution: %s (workflow: %s, user: %s)", execID, record.WorkflowName, record.User)

		ctx, err := engine.LoadState(execID, s.db, s.config.HomeDir)
		if err != nil {
			s.db.UpdateStatus(execID, "FAILED")
			log.Printf("❌ Execution %s recovery failed: %v", execID, err)
			continue
		}

		workflowRef := record.WorkflowID
		if workflowRef == "" {
			workflowRef = record.WorkflowName
		}

		userWorkflowDir := filepath.Join(s.config.HomeDir, record.User, "workflows")
		wf, err := parser.ResolveWorkflow(workflowRef, userWorkflowDir)
		if err != nil {
			s.db.UpdateStatus(execID, "FAILED")
			ctx.Log("Recovery failed: cannot load workflow definition (%v)", err)
			log.Printf("❌ Execution %s recovery failed: %v", execID, err)
			continue
		}

		job := &WorkflowJob{
			ExecutionID:   execID,
			Workflow:      wf,
			Context:       ctx,
			InitialInputs: nil,
			DB:            s.db,
		}

		if err := s.workerPool.Submit(job); err != nil {
			log.Printf("❌ Execution %s submit failed: %v", execID, err)
			s.db.UpdateStatus(execID, "FAILED")
			continue
		}

		log.Printf("✅ Execution %s submitted to worker pool for recovery", execID)
	}
}
