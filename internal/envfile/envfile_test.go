package envfile

import (
	"os"
	"strings"
	"testing"
)

func TestWrite_Permissions(t *testing.T) {
	path, err := Write([]string{"FOO=bar"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	defer Cleanup(path)
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Errorf("permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestWrite_Content(t *testing.T) {
	vars := []string{"KEY1=val1", "KEY2=val2", "KEY3=val=with=equals"}
	path, err := Write(vars)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	defer Cleanup(path)
	data, _ := os.ReadFile(path)
	for _, v := range vars {
		if !strings.Contains(string(data), v) {
			t.Errorf("file missing %q", v)
		}
	}
}

func TestCleanup_ValidPrefix(t *testing.T) {
	path, _ := Write([]string{"X=1"})
	Cleanup(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file still exists after Cleanup")
	}
}

func TestCleanup_WrongPrefix(t *testing.T) {
	f, _ := os.CreateTemp(os.TempDir(), "not-airun-*.txt")
	f.Close()
	defer os.Remove(f.Name())
	Cleanup(f.Name())
	if _, err := os.Stat(f.Name()); os.IsNotExist(err) {
		t.Error("Cleanup deleted file without .airun- prefix")
	}
}

func TestCleanup_EmptyPath(t *testing.T) {
	Cleanup("") // should not panic
}

func TestMaskLog(t *testing.T) {
	got := MaskLog("/some/long/path/.airun-abc.env")
	if got != ".airun-abc.env" {
		t.Errorf("MaskLog = %q", got)
	}
}
