package prereq

import (
	"fmt"
	"os/exec"
	"strings"
)

type Status struct {
	Docker  bool
	Clawker bool
	Router  bool
}

func Check() (*Status, error) {
	s := &Status{}
	if err := exec.Command("docker", "info").Run(); err == nil {
		s.Docker = true
	}
	if _, err := exec.LookPath("clawker"); err == nil {
		s.Clawker = true
	}
	if _, err := exec.LookPath("ccr"); err == nil {
		s.Router = true
	}
	return s, nil
}

func (s *Status) String() string {
	var lines []string
	check := func(name string, ok bool) {
		mark := "x"
		if ok {
			mark = "v"
		}
		lines = append(lines, fmt.Sprintf("  [%s] %s", mark, name))
	}
	check("Docker", s.Docker)
	check("Clawker", s.Clawker)
	check("Claude Code Router (ccr)", s.Router)
	return strings.Join(lines, "\n")
}

func (s *Status) Ready() bool {
	return s.Docker && s.Clawker
}

func EnsureRouterRunning() error {
	out, err := exec.Command("ccr", "status").CombinedOutput()
	if err == nil && strings.Contains(string(out), "running") {
		return nil
	}
	fmt.Println("[arun] Starting Claude Code Router...")
	cmd := exec.Command("ccr", "start")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}
