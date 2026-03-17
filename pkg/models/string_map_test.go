package models

import (
	"reflect"
	"testing"
)

func makeSample() StringMap {
	m := NewStringMap()
	m.Set("a", "1")
	m.Set("b", "2")
	m.Set("c", "3")
	return m
}

func asSet(s []string) map[string]struct{} {
	out := make(map[string]struct{}, len(s))
	for _, v := range s {
		out[v] = struct{}{}
	}
	return out
}

func TestNewStringMap_SetGetDelete(t *testing.T) {
	m := NewStringMap()
	if m == nil {
		t.Fatal("NewStringMap returned nil")
	}

	m.Set("k", "v")
	if v, ok := m.Get("k"); !ok || v != "v" {
		t.Fatalf("Get returned (%q, %v), want (\"v\", true)", v, ok)
	}

	m.Delete("k")
	if _, ok := m.Get("k"); ok {
		t.Fatal("Delete did not remove key")
	}
}

func TestClone_NilAndIndependentCopy(t *testing.T) {
	var nilMap StringMap
	clone, err := nilMap.Clone()
	if err != nil {
		t.Fatalf("Clone on nil returned error: %v", err)
	}
	if clone != nil {
		t.Fatalf("Clone on nil should return nil, got %#v", clone)
	}

	orig := makeSample()
	c, err := orig.Clone()
	if err != nil {
		t.Fatalf("Clone returned error: %v", err)
	}
	if !IsEqualMap(orig, c) {
		t.Fatalf("clone not equal to original: orig=%v clone=%v", orig, c)
	}

	// ensure independent copy
	orig.Set("a", "X")
	if got, _ := c.Get("a"); got == "X" {
		t.Fatal("clone changed when original modified (not independent)")
	}
}

func TestKeysValuesItems(t *testing.T) {
	m := makeSample()

	keys := m.Keys()
	values := m.Values()
	items := m.Items()

	if len(keys) != len(m) {
		t.Fatalf("Keys length mismatch: got %d, want %d", len(keys), len(m))
	}
	if len(values) != len(m) {
		t.Fatalf("Values length mismatch: got %d, want %d", len(values), len(m))
	}
	if len(items) != len(m) {
		t.Fatalf("Items length mismatch: got %d, want %d", len(items), len(m))
	}

	// Keys set matches item keys
	if !reflect.DeepEqual(asSet(keys), asSet(m.Keys())) {
		t.Fatalf("Keys mismatch sets: %v vs %v", keys, m.Keys())
	}

	// Items should be a copy: mutating returned map should not affect original
	items["a"] = "changed"
	if v, _ := m.Get("a"); v == "changed" {
		t.Fatal("Items returned map is not a copy")
	}
}

func TestMerge(t *testing.T) {
	a := NewStringMap()
	a.Set("x", "1")
	a.Set("y", "2")

	b := NewStringMap()
	b.Set("y", "20")
	b.Set("z", "3")

	a.Merge(b)

	// a should contain merged values, with b overwriting y
	expect := map[string]string{"x": "1", "y": "20", "z": "3"}
	if !reflect.DeepEqual(a.Items(), expect) {
		t.Fatalf("Merge result incorrect: got %v want %v", a.Items(), expect)
	}
	// b must remain unchanged
	if v, _ := b.Get("y"); v != "20" {
		t.Fatalf("source map changed after Merge: b = %v", b.Items())
	}
}

func TestIsEqualAndIsEqualMap(t *testing.T) {
	a := makeSample()
	b := makeSample()

	if !a.IsEqual(b) {
		t.Fatalf("expected maps to be equal: %v vs %v", a, b)
	}

	// different sizes
	b.Set("d", "4")
	if a.IsEqual(b) {
		t.Fatalf("expected maps to be different after mutation: %v vs %v", a, b)
	}

	// nil vs nil
	var n1, n2 StringMap
	if !IsEqualMap(n1, n2) {
		t.Fatal("nil maps should be equal")
	}
	if IsEqualMap(n1, makeSample()) {
		t.Fatal("nil and non-nil should not be equal")
	}
}
