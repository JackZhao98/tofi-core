package server

import (
	"testing"

	"tofi-core/internal/models"
)

// TestPlanAllowsSkill verifies the tier gating rules:
//   - Skill with empty tier is treated as "free" and allowed to everyone.
//   - Explicit tier matches a plan's allowed tiers (inclusive lower tiers).
//   - Admin bypasses tier checks entirely (nil AllowedSkillTiers).
func TestPlanAllowsSkill(t *testing.T) {
	tests := []struct {
		name      string
		plan      PlanLimits
		skillTier string
		want      bool
	}{
		{
			name:      "free plan allows untagged skill",
			plan:      PlanDefs["free"],
			skillTier: "",
			want:      true,
		},
		{
			name:      "free plan allows free skill",
			plan:      PlanDefs["free"],
			skillTier: "free",
			want:      true,
		},
		{
			name:      "free plan blocks developer skill",
			plan:      PlanDefs["free"],
			skillTier: "developer",
			want:      false,
		},
		{
			name:      "free plan blocks pro skill",
			plan:      PlanDefs["free"],
			skillTier: "pro",
			want:      false,
		},
		{
			name:      "developer plan allows free skill",
			plan:      PlanDefs["developer"],
			skillTier: "free",
			want:      true,
		},
		{
			name:      "developer plan allows developer skill",
			plan:      PlanDefs["developer"],
			skillTier: "developer",
			want:      true,
		},
		{
			name:      "developer plan blocks pro skill",
			plan:      PlanDefs["developer"],
			skillTier: "pro",
			want:      false,
		},
		{
			name:      "admin bypass (nil) allows anything",
			plan:      PlanDefs["admin"],
			skillTier: "pro",
			want:      true,
		},
		{
			name:      "admin bypass allows unknown tier",
			plan:      PlanDefs["admin"],
			skillTier: "some-future-pack",
			want:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PlanAllowsSkill(tt.plan, tt.skillTier)
			if got != tt.want {
				t.Errorf("PlanAllowsSkill(%s, tier=%q) = %v, want %v",
					tt.name, tt.skillTier, got, tt.want)
			}
		})
	}
}

// TestFilterSkillsByPlan exercises the catalog-side helper that strips
// skills a user can't use before they reach the planner LLM.
func TestFilterSkillsByPlan(t *testing.T) {
	catalog := []models.SkillManifest{
		{Name: "web-search", Tier: ""},            // default free
		{Name: "web-fetch", Tier: "free"},          // explicit free
		{Name: "brave-deep-research", Tier: "developer"},
		{Name: "sub-agent-fleet", Tier: "pro"},
	}

	tests := []struct {
		name     string
		plan     PlanLimits
		wantKeep []string
	}{
		{
			name:     "free keeps only free/untagged",
			plan:     PlanDefs["free"],
			wantKeep: []string{"web-search", "web-fetch"},
		},
		{
			name:     "developer keeps free + developer",
			plan:     PlanDefs["developer"],
			wantKeep: []string{"web-search", "web-fetch", "brave-deep-research"},
		},
		{
			name:     "admin keeps everything",
			plan:     PlanDefs["admin"],
			wantKeep: []string{"web-search", "web-fetch", "brave-deep-research", "sub-agent-fleet"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterSkillsByPlan(catalog, tt.plan)
			gotNames := make([]string, len(got))
			for i, s := range got {
				gotNames[i] = s.Name
			}
			if len(gotNames) != len(tt.wantKeep) {
				t.Fatalf("got %d skills %v, want %d %v",
					len(gotNames), gotNames, len(tt.wantKeep), tt.wantKeep)
			}
			for i, name := range tt.wantKeep {
				if gotNames[i] != name {
					t.Errorf("index %d: got %q, want %q", i, gotNames[i], name)
				}
			}
		})
	}
}
