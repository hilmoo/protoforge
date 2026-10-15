// Package syncmig is the port of migration/sync (the old shell script): it copies every
// drizzle/<dir>/migration.sql to <to>/<dir><suffix>, turning backticks into
// double quotes and dropping `--> statement-breakpoint` lines.
package syncmig

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/hilmoo/protoforge/internal/textops"
)

// Options configures one sync.
type Options struct {
	From   string // drizzle output directory (e.g. migration/drizzle)
	To     string // target directory; its contents are replaced
	Suffix string // default ".up.sql"
	Root   string // repo root; To must resolve inside it
	Extra  []textops.Rule
	// Warn receives non-fatal problems (a directory without migration.sql).
	Warn func(format string, a ...any)
	// DryRun only reports what would happen through Op.
	DryRun bool
	Op     func(verb, path string)
}

// breakpoint mirrors the original `/^-->\s*statement-breakpoint/d`.
var breakpoint = regexp.MustCompile(`^-->\s*statement-breakpoint`)

// Transform applies the built-in sync rewrites to one migration file.
func Transform(s string) string {
	s = strings.ReplaceAll(s, "`", `"`)
	return textops.DeleteLines(s, breakpoint.MatchString)
}

// CheckTarget refuses dangerous targets: "/", $HOME, or anything outside root.
func CheckTarget(to, root string) error {
	to, err := filepath.Abs(to)
	if err != nil {
		return err
	}
	to = filepath.Clean(to)
	if to == filepath.VolumeName(to)+string(filepath.Separator) {
		return fmt.Errorf("refusing to sync into filesystem root %s", to)
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.Clean(home) == to {
		return fmt.Errorf("refusing to sync into $HOME (%s)", to)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, to)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to sync into %s: it must be a directory inside the repo root %s", to, root)
	}
	return nil
}

// Run performs the sync and returns the files written (relative to To).
func Run(o Options) ([]string, error) {
	if o.Suffix == "" {
		o.Suffix = ".up.sql"
	}
	if o.Warn == nil {
		o.Warn = func(string, ...any) {}
	}
	if o.Op == nil {
		o.Op = func(string, string) {}
	}
	if err := CheckTarget(o.To, o.Root); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(o.From)
	if err != nil {
		return nil, err
	}
	// Bash globs `drizzle/*/` in sorted order; entries from ReadDir are sorted too.
	type job struct{ src, dest string }
	var jobs []job
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue // `*/` does not match dot directories
		}
		info, err := os.Stat(filepath.Join(o.From, e.Name())) // follow symlinks like `[ -d ]`
		if err != nil || !info.IsDir() {
			continue
		}
		src := filepath.Join(o.From, e.Name(), "migration.sql")
		if st, err := os.Stat(src); err != nil || st.IsDir() {
			o.Warn("no migration.sql found in %s", filepath.ToSlash(filepath.Join(o.From, e.Name()))+"/")
			continue
		}
		jobs = append(jobs, job{src, e.Name() + o.Suffix})
	}

	o.Op("clean", o.To)
	for _, j := range jobs {
		o.Op("write", filepath.Join(o.To, j.dest))
	}
	if o.DryRun {
		return nil, nil
	}

	if err := os.MkdirAll(o.To, 0o755); err != nil {
		return nil, err
	}
	old, err := os.ReadDir(o.To)
	if err != nil {
		return nil, err
	}
	for _, e := range old {
		// `rm -rf dir/*` leaves dotfiles alone.
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if err := os.RemoveAll(filepath.Join(o.To, e.Name())); err != nil {
			return nil, err
		}
	}
	var written []string
	for _, j := range jobs {
		b, err := os.ReadFile(j.src)
		if err != nil {
			return nil, err
		}
		out := Transform(string(b))
		for _, r := range o.Extra {
			if r.Matches(j.dest) {
				out = r.Apply(out)
			}
		}
		if err := textops.WriteFile(filepath.Join(o.To, j.dest), []byte(out)); err != nil {
			return nil, err
		}
		written = append(written, j.dest)
	}
	sort.Strings(written)
	return written, nil
}
