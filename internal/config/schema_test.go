package config

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

const schemaFile = "../../schema/protoforge.schema.json"

// TestSchemaUpToDate: the checked-in schema must match the generator.
func TestSchemaUpToDate(t *testing.T) {
	got, err := JSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(schemaFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("schema/protoforge.schema.json is stale; run: go run ./cmd/protoforge config schema -o schema/protoforge.schema.json")
	}
}

// TestEveryFieldDocumented keeps editor hovers complete.
func TestEveryFieldDocumented(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type)
	walk = func(rt reflect.Type) {
		for rt.Kind() == reflect.Pointer || rt.Kind() == reflect.Slice || rt.Kind() == reflect.Map {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] {
			return
		}
		seen[rt] = true
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if f.Tag.Get("yaml") == "-" {
				continue
			}
			if f.Tag.Get("doc") == "" {
				t.Errorf("%s.%s has no doc tag", rt.Name(), f.Name)
			}
			walk(f.Type)
		}
	}
	walk(reflect.TypeOf(Config{}))
}

func compileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	b, err := JSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(SchemaID, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(SchemaID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// yamlToJSON converts YAML into the JSON value model the validator expects.
func yamlToJSON(t *testing.T, src string) any {
	t.Helper()
	var v any
	if err := yaml.Unmarshal([]byte(src), &v); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	out, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestSchemaAcceptsValid: everything the loader accepts, the schema accepts.
func TestSchemaAcceptsValid(t *testing.T) {
	s := compileSchema(t)
	full, err := os.ReadFile("../../testdata/fixtures/config/full.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"full":           string(full),
		"minimal":        "version: 1\n",
		"empty sections": "version: 1\nmigration:\ntypespec:\nopenapi:\nproto:\n",
		"docs defaults":  "version: 1\nmigration:\n  docs: {}\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(src), "/repo"); err != nil {
				t.Fatalf("loader rejects it: %v", err)
			}
			if err := s.Validate(yamlToJSON(t, src)); err != nil {
				t.Fatalf("schema rejects it: %v", err)
			}
		})
	}
	// The resolved config printed by `config print` validates too.
	c, _ := Parse(full, "/repo")
	b, _ := c.Marshal()
	if err := s.Validate(yamlToJSON(t, string(b))); err != nil {
		t.Fatalf("schema rejects `config print` output: %v", err)
	}
}

// TestSchemaRejectsInvalid: the editor flags what the loader rejects.
func TestSchemaRejectsInvalid(t *testing.T) {
	s := compileSchema(t)
	cases := map[string]string{
		"unknown top key":     "version: 1\nbogus: 1\n",
		"unknown nested key":  "version: 1\nproto:\n  nope: x\n",
		"bad version":         "version: 2\n",
		"bad require_docs":    "version: 1\ntypespec:\n  from_drizzle:\n    require_docs: maybe\n",
		"bad proto package":   "version: 1\nproto:\n  proto_package: V1\n",
		"sync without to":     "version: 1\nmigration:\n  sync: {}\n",
		"rule with two kinds": "version: 1\nproto:\n  extra_replace:\n    - {glob: '*', from: a, regex: b}\n",
		"rule with no kind":   "version: 1\nproto:\n  extra_replace:\n    - {glob: '*', to: b}\n",
		"rule missing glob":   "version: 1\nproto:\n  extra_replace:\n    - {from: a}\n",
		"pipeline like stage": "version: 1\npipelines:\n  proto: [api]\n",
		"bad command key":     "version: 1\nproto:\n  run_after:\n    - {cmd: x, sh: true}\n",
		"wrong type":          "version: 1\nopenapi:\n  format: yes please\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			err := s.Validate(yamlToJSON(t, src))
			if err == nil {
				t.Fatal("schema accepted it")
			}
			if strings.TrimSpace(err.Error()) == "" {
				t.Fatal("empty error")
			}
		})
	}
}
