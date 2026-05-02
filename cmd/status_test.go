package cmd

import (
	"strings"
	"testing"

	"github.com/olimci/tohru/pkg/digest"
	"github.com/olimci/tohru/pkg/store"
	"github.com/olimci/tohru/pkg/store/state"
)

func TestRenderStatusTree(t *testing.T) {
	snapshot := store.StatusSnapshot{
		Profile: state.Profile{
			State: "loaded",
			Path:  "/profiles/main",
			Slug:  "main",
			Name:  "Main",
		},
		Tracked: []store.TrackedStatus{
			{Path: "/Users/test/.config/kitty/kitty.conf", PrevDigest: "abc", BackupPresent: true, ManagedKind: digest.KindFile, Operation: "copy"},
			{Path: "/Users/test/.config/nvim", PrevDigest: "def", BackupPresent: false, ManagedKind: digest.KindDir, Operation: "copy"},
			{Path: "/Users/test/.zshrc", PrevDigest: "ghi", Drifted: true, ManagedKind: digest.KindSymlink, Operation: "link"},
			{Path: "/Users/test/.gitconfig", ManagedKind: digest.KindFile, Operation: "copy"},
		},
	}

	got, err := renderStatus(snapshot, statusRenderOptions{ColorMode: "never"})
	if err != nil {
		t.Fatalf("renderStatus() error = %v", err)
	}

	for _, want := range []string{
		"On profile Main",
		"4 tracked  1 drifted  0 missing  1 new  1 backed up  1 backup-missing",
		"Tracked objects:",
		"/Users/test",
		"◌ ⎘ .gitconfig",
		"● ↗ .zshrc",
		"⚠ ⎘ nvim/",
		"↺ ⎘ kitty.conf",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderStatus() output missing %q\noutput:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Legend:") {
		t.Fatalf("renderStatus() output unexpectedly contains legend\noutput:\n%s", got)
	}
	if strings.Contains(got, "symlink") {
		t.Fatalf("renderStatus() output unexpectedly contains symlink text\noutput:\n%s", got)
	}
}

func TestRenderStatusFlat(t *testing.T) {
	snapshot := store.StatusSnapshot{
		Tracked: []store.TrackedStatus{
			{Path: "/tmp/example", PrevDigest: "abc", BackupPresent: true, ManagedKind: digest.KindFile, Operation: "copy"},
		},
	}

	got, err := renderStatus(snapshot, statusRenderOptions{Flat: true, ColorMode: "never"})
	if err != nil {
		t.Fatalf("renderStatus() error = %v", err)
	}

	if !strings.Contains(got, "↺ ⎘ example  /tmp/example") {
		t.Fatalf("renderStatus() flat output = %q", got)
	}
}

func TestRenderStatusNoTracked(t *testing.T) {
	got, err := renderStatus(store.StatusSnapshot{}, statusRenderOptions{ColorMode: "never"})
	if err != nil {
		t.Fatalf("renderStatus() error = %v", err)
	}

	if !strings.Contains(got, "Tracked objects:\n  (none)\n") {
		t.Fatalf("renderStatus() output = %q", got)
	}
}

func TestRenderStatusNeverColorHasNoANSI(t *testing.T) {
	snapshot := store.StatusSnapshot{
		Tracked: []store.TrackedStatus{
			{Path: "/tmp/example", PrevDigest: "abc", BackupPresent: true, ManagedKind: digest.KindFile, Operation: "copy"},
		},
	}

	got, err := renderStatus(snapshot, statusRenderOptions{ColorMode: "never"})
	if err != nil {
		t.Fatalf("renderStatus() error = %v", err)
	}

	if strings.Contains(got, "\x1b[") {
		t.Fatalf("renderStatus() emitted ANSI escapes: %q", got)
	}
}

func TestRenderStatusFoldsDirectoryChains(t *testing.T) {
	snapshot := store.StatusSnapshot{
		Tracked: []store.TrackedStatus{
			{Path: "/tmp/a/b/c/file.txt", PrevDigest: "abc", BackupPresent: true, ManagedKind: digest.KindFile, Operation: "copy"},
		},
	}

	got, err := renderStatus(snapshot, statusRenderOptions{ColorMode: "never"})
	if err != nil {
		t.Fatalf("renderStatus() error = %v", err)
	}

	if !strings.Contains(got, "/tmp/a/b/c") {
		t.Fatalf("renderStatus() did not fold directory chain\noutput:\n%s", got)
	}
	if !strings.Contains(got, "↺ ⎘ file.txt") {
		t.Fatalf("renderStatus() output missing folded leaf\noutput:\n%s", got)
	}
}
