package stages

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hilmoo/protoforge/internal/assets"
	"github.com/hilmoo/protoforge/internal/config"
	"github.com/hilmoo/protoforge/internal/flatten"
	"github.com/hilmoo/protoforge/internal/runner"
	"github.com/hilmoo/protoforge/internal/textops"
)

// Proto runs openapi-generator (protobuf-schema) per spec and normalises the
// output: one shared models package, one services package per spec.
type Proto struct {
	ctx *Ctx
	c   *config.Proto
}

func (p *Proto) Name() string { return "proto" }

func (p *Proto) dir() string { return p.ctx.Cfg.Abs("", p.c.Dir) }

func (p *Proto) registry() string { return p.ctx.Cfg.Abs(p.c.Dir, p.c.Registry) }

func (p *Proto) modelsDir() string { return filepath.Join(p.registry(), "models", p.c.ProtoPackage) }

func (p *Proto) servicesDir(s spec) string {
	return filepath.Join(p.registry(), "services", s.Name, s.ProtoPackage)
}

func (p *Proto) Steps() []Step {
	codegen := p.c.Codegen != nil && p.c.Codegen.Enabled
	return []Step{
		{Name: "models", Enabled: true, Run: p.models},
		{Name: "services", Enabled: true, Run: p.services},
		{Name: "flatten", Enabled: p.c.FlattenRequests, Note: "proto.flatten_requests is false", Run: p.flatten},
		{Name: "extra_replace", Enabled: len(p.c.ExtraReplace) > 0, Run: p.extraReplace},
		{Name: "cleanup", Enabled: true, Run: func() error { return p.ctx.removeAll(p.Name(), filepath.Join(p.dir(), ".tmp")) }},
		{Name: "codegen", Enabled: codegen, Note: "proto.codegen.enabled is false", Run: p.codegen},
		{Name: "run_after", Enabled: len(p.c.RunAfter) > 0, Run: func() error { return p.ctx.runCommands(p.Name(), p.dir(), p.c.RunAfter) }},
	}
}

func (p *Proto) norm(s spec) NormOptions {
	return NormOptions{Version: p.c.ProtoPackage, Spec: s.Name, ServicesVersion: s.ProtoPackage, GoPackageRoot: p.c.GoPackageRoot, AnyType: p.c.AnyType, DropImports: p.c.DropImports}
}

// models generates the shared models package from EVERY spec (not only those
// selected with --spec), so building one spec never drops another's models.
// A model emitted by several specs must come out identical.
func (p *Proto) models() error {
	outDir := p.modelsDir()
	if err := p.ctx.removeAll(p.Name(), outDir); err != nil {
		return err
	}
	templates, done, err := p.templates()
	if err != nil {
		return err
	}
	defer done()
	owner := map[string]string{} // file name -> spec that wrote it
	for _, s := range p.ctx.allSpecs() {
		if doc := OpenAPIOutput(p.ctx.Cfg, s.Spec); !p.ctx.R.DryRun && !exists(doc) {
			return fmt.Errorf("spec %s: %s not found; the shared models need every spec's document, so build %s once (e.g. without --spec)", s.Name, p.ctx.Cfg.Rel(doc), s.Name)
		}
		err := p.pass(s, "models", templates, func(name, content string) error {
			dst := filepath.Join(outDir, name)
			if prev, ok := owner[name]; ok {
				b, err := os.ReadFile(dst)
				if err != nil {
					return err
				}
				if stripHeader(string(b)) != stripHeader(content) {
					return fmt.Errorf("%s differs between specs %s and %s; a model shared by several specs must be identical in each OpenAPI document", p.ctx.Cfg.Rel(dst), prev, s.Name)
				}
				return nil
			}
			owner[name] = s.Name
			return textops.WriteFile(dst, []byte(content))
		})
		if err != nil {
			return fmt.Errorf("spec %s: %w", s.Name, err)
		}
	}
	if len(owner) > 0 {
		p.ctx.R.Logf(p.Name(), "%d models file(s) in %s", len(owner), p.ctx.Cfg.Rel(outDir))
	}
	return nil
}

// stripHeader drops the generator's leading /* ... */ comment, which names the
// source document (title, version) and so differs between specs even for an
// identical model.
func stripHeader(s string) string {
	if strings.HasPrefix(s, "/*") {
		if _, rest, ok := strings.Cut(s, "*/"); ok {
			return strings.TrimLeft(rest, "\r\n")
		}
	}
	return s
}

// services generates registry/services/<name>/<v> for each selected spec. A
// full run clears registry/services first, so removed specs leave nothing behind.
func (p *Proto) services() error {
	if len(p.ctx.Specs) == 0 {
		if err := p.ctx.removeAll(p.Name(), filepath.Join(p.registry(), "services")); err != nil {
			return err
		}
	}
	templates, done, err := p.templates()
	if err != nil {
		return err
	}
	defer done()
	return p.ctx.eachSpec(func(s spec) error {
		outDir := p.servicesDir(s)
		if err := p.ctx.removeAll(p.Name(), filepath.Join(p.registry(), "services", s.Name)); err != nil {
			return err
		}
		n := 0
		err := p.pass(s, "services", templates, func(name, content string) error {
			n++
			return textops.WriteFile(filepath.Join(outDir, name), []byte(content))
		})
		if err == nil && n > 0 {
			p.ctx.R.Logf(p.Name(), "%d services file(s) in %s", n, p.ctx.Cfg.Rel(outDir))
		}
		return err
	})
}

// templates returns the mustache template dir for the generator, extracting
// the embedded ones to a temp dir unless proto.templates is set.
func (p *Proto) templates() (string, func(), error) {
	if p.c.Templates != "" {
		return rel(p.dir(), p.ctx.Cfg.Abs(p.c.Dir, p.c.Templates)), func() {}, nil
	}
	if p.ctx.R.DryRun {
		return "<embedded templates>", func() {}, nil
	}
	tmp, err := os.MkdirTemp("", "protoforge-templates-")
	if err != nil {
		return "", nil, err
	}
	done := func() { os.RemoveAll(tmp) }
	if err := assets.WriteTemplates(tmp, p.c.ProtoPackage); err != nil {
		done()
		return "", nil, err
	}
	return tmp, done, nil
}

// pass is one generator run over s's OpenAPI document: kind is "models"
// (--global-property models) or "services" (--global-property apis). Each
// emitted file is normalised and handed to write, then removed.
func (p *Proto) pass(s spec, kind, templates string, write func(name, content string) error) error {
	dir, reg := p.dir(), p.registry()
	argv, err := p.ctx.tool(runner.ToolSpec{Name: "openapi-generator-cli", Bin: "openapi-generator-cli", Override: p.ctx.Cfg.Tools.OpenAPIGenerator}, dir)
	if err != nil {
		return err
	}
	gp := "models"
	if kind == "services" {
		gp = "apis"
	}
	argv = append(argv, "generate",
		"-i", rel(dir, OpenAPIOutput(p.ctx.Cfg, s.Spec)),
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
	if !p.ctx.R.DryRun {
		// The generator runs in dir, which a fresh project may not have yet.
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := p.ctx.run(p.Name(), dir, argv); err != nil {
		return err
	}

	outDir := p.modelsDir()
	if kind == "services" {
		outDir = p.servicesDir(s)
	}
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
	o := p.norm(s)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		c := string(b)
		if kind == "models" {
			c = NormalizeModel(c, o)
		} else {
			c = NormalizeService(c, o)
		}
		if err := write(filepath.Base(f), c); err != nil {
			return err
		}
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	return nil
}

func (p *Proto) flatten() error {
	return p.ctx.eachSpec(func(s spec) error {
		if !p.ctx.R.FileOp(p.Name(), "flatten *Request messages in %s", p.ctx.Cfg.Rel(p.servicesDir(s))) {
			return nil
		}
		return flatten.Run(flatten.Options{
			Registry:  p.registry(),
			Version:   p.c.ProtoPackage,
			Services:  p.servicesDir(s),
			WellKnown: flattenWKT(p.c.WellKnown, p.c.AnyType),
		})
	})
}

func (p *Proto) extraReplace() error {
	rules, err := textops.Compile(p.c.ExtraReplace)
	if err != nil {
		return err
	}
	dirs := []string{p.modelsDir()}
	for _, s := range p.ctx.specs() {
		dirs = append(dirs, p.servicesDir(s))
	}
	for _, d := range dirs {
		if !p.ctx.R.FileOp(p.Name(), "apply extra_replace to %s", p.ctx.Cfg.Rel(d)) {
			continue
		}
		changed, err := textops.ApplyDir(d, rules)
		if err != nil {
			return err
		}
		for _, c := range changed {
			p.ctx.R.Debugf(p.Name(), "extra_replace rewrote %s", p.ctx.Cfg.Rel(filepath.Join(d, c)))
		}
	}
	return nil
}

func (p *Proto) codegen() error {
	return p.ctx.runCommands(p.Name(), p.dir(), p.c.Codegen.Commands)
}
