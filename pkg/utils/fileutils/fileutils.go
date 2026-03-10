package fileutils

import (
	"cmp"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func ExpandHome(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
	}

	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}

	return path
}

func AbsPath(path string) (string, error) {
	expanded := ExpandHome(strings.TrimSpace(path))
	if expanded == "" {
		return "", fmt.Errorf("path is empty")
	}

	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path for %q: %w", path, err)
	}

	return filepath.Clean(abs), nil
}

func CopyFile(src, dest string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source file %s: %w", src, err)
	}
	if !srcInfo.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file: %s", src)
	}

	destDir := filepath.Dir(dest)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create parent directory for %s: %w", dest, err)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source file %s: %w", src, err)
	}
	defer srcFile.Close()

	dstFile, err := os.CreateTemp(destDir, filepath.Base(dest)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", dest, err)
	}
	tmpDest := dstFile.Name()
	if err := dstFile.Chmod(srcInfo.Mode().Perm()); err != nil {
		_ = dstFile.Close()
		_ = os.Remove(tmpDest)
		return fmt.Errorf("chmod temporary file %s: %w", tmpDest, err)
	}

	_, copyErr := io.Copy(dstFile, srcFile)
	closeErr := dstFile.Close()
	if copyErr != nil {
		_ = os.Remove(tmpDest)
		return fmt.Errorf("copy %s to %s: %w", src, tmpDest, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpDest)
		return fmt.Errorf("close temporary file %s: %w", tmpDest, closeErr)
	}

	if err := os.Rename(tmpDest, dest); err != nil {
		_ = os.Remove(tmpDest)
		return fmt.Errorf("replace %s with %s: %w", dest, tmpDest, err)
	}

	return nil
}

// CopyPath copies a filesystem object at src to dest.
// It preserves symlink targets, regular file modes, and directory structure.
func CopyPath(src, dest string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("stat source path %s: %w", src, err)
	}

	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return fmt.Errorf("read symlink %s: %w", src, err)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("create parent directory for %s: %w", dest, err)
		}
		if err := os.Symlink(target, dest); err != nil {
			return fmt.Errorf("create symlink %s -> %s: %w", dest, target, err)
		}
		return nil
	case info.Mode().IsRegular():
		return CopyFile(src, dest)
	case info.IsDir():
		return copyDir(src, dest)
	default:
		return fmt.Errorf("unsupported source type at %s (%s)", src, info.Mode().String())
	}
}

func RemovePath(path string) error {
	clean := filepath.Clean(path)
	if clean == "." || clean == string(filepath.Separator) {
		return fmt.Errorf("refusing to remove unsafe path: %s", path)
	}

	info, err := os.Lstat(clean)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return os.RemoveAll(clean)
	}

	return os.Remove(clean)
}

func PathDepth(path string) int {
	return len(SplitPathParts(path))
}

func CompareDepth(a, b string) int {
	if depth := cmp.Compare(PathDepth(a), PathDepth(b)); depth != 0 {
		return depth
	}
	return strings.Compare(a, b)
}

func SortByDepth(paths []string, descending bool) []string {
	sorted := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))

	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		sorted = append(sorted, path)
	}

	slices.SortFunc(sorted, func(a, b string) int {
		order := CompareDepth(a, b)
		if descending {
			return -order
		}
		return order
	})

	return sorted
}

func Escapes(rel string) bool {
	up := ".." + string(filepath.Separator)
	return rel == ".." || strings.HasPrefix(rel, up)
}

func SplitPathParts(path string) []string {
	clean := filepath.Clean(path)
	if clean == "." {
		return nil
	}

	raw := strings.Split(clean, string(filepath.Separator))
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		if part == "" || part == "." {
			continue
		}
		parts = append(parts, part)
	}
	return parts
}

func copyDir(srcRoot, destRoot string) error {
	err := filepath.WalkDir(srcRoot, func(srcPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(srcRoot, srcPath)
		if err != nil {
			return err
		}

		destPath := destRoot
		if rel != "." {
			destPath = filepath.Join(destRoot, rel)
		}

		info, err := os.Lstat(srcPath)
		if err != nil {
			return err
		}

		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(srcPath)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(target, destPath); err != nil {
				return err
			}
		case info.IsDir():
			if err := os.MkdirAll(destPath, info.Mode().Perm()); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			if err := CopyFile(srcPath, destPath); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported source type at %s (%s)", srcPath, info.Mode().String())
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("copy directory %s to %s: %w", srcRoot, destRoot, err)
	}

	return nil
}
