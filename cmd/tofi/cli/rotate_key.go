package cli

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"

	"tofi-core/internal/crypto"
)

// v0.8.x and earlier shipped a public default encryption key that was used
// whenever TOFI_ENCRYPTION_KEY was not set. v0.9.0 refuses to start without
// the env var (unless TOFI_DEV=1). Operators who deployed v0.8.x in dev
// mode and now want to move to a real key run this command, with --old-key
// defaulting to the value below so they don't have to type it out.
const defaultDevEncryptionKey = "tofi-default-encryption-key!!123"

var (
	rotateOldKey string
	rotateNewKey string
	rotateDryRun bool
	rotateYes    bool
)

var rotateEncKeyCmd = &cobra.Command{
	Use:   "rotate-encryption-key",
	Short: "Re-encrypt all stored secrets with a new TOFI_ENCRYPTION_KEY",
	Long: `Rotate the encryption key used for the secrets table (AI provider
keys and any user-configured secrets).

The typical upgrade path from v0.8.x is:

  1. Stop the tofi engine (` + "`tofi stop`" + `).
  2. Generate a new 32-byte key:
       openssl rand -base64 24
  3. Run this command to re-encrypt existing rows:
       tofi rotate-encryption-key --new-key "<NEW_KEY>"
     (--old-key defaults to the v0.8.x public fallback key.)
  4. Set TOFI_ENCRYPTION_KEY=<NEW_KEY> in your environment.
  5. Remove TOFI_DEV=1 if you set it as a stopgap.
  6. Start the engine (` + "`tofi start`" + `).

Use --dry-run first to verify every row decrypts with the old key.`,
	RunE: runRotateEncryptionKey,
}

func init() {
	rotateEncKeyCmd.Flags().StringVar(&rotateOldKey, "old-key", "",
		"old 32-byte key (defaults to the v0.8.x public fallback if unset)")
	rotateEncKeyCmd.Flags().StringVar(&rotateNewKey, "new-key", "",
		"new 32-byte key; or set TOFI_NEW_ENCRYPTION_KEY in the environment")
	rotateEncKeyCmd.Flags().BoolVar(&rotateDryRun, "dry-run", false,
		"decrypt every row with the old key to verify, but do not re-encrypt")
	rotateEncKeyCmd.Flags().BoolVar(&rotateYes, "yes", false,
		"skip the interactive 'rotate' confirmation prompt")
	rootCmd.AddCommand(rotateEncKeyCmd)
}

func runRotateEncryptionKey(cmd *cobra.Command, args []string) error {
	oldKey := rotateOldKey
	usingDefaultOldKey := false
	if oldKey == "" {
		oldKey = defaultDevEncryptionKey
		usingDefaultOldKey = true
	}
	if len(oldKey) != 32 {
		return fmt.Errorf("old key must be exactly 32 bytes, got %d", len(oldKey))
	}

	newKey := rotateNewKey
	if newKey == "" {
		newKey = os.Getenv("TOFI_NEW_ENCRYPTION_KEY")
	}
	if !rotateDryRun {
		if newKey == "" {
			return fmt.Errorf("--new-key is required (or set TOFI_NEW_ENCRYPTION_KEY); use --dry-run to only verify the old key")
		}
		if len(newKey) != 32 {
			return fmt.Errorf("new key must be exactly 32 bytes, got %d", len(newKey))
		}
		if newKey == oldKey {
			return fmt.Errorf("new key is identical to old key — nothing to rotate")
		}
	}

	dbPath := filepath.Join(homeDir, "tofi.db")
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("database not found at %s: %w", dbPath, err)
	}

	// Bypass storage.InitDB on purpose — that path triggers migrations and
	// expects the package-level crypto key to be initialised, which would
	// fight with the two-key rotation we're doing here.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	var total int
	if err := db.QueryRow("SELECT COUNT(*) FROM secrets").Scan(&total); err != nil {
		return fmt.Errorf("count secrets: %w", err)
	}
	if total == 0 {
		fmt.Println("No rows in `secrets` table — nothing to rotate.")
		return nil
	}

	fmt.Printf("Database:    %s\n", dbPath)
	fmt.Printf("Rows:        %d\n", total)
	if usingDefaultOldKey {
		fmt.Println("Old key:     <v0.8.x public fallback>")
	} else {
		fmt.Printf("Old key:     %s… (%d bytes)\n", oldKey[:8], len(oldKey))
	}
	if rotateDryRun {
		fmt.Println("New key:     <dry run — not used>")
	} else {
		fmt.Printf("New key:     %s… (%d bytes)\n", newKey[:8], len(newKey))
	}
	fmt.Println()

	if !rotateYes && !rotateDryRun {
		fmt.Println("⚠  Make sure the tofi engine is stopped before proceeding.")
		fmt.Print("Type 'rotate' to continue: ")
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(answer) != "rotate" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	rows, err := db.Query("SELECT id, encrypted_value FROM secrets")
	if err != nil {
		return fmt.Errorf("query secrets: %w", err)
	}

	type reEnc struct {
		ID       string
		NewValue string
	}
	var updates []reEnc
	var failed []string
	scanned := 0

	for rows.Next() {
		var id, enc string
		if err := rows.Scan(&id, &enc); err != nil {
			rows.Close()
			return fmt.Errorf("scan row: %w", err)
		}
		scanned++
		plain, err := crypto.DecryptWithKey([]byte(oldKey), enc)
		if err != nil {
			failed = append(failed, id)
			fmt.Printf("  ✗ %s: decrypt failed (%v)\n", id, err)
			continue
		}
		if rotateDryRun {
			fmt.Printf("  ✓ %s: decrypt OK (%d bytes)\n", id, len(plain))
			continue
		}
		reEncVal, err := crypto.EncryptWithKey([]byte(newKey), plain)
		if err != nil {
			rows.Close()
			return fmt.Errorf("re-encrypt %s: %w", id, err)
		}
		updates = append(updates, reEnc{ID: id, NewValue: reEncVal})
	}
	rows.Close()

	fmt.Println()
	if rotateDryRun {
		fmt.Printf("Dry run complete: %d scanned, %d decrypted, %d failed.\n",
			scanned, scanned-len(failed), len(failed))
		if len(failed) > 0 {
			fmt.Println("⚠  Investigate the failed rows before running a real rotation — they would be")
			fmt.Println("   unrecoverable under the new key.")
		}
		return nil
	}

	if len(failed) > 0 {
		return fmt.Errorf("refusing to rotate: %d rows failed to decrypt with the old key. "+
			"Run with --dry-run first and inspect those rows", len(failed))
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	stmt, err := tx.Prepare("UPDATE secrets SET encrypted_value = ? WHERE id = ?")
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare update: %w", err)
	}
	for _, u := range updates {
		if _, err := stmt.Exec(u.NewValue, u.ID); err != nil {
			stmt.Close()
			tx.Rollback()
			return fmt.Errorf("update %s: %w", u.ID, err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	fmt.Printf("✓ Re-encrypted %d secrets under the new key.\n", len(updates))
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Set TOFI_ENCRYPTION_KEY=<new key> in the engine's environment.")
	fmt.Println("  2. Remove TOFI_DEV=1 if it was set as a stopgap.")
	fmt.Println("  3. Start the engine: tofi start")
	return nil
}
