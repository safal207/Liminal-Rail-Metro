package codexadapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"unicode/utf8"
)

// Decode accepts exactly one bounded UTF-8 JSON object. It rejects duplicate,
// unknown and incorrectly cased fields, invalid Unicode, null scalars and deep
// nesting before Go's normally permissive struct decoder sees them.
func Decode(reader io.Reader, value any, maxBytes int64) error {
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > maxBytes {
		return errors.New("JSON exceeds byte limit")
	}
	if !utf8.Valid(data) {
		return errors.New("JSON must be valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := checkValue(decoder, reflect.TypeOf(value).Elem(), 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("expected exactly one JSON value")
	}
	// encoding/json replaces unpaired UTF-16 escapes; reject those too.
	if !validEscapes(data) {
		return errors.New("invalid Unicode escape")
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}

func checkValue(d *json.Decoder, typ reflect.Type, depth int) error {
	if depth > 32 {
		return errors.New("JSON nesting exceeds limit")
	}
	for typ != nil && typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return errors.New("explicit null is not accepted")
	}
	delim, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			keyToken, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return errors.New("duplicate or invalid JSON key")
			}
			seen[key] = true
			var child reflect.Type
			if typ != nil && typ.Kind() == reflect.Struct {
				for i := 0; i < typ.NumField(); i++ {
					field := typ.Field(i)
					name := field.Tag.Get("json")
					for j, c := range name {
						if c == ',' {
							name = name[:j]
							break
						}
					}
					if key == name {
						child = field.Type
						break
					}
				}
				if child == nil {
					return fmt.Errorf("unknown JSON field %q", key)
				}
			} else if typ != nil && typ.Kind() == reflect.Map {
				child = typ.Elem()
			}
			if err := checkValue(d, child, depth+1); err != nil {
				return err
			}
		}
	case '[':
		var child reflect.Type
		if typ != nil && typ.Kind() == reflect.Slice {
			child = typ.Elem()
		}
		for d.More() {
			if err := checkValue(d, child, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	_, err = d.Token()
	return err
}

func validEscapes(data []byte) bool {
	// Decode each JSON string with strconv's UTF-16 handling avoided: inspect
	// raw \uXXXX pairs directly. JSON syntax was already checked above.
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) || data[i] != 'u' {
			continue
		}
		var code uint16
		if _, err := fmt.Sscanf(string(data[i+1:i+5]), "%04x", &code); err != nil {
			return false
		}
		i += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return false
		}
		if code < 0xd800 || code > 0xdbff {
			continue
		}
		if i+6 >= len(data) || string(data[i+1:i+3]) != `\u` {
			return false
		}
		var low uint16
		if _, err := fmt.Sscanf(string(data[i+3:i+7]), "%04x", &low); err != nil || low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}
