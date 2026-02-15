package version

import "testing"

func TestParseSemVer(t *testing.T) {
	t.Parallel()

	v, err := ParseSemVer("v1.2.3")
	if err != nil {
		t.Fatalf("ParseSemVer returned error: %v", err)
	}

	if v.Major != 1 || v.Minor != 2 || v.Patch != 3 {
		t.Fatalf("ParseSemVer parsed wrong value: %#v", v)
	}
}

func TestParseSemVerRejectsInvalidFormat(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"",
		"1",
		"1.2",
		"1.2.3.4",
		"1.2.x",
		">=1.2.3",
		"1.2.3-beta",
	}

	for _, raw := range invalid {
		if _, err := ParseSemVer(raw); err == nil {
			t.Fatalf("expected parse error for %q", raw)
		}
	}
}

func TestEnsureCompatible(t *testing.T) {
	t.Parallel()

	current, err := ParseSemVer(Version)
	if err != nil {
		t.Fatalf("parse current version: %v", err)
	}

	if err := EnsureCompatible(""); err != nil {
		t.Fatalf("empty version should be accepted, got: %v", err)
	}
	if err := EnsureCompatible(current.String()); err != nil {
		t.Fatalf("current version should be compatible, got: %v", err)
	}

	newerPatch := SemVer{
		Major: current.Major,
		Minor: current.Minor,
		Patch: current.Patch + 1,
	}
	if err := EnsureCompatible(newerPatch.String()); err == nil {
		t.Fatalf("expected incompatibility for newer version %q", newerPatch.String())
	}

	nextMajor := SemVer{
		Major: current.Major + 1,
		Minor: 0,
		Patch: 0,
	}
	if err := EnsureCompatible(nextMajor.String()); err == nil {
		t.Fatalf("expected incompatibility for major mismatch %q", nextMajor.String())
	}
}
