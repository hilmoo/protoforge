// Package doctor checks that every external tool needed by the enabled stages
// is installed, and reports where it was found and its version.
package doctor

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/hilmoo/protoforge/internal/config"
	"github.com/hilmoo/protoforge/internal/dbexec"
	"github.com/hilmoo/protoforge/internal/runner"
	"github.com/hilmoo/protoforge/internal/stages"
)

type check struct {
	spec     runner.ToolSpec
	dir      string
	why      string
	required bool
	version  []string // argv suffix that prints the version
}

// Run prints a report and returns an error when a required tool is missing.
func Run(cfg *config.Config, r *runner.Runner, nodeRunner string, w io.Writer) error {
	var checks []check
	t := cfg.Tools
	add := func(c check) { checks = append(checks, c) }

	fmt.Fprintf(w, "config:      %s\n", cfg.Path)
	fmt.Fprintf(w, "node runner: %s\n\n", nodeRunner)

	if m := cfg.Migration; m != nil {
		dir := cfg.Abs("", m.Dir)
		if m.Generate || m.Push || m.Migrate {
			add(check{runner.ToolSpec{Name: "drizzle-kit", Bin: "drizzle-kit", Override: t.DrizzleKit, Node: true}, dir, "migration.generate/push/migrate", true, []string{"--version"}})
		}
		if m.Docs != nil {
			add(check{runner.ToolSpec{Name: "drizzle-docs", Bin: "drizzle-docs", Override: t.DrizzleDocs, Node: true}, dir, "migration.docs", true, []string{"--version"}})
		}
	}
	if ts := cfg.TypeSpec; ts != nil {
		dir := cfg.Abs("", ts.Dir)
		add(check{runner.ToolSpec{Name: "tsp", Bin: "tsp", Override: t.TSP, Node: true}, dir, "typespec.compile", true, []string{"--version"}})
		if ts.FromDrizzle != nil {
			add(check{runner.ToolSpec{Name: "node", Bin: "node"}, dir, "typespec.from_drizzle", true, []string{"--version"}})
		}
	}
	if o := cfg.OpenAPI; o != nil && o.Format {
		add(check{runner.ToolSpec{Name: "openapi-format", Bin: "openapi-format", Override: t.OpenAPIFormat, Node: true}, cfg.Abs("", o.Dir), "openapi.format", true, []string{"--version"}})
	}
	if p := cfg.Proto; p != nil {
		dir := cfg.Abs("", p.Dir)
		add(check{runner.ToolSpec{Name: "openapi-generator-cli", Bin: "openapi-generator-cli", Override: t.OpenAPIGenerator}, dir, "proto", true, []string{"version"}})
		if p.Codegen != nil && p.Codegen.Enabled {
			for _, c := range p.Codegen.Commands {
				if c.Shell {
					continue
				}
				argv, err := runner.SplitArgs(c.Cmd)
				if err == nil && len(argv) > 0 {
					add(check{runner.ToolSpec{Name: argv[0], Bin: argv[0]}, dir, "proto.codegen", true, []string{"--version"}})
				}
			}
		}
	}

	var missing []string
	for _, c := range checks {
		tool, err := r.Resolve(c.spec, c.dir, nodeRunner)
		if err != nil {
			status := "MISSING"
			if !c.required {
				status = "missing (optional)"
			} else {
				missing = append(missing, c.spec.Name)
			}
			fmt.Fprintf(w, "  ✗ %-22s %s — needed by %s\n", c.spec.Name, status, c.why)
			continue
		}
		version := versionOf(r, append(append([]string{}, tool.Argv...), c.version...), c.dir)
		extra := ""
		if c.spec.Name == "node" && !stages.NodeAtLeast(version, 18, 0) {
			extra = "  (too old: need >= 18; >= 22.6 if neither tsx nor bun is installed)"
			missing = append(missing, "node (too old)")
		}
		fmt.Fprintf(w, "  ✓ %-22s %s  [%s] %s%s\n", c.spec.Name, version, tool.Source, runner.QuoteArgs(tool.Argv), extra)
	}

	if m := cfg.Migration; m != nil && (m.Push || m.Migrate || len(m.PostPushSQL) > 0) {
		msg, ok := databaseStatus(m, runner.Getenv(r.Env, m.DatabaseURLEnv), cfg.Abs("", m.Dir))
		mark := "✓"
		if !ok {
			mark = "✗"
			missing = append(missing, m.DatabaseURLEnv)
		}
		fmt.Fprintf(w, "  %s %-22s %s\n", mark, m.DatabaseURLEnv, msg)
	}
	fmt.Fprintln(w)
	if len(missing) > 0 {
		return fmt.Errorf("missing requirements: %s", strings.Join(missing, ", "))
	}
	fmt.Fprintln(w, "all requirements satisfied")
	return nil
}

// databaseStatus describes the database location env var for the report. For
// sqlite the value is parsed exactly like post_push_sql does; remote libSQL URLs
// are fine for drizzle-kit itself, so they only fail when post_push_sql needs them.
func databaseStatus(m *config.Migration, val, dir string) (msg string, ok bool) {
	where := "needed by migration.push/migrate/post_push_sql"
	if val == "" {
		return "not set — " + where, false
	}
	if m.Dialect != "sqlite" {
		return "set", true
	}
	path, err := dbexec.SQLitePath(val, dir)
	if err != nil {
		if len(m.PostPushSQL) == 0 {
			return "set (not a local file: used by drizzle-kit only)", true
		}
		return fmt.Sprintf("%v (migration.post_push_sql needs a local file)", err), false
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Sprintf("sqlite file %s (exists)", path), true
	}
	if m.Push {
		return fmt.Sprintf("sqlite file %s (will be created by drizzle-kit push)", path), true
	}
	return fmt.Sprintf("sqlite file %s (does not exist yet)", path), true
}

func versionOf(r *runner.Runner, argv []string, dir string) string {
	type res struct {
		out string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		out, err := r.Output(argv, dir)
		ch <- res{out, err}
	}()
	select {
	case v := <-ch:
		if v.err != nil && v.out == "" {
			return "(version unknown)"
		}
		var lines []string
		for _, l := range strings.Split(v.out, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				lines = append(lines, l)
			}
		}
		switch {
		case len(lines) == 0:
			return "(version unknown)"
		case len(lines) <= 3:
			return strings.Join(lines, "; ")
		default:
			// Banners first; the version is usually the last line.
			return lines[len(lines)-1]
		}
	case <-time.After(60 * time.Second):
		return "(version check timed out)"
	}
}
