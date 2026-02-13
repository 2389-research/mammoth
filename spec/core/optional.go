// ABOUTME: OptionalField[T] implements 3-state JSON semantics: absent, null, or value.
// ABOUTME: This solves the "null vs missing" problem for partial update APIs.
package core

import (
	"bytes"
	"encoding/json"
)

// OptionalField represents a field that can be absent, explicitly null, or have a value.
// This maps to Rust's Option<Option<T>> pattern used in UpdateCard.body and CardUpdated.body.
//
//   - Set=false:             field absent from JSON (don't update)
//   - Set=true, Valid=false: field is JSON null (clear the value)
//   - Set=true, Valid=true:  field has a value (set to Value)
type OptionalField[T any] struct {
	Set   bool // Was this field present in the JSON?
	Valid bool // Is the value non-null?
	Value T
}

// Absent returns an OptionalField that represents a missing field.
func Absent[T any]() OptionalField[T] {
	return OptionalField[T]{}
}

// Null returns an OptionalField that represents an explicit null.
func Null[T any]() OptionalField[T] {
	return OptionalField[T]{Set: true}
}

// Present returns an OptionalField with a concrete value.
func Present[T any](v T) OptionalField[T] {
	return OptionalField[T]{Set: true, Valid: true, Value: v}
}

// MarshalJSON only emits JSON when the field is Set.
// If Set && !Valid, emits null. If Set && Valid, emits the value.
func (o OptionalField[T]) MarshalJSON() ([]byte, error) {
	if !o.Set {
		// This shouldn't normally be called for absent fields because
		// the struct-level marshaling handles omission via the custom
		// marshal on the parent struct. But if called directly, emit null.
		return []byte("null"), nil
	}
	if !o.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(o.Value)
}

// UnmarshalJSON sets the field state based on the JSON value.
// A JSON null sets Set=true, Valid=false. Any other value sets both true.
func (o *OptionalField[T]) UnmarshalJSON(data []byte) error {
	o.Set = true
	if bytes.Equal(data, []byte("null")) {
		o.Valid = false
		return nil
	}
	o.Valid = true
	return json.Unmarshal(data, &o.Value)
}
