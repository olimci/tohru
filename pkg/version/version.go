package version

import (
	"fmt"
	"strconv"
	"strings"
)

const Version = "0.1.0"

// SemVer is a minimal semantic version representation.
type SemVer struct {
	Major int
	Minor int
	Patch int
}

func (v SemVer) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// ParseSemVer parses versions in the form "MAJOR.MINOR.PATCH" with an optional "v" prefix.
func ParseSemVer(raw string) (SemVer, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return SemVer{}, fmt.Errorf("version is empty")
	}

	value = strings.TrimPrefix(value, "v")
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return SemVer{}, fmt.Errorf("invalid semantic version %q (expected MAJOR.MINOR.PATCH)", raw)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil || major < 0 {
		return SemVer{}, fmt.Errorf("invalid major version in %q", raw)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil || minor < 0 {
		return SemVer{}, fmt.Errorf("invalid minor version in %q", raw)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil || patch < 0 {
		return SemVer{}, fmt.Errorf("invalid patch version in %q", raw)
	}

	return SemVer{
		Major: major,
		Minor: minor,
		Patch: patch,
	}, nil
}

// EnsureCompatible validates whether a target version is supported by the current app version.
// Empty versions are treated as compatible for backward compatibility with older configs/manifests.
func EnsureCompatible(target string) error {
	value := strings.TrimSpace(target)
	if value == "" {
		return nil
	}

	current, err := ParseSemVer(Version)
	if err != nil {
		return fmt.Errorf("parse current version %q: %w", Version, err)
	}
	required, err := ParseSemVer(value)
	if err != nil {
		return err
	}

	if required.Major != current.Major {
		return fmt.Errorf("unsupported major version %d (current major is %d)", required.Major, current.Major)
	}
	if compare(current, required) < 0 {
		return fmt.Errorf("requires tohru >= %s (current %s)", required.String(), current.String())
	}

	return nil
}

func compare(a, b SemVer) int {
	if a.Major != b.Major {
		if a.Major < b.Major {
			return -1
		}
		return 1
	}
	if a.Minor != b.Minor {
		if a.Minor < b.Minor {
			return -1
		}
		return 1
	}
	if a.Patch != b.Patch {
		if a.Patch < b.Patch {
			return -1
		}
		return 1
	}
	return 0
}
