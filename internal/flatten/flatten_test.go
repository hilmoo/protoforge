package flatten

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtures = "../../testdata/fixtures/flatten"

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func compareDir(t *testing.T, got, want string) {
	t.Helper()
	wantFiles, _ := filepath.Glob(filepath.Join(want, "*.proto"))
	gotFiles, _ := filepath.Glob(filepath.Join(got, "*.proto"))
	if len(wantFiles) == 0 || len(wantFiles) != len(gotFiles) {
		t.Fatalf("file count: got %d want %d", len(gotFiles), len(wantFiles))
	}
	for _, w := range wantFiles {
		g := filepath.Join(got, filepath.Base(w))
		gb, err := os.ReadFile(g)
		if err != nil {
			t.Fatal(err)
		}
		wb, _ := os.ReadFile(w)
		if !bytes.Equal(gb, wb) {
			t.Errorf("%s differs from golden output", filepath.Base(w))
		}
	}
}

// TestGolden: "sample" is testdata/sample's services directory as produced by
// openapi-generator before flatten_services.py; its expected output is
// proto/registry/services/v1 (also the oracle's output). "synthetic" covers
// repeated model fields, nested messages, map fields, WKTs and enums.
func TestGolden(t *testing.T) {
	for _, name := range []string{"sample", "synthetic"} {
		t.Run(name, func(t *testing.T) {
			reg := t.TempDir()
			copyTree(t, filepath.Join(fixtures, name, "input"), reg)
			if err := Run(Options{Registry: reg, Version: "v1"}); err != nil {
				t.Fatal(err)
			}
			compareDir(t, filepath.Join(reg, "services", "v1"), filepath.Join(fixtures, name, "expected"))
		})
	}
}

func TestDuplicateFieldNumber(t *testing.T) {
	reg := t.TempDir()
	copyTree(t, filepath.Join(fixtures, "dup", "input"), reg)
	err := Run(Options{Registry: reg, Version: "v1"})
	if err == nil || err.Error() != "DupRequest: duplicate field number 1" {
		t.Fatalf("want duplicate field number error, got %v", err)
	}
}

// TestOtherVersion: a non-v1 package version rewrites the prefixes consistently.
func TestOtherVersion(t *testing.T) {
	reg := t.TempDir()
	copyTree(t, filepath.Join(fixtures, "synthetic", "input"), reg)
	for _, dir := range []string{"models", "services"} {
		src := filepath.Join(reg, dir, "v1")
		dst := filepath.Join(reg, dir, "v2")
		if err := os.Rename(src, dst); err != nil {
			t.Fatal(err)
		}
		files, _ := filepath.Glob(filepath.Join(dst, "*.proto"))
		for _, f := range files {
			b, _ := os.ReadFile(f)
			s := strings.ReplaceAll(string(b), "models.v1.", "models.v2.")
			s = strings.ReplaceAll(s, "models/v1/", "models/v2/")
			os.WriteFile(f, []byte(s), 0o644)
		}
	}
	if err := Run(Options{Registry: reg, Version: "v2"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(reg, "services", "v2", "pet_service.proto"))
	want, _ := os.ReadFile(filepath.Join(fixtures, "synthetic", "expected", "pet_service.proto"))
	w := strings.ReplaceAll(strings.ReplaceAll(string(want), "models.v1.", "models.v2."), "models/v1/", "models/v2/")
	if string(got) != w {
		t.Fatalf("v2 output differs:\n%s", got)
	}
}
