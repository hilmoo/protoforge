// Package initcmd scaffolds protoforge.yaml (and tspconfig.yaml / oaf-config.yaml)
// in a repository. It never writes scripts and never overwrites without --force.
package initcmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hilmoo/protoforge/internal/assets"
	"github.com/hilmoo/protoforge/internal/config"
)

// Options for Run.
type Options struct {
	Dir   string
	Force bool
	Out   io.Writer
}

type detected struct {
	migration, drizzleConfig, typespec, entry, openapi, output, proto string
}

var skipDirs = map[string]bool{"node_modules": true, ".git": true, "dist": true, "target": true, "vendor": true, ".devbox": true, "gen": true}

// detect searches (up to 3 levels deep) for drizzle.config.ts, a .tsp entry and an openapi.json.
func detect(root string) detected {
	d := detected{migration: "migration", drizzleConfig: "drizzle.config.ts", typespec: "openapi", entry: "./src", openapi: "openapi", output: "openapi.json", proto: "proto"}
	var drizzle, tspMain, oapi []string
	_ = filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if e.IsDir() {
			if p != root && (skipDirs[e.Name()] || strings.HasPrefix(e.Name(), ".")) || strings.Count(rel, string(filepath.Separator)) >= 3 {
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case e.Name() == "drizzle.config.ts" || e.Name() == "drizzle.config.js" || e.Name() == "drizzle.config.mjs":
			drizzle = append(drizzle, rel)
		case e.Name() == "main.tsp":
			tspMain = append(tspMain, rel)
		case e.Name() == "openapi.json":
			oapi = append(oapi, rel)
		}
		return nil
	})
	sort.Strings(drizzle)
	sort.Strings(tspMain)
	sort.Strings(oapi)
	if len(drizzle) > 0 {
		d.migration = slash(filepath.Dir(drizzle[0]))
		d.drizzleConfig = filepath.Base(drizzle[0])
	}
	if len(tspMain) > 0 {
		// main.tsp in <dir>/src -> dir=<dir>, entry=./src
		mainDir := filepath.Dir(tspMain[0])
		d.typespec = slash(filepath.Dir(mainDir))
		d.entry = "./" + filepath.Base(mainDir)
		if d.typespec == "." {
			d.typespec, d.entry = slash(mainDir), "."
		}
		d.openapi = d.typespec
	}
	if len(oapi) > 0 {
		d.openapi = slash(filepath.Dir(oapi[0]))
		d.output = filepath.Base(oapi[0])
	}
	return d
}

func slash(p string) string { return filepath.ToSlash(p) }

// Template renders the default protoforge.yaml for detected directories.
func template(d detected) string {
	schemaFromTS := slash(relTo(d.typespec, filepath.Join(d.migration, "src/schema")))
	return fmt.Sprintf(`# yaml-language-server: $schema=%s
# protoforge.yaml — see docs/config.md for every option and its default.
version: 1

env_file: .env                     # loaded into child processes and used for DATABASE_URL

# tools:                           # all optional; auto-detected
#   node_runner: pnpm exec         # pnpm exec | npx | bunx, picked from the lockfile
#   openapi_generator: openapi-generator-cli

migration:
  dir: %s
  drizzle_config: %s
  generate: true                   # drizzle-kit generate
  push: false                      # drizzle-kit push
  migrate: false                   # drizzle-kit migrate
  docs:                            # drizzle-docs generate; remove to skip
    schema_dir: ./src/schema
    dialect: postgresql
    format: dbml
    out: schema.dbml
  # post_push_sql: []              # raw SQL run in ONE transaction after push
  # sync:                          # copy drizzle/*/migration.sql into another tree
  #   to: ../server/migrations
  #   suffix: .up.sql

typespec:
  dir: %s
  from_drizzle:                   # generate TypeSpec models from the Drizzle schema
    schema_dir: %s
    out: src/_generated/db.tsp
    namespace: DbSchema
    read_only_columns: [id, created_at, updated_at]
    require_docs: warn             # warn | error | off

openapi:
  dir: %s
  format: true                    # openapi-format (oaf-config.yaml, or the built-in default)
  go_tags: false                   # inject x-oapi-codegen-extra-tags (validate/jsonschema)

proto:
  dir: %s
  registry: registry
  proto_package: v1
  go_package_root: proto
  drop_imports: []
  flatten_requests: true
  # codegen:
  #   enabled: false
  #   commands: [ "buf generate" ]

# One entry per service. Each compiles its own TypeSpec entry into its own
# OpenAPI document and registry/services/<name>/<v>; all share the models package.
specs:
  main:
    entry: %s                      # relative to typespec.dir
    output: %s                     # relative to openapi.dir

# pipelines:                       # defaults: api = typespec openapi proto, sql = migration, all = sql api
#   api: [ typespec, openapi, proto ]
`, config.SchemaID, d.migration, d.drizzleConfig, d.typespec, schemaFromTS, d.openapi, d.proto, d.entry, d.output)
}

func relTo(base, p string) string {
	r, err := filepath.Rel(filepath.FromSlash(base), filepath.FromSlash(p))
	if err != nil {
		return p
	}
	return r
}

// Run writes the files.
func Run(o Options) error {
	root, err := filepath.Abs(o.Dir)
	if err != nil {
		return err
	}
	d := detect(root)
	cfgText := template(d)
	if _, err := config.Parse([]byte(cfgText), root); err != nil {
		return fmt.Errorf("internal error: generated config is invalid: %w", err)
	}
	files := []struct{ path, body string }{
		{filepath.Join(root, config.FileName), cfgText},
		{filepath.Join(root, filepath.FromSlash(d.typespec), "tspconfig.yaml"), assets.TSPConfig},
		{filepath.Join(root, filepath.FromSlash(d.openapi), "oaf-config.yaml"), assets.OAFConfig},
	}
	for _, f := range files {
		rel, _ := filepath.Rel(root, f.path)
		if _, err := os.Stat(f.path); err == nil && !o.Force {
			fmt.Fprintf(o.Out, "  skip   %s (exists; use --force to overwrite)\n", slash(rel))
			continue
		}
		if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(f.path, []byte(f.body), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(o.Out, "  write  %s\n", slash(rel))
	}
	if err := gitignore(root, d, o.Out); err != nil {
		return err
	}
	fmt.Fprintf(o.Out, "\nInstall the npm tools protoforge wraps (in the package that owns them):\n\n")
	fmt.Fprintf(o.Out, "  pnpm add -D drizzle-kit drizzle-docs-generator tsx @typespec/compiler @typespec/http @typespec/openapi @typespec/openapi3 @typespec/rest openapi-format\n")
	fmt.Fprintf(o.Out, "  pnpm add drizzle-orm\n\n")
	fmt.Fprintf(o.Out, "and openapi-generator-cli (Java) for the proto stage. Then run `protoforge doctor`.\n")
	return nil
}

func gitignore(root string, d detected, w io.Writer) error {
	entries := []string{
		"# protoforge",
		filepath.ToSlash(filepath.Join(d.typespec, "@typespec")) + "/",
		filepath.ToSlash(filepath.Join(d.proto, ".tmp")) + "/",
		"node_modules/.cache/protoforge/",
	}
	p := filepath.Join(root, ".gitignore")
	old, _ := os.ReadFile(p)
	have := map[string]bool{}
	for _, l := range strings.Split(string(old), "\n") {
		have[strings.TrimSpace(l)] = true
	}
	var add []string
	for _, e := range entries {
		if !have[e] {
			add = append(add, e)
		}
	}
	if len(add) == 0 || (len(add) == 1 && add[0] == "# protoforge") {
		return nil
	}
	s := string(old)
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	if s != "" {
		s += "\n"
	}
	s += strings.Join(add, "\n") + "\n"
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(w, "  update .gitignore (+%d entries)\n", len(add))
	return nil
}
