package keys

import "testing"

func TestProviderByAlias(t *testing.T) {
	tests := []struct {
		alias string
		want  string
	}{
		{"z", "zai"}, {"zai", "zai"},
		{"m", "minimax"}, {"mm", "minimax"}, {"minimax", "minimax"},
		{"k", "kimi"}, {"kimi", "kimi"},
	}
	for _, tt := range tests {
		p := ProviderByAlias(tt.alias)
		if p == nil {
			t.Fatalf("ProviderByAlias(%q) = nil", tt.alias)
		}
		if p.ID != tt.want {
			t.Errorf("ProviderByAlias(%q).ID = %q, want %q", tt.alias, p.ID, tt.want)
		}
	}
}

func TestProviderByAliasUnknown(t *testing.T) {
	if p := ProviderByAlias("unknown"); p != nil {
		t.Errorf("ProviderByAlias(%q) = %v, want nil", "unknown", p)
	}
}

func TestAllProviders(t *testing.T) {
	all := AllProviders()
	if len(all) != 3 {
		t.Fatalf("AllProviders() returned %d, want 3", len(all))
	}
}
