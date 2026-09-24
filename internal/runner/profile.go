package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/miolamio/agent-runtime/internal/config"
	"github.com/miolamio/agent-runtime/internal/envfile"
	"github.com/miolamio/agent-runtime/internal/history"
	"github.com/miolamio/agent-runtime/internal/profile"
)

const profileManifestPath = "/run/airun/profile.json"

// Older images use the component volume without leases or a cache lock. Keep
// GC deferred while one is running, including during an old-image update.
func legacyCacheContainerActive() (bool, error) {
	out, err := exec.Command("docker", "ps", "--filter", "volume="+componentVolumeName, "--format", "{{.ID}}").Output()
	if err != nil {
		return true, fmt.Errorf("list component-cache containers: %w", err)
	}
	for _, id := range strings.Fields(string(out)) {
		version, err := exec.Command("docker", "inspect", "--format", `{{index .Config.Labels "io.airun.cache-gc-version"}}`, id).Output()
		if err != nil {
			return true, fmt.Errorf("inspect component-cache container %s: %w", id, err)
		}
		if strings.TrimSpace(string(version)) != "1" {
			return true, nil
		}
	}
	return false, nil
}

func requireProfileImage() error {
	out, err := exec.Command("docker", "image", "inspect", "--format",
		`{{index .Config.Labels "io.airun.profile-manifest-version"}}`, ImageName).Output()
	if err != nil {
		return fmt.Errorf("cannot inspect profile support in %s; run 'airun rebuild': %w", ImageName, err)
	}
	if strings.TrimSpace(string(out)) != "1" {
		return fmt.Errorf("%s does not support profile manifest version 1; run 'airun rebuild' before using --profile", ImageName)
	}
	return nil
}

// profileMounts passes declarative, secret-free input to container preparation.
// Native plugin references stay intact so the adapter can detect cross-source
// conflicts; the host must not filter references by their bare plugin name.
// The caller owns removal of the returned temporary file.
func profileMounts(p *profile.Profile, action string) (volumes []string, manifestPath string, env []string, err error) {
	var manifest profile.Manifest
	switch action {
	case "prepare":
		manifest, env, err = profile.Normalize(p, os.LookupEnv)
	case "update":
		manifest, err = profile.NormalizeForUpdate(p)
	default:
		return nil, "", nil, fmt.Errorf("unsupported profile action: %s", action)
	}
	if err != nil {
		return nil, "", nil, err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return nil, "", nil, fmt.Errorf("marshal profile manifest: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", nil, fmt.Errorf("profile temporary directory: %w", err)
	}
	dir := filepath.Join(home, ".airun", "tmp")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, "", nil, fmt.Errorf("create profile temporary directory: %w", err)
	}
	f, err := os.CreateTemp(dir, ".airun-profile-*.json")
	if err != nil {
		return nil, "", nil, fmt.Errorf("create profile manifest: %w", err)
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}()
	if _, err = f.Write(data); err != nil {
		return nil, "", nil, fmt.Errorf("write profile manifest: %w", err)
	}
	// The enclosing host directory is private. The sanitized file must also be
	// readable by the container's non-root UID through the read-only bind mount.
	if err = f.Chmod(0644); err != nil {
		return nil, "", nil, fmt.Errorf("set manifest permissions: %w", err)
	}
	if err = f.Close(); err != nil {
		return nil, "", nil, fmt.Errorf("close profile manifest: %w", err)
	}
	return []string{f.Name() + ":" + profileManifestPath + ":ro"}, f.Name(),
		append(env, "AIRUN_PROFILE_MANIFEST="+profileManifestPath), nil
}

// UpdateProfile refreshes this profile's selected artifacts without attaching a
// workspace, retaining session state, or running an agent. Container preparation
// owns the transaction and verifies the complete set before publication.
func UpdateProfile(cfg *config.Config, name string) error {
	p, err := profile.Load(name)
	if err != nil {
		return fmt.Errorf("profile: %w", err)
	}
	volumes, manifestPath, extraEnv, err := profileMounts(p, "update")
	if err != nil {
		return fmt.Errorf("profile manifest: %w", err)
	}
	defer os.Remove(manifestPath)
	if err := requireProfileImage(); err != nil {
		return err
	}
	extraEnv = append(extraEnv, "AIRUN_PROFILE_ACTION=update")
	if legacy, checkErr := legacyCacheContainerActive(); legacy {
		extraEnv = append(extraEnv, "AIRUN_CACHE_GC_SKIP=1")
		if checkErr != nil {
			fmt.Fprintf(os.Stderr, "[airun] warning: cache GC deferred: %v\n", checkErr)
		}
	}
	envPath, err := envfile.Write(extraEnv)
	if err != nil {
		return err
	}
	defer envfile.Cleanup(envPath)
	args := []string{"run", "--rm", "--name", "airun-profile-update-" + history.NewRunID(), "--env-file", envPath}
	args = appendStateAndExtras(args, cfg, RunOpts{Profile: name, NoState: true}, volumes)
	args = append(args, ImageName)
	fmt.Fprintf(os.Stderr, "[airun] updating profile=%s\n", name)
	cmd := exec.Command("docker", args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("update profile %q: %w", name, err)
	}
	fmt.Fprintf(os.Stderr, "[airun] updated profile=%s\n", name)
	return nil
}

// CleanProfile collects unreferenced artifacts across every profile and removes
// this profile's explicitly recoverable, failed session snapshots.
func CleanProfile(name string) error {
	if err := profile.ValidateKey(name); err != nil {
		return err
	}
	if err := requireProfileImage(); err != nil {
		return err
	}
	args := []string{"run", "--rm", "--name", "airun-profile-gc-" + history.NewRunID(),
		"-e", "AIRUN_PROFILE_MAINTENANCE=1", "-e", "AIRUN_COMPONENT_CACHE=" + componentMountPath,
		"-v", componentVolumeName + ":" + componentMountPath}
	if legacy, checkErr := legacyCacheContainerActive(); legacy {
		args = append(args, "-e", "AIRUN_CACHE_GC_SKIP=1")
		if checkErr != nil {
			fmt.Fprintf(os.Stderr, "[airun] warning: cache GC deferred: %v\n", checkErr)
		}
	}
	script := []string{"node", "/usr/local/lib/airun/profile-clean.mjs", componentMountPath}
	stateVolume := stateVolumeForProfile(name)
	if out, err := exec.Command("docker", "volume", "inspect", stateVolume).CombinedOutput(); err == nil {
		args = append(args, "-e", "AIRUN_PROFILE_STATE="+profileStateMountPath,
			"-v", stateVolume+":"+profileStateMountPath)
		script = append(script, profileStateMountPath)
	} else if !strings.Contains(strings.ToLower(string(out)), "no such volume") {
		return fmt.Errorf("inspect profile state volume: %s: %w", strings.TrimSpace(string(out)), err)
	}
	args = append(args, ImageName)
	args = append(args, script...)
	cmd := exec.Command("docker", args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("collect profile cache: %w", err)
	}
	return nil
}
