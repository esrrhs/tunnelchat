package dht

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
)

// Minimal bencode for KRPC dictionaries. Values are int64, string, []byte,
// []any, or map[string]any.
func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeValue(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeValue(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case int:
		fmt.Fprintf(buf, "i%de", x)
	case int64:
		fmt.Fprintf(buf, "i%de", x)
	case string:
		fmt.Fprintf(buf, "%d:%s", len(x), x)
	case []byte:
		fmt.Fprintf(buf, "%d:", len(x))
		buf.Write(x)
	case []any:
		buf.WriteByte('l')
		for _, it := range x {
			if err := writeValue(buf, it); err != nil {
				return err
			}
		}
		buf.WriteByte('e')
	case map[string]any:
		buf.WriteByte('d')
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := writeValue(buf, k); err != nil {
				return err
			}
			if err := writeValue(buf, x[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('e')
	default:
		return fmt.Errorf("unsupported bencode type %T", v)
	}
	return nil
}

func decode(data []byte) (any, error) {
	v, rest, err := parseValue(data)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("trailing bencode bytes")
	}
	return v, nil
}

func parseValue(data []byte) (any, []byte, error) {
	if len(data) == 0 {
		return nil, nil, fmt.Errorf("empty bencode")
	}
	switch data[0] {
	case 'i':
		end := bytes.IndexByte(data, 'e')
		if end < 2 {
			return nil, nil, fmt.Errorf("bad int")
		}
		n, err := strconv.ParseInt(string(data[1:end]), 10, 64)
		if err != nil {
			return nil, nil, err
		}
		return n, data[end+1:], nil
	case 'l':
		var list []any
		rest := data[1:]
		for {
			if len(rest) == 0 {
				return nil, nil, fmt.Errorf("unterminated list")
			}
			if rest[0] == 'e' {
				return list, rest[1:], nil
			}
			v, r, err := parseValue(rest)
			if err != nil {
				return nil, nil, err
			}
			list = append(list, v)
			rest = r
		}
	case 'd':
		m := make(map[string]any)
		rest := data[1:]
		for {
			if len(rest) == 0 {
				return nil, nil, fmt.Errorf("unterminated dict")
			}
			if rest[0] == 'e' {
				return m, rest[1:], nil
			}
			k, r, err := parseValue(rest)
			if err != nil {
				return nil, nil, err
			}
			ks, ok := k.(string)
			if !ok {
				return nil, nil, fmt.Errorf("dict key not string")
			}
			v, r2, err := parseValue(r)
			if err != nil {
				return nil, nil, err
			}
			m[ks] = v
			rest = r2
		}
	default:
		if data[0] < '0' || data[0] > '9' {
			return nil, nil, fmt.Errorf("bad bencode tag %q", data[0])
		}
		colon := bytes.IndexByte(data, ':')
		if colon < 1 {
			return nil, nil, fmt.Errorf("bad string")
		}
		n, err := strconv.Atoi(string(data[:colon]))
		if err != nil || n < 0 || colon+1+n > len(data) {
			return nil, nil, fmt.Errorf("bad string length")
		}
		s := string(data[colon+1 : colon+1+n])
		return s, data[colon+1+n:], nil
	}
}

func asDict(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func asString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func asList(v any) ([]any, bool) {
	l, ok := v.([]any)
	return l, ok
}

func asInt(v any) (int64, bool) {
	n, ok := v.(int64)
	return n, ok
}
