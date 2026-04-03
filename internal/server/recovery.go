package server

import "log"

// recoverAll is the unified recovery entry point called once at server startup.
// It handles zombie/orphan states caused by server restarts.
func (s *Server) recoverAll() {
	log.Println("Starting unified recovery...")

	// App runs: running → failed (dispatchRun goroutines killed by restart)
	if n, err := s.db.RecoverRunningAppRuns(); err != nil {
		log.Printf("App runs zombie recovery failed: %v", err)
	} else if n > 0 {
		log.Printf("Recovered %d zombie app_runs (running → failed)", n)
	}

	log.Println("Unified recovery complete")
}
