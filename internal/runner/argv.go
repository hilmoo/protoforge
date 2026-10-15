package runner

import (
	"fmt"
	"strings"
)

// SplitArgs splits a command line into argv using POSIX-shell-like rules:
// whitespace separates words, single quotes are literal, double quotes allow
// backslash escapes of `"` and `\`, and a backslash outside quotes escapes the
// next character. No globbing, variables or operators are interpreted, so the
// same string behaves identically on every OS.
func SplitArgs(s string) ([]string, error) {
	var (
		args    []string
		cur     strings.Builder
		inWord  bool
		quote   rune
		escaped bool
	)
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
			inWord = true
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			switch r {
			case '"':
				quote = 0
			case '\\':
				escaped = true
			default:
				cur.WriteRune(r)
			}
		case r == '\\':
			escaped = true
		case r == '\'' || r == '"':
			quote = r
			inWord = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if inWord {
				args = append(args, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if escaped {
		return nil, fmt.Errorf("unterminated escape in %q", s)
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote in %q", quote, s)
	}
	if inWord {
		args = append(args, cur.String())
	}
	return args, nil
}

// QuoteArgs renders argv for display (dry-run and error messages).
func QuoteArgs(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if a == "" || strings.ContainsAny(a, " \t\n\"'\\$`|&;<>()*?[]{}") {
			parts[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		} else {
			parts[i] = a
		}
	}
	return strings.Join(parts, " ")
}
