// SPDX-License-Identifier: Apache-2.0

package models

type StringMap map[string]string

func NewStringMap() StringMap {
	return make(StringMap)
}

func (s StringMap) Clone() (StringMap, error) {
	if s == nil {
		return nil, nil
	}
	clone := make(StringMap, len(s))
	for k, v := range s {
		clone[k] = v
	}
	return clone, nil
}

func (s StringMap) Get(key string) (string, bool) {
	value, exists := s[key]
	return value, exists
}

func (s StringMap) Set(key, value string) {
	s[key] = value
}

func (s StringMap) Delete(key string) {
	delete(s, key)
}

func (s StringMap) Keys() []string {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	return keys
}

func (s StringMap) Values() []string {
	values := make([]string, 0, len(s))
	for _, v := range s {
		values = append(values, v)
	}
	return values
}

func (s StringMap) Items() map[string]string {
	items := make(map[string]string, len(s))
	for k, v := range s {
		items[k] = v
	}
	return items
}

func (s StringMap) Merge(other StringMap) {
	for k, v := range other {
		s[k] = v
	}
}

func (s StringMap) IsEqual(other StringMap) bool {
	return IsEqualMap(s, other)
}

// IsEqualMap compares two automa.StateBag values using Items()
func IsEqualMap(a, b StringMap) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		bv, ok := b[k]
		if !ok || v != bv {
			return false
		}
	}
	return true
}
