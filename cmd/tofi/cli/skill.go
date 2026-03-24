package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"tofi-core/internal/skills"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(skillCmd)
	skillCmd.AddCommand(skillListCmd)
	skillCmd.AddCommand(skillRemoveCmd)
	skillCmd.AddCommand(skillInstallCmd)
	skillCmd.AddCommand(skillImportCmd)
	skillCmd.AddCommand(skillCreateCmd)
}

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage skills",
	Long:  "Browse, install, import, create, and remove skills.",
	RunE: func(cmd *cobra.Command, args []string) error {
		reason, err := runSkillSection(cmd)
		if err != nil {
			return err
		}
		if reason == exitQuit {
			return nil
		}
		return runMainMenuLoop(cmd)
	},
}

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed skills",
	RunE: func(cmd *cobra.Command, args []string) error {
		store := skills.NewLocalStore(homeDir)
		userID := resolveUserID()

		userSkills, err := store.ListUserSkills(userID)
		if err != nil {
			return err
		}

		if len(userSkills) == 0 {
			fmt.Println("No skills installed.")
			return nil
		}

		for _, s := range userSkills {
			scope := s.Scope
			if len(scope) > 0 {
				scope = strings.ToUpper(scope[:1]) + scope[1:]
			}
			fmt.Printf("  %-20s %s · v%s\n", s.Name, scope, s.Version)
		}
		return nil
	},
}

var skillRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a skill",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store := skills.NewLocalStore(homeDir)
		userID := resolveUserID()

		if err := store.DeactivateSkill(userID, args[0]); err != nil {
			return err
		}
		fmt.Printf("  ✓ Removed skill %q\n", args[0])
		return nil
	},
}

var skillInstallCmd = &cobra.Command{
	Use:   "install <source>",
	Short: "Install skill from GitHub",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store := skills.NewLocalStore(homeDir)
		installer := skills.NewSkillInstaller(store)
		userID := resolveUserID()

		fmt.Printf("  Installing from %s...\n", args[0])
		result, err := installer.InstallForUser(args[0], userID, nil)
		if err != nil {
			return err
		}

		for _, s := range result.Skills {
			fmt.Printf("  ✓ Installed %s (v%s)\n", s.Manifest.Name, s.Manifest.Version)
		}
		return nil
	},
}

var skillImportCmd = &cobra.Command{
	Use:   "import <path>",
	Short: "Import skills from local directory",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store := skills.NewLocalStore(homeDir)
		userID := resolveUserID()

		path := args[0]
		if strings.HasPrefix(path, "~") {
			home, _ := os.UserHomeDir()
			path = filepath.Join(home, path[1:])
		}

		discovered, err := skills.DiscoverSkills(path)
		if err != nil || len(discovered) == 0 {
			return fmt.Errorf("no skills found in %s", path)
		}

		for _, skill := range discovered {
			skillDir := filepath.Join(path, skill.Manifest.Name)
			// If skill is at root, use path directly
			if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); os.IsNotExist(err) {
				skillDir = path
			}
			if err := store.SaveUserSkill(userID, skill.Manifest.Name, skillDir); err != nil {
				fmt.Printf("  ✗ Failed to import %s: %v\n", skill.Manifest.Name, err)
				continue
			}
			fmt.Printf("  ✓ Imported %s\n", skill.Manifest.Name)
		}
		return nil
	},
}

var skillCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new skill template",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store := skills.NewLocalStore(homeDir)
		userID := resolveUserID()

		name := args[0]
		template := fmt.Sprintf(`---
name: %s
description: TODO
version: "1.0"
---

# %s

Instructions for the AI agent go here.
`, name, name)

		userDir := store.UserSkillDir(userID)
		if err := os.MkdirAll(userDir, 0755); err != nil {
			return err
		}

		skillDir := filepath.Join(userDir, name)
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			return err
		}

		skillPath := filepath.Join(skillDir, "SKILL.md")
		if err := os.WriteFile(skillPath, []byte(template), 0644); err != nil {
			return err
		}

		fmt.Printf("  ✓ Created skill %q at %s\n", name, skillPath)
		return nil
	},
}

// resolveUserID returns the current user ID for skill operations.
func resolveUserID() string {
	// For now, use a default user. In multi-user mode, this would come from auth.
	return "default"
}
