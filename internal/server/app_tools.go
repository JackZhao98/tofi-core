package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"tofi-core/internal/mcp"
	"tofi-core/internal/provider"
	"tofi-core/internal/workspace"
)

// buildAppTools creates tools that let the AI manage Apps from global chat.
func (s *Server) buildAppTools(userID string) []mcp.ExtraBuiltinTool {
	return []mcp.ExtraBuiltinTool{
		s.buildListAppsTool(userID),
		s.buildCreateAppTool(userID),
		s.buildUpdateAppTool(userID),
		s.buildDeleteAppTool(userID),
		s.buildRunAppTool(userID),
		s.buildListAppRunsTool(userID),
		s.buildActivateAppTool(userID),
		s.buildListNotifyTargetsTool(userID),
		s.buildSetNotifyTargetsTool(userID),
	}
}

// ── tofi_list_apps ──

func (s *Server) buildListAppsTool(userID string) mcp.ExtraBuiltinTool {
	return mcp.ExtraBuiltinTool{
		Schema: provider.Tool{
			Name:        "tofi_list_apps",
			Description: "List all Apps for the current user. Returns name, description, active status, schedule, and next run time. Use this when the user asks about their apps, automations, or scheduled tasks.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		Handler: func(args map[string]any) (string, error) {
			records, err := s.db.ListApps(userID)
			if err != nil {
				return "", fmt.Errorf("failed to list apps: %w", err)
			}
			if len(records) == 0 {
				return "No apps found. You can create one with tofi_create_app.", nil
			}

			var out strings.Builder
			for _, a := range records {
				status := "inactive"
				if a.IsActive {
					status = "active"
				}
				out.WriteString(fmt.Sprintf("- **%s** (ID: %s)\n  %s\n  Status: %s",
					a.Name, a.ID, a.Description, status))

				if a.IsActive && a.ScheduleRules != "" {
					nextTimes := ExpandSchedule(a.ScheduleRules, time.Now(), 1)
					if len(nextTimes) > 0 {
						out.WriteString(fmt.Sprintf(" | Next run: %s", nextTimes[0].Format("2006-01-02 15:04")))
					}
				}
				out.WriteString("\n")
			}
			return out.String(), nil
		},
	}
}

// ── tofi_create_app ──

func (s *Server) buildCreateAppTool(userID string) mcp.ExtraBuiltinTool {
	return mcp.ExtraBuiltinTool{
		Schema: provider.Tool{
			Name: "tofi_create_app",
			Description: `Create a new App (automated AI task). The prompt is the instruction the AI will execute each run.
Schedule format is a JSON array of rule objects, e.g. [{"time":"09:00","repeat":{"type":"daily"}}].
Skills is an array of skill names to attach.
Use this when the user wants to create a new automation, scheduled task, or recurring AI job.`,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "App name (kebab-case, e.g. 'daily-weather')",
					},
					"description": map[string]any{
						"type":        "string",
						"description": "Short description of what the app does",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "The full instruction/prompt the AI will execute each run",
					},
					"model": map[string]any{
						"type":        "string",
						"description": "Model to use (optional, uses default if omitted)",
					},
					"skills": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Skill names to attach (optional)",
					},
					"schedule": map[string]any{
						"type":        "string",
						"description": "Schedule rules as JSON array string, e.g. '[{\"time\":\"09:00\",\"repeat\":{\"type\":\"daily\"}}]' (optional)",
					},
				},
				"required": []string{"name", "description", "prompt"},
			},
		},
		Handler: func(args map[string]any) (string, error) {
			name, _ := args["name"].(string)
			description, _ := args["description"].(string)
			prompt, _ := args["prompt"].(string)

			if name == "" || prompt == "" {
				return "", fmt.Errorf("name and prompt are required")
			}

			model, _ := args["model"].(string)
			var skillsList []string
			if skills, ok := args["skills"].([]any); ok {
				for _, sk := range skills {
					if s, ok := sk.(string); ok {
						skillsList = append(skillsList, s)
					}
				}
			}

			var scheduleRules *json.RawMessage
			if schedStr, ok := args["schedule"].(string); ok && schedStr != "" {
				raw := json.RawMessage(schedStr)
				scheduleRules = &raw
			}

			// Build AgentDef
			def := requestToAgentDef(name, description, prompt, "", model,
				skillsList, scheduleRules, nil, nil, nil, nil)

			// Write to filesystem
			if s.workspace != nil {
				if err := s.workspace.WriteAgent(userID, def); err != nil {
					return "", fmt.Errorf("failed to write app files: %w", err)
				}
			}

			// Sync to DB
			if s.workspaceSync != nil {
				record, err := s.workspaceSync.SyncAgentToDB(userID, name)
				if err != nil {
					return "", fmt.Errorf("failed to sync app: %w", err)
				}
				return fmt.Sprintf("App created successfully.\nName: %s\nID: %s\nPrompt: %s",
					record.Name, record.ID, truncate(prompt, 100)), nil
			}

			return fmt.Sprintf("App '%s' created successfully.", name), nil
		},
	}
}

// ── tofi_update_app ──

func (s *Server) buildUpdateAppTool(userID string) mcp.ExtraBuiltinTool {
	return mcp.ExtraBuiltinTool{
		Schema: provider.Tool{
			Name:        "tofi_update_app",
			Description: "Update an existing App's configuration. Only provided fields will be changed. Use this when the user wants to modify an app's prompt, schedule, skills, or other settings.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"app_id": map[string]any{
						"type":        "string",
						"description": "The App ID to update",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "New name (optional)",
					},
					"description": map[string]any{
						"type":        "string",
						"description": "New description (optional)",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "New prompt/instruction (optional)",
					},
					"model": map[string]any{
						"type":        "string",
						"description": "New model (optional)",
					},
					"skills": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "New skill names list (optional, replaces existing)",
					},
					"schedule": map[string]any{
						"type":        "string",
						"description": "New schedule rules as JSON array string (optional)",
					},
				},
				"required": []string{"app_id"},
			},
		},
		Handler: func(args map[string]any) (string, error) {
			appID, _ := args["app_id"].(string)
			if appID == "" {
				return "", fmt.Errorf("app_id is required")
			}

			existing, err := s.db.GetApp(appID)
			if err != nil || existing.UserID != userID {
				return "", fmt.Errorf("app not found: %s", appID)
			}

			oldName := existing.Name

			if v, ok := args["name"].(string); ok && v != "" {
				existing.Name = v
			}
			if v, ok := args["description"].(string); ok && v != "" {
				existing.Description = v
			}
			if v, ok := args["prompt"].(string); ok && v != "" {
				existing.Prompt = v
			}
			if v, ok := args["model"].(string); ok && v != "" {
				existing.Model = v
			}
			if skills, ok := args["skills"].([]any); ok {
				var names []string
				for _, sk := range skills {
					if s, ok := sk.(string); ok {
						names = append(names, s)
					}
				}
				skillsJSON, _ := json.Marshal(names)
				existing.Skills = string(skillsJSON)
			}
			if schedStr, ok := args["schedule"].(string); ok && schedStr != "" {
				existing.ScheduleRules = schedStr
			}

			// Write to filesystem
			if s.workspace != nil {
				if oldName != existing.Name {
					_ = s.workspace.DeleteAgent(userID, oldName)
				}
				def := workspace.RecordToAgentDef(existing)
				if err := s.workspace.WriteAgent(userID, def); err != nil {
					return "", fmt.Errorf("failed to write app: %w", err)
				}
			}

			// Sync to DB
			if s.workspaceSync != nil {
				synced, err := s.workspaceSync.SyncAgentToDB(userID, existing.Name)
				if err != nil {
					if dbErr := s.db.UpdateApp(existing); dbErr != nil {
						return "", fmt.Errorf("failed to update app: %w", dbErr)
					}
				} else {
					synced.IsActive = existing.IsActive
					synced.Parameters = existing.Parameters
					synced.ID = existing.ID
					if dbErr := s.db.UpdateApp(synced); dbErr != nil {
						return "", fmt.Errorf("failed to update app index: %w", dbErr)
					}
				}
			} else {
				if dbErr := s.db.UpdateApp(existing); dbErr != nil {
					return "", fmt.Errorf("failed to update app: %w", dbErr)
				}
			}

			// Reschedule if active
			if existing.IsActive && s.appScheduler != nil {
				s.appScheduler.RemoveApp(existing.ID)
				_ = s.appScheduler.ActivateApp(existing)
			}

			return fmt.Sprintf("App '%s' updated successfully.", existing.Name), nil
		},
	}
}

// ── tofi_delete_app ──

func (s *Server) buildDeleteAppTool(userID string) mcp.ExtraBuiltinTool {
	return mcp.ExtraBuiltinTool{
		Schema: provider.Tool{
			Name:        "tofi_delete_app",
			Description: "Delete an App permanently. This removes the app, its files, and cancels any pending runs. Use when the user wants to remove an automation.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"app_id": map[string]any{
						"type":        "string",
						"description": "The App ID to delete",
					},
				},
				"required": []string{"app_id"},
			},
		},
		Handler: func(args map[string]any) (string, error) {
			appID, _ := args["app_id"].(string)
			if appID == "" {
				return "", fmt.Errorf("app_id is required")
			}

			app, err := s.db.GetApp(appID)
			if err != nil || app.UserID != userID {
				return "", fmt.Errorf("app not found: %s", appID)
			}

			name := app.Name

			// Remove from scheduler
			if s.appScheduler != nil {
				s.appScheduler.RemoveApp(appID)
			}

			// Cancel pending runs
			s.db.CancelPendingAppRuns(appID)

			// Delete files
			if s.workspace != nil {
				_ = s.workspace.DeleteAgent(userID, name)
			}

			// Delete from DB
			if err := s.db.DeleteApp(appID, userID); err != nil {
				return "", fmt.Errorf("failed to delete app: %w", err)
			}

			return fmt.Sprintf("App '%s' deleted successfully.", name), nil
		},
	}
}

// ── tofi_run_app ──

func (s *Server) buildRunAppTool(userID string) mcp.ExtraBuiltinTool {
	return mcp.ExtraBuiltinTool{
		Schema: provider.Tool{
			Name:        "tofi_run_app",
			Description: "Manually trigger an App to run immediately. The app will execute in the background and create a new chat session with the results. Use when the user wants to run an automation right now.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"app_id": map[string]any{
						"type":        "string",
						"description": "The App ID to run",
					},
				},
				"required": []string{"app_id"},
			},
		},
		Handler: func(args map[string]any) (string, error) {
			appID, _ := args["app_id"].(string)
			if appID == "" {
				return "", fmt.Errorf("app_id is required")
			}

			app, err := s.db.GetApp(appID)
			if err != nil || app.UserID != userID {
				return "", fmt.Errorf("app not found: %s", appID)
			}

			if app.Prompt == "" {
				return "", fmt.Errorf("app '%s' has no prompt configured", app.Name)
			}

			if s.appScheduler == nil {
				return "", fmt.Errorf("scheduler not available")
			}

			run, err := s.appScheduler.DispatchManualRun(app, userID)
			if err != nil {
				return "", fmt.Errorf("failed to dispatch run: %w", err)
			}

			return fmt.Sprintf("App '%s' triggered successfully.\nRun ID: %s\nStatus: %s\nThe app is now executing in the background.",
				app.Name, run.ID, run.Status), nil
		},
	}
}

// ── tofi_list_app_runs ──

func (s *Server) buildListAppRunsTool(userID string) mcp.ExtraBuiltinTool {
	return mcp.ExtraBuiltinTool{
		Schema: provider.Tool{
			Name:        "tofi_list_app_runs",
			Description: "List recent runs for an App. Shows status, trigger type, timestamps, and associated session IDs. Use when the user asks about an app's execution history.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"app_id": map[string]any{
						"type":        "string",
						"description": "The App ID to query runs for",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Max number of runs to return (default: 5, max: 20)",
					},
				},
				"required": []string{"app_id"},
			},
		},
		Handler: func(args map[string]any) (string, error) {
			appID, _ := args["app_id"].(string)
			if appID == "" {
				return "", fmt.Errorf("app_id is required")
			}

			app, err := s.db.GetApp(appID)
			if err != nil || app.UserID != userID {
				return "", fmt.Errorf("app not found: %s", appID)
			}

			limit := 5
			if l, ok := args["limit"].(float64); ok && l > 0 {
				limit = int(l)
				if limit > 20 {
					limit = 20
				}
			}

			runs, err := s.db.ListAppRuns(appID, "", limit)
			if err != nil {
				return "", fmt.Errorf("failed to list runs: %w", err)
			}

			if len(runs) == 0 {
				return fmt.Sprintf("No runs found for app '%s'.", app.Name), nil
			}

			var out strings.Builder
			out.WriteString(fmt.Sprintf("Recent runs for '%s':\n", app.Name))
			for _, r := range runs {
				line := fmt.Sprintf("- [%s] %s | trigger: %s",
					r.Status, r.CreatedAt, r.Trigger)
				if r.SessionID != "" {
					line += " | session: " + r.SessionID
				}
				if r.CompletedAt != "" {
					line += " | completed: " + r.CompletedAt
				}
				out.WriteString(line + "\n")
			}
			return out.String(), nil
		},
	}
}

// ── tofi_activate_app ──

func (s *Server) buildActivateAppTool(userID string) mcp.ExtraBuiltinTool {
	return mcp.ExtraBuiltinTool{
		Schema: provider.Tool{
			Name:        "tofi_activate_app",
			Description: "Activate or deactivate an App's scheduled runs. When active, the app runs according to its schedule. When deactivated, pending runs are cancelled. Use when the user wants to enable/disable an automation's schedule.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"app_id": map[string]any{
						"type":        "string",
						"description": "The App ID",
					},
					"active": map[string]any{
						"type":        "boolean",
						"description": "true to activate, false to deactivate",
					},
				},
				"required": []string{"app_id", "active"},
			},
		},
		Handler: func(args map[string]any) (string, error) {
			appID, _ := args["app_id"].(string)
			active, _ := args["active"].(bool)

			if appID == "" {
				return "", fmt.Errorf("app_id is required")
			}

			app, err := s.db.GetApp(appID)
			if err != nil || app.UserID != userID {
				return "", fmt.Errorf("app not found: %s", appID)
			}

			if active {
				// Activate
				if app.ScheduleRules == "" || app.ScheduleRules == "[]" {
					return "", fmt.Errorf("app '%s' has no schedule rules configured", app.Name)
				}
				if err := s.db.SetAppActive(appID, userID, true); err != nil {
					return "", fmt.Errorf("failed to activate: %w", err)
				}
				if s.appScheduler != nil {
					app.IsActive = true
					_ = s.appScheduler.ActivateApp(app)
				}
				return fmt.Sprintf("App '%s' activated. It will run according to its schedule.", app.Name), nil
			}

			// Deactivate
			if err := s.db.SetAppActive(appID, userID, false); err != nil {
				return "", fmt.Errorf("failed to deactivate: %w", err)
			}
			cancelled, _ := s.db.CancelPendingAppRuns(appID)
			if s.appScheduler != nil {
				s.appScheduler.RemoveApp(appID)
			}
			return fmt.Sprintf("App '%s' deactivated. %d pending runs cancelled.", app.Name, cancelled), nil
		},
	}
}

// ── tofi_list_notify_targets ──

func (s *Server) buildListNotifyTargetsTool(userID string) mcp.ExtraBuiltinTool {
	return mcp.ExtraBuiltinTool{
		Schema: provider.Tool{
			Name:        "tofi_list_notify_targets",
			Description: "List notification targets for an App, or list all available receivers if no app_id is given. Shows who will receive push notifications when the App completes a run.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"app_id": map[string]any{
						"type":        "string",
						"description": "App ID to list targets for (optional — omit to list all available receivers)",
					},
				},
			},
		},
		Handler: func(args map[string]any) (string, error) {
			appID, _ := args["app_id"].(string)

			if appID != "" {
				// List targets for specific app
				app, err := s.db.GetApp(appID)
				if err != nil || app.UserID != userID {
					return "", fmt.Errorf("app not found: %s", appID)
				}

				targets, err := s.db.ListAppNotifyTargets(appID)
				if err != nil {
					return "", fmt.Errorf("failed to list targets: %w", err)
				}

				if len(targets) == 0 {
					return fmt.Sprintf("App '%s' has no notify targets configured.", app.Name), nil
				}

				var out strings.Builder
				out.WriteString(fmt.Sprintf("Notify targets for '%s':\n", app.Name))
				for _, t := range targets {
					out.WriteString(fmt.Sprintf("- %s (ID: %d, connector: %s)\n", t.DisplayName, t.ID, t.ConnectorID[:8]))
				}
				return out.String(), nil
			}

			// List all available receivers across all connectors
			connectors, err := s.db.ListConnectors(userID)
			if err != nil {
				return "", fmt.Errorf("failed to list connectors: %w", err)
			}

			if len(connectors) == 0 {
				return "No connectors configured. Use 'tofi connect' to set up Telegram/Slack/Discord/Email first.", nil
			}

			var out strings.Builder
			out.WriteString("Available receivers:\n")
			for _, c := range connectors {
				if !c.Enabled {
					continue
				}
				receivers, err := s.db.ListConnectorReceivers(c.ID)
				if err != nil || len(receivers) == 0 {
					continue
				}
				out.WriteString(fmt.Sprintf("\n[%s] %s:\n", c.Type, c.Name))
				for _, r := range receivers {
					out.WriteString(fmt.Sprintf("  - %s (receiver_id: %d)\n", r.DisplayName, r.ID))
				}
			}
			return out.String(), nil
		},
	}
}

// ── tofi_set_notify_targets ──

func (s *Server) buildSetNotifyTargetsTool(userID string) mcp.ExtraBuiltinTool {
	return mcp.ExtraBuiltinTool{
		Schema: provider.Tool{
			Name:        "tofi_set_notify_targets",
			Description: "Set which receivers should receive push notifications when an App completes a run. Pass receiver_ids to set specific targets, or pass 'all' to notify all available receivers. This replaces any existing targets.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"app_id": map[string]any{
						"type":        "string",
						"description": "The App ID",
					},
					"receiver_ids": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "integer"},
						"description": "Array of receiver IDs to notify. Use tofi_list_notify_targets (without app_id) to see available receivers.",
					},
					"all": map[string]any{
						"type":        "boolean",
						"description": "Set to true to add ALL available receivers as targets",
					},
				},
				"required": []string{"app_id"},
			},
		},
		Handler: func(args map[string]any) (string, error) {
			appID, _ := args["app_id"].(string)
			if appID == "" {
				return "", fmt.Errorf("app_id is required")
			}

			app, err := s.db.GetApp(appID)
			if err != nil || app.UserID != userID {
				return "", fmt.Errorf("app not found: %s", appID)
			}

			var receiverIDs []int64

			if all, ok := args["all"].(bool); ok && all {
				// Collect all receivers from all enabled connectors
				connectors, err := s.db.ListConnectors(userID)
				if err != nil {
					return "", fmt.Errorf("failed to list connectors: %w", err)
				}
				for _, c := range connectors {
					if !c.Enabled {
						continue
					}
					receivers, err := s.db.ListConnectorReceivers(c.ID)
					if err != nil {
						continue
					}
					for _, r := range receivers {
						receiverIDs = append(receiverIDs, r.ID)
					}
				}
			} else if ids, ok := args["receiver_ids"].([]any); ok {
				for _, id := range ids {
					if f, ok := id.(float64); ok {
						receiverIDs = append(receiverIDs, int64(f))
					}
				}
			}

			if err := s.db.SetAppNotifyTargets(appID, receiverIDs); err != nil {
				return "", fmt.Errorf("failed to set targets: %w", err)
			}

			if len(receiverIDs) == 0 {
				return fmt.Sprintf("Cleared all notify targets for '%s'.", app.Name), nil
			}
			return fmt.Sprintf("Set %d notify target(s) for '%s'.", len(receiverIDs), app.Name), nil
		},
	}
}

// ── Helpers ──

func truncate(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "..."
}
