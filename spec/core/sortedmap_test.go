// ABOUTME: Tests for OrderedMap sorted map implementation.
// ABOUTME: Covers CRUD operations, ordering, iteration, cloning, and JSON marshaling.
package core_test

import (
	"encoding/json"
	"math/rand"
	"testing"
	"time"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/oklog/ulid/v2"
)

// makeULID creates a deterministic ULID at a given millisecond timestamp.
// Using a fixed entropy source so tests are reproducible.
func makeULID(t *testing.T, ms uint64) ulid.ULID {
	t.Helper()
	entropy := rand.New(rand.NewSource(int64(ms)))
	id, err := ulid.New(ms, entropy)
	if err != nil {
		t.Fatalf("failed to create ULID: %v", err)
	}
	return id
}

// makeULIDs creates n ULIDs at increasing timestamps so they sort lexicographically
// in the order they were created.
func makeULIDs(t *testing.T, n int) []ulid.ULID {
	t.Helper()
	ids := make([]ulid.ULID, n)
	base := uint64(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli())
	for i := 0; i < n; i++ {
		ids[i] = makeULID(t, base+uint64(i)*1000)
	}
	return ids
}

func TestNewOrderedMap_CreatesEmptyMap(t *testing.T) {
	m := core.NewOrderedMap[ulid.ULID, string]()

	if m.Len() != 0 {
		t.Errorf("expected empty map with Len()=0, got %d", m.Len())
	}

	keys := m.Keys()
	if len(keys) != 0 {
		t.Errorf("expected empty keys slice, got %v", keys)
	}

	values := m.Values()
	if len(values) != 0 {
		t.Errorf("expected empty values slice, got %v", values)
	}
}

func TestSetGet_BasicOperations(t *testing.T) {
	m := core.NewOrderedMap[ulid.ULID, string]()
	ids := makeULIDs(t, 3)

	m.Set(ids[0], "alpha")
	m.Set(ids[1], "beta")
	m.Set(ids[2], "gamma")

	for i, expected := range []string{"alpha", "beta", "gamma"} {
		val, ok := m.Get(ids[i])
		if !ok {
			t.Errorf("expected key %s to be found", ids[i])
		}
		if val != expected {
			t.Errorf("expected value %q for key %s, got %q", expected, ids[i], val)
		}
	}
}

func TestGet_MissingKey_ReturnsFalse(t *testing.T) {
	m := core.NewOrderedMap[ulid.ULID, string]()
	missing := makeULID(t, 999999)

	val, ok := m.Get(missing)
	if ok {
		t.Errorf("expected ok=false for missing key, got ok=true with value %q", val)
	}
	if val != "" {
		t.Errorf("expected zero value for missing key, got %q", val)
	}
}

func TestSet_OverwritesExistingValue(t *testing.T) {
	m := core.NewOrderedMap[ulid.ULID, string]()
	id := makeULID(t, 1000)

	m.Set(id, "original")
	m.Set(id, "updated")

	val, ok := m.Get(id)
	if !ok {
		t.Fatal("expected key to be found after overwrite")
	}
	if val != "updated" {
		t.Errorf("expected overwritten value %q, got %q", "updated", val)
	}

	// Overwriting should not duplicate the key
	if m.Len() != 1 {
		t.Errorf("expected Len()=1 after overwrite, got %d", m.Len())
	}
	keys := m.Keys()
	if len(keys) != 1 {
		t.Errorf("expected 1 key after overwrite, got %d keys", len(keys))
	}
}

func TestDelete_RemovesEntry(t *testing.T) {
	m := core.NewOrderedMap[ulid.ULID, string]()
	ids := makeULIDs(t, 3)

	m.Set(ids[0], "alpha")
	m.Set(ids[1], "beta")
	m.Set(ids[2], "gamma")

	m.Delete(ids[1])

	_, ok := m.Get(ids[1])
	if ok {
		t.Error("expected deleted key to not be found")
	}
	if m.Len() != 2 {
		t.Errorf("expected Len()=2 after delete, got %d", m.Len())
	}
}

func TestDelete_MaintainsOrder(t *testing.T) {
	m := core.NewOrderedMap[ulid.ULID, string]()
	ids := makeULIDs(t, 4)

	m.Set(ids[0], "a")
	m.Set(ids[1], "b")
	m.Set(ids[2], "c")
	m.Set(ids[3], "d")

	// Delete the middle element
	m.Delete(ids[1])

	keys := m.Keys()
	expected := []ulid.ULID{ids[0], ids[2], ids[3]}
	if len(keys) != len(expected) {
		t.Fatalf("expected %d keys, got %d", len(expected), len(keys))
	}
	for i, k := range keys {
		if k != expected[i] {
			t.Errorf("keys[%d] = %s, expected %s", i, k, expected[i])
		}
	}

	vals := m.Values()
	expectedVals := []string{"a", "c", "d"}
	for i, v := range vals {
		if v != expectedVals[i] {
			t.Errorf("values[%d] = %q, expected %q", i, v, expectedVals[i])
		}
	}
}

func TestDelete_NonExistentKey_IsNoop(t *testing.T) {
	m := core.NewOrderedMap[ulid.ULID, string]()
	id := makeULID(t, 1000)
	missing := makeULID(t, 2000)

	m.Set(id, "value")
	m.Delete(missing) // should not panic or change anything

	if m.Len() != 1 {
		t.Errorf("expected Len()=1 after deleting non-existent key, got %d", m.Len())
	}
}

func TestLen_ReturnsCorrectCount(t *testing.T) {
	m := core.NewOrderedMap[ulid.ULID, int]()

	if m.Len() != 0 {
		t.Errorf("empty map: expected 0, got %d", m.Len())
	}

	ids := makeULIDs(t, 5)
	for i, id := range ids {
		m.Set(id, i)
		if m.Len() != i+1 {
			t.Errorf("after %d insertions: expected %d, got %d", i+1, i+1, m.Len())
		}
	}

	// Overwrite should not change count
	m.Set(ids[0], 999)
	if m.Len() != 5 {
		t.Errorf("after overwrite: expected 5, got %d", m.Len())
	}

	// Delete should decrement count
	m.Delete(ids[2])
	if m.Len() != 4 {
		t.Errorf("after delete: expected 4, got %d", m.Len())
	}
}

func TestKeys_ReturnsSortedOrder(t *testing.T) {
	m := core.NewOrderedMap[ulid.ULID, string]()
	ids := makeULIDs(t, 5)

	// Insert in reverse order to verify sorting
	for i := len(ids) - 1; i >= 0; i-- {
		m.Set(ids[i], ids[i].String())
	}

	keys := m.Keys()
	if len(keys) != len(ids) {
		t.Fatalf("expected %d keys, got %d", len(ids), len(keys))
	}

	// ULIDs created with increasing timestamps sort lexicographically
	for i := 0; i < len(keys)-1; i++ {
		if keys[i].String() >= keys[i+1].String() {
			t.Errorf("keys not in sorted order: keys[%d]=%s >= keys[%d]=%s",
				i, keys[i], i+1, keys[i+1])
		}
	}

	// Verify they match the original sorted IDs
	for i, k := range keys {
		if k != ids[i] {
			t.Errorf("keys[%d] = %s, expected %s", i, k, ids[i])
		}
	}
}

func TestKeys_ReturnsDefensiveCopy(t *testing.T) {
	m := core.NewOrderedMap[ulid.ULID, string]()
	ids := makeULIDs(t, 2)

	m.Set(ids[0], "a")
	m.Set(ids[1], "b")

	keys := m.Keys()
	// Mutate the returned slice
	keys[0] = makeULID(t, 999999999)

	// Original map keys should be unaffected
	originalKeys := m.Keys()
	if originalKeys[0] != ids[0] {
		t.Error("mutating returned Keys() slice should not affect the map")
	}
}

func TestValues_ReturnsSortedKeyOrder(t *testing.T) {
	m := core.NewOrderedMap[ulid.ULID, string]()
	ids := makeULIDs(t, 4)

	// Insert in scrambled order
	m.Set(ids[2], "c")
	m.Set(ids[0], "a")
	m.Set(ids[3], "d")
	m.Set(ids[1], "b")

	vals := m.Values()
	expected := []string{"a", "b", "c", "d"}
	if len(vals) != len(expected) {
		t.Fatalf("expected %d values, got %d", len(expected), len(vals))
	}
	for i, v := range vals {
		if v != expected[i] {
			t.Errorf("values[%d] = %q, expected %q", i, v, expected[i])
		}
	}
}

func TestRange_IteratesInOrder(t *testing.T) {
	m := core.NewOrderedMap[ulid.ULID, int]()
	ids := makeULIDs(t, 5)

	// Insert in reverse order
	for i := len(ids) - 1; i >= 0; i-- {
		m.Set(ids[i], i)
	}

	var visitedKeys []ulid.ULID
	var visitedVals []int

	m.Range(func(k ulid.ULID, v int) bool {
		visitedKeys = append(visitedKeys, k)
		visitedVals = append(visitedVals, v)
		return true
	})

	if len(visitedKeys) != 5 {
		t.Fatalf("expected 5 iterations, got %d", len(visitedKeys))
	}

	for i := 0; i < len(visitedKeys)-1; i++ {
		if visitedKeys[i].String() >= visitedKeys[i+1].String() {
			t.Errorf("Range not in sorted order at index %d", i)
		}
	}

	// Values should correspond to the sorted keys
	for i, k := range visitedKeys {
		if k != ids[visitedVals[i]] {
			t.Errorf("key-value mismatch at position %d", i)
		}
	}
}

func TestRange_StopsEarly(t *testing.T) {
	m := core.NewOrderedMap[ulid.ULID, string]()
	ids := makeULIDs(t, 5)

	for i, id := range ids {
		m.Set(id, string(rune('a'+i)))
	}

	count := 0
	m.Range(func(k ulid.ULID, v string) bool {
		count++
		return count < 3 // stop after 3 iterations
	})

	if count != 3 {
		t.Errorf("expected Range to stop after 3 iterations, got %d", count)
	}
}

func TestRange_EmptyMap(t *testing.T) {
	m := core.NewOrderedMap[ulid.ULID, string]()

	count := 0
	m.Range(func(k ulid.ULID, v string) bool {
		count++
		return true
	})

	if count != 0 {
		t.Errorf("expected 0 iterations on empty map, got %d", count)
	}
}

func TestClone_CreatesIndependentCopy(t *testing.T) {
	m := core.NewOrderedMap[ulid.ULID, string]()
	ids := makeULIDs(t, 3)

	m.Set(ids[0], "alpha")
	m.Set(ids[1], "beta")
	m.Set(ids[2], "gamma")

	clone := m.Clone()

	// Clone has same contents
	if clone.Len() != m.Len() {
		t.Errorf("clone Len()=%d, original Len()=%d", clone.Len(), m.Len())
	}
	for _, id := range ids {
		origVal, _ := m.Get(id)
		cloneVal, ok := clone.Get(id)
		if !ok {
			t.Errorf("clone missing key %s", id)
		}
		if cloneVal != origVal {
			t.Errorf("clone value %q != original value %q for key %s", cloneVal, origVal, id)
		}
	}

	// Mutating clone does not affect original
	newID := makeULID(t, 99999999)
	clone.Set(newID, "delta")
	clone.Set(ids[0], "modified")

	if m.Len() != 3 {
		t.Errorf("original Len() changed after clone mutation: got %d", m.Len())
	}
	origVal, _ := m.Get(ids[0])
	if origVal != "alpha" {
		t.Errorf("original value changed after clone mutation: got %q", origVal)
	}
	_, ok := m.Get(newID)
	if ok {
		t.Error("key added to clone should not appear in original")
	}

	// Mutating original does not affect clone
	m.Delete(ids[2])
	cloneVal, ok := clone.Get(ids[2])
	if !ok || cloneVal != "gamma" {
		t.Error("deleting from original should not affect clone")
	}
}

func TestClone_PreservesSortOrder(t *testing.T) {
	m := core.NewOrderedMap[ulid.ULID, int]()
	ids := makeULIDs(t, 5)

	// Insert in reverse order
	for i := len(ids) - 1; i >= 0; i-- {
		m.Set(ids[i], i)
	}

	clone := m.Clone()
	origKeys := m.Keys()
	cloneKeys := clone.Keys()

	if len(origKeys) != len(cloneKeys) {
		t.Fatalf("key count mismatch: original=%d, clone=%d", len(origKeys), len(cloneKeys))
	}
	for i := range origKeys {
		if origKeys[i] != cloneKeys[i] {
			t.Errorf("key order mismatch at %d: original=%s, clone=%s",
				i, origKeys[i], cloneKeys[i])
		}
	}
}

func TestMarshalJSON_ProducesSortedObject(t *testing.T) {
	m := core.NewOrderedMap[ulid.ULID, string]()
	ids := makeULIDs(t, 3)

	// Insert in reverse order
	m.Set(ids[2], "gamma")
	m.Set(ids[0], "alpha")
	m.Set(ids[1], "beta")

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	// Parse back as ordered JSON to verify key order
	var raw json.RawMessage = data
	var parsed map[string]string
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	// Verify all values are present and correct
	if parsed[ids[0].String()] != "alpha" {
		t.Errorf("expected 'alpha' for key %s, got %q", ids[0], parsed[ids[0].String()])
	}
	if parsed[ids[1].String()] != "beta" {
		t.Errorf("expected 'beta' for key %s, got %q", ids[1], parsed[ids[1].String()])
	}
	if parsed[ids[2].String()] != "gamma" {
		t.Errorf("expected 'gamma' for key %s, got %q", ids[2], parsed[ids[2].String()])
	}

	// Verify key order in the raw JSON by checking positions
	jsonStr := string(data)
	pos0 := findSubstringPos(jsonStr, ids[0].String())
	pos1 := findSubstringPos(jsonStr, ids[1].String())
	pos2 := findSubstringPos(jsonStr, ids[2].String())

	if pos0 >= pos1 || pos1 >= pos2 {
		t.Errorf("JSON keys are not in sorted order: positions %d, %d, %d in %s",
			pos0, pos1, pos2, jsonStr)
	}
}

func TestMarshalJSON_EmptyMap(t *testing.T) {
	m := core.NewOrderedMap[ulid.ULID, string]()

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("MarshalJSON on empty map failed: %v", err)
	}

	if string(data) != "{}" {
		t.Errorf("expected '{}' for empty map, got %q", string(data))
	}
}

func TestMarshalJSON_WithIntValues(t *testing.T) {
	m := core.NewOrderedMap[ulid.ULID, int]()
	ids := makeULIDs(t, 2)

	m.Set(ids[0], 42)
	m.Set(ids[1], 99)

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	var parsed map[string]int
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if parsed[ids[0].String()] != 42 {
		t.Errorf("expected 42 for key %s, got %d", ids[0], parsed[ids[0].String()])
	}
	if parsed[ids[1].String()] != 99 {
		t.Errorf("expected 99 for key %s, got %d", ids[1], parsed[ids[1].String()])
	}
}

func TestMarshalJSON_WithStructValues(t *testing.T) {
	type item struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	m := core.NewOrderedMap[ulid.ULID, item]()
	ids := makeULIDs(t, 2)

	m.Set(ids[0], item{Name: "foo", Count: 1})
	m.Set(ids[1], item{Name: "bar", Count: 2})

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	var parsed map[string]item
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if parsed[ids[0].String()].Name != "foo" {
		t.Errorf("expected 'foo', got %q", parsed[ids[0].String()].Name)
	}
	if parsed[ids[1].String()].Count != 2 {
		t.Errorf("expected count 2, got %d", parsed[ids[1].String()].Count)
	}
}

func TestInsertionOrder_DoesNotMatter(t *testing.T) {
	// Verify that regardless of insertion order, keys and values come out sorted.
	ids := makeULIDs(t, 6)

	// Build map in forward order
	m1 := core.NewOrderedMap[ulid.ULID, int]()
	for i, id := range ids {
		m1.Set(id, i)
	}

	// Build map in reverse order
	m2 := core.NewOrderedMap[ulid.ULID, int]()
	for i := len(ids) - 1; i >= 0; i-- {
		m2.Set(ids[i], i)
	}

	// Build map in scrambled order
	m3 := core.NewOrderedMap[ulid.ULID, int]()
	scrambled := []int{3, 0, 5, 1, 4, 2}
	for _, idx := range scrambled {
		m3.Set(ids[idx], idx)
	}

	keys1 := m1.Keys()
	keys2 := m2.Keys()
	keys3 := m3.Keys()

	for i := range keys1 {
		if keys1[i] != keys2[i] || keys2[i] != keys3[i] {
			t.Errorf("key order mismatch at index %d: %s, %s, %s",
				i, keys1[i], keys2[i], keys3[i])
		}
	}
}

// findSubstringPos returns the byte index of the first occurrence of substr in s,
// or -1 if not found.
func findSubstringPos(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
