package runner

import (
	"fmt"
	"strings"
	"sync"

	"github.com/miolamio/agent-runtime/internal/config"
)

type AgentSpec struct {
	Name   string
	Prompt string
}

func ParseAgentSpec(spec string) (AgentSpec, error) {
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) != 2 {
		return AgentSpec{}, fmt.Errorf("invalid agent spec %q — expected 'name:prompt'", spec)
	}
	return AgentSpec{Name: parts[0], Prompt: parts[1]}, nil
}

func RunParallel(cfg *config.Config, agents []AgentSpec, base RunOpts) error {
	var wg sync.WaitGroup
	errors := make(chan error, len(agents))

	for _, agent := range agents {
		wg.Add(1)
		go func(a AgentSpec) {
			defer wg.Done()
			opts := base
			opts.Prompt = a.Prompt
			opts.Name = a.Name
			opts.NoState = true // preserve the parallel session-persistence policy
			if err := Run(cfg, opts); err != nil {
				errors <- fmt.Errorf("agent %s failed: %w", a.Name, err)
			}
		}(agent)
	}

	wg.Wait()
	close(errors)

	var errs []string
	for err := range errors {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("parallel execution errors:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}
