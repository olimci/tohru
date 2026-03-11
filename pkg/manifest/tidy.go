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
	out := cloneRoot(parent)
	mergedEntries, err := mergeRootEntries(out.Entries, relParts, child)
	if err != nil {
		return Root{}, err
	}
	out.Entries = mergedEntries
	return out, nil
}

func mergeRootEntries(entries map[string]Entry, relParts []string, child Root) (map[string]Entry, error) {
	out := cloneEntries(entries)
	if out == nil {
		out = map[string]Entry{}
	}

	head := relParts[0]
	entry, exists := out[head]
	if !exists {
		entry = Entry{}
	}

	if len(relParts) == 1 {
		merged, err := mergeContainerEntry(entry, rootAsEntry(child), strings.Join(relParts, "."))
		if err != nil {
			return nil, err
		}
		out[head] = merged
		return out, nil
	}

	entryType := strings.ToLower(strings.TrimSpace(entry.Type))
	if entryType == "copy" || entryType == "link" {
		return nil, fmt.Errorf("cannot merge nested roots: %q is already a leaf", strings.Join(relParts, "."))
	}

	mergedChildren, err := mergeRootEntries(entry.Entries, relParts[1:], child)
	if err != nil {
		return nil, err
	}
	entry.Entries = mergedChildren
	out[head] = entry
	return out, nil
}

func mergeContainerEntry(dst Entry, src Entry, path string) (Entry, error) {
	dstType := strings.ToLower(strings.TrimSpace(dst.Type))
	if dstType == "copy" || dstType == "link" {
		return Entry{}, fmt.Errorf("cannot merge nested roots: %q is already a leaf", path)
	}
	if len(src.Entries) == 0 {
		return cloneEntry(dst), nil
	}

	out := cloneEntry(dst)
	if out.Defaults == nil {
		out.Defaults = cloneDefaults(src.Defaults)
	} else if src.Defaults != nil && !reflect.DeepEqual(out.Defaults, src.Defaults) {
		return Entry{}, fmt.Errorf("cannot merge path %q due to conflicting defaults", path)
	}

	mergedEntries, err := mergeEntryMaps(out.Entries, src.Entries, path)
	if err != nil {
		return Entry{}, err
	}
	out.Entries = mergedEntries
	return out, nil
}

func mergeEntryMaps(dst, src map[string]Entry, base string) (map[string]Entry, error) {
	out := cloneEntries(dst)
	if out == nil {
		out = map[string]Entry{}
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
			out[key] = cloneEntry(srcVal)
			continue
		}

		srcLeaf := len(srcVal.Entries) == 0
		dstLeaf := len(dstVal.Entries) == 0

		if !srcLeaf && !dstLeaf {
			merged, err := mergeContainerEntry(dstVal, srcVal, path)
			if err != nil {
				return nil, err
			}
			out[key] = merged
			continue
		}

		if reflect.DeepEqual(dstVal, srcVal) {
			continue
		}

		return nil, fmt.Errorf("cannot merge path %q due to conflicting definitions", path)
	}
	return out, nil
}

func rootAsEntry(root Root) Entry {
	return Entry{
		Defaults: cloneDefaults(root.Defaults),
		Entries:  cloneEntries(root.Entries),
	}
}

func cloneRoot(root Root) Root {
	return Root{
		Source:   root.Source,
		Dest:     root.Dest,
		Defaults: cloneDefaults(root.Defaults),
		Entries:  cloneEntries(root.Entries),
	}
}

func cloneEntries(entries map[string]Entry) map[string]Entry {
	if entries == nil {
		return nil
	}
	out := make(map[string]Entry, len(entries))
	for key, entry := range entries {
		out[key] = cloneEntry(entry)
	}
	return out
}

func cloneEntry(entry Entry) Entry {
	return Entry{
		Type:     entry.Type,
		Track:    cloneBool(entry.Track),
		Defaults: cloneDefaults(entry.Defaults),
		Entries:  cloneEntries(entry.Entries),
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
