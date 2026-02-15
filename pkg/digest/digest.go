package digest

import (
	"fmt"
	"strings"
)

// Kind represents the type of object
type Kind string

const (
	KindNull    Kind = "null"
	KindFile    Kind = "file"
	KindDir     Kind = "dir"
	KindSymlink Kind = "symlink"
)

const AlgorithmSHA256 = "sha256"

func New(kind Kind, algorithm, sum string) (Digest, error) {
	if err := validateKind(kind); err != nil {
		return Digest{}, err
	}

	if kind == KindNull {
		if algorithm != "" || sum != "" {
			return Digest{}, fmt.Errorf("null digest must not include algorithm or sum")
		}
		return Digest{Kind: KindNull}, nil
	}

	if strings.TrimSpace(algorithm) == "" {
		return Digest{}, fmt.Errorf("digest algorithm is required")
	}
	if strings.TrimSpace(sum) == "" {
		return Digest{}, fmt.Errorf("digest sum is required")
	}

	return Digest{
		Kind:      kind,
		Algorithm: strings.TrimSpace(algorithm),
		Sum:       strings.TrimSpace(sum),
	}, nil
}

// Digest is a typed digest value.
// represented as "<kind>:<algorithm>:<hex>".
type Digest struct {
	Kind      Kind
	Algorithm string
	Sum       string
}

func (d Digest) IsZero() bool {
	return d.Kind == "" && d.Algorithm == "" && d.Sum == ""
}

func (d Digest) String() string {
	if d.IsZero() {
		return ""
	}
	if d.Kind == KindNull {
		return string(KindNull)
	}
	return fmt.Sprintf("%s:%s:%s", d.Kind, d.Algorithm, d.Sum)
}

func Parse(raw string) (Digest, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Digest{}, nil
	}
	if raw == string(KindNull) {
		return Digest{Kind: KindNull}, nil
	}

	parts := strings.Split(raw, ":")
	if len(parts) != 3 {
		return Digest{}, fmt.Errorf("invalid digest %q (expected kind:algorithm:sum)", raw)
	}

	return New(Kind(parts[0]), parts[1], parts[2])
}

func validateKind(kind Kind) error {
	switch kind {
	case KindNull, KindFile, KindDir, KindSymlink:
		return nil
	default:
		return fmt.Errorf("unsupported digest kind %q", kind)
	}
}
