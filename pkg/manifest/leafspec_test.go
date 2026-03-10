package manifest

import "testing"

func TestIsLeafSpec(t *testing.T) {
	tests := []struct {
		name string
		raw  any
		want bool
	}{
		{name: "string mode", raw: "copy", want: true},
		{name: "spec map", raw: map[string]any{"mode": "copy", "tracked": false}, want: true},
		{name: "dir map", raw: map[string]any{"kind": "dir"}, want: true},
		{name: "empty map", raw: map[string]any{}, want: false},
		{name: "nested map", raw: map[string]any{"kitty": map[string]any{"mode": "copy"}}, want: false},
		{name: "mixed map", raw: map[string]any{"mode": "copy", "kitty": map[string]any{}}, want: false},
		{name: "other scalar", raw: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsLeafSpec(tt.raw); got != tt.want {
				t.Fatalf("IsLeafSpec(%#v) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestIsDirSpec(t *testing.T) {
	if !IsDirSpec(map[string]any{"kind": "dir"}) {
		t.Fatal("expected dir leaf spec to be recognized")
	}
	if IsDirSpec(map[string]any{"mode": "copy"}) {
		t.Fatal("expected file leaf spec to not be treated as a dir")
	}
}
