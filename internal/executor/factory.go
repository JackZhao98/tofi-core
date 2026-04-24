package executor

import (
	"log"
	"os/exec"
	"runtime"

	"tofi-core/internal/paths"
)

// NewExecutor picks the right executor for this host. On Linux with
// `runsc` in PATH, returns a GvisorExecutor. Everywhere else (macOS dev
// box, Linux without gVisor installed), returns a DevExecutor with a
// loud banner — that path is development-only, not production-safe.
//
// An empty homeDir falls back to paths.TofiHome().
func NewExecutor(homeDir string) Executor {
	if homeDir == "" {
		homeDir = paths.TofiHome()
	}
	if runtime.GOOS == "linux" {
		if runsc, err := exec.LookPath("runsc"); err == nil {
			g, err := NewGvisorExecutor(homeDir, runsc)
			if err == nil {
				log.Printf("🛡️  [sandbox] gVisor executor active (runsc=%s)", runsc)
				return g
			}
			log.Printf("⚠️  [sandbox] runsc found but init failed: %v — falling back to DevExecutor", err)
		}
	}
	logDevExecutorBanner()
	return NewDirectExecutor(homeDir)
}

func logDevExecutorBanner() {
	log.Println("┌─────────────────────────────────────────────────────────────┐")
	log.Println("│  ⚠️  INSECURE DEV EXECUTOR                                  │")
	log.Println("│  runsc/gVisor unavailable — sandbox is cmd.Dir + seatbelt.  │")
	log.Println("│  NEVER run this build in production with real users.        │")
	log.Println("└─────────────────────────────────────────────────────────────┘")
}
