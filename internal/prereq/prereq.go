package prereq

import (
	"fmt"
	"os/exec"
)

type Status struct {
	Docker bool
}

func Check() (*Status, error) {
	s := &Status{}
	if err := exec.Command("docker", "info").Run(); err == nil {
		s.Docker = true
	}
	return s, nil
}

func (s *Status) String() string {
	mark := "x"
	if s.Docker {
		mark = "v"
	}
	return fmt.Sprintf("  [%s] Docker", mark)
}

func (s *Status) Ready() bool {
	return s.Docker
}
