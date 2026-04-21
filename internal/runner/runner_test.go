package runner

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/miolamio/agent-runtime/internal/config"
)

func TestStateVolumeForProfile(t *testing.T) {
	tests := []struct {
		name    string
		profile string
		want    string
	}{
		{"empty profile uses default volume", "", stateVolumeName},
		{"named profile gets airun-state- prefix", "dev", "airun-state-dev"},
		{"names with dashes are preserved verbatim", "ceo-research", "airun-state-ceo-research"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stateVolumeForProfile(tt.profile); got != tt.want {
				t.Errorf("stateVolumeForProfile(%q) = %q, want %q", tt.profile, got, tt.want)
			}
		})
	}
}

func TestAppendClaudeCmd(t *testing.T) {
	tests := []struct {
		name string
		opts RunOpts
		want []string
	}{
		{
			name: "base invocation without loop",
			opts: RunOpts{Prompt: "hello"},
			want: []string{"claude", "-p", "hello", "--dangerously-skip-permissions"},
		},
		{
			name: "loop with positive MaxLoops adds --max-turns",
			opts: RunOpts{Prompt: "hi", Loop: true, MaxLoops: 3},
			want: []string{"claude", "-p", "hi", "--dangerously-skip-permissions", "--max-turns", "3"},
		},
		{
			name: "loop with zero MaxLoops omits --max-turns",
			opts: RunOpts{Prompt: "hi", Loop: true, MaxLoops: 0},
			want: []string{"claude", "-p", "hi", "--dangerously-skip-permissions"},
		},
		{
			name: "MaxLoops without Loop is ignored",
			opts: RunOpts{Prompt: "hi", Loop: false, MaxLoops: 5},
			want: []string{"claude", "-p", "hi", "--dangerously-skip-permissions"},
		},
		{
			name: "prompt with spaces is passed as a single arg",
			opts: RunOpts{Prompt: "do the thing now"},
			want: []string{"claude", "-p", "do the thing now", "--dangerously-skip-permissions"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendClaudeCmd(nil, tt.opts)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("appendClaudeCmd: got %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("appends onto existing slice", func(t *testing.T) {
		got := appendClaudeCmd([]string{"run", "--rm", "image"}, RunOpts{Prompt: "x"})
		want := []string{"run", "--rm", "image", "claude", "-p", "x", "--dangerously-skip-permissions"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

func TestAppendStateAndExtras(t *testing.T) {
	// Use a real temp dir as AgentsDir to exercise the os.Stat branch.
	tmpAgents := t.TempDir()
	missingAgents := filepath.Join(t.TempDir(), "does-not-exist")

	// Verify the "exists" fixture is actually a directory.
	if info, err := os.Stat(tmpAgents); err != nil || !info.IsDir() {
		t.Fatalf("test setup: %s should be a directory", tmpAgents)
	}

	type check struct {
		name       string
		cfg        *config.Config
		opts       RunOpts
		extra      []string
		wantIncl   []string // substrings that MUST appear concatenated in output
		wantExcl   []string // substrings that MUST NOT appear
	}
	tests := []check{
		{
			name:     "NoState=true omits state volume mount",
			cfg:      &config.Config{AgentsDir: missingAgents},
			opts:     RunOpts{NoState: true, Profile: "dev"},
			wantExcl: []string{"airun-state-", stateVolumeName},
		},
		{
			name:     "NoState=false with empty profile uses default state volume",
			cfg:      &config.Config{AgentsDir: missingAgents},
			opts:     RunOpts{NoState: false, Profile: ""},
			wantIncl: []string{"-v", stateVolumeName + ":" + stateMountPath},
		},
		{
			name:     "NoState=false with named profile uses per-profile volume",
			cfg:      &config.Config{AgentsDir: missingAgents},
			opts:     RunOpts{NoState: false, Profile: "research"},
			wantIncl: []string{"-v", "airun-state-research:" + stateMountPath},
		},
		{
			name:     "extraVolumes are passed through in order",
			cfg:      &config.Config{AgentsDir: missingAgents},
			opts:     RunOpts{NoState: true},
			extra:    []string{"/a:/b:ro", "/c:/d"},
			wantIncl: []string{"-v", "/a:/b:ro", "-v", "/c:/d"},
		},
		{
			name:     "existing AgentsDir adds :ro mount",
			cfg:      &config.Config{AgentsDir: tmpAgents},
			opts:     RunOpts{NoState: true},
			wantIncl: []string{"-v", tmpAgents + ":/home/claude/.claude/agents:ro"},
		},
		{
			name:     "missing AgentsDir is silently skipped",
			cfg:      &config.Config{AgentsDir: missingAgents},
			opts:     RunOpts{NoState: true},
			wantExcl: []string{missingAgents},
		},
		{
			name:     "browser=vnc adds AIRUN_BROWSER env and port 6080",
			cfg:      &config.Config{AgentsDir: missingAgents},
			opts:     RunOpts{NoState: true, Browser: "vnc"},
			wantIncl: []string{"AIRUN_BROWSER=vnc", "6080:6080"},
			wantExcl: []string{"9222:9222"},
		},
		{
			name:     "browser=cdp adds AIRUN_BROWSER env and port 9222",
			cfg:      &config.Config{AgentsDir: missingAgents},
			opts:     RunOpts{NoState: true, Browser: "cdp"},
			wantIncl: []string{"AIRUN_BROWSER=cdp", "9222:9222"},
			wantExcl: []string{"6080:6080"},
		},
		{
			name:     "browser=both opens 6080 and 9222",
			cfg:      &config.Config{AgentsDir: missingAgents},
			opts:     RunOpts{NoState: true, Browser: "both"},
			wantIncl: []string{"AIRUN_BROWSER=both", "6080:6080", "9222:9222"},
		},
		{
			name:     "browser empty adds no browser flags",
			cfg:      &config.Config{AgentsDir: missingAgents},
			opts:     RunOpts{NoState: true, Browser: ""},
			wantExcl: []string{"AIRUN_BROWSER", "6080:6080", "9222:9222"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendStateAndExtras(nil, tt.cfg, tt.opts, tt.extra)
			joined := strings.Join(got, " ")
			for _, s := range tt.wantIncl {
				if !strings.Contains(joined, s) {
					t.Errorf("expected substring %q in output; got:\n  %s", s, joined)
				}
			}
			for _, s := range tt.wantExcl {
				if strings.Contains(joined, s) {
					t.Errorf("expected %q NOT in output; got:\n  %s", s, joined)
				}
			}
		})
	}
}

func TestParseAgentSpec(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		want    AgentSpec
		wantErr bool
	}{
		{"name:prompt splits on first colon", "alice:write tests", AgentSpec{Name: "alice", Prompt: "write tests"}, false},
		{"prompt may contain colons", "bob:ratio 3:1", AgentSpec{Name: "bob", Prompt: "ratio 3:1"}, false},
		{"empty prompt is allowed", "carol:", AgentSpec{Name: "carol", Prompt: ""}, false},
		{"empty name is allowed", ":only prompt", AgentSpec{Name: "", Prompt: "only prompt"}, false},
		{"no colon returns error", "dave-with-no-colon", AgentSpec{}, true},
		{"empty string returns error", "", AgentSpec{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAgentSpec(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
