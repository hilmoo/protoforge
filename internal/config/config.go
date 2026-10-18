// Package config loads, defaults and validates protoforge.yaml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName is the default config file name.
const FileName = "protoforge.yaml"

// Config is the root of protoforge.yaml.
//
// Struct tags drive both YAML decoding and the generated JSON Schema
// (see schema.go): `doc` is the description, `default` a JSON default value,
// `enum` comma-separated JSON values, `pattern` a regex, `nullable` allows an
// empty (null) section that means "enabled with defaults".
type Config struct {
	Version   int                 `yaml:"version" doc:"Config schema version." enum:"1" default:"1"`
	EnvFile   string              `yaml:"env_file,omitempty" doc:"dotenv file loaded into every child process (and used for DATABASE_URL). Does not override variables already set. A missing file is ignored."`
	Env       map[string]string   `yaml:"env,omitempty" doc:"Extra environment variables for child processes. Values support ${VAR} expansion and override the environment."`
	Tools     Tools               `yaml:"tools,omitempty" doc:"Overrides for how external tools are invoked. All optional; auto-detected."`
	Migration *Migration          `yaml:"migration,omitempty" nullable:"true" doc:"Drizzle stage: drizzle-kit generate/push/migrate, drizzle-docs, post_push_sql and sync. Absent = stage disabled; empty = enabled with defaults."`
	TypeSpec  *TypeSpec           `yaml:"typespec,omitempty" nullable:"true" doc:"TypeSpec stage: embedded Drizzle -> TypeSpec generator and tsp compile. Absent = stage disabled; empty = enabled with defaults."`
	OpenAPI   *OpenAPI            `yaml:"openapi,omitempty" nullable:"true" doc:"OpenAPI stage: openapi-format and Go struct-tag injection. Absent = stage disabled; empty = enabled with defaults."`
	Proto     *Proto              `yaml:"proto,omitempty" nullable:"true" doc:"Proto stage: openapi-generator protobuf-schema passes, normalisation and request flattening. Absent = stage disabled; empty = enabled with defaults."`
	Pipelines map[string][]string `yaml:"pipelines,omitempty" doc:"Named sequences of stages, single steps (stage.step, e.g. typespec.from_drizzle) and/or other pipelines. Defaults: api = [typespec, openapi, proto], sql = [migration], all = [sql, api], preview = [migration.docs, typespec, openapi] (no DB writes), proto-gen = api without proto codegen (no DB writes)." stageref:"true"`

	// Root is the directory holding the config file. Every relative path is
	// resolved against it (or against a stage dir, which is itself relative to Root).
	Root string `yaml:"-"`
	// Path is the absolute path of the loaded config file ("" for in-memory configs).
	Path string `yaml:"-"`
}

// Tools overrides how external tools are invoked. Every field is optional.
// A value is a command line (parsed into argv), e.g. "java -jar openapi-generator.jar".
type Tools struct {
	NodeRunner       string `yaml:"node_runner,omitempty" doc:"Package-manager exec command used as the last fallback for npm tools. Default: picked from the nearest lockfile (pnpm exec | bunx | yarn exec | npx)." examples:"[\"pnpm exec\",\"npx\",\"bunx\"]"`
	OpenAPIGenerator string `yaml:"openapi_generator,omitempty" doc:"Command for openapi-generator. Default: openapi-generator-cli on PATH." examples:"[\"openapi-generator-cli\",\"java -jar openapi-generator-cli.jar\",\"npx @openapitools/openapi-generator-cli\"]"`
	DrizzleKit       string `yaml:"drizzle_kit,omitempty" doc:"Command for drizzle-kit. Default: node_modules/.bin, then PATH, then the node runner."`
	DrizzleDocs      string `yaml:"drizzle_docs,omitempty" doc:"Command for drizzle-docs. Default: node_modules/.bin, then PATH, then the node runner."`
	TSP              string `yaml:"tsp,omitempty" doc:"Command for the TypeSpec compiler. Default: node_modules/.bin, then PATH, then the node runner."`
	OpenAPIFormat    string `yaml:"openapi_format,omitempty" doc:"Command for openapi-format. Default: node_modules/.bin, then PATH, then the node runner."`
	Spectral         string `yaml:"spectral,omitempty" doc:"Command for spectral (openapi --only lint)."`
	TSRunner         string `yaml:"ts_runner,omitempty" doc:"Runs the embedded Drizzle -> TypeSpec script. Default: tsx, then bun, then node --experimental-strip-types (Node >= 22.6)." examples:"[\"tsx\",\"bun\",\"node --experimental-strip-types\"]"`
}

// Command is one escape-hatch command (run_after, codegen). In YAML it is either a
// plain string or {cmd: "...", shell: true}.
type Command struct {
	Cmd   string `yaml:"cmd" doc:"Command line. Split into argv shell-style and run without a shell unless shell is true."`
	Shell bool   `yaml:"shell,omitempty" doc:"Run through sh -c (cmd /C on Windows). Escape hatch only." default:"false"`
}

// UnmarshalYAML accepts both the string and the mapping form.
func (c *Command) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		c.Cmd = n.Value
		return nil
	}
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: command must be a string or {cmd, shell}", n.Line)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		var err error
		switch k.Value {
		case "cmd":
			err = v.Decode(&c.Cmd)
		case "shell":
			err = v.Decode(&c.Shell)
		default:
			return fmt.Errorf("line %d: field %s not found in type config.Command", k.Line, k.Value)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// MarshalYAML prints the short form when possible.
func (c Command) MarshalYAML() (any, error) {
	if !c.Shell {
		return c.Cmd, nil
	}
	type plain Command
	return plain(c), nil
}

// ReplaceRule is one extra_replace rule. Exactly one of From, Regex or
// DeleteLineMatching must be set.
type ReplaceRule struct {
	Glob               string `yaml:"glob" required:"true" doc:"Files the rule applies to. Without a slash it matches the base name (e.g. *.proto); with a slash, the path relative to the rule's base dir."`
	From               string `yaml:"from,omitempty" doc:"Literal text to replace with 'to'."`
	Regex              string `yaml:"regex,omitempty" doc:"Go RE2 regex applied to the whole file; 'to' may use $1 / ${name}. Use (?m) for per-line ^ and $."`
	To                 string `yaml:"to,omitempty" doc:"Replacement for from/regex."`
	DeleteLineMatching string `yaml:"delete_line_matching,omitempty" doc:"Go RE2 regex; every matching line is deleted."`
}

// Migration configures the drizzle stage.
type Migration struct {
	Dir           string    `yaml:"dir" doc:"Drizzle project directory." default:"\"migration\""`
	DrizzleConfig string    `yaml:"drizzle_config" doc:"Passed to drizzle-kit as --config (relative to dir)." default:"\"drizzle.config.ts\""`
	DrizzleOut    string    `yaml:"drizzle_out" doc:"Drizzle's out directory, read by sync (relative to dir)." default:"\"drizzle\""`
	Generate      bool      `yaml:"generate" doc:"Run drizzle-kit generate." default:"false"`
	Push          bool      `yaml:"push" doc:"Run drizzle-kit push (then post_push_sql)." default:"false"`
	Migrate       bool      `yaml:"migrate" doc:"Run drizzle-kit migrate." default:"false"`
	Docs          *Docs     `yaml:"docs,omitempty" doc:"Run drizzle-docs generate. Use docs: {} for all defaults; omit to skip."`
	PostPushSQL   []string  `yaml:"post_push_sql,omitempty" doc:"Raw SQL run in ONE transaction over DATABASE_URL after push (rolled back on error). Each entry may hold several statements or DO blocks. Also runnable with --only post_push_sql."`
	Sync          *Sync     `yaml:"sync,omitempty" doc:"Copy drizzle/<name>/migration.sql to <to>/<name><suffix>, turning backticks into double quotes and removing statement-breakpoint lines. Omit to skip."`
	RunAfter      []Command `yaml:"run_after,omitempty" doc:"Escape-hatch commands run in dir after the stage."`
}

// Docs configures `drizzle-docs generate`.
type Docs struct {
	SchemaDir string `yaml:"schema_dir" doc:"Schema directory (relative to migration.dir)." default:"\"./src/schema\""`
	Dialect   string `yaml:"dialect" doc:"Passed as -d." default:"\"postgresql\"" examples:"[\"postgresql\",\"mysql\",\"sqlite\"]"`
	Format    string `yaml:"format" doc:"Passed as -f." default:"\"dbml\"" examples:"[\"dbml\",\"markdown\"]"`
	Out       string `yaml:"out" doc:"Output file (relative to migration.dir)." default:"\"schema.dbml\""`
}

// Sync configures the built-in migration sync (the old shell script).
type Sync struct {
	To           string        `yaml:"to" required:"true" doc:"Target directory (relative to migration.dir). Its contents are replaced; must be inside the repo root."`
	Suffix       string        `yaml:"suffix" doc:"Suffix appended to each migration directory name." default:"\".up.sql\""`
	ExtraReplace []ReplaceRule `yaml:"extra_replace,omitempty" doc:"Extra rewrite rules applied to each written file."`
}

// TypeSpec configures the typespec stage.
type TypeSpec struct {
	Dir         string       `yaml:"dir" doc:"TypeSpec project directory." default:"\"openapi\""`
	Entry       string       `yaml:"entry" doc:"Entry passed to tsp compile (relative to dir). Without dir/tspconfig.yaml a built-in config is used." default:"\"./src\""`
	FromDrizzle *FromDrizzle `yaml:"from_drizzle,omitempty" doc:"Generate TypeSpec models/enums from the Drizzle schema with the embedded script. Omit to skip."`
	RunAfter    []Command    `yaml:"run_after,omitempty" doc:"Escape-hatch commands run in dir after the stage."`
}

// FromDrizzle configures the embedded Drizzle -> TypeSpec script. Paths are
// relative to typespec.dir.
type FromDrizzle struct {
	SchemaDir       string   `yaml:"schema_dir" doc:"Directory of Drizzle *.ts schema files (relative to typespec.dir)." default:"\"../migration/src/schema\""`
	Out             string   `yaml:"out" doc:"Generated .tsp file (relative to typespec.dir)." default:"\"src/_generated/db.tsp\""`
	Namespace       string   `yaml:"namespace" doc:"TypeSpec namespace of the generated file." default:"\"DbSchema\"" pattern:"^[A-Za-z_][\\w.]*$"`
	ReadOnlyColumns []string `yaml:"read_only_columns" doc:"Columns that get @visibility(Lifecycle.Read, Lifecycle.Delete)." default:"[\"id\",\"created_at\",\"updated_at\"]"`
	RequireDocs     string   `yaml:"require_docs" doc:"Columns without a // doc comment: warn prints a warning, error fails without writing, off is silent." enum:"\"warn\",\"error\",\"off\"" default:"\"warn\""`
}

// OpenAPI configures the openapi stage.
type OpenAPI struct {
	Dir      string    `yaml:"dir" doc:"Directory holding the OpenAPI document." default:"\"openapi\""`
	Output   string    `yaml:"output" doc:"Compiled OpenAPI JSON (relative to dir)." default:"\"openapi.json\""`
	Format   bool      `yaml:"format" doc:"Run openapi-format with dir/oaf-config.yaml (or a built-in config)." default:"false"`
	GoTags   bool      `yaml:"go_tags" doc:"Inject x-oapi-codegen-extra-tags (validate / jsonschema Go struct tags)." default:"false"`
	Lint     string    `yaml:"lint,omitempty" doc:"Spectral ruleset path; used only by stage openapi --only lint."`
	RunAfter []Command `yaml:"run_after,omitempty" doc:"Escape-hatch commands run in dir after the stage."`
}

// Proto configures the proto stage.
type Proto struct {
	Dir             string            `yaml:"dir" doc:"Working directory for openapi-generator." default:"\"proto\""`
	Registry        string            `yaml:"registry" doc:"Generator output root (relative to dir)." default:"\"registry\""`
	Templates       string            `yaml:"templates,omitempty" doc:"Directory with api.mustache / model.mustache (relative to dir). Default: the embedded templates."`
	ProtoPackage    string            `yaml:"proto_package" doc:"Version segment: package models.<v> / services.<v>, output dirs registry/{models,services}/<v>." default:"\"v1\"" pattern:"^[a-z][a-z0-9_]*$"`
	GoPackageRoot   string            `yaml:"go_package_root" doc:"option go_package = \"<root>/models/<v>;models<v>\" (and services)." default:"\"proto\""`
	WellKnown       map[string]string `yaml:"well_known" doc:"OpenAPI type -> protobuf type, passed as --type-mappings with matching --import-mappings." default:"{\"DateTime\":\"google.protobuf.Timestamp\"}"`
	AnyType         string            `yaml:"any_type" doc:"Replacement for the generator's AnyType (and its any_type.proto import)." default:"\"google.protobuf.Any\""`
	DropImports     []string          `yaml:"drop_imports" doc:"Import stems (prefix match) whose import lines are removed, e.g. todo_object_mapping." default:"[]"`
	FlattenRequests bool              `yaml:"flatten_requests" doc:"Inline model fields into *Request service messages." default:"false"`
	ExtraReplace    []ReplaceRule     `yaml:"extra_replace,omitempty" doc:"Rewrite rules applied to registry/{models,services}/<v> after flattening."`
	Codegen         *Codegen          `yaml:"codegen,omitempty" doc:"Optional minimal codegen step run in dir (e.g. buf generate)."`
	RunAfter        []Command         `yaml:"run_after,omitempty" doc:"Escape-hatch commands run in dir after the stage."`
}

// Codegen is the optional minimal codegen stage (buf generate or a command list).
type Codegen struct {
	Enabled  bool      `yaml:"enabled" doc:"Run the commands at the end of the proto stage." default:"false"`
	Commands []Command `yaml:"commands" doc:"Commands to run in proto.dir." default:"[\"buf generate\"]"`
}

// StageNames lists the built-in stages in pipeline order.
var StageNames = []string{"migration", "typespec", "openapi", "proto"}

// StageSteps lists the sub-steps of each stage in run order. It mirrors the
// Steps() of the stage implementations (checked by a test in package stages) and
// makes `stage.step` references such as typespec.from_drizzle valid in pipelines.
var StageSteps = map[string][]string{
	"migration": {"generate", "docs", "push", "post_push_sql", "migrate", "sync", "run_after"},
	"typespec":  {"from_drizzle", "compile", "run_after"},
	"openapi":   {"format", "go_tags", "lint", "run_after"},
	"proto":     {"models", "services", "flatten", "extra_replace", "cleanup", "codegen", "run_after"},
}

// SplitRef splits a "stage" or "stage.step" reference. step is "" for a whole stage.
func SplitRef(ref string) (stage, step string) {
	stage, step, _ = strings.Cut(ref, ".")
	return
}

// IsRef reports whether ref is a built-in stage or a known stage.step.
func IsRef(ref string) bool {
	stage, step := SplitRef(ref)
	if !IsStage(stage) {
		return false
	}
	if step == "" && !strings.Contains(ref, ".") {
		return true
	}
	for _, s := range StageSteps[stage] {
		if s == step {
			return true
		}
	}
	return false
}

// IsStage reports whether name is a built-in stage.
func IsStage(name string) bool {
	for _, s := range StageNames {
		if s == name {
			return true
		}
	}
	return false
}

// DefaultPipelines are always available unless overridden.
func DefaultPipelines() map[string][]string {
	return map[string][]string{
		"api": {"typespec", "openapi", "proto"},
		"sql": {"migration"},
		"all": {"sql", "api"},
		// Schema change preview: docs + OpenAPI, never touches the database.
		"preview": {"migration.docs", "typespec", "openapi"},
		// Schema -> OpenAPI -> proto without codegen or database writes.
		"proto-gen": {"typespec", "openapi", "proto.models", "proto.services", "proto.flatten", "proto.extra_replace", "proto.cleanup"},
	}
}

// Find walks up from dir looking for protoforge.yaml.
func Find(dir string) (string, error) {
	d, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		p := filepath.Join(d, FileName)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("%s not found in %s or any parent directory (run `protoforge init`)", FileName, dir)
		}
		d = parent
	}
}

// Load reads, defaults and validates the config at path.
func Load(path string) (*Config, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	cfg, err := Parse(data, filepath.Dir(abs))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg.Path = abs
	return cfg, nil
}

// Parse decodes YAML (unknown keys are errors with line numbers), applies
// defaults and validates. root is the directory relative paths resolve against.
func Parse(data []byte, root string) (*Config, error) {
	var n yaml.Node
	if err := yaml.Unmarshal(data, &n); err != nil {
		return nil, err
	}
	cfg := &Config{}
	if len(n.Content) > 0 {
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(cfg); err != nil {
			return nil, err
		}
		// Pipeline/stage sections that are present but empty (`proto:`) mean "use defaults".
		markPresent(n.Content[0], cfg)
	}
	cfg.Root = root
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// markPresent turns `section:` with a null value into an empty (defaulted) section.
func markPresent(root *yaml.Node, cfg *Config) {
	if root.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i].Value, root.Content[i+1]
		if v.Tag != "!!null" {
			continue
		}
		switch k {
		case "migration":
			cfg.Migration = &Migration{}
		case "typespec":
			cfg.TypeSpec = &TypeSpec{}
		case "openapi":
			cfg.OpenAPI = &OpenAPI{}
		case "proto":
			cfg.Proto = &Proto{}
		}
	}
}

// ApplyDefaults fills every unset option with its documented default.
func (c *Config) ApplyDefaults() {
	if c.Version == 0 {
		c.Version = 1
	}
	if m := c.Migration; m != nil {
		def(&m.Dir, "migration")
		def(&m.DrizzleConfig, "drizzle.config.ts")
		def(&m.DrizzleOut, "drizzle")
		if d := m.Docs; d != nil {
			def(&d.SchemaDir, "./src/schema")
			def(&d.Dialect, "postgresql")
			def(&d.Format, "dbml")
			def(&d.Out, "schema.dbml")
		}
		if s := m.Sync; s != nil {
			def(&s.Suffix, ".up.sql")
		}
	}
	if t := c.TypeSpec; t != nil {
		def(&t.Dir, "openapi")
		def(&t.Entry, "./src")
		if f := t.FromDrizzle; f != nil {
			def(&f.SchemaDir, "../migration/src/schema")
			def(&f.Out, "src/_generated/db.tsp")
			def(&f.Namespace, "DbSchema")
			def(&f.RequireDocs, "warn")
			if f.ReadOnlyColumns == nil {
				f.ReadOnlyColumns = []string{"id", "created_at", "updated_at"}
			}
		}
	}
	if o := c.OpenAPI; o != nil {
		def(&o.Dir, "openapi")
		def(&o.Output, "openapi.json")
	}
	if p := c.Proto; p != nil {
		def(&p.Dir, "proto")
		def(&p.Registry, "registry")
		def(&p.ProtoPackage, "v1")
		def(&p.GoPackageRoot, "proto")
		def(&p.AnyType, "google.protobuf.Any")
		if p.WellKnown == nil {
			p.WellKnown = map[string]string{"DateTime": "google.protobuf.Timestamp"}
		}
		if p.Codegen != nil && len(p.Codegen.Commands) == 0 {
			p.Codegen.Commands = []Command{{Cmd: "buf generate"}}
		}
	}
	pl := DefaultPipelines()
	for k, v := range c.Pipelines {
		pl[k] = v
	}
	c.Pipelines = pl
}

func def(p *string, v string) {
	if *p == "" {
		*p = v
	}
}

var protoPkgRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Validate checks the config without touching the filesystem (so a freshly
// initialised config validates in an empty directory).
func (c *Config) Validate() error {
	var errs []string
	add := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }

	if c.Version != 1 {
		add("version: unsupported version %d (expected 1)", c.Version)
	}
	for k := range c.Env {
		if k == "" || strings.ContainsAny(k, "= ") {
			add("env: invalid variable name %q", k)
		}
	}
	if m := c.Migration; m != nil {
		if d := m.Docs; d != nil && d.Out == "" {
			add("migration.docs.out: must not be empty")
		}
		if s := m.Sync; s != nil {
			if s.To == "" {
				add("migration.sync.to: required")
			}
			validateRules("migration.sync.extra_replace", s.ExtraReplace, add)
		}
		validateCommands("migration.run_after", m.RunAfter, add)
	}
	if t := c.TypeSpec; t != nil {
		if f := t.FromDrizzle; f != nil {
			switch f.RequireDocs {
			case "warn", "error", "off":
			default:
				add("typespec.from_drizzle.require_docs: must be warn, error or off (got %q)", f.RequireDocs)
			}
			if !regexp.MustCompile(`^[A-Za-z_][\w.]*$`).MatchString(f.Namespace) {
				add("typespec.from_drizzle.namespace: invalid TypeSpec namespace %q", f.Namespace)
			}
		}
		validateCommands("typespec.run_after", t.RunAfter, add)
	}
	if o := c.OpenAPI; o != nil {
		validateCommands("openapi.run_after", o.RunAfter, add)
	}
	if p := c.Proto; p != nil {
		if !protoPkgRe.MatchString(p.ProtoPackage) {
			add("proto.proto_package: must match %s (got %q)", protoPkgRe, p.ProtoPackage)
		}
		for k, v := range p.WellKnown {
			if k == "" || v == "" {
				add("proto.well_known: empty mapping %q: %q", k, v)
			}
		}
		for _, d := range p.DropImports {
			if d == "" {
				add("proto.drop_imports: empty entry")
			}
		}
		validateRules("proto.extra_replace", p.ExtraReplace, add)
		if p.Codegen != nil {
			validateCommands("proto.codegen.commands", p.Codegen.Commands, add)
		}
		validateCommands("proto.run_after", p.RunAfter, add)
	}
	for name, steps := range c.Pipelines {
		if IsStage(name) {
			add("pipelines.%s: name collides with a built-in stage", name)
			continue
		}
		if len(steps) == 0 {
			add("pipelines.%s: empty pipeline", name)
		}
		for _, s := range steps {
			if !IsRef(s) {
				if _, ok := c.Pipelines[s]; !ok {
					add("pipelines.%s: unknown stage, step or pipeline %q", name, s)
				}
			}
		}
	}
	for name := range c.Pipelines {
		if _, err := c.Expand([]string{name}); err != nil {
			add("pipelines.%s: %v", name, err)
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return errors.New("invalid config:\n  " + strings.Join(errs, "\n  "))
	}
	return nil
}

func validateRules(path string, rules []ReplaceRule, add func(string, ...any)) {
	for i, r := range rules {
		p := fmt.Sprintf("%s[%d]", path, i)
		if r.Glob == "" {
			add("%s.glob: required", p)
		} else if _, err := filepath.Match(r.Glob, "x"); err != nil {
			add("%s.glob: %v", p, err)
		}
		n := 0
		for _, s := range []string{r.From, r.Regex, r.DeleteLineMatching} {
			if s != "" {
				n++
			}
		}
		if n != 1 {
			add("%s: exactly one of from, regex, delete_line_matching is required", p)
		}
		for _, re := range []string{r.Regex, r.DeleteLineMatching} {
			if re != "" {
				if _, err := regexp.Compile(re); err != nil {
					add("%s: invalid regex: %v", p, err)
				}
			}
		}
	}
}

func validateCommands(path string, cmds []Command, add func(string, ...any)) {
	for i, c := range cmds {
		if strings.TrimSpace(c.Cmd) == "" {
			add("%s[%d]: empty command", path, i)
		}
	}
}

// Expand resolves pipeline names to an ordered list of "stage" or "stage.step"
// references. Entries are not de-duplicated: `run api api` runs api twice, like
// calling a task twice.
func (c *Config) Expand(names []string) ([]string, error) {
	var out []string
	var walk func(n string, stack []string) error
	walk = func(n string, stack []string) error {
		if IsRef(n) {
			out = append(out, n)
			return nil
		}
		steps, ok := c.Pipelines[n]
		if !ok {
			return fmt.Errorf("unknown stage, step or pipeline %q", n)
		}
		for _, s := range stack {
			if s == n {
				return fmt.Errorf("pipeline cycle: %s -> %s", strings.Join(stack, " -> "), n)
			}
		}
		for _, s := range steps {
			if err := walk(s, append(stack, n)); err != nil {
				return err
			}
		}
		return nil
	}
	for _, n := range names {
		if err := walk(n, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// StageEnabled reports whether a stage section is configured.
func (c *Config) StageEnabled(name string) bool {
	switch name {
	case "migration":
		return c.Migration != nil
	case "typespec":
		return c.TypeSpec != nil
	case "openapi":
		return c.OpenAPI != nil
	case "proto":
		return c.Proto != nil
	}
	return false
}

// Abs resolves a config path (forward slashes allowed) against base, which is
// itself resolved against the config root.
func (c *Config) Abs(base, p string) string {
	p = filepath.FromSlash(p)
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	b := filepath.FromSlash(base)
	if !filepath.IsAbs(b) {
		b = filepath.Join(c.Root, b)
	}
	return filepath.Join(b, p)
}

// Rel returns p relative to the config root, with forward slashes, for messages.
func (c *Config) Rel(p string) string {
	r, err := filepath.Rel(c.Root, p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(r)
}

// Marshal renders the resolved config as YAML.
func (c *Config) Marshal() ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
