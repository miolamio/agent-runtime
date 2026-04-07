package students

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentAddAndFind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "students.json")
	os.WriteFile(path, []byte("[]"), 0600)
	mgr := New(path)

	const numWorkers = 10
	const usersPerWorker = 5

	var wg sync.WaitGroup
	type result struct {
		name  string
		token string
	}
	results := make(chan result, numWorkers*usersPerWorker)

	// Concurrent Add
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < usersPerWorker; i++ {
				name := fmt.Sprintf("w%d_u%d", workerID, i)
				tok, err := mgr.Add(name)
				if err != nil {
					t.Errorf("Add %s: %v", name, err)
					return
				}
				results <- result{name: name, token: tok}
			}
		}(w)
	}

	wg.Wait()
	close(results)

	// Verify all tokens resolve correctly
	for r := range results {
		s := mgr.FindByToken(r.token)
		if s == nil {
			t.Errorf("FindByToken nil for %s", r.name)
			continue
		}
		if s.Name != r.name {
			t.Errorf("FindByToken(%s) = %q, want %q", r.token[:8], s.Name, r.name)
		}
	}

	// Verify total count
	all := mgr.List()
	expected := numWorkers * usersPerWorker
	if len(all) != expected {
		t.Errorf("List() len = %d, want %d", len(all), expected)
	}
}
