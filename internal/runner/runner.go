// Package runner executes external commands: argv parsing, tool resolution,
// environment, live prefixed streaming, dry-run and signal forwarding.
package runner

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Runner runs commands for one protoforge invocation.
type Runner struct {
	DryRun  bool
	Verbose bool
	Color   bool
	Stdout  io.Writer
	Stderr  io.Writer
	// Env is the full child environment ("K=V" entries).
	Env []string
	// Root is the repo root (the config file's directory); used for display.
	Root string

	mu      sync.Mutex
	current *os.Process
	sigOnce sync.Once
}

// New returns a Runner writing to the process's stdout/stderr.
func New(root string, env []string) *Runner {
	return &Runner{Stdout: os.Stdout, Stderr: os.Stderr, Env: env, Root: root}
}

// Cmd is one command to run.
type Cmd struct {
	Argv  []string
	Dir   string
	Shell bool     // run through `sh -c` / `cmd /C` (opt-in escape hatch only)
	Env   []string // extra "K=V" entries on top of Runner.Env
	// Prefix is printed before every streamed output line (usually the stage name).
	Prefix string
}

// ExitError is returned when a command fails; it carries everything needed to
// reproduce the failure.
type ExitError struct {
	Cmd  string
	Dir  string
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Code >= 0 {
		return fmt.Sprintf("command failed (exit code %d)\n  command: %s\n  dir:     %s", e.Code, e.Cmd, e.Dir)
	}
	return fmt.Sprintf("command failed: %v\n  command: %s\n  dir:     %s", e.Err, e.Cmd, e.Dir)
}

func (e *ExitError) Unwrap() error { return e.Err }

// Display renders the command as it would be typed.
func (c Cmd) Display() string {
	if c.Shell {
		return c.Argv[0]
	}
	return QuoteArgs(c.Argv)
}

// Logf prints a protoforge message to stderr.
func (r *Runner) Logf(prefix, format string, a ...any) {
	fmt.Fprintf(r.Stderr, "%s%s\n", r.prefix(prefix), fmt.Sprintf(format, a...))
}

// Debugf prints only with --verbose.
func (r *Runner) Debugf(prefix, format string, a ...any) {
	if r.Verbose {
		r.Logf(prefix, format, a...)
	}
}

// Warnf prints a warning.
func (r *Runner) Warnf(prefix, format string, a ...any) {
	w := "warning: "
	if r.Color {
		w = "\x1b[33mwarning:\x1b[0m "
	}
	fmt.Fprintf(r.Stderr, "%s%s%s\n", r.prefix(prefix), w, fmt.Sprintf(format, a...))
}

func (r *Runner) prefix(p string) string {
	if p == "" {
		return ""
	}
	if r.Color {
		return "\x1b[36m[" + p + "]\x1b[0m "
	}
	return "[" + p + "] "
}

// FileOp reports (and in dry-run mode, only reports) a native file operation.
// It returns true when the operation should actually be performed.
func (r *Runner) FileOp(prefix, format string, a ...any) bool {
	if r.DryRun {
		fmt.Fprintf(r.Stdout, "%s%s\n", r.prefix(prefix), fmt.Sprintf(format, a...))
		return false
	}
	r.Debugf(prefix, format, a...)
	return true
}

// Run executes c, streaming output live with the prefix. In dry-run mode it only prints.
func (r *Runner) Run(c Cmd) error {
	if len(c.Argv) == 0 {
		return errors.New("empty command")
	}
	dir := c.Dir
	if dir == "" {
		dir = r.Root
	}
	disp := c.Display()
	if r.DryRun {
		fmt.Fprintf(r.Stdout, "%s$ %s  (in %s)\n", r.prefix(c.Prefix), disp, r.rel(dir))
		return nil
	}
	r.Logf(c.Prefix, "$ %s", disp)

	var cmd *exec.Cmd
	if c.Shell {
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", c.Argv[0])
		} else {
			cmd = exec.Command("sh", "-c", c.Argv[0])
		}
	} else {
		path, err := LookPath(c.Argv[0], dir, r.Env)
		if err != nil {
			return &ExitError{Cmd: disp, Dir: dir, Code: -1, Err: err}
		}
		cmd = exec.Command(path, c.Argv[1:]...)
	}
	cmd.Dir = dir
	cmd.Env = append(append([]string{}, r.Env...), c.Env...)
	cmd.Stdin = os.Stdin
	outW := newLineWriter(r.Stdout, r.prefix(c.Prefix))
	errW := newLineWriter(r.Stderr, r.prefix(c.Prefix))
	cmd.Stdout, cmd.Stderr = outW, errW

	r.forwardSignals()
	if err := cmd.Start(); err != nil {
		return &ExitError{Cmd: disp, Dir: dir, Code: -1, Err: err}
	}
	r.mu.Lock()
	r.current = cmd.Process
	r.mu.Unlock()
	err := cmd.Wait()
	r.mu.Lock()
	r.current = nil
	r.mu.Unlock()
	outW.Flush()
	errW.Flush()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return &ExitError{Cmd: disp, Dir: dir, Code: ee.ExitCode(), Err: err}
		}
		return &ExitError{Cmd: disp, Dir: dir, Code: -1, Err: err}
	}
	return nil
}

// Output runs a command and captures stdout (used by doctor for versions).
// It ignores dry-run, because it never changes anything.
func (r *Runner) Output(argv []string, dir string) (string, error) {
	path, err := LookPath(argv[0], dir, r.Env)
	if err != nil {
		return "", err
	}
	cmd := exec.Command(path, argv[1:]...)
	cmd.Dir = dir
	cmd.Env = r.Env
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	return strings.TrimSpace(out.String()), err
}

// forwardSignals installs (once) a handler that relays Ctrl-C to the running child
// instead of killing protoforge first, so the child can clean up.
func (r *Runner) forwardSignals() {
	r.sigOnce.Do(func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, interruptSignals()...)
		go func() {
			for s := range ch {
				r.mu.Lock()
				p := r.current
				r.mu.Unlock()
				if p == nil {
					os.Exit(130)
				}
				if runtime.GOOS == "windows" {
					// Console Ctrl-C already reaches the child (same console group).
					continue
				}
				_ = p.Signal(s)
			}
		}()
	})
}

func (r *Runner) rel(dir string) string {
	if r.Root == "" {
		return dir
	}
	rel, err := filepath.Rel(r.Root, dir)
	if err != nil {
		return dir
	}
	return filepath.ToSlash(rel)
}

// lineWriter prefixes each line written to it.
type lineWriter struct {
	mu     sync.Mutex
	w      io.Writer
	prefix string
	buf    []byte
}

func newLineWriter(w io.Writer, prefix string) *lineWriter {
	return &lineWriter{w: w, prefix: prefix}
}

func (l *lineWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		if _, err := fmt.Fprintf(l.w, "%s%s\n", l.prefix, l.buf[:i]); err != nil {
			return 0, err
		}
		l.buf = l.buf[i+1:]
	}
	return len(p), nil
}

// Flush writes any trailing partial line.
func (l *lineWriter) Flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buf) > 0 {
		fmt.Fprintf(l.w, "%s%s\n", l.prefix, l.buf)
		l.buf = nil
	}
}

// LoadEnvFile parses a dotenv file: KEY=VALUE lines, `#` comments, optional
// `export ` prefix, single or double quoted values (double quotes support \n).
func LoadEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: expected KEY=VALUE", path, n)
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch {
		case len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"':
			v = strings.NewReplacer(`\n`, "\n", `\"`, `"`, `\\`, `\`).Replace(v[1 : len(v)-1])
		case len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'':
			v = v[1 : len(v)-1]
		default:
			if i := strings.Index(v, " #"); i >= 0 {
				v = strings.TrimSpace(v[:i])
			}
		}
		out[k] = v
	}
	return out, sc.Err()
}

// BuildEnv merges the process environment, the env file (which does not
// override variables already set) and the config `env:` map (which does, after
// ${VAR} expansion against the process + env file environment).
func BuildEnv(base []string, file map[string]string, extra map[string]string) []string {
	m := map[string]string{}
	var order []string
	set := func(k, v string) {
		if _, ok := m[k]; !ok {
			order = append(order, k)
		}
		m[k] = v
	}
	for _, kv := range base {
		k, v, _ := strings.Cut(kv, "=")
		set(k, v)
	}
	keys := sortedKeys(file)
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			set(k, file[k])
		}
	}
	// Expand against the environment before env: applies, so the result does
	// not depend on the order of the keys.
	before := make(map[string]string, len(m))
	for k, v := range m {
		before[k] = v
	}
	for _, k := range sortedKeys(extra) {
		set(k, os.Expand(extra[k], func(name string) string { return before[name] }))
	}
	out := make([]string, 0, len(order))
	for _, k := range order {
		out = append(out, k+"="+m[k])
	}
	return out
}

// Getenv looks up a variable in an env slice.
func Getenv(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		k, v, _ := strings.Cut(env[i], "=")
		if k == key || (runtime.GOOS == "windows" && strings.EqualFold(k, key)) {
			return v
		}
	}
	return ""
}
