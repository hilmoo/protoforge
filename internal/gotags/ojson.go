package gotags

// An order-preserving JSON model that round-trips exactly like the original's
// json.load / json.dump(indent=2, ensure_ascii=False).

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Object is a JSON object that keeps key order (like an insertion-ordered dict).
type Object struct {
	keys []string
	vals map[string]any
}

// NewObject returns an empty object.
func NewObject() *Object { return &Object{vals: map[string]any{}} }

// Get returns the value for k.
func (o *Object) Get(k string) (any, bool) {
	v, ok := o.vals[k]
	return v, ok
}

// Has reports whether k is present (`k in d`).
func (o *Object) Has(k string) bool {
	_, ok := o.vals[k]
	return ok
}

// Set inserts or replaces k. A replaced key keeps its position (dict semantics).
func (o *Object) Set(k string, v any) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

// Keys returns the keys in insertion order.
func (o *Object) Keys() []string { return o.keys }

// Len returns the number of keys.
func (o *Object) Len() int { return len(o.keys) }

// Copy is a shallow copy (`dict(d)`).
func (o *Object) Copy() *Object {
	c := &Object{keys: append([]string{}, o.keys...), vals: make(map[string]any, len(o.vals))}
	for k, v := range o.vals {
		c.vals[k] = v
	}
	return c
}

// Int is a JSON integer (arbitrary precision).
type Int struct{ *big.Int }

// Float is a JSON number with a fraction or exponent (a binary64 float).
type Float float64

// Parse decodes JSON into *Object, []any, string, Int, Float, bool and nil.
// Duplicate keys keep the first position and the last value, like the original.
func Parse(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := parseValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("extra data after JSON value")
	}
	return v, nil
}

func parseValue(dec *json.Decoder) (any, error) {
	t, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := t.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := NewObject()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, ok := kt.(string)
				if !ok {
					return nil, fmt.Errorf("object key is not a string: %v", kt)
				}
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				o.Set(k, v)
			}
			_, err := dec.Token()
			return o, err
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			_, err := dec.Token()
			return arr, err
		}
		return nil, fmt.Errorf("unexpected delimiter %v", t)
	case json.Number:
		s := string(t)
		if !strings.ContainsAny(s, ".eE") {
			n, ok := new(big.Int).SetString(s, 10)
			if !ok {
				return nil, fmt.Errorf("bad integer %s", s)
			}
			return Int{n}, nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return nil, err
		}
		return Float(f), nil
	default:
		return t, nil // string, bool, nil
	}
}

// Marshal renders v like json.dump(v, indent=2, ensure_ascii=False) (no trailing newline).
func Marshal(v any) []byte {
	var b bytes.Buffer
	writeValue(&b, v, 2, 0)
	return b.Bytes()
}

// Dumps renders v like json.dumps(v, ensure_ascii=False): one line, ", " and ": " separators.
func Dumps(v any) string {
	var b bytes.Buffer
	writeValue(&b, v, -1, 0)
	return b.String()
}

func writeValue(b *bytes.Buffer, v any, indent, level int) {
	switch v := v.(type) {
	case *Object:
		if v.Len() == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteByte('{')
		for i, k := range v.keys {
			if i > 0 {
				b.WriteByte(',')
				if indent < 0 {
					b.WriteByte(' ')
				}
			}
			newline(b, indent, level+1)
			writeString(b, k)
			b.WriteString(": ")
			writeValue(b, v.vals[k], indent, level+1)
		}
		newline(b, indent, level)
		b.WriteByte('}')
	case []any:
		if len(v) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteByte('[')
		for i, e := range v {
			if i > 0 {
				b.WriteByte(',')
				if indent < 0 {
					b.WriteByte(' ')
				}
			}
			newline(b, indent, level+1)
			writeValue(b, e, indent, level+1)
		}
		newline(b, indent, level)
		b.WriteByte(']')
	case string:
		writeString(b, v)
	case nil:
		b.WriteString("null")
	case bool:
		if v {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case Int:
		b.WriteString(v.String())
	case Float:
		b.WriteString(jsonFloat(float64(v)))
	default:
		panic(fmt.Sprintf("gotags: unexpected JSON value %T", v))
	}
}

func newline(b *bytes.Buffer, indent, level int) {
	if indent < 0 {
		return
	}
	b.WriteByte('\n')
	b.WriteString(strings.Repeat(" ", indent*level))
}

// writeString escapes like json.dump with ensure_ascii=False.
func writeString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r < 0x20:
			fmt.Fprintf(b, `\u%04x`, r)
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	b.WriteByte('"')
}

func jsonFloat(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case math.IsNaN(f):
		return "NaN"
	}
	return PyFloatRepr(f)
}

// PyFloatRepr formats f like repr(float) in the original.
func PyFloatRepr(f float64) string {
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	if math.IsNaN(f) {
		return "nan"
	}
	e := strconv.FormatFloat(f, 'e', -1, 64) // e.g. "1.5e-05", "1e+16"
	mant, expS, _ := strings.Cut(e, "e")
	exp, _ := strconv.Atoi(expS)
	if exp < -4 || exp >= 16 {
		sign := "+"
		if exp < 0 {
			sign = "-"
			exp = -exp
		}
		return fmt.Sprintf("%se%s%02d", mant, sign, exp)
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.ContainsAny(s, ".") {
		s += ".0"
	}
	return s
}

// PyStr formats a JSON value like str() in the original.
func PyStr(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case nil:
		return "None"
	case bool:
		if v {
			return "True"
		}
		return "False"
	case Int:
		return v.String()
	case Float:
		return PyFloatRepr(float64(v))
	default:
		return pyRepr(v)
	}
}

func pyRepr(v any) string {
	switch v := v.(type) {
	case string:
		return pyStrRepr(v)
	case *Object:
		parts := make([]string, 0, v.Len())
		for _, k := range v.keys {
			parts = append(parts, pyStrRepr(k)+": "+pyRepr(v.vals[k]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case []any:
		parts := make([]string, len(v))
		for i, e := range v {
			parts[i] = pyRepr(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return PyStr(v)
	}
}

func pyStrRepr(s string) string {
	q := "'"
	if strings.Contains(s, "'") && !strings.Contains(s, `"`) {
		q = `"`
	}
	var b strings.Builder
	b.WriteString(q)
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case string(r) == q:
			b.WriteString(`\` + q)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteString(q)
	return b.String()
}

// truthy is bool(v) in the original.
func truthy(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case Int:
		return v.Sign() != 0
	case Float:
		return v != 0
	case *Object:
		return v.Len() > 0
	case []any:
		return len(v) > 0
	}
	return true
}
