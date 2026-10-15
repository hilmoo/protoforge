// Package flatten is the port of proto/scripts/flatten_services.py.
//
// openapi-generator emits `message XRequest { models.v1.Foo foo = 1; }`. This
// inlines Foo's fields into the request so it is not a wrapper around a model
// message. Enums and well-known types stay imported; responses keep their
// model imports.
package flatten

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var (
	msgRe    = regexp.MustCompile(`(?ms)^message (\w+) \{\n(.*?)^\}\n`)
	fieldRe  = regexp.MustCompile(`^(\s*)((?:optional|repeated) )?(map<[^>]*>|[\w.]+) (\w+) = (\d+)(.*;)$`)
	enumRe   = regexp.MustCompile(`(?m)^enum (\w+) \{`)
	wordRe   = regexp.MustCompile(`[\w.]+`)
	numRe    = regexp.MustCompile(`= (\d+)`)
	importRe = regexp.MustCompile(`(?m)^import .*\n`)
	blankRe  = regexp.MustCompile(`\n{3,}`)
)

// WellKnown maps protobuf well-known types to their import paths.
var WellKnown = map[string]string{
	"google.protobuf.Timestamp": "google/protobuf/timestamp.proto",
	"google.protobuf.Any":       "google/protobuf/any.proto",
}

// Options configures a run.
type Options struct {
	Registry string // registry root (contains models/<ver> and services/<ver>)
	Version  string // proto package version, e.g. "v1"
	// WellKnown overrides the default WKT import table when non-nil.
	WellKnown map[string]string
}

type state struct {
	ver      string
	prefix   string // "models.<ver>."
	messages map[string]string
	files    map[string]string
	wkt      map[string]string
}

// ordered is an insertion-ordered map (dict semantics of the original).
type ordered struct {
	keys []string
	vals map[string]string
}

func (o *ordered) has(k string) bool { _, ok := o.vals[k]; return ok }
func (o *ordered) set(k, v string) {
	if !o.has(k) {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

// Run flattens every services/<ver>/*.proto in place.
func Run(o Options) error {
	if o.Version == "" {
		o.Version = "v1"
	}
	st := &state{
		ver:      o.Version,
		prefix:   "models." + o.Version + ".",
		messages: map[string]string{},
		files:    map[string]string{},
		wkt:      o.WellKnown,
	}
	if st.wkt == nil {
		st.wkt = WellKnown
	}
	modelsDir := filepath.Join(o.Registry, "models", o.Version)
	servicesDir := filepath.Join(o.Registry, "services", o.Version)

	models, err := globSorted(modelsDir)
	if err != nil {
		return err
	}
	for _, p := range models {
		text, err := readText(p)
		if err != nil {
			return err
		}
		stem := strings.TrimSuffix(filepath.Base(p), ".proto")
		for _, m := range msgRe.FindAllStringSubmatch(text, -1) {
			st.messages[m[1]] = strings.Trim(m[2], "\n")
			st.files[m[1]] = stem
		}
		for _, m := range enumRe.FindAllStringSubmatch(text, -1) {
			st.files[m[1]] = stem
		}
	}

	services, err := globSorted(servicesDir)
	if err != nil {
		return err
	}
	for _, p := range services {
		text, err := readText(p)
		if err != nil {
			return err
		}
		out, err := st.file(text)
		if err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(out), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// File flattens one services file given the models directory contents; exposed for tests.
func (st *state) file(text string) (string, error) {
	used := map[string]bool{}
	var ferr error

	// MSG_RE.sub(repl, text)
	var b strings.Builder
	last := 0
	for _, m := range msgRe.FindAllStringSubmatchIndex(text, -1) {
		b.WriteString(text[last:m[0]])
		name, body := text[m[2]:m[3]], text[m[4]:m[5]]
		if !strings.HasSuffix(name, "Request") {
			b.WriteString(text[m[0]:m[1]])
		} else {
			flat, err := st.flatten(name, body, used)
			if err != nil && ferr == nil {
				ferr = err
			}
			if strings.TrimSpace(flat) != "" {
				fmt.Fprintf(&b, "message %s {\n%s\n}\n", name, flat)
			} else {
				fmt.Fprintf(&b, "message %s {\n}\n", name)
			}
		}
		last = m[1]
	}
	b.WriteString(text[last:])
	if ferr != nil {
		return "", ferr
	}
	text = b.String()

	refRe := regexp.MustCompile(regexp.QuoteMeta(st.prefix) + `(\w+)`)
	for _, m := range refRe.FindAllStringSubmatch(text, -1) {
		if _, ok := st.files[m[1]]; ok {
			used[m[1]] = true
		}
	}
	importSet := map[string]bool{}
	for t := range used {
		if w, ok := st.wkt[t]; ok && w != "" {
			importSet[w] = true
		} else {
			importSet["models/"+st.ver+"/"+st.files[t]+".proto"] = true
		}
	}
	imports := make([]string, 0, len(importSet))
	for i := range importSet {
		imports = append(imports, i)
	}
	sort.Strings(imports) // code-point order == byte order for UTF-8
	text = importRe.ReplaceAllString(text, "")
	var block strings.Builder
	for _, i := range imports {
		fmt.Fprintf(&block, "import \"%s\";\n", i)
	}
	repl := "\nservice "
	if block.Len() > 0 {
		repl = "\n" + block.String() + "\nservice "
	}
	text = strings.Replace(text, "\nservice ", repl, 1)
	return blankRe.ReplaceAllString(text, "\n\n"), nil
}

// qualify prefixes enum references with models.<ver>. and records them;
// message references are kept unqualified and defined as nested messages.
func (st *state) qualify(body string, used map[string]bool, nested *ordered) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		m := fieldRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ind, label, typ, name, num, rest := m[1], m[2], m[3], m[4], m[5], m[6]
		fix := func(t string) string {
			if msg, ok := st.messages[t]; ok {
				if !nested.has(t) {
					nested.set(t, "")
					nested.set(t, st.qualify(msg, used, nested))
				}
				return t
			}
			if _, ok := st.files[t]; ok {
				used[t] = true
				return st.prefix + t
			}
			if _, ok := st.wkt[t]; ok {
				used[t] = true
			}
			return t
		}
		if strings.HasPrefix(typ, "map<") {
			typ = wordRe.ReplaceAllStringFunc(typ, func(w string) string {
				if w == "map" {
					return "map"
				}
				return fix(w)
			})
		} else {
			typ = fix(typ)
		}
		lines[i] = ind + label + typ + " " + name + " = " + num + rest
	}
	return strings.Join(lines, "\n")
}

func indent(body string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		if strings.TrimFunc(l, pyIsSpace) != "" {
			lines[i] = pad + l
		}
	}
	return strings.Join(lines, "\n")
}

// pyIsSpace mirrors str.isspace() for one character (as used by str.strip()).
func pyIsSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

func (st *state) flatten(name, body string, used map[string]bool) (string, error) {
	var parts []string
	seen := map[string]bool{}
	nested := &ordered{vals: map[string]string{}}
	for _, line := range strings.Split(body, "\n") {
		m := fieldRe.FindStringSubmatch(line)
		if m == nil || !strings.HasPrefix(m[3], st.prefix) {
			parts = append(parts, strings.Trim(line, "\n"))
			continue
		}
		model := m[3][len(st.prefix):]
		msg, ok := st.messages[model]
		if !ok {
			parts = append(parts, strings.Trim(line, "\n"))
			continue
		}
		inner := st.qualify(msg, used, nested)
		if m[2] == "repeated " {
			parts = append(parts, fmt.Sprintf("  message Item {\n%s\n  }\n\n  repeated Item %s = %s;", indent(inner, 2), m[4], m[5]))
		} else {
			parts = append(parts, inner)
		}
		for _, n := range numRe.FindAllStringSubmatch(inner, -1) {
			if seen[n[1]] {
				return "", fmt.Errorf("%s: duplicate field number %s", name, n[1])
			}
			seen[n[1]] = true
		}
	}
	defs := make([]string, 0, len(nested.keys))
	for _, t := range nested.keys {
		defs = append(defs, fmt.Sprintf("  message %s {\n%s\n  }\n", t, indent(nested.vals[t], 2)))
	}
	return strings.Trim(strings.Join(append(defs, parts...), "\n"), "\n"), nil
}

func globSorted(dir string) ([]string, error) {
	ps, err := filepath.Glob(filepath.Join(dir, "*.proto"))
	if err != nil {
		return nil, err
	}
	sort.Strings(ps)
	return ps, nil
}

// readText mirrors Path.read_text(): universal newlines.
func readText(p string) (string, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n"), nil
}
