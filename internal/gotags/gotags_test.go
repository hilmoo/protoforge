package gotags

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const fixtures = "../../testdata/fixtures/gotags"

// TestGolden compares the Go port with the checked-in output of the oracle,
// first as parsed JSON (deep-equal) and then byte for byte.
func TestGolden(t *testing.T) {
	for _, name := range []string{"sample", "rules"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(fixtures, name)
			in := read(t, filepath.Join(dir, "input.json"))
			want := read(t, filepath.Join(dir, "expected.json"))
			var warn bytes.Buffer
			got, err := TransformBytes(in, &warn)
			if err != nil {
				t.Fatal(err)
			}
			var g, w any
			if err := json.Unmarshal(got, &g); err != nil {
				t.Fatalf("output is not valid JSON: %v", err)
			}
			if err := json.Unmarshal(want, &w); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(g, w) {
				t.Fatalf("parsed JSON differs from oracle output")
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("bytes differ from oracle output at %d", firstDiff(got, want))
			}
			if stderr, err := os.ReadFile(filepath.Join(dir, "expected.stderr")); err == nil {
				if warn.String() != string(stderr) {
					t.Fatalf("warnings differ:\ngot:  %q\nwant: %q", warn.String(), stderr)
				}
			}
		})
	}
}

// TestIdempotent: running the transform on its own output changes nothing.
func TestIdempotent(t *testing.T) {
	want := read(t, filepath.Join(fixtures, "sample", "expected.json"))
	got, err := TransformBytes(want, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("transform is not idempotent")
	}
}

func TestPyFloatRepr(t *testing.T) {
	for in, want := range map[float64]string{
		1: "1.0", 0.5: "0.5", 1e16: "1e+16", 1e15: "1000000000000000.0", 1e-5: "1e-05",
		0.0001: "0.0001", 1.5e-7: "1.5e-07", -2.25: "-2.25", 123456789.125: "123456789.125",
	} {
		if got := PyFloatRepr(in); got != want {
			t.Errorf("PyFloatRepr(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestDumps(t *testing.T) {
	v, err := Parse([]byte(`{"a": [1, 2.0, null, true], "b": {}, "c": "é\"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Dumps(v), `{"a": [1, 2.0, null, true], "b": {}, "c": "é\"x"}`; got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func read(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func firstDiff(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}
