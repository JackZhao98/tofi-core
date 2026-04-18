package server

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tofi-core/internal/capability"
	"tofi-core/internal/crypto"
	"tofi-core/internal/agent"
	"tofi-core/internal/skills"
	"tofi-core/internal/storage"
)

// buildSkillToolsFromRecords builds SkillTool list from skill records and resolves secrets.
// Returns: skillTools, secretEnv, missingSecrets
func (s *Server) buildSkillToolsFromRecords(userID string, skillRecords []*storage.SkillRecord) ([]agent.SkillTool, map[string]string, []string) {
	localStore := skills.NewLocalStore(s.config.HomeDir)
	var skillTools []agent.SkillTool
	secretEnv := make(map[string]string)
	var missingSecrets []string

	for _, skill := range skillRecords {
		st := agent.SkillTool{
			ID:           skill.ID,
			Name:         skill.Name,
			Description:  skill.Description,
			Instructions: skill.Instructions,
		}
		// 如果 skill 有脚本，传入磁盘绝对路径（用于创建 symlink）
		if skill.HasScripts {
			skillDir := localStore.SkillDir(skill.Name)
			if abs, err := filepath.Abs(skillDir); err == nil {
				skillDir = abs
			}
			st.SkillDir = skillDir
		}
		skillTools = append(skillTools, st)

		// Resolve secrets with the shared 3-tier (user → service key → env) logic.
		for _, secretName := range skill.RequiredSecretsList() {
			if _, ok := secretEnv[secretName]; ok {
				continue
			}
			val, source := s.resolveSecret(userID, secretName)
			if val == "" {
				missingSecrets = append(missingSecrets,
					fmt.Sprintf("Skill '%s' requires secret '%s'", skill.Name, secretName))
				continue
			}
			secretEnv[secretName] = val
			s.injectUsageCallback(userID, secretName, secretEnv)
			_ = source
		}
	}

	return skillTools, secretEnv, missingSecrets
}

// resolveSecret finds a secret value for a given user, walking the 3-tier
// resolution: encrypted user DB → system service key (for known services)
// → TOFI process env. Returns ("", "") if nothing is configured.
func (s *Server) resolveSecret(userID, secretName string) (value string, source string) {
	// 1. User-scope encrypted secret.
	if userID != "" {
		if rec, err := s.db.GetSecret(userID, secretName); err == nil {
			if plain, err := crypto.Decrypt(rec.EncryptedValue); err == nil && plain != "" {
				return plain, "user"
			}
		}
	}
	// 2. System-scope service key (admin-configured via /admin/service-keys).
	//    Only triggered for secrets a skill declared as a known service
	//    provider, so we never accidentally silently surface one user's
	//    personal secret to another caller.
	if provider, ok := storage.KnownServiceSecrets[secretName]; ok {
		if plain, err := s.db.GetServiceKey(provider, ""); err == nil && plain != "" {
			return plain, "system"
		}
	}
	// 3. TOFI process env (last-resort fallback for local dev).
	if v := os.Getenv(secretName); v != "" {
		return v, "env"
	}
	return "", ""
}

// injectUsageCallback issues a short-lived callback token for the resolved
// secret's provider and writes TOFI_USAGE_URL + TOFI_USAGE_TOKEN into the
// env map so the skill script can POST usage events back on loopback. No-op
// when the secret isn't mapped to a known service provider.
func (s *Server) injectUsageCallback(userID, secretName string, secretEnv map[string]string) {
	if s.usageTokens == nil {
		return
	}
	provider, ok := storage.KnownServiceSecrets[secretName]
	if !ok {
		return
	}
	if _, exists := secretEnv["TOFI_USAGE_TOKEN"]; exists {
		return
	}
	token := s.usageTokens.issue(userID, provider, 2*time.Hour)
	if token == "" {
		return
	}
	secretEnv["TOFI_USAGE_TOKEN"] = token
	secretEnv["TOFI_USAGE_PROVIDER"] = provider
	secretEnv["TOFI_USAGE_URL"] = fmt.Sprintf("http://127.0.0.1:%d/api/v1/internal/usage", s.config.Port)
}

// buildSkillToolsFromNames loads skills by name and builds tools.
// Returns skillTools, skillInstructions (for appending to system prompt), and secretEnv.
// Unlike buildSkillToolsFromRecords, this silently skips missing skills and secrets
// (appropriate for chat context where missing secrets are non-fatal).
func (s *Server) buildSkillToolsFromNames(userID string, skillNames []string) ([]agent.SkillTool, []string, map[string]string) {
	var records []*storage.SkillRecord
	var skillInstructions []string

	for _, skillName := range skillNames {
		skillName = strings.TrimSpace(skillName)
		if skillName == "" {
			continue
		}
		rec, err := s.db.GetSkillByName(userID, skillName)
		if err != nil {
			log.Printf("[chat] skill %q not found: %v", skillName, err)
			continue
		}
		records = append(records, rec)
		if rec.Instructions != "" {
			skillInstructions = append(skillInstructions, rec.Instructions)
		}
	}

	skillTools, secretEnv, _ := s.buildSkillToolsFromRecords(userID, records)
	return skillTools, skillInstructions, secretEnv
}

// SkillUnavailable records a skill that was requested but can't be used
// because a required secret isn't configured anywhere in the 3-tier
// resolver. The chat / app-run paths surface these to the agent via the
// system prompt so it fails fast ("I can't search the web because the
// admin hasn't configured Brave") instead of silently missing a tool
// and confabulating its way through a research task.
type SkillUnavailable struct {
	SkillName  string
	SecretName string
}

// buildSkillTools loads skills from embed FS (system) and filesystem (user).
// Does not query the database — filesystem is the single source of truth.
// Returns skillTools, skillInstructions, secretEnv, and a list of skills
// that could not be enabled because required secrets weren't configured.
// Skills in the unavailable list are EXCLUDED from skillTools so the agent
// never sees them as viable choices; the caller should still inform the
// agent via system prompt so it can explain the outage to the user.
func (s *Server) buildSkillTools(userID string, skillNames []string) ([]agent.SkillTool, []string, map[string]string, []SkillUnavailable) {
	localStore := skills.NewLocalStore(s.config.HomeDir)
	systemSkills := skills.LoadAllSystemSkills()

	var skillTools []agent.SkillTool
	var skillInstructions []string
	var unavailable []SkillUnavailable
	secretEnv := make(map[string]string)

	for _, name := range skillNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		var st agent.SkillTool
		var requiredSecrets []string

		if sf, ok := systemSkills[name]; ok {
			// System skill — from embed FS
			st = agent.SkillTool{
				ID:           "system/" + name,
				Name:         sf.Manifest.Name,
				Description:  sf.Manifest.Description,
				Instructions: sf.Body,
				DirectTools:  sf.Manifest.Tools,
			}
			if len(sf.ScriptDirs) > 0 {
				// Scripts are copied to disk by InstallSystemSkills()
				// SkillDir = skill root (e.g., ~/.tofi/skills/web-search), NOT the scripts/ subdirectory.
				// DirectTool.Script paths are relative to this root (e.g., "scripts/search.py").
				localStore := skills.NewLocalStore(s.config.HomeDir)
				st.SkillDir = localStore.SkillDir(name)
			}
			requiredSecrets = sf.Manifest.RequiredSecrets
		} else {
			// User skill — from filesystem
			sf, err := localStore.LoadSkill(userID, name)
			if err != nil {
				log.Printf("[chat] skill %q not found: %v", name, err)
				continue
			}
			st = agent.SkillTool{
				ID:           "user/" + name,
				Name:         sf.Manifest.Name,
				Description:  sf.Manifest.Description,
				Instructions: sf.Body,
				DirectTools:  sf.Manifest.Tools,
			}
			if len(sf.ScriptDirs) > 0 {
				st.SkillDir = sf.Dir
			}
			requiredSecrets = sf.Manifest.RequiredSecrets
		}

		// Resolve secrets via shared 3-tier helper. If ANY required secret
		// is missing, mark the skill unavailable and skip registering it
		// — the agent shouldn't see a tool it can't actually call.
		skipSkill := false
		var localSecrets []struct{ name, value string }
		for _, secretName := range requiredSecrets {
			if v, ok := secretEnv[secretName]; ok {
				localSecrets = append(localSecrets, struct{ name, value string }{secretName, v})
				continue
			}
			val, _ := s.resolveSecret(userID, secretName)
			if val == "" {
				unavailable = append(unavailable, SkillUnavailable{
					SkillName:  name,
					SecretName: secretName,
				})
				skipSkill = true
				break
			}
			localSecrets = append(localSecrets, struct{ name, value string }{secretName, val})
		}
		if skipSkill {
			continue
		}

		// All required secrets resolved — commit the skill + its env.
		for _, sec := range localSecrets {
			if _, exists := secretEnv[sec.name]; !exists {
				secretEnv[sec.name] = sec.value
				s.injectUsageCallback(userID, sec.name, secretEnv)
			}
		}

		skillTools = append(skillTools, st)
		if st.Instructions != "" {
			skillInstructions = append(skillInstructions, st.Instructions)
		}
	}

	return skillTools, skillInstructions, secretEnv, unavailable
}

// buildCapabilitiesFromJSON parses capabilities JSON and returns MCP servers + extra tools.
func (s *Server) buildCapabilitiesFromJSON(userID, capsJSON string) ([]agent.MCPServerConfig, []agent.ExtraBuiltinTool) {
	caps, err := capability.Parse(capsJSON)
	if err != nil {
		log.Printf("⚠️ Invalid capabilities JSON: %v", err)
		return nil, nil
	}
	if caps == nil {
		return nil, nil
	}

	secretGetter := func(name string) (string, error) {
		rec, err := s.db.GetSecret(userID, name)
		if err != nil {
			return "", err
		}
		return crypto.Decrypt(rec.EncryptedValue)
	}

	if err := capability.ResolveSecrets(caps, secretGetter); err != nil {
		log.Printf("⚠️ Failed to resolve capability secrets: %v", err)
	}

	mcpServers := capability.BuildMCPServers(caps)
	extraTools := capability.BuildExtraTools(caps, secretGetter)

	return mcpServers, extraTools
}

// buildCapabilitiesFromMap marshals a capabilities map to JSON and delegates to buildCapabilitiesFromJSON.
func (s *Server) buildCapabilitiesFromMap(userID string, capsMap interface{}) ([]agent.MCPServerConfig, []agent.ExtraBuiltinTool) {
	capsJSON, err := json.Marshal(capsMap)
	if err != nil {
		log.Printf("⚠️ Failed to marshal capabilities: %v", err)
		return nil, nil
	}
	return s.buildCapabilitiesFromJSON(userID, string(capsJSON))
}
