package stages

import (
	"os"
	"path/filepath"

	"github.com/hilmoo/protoforge/internal/assets"
	"github.com/hilmoo/protoforge/internal/config"
	"github.com/hilmoo/protoforge/internal/gotags"
	"github.com/hilmoo/protoforge/internal/runner"
)

// OpenAPI formats the compiled document and injects Go struct tags.
type OpenAPI struct {
	ctx *Ctx
	c   *config.OpenAPI
}

func (o *OpenAPI) Name() string { return "openapi" }

func (o *OpenAPI) dir() string { return o.ctx.Cfg.Abs("", o.c.Dir) }

func (o *OpenAPI) Steps() []Step {
	return []Step{
		{Name: "format", Enabled: o.c.Format, Note: "openapi.format is false", Run: o.format},
		{Name: "go_tags", Enabled: o.c.GoTags, Note: "openapi.go_tags is false", Run: o.goTags},
		{Name: "lint", Enabled: false, Note: "only via --only lint", Run: o.lint},
		{Name: "run_after", Enabled: len(o.c.RunAfter) > 0, Run: func() error { return o.ctx.runCommands(o.Name(), o.dir(), o.c.RunAfter) }},
	}
}

func (o *OpenAPI) format() error {
	dir := o.dir()
	argv, err := o.ctx.tool(runner.ToolSpec{Name: "openapi-format", Bin: "openapi-format", Override: o.ctx.Cfg.Tools.OpenAPIFormat, Node: true}, dir)
	if err != nil {
		return err
	}
	conf := "oaf-config.yaml"
	if !exists(filepath.Join(dir, conf)) {
		tmp, err := os.MkdirTemp("", "protoforge-oaf-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		conf = filepath.Join(tmp, "oaf-config.yaml")
		if err := os.WriteFile(conf, []byte(assets.OAFConfig), 0o644); err != nil {
			return err
		}
		o.ctx.R.Debugf(o.Name(), "oaf-config.yaml not found; using the built-in default")
	}
	out := rel(dir, OpenAPIOutput(o.ctx.Cfg))
	return o.ctx.run(o.Name(), dir, append(argv, "--configFile", conf, "-o", out, out))
}

func (o *OpenAPI) goTags() error {
	out := OpenAPIOutput(o.ctx.Cfg)
	if !o.ctx.R.FileOp(o.Name(), "inject x-oapi-codegen-extra-tags into %s", o.ctx.Cfg.Rel(out)) {
		return nil
	}
	w := &prefixWriter{r: o.ctx.R, prefix: o.Name()}
	return gotags.TransformFile(out, out, w)
}

func (o *OpenAPI) lint() error {
	dir := o.dir()
	argv, err := o.ctx.tool(runner.ToolSpec{Name: "spectral", Bin: "spectral", Override: o.ctx.Cfg.Tools.Spectral, Node: true}, dir)
	if err != nil {
		return err
	}
	argv = append(argv, "lint", rel(dir, OpenAPIOutput(o.ctx.Cfg)))
	if o.c.Lint != "" {
		argv = append(argv, "--ruleset", o.c.Lint)
	}
	return o.ctx.run(o.Name(), dir, argv)
}

// prefixWriter sends the gotags `[WARN]` lines to stderr with the stage prefix.
type prefixWriter struct {
	r      *runner.Runner
	prefix string
}

func (p *prefixWriter) Write(b []byte) (int, error) {
	s := string(b)
	for len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	p.r.Logf(p.prefix, "%s", s)
	return len(b), nil
}
