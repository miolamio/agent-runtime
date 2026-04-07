package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSave(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "test-run")
	rec := RunRecord{
		Timestamp: "2026-04-07_12-00-00", Profile: "dev", Provider: "zai",
		Model: "glm-5.1", Prompt: "test prompt", DurationMs: 1500,
		ExitCode: 0, RunDir: runDir,
	}
	if err := Save(rec, "test output"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Check meta.json
	data, err := os.ReadFile(filepath.Join(runDir, "meta.json"))
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	var loaded RunRecord
	json.Unmarshal(data, &loaded)
	if loaded.Provider != "zai" {
		t.Errorf("provider = %q", loaded.Provider)
	}
	// Check prompt.txt
	p, _ := os.ReadFile(filepath.Join(runDir, "prompt.txt"))
	if string(p) != "test prompt" {
		t.Errorf("prompt = %q", string(p))
	}
	// Check output.txt
	o, _ := os.ReadFile(filepath.Join(runDir, "output.txt"))
	if string(o) != "test output" {
		t.Errorf("output = %q", string(o))
	}
	// Check permissions
	info, _ := os.Stat(filepath.Join(runDir, "meta.json"))
	if info.Mode().Perm() != 0600 {
		t.Errorf("perm = %o, want 0600", info.Mode().Perm())
	}
}

func TestFormatTable_Truncation(t *testing.T) {
	rec := RunRecord{Timestamp: "2026-04-07_12-00-00", Prompt: "This is a very long prompt that should be truncated at forty characters", Profile: "d", Provider: "z"}
	table := FormatTable([]RunRecord{rec})
	if !strings.Contains(table, "...") {
		t.Error("long prompt not truncated")
	}
}

func TestFormatTable_Empty(t *testing.T) {
	table := FormatTable(nil)
	if !strings.Contains(table, "TIME") {
		t.Error("missing header")
	}
	lines := strings.Split(strings.TrimSpace(table), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
}

func TestFormatTable_ExitCode(t *testing.T) {
	recs := []RunRecord{
		{Timestamp: "2026-04-07_12-00-00", ExitCode: 0, Prompt: "ok", Profile: "d", Provider: "z"},
		{Timestamp: "2026-04-07_12-01-00", ExitCode: 1, Prompt: "fail", Profile: "d", Provider: "z"},
	}
	table := FormatTable(recs)
	lines := strings.Split(strings.TrimSpace(table), "\n")
	if !strings.Contains(lines[2], "ok") {
		t.Error("exit 0 should show ok")
	}
	if !strings.Contains(lines[3], "fail") {
		t.Error("exit 1 should show fail")
	}
}
