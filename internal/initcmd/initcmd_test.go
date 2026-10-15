package initcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hilmoo/protoforge/internal/config"
)

func TestInitEmptyDir(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := Run(Options{Dir: dir, Out: &out}); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(filepath.Join(dir, config.FileName)); err != nil {
		t.Fatalf("generated config does not load: %v", err)
	}
	for _, f := range []string{"openapi/tspconfig.yaml", "openapi/oaf-config.yaml", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s not written", f)
		}
	}
	if !strings.Contains(out.String(), "pnpm add -D") {
		t.Error("install line not printed")
	}
	// Second run never overwrites without --force, and does not duplicate .gitignore entries.
	os.WriteFile(filepath.Join(dir, config.FileName), []byte("version: 1\n"), 0o644)
	out.Reset()
	if err := Run(Options{Dir: dir, Out: &out}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, config.FileName)); string(b) != "version: 1\n" {
		t.Fatal("overwrote protoforge.yaml without --force")
	}
	if gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); strings.Count(string(gi), "# protoforge") != 1 {
		t.Fatalf(".gitignore duplicated:\n%s", gi)
	}
	if err := Run(Options{Dir: dir, Force: true, Out: &out}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, config.FileName)); string(b) == "version: 1\n" {
		t.Fatal("--force did not overwrite")
	}
}

func TestDetect(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"db/drizzle.config.ts", "api/spec/main.tsp", "api/openapi.json", "node_modules/x/main.tsp"} {
		p := filepath.Join(dir, filepath.FromSlash(f))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, nil, 0o644)
	}
	d := detect(dir)
	if d.migration != "db" || d.typespec != "api" || d.entry != "./spec" || d.openapi != "api" {
		t.Fatalf("detect: %+v", d)
	}
	if _, err := config.Parse([]byte(template(d)), dir); err != nil {
		t.Fatal(err)
	}
}
