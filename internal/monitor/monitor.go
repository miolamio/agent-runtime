package monitor

import (
	"os"
	"os/exec"
)

func ShowStatus() error {
	cmd := exec.Command("docker", "ps",
		"--filter", "ancestor=automatica-runtime",
		"--format", "table {{.Names}}\t{{.Status}}\t{{.CreatedAt}}\t{{.Image}}")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
