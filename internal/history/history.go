package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type RunRecord struct {
	Timestamp  string `json:"timestamp"`
	Profile    string `json:"profile"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Prompt     string `json:"prompt"`
	DurationMs int64  `json:"duration_ms"`
	ExitCode   int    `json:"exit_code"`
	RunDir     string `json:"run_dir"`
}

func runsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[airun] warning: cannot resolve home dir for run history: %v\n", err)
		return ""
	}
	return filepath.Join(home, ".airun", "runs")
}

func Save(rec RunRecord, output string) error {
	dir := rec.RunDir
	if err := os.MkdirAll(dir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "[airun] warning: cannot create history dir: %v\n", err)
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	for name, content := range map[string][]byte{
		"meta.json":  data,
		"prompt.txt": []byte(rec.Prompt),
		"output.txt": []byte(output),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0600); err != nil {
			fmt.Fprintf(os.Stderr, "[airun] warning: cannot write %s: %v\n", name, err)
		}
	}
	return nil
}

func NewRunDir(profile, provider string) string {
	ts := time.Now().Format("2006-01-02_15-04-05")
	name := fmt.Sprintf("%s_%s_%s", ts, profile, provider)
	return filepath.Join(runsDir(), name)
}

func List(limit int) ([]RunRecord, error) {
	base := runsDir()
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() > entries[j].Name()
	})
	var records []RunRecord
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		metaPath := filepath.Join(base, e.Name(), "meta.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var rec RunRecord
		if json.Unmarshal(data, &rec) == nil {
			records = append(records, rec)
		}
		if len(records) >= limit {
			break
		}
	}
	return records, nil
}

func FormatTable(records []RunRecord) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-18s %-8s %-6s %8s  %-40s %s\n",
		"TIME", "PROFILE", "PROV", "DURATION", "PROMPT", "STATUS")
	sb.WriteString(strings.Repeat("-", 95) + "\n")
	for _, r := range records {
		prompt := r.Prompt
		if len(prompt) > 40 {
			prompt = prompt[:37] + "..."
		}
		status := "ok"
		if r.ExitCode != 0 {
			status = "fail"
		}
		dur := fmt.Sprintf("%.1fs", float64(r.DurationMs)/1000)
		fmt.Fprintf(&sb, "%-18s %-8s %-6s %8s  %-40s %s\n",
			r.Timestamp[:16], r.Profile, r.Provider, dur, prompt, status)
	}
	return sb.String()
}
