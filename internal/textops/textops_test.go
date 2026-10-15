package textops

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/hilmoo/protoforge/internal/config"
)

func TestRules(t *testing.T) {
	tests := []struct {
		name string
		rule config.ReplaceRule
		in   string
		want string
	}{
		{"literal", config.ReplaceRule{Glob: "*", From: "a.b", To: "X"}, "a.b axb a.b", "X axb X"},
		{"regex groups", config.ReplaceRule{Glob: "*", Regex: `(\w+)=(\d+)`, To: "$2:$1"}, "x=1 y=22", "1:x 22:y"},
		{"regex multiline", config.ReplaceRule{Glob: "*", Regex: `(?m)^// .*\n`, To: ""}, "// c\nkeep\n// d\n", "keep\n"},
		{"delete line", config.ReplaceRule{Glob: "*", DeleteLineMatching: `^import "x`}, "a\nimport \"x/y\";\nb\n", "a\nb\n"},
		{"delete last line no newline", config.ReplaceRule{Glob: "*", DeleteLineMatching: `^z`}, "a\nz", "a\n"},
		{"delete nothing", config.ReplaceRule{Glob: "*", DeleteLineMatching: `nope`}, "a\nb", "a\nb"},
		{"delete all", config.ReplaceRule{Glob: "*", DeleteLineMatching: `.`}, "a\nb\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs, err := Compile([]config.ReplaceRule{tt.rule})
			if err != nil {
				t.Fatal(err)
			}
			if got := rs[0].Apply(tt.in); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestMatches(t *testing.T) {
	r := Rule{Glob: "*.proto"}
	for p, want := range map[string]bool{"a.proto": true, "x/y/a.proto": true, "a.txt": false} {
		if r.Matches(p) != want {
			t.Errorf("%s: want %v", p, want)
		}
	}
	r = Rule{Glob: "models/*.proto"}
	if !r.Matches("models/a.proto") || r.Matches("services/a.proto") {
		t.Error("slash globs match the relative path")
	}
}

func TestApplyDirRewritesOnlyChanged(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.proto"), filepath.Join(dir, "b.proto")
	os.WriteFile(a, []byte("foo\n"), 0o644)
	os.WriteFile(b, []byte("bar\n"), 0o644)
	old := mtime(t, b)
	rules, _ := Compile([]config.ReplaceRule{{Glob: "*.proto", From: "foo", To: "baz"}})
	changed, err := ApplyDir(dir, rules)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0] != "a.proto" {
		t.Fatalf("changed = %v", changed)
	}
	if got, _ := os.ReadFile(a); string(got) != "baz\n" {
		t.Fatalf("a = %q", got)
	}
	if mtime(t, b) != old {
		t.Fatal("unchanged file was rewritten")
	}
}

func TestReplaceLines(t *testing.T) {
	re := regexp.MustCompile(`^package x\..*;`)
	got := ReplaceLines("package x.y;\n  package x.z;\nend", re, "package v1;\noption o;")
	if got != "package v1;\noption o;\n  package x.z;\nend" {
		t.Fatalf("got %q", got)
	}
}

func TestCompileErrors(t *testing.T) {
	if _, err := Compile([]config.ReplaceRule{{Glob: "*", Regex: "("}}); err == nil {
		t.Fatal("bad regex should fail")
	}
	if _, err := Compile([]config.ReplaceRule{{Glob: "*"}}); err == nil {
		t.Fatal("empty rule should fail")
	}
}

func mtime(t *testing.T, p string) int64 {
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.ModTime().UnixNano()
}
