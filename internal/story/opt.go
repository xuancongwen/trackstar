package story

import (
	"bytes"
	"encoding/json"
)

// Opt distinguishes "field absent" from "field set to null" in PATCH bodies.
type Opt[T any] struct {
	Set   bool
	Value *T // nil when the JSON value was null
}

func (o *Opt[T]) UnmarshalJSON(data []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		o.Value = nil
		return nil
	}
	o.Value = new(T)
	return json.Unmarshal(data, o.Value)
}

// Some returns an Opt holding v; Null returns an explicit null.
func Some[T any](v T) Opt[T] { return Opt[T]{Set: true, Value: &v} }
func Null[T any]() Opt[T]    { return Opt[T]{Set: true} }
