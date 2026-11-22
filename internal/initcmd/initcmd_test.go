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

func TestInspectDrizzleConfig(t *testing.T) {
	for _, tc := range []struct{ name, src, dialect, env string }{
		{"postgres", `export default defineConfig({ dialect: "postgresql", dbCredentials: { url: process.env.DATABASE_URL! } })`, "postgresql", "DATABASE_URL"},
		{"sqlite, single quotes", `defineConfig({ dialect: 'sqlite', dbCredentials: { url: process.env.DB_FILE_NAME! } })`, "sqlite", "DB_FILE_NAME"},
		{"turso falls back", `defineConfig({ dialect: "turso", dbCredentials: { url: process.env.TURSO_URL } })`, "postgresql", "TURSO_URL"},
		{"mysql falls back", `defineConfig({ dialect: "mysql" })`, "postgresql", "DATABASE_URL"},
		{"no match", `export default {}`, "postgresql", "DATABASE_URL"},
		{"NODE_ENV is skipped", `const dev = process.env.NODE_ENV; defineConfig({ dialect: "sqlite", dbCredentials: { url: process.env.DB } })`, "sqlite", "DB"},
	} {
		d, e := inspectDrizzleConfig(tc.src)
		if d != tc.dialect || e != tc.env {
			t.Errorf("%s: got (%s, %s), want (%s, %s)", tc.name, d, e, tc.dialect, tc.env)
		}
	}
}

func TestInitSQLite(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "db"), 0o755)
	os.WriteFile(filepath.Join(dir, "db", "drizzle.config.ts"), []byte(`export default defineConfig({ dialect: "sqlite", dbCredentials: { url: process.env.DB_FILE_NAME! } })`), 0o644)
	var out bytes.Buffer
	if err := Run(Options{Dir: dir, Out: &out}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(dir, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	m := cfg.Migration
	if m.Dialect != "sqlite" || m.DatabaseURLEnv != "DB_FILE_NAME" || m.Docs.Dialect != "sqlite" {
		t.Fatalf("migration: %+v docs: %+v", m, m.Docs)
	}
	// With no drizzle.config.ts the Postgres defaults apply.
	dir2 := t.TempDir()
	if err := Run(Options{Dir: dir2, Out: &out}); err != nil {
		t.Fatal(err)
	}
	cfg2, err := config.Load(filepath.Join(dir2, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Migration.Dialect != "postgresql" || cfg2.Migration.DatabaseURLEnv != "DATABASE_URL" || cfg2.Migration.Docs.Dialect != "postgresql" {
		t.Fatalf("postgres default broken: %+v", cfg2.Migration)
	}
}
