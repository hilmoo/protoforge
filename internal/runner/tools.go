package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// LookPath finds an executable like exec.LookPath, but against the PATH in env
// (the child environment) and, on Windows, honouring PATHEXT so `.cmd`/`.bat`
// shims such as pnpm.cmd and npx.cmd are found. Names containing a path
// separator are resolved against dir.
func LookPath(name, dir string, env []string) (string, error) {
	exts := []string{""}
	if runtime.GOOS == "windows" {
		exts = windowsExts(env)
		if filepath.Ext(name) != "" {
			exts = append([]string{""}, exts...)
		}
	}
	if strings.ContainsAny(name, `/\`) {
		p := name
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		for _, e := range exts {
			if isExecutable(p + e) {
				return p + e, nil
			}
		}
		return "", fmt.Errorf("%s: not found or not executable", p)
	}
	path := Getenv(env, "PATH")
	for _, d := range filepath.SplitList(path) {
		if d == "" {
			continue
		}
		for _, e := range exts {
			p := filepath.Join(d, name+e)
			if isExecutable(p) {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("%q not found in PATH", name)
}

func windowsExts(env []string) []string {
	pe := Getenv(env, "PATHEXT")
	if pe == "" {
		pe = ".COM;.EXE;.BAT;.CMD"
	}
	var out []string
	for _, e := range strings.Split(pe, ";") {
		if e != "" {
			out = append(out, strings.ToLower(e))
		}
	}
	return out
}

func isExecutable(p string) bool {
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return st.Mode()&0o111 != 0
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Tool describes how an external tool was resolved.
type Tool struct {
	Name   string   // logical name, e.g. "drizzle-kit"
	Argv   []string // argv prefix to invoke it
	Source string   // "config", "node_modules", "PATH", "node runner"
}

// ErrNotFound means a tool could not be resolved at all.
var ErrNotFound = errors.New("not found")

// ToolSpec says how to find one external tool.
type ToolSpec struct {
	Name     string // display name
	Bin      string // executable name
	Override string // tools.<x> from the config, if set
	Node     bool   // an npm package binary: look in node_modules/.bin and fall back to the node runner
}

// Resolve finds a tool. Order: the config override, then (for npm tools) the
// nearest node_modules/.bin from dir up to the repo root (so the project's
// pinned version wins), then PATH, then the node runner (`pnpm exec x`).
func (r *Runner) Resolve(spec ToolSpec, dir, nodeRunner string) (Tool, error) {
	t := Tool{Name: spec.Name}
	if spec.Override != "" {
		argv, err := SplitArgs(spec.Override)
		if err != nil || len(argv) == 0 {
			return t, fmt.Errorf("tools override for %s: invalid command %q", spec.Name, spec.Override)
		}
		t.Argv, t.Source = argv, "config"
		return t, nil
	}
	if spec.Node {
		if p := findNodeBin(spec.Bin, dir, r.Root, r.Env); p != "" {
			t.Argv, t.Source = []string{p}, "node_modules"
			return t, nil
		}
	}
	if p, err := LookPath(spec.Bin, dir, r.Env); err == nil {
		t.Argv, t.Source = []string{p}, "PATH"
		return t, nil
	}
	if spec.Node && nodeRunner != "" {
		argv, err := SplitArgs(nodeRunner)
		if err == nil && len(argv) > 0 {
			if _, err := LookPath(argv[0], dir, r.Env); err == nil {
				t.Argv, t.Source = append(argv, spec.Bin), "node runner"
				return t, nil
			}
		}
	}
	return t, fmt.Errorf("%s: %w (looked in node_modules/.bin, PATH and the node runner)", spec.Name, ErrNotFound)
}

func findNodeBin(bin, dir, root string, env []string) string {
	d := dir
	for {
		cand := filepath.Join(d, "node_modules", ".bin", bin)
		if p, err := LookPath(cand, d, env); err == nil {
			return p
		}
		if d == root || filepath.Dir(d) == d || !strings.HasPrefix(d, root) {
			return ""
		}
		d = filepath.Dir(d)
	}
}

// DetectNodeRunner picks the package-manager exec command from the nearest lockfile.
func DetectNodeRunner(dir string) string {
	d := dir
	for {
		for _, c := range []struct{ file, runner string }{
			{"pnpm-lock.yaml", "pnpm exec"},
			{"bun.lock", "bunx"},
			{"bun.lockb", "bunx"},
			{"yarn.lock", "yarn exec"},
			{"package-lock.json", "npx"},
		} {
			if _, err := os.Stat(filepath.Join(d, c.file)); err == nil {
				return c.runner
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "npx"
		}
		d = parent
	}
}
