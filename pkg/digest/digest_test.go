package digest

import "testing"

func TestParseRoundTrip(t *testing.T) {
	t.Parallel()

	raw := "file:sha256:abcd1234"

	v, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if got := v.String(); got != raw {
		t.Fatalf("String() = %q, want %q", got, raw)
	}
}

func TestParseRejectsInvalidFormat(t *testing.T) {
	t.Parallel()

	if _, err := Parse("file:sha256"); err == nil {
		t.Fatal("expected parse error for malformed digest")
	}
}

func TestParseEmptyIsZero(t *testing.T) {
	t.Parallel()

	v, err := Parse("")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if !v.IsZero() {
		t.Fatalf("expected zero digest for empty input, got %#v", v)
	}
}
