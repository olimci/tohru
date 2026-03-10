package manifest

import (
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/olimci/tohru/pkg/utils/cloneutils"
	"github.com/olimci/tohru/pkg/utils/fileutils"
)

// Tidy merges nested tree roots when the source/dest nesting is equivalent.
// It updates m.Trees in-place and rebuilds the compiled plan.
func (m *Manifest) Tidy() (int, error) {
	tidied, merges, err := tidy(m.Trees)
	if err != nil {
		return 0, err
	}
	m.Trees = tidied
	if err := m.Resolve(); err != nil {
		return 0, err
	}
	return merges, nil
}

func tidy(trees map[string]Tree) (map[string]Tree, int, error) {
	out := maps.Clone(trees)
	for source, tree := range out {
		tree.Files = cloneutils.AnyMap(tree.Files)
		out[source] = tree
	}
	merges := 0

	for {
		parentSource, childSource, relParts, ok := findNestedPair(out)
		if !ok {
			break
		}

		parent := out[parentSource]
		child := out[childSource]

		mergedFiles, err := mergeTreeFiles(parent.Files, child.Files, relParts)
		if err != nil {
			return nil, merges, err
		}
		parent.Files = mergedFiles
		out[parentSource] = parent
		delete(out, childSource)
		merges++
	}

	return out, merges, nil
}

func findNestedPair(trees map[string]Tree) (string, string, []string, bool) {
	sources := slices.Sorted(maps.Keys(trees))
	bestParent := ""
	bestChild := ""
	bestDepth := int(^uint(0) >> 1)
	var bestRel []string

	for _, parentSource := range sources {
		parent := trees[parentSource]
		for _, childSource := range sources {
			if parentSource == childSource {
				continue
			}
			child := trees[childSource]

			relParts, ok := nestedTreePrefix(parentSource, parent, childSource, child)
			if !ok {
				continue
			}

			depth := fileutils.PathDepth(strings.TrimSpace(parentSource))
			if bestParent == "" || depth < bestDepth || (depth == bestDepth && parentSource < bestParent) {
				bestParent = parentSource
				bestChild = childSource
				bestDepth = depth
				bestRel = relParts
			}
		}
	}

	if bestParent == "" {
		return "", "", nil, false
	}
	return bestParent, bestChild, bestRel, true
}

func nestedTreePrefix(parentSource string, parent Tree, childSource string, child Tree) ([]string, bool) {
	parentSrc := filepath.Clean(strings.TrimSpace(parentSource))
	childSrc := filepath.Clean(strings.TrimSpace(childSource))
	parentDst := filepath.Clean(strings.TrimSpace(parent.Dest))
	childDst := filepath.Clean(strings.TrimSpace(child.Dest))
	if parentSrc == "" || childSrc == "" || parentDst == "" || childDst == "" {
		return nil, false
	}

	relSrc, err := filepath.Rel(parentSrc, childSrc)
	if err != nil || relSrc == "." || fileutils.Escapes(relSrc) {
		return nil, false
	}
	relDst, err := filepath.Rel(parentDst, childDst)
	if err != nil || relDst == "." || fileutils.Escapes(relDst) {
		return nil, false
	}
	if relSrc != relDst {
		return nil, false
	}

	parts := fileutils.SplitPathParts(relSrc)
	if len(parts) == 0 {
		return nil, false
	}
	return parts, true
}

func mergeTreeFiles(parent, child map[string]any, relParts []string) (map[string]any, error) {
	out := cloneutils.AnyMap(parent)
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
		if IsLeafSpec(raw) {
			return nil, fmt.Errorf("cannot merge nested tree roots: %q is already a leaf", strings.Join(relParts, "."))
		}
		next, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("cannot merge nested tree roots at %q", strings.Join(relParts, "."))
		}
		node = next
	}

	src := cloneutils.AnyMap(child)
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
			dst[key] = cloneutils.Any(srcVal)
			continue
		}

		srcLeaf := IsLeafSpec(srcVal)
		dstLeaf := IsLeafSpec(dstVal)

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
