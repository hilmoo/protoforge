package stages

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hilmoo/protoforge/internal/config"
	"github.com/hilmoo/protoforge/internal/runner"
)

const fx = "../../testdata/fixtures"

var defaultNorm = NormOptions{Version: "v1", GoPackageRoot: "proto", AnyType: "google.protobuf.Any", DropImports: []string{"todo_object_mapping", "common_error_error"}}

const sample = "../../testdata/sample"

// TestNormalizeFixtures: raw openapi-generator output -> the old line-edit chain's output.
// Expected trees: the sample's models/v1 and its services/v1 right before
// flattening (testdata/fixtures/flatten/sample/input), made by the regen script in testdata/oracle.
func TestNormalizeFixtures(t *testing.T) {
	for _, kind := range []string{"models", "services"} {
		raw, _ := filepath.Glob(filepath.Join(fx, "proto", "raw", kind, "*.proto"))
		if len(raw) == 0 {
			t.Fatalf("no raw %s fixtures", kind)
		}
		for _, f := range raw {
			b, _ := os.ReadFile(f)
			var got string
			if kind == "models" {
				got = NormalizeModel(string(b), defaultNorm)
			} else {
				got = NormalizeService(string(b), defaultNorm)
			}
			want, err := os.ReadFile(filepath.Join(fx, "flatten", "sample", "input", kind, "v1", filepath.Base(f)))
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("%s/%s differs from the old line-edit output", kind, filepath.Base(f))
			}
		}
	}
}

func TestNormalizeOtherVersion(t *testing.T) {
	o := NormOptions{Version: "v2", GoPackageRoot: "github.com/x/gen", AnyType: "google.protobuf.Any", DropImports: []string{"skip_me"}}
	in := "package openapitools;\n\nimport \"models/a.proto\";\nimport \"models/any_type.proto\";\nimport \"models/skip_me.proto\";\n\nmessage M { AnyType x = 1; }\n"
	want := "package models.v2;\noption go_package = \"github.com/x/gen/models/v2;modelsv2\";\n\nimport \"models/v2/a.proto\";\nimport \"google/protobuf/any.proto\";\n\nmessage M { google.protobuf.Any x = 1; }\n"
	if got := NormalizeModel(in, o); got != want {
		t.Fatalf("got:\n%s", got)
	}
	in = "package openapitools.services.petservice;\n\nimport public \"models/a.proto\";\nimport public \"models/skip_me_too.proto\";\n"
	want = "package services.v2;\noption go_package = \"github.com/x/gen/services/v2;servicesv2\";\n\nimport \"models/v2/a.proto\";\n"
	if got := NormalizeService(in, o); got != want {
		t.Fatalf("got:\n%s", got)
	}
	o.Spec, o.ServicesVersion = "pdf_render", "v3"
	want = "package services.pdf_render.v3;\noption go_package = \"github.com/x/gen/services/pdf_render/v3;pdfrenderv3\";\n\nimport \"models/v2/a.proto\";\n"
	if got := NormalizeService(in, o); got != want {
		t.Fatalf("got:\n%s", got)
	}
}

// oapiDoc is a minimal OpenAPI document with the shared ApiError model, one
// spec-specific model and one operation returning it. Titles differ per spec,
// as with real services, so the generated file headers differ too. extra adds
// a field to ApiError, so the shared model differs from the other spec's.
func oapiDoc(model, op string, extra bool) string {
	errProps := `"code": {"type": "integer", "format": "int32"}, "message": {"type": "string"}`
	if extra {
		errProps += `, "detail": {"type": "string"}`
	}
	return `{
  "openapi": "3.0.0",
  "info": {"title": "` + model + `", "version": "1.0.0"},
  "paths": {
    "/` + op + `": {
      "get": {
        "operationId": "` + op + `",
        "tags": ["` + model + `"],
        "responses": {
          "200": {"description": "ok", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/` + model + `"}}}},
          "default": {"description": "error", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/ApiError"}}}}
        }
      }
    }
  },
  "components": {
    "schemas": {
      "ApiError": {"type": "object", "required": ["code", "message"], "properties": {` + errProps + `}},
      "` + model + `": {"type": "object", "required": ["id"], "properties": {"id": {"type": "string"}}}
    }
  }
}
`
}

// TestProtoMultiSpecWithGenerator runs the real openapi-generator over two
// specs that share a model: the shared model is written once into models/v1,
// each spec gets its own services package, --spec keeps the other spec's
// models and services, and a shared model that differs between specs fails.
func TestProtoMultiSpecWithGenerator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	if _, err := exec.LookPath("openapi-generator-cli"); err != nil {
		t.Skip("openapi-generator-cli not installed")
	}
	root := t.TempDir()
	cfg, err := config.Parse([]byte("version: 1\nopenapi:\n  dir: api\nproto:\nspecs:\n  core: {entry: ./src/core}\n  pdf: {entry: ./src/pdf}\n"), root)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, s string) {
		t.Helper()
		p := filepath.Join(root, "api", name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("core.openapi.json", oapiDoc("CoreThing", "getThing", false))
	write("pdf.openapi.json", oapiDoc("PdfDoc", "getDoc", false))

	run := func(specs ...string) (string, error) {
		r := runner.New(root, os.Environ())
		var out bytes.Buffer
		r.Stdout, r.Stderr = &out, &out
		ctx := &Ctx{Cfg: cfg, R: r, NodeRunner: "npx", Specs: specs}
		s, _ := New(ctx, "proto")
		err := RunStage(ctx, s, []string{"models", "services"})
		return out.String(), err
	}
	reg := filepath.Join(root, "proto", "registry")
	read := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(reg, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	servicePkg := func(spec string) string {
		t.Helper()
		files, _ := filepath.Glob(filepath.Join(reg, "services", spec, "v1", "*.proto"))
		if len(files) == 0 {
			t.Fatalf("no services/%s/v1/*.proto", spec)
		}
		b, _ := os.ReadFile(files[0])
		return string(b)
	}

	if out, err := run(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, f := range []string{"models/v1/api_error.proto", "models/v1/core_thing.proto", "models/v1/pdf_doc.proto"} {
		if !strings.Contains(read(f), "package models.v1;") {
			t.Errorf("%s: not in package models.v1", f)
		}
	}
	for _, spec := range []string{"core", "pdf"} {
		s := servicePkg(spec)
		if !strings.Contains(s, "package services."+spec+".v1;") || !strings.Contains(s, `option go_package = "proto/services/`+spec+`/v1;`+spec+`v1";`) {
			t.Errorf("services/%s: wrong package:\n%s", spec, s)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(reg, "services", "*.proto")); len(left) > 0 {
		t.Errorf("generator output left behind: %v", left)
	}

	// --spec pdf: core's services stay, and core's models are still generated.
	coreSvc := servicePkg("core")
	if out, err := run("pdf"); err != nil {
		t.Fatalf("--spec pdf: %v\n%s", err, out)
	}
	if servicePkg("core") != coreSvc {
		t.Error("--spec pdf changed services/core")
	}
	read("models/v1/core_thing.proto")

	// The shared ApiError now differs between the two documents.
	write("pdf.openapi.json", oapiDoc("PdfDoc", "getDoc", true))
	out, err := run()
	if err == nil || !strings.Contains(err.Error(), "api_error.proto differs between specs core and pdf") {
		t.Fatalf("want a shared-model conflict, got %v\n%s", err, out)
	}
}

// TestMultiSpecDryRun: with --spec pdf only pdf is compiled, formatted and gets
// services, but the shared models are still generated from every spec.
func TestMultiSpecDryRun(t *testing.T) {
	cfg, err := config.Parse([]byte(`version: 1
typespec:
  dir: api
openapi:
  dir: api
  format: true
proto:
  flatten_requests: true
specs:
  core: {entry: ./src/core}
  pdf: {entry: ./src/pdf, proto_package: v2}
`), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := runner.New(cfg.Root, nil)
	var out bytes.Buffer
	r.Stdout, r.Stderr, r.DryRun = &out, &out, true
	ctx := &Ctx{Cfg: cfg, R: r, NodeRunner: "npx", Specs: []string{"pdf"}}
	for _, name := range []string{"typespec", "openapi", "proto"} {
		s, _ := New(ctx, name)
		if err := RunStage(ctx, s, nil); err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out.String())
		}
	}
	// Quoting and separators differ per OS.
	got := strings.NewReplacer("'", "", `\`, "/").Replace(out.String())
	for _, want := range []string{
		"compile ./src/pdf",
		"-o ./pdf.openapi.json ./pdf.openapi.json",
		"-i ../api/core.openapi.json",
		"-i ../api/pdf.openapi.json",
		"proto/registry/services/pdf/v2",
		"flatten *Request messages in proto/registry/services/pdf/v2",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, bad := range []string{"compile ./src/core", "core.openapi.json ./core.openapi.json", "services/core", "remove proto/registry/services\n"} {
		if strings.Contains(got, bad) {
			t.Errorf("unexpected %q", bad)
		}
	}
	if t.Failed() {
		t.Log(got)
	}
	if n := strings.Count(got, "-i ../api/core.openapi.json"); n != 1 {
		t.Errorf("core document used by %d generator runs, want 1 (models only)", n)
	}
}

func TestGeneratorMappings(t *testing.T) {
	ty, im := generatorMappings(map[string]string{"DateTime": "google.protobuf.Timestamp"})
	if ty != "DateTime=google.protobuf.Timestamp" || im != "google.protobuf.Timestamp=google/protobuf/timestamp" {
		t.Fatalf("%s | %s", ty, im)
	}
}

// sampleCopy copies testdata/sample into a temp dir, symlinking (not copying)
// node_modules. It skips the test when the sample's npm deps are not installed
// (`npm install` in testdata/sample/db and testdata/sample/api).
func sampleCopy(t *testing.T, needNode ...string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test")
	}
	src, _ := filepath.Abs(sample)
	for _, d := range needNode {
		if _, err := os.Stat(filepath.Join(src, d, "node_modules")); err != nil {
			t.Skipf("run `npm install` in testdata/sample/%s to enable this test", d)
		}
	}
	dst := filepath.Join(t.TempDir(), "sample")
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if d.IsDir() && d.Name() == "node_modules" {
			if err := os.Symlink(p, out); err != nil {
				t.Skipf("cannot symlink node_modules: %v", err)
			}
			return filepath.SkipDir
		}
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
	return dst
}

func sampleCtx(t *testing.T, root string) (*Ctx, *bytes.Buffer) {
	t.Helper()
	cfg, err := config.Load(filepath.Join(root, "protoforge.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	r := runner.New(root, os.Environ())
	var out bytes.Buffer
	r.Stdout, r.Stderr = &out, &out
	return &Ctx{Cfg: cfg, R: r, NodeRunner: "npx"}, &out
}

// snapshot reads every file under root except node_modules.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	m := map[string]string{}
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == "node_modules" {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			b, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(root, p)
			m[filepath.ToSlash(rel)] = string(b)
		}
		return nil
	})
	return m
}

// TestDrizzleToTypeSpec runs the embedded script against the sample schema; the
// result must equal api/src/_generated/db.tsp, which the original script produced.
func TestDrizzleToTypeSpec(t *testing.T) {
	root := sampleCopy(t, "db")
	ctx, out := sampleCtx(t, root)
	want, _ := os.ReadFile(filepath.Join(root, "api", "src", "_generated", "db.tsp"))
	os.Remove(filepath.Join(root, "api", "src", "_generated", "db.tsp"))
	ts := &TypeSpec{ctx: ctx, c: ctx.Cfg.TypeSpec}
	if err := ts.fromDrizzle(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got, _ := os.ReadFile(filepath.Join(root, "api", "src", "_generated", "db.tsp"))
	if string(got) != string(want) {
		t.Fatalf("generated TypeSpec differs from the golden db.tsp:\n%s", got)
	}
	if !strings.Contains(out.String(), "drizzle-to-typespec runner: ") {
		t.Error("runner choice not reported")
	}
	if !strings.Contains(out.String(), "no doc comment for reviews.body") {
		t.Error("require_docs: warn did not warn")
	}
	if left, _ := filepath.Glob(filepath.Join(root, "db", "node_modules", ".cache", "protoforge", "*.mts")); len(left) > 0 {
		t.Errorf("temp script not removed: %v", left)
	}
}

// TestDrizzleSpreads covers doc resolution for spread columns in every form:
// aliased and namespace imports, nested and non-exported spreads, helper
// functions, inline spreads, overrides, JSDoc, trailing comments, shorthand
// properties, the (t) => ({...}) form and schema.table(...).
func TestDrizzleSpreads(t *testing.T) {
	root := sampleCopy(t, "db")
	ctx, out := sampleCtx(t, root)
	src, _ := filepath.Abs(filepath.Join(fx, "drizzle", "spreads", "schema"))
	dst := filepath.Join(root, "db", "src", "spreads")
	os.MkdirAll(dst, 0o755)
	files, _ := filepath.Glob(filepath.Join(src, "*.ts"))
	for _, f := range files {
		b, _ := os.ReadFile(f)
		os.WriteFile(filepath.Join(dst, filepath.Base(f)), b, 0o644)
	}
	ctx.Cfg.TypeSpec.FromDrizzle.SchemaDir = "../db/src/spreads"
	ctx.Cfg.TypeSpec.FromDrizzle.Out = "spreads.tsp"
	ts := &TypeSpec{ctx: ctx, c: ctx.Cfg.TypeSpec}
	if err := ts.fromDrizzle(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got, _ := os.ReadFile(filepath.Join(root, "api", "spreads.tsp"))
	want, _ := os.ReadFile(filepath.Join(fx, "drizzle", "spreads", "expected.tsp"))
	// The header names the schema dir, which differs between fixture and copy.
	g, w := strings.SplitN(string(got), "\n", 2), strings.SplitN(string(want), "\n", 2)
	if g[1] != w[1] {
		t.Fatalf("spreads output differs from golden:\n%s", got)
	}
	for _, warn := range []string{"no doc comment for items.name", "no doc comment for orders.status"} {
		if !strings.Contains(out.String(), warn) {
			t.Errorf("missing warning %q", warn)
		}
	}
	if strings.Count(out.String(), "no doc comment") != 2 {
		t.Errorf("unexpected warnings:\n%s", out)
	}
}

// TestProtoWithGenerator runs the real openapi-generator on the sample's OpenAPI
// document and compares the registry with the golden one.
func TestProtoWithGenerator(t *testing.T) {
	if _, err := exec.LookPath("openapi-generator-cli"); err != nil {
		t.Skip("openapi-generator-cli not installed; normalisation is covered by TestNormalizeFixtures")
	}
	root := sampleCopy(t)
	want := snapshot(t, filepath.Join(root, "proto"))
	os.RemoveAll(filepath.Join(root, "proto", "registry"))
	ctx, out := sampleCtx(t, root)
	s, _ := New(ctx, "proto")
	if err := RunStage(ctx, s, nil); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	compareSnapshots(t, snapshot(t, filepath.Join(root, "proto")), want)
}

// TestSamplePipeline is the end-to-end check: `protoforge run all` on a pristine
// copy of the sample must leave every file exactly as checked in (the
// equivalent of a clean `git status`).
func TestSamplePipeline(t *testing.T) {
	if _, err := exec.LookPath("openapi-generator-cli"); err != nil {
		t.Skip("openapi-generator-cli not installed")
	}
	root := sampleCopy(t, "db", "api")
	want := snapshot(t, root)
	// Delete every generated output first, so nothing passes by being left alone.
	for _, p := range []string{"api/src/_generated", "api/openapi.json", "proto/registry", "server/migrations", "db/schema.dbml"} {
		os.RemoveAll(filepath.Join(root, filepath.FromSlash(p)))
	}
	ctx, out := sampleCtx(t, root)
	names, err := ctx.Cfg.Expand([]string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		s, err := New(ctx, n)
		if err != nil {
			t.Fatal(err)
		}
		if err := RunStage(ctx, s, nil); err != nil {
			t.Fatalf("%s: %v\n%s", n, err, out)
		}
	}
	compareSnapshots(t, snapshot(t, root), want)
}

func compareSnapshots(t *testing.T, got, want map[string]string) {
	t.Helper()
	for p, w := range want {
		g, ok := got[p]
		switch {
		case !ok:
			t.Errorf("missing: %s", p)
		case g != w:
			t.Errorf("changed: %s", p)
		}
	}
	for p := range got {
		if _, ok := want[p]; !ok {
			t.Errorf("unexpected new file: %s", p)
		}
	}
}

func TestRunStageOnly(t *testing.T) {
	cfg, err := config.Parse([]byte("version: 1\nopenapi:\n  format: true\n"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := runner.New(cfg.Root, nil)
	var out bytes.Buffer
	r.Stdout, r.Stderr, r.DryRun = &out, &out, true
	ctx := &Ctx{Cfg: cfg, R: r, NodeRunner: "npx"}
	s, _ := New(ctx, "openapi")
	if err := RunStage(ctx, s, []string{"nope"}); err == nil || !strings.Contains(err.Error(), "no step") {
		t.Fatalf("unknown --only should fail: %v", err)
	}
	if err := RunStage(ctx, s, []string{"lint"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "step lint") || strings.Contains(out.String(), "step format") {
		t.Fatalf("--only lint ran the wrong steps:\n%s", out.String())
	}
	if _, err := New(ctx, "proto"); err == nil {
		t.Fatal("unconfigured stage should error")
	}
}

// config.StageSteps must list exactly the steps the stages implement, in order.
func TestStageStepsMatchConfig(t *testing.T) {
	ctx := &Ctx{Cfg: &config.Config{
		Migration: &config.Migration{}, TypeSpec: &config.TypeSpec{},
		OpenAPI: &config.OpenAPI{}, Proto: &config.Proto{},
	}}
	for _, name := range config.StageNames {
		s, err := New(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, st := range s.Steps() {
			got = append(got, st.Name)
		}
		if !reflect.DeepEqual(got, config.StageSteps[name]) {
			t.Errorf("%s: steps %v, config.StageSteps %v", name, got, config.StageSteps[name])
		}
	}
}
