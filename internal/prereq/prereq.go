package prereq

import (
	"fmt"
	"os/exec"
	"strings"
)

type Status struct {
	Docker  bool
	Clawker bool
}

func Check() (*Status, error) {
	s := &Status{}
	if err := exec.Command("docker", "info").Run(); err == nil {
		s.Docker = true
	}
	if _, err := exec.LookPath("clawker"); err == nil {
		s.Clawker = true
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
	return strings.Join(lines, "\n")
}

func (s *Status) Ready() bool {
	return s.Docker && s.Clawker
}
