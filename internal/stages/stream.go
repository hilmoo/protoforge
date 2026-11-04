package stages

import (
	"fmt"
	"regexp"
	"strings"
)

// streamRpcRe matches an rpc line carrying the marker the services template
// emits for the `x-protoforge-stream` operation extension.
var streamRpcRe = regexp.MustCompile(`^(\s*rpc\s+\w+\s*\()([\w.]+)(\)\s*returns\s*\()([\w.]+)(\)\s*;)[ \t]*//[ \t]*protoforge:stream=(\S*)[ \t]*$`)

// ApplyStreaming turns `// protoforge:stream=<mode>` markers into proto
// streaming rpcs. Modes: server (stream the response), client (stream the
// request), bidi (both). Any other value is an error.
func ApplyStreaming(s string) (string, error) {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		trimmed := strings.TrimRight(l, "\r")
		m := streamRpcRe.FindStringSubmatch(trimmed)
		if m == nil {
			if strings.Contains(l, "protoforge:stream=") {
				return "", fmt.Errorf("unparseable streaming rpc line: %q", strings.TrimSpace(l))
			}
			continue
		}
		req, res := m[2], m[4]
		switch m[6] {
		case "server":
			res = "stream " + res
		case "client":
			req = "stream " + req
		case "bidi":
			req, res = "stream "+req, "stream "+res
		default:
			return "", fmt.Errorf("x-protoforge-stream: invalid value %q in %q (want server, client or bidi)", m[6], strings.TrimSpace(trimmed))
		}
		out := m[1] + req + m[3] + res + m[5]
		if strings.HasSuffix(l, "\r") {
			out += "\r"
		}
		lines[i] = out
	}
	return strings.Join(lines, "\n"), nil
}
