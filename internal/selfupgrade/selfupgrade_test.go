package selfupgrade

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int // <0, 0, >0
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"2.0.0", "1.9.9", 1},
		{"0.0.8", "0.0.5", 1},
		{"0.0.5", "0.0.8", -1},
		{"1.0", "1.0.0", 0},
		{"1.0.0", "1.0", 0},
		{"0.1.0", "0.0.10", 1},
		{"10.0.0", "9.9.9", 1},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%s_vs_%s", tc.a, tc.b), func(t *testing.T) {
			got := compareSemver(tc.a, tc.b)
			switch {
			case tc.want < 0 && got >= 0:
				t.Errorf("compareSemver(%q, %q) = %d, want < 0", tc.a, tc.b, got)
			case tc.want == 0 && got != 0:
				t.Errorf("compareSemver(%q, %q) = %d, want 0", tc.a, tc.b, got)
			case tc.want > 0 && got <= 0:
				t.Errorf("compareSemver(%q, %q) = %d, want > 0", tc.a, tc.b, got)
			}
		})
	}
}

func TestExtractBinaryFromTarGz(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "test.tar.gz")
	destPath := filepath.Join(dir, "extracted")

	// Create a test tar.gz with two files: "tentacle" and "tentacle-experimental"
	createTestArchive(t, archivePath, map[string]string{
		"tentacle":              "binary-content-main",
		"tentacle-experimental": "binary-content-exp",
	})

	// Extract "tentacle"
	if err := extractBinaryFromTarGz(archivePath, "tentacle", destPath); err != nil {
		t.Fatalf("extractBinaryFromTarGz: %v", err)
	}

	content, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if string(content) != "binary-content-main" {
		t.Errorf("got %q, want %q", string(content), "binary-content-main")
	}
}

func TestExtractBinaryFromTarGz_NotFound(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "test.tar.gz")
	destPath := filepath.Join(dir, "extracted")

	createTestArchive(t, archivePath, map[string]string{
		"other-binary": "content",
	})

	err := extractBinaryFromTarGz(archivePath, "tentacle", destPath)
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
	if got := err.Error(); got != `binary "tentacle" not found in archive` {
		t.Errorf("got error %q, want 'binary not found' message", got)
	}
}

func TestOfflineError(t *testing.T) {
	cause := fmt.Errorf("dial tcp: lookup joyautomation.com: no such host")
	err := &OfflineError{Cause: cause}

	if got := err.Error(); got != "unable to reach release manifest — check your internet connection" {
		t.Errorf("got %q", got)
	}

	var offline *OfflineError
	if !errors.As(err, &offline) {
		t.Error("errors.As should match OfflineError")
	}

	if !errors.Is(err, cause) {
		t.Error("Unwrap should return cause")
	}
}

func TestGetStatus_DefaultIdle(t *testing.T) {
	// Reset status to idle for this test
	setStatus("idle", "", "")
	s := GetStatus()
	if s.State != "idle" {
		t.Errorf("got state %q, want idle", s.State)
	}
}

func TestSetStatus(t *testing.T) {
	setStatus("downloading", "1.2.3", "")
	s := GetStatus()
	if s.State != "downloading" || s.Version != "1.2.3" || s.Error != "" {
		t.Errorf("unexpected status: %+v", s)
	}

	setStatus("failed", "1.2.3", "network error")
	s = GetStatus()
	if s.State != "failed" || s.Error != "network error" {
		t.Errorf("unexpected status: %+v", s)
	}

	// Reset
	setStatus("idle", "", "")
}

// createTestArchive makes a tar.gz with the given filename→content entries.
func createTestArchive(t *testing.T, path string, files map[string]string) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0755,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
}
