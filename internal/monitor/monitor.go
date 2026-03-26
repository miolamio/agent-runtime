package monitor

import (
	"fmt"
	"os"
	"os/exec"
)

func ShowStatus() error {
	cmd := exec.Command("docker", "ps",
		"--filter", "label=clawker",
		"--format", "table {{.Names}}\t{{.Status}}\t{{.CreatedAt}}\t{{.Image}}")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func StartMonitoring() error {
	out, err := exec.Command("clawker", "monitor", "status").CombinedOutput()
	if err != nil {
		fmt.Println("[arun] Starting monitoring stack...")
		cmd := exec.Command("clawker", "monitor", "up")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to start monitoring: %w", err)
		}
	} else {
		fmt.Printf("%s\n", out)
	}

	fmt.Println("\nGrafana: http://localhost:3000")
	fmt.Println("Prometheus: http://localhost:9090")
	return nil
}
