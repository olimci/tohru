package manifest

import (
	"path/filepath"
	"strings"
)

const sourceDotEscapePrefix = "dot_"

// EncodeSourcePart maps destination path segments to source tree segments.
// Hidden segments are encoded with `dot_` and literal `dot_` prefixes are escaped.
func EncodeSourcePart(part string) string {
	switch {
	case strings.HasPrefix(part, "."):
		return sourceDotEscapePrefix + part[1:]
	case strings.HasPrefix(part, sourceDotEscapePrefix):
		return sourceDotEscapePrefix + "_" + part[len(sourceDotEscapePrefix):]
	default:
		return part
	}
}

func EncodeSourceParts(parts []string) []string {
	out := make([]string, len(parts))
	for i, part := range parts {
		out[i] = EncodeSourcePart(part)
	}
	return out
}

func SourcePath(sourceRoot string, parts []string) string {
	pathParts := make([]string, 0, len(parts)+1)
	pathParts = append(pathParts, sourceRoot)
	pathParts = append(pathParts, EncodeSourceParts(parts)...)
	return filepath.Join(pathParts...)
}
