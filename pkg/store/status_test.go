package store

import (
	"testing"

	"github.com/olimci/tohru/pkg/digest"
)

func TestTrackedPresentation(t *testing.T) {
	tests := []struct {
		name          string
		rawDigest     string
		wantKind      digest.Kind
		wantOperation string
		wantErr       bool
	}{
		{
			name:          "file digest maps to copy",
			rawDigest:     "file:sha256:abc123",
			wantKind:      digest.KindFile,
			wantOperation: "copy",
		},
		{
			name:          "dir digest maps to copy",
			rawDigest:     "dir:sha256:def456",
			wantKind:      digest.KindDir,
			wantOperation: "copy",
		},
		{
			name:          "symlink digest maps to link",
			rawDigest:     "symlink:sha256:fedcba",
			wantKind:      digest.KindSymlink,
			wantOperation: "link",
		},
		{
			name:      "null digest has no operation",
			rawDigest: "null",
			wantKind:  digest.KindNull,
		},
		{
			name:      "empty digest has no metadata",
			rawDigest: "",
		},
		{
			name:      "invalid digest errors",
			rawDigest: "oops",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKind, gotOperation, err := trackedPresentation(tt.rawDigest)
			if (err != nil) != tt.wantErr {
				t.Fatalf("trackedPresentation() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if gotKind != tt.wantKind {
				t.Fatalf("trackedPresentation() kind = %q, want %q", gotKind, tt.wantKind)
			}
			if gotOperation != tt.wantOperation {
				t.Fatalf("trackedPresentation() operation = %q, want %q", gotOperation, tt.wantOperation)
			}
		})
	}
}
