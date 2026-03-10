package manifest

import (
	"fmt"
	"strings"
)

type Leaf struct {
	Mode    string
	Kind    string
	Tracked *bool
}

func IsLeafSpec(raw any) bool {
	_, ok, err := DecodeLeaf(raw)
	return err == nil && ok
}

func IsDirSpec(raw any) bool {
	spec, ok, err := DecodeLeaf(raw)
	return err == nil && ok && strings.EqualFold(strings.TrimSpace(spec.Kind), "dir")
}

func DecodeLeaf(raw any) (Leaf, bool, error) {
	switch value := raw.(type) {
	case string:
		return Leaf{Mode: value}, true, nil
	case map[string]any:
		hasSpecKey, hasNonSpecKey := leafSpecKeyFlags(value)
		if !hasSpecKey {
			return Leaf{}, false, nil
		}
		if hasNonSpecKey {
			return Leaf{}, false, fmt.Errorf("cannot mix spec keys (mode/kind/tracked) with nested keys")
		}

		spec := Leaf{}
		for key, rawField := range value {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "mode":
				mode, ok := rawField.(string)
				if !ok {
					return Leaf{}, false, fmt.Errorf("mode must be a string")
				}
				spec.Mode = mode
			case "kind":
				kind, ok := rawField.(string)
				if !ok {
					return Leaf{}, false, fmt.Errorf("kind must be a string")
				}
				spec.Kind = kind
			case "tracked":
				tracked, ok := rawField.(bool)
				if !ok {
					return Leaf{}, false, fmt.Errorf("tracked must be a boolean")
				}
				trackedCopy := tracked
				spec.Tracked = &trackedCopy
			}
		}
		return spec, true, nil
	default:
		return Leaf{}, false, fmt.Errorf("unsupported value type %T", raw)
	}
}

func leafSpecKeyFlags(value map[string]any) (bool, bool) {
	if len(value) == 0 {
		return false, false
	}

	hasSpecKey := false
	hasNonSpecKey := false
	for key := range value {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "mode", "kind", "tracked":
			hasSpecKey = true
		default:
			hasNonSpecKey = true
		}
	}

	return hasSpecKey, hasNonSpecKey
}
