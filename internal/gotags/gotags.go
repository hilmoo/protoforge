// Package gotags is the port of transform_openapi (the former gist). It injects
// `x-oapi-codegen-extra-tags` (validate / jsonschema struct tags) into OpenAPI 3.0
// schema properties and operation parameters.
//
// Every function mirrors the original function of the same name; the output is
// byte-identical to `json.dump(spec, fh, indent=2, ensure_ascii=False)`.
package gotags

import (
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"strings"
	"unicode"
)

const extraTagsKey = "x-oapi-codegen-extra-tags"

// presenceKey marks a required numeric property that must be generated as proto3
// `optional` (a Go pointer), so `validate:"required"` rejects a missing value but accepts 0.
const presenceKey = "x-protoforge-presence"

// formatParamMap mirrors FORMAT_PARAM_MAP ("date-time" is deliberately absent).
var formatParamMap = map[string]string{
	"date": "datetime=2006-01-02",
	"time": "datetime=15:04:05",
}

const schemaRefPrefix = "#/components/schemas/"

var wrapperOverrideKeys = []string{"description", "example", "x-mcp", "x-validator"}

var resolvableTypes = []string{"string", "integer", "number"}

// Transformer holds the warning sink.
type Transformer struct {
	Warn io.Writer // receives the `[WARN]` lines (stderr in the original)
	err  error
}

// TransformFile reads in, transforms it and writes out (which may be the same path).
func TransformFile(in, out string, warn io.Writer) error {
	data, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	res, err := TransformBytes(data, warn)
	if err != nil {
		return fmt.Errorf("%s: %w", in, err)
	}
	return os.WriteFile(out, res, 0o644)
}

// TransformBytes transforms an OpenAPI JSON document.
func TransformBytes(data []byte, warn io.Writer) ([]byte, error) {
	if warn == nil {
		warn = io.Discard
	}
	spec, err := Parse(data)
	if err != nil {
		return nil, err
	}
	t := &Transformer{Warn: warn}
	if err := t.Transform(spec); err != nil {
		return nil, err
	}
	return Marshal(spec), nil
}

// Transform mutates spec in place (transform()).
func (t *Transformer) Transform(spec any) error {
	root, ok := spec.(*Object)
	if !ok {
		return errors.New("OpenAPI document is not a JSON object")
	}
	schemas := asObject(get(asObject(get(root, "components")), "schemas"))
	if schemas != nil {
		for _, name := range schemas.Keys() {
			if s, ok := mustGet(schemas, name).(*Object); ok {
				t.injectExtraTagsIntoSchema(s, schemas)
			}
		}
	}
	t.transformPaths(root)
	return t.err
}

// ---------------------------------------------------------------------------
// small dict helpers (original dict semantics)
// ---------------------------------------------------------------------------

func get(o *Object, k string) any {
	if o == nil {
		return nil
	}
	v, _ := o.Get(k)
	return v
}

func mustGet(o *Object, k string) any { v, _ := o.Get(k); return v }

func asObject(v any) *Object {
	o, _ := v.(*Object)
	return o
}

// getStr is `d.get(k, "")` for values compared against strings.
func getStr(o *Object, k string) string {
	s, _ := get(o, k).(string)
	return s
}

func has(o *Object, k string) bool { return o != nil && o.Has(k) }

func isTrue(v any) bool { b, ok := v.(bool); return ok && b }

func inList(s string, list []string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// pyStrip mirrors str.strip() with no arguments.
func pyStrip(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
	})
}

// numTag renders the value of minimum/maximum, converting integral floats for integer types.
func numTag(val any, fieldType string) string {
	if f, ok := val.(Float); ok && fieldType == "integer" && !math.IsInf(float64(f), 0) && float64(f) == math.Trunc(float64(f)) {
		i, _ := big.NewFloat(float64(f)).Int(nil)
		return i.String()
	}
	return PyStr(val)
}

func enumValues(v any) string {
	arr, ok := v.([]any)
	if !ok {
		if s, ok := v.(string); ok { // iterating a str yields characters
			var parts []string
			for _, r := range s {
				parts = append(parts, string(r))
			}
			return strings.Join(parts, " ")
		}
		if o, ok := v.(*Object); ok { // iterating a dict yields keys
			return strings.Join(o.Keys(), " ")
		}
		return ""
	}
	var parts []string
	for _, e := range arr {
		if e != nil {
			parts = append(parts, PyStr(e))
		}
	}
	return strings.Join(parts, " ")
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func itemValidateTags(items *Object) []string {
	var parts []string
	itemType := getStr(items, "type")
	fmtV := getStr(items, "format")

	switch itemType {
	case "string":
		if has(items, "minLength") {
			parts = append(parts, "min="+PyStr(get(items, "minLength")))
		}
		if has(items, "maxLength") {
			parts = append(parts, "max="+PyStr(get(items, "maxLength")))
		}
		if fmtV == "date-time" {
			// Skip tag injection if format is already date-time
		} else if m, ok := formatParamMap[fmtV]; ok {
			parts = append(parts, m)
		} else if fmtV != "" {
			parts = append(parts, fmtV)
		}
		if has(items, "enum") {
			if values := enumValues(get(items, "enum")); values != "" {
				parts = append(parts, "oneof="+values)
			}
		}
	case "integer", "number":
		exclMin := truthy(get(items, "exclusiveMinimum"))
		exclMax := truthy(get(items, "exclusiveMaximum"))
		if has(items, "minimum") {
			tag := "gte"
			if exclMin {
				tag = "gt"
			}
			parts = append(parts, tag+"="+numTag(get(items, "minimum"), itemType))
		}
		if has(items, "maximum") {
			tag := "lte"
			if exclMax {
				tag = "lt"
			}
			parts = append(parts, tag+"="+numTag(get(items, "maximum"), itemType))
		}
		if fmtV != "" {
			parts = append(parts, fmtV)
		}
		if has(items, "enum") {
			if values := enumValues(get(items, "enum")); values != "" {
				parts = append(parts, "oneof="+values)
			}
		}
	case "array":
		if has(items, "minItems") {
			parts = append(parts, "min="+PyStr(get(items, "minItems")))
		}
		if has(items, "maxItems") {
			parts = append(parts, "max="+PyStr(get(items, "maxItems")))
		}
		if truthy(get(items, "uniqueItems")) {
			parts = append(parts, "unique")
		}
		nested := NewObject()
		if v, ok := items.Get("items"); ok {
			nested, _ = v.(*Object)
		}
		if nested != nil && !nested.Has("$ref") {
			if nestedTags := itemValidateTags(nested); len(nestedTags) > 0 {
				parts = append(parts, "dive")
				parts = append(parts, nestedTags...)
			}
		}
	case "object":
		if has(items, "minProperties") {
			parts = append(parts, "min="+PyStr(get(items, "minProperties")))
		}
		if has(items, "maxProperties") {
			parts = append(parts, "max="+PyStr(get(items, "maxProperties")))
		}
	}
	return parts
}

// ---------------------------------------------------------------------------
// Validate-tag & Extra-tag building
// ---------------------------------------------------------------------------

func (t *Transformer) buildValidateTag(fieldName string, prop *Object, required map[string]bool, skipStringEnum bool) string {
	var parts []string
	isRequired := required[fieldName]
	fieldType := getStr(prop, "type")
	fmtV := getStr(prop, "format")

	if isRequired {
		if fieldType == "boolean" && !truthy(get(prop, "nullable")) {
			// `required` rejects the zero value, which for a plain `bool` is `false`.
		} else {
			parts = append(parts, "required")
		}
	} else {
		parts = append(parts, "omitempty")
	}

	switch fieldType {
	case "string":
		if has(prop, "minLength") {
			parts = append(parts, "min="+PyStr(get(prop, "minLength")))
		}
		if has(prop, "maxLength") {
			parts = append(parts, "max="+PyStr(get(prop, "maxLength")))
		}
		if fmtV == "date-time" {
			// Skip tag injection if format is already date-time
		} else if m, ok := formatParamMap[fmtV]; ok {
			parts = append(parts, m)
		} else if fmtV != "" {
			parts = append(parts, fmtV)
		}
		if has(prop, "pattern") {
			fmt.Fprintf(t.Warn, "  [WARN] schema/field '%s' has 'pattern' constraint which cannot be "+
				"expressed as a validator tag. Use 'x-validator' to add a custom tag.\n", fieldName)
		}
		if has(prop, "enum") && !skipStringEnum {
			if values := enumValues(get(prop, "enum")); values != "" {
				parts = append(parts, "oneof="+values)
			}
		}
	case "integer", "number":
		exclMin := truthy(get(prop, "exclusiveMinimum"))
		exclMax := truthy(get(prop, "exclusiveMaximum"))
		if has(prop, "minimum") {
			tag := "gte"
			if exclMin {
				tag = "gt"
			}
			parts = append(parts, tag+"="+numTag(get(prop, "minimum"), fieldType))
		}
		if has(prop, "maximum") {
			tag := "lte"
			if exclMax {
				tag = "lt"
			}
			parts = append(parts, tag+"="+numTag(get(prop, "maximum"), fieldType))
		}
		if fmtV != "" {
			parts = append(parts, fmtV)
		}
		if has(prop, "enum") {
			if values := enumValues(get(prop, "enum")); values != "" {
				parts = append(parts, "oneof="+values)
			}
		}
	case "array":
		if has(prop, "minItems") {
			parts = append(parts, "min="+PyStr(get(prop, "minItems")))
		}
		if has(prop, "maxItems") {
			parts = append(parts, "max="+PyStr(get(prop, "maxItems")))
		}
		if truthy(get(prop, "uniqueItems")) {
			parts = append(parts, "unique")
		}
		items := NewObject()
		if v, ok := prop.Get("items"); ok {
			items, _ = v.(*Object)
		}
		if items != nil && !items.Has("$ref") {
			if itemTags := itemValidateTags(items); len(itemTags) > 0 {
				parts = append(parts, "dive")
				parts = append(parts, itemTags...)
			}
		}
	case "object":
		if has(prop, "minProperties") {
			parts = append(parts, "min="+PyStr(get(prop, "minProperties")))
		}
		if has(prop, "maxProperties") {
			parts = append(parts, "max="+PyStr(get(prop, "maxProperties")))
		}
	}

	if v, ok := prop.Get("x-validator"); ok {
		s, isStr := v.(string)
		if !isStr {
			// The original would crash on .strip(); surface it as an error.
			if t.err == nil {
				t.err = fmt.Errorf("field %q: x-validator must be a string", fieldName)
			}
		} else if xv := pyStrip(s); xv != "" {
			parts = append(parts, xv)
		}
	}
	return strings.Join(parts, ",")
}

// tags is an ordered {validate, jsonschema} pair (the original inserts validate first).
type tags struct {
	validate, jsonschema       string
	hasValidate, hasJSONSchema bool
}

func (t tags) empty() bool { return !t.hasValidate && !t.hasJSONSchema }

func (t *Transformer) buildExtraTags(fieldName string, prop *Object, required map[string]bool, parentInject, skipStringEnum bool) tags {
	var out tags
	if v := t.buildValidateTag(fieldName, prop, required, skipStringEnum); v != "" {
		out.validate, out.hasValidate = v, true
	}
	doInject := parentInject || isTrue(get(prop, "x-mcp"))
	if !doInject {
		return out
	}
	desc := ""
	if v, ok := prop.Get("description"); ok {
		s, _ := v.(string)
		desc = pyStrip(s)
	}
	desc = strings.ReplaceAll(strings.ReplaceAll(desc, "\n", " "), "\r", "")

	exampleStr := ""
	if ex, ok := prop.Get("example"); ok {
		if s, ok := ex.(string); ok {
			exampleStr = s
		} else {
			exampleStr = Dumps(ex)
		}
		exampleStr = strings.ReplaceAll(exampleStr, "\r", `\r`)
		exampleStr = strings.ReplaceAll(exampleStr, "\n", `\n`)
		exampleStr = strings.ReplaceAll(exampleStr, `"`, "'")
		exampleStr = strings.ReplaceAll(exampleStr, `\'`, "'")
	}
	switch {
	case desc != "" && exampleStr != "":
		if !strings.HasSuffix(desc, ".") {
			desc += "."
		}
		out.jsonschema, out.hasJSONSchema = desc+" Example: "+exampleStr, true
	case desc != "":
		out.jsonschema, out.hasJSONSchema = desc, true
	case exampleStr != "":
		out.jsonschema, out.hasJSONSchema = "Example: "+exampleStr, true
	}
	return out
}

// ---------------------------------------------------------------------------
// $ref wrapper resolution
// ---------------------------------------------------------------------------

func resolveRefWrapper(prop, schemas *Object) *Object {
	ref, hasRef := prop.Get("$ref")
	if !hasRef || ref == nil {
		ref = nil
		for _, kw := range []string{"allOf", "oneOf", "anyOf"} {
			entries, _ := get(prop, kw).([]any)
			if len(entries) == 1 {
				if e, ok := entries[0].(*Object); ok && e.Has("$ref") {
					ref = mustGet(e, "$ref")
					break
				}
			}
		}
	}
	s, ok := ref.(string)
	if !ok || !strings.HasPrefix(s, schemaRefPrefix) {
		return nil
	}
	return asObject(get(schemas, s[len(schemaRefPrefix):]))
}

func (t *Transformer) buildWrapperTags(fieldName string, prop *Object, required map[string]bool, parentInject bool, schemas *Object) tags {
	resolved := resolveRefWrapper(prop, schemas)
	if resolved == nil || resolved.Len() == 0 {
		return tags{}
	}
	typ, isStr := get(resolved, "type").(string)
	if !isStr || !inList(typ, resolvableTypes) {
		return tags{}
	}
	merged := NewObject()
	for _, k := range resolved.Keys() {
		if k != extraTagsKey {
			merged.Set(k, mustGet(resolved, k))
		}
	}
	for _, k := range prop.Keys() {
		if inList(k, wrapperOverrideKeys) {
			merged.Set(k, mustGet(prop, k))
		}
	}
	return t.buildExtraTags(fieldName, merged, required, parentInject, true)
}

// ---------------------------------------------------------------------------
// Extra-tags injection
// ---------------------------------------------------------------------------

func isInlineProperty(prop *Object) bool {
	if prop.Has("$ref") {
		return false
	}
	for _, kw := range []string{"allOf", "oneOf", "anyOf"} {
		v, ok := prop.Get(kw)
		if !ok || !truthy(v) {
			continue
		}
		arr, isArr := v.([]any)
		if !isArr {
			// Iterating a non-empty str/dict yields no dicts, so all() is True.
			return false
		}
		all := true
		for _, e := range arr {
			if o, ok := e.(*Object); ok && !o.Has("$ref") {
				all = false
				break
			}
		}
		if all {
			return false
		}
	}
	return true
}

func mergeTags(target *Object, extra tags) {
	if extra.empty() {
		return
	}
	merged := NewObject()
	var existing *Object
	if v, ok := target.Get(extraTagsKey); ok {
		existing, _ = v.(*Object)
		if existing != nil {
			merged = existing.Copy()
		}
	}
	if existing != nil && existing.Has("validate") && extra.hasValidate {
		ev, _ := mustGet(existing, "validate").(string)
		existingParts := strings.Split(ev, ",")
		seen := map[string]bool{}
		for _, p := range existingParts {
			seen[p] = true
		}
		combined := append([]string{}, existingParts...)
		for _, p := range strings.Split(extra.validate, ",") {
			if !seen[p] {
				seen[p] = true
				combined = append(combined, p)
			}
		}
		merged.Set("validate", strings.Join(combined, ","))
	} else if extra.hasValidate {
		merged.Set("validate", extra.validate)
	}
	if extra.hasJSONSchema {
		merged.Set("jsonschema", extra.jsonschema)
	}
	target.Set(extraTagsKey, merged)
}

func requiredSet(v any) map[string]bool {
	out := map[string]bool{}
	if arr, ok := v.([]any); ok {
		for _, e := range arr {
			if s, ok := e.(string); ok {
				out[s] = true
			}
		}
	}
	return out
}

func (t *Transformer) injectExtraTagsIntoSchema(schema, schemas *Object) {
	schemaLevelInject := isTrue(get(schema, "x-mcp"))

	if schema.Has("properties") || schema.Has("allOf") {
		topRequired := requiredSet(get(schema, "required"))
		t.injectIntoProperties(asObject(get(schema, "properties")), topRequired, schemaLevelInject, schemas)

		subs, _ := get(schema, "allOf").([]any)
		for _, s := range subs {
			sub, ok := s.(*Object)
			if !ok || sub.Has("$ref") {
				continue
			}
			subRequired := map[string]bool{}
			for k := range topRequired {
				subRequired[k] = true
			}
			for k := range requiredSet(get(sub, "required")) {
				subRequired[k] = true
			}
			t.injectIntoProperties(asObject(get(sub, "properties")), subRequired, schemaLevelInject, schemas)
		}
	} else if typ, _ := get(schema, "type").(string); inList(typ, []string{"string", "integer", "number", "array"}) {
		if extra := t.buildExtraTags("", schema, map[string]bool{}, schemaLevelInject, true); !extra.empty() {
			mergeTags(schema, extra)
		}
	}
}

func (t *Transformer) injectIntoProperties(properties *Object, required map[string]bool, parentInject bool, schemas *Object) {
	if properties == nil {
		return
	}
	for _, fieldName := range properties.Keys() {
		prop, ok := mustGet(properties, fieldName).(*Object)
		if !ok {
			continue
		}
		var extra tags
		if isInlineProperty(prop) {
			extra = t.buildExtraTags(fieldName, prop, required, parentInject, false)
		} else {
			extra = t.buildWrapperTags(fieldName, prop, required, parentInject, schemas)
		}
		if !extra.empty() {
			mergeTags(prop, extra)
		}
		if required[fieldName] && isNumeric(prop, schemas) {
			prop.Set(presenceKey, true)
		}
	}
}

// isNumeric reports whether the property is an integer/number, directly or through a $ref wrapper.
func isNumeric(prop, schemas *Object) bool {
	if isInlineProperty(prop) {
		return inList(getStr(prop, "type"), []string{"integer", "number"})
	}
	return inList(getStr(resolveRefWrapper(prop, schemas), "type"), []string{"integer", "number"})
}

// ---------------------------------------------------------------------------
// Path Parameter Injection
// ---------------------------------------------------------------------------

func (t *Transformer) transformPaths(spec *Object) {
	paths := asObject(get(spec, "paths"))
	if paths == nil {
		return
	}
	for _, url := range paths.Keys() {
		pathItem, ok := mustGet(paths, url).(*Object)
		if !ok {
			continue
		}
		t.processParameters(get(pathItem, "parameters"))
		for _, method := range pathItem.Keys() {
			op, ok := mustGet(pathItem, method).(*Object)
			if strings.HasPrefix(method, "x-") || !ok {
				continue
			}
			t.processParameters(get(op, "parameters"))
		}
	}
}

func (t *Transformer) processParameters(v any) {
	params, ok := v.([]any)
	if !ok {
		return
	}
	for _, p := range params {
		param, ok := p.(*Object)
		if !ok || param.Has("$ref") {
			continue
		}
		paramName := getStr(param, "name")
		schema, ok := get(param, "schema").(*Object)
		if !ok || schema.Len() == 0 {
			continue
		}
		required := map[string]bool{}
		if truthy(get(param, "required")) {
			required[paramName] = true
		}
		paramInject := isTrue(get(param, "x-mcp"))
		if extra := t.buildExtraTags(paramName, schema, required, paramInject, false); !extra.empty() {
			mergeTags(param, extra)
		}
	}
}
