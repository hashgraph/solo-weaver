package core

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Parameter represents a key-value pair with an optional default value for the intent's parameters.
type Parameter struct {
	Key         string      `yaml:"key" json:"key"`
	Value       interface{} `yaml:"value" json:"value"`
	Description string      `yaml:"description" json:"description"`
}

// String returns the parameter value as a string.
func (p *Parameter) String(empty string) string {
	switch v := p.Value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case fmt.Stringer:
		return v.String()
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", v)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%v", v)
	case bool:
		return strconv.FormatBool(v)
	default:
		return empty
	}
}

// Int returns the parameter value as an integer.
func (p *Parameter) Int(empty int) int {
	switch v := p.Value.(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint:
		return int(v)
	case uint8:
		return int(v)
	case uint16:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return i
		}
	case bool:
		if v {
			return 1
		}
		return 0
	}
	return empty
}

// Int64 returns the parameter value as an int64.
func (p *Parameter) Int64(empty int64) int64 {
	switch v := p.Value.(type) {
	case int:
		return int64(v)
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case uint:
		return int64(v)
	case uint8:
		return int64(v)
	case uint16:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		return int64(v)
	case float32:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		if i, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return i
		}
	case bool:
		if v {
			return 1
		}
		return 0
	}
	return empty
}

// Bool returns the parameter value as a boolean.
func (p *Parameter) Bool(empty bool) bool {
	switch v := p.Value.(type) {
	case bool:
		return v
	case string:
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	case int, int8, int16, int32, int64:
		return p.Int(0) != 0
	case uint, uint8, uint16, uint32, uint64:
		return p.Int(0) != 0
	}
	return empty
}

// Float64 returns the parameter value as a float64.
func (p *Parameter) Float64(empty float64) float64 {
	switch v := p.Value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int, int8, int16, int32, int64:
		return float64(p.Int(0))
	case uint, uint8, uint16, uint32, uint64:
		return float64(p.Int(0))
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return empty
}

// StringSlice returns the parameter value as a slice of strings.
func (p *Parameter) StringSlice(empty []string) []string {
	switch v := p.Value.(type) {
	case []string:
		return v
	case []interface{}:
		res := make([]string, 0, len(v))
		for _, e := range v {
			res = append(res, fmt.Sprint(e))
		}
		return res
	case string:
		trim := strings.TrimSpace(v)
		if trim == "" {
			return empty
		}
		parts := strings.Split(trim, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	default:
		return empty
	}
}

// StringMap returns the parameter value as a map[string]string.
func (p *Parameter) StringMap(empty map[string]string) map[string]string {
	switch v := p.Value.(type) {
	case map[string]string:
		return v
	case map[string]interface{}:
		out := make(map[string]string, len(v))
		for k, val := range v {
			out[k] = fmt.Sprint(val)
		}
		return out
	}
	return empty
}

// Duration returns the parameter value as time.Duration.
func (p *Parameter) Duration(empty time.Duration) time.Duration {
	switch v := p.Value.(type) {
	case time.Duration:
		return v
	case string:
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil {
			return d
		}
	case int, int8, int16, int32, int64:
		return time.Duration(p.Int64(0)) * time.Second
	case float32, float64:
		return time.Duration(p.Float64(0) * float64(time.Second))
	}
	return empty
}

// Clone returns a deep copy of the Parameter.
func (p *Parameter) Clone() *Parameter {
	cp := Parameter{Key: p.Key, Description: p.Description}
	if p.Value == nil {
		return &cp
	}
	cp.Value = clone(p.Value)
	return &cp
}

func clone(src interface{}) interface{} {
	if src == nil {
		return nil
	}

	// Prefer explicit Clone methods if available.
	if cl, ok := src.(interface{ Clone() interface{} }); ok {
		return cl.Clone()
	}
	if clp, ok := src.(interface{ Clone() *Parameter }); ok {
		return clp.Clone()
	}

	switch v := src.(type) {
	case string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return v
	case []byte:
		b := make([]byte, len(v))
		copy(b, v)
		return b
	case []string:
		s := make([]string, len(v))
		copy(s, v)
		return s
	case []interface{}:
		res := make([]interface{}, len(v))
		for i, e := range v {
			res[i] = clone(e)
		}
		return res
	case map[string]string:
		m := make(map[string]string, len(v))
		for k, val := range v {
			m[k] = val
		}
		return m
	case map[string]interface{}:
		m := make(map[string]interface{}, len(v))
		for k, val := range v {
			m[k] = clone(val)
		}
		return m
	default:
		// Fallback: YAML deep-copy into a new value of the same type.
		b, err := yaml.Marshal(src)
		if err != nil {
			return src
		}
		t := reflect.TypeOf(src)
		dstPtr := reflect.New(t)
		if err := yaml.Unmarshal(b, dstPtr.Interface()); err == nil {
			return reflect.Indirect(dstPtr).Interface()
		}
		// Last resort: return original (shallow).
		return src
	}
}
