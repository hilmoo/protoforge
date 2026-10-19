package stages

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/hilmoo/protoforge/internal/assets"
	"github.com/hilmoo/protoforge/internal/config"
	"github.com/hilmoo/protoforge/internal/runner"
)

// TypeSpec runs the embedded Drizzle -> TypeSpec script and `tsp compile`.
type TypeSpec struct {
	ctx *Ctx
	c   *config.TypeSpec
}

func (t *TypeSpec) Name() string { return "typespec" }

func (t *TypeSpec) dir() string { return t.ctx.Cfg.Abs("", t.c.Dir) }

func (t *TypeSpec) Steps() []Step {
	return []Step{
		{Name: "from_drizzle", Enabled: t.c.FromDrizzle != nil, Note: "typespec.from_drizzle not set", Run: t.fromDrizzle},
		{Name: "compile", Enabled: true, Run: t.compile},
		{Name: "run_after", Enabled: len(t.c.RunAfter) > 0, Run: func() error { return t.ctx.runCommands(t.Name(), t.dir(), t.c.RunAfter) }},
	}
}

// OpenAPIOutput is where the OpenAPI document compiled from s goes.
func OpenAPIOutput(cfg *config.Config, s *config.Spec) string {
	if cfg.OpenAPI != nil {
		return cfg.Abs(cfg.OpenAPI.Dir, s.Output)
	}
	return cfg.Abs(cfg.TypeSpec.Dir, s.Output)
}

// compile runs `tsp compile <entry>` once per selected spec.
func (t *TypeSpec) compile() error {
	dir := t.dir()
	tsp, err := t.ctx.tool(runner.ToolSpec{Name: "tsp", Bin: "tsp", Override: t.ctx.Cfg.Tools.TSP, Node: true}, dir)
	if err != nil {
		return err
	}
	var conf []string
	if !exists(filepath.Join(dir, "tspconfig.yaml")) {
		tmp, err := os.MkdirTemp("", "protoforge-tsp-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		p := filepath.Join(tmp, "tspconfig.yaml")
		if err := os.WriteFile(p, []byte(assets.TSPConfig), 0o644); err != nil {
			return err
		}
		t.ctx.R.Debugf(t.Name(), "tspconfig.yaml not found; using the built-in default")
		conf = []string{"--config", p}
	}
	return t.ctx.eachSpec(func(s spec) error {
		argv := append(append(append([]string{}, tsp...), "compile", s.Entry), conf...)
		return t.compileSpec(dir, argv, s)
	})
}

func (t *TypeSpec) compileSpec(dir string, argv []string, s spec) error {
	if err := t.ctx.run(t.Name(), dir, argv); err != nil {
		return err
	}

	// Replaces `cp ./@typespec/openapi3/openapi.json ./openapi.json; rm -r @typespec`.
	emitted := filepath.Join(dir, "@typespec", "openapi3", "openapi.json")
	out := OpenAPIOutput(t.ctx.Cfg, s.Spec)
	if t.ctx.R.FileOp(t.Name(), "move %s -> %s", t.ctx.Cfg.Rel(emitted), t.ctx.Cfg.Rel(out)) {
		if !exists(emitted) {
			// One document per @service namespace: openapi.<Namespace>.json.
			if many, _ := filepath.Glob(filepath.Join(dir, "@typespec", "openapi3", "openapi.*.json")); len(many) > 0 {
				for i, m := range many {
					many[i] = filepath.Base(m)
				}
				_ = t.ctx.removeAll(t.Name(), filepath.Join(dir, "@typespec"))
				return fmt.Errorf("%s declares more than one @service (tsp emitted %s); give each service its own entry in specs", s.Entry, strings.Join(many, ", "))
			}
			return fmt.Errorf("tsp compile did not produce %s (check output-dir in tspconfig.yaml)", t.ctx.Cfg.Rel(emitted))
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := moveFile(emitted, out); err != nil {
			return err
		}
	}
	return t.ctx.removeAll(t.Name(), filepath.Join(dir, "@typespec"))
}

type drizzleParams struct {
	SchemaDir       string   `json:"schemaDir"`
	OutFile         string   `json:"outFile"`
	Namespace       string   `json:"namespace"`
	ReadOnlyColumns []string `json:"readOnlyColumns"`
	RequireDocs     string   `json:"requireDocs"`
	Header          []string `json:"header"`
}

func (t *TypeSpec) fromDrizzle() error {
	f := t.c.FromDrizzle
	schemaDir := t.ctx.Cfg.Abs(t.c.Dir, f.SchemaDir)
	outFile := t.ctx.Cfg.Abs(t.c.Dir, f.Out)

	project, searched := findPackage(schemaDir, "drizzle-orm")
	if project == "" {
		if t.ctx.R.DryRun {
			project = schemaDir
		} else {
			return fmt.Errorf("cannot resolve drizzle-orm: no node_modules/drizzle-orm found from %s upwards (searched: %s); install it in the schema's project",
				t.ctx.Cfg.Rel(schemaDir), strings.Join(searched, ", "))
		}
	}
	cacheDir := filepath.Join(project, "node_modules", ".cache", "protoforge")
	script := filepath.Join(cacheDir, "drizzle-to-tsp."+assets.DrizzleScriptHash()+".mts")

	params, err := json.Marshal(drizzleParams{
		SchemaDir:       schemaDir,
		OutFile:         outFile,
		Namespace:       f.Namespace,
		ReadOnlyColumns: f.ReadOnlyColumns,
		RequireDocs:     f.RequireDocs,
		Header: []string{
			fmt.Sprintf("// AUTO-GENERATED from %s by protoforge (embedded drizzle-to-typespec).", t.ctx.Cfg.Rel(schemaDir)),
			"// DO NOT EDIT. Run `protoforge stage typespec --only from_drizzle` to regenerate.",
		},
	})
	if err != nil {
		return err
	}

	runnerArgv, name, err := t.tsRunner(project)
	if err != nil {
		return err
	}
	if t.ctx.R.FileOp(t.Name(), "write embedded script %s", t.ctx.Cfg.Rel(script)) {
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(script, []byte(assets.DrizzleToTSP), 0o644); err != nil {
			return err
		}
		if t.ctx.R.Verbose {
			t.ctx.R.Logf(t.Name(), "keeping %s (--verbose)", t.ctx.Cfg.Rel(script))
		} else {
			defer os.Remove(script)
		}
	}
	t.ctx.R.Logf(t.Name(), "drizzle-to-typespec runner: %s", name)
	return t.ctx.run(t.Name(), project, append(runnerArgv, script, string(params)))
}

// tsRunner picks tsx, bun or `node --experimental-strip-types` (Node >= 22.6).
func (t *TypeSpec) tsRunner(project string) ([]string, string, error) {
	if o := t.ctx.Cfg.Tools.TSRunner; o != "" {
		argv, err := runner.SplitArgs(o)
		if err != nil || len(argv) == 0 {
			return nil, "", fmt.Errorf("tools.ts_runner: invalid command %q", o)
		}
		return argv, o + " (tools.ts_runner)", nil
	}
	r := t.ctx.R
	if tt, err := r.Resolve(runner.ToolSpec{Name: "tsx", Bin: "tsx", Node: true}, project, ""); err == nil {
		return tt.Argv, "tsx (" + tt.Source + ")", nil
	}
	if p, err := runner.LookPath("bun", project, r.Env); err == nil {
		return []string{p}, "bun", nil
	}
	if p, err := runner.LookPath("node", project, r.Env); err == nil {
		v, _ := r.Output([]string{p, "--version"}, project)
		if NodeAtLeast(v, 22, 6) {
			return []string{p, "--experimental-strip-types", "--no-warnings"}, "node --experimental-strip-types (" + v + ")", nil
		}
		return nil, "", fmt.Errorf("node %s is too old to run TypeScript (need >= 22.6), and neither tsx nor bun was found", v)
	}
	if r.DryRun {
		return []string{"tsx"}, "tsx (unresolved)", nil
	}
	return nil, "", fmt.Errorf("no TypeScript runner found: install tsx in %s, or bun, or node >= 22.6", t.ctx.Cfg.Rel(project))
}

var nodeVersionRe = regexp.MustCompile(`v?(\d+)\.(\d+)`)

// NodeAtLeast parses `node --version` output.
func NodeAtLeast(v string, major, minor int) bool {
	m := nodeVersionRe.FindStringSubmatch(v)
	if m == nil {
		return false
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	return a > major || (a == major && b >= minor)
}

// findPackage walks up from dir looking for node_modules/<pkg>/package.json.
func findPackage(dir, pkg string) (string, []string) {
	var searched []string
	d := dir
	for {
		searched = append(searched, filepath.ToSlash(filepath.Join(d, "node_modules")))
		if exists(filepath.Join(d, "node_modules", filepath.FromSlash(pkg), "package.json")) {
			return d, searched
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", searched
		}
		d = parent
	}
}
