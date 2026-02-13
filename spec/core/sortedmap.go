// ABOUTME: OrderedMap provides a sorted map keyed by comparable types.
// ABOUTME: Equivalent to Rust's BTreeMap, used for deterministic card ordering by ULID.
package core

import (
	"encoding/json"
	"sort"
)

// OrderedMap maintains keys in sorted order, equivalent to BTreeMap in Rust.
// K must be comparable and support ordering via sort. In practice, K is ulid.ULID
// which implements fmt.Stringer for consistent ordering.
type OrderedMap[K interface {
	comparable
	String() string
}, V any] struct {
	data map[K]V
	keys []K
}

// NewOrderedMap creates an empty OrderedMap.
func NewOrderedMap[K interface {
	comparable
	String() string
}, V any]() *OrderedMap[K, V] {
	return &OrderedMap[K, V]{
		data: make(map[K]V),
		keys: nil,
	}
}

// Set inserts or updates a key-value pair, maintaining sorted key order.
func (m *OrderedMap[K, V]) Set(key K, val V) {
	if _, exists := m.data[key]; !exists {
		m.keys = append(m.keys, key)
		m.sortKeys()
	}
	m.data[key] = val
}

// Get retrieves a value by key. Returns the value and whether it was found.
func (m *OrderedMap[K, V]) Get(key K) (V, bool) {
	v, ok := m.data[key]
	return v, ok
}

// Delete removes a key-value pair.
func (m *OrderedMap[K, V]) Delete(key K) {
	if _, exists := m.data[key]; !exists {
		return
	}
	delete(m.data, key)
	for i, k := range m.keys {
		if k == key {
			m.keys = append(m.keys[:i], m.keys[i+1:]...)
			break
		}
	}
}

// Len returns the number of entries.
func (m *OrderedMap[K, V]) Len() int {
	return len(m.data)
}

// Keys returns all keys in sorted order.
func (m *OrderedMap[K, V]) Keys() []K {
	result := make([]K, len(m.keys))
	copy(result, m.keys)
	return result
}

// Values returns all values in key-sorted order.
func (m *OrderedMap[K, V]) Values() []V {
	result := make([]V, 0, len(m.keys))
	for _, k := range m.keys {
		result = append(result, m.data[k])
	}
	return result
}

// Range iterates over entries in sorted key order. Return false to stop.
func (m *OrderedMap[K, V]) Range(fn func(K, V) bool) {
	for _, k := range m.keys {
		if !fn(k, m.data[k]) {
			break
		}
	}
}

// Clone returns a shallow copy of the map.
func (m *OrderedMap[K, V]) Clone() *OrderedMap[K, V] {
	c := NewOrderedMap[K, V]()
	for _, k := range m.keys {
		c.Set(k, m.data[k])
	}
	return c
}

func (m *OrderedMap[K, V]) sortKeys() {
	sort.Slice(m.keys, func(i, j int) bool {
		return m.keys[i].String() < m.keys[j].String()
	})
}

// MarshalJSON serializes the map as a JSON object with sorted keys.
func (m *OrderedMap[K, V]) MarshalJSON() ([]byte, error) {
	// Build an ordered list of entries for JSON encoding
	type entry struct {
		Key string
		Val V
	}
	entries := make([]entry, 0, len(m.keys))
	for _, k := range m.keys {
		entries = append(entries, entry{Key: k.String(), Val: m.data[k]})
	}
	// Encode as a JSON object
	buf := []byte{'{'}
	for i, e := range entries {
		if i > 0 {
			buf = append(buf, ',')
		}
		keyJSON, err := json.Marshal(e.Key)
		if err != nil {
			return nil, err
		}
		valJSON, err := json.Marshal(e.Val)
		if err != nil {
			return nil, err
		}
		buf = append(buf, keyJSON...)
		buf = append(buf, ':')
		buf = append(buf, valJSON...)
	}
	buf = append(buf, '}')
	return buf, nil
}

// UnmarshalJSON is intentionally not implemented generically because
// K deserialization depends on the concrete type (e.g., ulid.ULID parsing).
// Consumers should deserialize into a map[string]V and rebuild the OrderedMap.
