package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name, yaml, want string
	}{
		{"unknown top key", "version: 1\nbogus: 1\n", "line 2: field bogus not found"},
		{"unknown nested key", "version: 1\nproto:\n  dir: p\n  nope: x\n", "line 4: field nope not found"},
		{"unknown command key", "version: 1\nproto:\n  run_after:\n    - {cmd: x, sh: true}\n", "line 4: field sh not found"},
		{"bad version", "version: 2\n", "unsupported version 2"},
		{"bad require_docs", "version: 1\ntypespec:\n  from_drizzle:\n    require_docs: maybe\n", "require_docs: must be warn, error or off"},
		{"bad proto package", "version: 1\nproto:\n  proto_package: V1\n", "proto.proto_package"},
		{"sync without to", "version: 1\nmigration:\n  sync: {}\n", "migration.sync.to: required"},
		{"rule with two kinds", "version: 1\nproto:\n  extra_replace:\n    - {glob: '*', from: a, regex: b}\n", "exactly one of"},
		{"rule bad regex", "version: 1\nproto:\n  extra_replace:\n    - {glob: '*', regex: '('}\n", "invalid regex"},
		{"rule missing glob", "version: 1\nproto:\n  extra_replace:\n    - {from: a}\n", "glob: required"},
		{"pipeline unknown", "version: 1\npipelines:\n  x: [nope]\n", `unknown stage or pipeline "nope"`},
		{"pipeline cycle", "version: 1\npipelines:\n  a: [b]\n  b: [a]\n", "pipeline cycle"},
		{"pipeline named like stage", "version: 1\npipelines:\n  proto: [api]\n", "collides with a built-in stage"},
		{"empty command", "version: 1\nproto:\n  run_after: ['  ']\n", "empty command"},
		{"bad namespace", "version: 1\ntypespec:\n  from_drizzle:\n    namespace: '1abc'\n", "invalid TypeSpec namespace"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml), "/repo")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestDefaults(t *testing.T) {
	c, err := Parse([]byte("version: 1\nmigration:\n  docs: {}\n  sync: {to: ../x}\ntypespec:\n  from_drizzle: {}\nopenapi:\nproto:\n"), "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if c.Migration.Dir != "migration" || c.Migration.Docs.Out != "schema.dbml" || c.Migration.Sync.Suffix != ".up.sql" {
		t.Errorf("migration defaults: %+v", c.Migration)
	}
	if f := c.TypeSpec.FromDrizzle; f.Namespace != "DbSchema" || f.RequireDocs != "warn" || !reflect.DeepEqual(f.ReadOnlyColumns, []string{"id", "created_at", "updated_at"}) {
		t.Errorf("from_drizzle defaults: %+v", f)
	}
	if c.OpenAPI == nil || c.OpenAPI.Output != "openapi.json" {
		t.Errorf("empty `openapi:` should enable the stage with defaults")
	}
	if p := c.Proto; p.ProtoPackage != "v1" || p.AnyType != "google.protobuf.Any" || p.WellKnown["DateTime"] != "google.protobuf.Timestamp" {
		t.Errorf("proto defaults: %+v", p)
	}
}

func TestCommandForms(t *testing.T) {
	c, err := Parse([]byte("version: 1\nproto:\n  run_after:\n    - buf lint\n    - {cmd: 'echo hi > x', shell: true}\n"), "/repo")
	if err != nil {
		t.Fatal(err)
	}
	want := []Command{{Cmd: "buf lint"}, {Cmd: "echo hi > x", Shell: true}}
	if !reflect.DeepEqual(c.Proto.RunAfter, want) {
		t.Fatalf("got %+v", c.Proto.RunAfter)
	}
}

func TestExpand(t *testing.T) {
	c, err := Parse([]byte("version: 1\npipelines:\n  docs: [migration]\n"), "/repo")
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Expand([]string{"all", "typespec", "docs"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"migration", "typespec", "openapi", "proto", "typespec", "migration"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestAbsAndRel(t *testing.T) {
	root := filepath.FromSlash("/repo")
	c := &Config{Root: root}
	if got := c.Abs("openapi", "../migration/src/schema"); got != filepath.FromSlash("/repo/migration/src/schema") {
		t.Errorf("Abs: %s", got)
	}
	if got := c.Rel(filepath.FromSlash("/repo/a/b")); got != "a/b" {
		t.Errorf("Rel: %s", got)
	}
}

func TestSampleConfig(t *testing.T) {
	if _, err := Load("../../testdata/sample/protoforge.yaml"); err != nil {
		t.Fatal(err)
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	c, err := Parse([]byte("version: 1\nproto:\n  run_after: [x]\n"), "/repo")
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(b, "/repo"); err != nil {
		t.Fatalf("printed config does not parse: %v\n%s", err, b)
	}
}
