package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// canonicalJSON writes a value in the JSON Canonicalization Scheme (RFC
// 8785): object keys sorted by their UTF-16 code units, no white space,
// strings escaped as ECMAScript does and nothing more, numbers as
// ECMAScript prints them. It takes what encoding/json decodes into: maps,
// slices, strings, float64, json.Number, bool and nil.
func canonicalJSON(v any) ([]byte, error) {
	var b strings.Builder
	if err := writeCanonical(&b, v); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

func writeCanonical(b *strings.Builder, v any) error {
	switch v := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if v {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		writeCanonicalString(b, v)
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return err
		}
		return writeCanonicalNumber(b, f)
	case float64:
		return writeCanonicalNumber(b, v)
	case int:
		return writeCanonicalNumber(b, float64(v))
	case int64:
		return writeCanonicalNumber(b, float64(v))
	case []any:
		b.WriteByte('[')
		for i, x := range v {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeCanonical(b, x); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return utf16Less(keys[i], keys[j]) })
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeCanonicalString(b, k)
			b.WriteByte(':')
			if err := writeCanonical(b, v[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("canonical JSON: %T is not a JSON value", v)
	}
	return nil
}

func utf16Less(a, b string) bool {
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}

func writeCanonicalString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// writeCanonicalNumber prints a number as ECMAScript's Number.toString.
func writeCanonicalNumber(b *strings.Builder, f float64) error {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return errors.New("canonical JSON: a number that is not finite")
	}
	if f == 0 {
		b.WriteByte('0')
		return nil
	}
	abs := math.Abs(f)
	if abs >= 1e21 || abs < 1e-6 {
		s := strconv.FormatFloat(f, 'e', -1, 64) // 1e+21, 1.5e-07
		mantissa, exp, _ := strings.Cut(s, "e")
		n, _ := strconv.Atoi(exp)
		sign := "+"
		if n < 0 {
			sign, n = "-", -n
		}
		b.WriteString(mantissa + "e" + sign + strconv.Itoa(n))
		return nil
	}
	b.WriteString(strconv.FormatFloat(f, 'f', -1, 64))
	return nil
}
