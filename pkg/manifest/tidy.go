package manifest

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
)

// TidyTrees merges nested tree roots when the source/dest nesting is equivalent.
// It updates m.Trees in-place and re-resolves defaults.
func (m *Manifest) TidyTrees() (int, error) {
	tidied, merges, err := tidyTrees(m.Trees)
	if err != nil {
		return 0, err
	}
	m.Trees = tidied
	if err := m.ResolveDefaults(); err != nil {
		return 0, err
	}
	return merges, nil
}

func tidyTrees(trees []Tree) ([]Tree, int, error) {
	out := cloneTrees(trees)
	merges := 0

	for {
		parentIdx, childIdx, relParts, ok := findNestedPair(out)
		if !ok {
			break
		}

		mergedFiles, err := mergeTreeFiles(out[parentIdx].Files, out[childIdx].Files, relParts)
		if err != nil {
			return nil, merges, err
		}
		out[parentIdx].Files = mergedFiles

		out = append(out[:childIdx], out[childIdx+1:]...)
		merges++
	}

	return out, merges, nil
}

func findNestedPair(trees []Tree) (int, int, []string, bool) {
	bestParent := -1
	bestChild := -1
	bestDepth := int(^uint(0) >> 1)
	var bestRel []string

	for i := range trees {
		for j := range trees {
			if i == j {
				continue
			}
			relParts, ok := nestedTreePrefix(trees[i], trees[j])
			if !ok {
				continue
			}

			depth := pathDepth(strings.TrimSpace(trees[i].Source))
			if bestParent == -1 || depth < bestDepth || (depth == bestDepth && i < bestParent) {
				bestParent = i
				bestChild = j
				bestDepth = depth
				bestRel = relParts
			}
		}
	}

	if bestParent == -1 {
		return 0, 0, nil, false
	}
	return bestParent, bestChild, bestRel, true
}

func nestedTreePrefix(parent, child Tree) ([]string, bool) {
	parentSrc := filepath.Clean(strings.TrimSpace(parent.Source))
	childSrc := filepath.Clean(strings.TrimSpace(child.Source))
	parentDst := filepath.Clean(strings.TrimSpace(parent.Dest))
	childDst := filepath.Clean(strings.TrimSpace(child.Dest))
	if parentSrc == "" || childSrc == "" || parentDst == "" || childDst == "" {
		return nil, false
	}

	relSrc, err := filepath.Rel(parentSrc, childSrc)
	if err != nil || relSrc == "." || pathEscapes(relSrc) {
		return nil, false
	}
	relDst, err := filepath.Rel(parentDst, childDst)
	if err != nil || relDst == "." || pathEscapes(relDst) {
		return nil, false
	}
	if relSrc != relDst {
		return nil, false
	}

	parts := splitPathParts(relSrc)
	if len(parts) == 0 {
		return nil, false
	}
	return parts, true
}

func mergeTreeFiles(parent, child map[string]any, relParts []string) (map[string]any, error) {
	out := cloneAnyMap(parent)
	if out == nil {
		out = map[string]any{}
	}

	node := out
	for _, part := range relParts {
		raw, exists := node[part]
		if !exists {
			next := map[string]any{}
			node[part] = next
			node = next
			continue
		}
		if isLeafValue(raw) {
			return nil, fmt.Errorf("cannot merge nested tree roots: %q is already a leaf", strings.Join(relParts, "."))
		}
		next, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("cannot merge nested tree roots at %q", strings.Join(relParts, "."))
		}
		node = next
	}

	src := cloneAnyMap(child)
	if src == nil {
		return out, nil
	}
	if err := mergeNode(node, src, strings.Join(relParts, ".")); err != nil {
		return nil, err
	}

	return out, nil
}

func mergeNode(dst, src map[string]any, base string) error {
	for key, srcVal := range src {
		path := key
		if base != "" {
			path = base + "." + key
		}

		dstVal, exists := dst[key]
		if !exists {
			dst[key] = cloneAny(srcVal)
			continue
		}

		srcLeaf := isLeafValue(srcVal)
		dstLeaf := isLeafValue(dstVal)

		if !srcLeaf && !dstLeaf {
			srcMap, srcOK := srcVal.(map[string]any)
			dstMap, dstOK := dstVal.(map[string]any)
			if !srcOK || !dstOK {
				return fmt.Errorf("cannot merge path %q", path)
			}
			if err := mergeNode(dstMap, srcMap, path); err != nil {
				return err
			}
			continue
		}

		if reflect.DeepEqual(dstVal, srcVal) {
			continue
		}

		return fmt.Errorf("cannot merge path %q due to conflicting definitions", path)
	}
	return nil
}

func isLeafValue(raw any) bool {
	switch value := raw.(type) {
	case string:
		return true
	case map[string]any:
		if len(value) == 0 {
			return false
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
		return hasSpecKey && !hasNonSpecKey
	default:
		return true
	}
}

func cloneTrees(in []Tree) []Tree {
	if len(in) == 0 {
		return nil
	}
	out := make([]Tree, len(in))
	for i, tree := range in {
		out[i] = Tree{
			Source: tree.Source,
			Dest:   tree.Dest,
			Files:  cloneAnyMap(tree.Files),
		}
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneAny(v)
	}
	return out
}

func cloneAny(v any) any {
	switch value := v.(type) {
	case map[string]any:
		return cloneAnyMap(value)
	case []any:
		out := make([]any, len(value))
		for i := range value {
			out[i] = cloneAny(value[i])
		}
		return out
	default:
		return value
	}
}

func pathEscapes(rel string) bool {
	up := ".." + string(filepath.Separator)
	return rel == ".." || strings.HasPrefix(rel, up)
}

func splitPathParts(path string) []string {
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		out = append(out, p)
	}
	return out
}

func pathDepth(path string) int {
	return len(splitPathParts(path))
}
