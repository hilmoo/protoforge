package stages

import (
	"regexp"
	"sort"
	"strings"

	"github.com/hilmoo/protoforge/internal/textops"
)

// NormOptions drives the built-in proto normalisations (the old in-place line-editing chain).
type NormOptions struct {
	Version         string   // models proto_package, e.g. "v1"
	Spec            string   // spec name: package services.<spec>.<v>; "" gives services.<v>
	ServicesVersion string   // version segment of the services package; "" means Version
	GoPackageRoot   string   // e.g. "proto"
	AnyType         string   // e.g. "google.protobuf.Any"; "" disables the AnyType rewrite
	DropImports     []string // import stems (prefixes) to delete
}

// goPkgSuffix turns "v1" into the Go package suffix used in go_package ("modelsv1").
func goPkgSuffix(v string) string {
	return regexp.MustCompile(`[^a-z0-9]`).ReplaceAllString(v, "")
}

// dropImport reports whether line imports one of the dropped stems. It matches
// both `import "models/<ver>/x...` (models pass, after rewriting) and
// `import public "models/x...` (services pass, before rewriting), like the
// `/.../d` line deletions it replaces.
func dropImport(line, ver string, drops []string) bool {
	for _, d := range drops {
		for _, p := range []string{
			`import "models/` + ver + `/` + d,
			`import "models/` + d,
			`import public "models/` + d,
			`import public "models/` + ver + `/` + d,
		} {
			if strings.Contains(line, p) {
				return true
			}
		}
	}
	return false
}

// NormalizeModel reproduces, in order, the `gen-from-openapi` line edits:
//
//	s|import "models/|import "models/v1/|g
//	s/package openapitools;/package models.v1;/g
//	s|import "models/v1/any_type.proto";|import "google/protobuf/any.proto";|g
//	s/AnyType/google.protobuf.Any/g
//	/import "models\/v1\/todo_object_mapping.proto";/d
//	s|package models.v1;|package models.v1;\noption go_package = "proto/models/v1;modelsv1";|g
func NormalizeModel(s string, o NormOptions) string {
	v := o.Version
	s = strings.ReplaceAll(s, `import "models/`, `import "models/`+v+`/`)
	s = strings.ReplaceAll(s, "package openapitools;", "package models."+v+";")
	if o.AnyType != "" {
		if imp, ok := wktImport(o.AnyType); ok {
			s = strings.ReplaceAll(s, `import "models/`+v+`/any_type.proto";`, `import "`+imp+`";`)
		}
		s = strings.ReplaceAll(s, "AnyType", o.AnyType)
	}
	s = textops.DeleteLines(s, func(l string) bool { return dropImport(l, v, o.DropImports) })
	s = strings.ReplaceAll(s, "package models."+v+";",
		"package models."+v+";\noption go_package = \""+o.GoPackageRoot+"/models/"+v+";models"+goPkgSuffix(v)+"\";")
	return s
}

var servicesPkgRe = regexp.MustCompile(`^package openapitools\.services\..*;`)

// NormalizeService reproduces the `gen-services-from-openapi` line edits, with
// the spec name in the package (here spec "core"):
//
//	/import public "models\/common_error_error/d
//	s|import public "models/|import "models/v1/|g
//	s/^package openapitools\.services\..*;/package services.core.v1;\noption go_package = "proto\/services\/core\/v1;corev1";/
func NormalizeService(s string, o NormOptions) string {
	v, sv := o.Version, o.ServicesVersion
	if sv == "" {
		sv = v
	}
	s = textops.DeleteLines(s, func(l string) bool { return dropImport(l, v, o.DropImports) })
	s = strings.ReplaceAll(s, `import public "models/`, `import "models/`+v+`/`)
	pkg, dir, goName := "services.", "services/", "services"
	if o.Spec != "" {
		pkg, dir, goName = pkg+o.Spec+".", dir+o.Spec+"/", o.Spec
	}
	repl := "package " + pkg + sv + ";\noption go_package = \"" + o.GoPackageRoot + "/" + dir + sv + ";" + goPkgSuffix(goName+sv) + "\";"
	return textops.ReplaceLines(s, servicesPkgRe, strings.ReplaceAll(repl, "$", "$$"))
}

// wellKnownImports maps google.protobuf types to their files.
var wellKnownImports = map[string]string{
	"google.protobuf.Any":         "google/protobuf/any.proto",
	"google.protobuf.Timestamp":   "google/protobuf/timestamp.proto",
	"google.protobuf.Duration":    "google/protobuf/duration.proto",
	"google.protobuf.Struct":      "google/protobuf/struct.proto",
	"google.protobuf.Value":       "google/protobuf/struct.proto",
	"google.protobuf.ListValue":   "google/protobuf/struct.proto",
	"google.protobuf.Empty":       "google/protobuf/empty.proto",
	"google.protobuf.FieldMask":   "google/protobuf/field_mask.proto",
	"google.protobuf.StringValue": "google/protobuf/wrappers.proto",
	"google.protobuf.Int32Value":  "google/protobuf/wrappers.proto",
	"google.protobuf.Int64Value":  "google/protobuf/wrappers.proto",
	"google.protobuf.BoolValue":   "google/protobuf/wrappers.proto",
	"google.protobuf.DoubleValue": "google/protobuf/wrappers.proto",
	"google.protobuf.FloatValue":  "google/protobuf/wrappers.proto",
	"google.protobuf.BytesValue":  "google/protobuf/wrappers.proto",
	"google.protobuf.UInt32Value": "google/protobuf/wrappers.proto",
	"google.protobuf.UInt64Value": "google/protobuf/wrappers.proto",
}

func wktImport(t string) (string, bool) {
	p, ok := wellKnownImports[t]
	return p, ok
}

// generatorMappings renders --type-mappings / --import-mappings for well_known.
func generatorMappings(wk map[string]string) (types, imports string) {
	keys := make([]string, 0, len(wk))
	for k := range wk {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var t, i []string
	seen := map[string]bool{}
	for _, k := range keys {
		t = append(t, k+"="+wk[k])
		if p, ok := wktImport(wk[k]); ok && !seen[wk[k]] {
			seen[wk[k]] = true
			i = append(i, wk[k]+"="+strings.TrimSuffix(p, ".proto"))
		}
	}
	return strings.Join(t, ","), strings.Join(i, ",")
}

// flattenWKT is the well-known type table handed to flatten: the types the
// config can produce (well_known values and any_type).
func flattenWKT(wk map[string]string, anyType string) map[string]string {
	out := map[string]string{}
	for _, t := range wk {
		if p, ok := wktImport(t); ok {
			out[t] = p
		}
	}
	if p, ok := wktImport(anyType); ok {
		out[anyType] = p
	}
	return out
}
