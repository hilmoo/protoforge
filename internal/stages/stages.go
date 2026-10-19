// Package stages implements the pipeline stages. Each stage exposes named,
// individually runnable sub-steps, which is what makes `--only` and `--dry-run` work.
package stages

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hilmoo/protoforge/internal/config"
	"github.com/hilmoo/protoforge/internal/runner"
)

// Ctx is shared by every stage in one invocation.
type Ctx struct {
	Cfg        *config.Config
	R          *runner.Runner
	NodeRunner string // resolved tools.node_runner
	// ContinueOnError keeps running later steps after a failure.
	ContinueOnError bool
	// Specs limits the per-spec work to these specs (--spec); empty means every spec.
	Specs []string
}

// spec is one named entry of config.Specs.
type spec struct {
	Name string
	*config.Spec
}

// allSpecs returns every configured spec in name order.
func (c *Ctx) allSpecs() []spec {
	var out []spec
	for _, n := range c.Cfg.SpecNames() {
		out = append(out, spec{n, c.Cfg.Specs[n]})
	}
	return out
}

// specs returns the specs selected with --spec (all of them by default), in name order.
func (c *Ctx) specs() []spec {
	if len(c.Specs) == 0 {
		return c.allSpecs()
	}
	var out []spec
	for _, s := range c.allSpecs() {
		if contains(c.Specs, s.Name) {
			out = append(out, s)
		}
	}
	return out
}

// eachSpec runs fn for every selected spec, prefixing errors with the spec name.
func (c *Ctx) eachSpec(fn func(s spec) error) error {
	for _, s := range c.specs() {
		if err := fn(s); err != nil {
			return fmt.Errorf("spec %s: %w", s.Name, err)
		}
	}
	return nil
}

// Step is one named sub-step of a stage.
type Step struct {
	Name    string
	Enabled bool   // run by default (`run` / `stage` without --only)
	Note    string // shown in dry-run when the step is disabled
	Run     func() error
}

// Stage is a pipeline stage.
type Stage interface {
	Name() string
	Steps() []Step
}

// New returns the stage with the given name, or an error if it is not configured.
func New(ctx *Ctx, name string) (Stage, error) {
	if !ctx.Cfg.StageEnabled(name) {
		return nil, fmt.Errorf("stage %q is not configured in %s", name, config.FileName)
	}
	switch name {
	case "migration":
		return &Migration{ctx: ctx, c: ctx.Cfg.Migration}, nil
	case "typespec":
		return &TypeSpec{ctx: ctx, c: ctx.Cfg.TypeSpec}, nil
	case "openapi":
		return &OpenAPI{ctx: ctx, c: ctx.Cfg.OpenAPI}, nil
	case "proto":
		return &Proto{ctx: ctx, c: ctx.Cfg.Proto}, nil
	}
	return nil, fmt.Errorf("unknown stage %q", name)
}

// RunStage runs the enabled steps of s, or exactly the steps named in only.
func RunStage(ctx *Ctx, s Stage, only []string) error {
	steps := s.Steps()
	if len(only) > 0 {
		known := map[string]bool{}
		for _, st := range steps {
			known[st.Name] = true
		}
		for _, o := range only {
			if !known[o] {
				names := make([]string, len(steps))
				for i, st := range steps {
					names[i] = st.Name
				}
				return fmt.Errorf("stage %s has no step %q (steps: %s)", s.Name(), o, strings.Join(names, ", "))
			}
		}
	}
	var errs []error
	for _, st := range steps {
		run := st.Enabled
		if len(only) > 0 {
			run = contains(only, st.Name)
		}
		if !run {
			if ctx.R.Verbose {
				ctx.R.Logf(s.Name(), "skip %s%s", st.Name, note(st.Note))
			}
			continue
		}
		if ctx.R.DryRun {
			fmt.Fprintf(ctx.R.Stdout, "[%s] step %s\n", s.Name(), st.Name)
		} else {
			ctx.R.Logf(s.Name(), "▶ %s", st.Name)
		}
		if err := st.Run(); err != nil {
			err = fmt.Errorf("%s/%s: %w", s.Name(), st.Name, err)
			if !ctx.ContinueOnError {
				return err
			}
			ctx.R.Logf(s.Name(), "error (continuing): %v", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func note(n string) string {
	if n == "" {
		return ""
	}
	return " (" + n + ")"
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// tool resolves an external tool. In dry-run mode an unresolved tool is shown by
// its bare name instead of failing, so the plan is still printed.
func (c *Ctx) tool(spec runner.ToolSpec, dir string) ([]string, error) {
	t, err := c.R.Resolve(spec, dir, c.NodeRunner)
	if err != nil {
		if c.R.DryRun {
			return []string{spec.Bin}, nil
		}
		return nil, err
	}
	c.R.Debugf("", "using %s from %s: %s", spec.Name, t.Source, runner.QuoteArgs(t.Argv))
	return t.Argv, nil
}

func (c *Ctx) run(prefix, dir string, argv []string) error {
	return c.R.Run(runner.Cmd{Argv: argv, Dir: dir, Prefix: prefix})
}

// runCommands runs run_after / codegen commands.
func (c *Ctx) runCommands(prefix, dir string, cmds []config.Command) error {
	for _, cmd := range cmds {
		rc := runner.Cmd{Dir: dir, Prefix: prefix, Shell: cmd.Shell}
		if cmd.Shell {
			rc.Argv = []string{cmd.Cmd}
		} else {
			argv, err := runner.SplitArgs(cmd.Cmd)
			if err != nil {
				return err
			}
			rc.Argv = argv
		}
		if err := c.R.Run(rc); err != nil {
			return err
		}
	}
	return nil
}

// rel renders p relative to base for command lines (forward slashes are fine on
// Windows too, but keep native separators for the child process).
func rel(base, p string) string {
	r, err := filepath.Rel(base, p)
	if err != nil {
		return p
	}
	if !strings.HasPrefix(r, ".") && !filepath.IsAbs(r) {
		return "." + string(filepath.Separator) + r
	}
	return r
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// removeAll is `rm -r path || true`, reported in dry-run.
func (c *Ctx) removeAll(prefix, p string) error {
	if !exists(p) {
		return nil
	}
	if !c.R.FileOp(prefix, "remove %s", c.Cfg.Rel(p)) {
		return nil
	}
	return os.RemoveAll(p)
}

// moveFile renames, falling back to copy+delete across devices.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return err
	}
	return os.Remove(src)
}
