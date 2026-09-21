// Package apperr defines the error kinds services return; the API layer maps
// them to HTTP status codes. Anything else is an internal error.
package apperr

import (
	"errors"
	"fmt"
)

type Kind int

const (
	KindInvalid Kind = iota + 1
	KindUnauthorized
	KindForbidden
	KindNotFound
	KindConflict
	KindRateLimited
)

type Error struct {
	Kind    Kind
	Message string
}

func (e *Error) Error() string { return e.Message }

func Invalid(format string, args ...any) error {
	return &Error{Kind: KindInvalid, Message: fmt.Sprintf(format, args...)}
}

func Unauthorized(msg string) error { return &Error{Kind: KindUnauthorized, Message: msg} }
func Forbidden(msg string) error    { return &Error{Kind: KindForbidden, Message: msg} }
func NotFound(what string) error    { return &Error{Kind: KindNotFound, Message: what + " not found"} }
func Conflict(msg string) error     { return &Error{Kind: KindConflict, Message: msg} }
func RateLimited(msg string) error  { return &Error{Kind: KindRateLimited, Message: msg} }

// KindOf returns the kind of err, or 0 when it is not an application error.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return 0
}
