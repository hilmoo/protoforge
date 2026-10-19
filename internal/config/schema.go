package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// SchemaID is the canonical URL of the published schema.
const SchemaID = "https://raw.githubusercontent.com/hilmoo/protoforge/main/schema/protoforge.schema.json"

// JSONSchema generates the JSON Schema (draft-07, the dialect the VS Code YAML
// extension supports best) for protoforge.yaml from the Config struct tags, so
// the schema cannot drift from what the loader accepts.
func JSONSchema() ([]byte, error) {
	root := structSchema(reflect.TypeOf(Config{}))
	out := omap{}
	out.set("$schema", "http://json-schema.org/draft-07/schema#")
	out.set("$id", SchemaID)
	out.set("title", "protoforge.yaml")
	out.set("description", "Configuration for protoforge: Drizzle -> TypeSpec -> OpenAPI -> .proto. See docs/config.md.")
	for _, k := range root.keys {
		out.set(k, root.vals[k])
	}
	defs := omap{}
	defs.set("command", commandSchema())
	defs.set("replaceRule", structSchema(reflect.TypeOf(ReplaceRule{})))
	out.set("definitions", defs)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var (
	commandType = reflect.TypeOf(Command{})
	ruleType    = reflect.TypeOf(ReplaceRule{})
)

func structSchema(t reflect.Type) omap {
	s := omap{}
	s.set("type", "object")
	props := omap{}
	var required []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if name == "-" || name == "" {
			continue
		}
		props.set(name, fieldSchema(f))
		if f.Tag.Get("required") == "true" {
			required = append(required, name)
		}
	}
	s.set("properties", props)
	if len(required) > 0 {
		s.set("required", required)
	}
	s.set("additionalProperties", false)
	if t == ruleType {
		// Exactly one of from / regex / delete_line_matching.
		var one []any
		for _, k := range []string{"from", "regex", "delete_line_matching"} {
			one = append(one, omap{}.with("required", []string{k}))
		}
		s.set("oneOf", one)
	}
	return s
}

func fieldSchema(f reflect.StructField) omap {
	s := typeSchema(f.Type)
	if f.Tag.Get("nullable") == "true" {
		// `proto:` with no value enables the stage with defaults.
		s = omap{}.with("anyOf", []any{s, omap{}.with("type", "null")})
	}
	if d := f.Tag.Get("doc"); d != "" {
		s.set("description", d)
	}
	if d := f.Tag.Get("default"); d != "" {
		s.set("default", mustJSON(d, f.Name, "default"))
	}
	if e := f.Tag.Get("enum"); e != "" {
		s.set("enum", mustJSON("["+e+"]", f.Name, "enum"))
	}
	if e := f.Tag.Get("examples"); e != "" {
		s.set("examples", mustJSON(e, f.Name, "examples"))
	}
	if p := f.Tag.Get("pattern"); p != "" {
		s.set("pattern", p)
	}
	if p := f.Tag.Get("keypattern"); p != "" {
		s.set("propertyNames", omap{}.with("pattern", p))
	}
	if f.Tag.Get("stageref") == "true" {
		s.set("propertyNames", omap{}.with("not", omap{}.with("enum", StageNames)))
		s.set("examples", []any{map[string][]string{"api": {"typespec", "openapi", "proto"}}})
	}
	return s
}

func typeSchema(t reflect.Type) omap {
	switch {
	case t == commandType:
		return omap{}.with("$ref", "#/definitions/command")
	case t == ruleType:
		return omap{}.with("$ref", "#/definitions/replaceRule")
	}
	switch t.Kind() {
	case reflect.Pointer:
		return typeSchema(t.Elem())
	case reflect.Struct:
		return structSchema(t)
	case reflect.String:
		return omap{}.with("type", "string")
	case reflect.Bool:
		return omap{}.with("type", "boolean")
	case reflect.Int:
		return omap{}.with("type", "integer")
	case reflect.Slice:
		return omap{}.with("type", "array").with("items", typeSchema(t.Elem()))
	case reflect.Map:
		return omap{}.with("type", "object").with("additionalProperties", typeSchema(t.Elem()))
	}
	panic(fmt.Sprintf("config schema: unsupported type %s", t))
}

func commandSchema() omap {
	obj := structSchema(commandType)
	obj.set("required", []string{"cmd"})
	return omap{}.
		with("description", "A command: a plain string (split into argv, run without a shell) or {cmd, shell}.").
		with("oneOf", []any{
			omap{}.with("type", "string").with("minLength", 1),
			obj,
		})
}

func mustJSON(s, field, tag string) any {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		panic(fmt.Sprintf("config schema: field %s: bad %s tag %q: %v", field, tag, s, err))
	}
	return v
}

// omap is a JSON object that keeps insertion order, so the schema reads in the
// same order as the struct fields.
type omap struct {
	keys []string
	vals map[string]any
}

func (o *omap) set(k string, v any) {
	if o.vals == nil {
		o.vals = map[string]any{}
	}
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

func (o omap) with(k string, v any) omap {
	o.set(k, v)
	return o
}

func (o omap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		var vb bytes.Buffer
		enc := json.NewEncoder(&vb)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(o.vals[k]); err != nil {
			return nil, err
		}
		buf.Write(bytes.TrimRight(vb.Bytes(), "\n"))
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
