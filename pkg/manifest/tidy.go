package manifest

import (
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/olimci/tohru/pkg/utils/fileutils"
)

// Tidy merges nested roots when the source/dest nesting is equivalent.
// It updates m.Roots in-place and rebuilds the compiled plan.
func (m *Manifest) Tidy() (int, error) {
	tidied, merges, err := tidy(m.Roots)
	if err != nil {
		return 0, err
	}
	m.Roots = tidied
	if err := m.Resolve(); err != nil {
		return 0, err
	}
	return merges, nil
}

func tidy(roots []Root) ([]Root, int, error) {
	out := make([]Root, len(roots))
	for i, root := range roots {
		out[i] = cloneRoot(root)
	}
	merges := 0

	for {
		parentIdx, childIdx, relParts, ok := findNestedPair(out)
		if !ok {
			break
		}

		mergedRoot, err := mergeRoot(out[parentIdx], out[childIdx], relParts)
		if err != nil {
			return nil, merges, err
		}
		out[parentIdx] = mergedRoot
		out = append(out[:childIdx], out[childIdx+1:]...)
		merges++
	}

	return out, merges, nil
}

func findNestedPair(roots []Root) (int, int, []string, bool) {
	bestParent := -1
	bestChild := -1
	bestDepth := int(^uint(0) >> 1)
	var bestRel []string

	for parentIdx, parent := range roots {
		for childIdx, child := range roots {
			if parentIdx == childIdx {
				continue
			}

			relParts, ok := nestedRootPrefix(parent, child)
			if !ok {
				continue
			}

			depth := fileutils.PathDepth(strings.TrimSpace(parent.Source))
			if bestParent == -1 || depth < bestDepth || (depth == bestDepth && parent.Source < roots[bestParent].Source) {
				bestParent = parentIdx
				bestChild = childIdx
				bestDepth = depth
				bestRel = relParts
			}
		}
	}

	if bestParent == -1 {
		return -1, -1, nil, false
	}
	return bestParent, bestChild, bestRel, true
}

func nestedRootPrefix(parent Root, child Root) ([]string, bool) {
	parentSrc := filepath.Clean(strings.TrimSpace(parent.Source))
	childSrc := filepath.Clean(strings.TrimSpace(child.Source))
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

func mergeRoot(parent Root, child Root, relParts []string) (Root, error) {
	if !reflect.DeepEqual(mergeDefaults(Defaults{}, parent.Defaults), mergeDefaults(Defaults{}, child.Defaults)) {
		return Root{}, fmt.Errorf("cannot merge nested roots at %q due to conflicting defaults", strings.Join(relParts, "."))
	}

	out := cloneRoot(parent)
	mergedTree, err := mergeRootTree(out.Tree, relParts, child.Tree)
	if err != nil {
		return Root{}, err
	}
	out.Tree = mergedTree
	return out, nil
}

func mergeRootTree(tree Tree, relParts []string, child Tree) (Tree, error) {
	out := cloneTree(tree)
	if out == nil {
		out = Tree{}
	}

	head := relParts[0]
	node, exists := out[head]
	if !exists {
		node = DirectoryNode(nil, nil)
	}
	if node.IsFile() {
		return nil, fmt.Errorf("cannot merge nested roots: %q is already a leaf", strings.Join(relParts, "."))
	}

	if len(relParts) == 1 {
		merged, err := mergeDirectoryTree(node.Dir.Tree, child, strings.Join(relParts, "."))
		if err != nil {
			return nil, err
		}
		node.Dir.Tree = merged
		out[head] = node
		return out, nil
	}

	mergedChildren, err := mergeRootTree(node.Dir.Tree, relParts[1:], child)
	if err != nil {
		return nil, err
	}
	node.Dir.Tree = mergedChildren
	out[head] = node
	return out, nil
}

func mergeDirectoryTree(dst, src Tree, base string) (Tree, error) {
	out := cloneTree(dst)
	if out == nil {
		out = Tree{}
	}

	keys := make([]string, 0, len(src))
	for key := range src {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	for _, key := range keys {
		srcVal := src[key]
		path := key
		if base != "" {
			path = base + "." + key
		}

		dstVal, exists := out[key]
		if !exists {
			out[key] = cloneNode(srcVal)
			continue
		}

		if dstVal.IsDir() && srcVal.IsDir() {
			if !reflect.DeepEqual(dstVal.Dir.Flags, srcVal.Dir.Flags) {
				return nil, fmt.Errorf("cannot merge path %q due to conflicting directory metadata", path)
			}
			merged, err := mergeDirectoryTree(dstVal.Dir.Tree, srcVal.Dir.Tree, path)
			if err != nil {
				return nil, err
			}
			dstVal.Dir.Tree = merged
			out[key] = dstVal
			continue
		}

		if reflect.DeepEqual(dstVal, srcVal) {
			continue
		}

		return nil, fmt.Errorf("cannot merge path %q due to conflicting definitions", path)
	}

	return out, nil
}

func cloneRoot(root Root) Root {
	return Root{
		Source:   root.Source,
		Dest:     root.Dest,
		Defaults: cloneDefaults(root.Defaults),
		Tree:     cloneTree(root.Tree),
	}
}

func cloneDefaults(defaults *Defaults) *Defaults {
	if defaults == nil {
		return nil
	}
	return &Defaults{
		Type:  defaults.Type,
		Track: cloneBool(defaults.Track),
	}
}
