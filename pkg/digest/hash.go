package digest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// ForPath computes the digest of the object at path.
func ForPath(path string) (Digest, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Digest{}, err
	}

	return forPathWithInfo(path, info)
}

func forPathWithInfo(path string, info os.FileInfo) (Digest, error) {
	mode := info.Mode()

	switch {
	case mode&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return Digest{}, fmt.Errorf("read symlink %s: %w", path, err)
		}
		sum := sha256.Sum256([]byte(target))
		return New(KindSymlink, AlgorithmSHA256, hex.EncodeToString(sum[:]))
	case mode.IsRegular():
		sum, err := hashFile(path)
		if err != nil {
			return Digest{}, err
		}
		return New(KindFile, AlgorithmSHA256, sum)
	case mode.IsDir():
		sum, err := hashDir(path)
		if err != nil {
			return Digest{}, err
		}
		return New(KindDir, AlgorithmSHA256, sum)
	default:
		return Digest{}, fmt.Errorf("unsupported file type at %s (%s)", path, mode.String())
	}
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file %s: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash file %s: %w", path, err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

type dirRecord struct {
	RelPath string
	Type    string
	Payload string
}

func hashDir(root string) (string, error) {
	records := make([]dirRecord, 0, 32)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		rec := dirRecord{
			RelPath: filepath.ToSlash(rel),
		}

		switch {
		case d.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			rec.Type = "symlink"
			rec.Payload = target
		case d.Type().IsRegular():
			fileHash, err := hashFile(path)
			if err != nil {
				return err
			}
			rec.Type = "file"
			rec.Payload = fileHash
		case d.IsDir():
			rec.Type = "dir"
			rec.Payload = ""
		default:
			return fmt.Errorf("unsupported file type in directory hash: %s", path)
		}

		records = append(records, rec)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walk directory %s: %w", root, err)
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].RelPath < records[j].RelPath
	})

	h := sha256.New()
	for _, rec := range records {
		if _, err := io.WriteString(h, rec.RelPath+"\n"); err != nil {
			return "", err
		}
		if _, err := io.WriteString(h, rec.Type+"\n"); err != nil {
			return "", err
		}
		if _, err := io.WriteString(h, rec.Payload+"\n"); err != nil {
			return "", err
		}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
