package dockerproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// object is a JSON object as the daemon reads it into a struct: its fields in
// order. The daemon's decoder (encoding/json) matches a field name ignoring
// case and takes the last of several, so an object is checked only after
// normalize has made every name exact and left one of each: otherwise
// "privileged": true would pass a check of "Privileged" and still reach the
// daemon.
type object struct {
	fields []field
}

type field struct {
	key string
	val json.RawMessage
}

// parseObject reads raw as an object; nil for JSON null or nothing.
func parseObject(raw json.RawMessage) (*object, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("want a JSON object")
	}
	o := &object{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("want a field name")
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		o.fields = append(o.fields, field{key: key, val: val})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("trailing data after the JSON object")
	}
	return o, nil
}

// normalize makes each field's name the exact one of allowed it matches,
// ignoring case as the daemon does, and refuses a field that matches none
// (deny by default) or a field given twice. where names the object in a
// refusal.
func (o *object) normalize(allowed []string, where string) error {
	if o == nil {
		return nil
	}
	seen := map[string]bool{}
	for i := range o.fields {
		name := ""
		for _, a := range allowed {
			if strings.EqualFold(o.fields[i].key, a) {
				name = a
				break
			}
		}
		if name == "" {
			return refuse("%s%s is not allowed", where, o.fields[i].key)
		}
		if seen[name] {
			return refuse("%s%s is given twice", where, name)
		}
		seen[name] = true
		o.fields[i].key = name
	}
	return nil
}

// raw is the value of a field, nil when it is absent or null.
func (o *object) raw(key string) json.RawMessage {
	if o == nil {
		return nil
	}
	for _, f := range o.fields {
		if f.key == key {
			if bytes.Equal(bytes.TrimSpace(f.val), []byte("null")) {
				return nil
			}
			return f.val
		}
	}
	return nil
}

// decode reads a field into v; false when it is absent or null.
func (o *object) decode(key string, v any) (bool, error) {
	raw := o.raw(key)
	if raw == nil {
		return false, nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return false, refuse("%s: %v", key, err)
	}
	return true, nil
}

// set gives a field a value, replacing it in place or adding it.
func (o *object) set(key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	for i := range o.fields {
		if o.fields[i].key == key {
			o.fields[i].val = raw
			return nil
		}
	}
	o.fields = append(o.fields, field{key: key, val: raw})
	return nil
}

// drop removes a field.
func (o *object) drop(key string) {
	o.fields = slices.DeleteFunc(o.fields, func(f field) bool { return f.key == key })
}

func (o *object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range o.fields {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := json.Marshal(f.key)
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(f.val)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// objects reads a field that is a list of objects.
func (o *object) objects(key string) ([]*object, error) {
	var raws []json.RawMessage
	if _, err := o.decode(key, &raws); err != nil {
		return nil, err
	}
	out := make([]*object, 0, len(raws))
	for _, raw := range raws {
		item, err := parseObject(raw)
		if err != nil {
			return nil, refuse("%s: %v", key, err)
		}
		if item == nil {
			item = &object{}
		}
		out = append(out, item)
	}
	return out, nil
}

// child reads a field that is an object; an empty one when absent.
func (o *object) child(key string) (*object, error) {
	child, err := parseObject(o.raw(key))
	if err != nil {
		return nil, refuse("%s: %v", key, err)
	}
	if child == nil {
		child = &object{}
	}
	return child, nil
}
