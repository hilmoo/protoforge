// Package textops implements the portable replacements for the old in-place line-editing chain:
// literal and regex replacement, line deletion, and glob-scoped rule application.
package textops

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/hilmoo/protoforge/internal/config"
)

// Rule is a compiled extra_replace rule.
type Rule struct {
	Glob    string
	from    string
	re      *regexp.Regexp
	to      string
	delLine *regexp.Regexp
}

// Compile turns config rules into executable rules.
func Compile(rules []config.ReplaceRule) ([]Rule, error) {
	out := make([]Rule, 0, len(rules))
	for i, r := range rules {
		cr := Rule{Glob: filepath.ToSlash(r.Glob), from: r.From, to: r.To}
		var err error
		switch {
		case r.Regex != "":
			cr.re, err = regexp.Compile(r.Regex)
		case r.DeleteLineMatching != "":
			cr.delLine, err = regexp.Compile(r.DeleteLineMatching)
		case r.From == "":
			err = fmt.Errorf("no from, regex or delete_line_matching")
		}
		if err != nil {
			return nil, fmt.Errorf("extra_replace[%d]: %w", i, err)
		}
		out = append(out, cr)
	}
	return out, nil
}

// Apply runs one rule over s.
func (r Rule) Apply(s string) string {
	switch {
	case r.re != nil:
		return r.re.ReplaceAllString(s, r.to)
	case r.delLine != nil:
		return DeleteLines(s, r.delLine.MatchString)
	default:
		return strings.ReplaceAll(s, r.from, r.to)
	}
}

// Matches reports whether rel (a slash-separated path relative to the rule's
// base directory) is selected by the rule's glob. A glob without a slash is
// matched against the base name, so "*.proto" selects files at any depth.
func (r Rule) Matches(rel string) bool {
	rel = filepath.ToSlash(rel)
	target := rel
	if !strings.Contains(r.Glob, "/") {
		target = filepath.Base(rel)
	}
	ok, _ := filepath.Match(r.Glob, target)
	return ok
}

// ApplyFile applies the matching rules, in order, to a file. The file is
// rewritten only when its content changed. It returns whether it changed.
func ApplyFile(path, rel string, rules []Rule) (bool, error) {
	var sel []Rule
	for _, r := range rules {
		if r.Matches(rel) {
			sel = append(sel, r)
		}
	}
	if len(sel) == 0 {
		return false, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	s := string(b)
	out := s
	for _, r := range sel {
		out = r.Apply(out)
	}
	if out == s {
		return false, nil
	}
	return true, WriteFile(path, []byte(out))
}

// ApplyDir applies rules to every regular file under dir.
func ApplyDir(dir string, rules []Rule) ([]string, error) {
	if len(rules) == 0 {
		return nil, nil
	}
	var changed []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		ok, err := ApplyFile(p, rel, rules)
		if ok {
			changed = append(changed, rel)
		}
		return err
	})
	sort.Strings(changed)
	return changed, err
}

// DeleteLines removes every line for which match returns true, like a stream editor's `/re/d`.
// Each kept line keeps its own terminator, so a missing final newline is
// preserved exactly as a stream editor preserves it.
func DeleteLines(s string, match func(string) bool) string {
	var b strings.Builder
	for _, piece := range strings.SplitAfter(s, "\n") {
		if piece == "" {
			continue
		}
		if !match(strings.TrimSuffix(piece, "\n")) {
			b.WriteString(piece)
		}
	}
	return b.String()
}

// ReplaceLines applies a regex replacement to each line separately (stream-editor `s///`
// semantics, where `^`/`$` anchor to the line). The replacement may contain "\n".
func ReplaceLines(s string, re *regexp.Regexp, repl string) string {
	var b strings.Builder
	for _, piece := range strings.SplitAfter(s, "\n") {
		line, nl := strings.CutSuffix(piece, "\n")
		b.WriteString(re.ReplaceAllString(line, repl))
		if nl {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// WriteFile writes data with 0644 permissions. Callers always pass "\n" line
// endings, so output is byte-identical on every OS.
func WriteFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
