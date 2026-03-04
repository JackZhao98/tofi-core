package skills

import (
	"testing"
)

func TestParseSource_GitHubShorthand(t *testing.T) {
	tests := []struct {
		input       string
		owner       string
		repo        string
		skillFilter string
	}{
		{"vercel-labs/skills", "vercel-labs", "skills", ""},
		{"vercel-labs/skills@find-skills", "vercel-labs", "skills", "find-skills"},
		{"anthropics/skills@code-review", "anthropics", "skills", "code-review"},
		{"user123/my-repo", "user123", "my-repo", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			ps, err := ParseSource(tt.input)
			if err != nil {
				t.Fatalf("ParseSource(%q) error: %v", tt.input, err)
			}
			if ps.Type != SourceGitHub {
				t.Errorf("type = %s, want github", ps.Type)
			}
			if ps.Owner != tt.owner {
				t.Errorf("owner = %q, want %q", ps.Owner, tt.owner)
			}
			if ps.Repo != tt.repo {
				t.Errorf("repo = %q, want %q", ps.Repo, tt.repo)
			}
			if ps.SkillFilter != tt.skillFilter {
				t.Errorf("skillFilter = %q, want %q", ps.SkillFilter, tt.skillFilter)
			}
			if ps.CloneURL == "" {
				t.Error("cloneURL should not be empty")
			}
		})
	}
}

func TestParseSource_GitHubURL(t *testing.T) {
	tests := []struct {
		input  string
		owner  string
		repo   string
		ref    string
		subpath string
	}{
		{"https://github.com/vercel-labs/skills", "vercel-labs", "skills", "", ""},
		{"https://github.com/vercel-labs/skills.git", "vercel-labs", "skills", "", ""},
		{"https://github.com/owner/repo/tree/main", "owner", "repo", "main", ""},
		{"https://github.com/owner/repo/tree/main/skills/foo", "owner", "repo", "main", "skills/foo"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			ps, err := ParseSource(tt.input)
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if ps.Type != SourceGitHub {
				t.Errorf("type = %s, want github", ps.Type)
			}
			if ps.Owner != tt.owner {
				t.Errorf("owner = %q, want %q", ps.Owner, tt.owner)
			}
			if ps.Repo != tt.repo {
				t.Errorf("repo = %q, want %q", ps.Repo, tt.repo)
			}
			if ps.Ref != tt.ref {
				t.Errorf("ref = %q, want %q", ps.Ref, tt.ref)
			}
			if ps.Subpath != tt.subpath {
				t.Errorf("subpath = %q, want %q", ps.Subpath, tt.subpath)
			}
		})
	}
}

func TestParseSource_GitSSH(t *testing.T) {
	ps, err := ParseSource("git@github.com:vercel-labs/skills.git")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if ps.Type != SourceGitHub {
		t.Errorf("type = %s, want github", ps.Type)
	}
	if ps.Owner != "vercel-labs" {
		t.Errorf("owner = %q", ps.Owner)
	}
	if ps.Repo != "skills" {
		t.Errorf("repo = %q", ps.Repo)
	}
}

func TestParseSource_LocalPath(t *testing.T) {
	tests := []string{"./my-skills", "../skills", "/absolute/path"}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			ps, err := ParseSource(input)
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if ps.Type != SourceLocal {
				t.Errorf("type = %s, want local", ps.Type)
			}
		})
	}
}

func TestParseSource_DisplayURL(t *testing.T) {
	ps, _ := ParseSource("vercel-labs/skills@find-skills")
	if d := ps.DisplayURL(); d != "vercel-labs/skills@find-skills" {
		t.Errorf("DisplayURL = %q, want %q", d, "vercel-labs/skills@find-skills")
	}

	ps2, _ := ParseSource("vercel-labs/skills")
	if d := ps2.DisplayURL(); d != "vercel-labs/skills" {
		t.Errorf("DisplayURL = %q, want %q", d, "vercel-labs/skills")
	}
}

func TestParseSource_Invalid(t *testing.T) {
	invalids := []string{"", "single-word", "a/b/c/d"}
	for _, input := range invalids {
		t.Run(input, func(t *testing.T) {
			_, err := ParseSource(input)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}
