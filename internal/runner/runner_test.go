package runner

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// The test binary doubles as a fake external tool: when PF_FAKE_TOOL is set it
// prints its argv and one env var, then exits with the requested code.
func TestMain(m *testing.M) {
	if os.Getenv("PF_FAKE_TOOL") == "1" {
		args := os.Args[1:]
		for len(args) > 0 && strings.HasPrefix(args[0], "-test.") {
			args = args[1:]
		}
		fmt.Printf("argv=%q\n", args)
		fmt.Printf("FOO=%s\n", os.Getenv("FOO"))
		fmt.Fprint(os.Stderr, "partial-stderr") // no trailing newline
		if c := os.Getenv("PF_EXIT"); c != "" {
			var n int
			fmt.Sscan(c, &n)
			os.Exit(n)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestSplitArgs(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"pnpm exec", []string{"pnpm", "exec"}},
		{"  a   b\tc ", []string{"a", "b", "c"}},
		{`java -jar "/opt/my tools/x.jar"`, []string{"java", "-jar", "/opt/my tools/x.jar"}},
		{`a 'b c' "d \"e\""`, []string{"a", "b c", `d "e"`}},
		{`a\ b`, []string{"a b"}},
		{`x ''`, []string{"x", ""}},
		{`echo $HOME *.go`, []string{"echo", "$HOME", "*.go"}},
	}
	for _, tt := range tests {
		got, err := SplitArgs(tt.in)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("SplitArgs(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	for _, bad := range []string{`"open`, `'open`, `trail\`} {
		if _, err := SplitArgs(bad); err == nil {
			t.Errorf("SplitArgs(%q) should fail", bad)
		}
	}
}

func fakeRunner(t *testing.T, extraEnv ...string) (*Runner, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	env := append(os.Environ(), "PF_FAKE_TOOL=1")
	env = append(env, extraEnv...)
	r := New(t.TempDir(), env)
	var out, errb bytes.Buffer
	r.Stdout, r.Stderr = &out, &errb
	return r, &out, &errb
}

func TestRunArgvEnvAndPrefix(t *testing.T) {
	self, _ := filepath.Abs(os.Args[0])
	r, out, errb := fakeRunner(t, "FOO=bar")
	err := r.Run(Cmd{Argv: []string{self, "a b", `c"d`}, Prefix: "stage", Env: []string{"FOO=override"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `[stage] argv=["a b" "c\"d"]`) {
		t.Errorf("argv not passed intact: %s", out)
	}
	if !strings.Contains(out.String(), "[stage] FOO=override") {
		t.Errorf("Cmd.Env should override Runner.Env: %s", out)
	}
	if !strings.Contains(errb.String(), "[stage] partial-stderr\n") {
		t.Errorf("partial stderr line not flushed with prefix: %q", errb)
	}
}

func TestRunExitCode(t *testing.T) {
	self, _ := filepath.Abs(os.Args[0])
	r, _, _ := fakeRunner(t, "PF_EXIT=3")
	err := r.Run(Cmd{Argv: []string{self}, Dir: r.Root})
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 3 || ee.Dir != r.Root {
		t.Fatalf("want ExitError code 3, got %v", err)
	}
	if !strings.Contains(err.Error(), "exit code 3") || !strings.Contains(err.Error(), self) {
		t.Errorf("error should name command and code: %v", err)
	}
}

func TestDryRun(t *testing.T) {
	r, out, _ := fakeRunner(t)
	r.DryRun = true
	sub := filepath.Join(r.Root, "openapi")
	if err := r.Run(Cmd{Argv: []string{"tsp", "compile", "./src dir"}, Dir: sub, Prefix: "typespec"}); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "[typespec] $ tsp compile './src dir'  (in openapi)\n"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if r.FileOp("x", "remove %s", "a") {
		t.Fatal("FileOp must return false in dry-run")
	}
}

func TestMissingTool(t *testing.T) {
	r, _, _ := fakeRunner(t)
	err := r.Run(Cmd{Argv: []string{"definitely-not-a-tool-xyz"}})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("got %v", err)
	}
}

func TestBuildEnv(t *testing.T) {
	env := BuildEnv([]string{"A=1", "PATH=/bin"}, map[string]string{"A": "file", "B": "2"}, map[string]string{"C": "${B}-${A}", "A": "x"})
	if Getenv(env, "A") != "x" || Getenv(env, "B") != "2" || Getenv(env, "C") != "2-1" {
		t.Fatalf("env = %v", env)
	}
}

func TestLoadEnvFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("# c\nexport A=1\nB=\"x\\ny\"\nC='$lit'\nD=v # comment\n\nE=\n"), 0o644)
	m, err := LoadEnvFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": "1", "B": "x\ny", "C": "$lit", "D": "v", "E": ""}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("got %v", m)
	}
}

func TestResolveOrder(t *testing.T) {
	root := t.TempDir()
	stage := filepath.Join(root, "openapi")
	bin := filepath.Join(root, "node_modules", ".bin")
	os.MkdirAll(bin, 0o755)
	os.MkdirAll(stage, 0o755)
	name := "faketool"
	if runtime.GOOS == "windows" {
		name += ".cmd"
	}
	os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755)
	r := New(root, []string{"PATH=" + t.TempDir(), "PATHEXT=.CMD;.EXE"})

	tool, err := r.Resolve(ToolSpec{Name: "faketool", Bin: "faketool", Node: true}, stage, "")
	if err != nil || tool.Source != "node_modules" {
		t.Fatalf("node_modules lookup: %+v %v", tool, err)
	}
	tool, err = r.Resolve(ToolSpec{Name: "faketool", Bin: "faketool", Override: "java -jar 'a b.jar'", Node: true}, stage, "")
	if err != nil || tool.Source != "config" || !reflect.DeepEqual(tool.Argv, []string{"java", "-jar", "a b.jar"}) {
		t.Fatalf("override: %+v %v", tool, err)
	}
	if _, err := r.Resolve(ToolSpec{Name: "missing", Bin: "missing", Node: true}, stage, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestDetectNodeRunner(t *testing.T) {
	d := t.TempDir()
	sub := filepath.Join(d, "a", "b")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(d, "pnpm-lock.yaml"), nil, 0o644)
	if got := DetectNodeRunner(sub); got != "pnpm exec" {
		t.Fatalf("got %s", got)
	}
}
