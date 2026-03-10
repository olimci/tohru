package profileutils

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

func DisplayName(slug, name, loc string) string {
	if trimmedName := strings.TrimSpace(name); trimmedName != "" {
		return trimmedName
	}
	if trimmedSlug := strings.TrimSpace(slug); trimmedSlug != "" {
		return trimmedSlug
	}
	trimmedLoc := strings.TrimSpace(loc)
	if trimmedLoc == "" {
		return ""
	}
	return filepath.Base(filepath.Clean(trimmedLoc))
}

func NormalizeSlug(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func ValidateSlug(raw, label string, allowEmpty bool) (string, error) {
	slug := NormalizeSlug(raw)
	if slug == "" {
		if allowEmpty {
			return "", nil
		}
		return "", fmt.Errorf("%s is empty", label)
	}

	for _, r := range slug {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			continue
		}
		return "", fmt.Errorf("%s %q contains invalid character %q (allowed: letters, digits, '-', '_')", label, raw, r)
	}

	return slug, nil
}
