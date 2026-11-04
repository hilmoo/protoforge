package stages

import (
	"strings"
	"testing"
)

func TestApplyStreaming(t *testing.T) {
	in := strings.Join([]string{
		"  rpc A (ARequest) returns (AResponse); // protoforge:stream=server",
		"  rpc B (BRequest) returns (BResponse); // protoforge:stream=client",
		"  rpc C (CRequest) returns (CResponse); // protoforge:stream=bidi",
		"  rpc D (DRequest) returns (DResponse);",
	}, "\n")
	want := strings.Join([]string{
		"  rpc A (ARequest) returns (stream AResponse);",
		"  rpc B (stream BRequest) returns (BResponse);",
		"  rpc C (stream CRequest) returns (stream CResponse);",
		"  rpc D (DRequest) returns (DResponse);",
	}, "\n")
	got, err := ApplyStreaming(in)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestApplyStreamingInvalid(t *testing.T) {
	if _, err := ApplyStreaming("  rpc A (ARequest) returns (AResponse); // protoforge:stream=nope"); err == nil {
		t.Fatal("expected error for invalid mode")
	}
}
