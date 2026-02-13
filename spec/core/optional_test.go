// ABOUTME: Tests for OptionalField[T] three-state JSON semantics.
// ABOUTME: Covers absent, null, and present states with round-trip verification.
package core_test

import (
	"encoding/json"
	"testing"

	"github.com/2389-research/mammoth/spec/core"
)

func TestAbsent(t *testing.T) {
	opt := core.Absent[string]()
	if opt.Set {
		t.Error("Absent().Set should be false")
	}
	if opt.Valid {
		t.Error("Absent().Valid should be false")
	}
	if opt.Value != "" {
		t.Errorf("Absent().Value should be zero value, got %q", opt.Value)
	}
}

func TestNull(t *testing.T) {
	opt := core.Null[string]()
	if !opt.Set {
		t.Error("Null().Set should be true")
	}
	if opt.Valid {
		t.Error("Null().Valid should be false")
	}
}

func TestPresent(t *testing.T) {
	opt := core.Present("hello")
	if !opt.Set {
		t.Error("Present().Set should be true")
	}
	if !opt.Valid {
		t.Error("Present().Valid should be true")
	}
	if opt.Value != "hello" {
		t.Errorf("Present().Value = %q, want %q", opt.Value, "hello")
	}
}

func TestPresentInt(t *testing.T) {
	opt := core.Present(42)
	if !opt.Set {
		t.Error("Present(42).Set should be true")
	}
	if !opt.Valid {
		t.Error("Present(42).Valid should be true")
	}
	if opt.Value != 42 {
		t.Errorf("Present(42).Value = %d, want 42", opt.Value)
	}
}

// --- MarshalJSON ---

func TestMarshalJSON_Absent(t *testing.T) {
	opt := core.Absent[string]()
	data, err := json.Marshal(opt)
	if err != nil {
		t.Fatalf("MarshalJSON(Absent) error: %v", err)
	}
	// When marshaled directly (not via parent struct), absent emits "null"
	if string(data) != "null" {
		t.Errorf("MarshalJSON(Absent) = %s, want null", data)
	}
}

func TestMarshalJSON_Null(t *testing.T) {
	opt := core.Null[string]()
	data, err := json.Marshal(opt)
	if err != nil {
		t.Fatalf("MarshalJSON(Null) error: %v", err)
	}
	if string(data) != "null" {
		t.Errorf("MarshalJSON(Null) = %s, want null", data)
	}
}

func TestMarshalJSON_PresentString(t *testing.T) {
	opt := core.Present("world")
	data, err := json.Marshal(opt)
	if err != nil {
		t.Fatalf("MarshalJSON(Present) error: %v", err)
	}
	if string(data) != `"world"` {
		t.Errorf("MarshalJSON(Present) = %s, want %q", data, `"world"`)
	}
}

func TestMarshalJSON_PresentInt(t *testing.T) {
	opt := core.Present(99)
	data, err := json.Marshal(opt)
	if err != nil {
		t.Fatalf("MarshalJSON(Present(99)) error: %v", err)
	}
	if string(data) != "99" {
		t.Errorf("MarshalJSON(Present(99)) = %s, want 99", data)
	}
}

func TestMarshalJSON_PresentBool(t *testing.T) {
	opt := core.Present(true)
	data, err := json.Marshal(opt)
	if err != nil {
		t.Fatalf("MarshalJSON(Present(true)) error: %v", err)
	}
	if string(data) != "true" {
		t.Errorf("MarshalJSON(Present(true)) = %s, want true", data)
	}
}

// --- UnmarshalJSON ---

func TestUnmarshalJSON_Null(t *testing.T) {
	var opt core.OptionalField[string]
	err := json.Unmarshal([]byte("null"), &opt)
	if err != nil {
		t.Fatalf("UnmarshalJSON(null) error: %v", err)
	}
	if !opt.Set {
		t.Error("UnmarshalJSON(null).Set should be true")
	}
	if opt.Valid {
		t.Error("UnmarshalJSON(null).Valid should be false")
	}
}

func TestUnmarshalJSON_StringValue(t *testing.T) {
	var opt core.OptionalField[string]
	err := json.Unmarshal([]byte(`"test"`), &opt)
	if err != nil {
		t.Fatalf("UnmarshalJSON(string) error: %v", err)
	}
	if !opt.Set {
		t.Error("UnmarshalJSON(string).Set should be true")
	}
	if !opt.Valid {
		t.Error("UnmarshalJSON(string).Valid should be true")
	}
	if opt.Value != "test" {
		t.Errorf("UnmarshalJSON(string).Value = %q, want %q", opt.Value, "test")
	}
}

func TestUnmarshalJSON_IntValue(t *testing.T) {
	var opt core.OptionalField[int]
	err := json.Unmarshal([]byte("7"), &opt)
	if err != nil {
		t.Fatalf("UnmarshalJSON(int) error: %v", err)
	}
	if !opt.Set {
		t.Error("Set should be true")
	}
	if !opt.Valid {
		t.Error("Valid should be true")
	}
	if opt.Value != 7 {
		t.Errorf("Value = %d, want 7", opt.Value)
	}
}

// --- Absent via parent struct (field not present in JSON) ---

// wrapper is a parent struct used to verify that absent fields
// remain in the zero state when not present in JSON input.
type wrapper struct {
	Name core.OptionalField[string] `json:"name"`
	Age  core.OptionalField[int]    `json:"age"`
}

func TestUnmarshalJSON_AbsentViaParentStruct(t *testing.T) {
	input := `{}`
	var w wrapper
	err := json.Unmarshal([]byte(input), &w)
	if err != nil {
		t.Fatalf("Unmarshal({}) error: %v", err)
	}
	if w.Name.Set {
		t.Error("Name.Set should be false when field is absent")
	}
	if w.Name.Valid {
		t.Error("Name.Valid should be false when field is absent")
	}
	if w.Age.Set {
		t.Error("Age.Set should be false when field is absent")
	}
	if w.Age.Valid {
		t.Error("Age.Valid should be false when field is absent")
	}
}

func TestUnmarshalJSON_ParentStructNullField(t *testing.T) {
	input := `{"name": null}`
	var w wrapper
	err := json.Unmarshal([]byte(input), &w)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if !w.Name.Set {
		t.Error("Name.Set should be true when field is null")
	}
	if w.Name.Valid {
		t.Error("Name.Valid should be false when field is null")
	}
	// Age was absent
	if w.Age.Set {
		t.Error("Age.Set should be false when field is absent")
	}
}

func TestUnmarshalJSON_ParentStructPresentField(t *testing.T) {
	input := `{"name": "Alice", "age": 30}`
	var w wrapper
	err := json.Unmarshal([]byte(input), &w)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if !w.Name.Set {
		t.Error("Name.Set should be true")
	}
	if !w.Name.Valid {
		t.Error("Name.Valid should be true")
	}
	if w.Name.Value != "Alice" {
		t.Errorf("Name.Value = %q, want %q", w.Name.Value, "Alice")
	}
	if !w.Age.Set {
		t.Error("Age.Set should be true")
	}
	if !w.Age.Valid {
		t.Error("Age.Valid should be true")
	}
	if w.Age.Value != 30 {
		t.Errorf("Age.Value = %d, want 30", w.Age.Value)
	}
}

func TestUnmarshalJSON_ParentStructMixed(t *testing.T) {
	// name is present, age is null, and no other fields
	input := `{"name": "Bob", "age": null}`
	var w wrapper
	err := json.Unmarshal([]byte(input), &w)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if !w.Name.Set || !w.Name.Valid || w.Name.Value != "Bob" {
		t.Errorf("Name = {Set:%v Valid:%v Value:%q}, want present 'Bob'",
			w.Name.Set, w.Name.Valid, w.Name.Value)
	}
	if !w.Age.Set || w.Age.Valid {
		t.Errorf("Age = {Set:%v Valid:%v}, want null (Set:true Valid:false)",
			w.Age.Set, w.Age.Valid)
	}
}

// --- Round-trip tests ---

func TestRoundTrip_Present(t *testing.T) {
	original := core.Present("round-trip")
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded core.OptionalField[string]
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if !decoded.Set {
		t.Error("round-trip: Set should be true")
	}
	if !decoded.Valid {
		t.Error("round-trip: Valid should be true")
	}
	if decoded.Value != "round-trip" {
		t.Errorf("round-trip: Value = %q, want %q", decoded.Value, "round-trip")
	}
}

func TestRoundTrip_Null(t *testing.T) {
	original := core.Null[string]()
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded core.OptionalField[string]
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if !decoded.Set {
		t.Error("round-trip null: Set should be true")
	}
	if decoded.Valid {
		t.Error("round-trip null: Valid should be false")
	}
}

func TestRoundTrip_PresentInt(t *testing.T) {
	original := core.Present(256)
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded core.OptionalField[int]
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if !decoded.Set || !decoded.Valid || decoded.Value != 256 {
		t.Errorf("round-trip int: got {Set:%v Valid:%v Value:%d}, want present 256",
			decoded.Set, decoded.Valid, decoded.Value)
	}
}

func TestRoundTrip_ParentStructAllStates(t *testing.T) {
	// Marshal a struct where name is present and age is absent.
	// Since encoding/json doesn't know about our absent semantics at
	// the struct level (without a custom struct marshaler), the marshal
	// will include both fields. The key test here is that unmarshal
	// correctly distinguishes present values from null.
	input := `{"name": "Charlie"}`
	var w wrapper
	err := json.Unmarshal([]byte(input), &w)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Verify initial state: name present, age absent
	if !w.Name.Set || !w.Name.Valid || w.Name.Value != "Charlie" {
		t.Errorf("Name state wrong after unmarshal: {Set:%v Valid:%v Value:%q}",
			w.Name.Set, w.Name.Valid, w.Name.Value)
	}
	if w.Age.Set {
		t.Error("Age should be absent (Set=false) after unmarshal of partial JSON")
	}

	// Now re-marshal. Note: standard json.Marshal will include Age as "null"
	// since it doesn't know about the absent concept at the struct level.
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Unmarshal again and verify name is preserved
	var w2 wrapper
	err = json.Unmarshal(data, &w2)
	if err != nil {
		t.Fatalf("second Unmarshal error: %v", err)
	}

	if !w2.Name.Set || !w2.Name.Valid || w2.Name.Value != "Charlie" {
		t.Errorf("round-trip Name = {Set:%v Valid:%v Value:%q}, want present 'Charlie'",
			w2.Name.Set, w2.Name.Valid, w2.Name.Value)
	}
}

// --- Edge case: UnmarshalJSON with invalid data ---

func TestUnmarshalJSON_InvalidData(t *testing.T) {
	var opt core.OptionalField[int]
	err := json.Unmarshal([]byte(`"not-an-int"`), &opt)
	if err == nil {
		t.Error("UnmarshalJSON should return error for invalid int data")
	}
	// Set should still be true because UnmarshalJSON was called
	if !opt.Set {
		t.Error("Set should be true even on parse error (field was present)")
	}
}

// --- Edge case: complex value types ---

type nested struct {
	X int    `json:"x"`
	Y string `json:"y"`
}

func TestPresent_NestedStruct(t *testing.T) {
	val := nested{X: 1, Y: "ok"}
	opt := core.Present(val)

	data, err := json.Marshal(opt)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded core.OptionalField[nested]
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if !decoded.Set || !decoded.Valid {
		t.Errorf("nested round-trip: Set=%v Valid=%v, want both true",
			decoded.Set, decoded.Valid)
	}
	if decoded.Value.X != 1 || decoded.Value.Y != "ok" {
		t.Errorf("nested round-trip: Value = %+v, want {X:1 Y:ok}", decoded.Value)
	}
}

func TestNull_NestedStruct(t *testing.T) {
	opt := core.Null[nested]()
	data, err := json.Marshal(opt)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	if string(data) != "null" {
		t.Errorf("Marshal(Null[nested]) = %s, want null", data)
	}
}

// --- Edge case: slice value type ---

func TestPresent_SliceValue(t *testing.T) {
	opt := core.Present([]string{"a", "b", "c"})
	data, err := json.Marshal(opt)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded core.OptionalField[[]string]
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if !decoded.Set || !decoded.Valid {
		t.Fatalf("slice round-trip: Set=%v Valid=%v, want both true",
			decoded.Set, decoded.Valid)
	}
	if len(decoded.Value) != 3 || decoded.Value[0] != "a" || decoded.Value[1] != "b" || decoded.Value[2] != "c" {
		t.Errorf("slice round-trip: Value = %v, want [a b c]", decoded.Value)
	}
}

// --- Zero value semantics ---

func TestAbsent_ZeroValue(t *testing.T) {
	// A zero-value OptionalField should be equivalent to Absent()
	var opt core.OptionalField[string]
	if opt.Set {
		t.Error("zero value Set should be false")
	}
	if opt.Valid {
		t.Error("zero value Valid should be false")
	}
}
