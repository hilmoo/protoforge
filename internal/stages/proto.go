package stages

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/hilmoo/protoforge/internal/assets"
	"github.com/hilmoo/protoforge/internal/config"
	"github.com/hilmoo/protoforge/internal/flatten"
	"github.com/hilmoo/protoforge/internal/runner"
	"github.com/hilmoo/protoforge/internal/textops"
)

// Proto runs openapi-generator (protobuf-schema) twice and normalises the output.
type Proto struct {
	ctx *Ctx
	c   *config.Proto
}

func (p *Proto) Name() string { return "proto" }

func (p *Proto) dir() string { return p.ctx.Cfg.Abs("", p.c.Dir) }

func (p *Proto) registry() string { return p.ctx.Cfg.Abs(p.c.Dir, p.c.Registry) }

func (p *Proto) Steps() []Step {
	codegen := p.c.Codegen != nil && p.c.Codegen.Enabled
	return []Step{
		{Name: "models", Enabled: true, Run: func() error { return p.pass("models") }},
		{Name: "services", Enabled: true, Run: func() error { return p.pass("services") }},
		{Name: "flatten", Enabled: p.c.FlattenRequests, Note: "proto.flatten_requests is false", Run: p.flatten},
		{Name: "extra_replace", Enabled: len(p.c.ExtraReplace) > 0, Run: p.extraReplace},
		{Name: "cleanup", Enabled: true, Run: func() error { return p.ctx.removeAll(p.Name(), filepath.Join(p.dir(), ".tmp")) }},
		{Name: "codegen", Enabled: codegen, Note: "proto.codegen.enabled is false", Run: p.codegen},
		{Name: "run_after", Enabled: len(p.c.RunAfter) > 0, Run: func() error { return p.ctx.runCommands(p.Name(), p.dir(), p.c.RunAfter) }},
	}
}

func (p *Proto) norm() NormOptions {
	return NormOptions{Version: p.c.ProtoPackage, GoPackageRoot: p.c.GoPackageRoot, AnyType: p.c.AnyType, DropImports: p.c.DropImports}
}

// pass is one generator run: kind is "models" (--global-property models) or
// "services" (--global-property apis).
func (p *Proto) pass(kind string) error {
	dir, reg := p.dir(), p.registry()
	outDir := filepath.Join(reg, kind, p.c.ProtoPackage)
	if err := p.ctx.removeAll(p.Name(), outDir); err != nil {
		return err
	}

	templates := ""
	if p.c.Templates != "" {
		templates = rel(dir, p.ctx.Cfg.Abs(p.c.Dir, p.c.Templates))
	} else if p.ctx.R.DryRun {
		templates = "<embedded templates>"
	} else {
		tmp, err := os.MkdirTemp("", "protoforge-templates-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		if err := assets.WriteTemplates(tmp, p.c.ProtoPackage); err != nil {
			return err
		}
		templates = tmp
	}

	argv, err := p.ctx.tool(runner.ToolSpec{Name: "openapi-generator-cli", Bin: "openapi-generator-cli", Override: p.ctx.Cfg.Tools.OpenAPIGenerator}, dir)
	if err != nil {
		return err
	}
	gp := "models"
	if kind == "services" {
		gp = "apis"
	}
	argv = append(argv, "generate",
		"-i", rel(dir, OpenAPIOutput(p.ctx.Cfg)),
		"-t", templates,
		"--global-property", gp,
		"-g", "protobuf-schema",
		"-o", rel(dir, reg))
	if kind == "models" {
		// Like the original pipeline, only the models pass gets the type/import mappings.
		if t, i := generatorMappings(p.c.WellKnown); t != "" {
			argv = append(argv, "--type-mappings", t)
			if i != "" {
				argv = append(argv, "--import-mappings", i)
			}
		}
	}
	if err := p.ctx.run(p.Name(), dir, argv); err != nil {
		return err
	}

	// mkdir -p registry/<kind>/<ver>; mv registry/<kind>/*.proto registry/<kind>/<ver>/
	if !p.ctx.R.FileOp(p.Name(), "move %s/*.proto -> %s/, then normalise", p.ctx.Cfg.Rel(filepath.Join(reg, kind)), p.ctx.Cfg.Rel(outDir)) {
		return nil
	}
	files, err := filepath.Glob(filepath.Join(reg, kind, "*.proto"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("openapi-generator produced no %s/*.proto files", p.ctx.Cfg.Rel(filepath.Join(reg, kind)))
	}
	sort.Strings(files)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	o := p.norm()
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		s := string(b)
		if kind == "models" {
			s = NormalizeModel(s, o)
		} else {
			s = NormalizeService(s, o)
		}
		if err := textops.WriteFile(filepath.Join(outDir, filepath.Base(f)), []byte(s)); err != nil {
			return err
		}
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	p.ctx.R.Logf(p.Name(), "%d %s file(s) in %s", len(files), kind, p.ctx.Cfg.Rel(outDir))
	return nil
}

func (p *Proto) flatten() error {
	if !p.ctx.R.FileOp(p.Name(), "flatten *Request messages in %s", p.ctx.Cfg.Rel(filepath.Join(p.registry(), "services", p.c.ProtoPackage))) {
		return nil
	}
	return flatten.Run(flatten.Options{
		Registry:  p.registry(),
		Version:   p.c.ProtoPackage,
		WellKnown: flattenWKT(p.c.WellKnown, p.c.AnyType),
	})
}

func (p *Proto) extraReplace() error {
	rules, err := textops.Compile(p.c.ExtraReplace)
	if err != nil {
		return err
	}
	reg := p.registry()
	for _, kind := range []string{"models", "services"} {
		d := filepath.Join(reg, kind, p.c.ProtoPackage)
		if !p.ctx.R.FileOp(p.Name(), "apply extra_replace to %s", p.ctx.Cfg.Rel(d)) {
			continue
		}
		changed, err := textops.ApplyDir(d, rules)
		if err != nil {
			return err
		}
		for _, c := range changed {
			p.ctx.R.Debugf(p.Name(), "extra_replace rewrote %s/%s", kind, c)
		}
	}
	return nil
}

func (p *Proto) codegen() error {
	return p.ctx.runCommands(p.Name(), p.dir(), p.c.Codegen.Commands)
}
