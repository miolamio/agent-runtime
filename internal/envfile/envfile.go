package envfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Write(envVars []string) (string, error) {
	f, err := os.CreateTemp(os.TempDir(), ".arun-*.env")
	if err != nil {
		return "", fmt.Errorf("cannot create temp env file: %w", err)
	}
	defer f.Close()
	if err := os.Chmod(f.Name(), 0600); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("cannot chmod env file: %w", err)
	}
	for _, env := range envVars {
		if _, err := fmt.Fprintln(f, env); err != nil {
			os.Remove(f.Name())
			return "", fmt.Errorf("cannot write env file: %w", err)
		}
	}
	return f.Name(), nil
}

func Cleanup(path string) {
	if path != "" && strings.HasPrefix(filepath.Base(path), ".arun-") {
		os.Remove(path)
	}
}

func MaskLog(path string) string {
	return filepath.Base(path)
}
