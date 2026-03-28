// internal/keys/envfile.go
package keys

import (
	"bufio"
	"os"
	"strings"
)

// ReadEnvKey reads a single key value from an env file.
func ReadEnvKey(path, key string) (string, error) {
	kv, err := ReadAllEnvKeys(path)
	if err != nil {
		return "", err
	}
	return kv[key], nil
}

// ReadAllEnvKeys reads all key=value pairs from an env file, skipping comments and blank lines.
func ReadAllEnvKeys(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	kv := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			kv[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return kv, scanner.Err()
}

// UpdateEnvKey replaces or appends a key=value pair in the env file.
func UpdateEnvKey(path, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
			lines[i] = key + "=" + value
			found = true
			break
		}
	}

	if !found {
		lines = append(lines, key+"="+value)
	}

	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0600)
}
