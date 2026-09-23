package goproxy

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
)

var (
	// errBadUpstream indicates an upstream is in a bad state.
	errBadUpstream = errors.New("bad upstream")

	// errFetchTimedOut indicates a fetch operation has timed out.
	errFetchTimedOut = errors.New("fetch timed out")
)

// isBadUpstreamError reports whether err indicates an upstream is in a bad state.
func isBadUpstreamError(err error) bool {
	return errors.Is(err, errBadUpstream)
}

// isFetchTimedOutError reports whether err or any error it wraps indicates a
// fetch timeout.
func isFetchTimedOutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errFetchTimedOut) {
		return true
	}
	for err != nil {
		switch e := err.(type) {
		case interface{ Timeout() bool }:
			if e.Timeout() {
				return true
			}
		case interface{ As(any) bool }:
			var t interface {
				error
				Timeout() bool
			}
			if e.As(&t) && t.Timeout() {
				return true
			}
		}
		switch e := err.(type) {
		case interface{ Unwrap() error }:
			err = e.Unwrap()
		case interface{ Unwrap() []error }:
			return slices.ContainsFunc(e.Unwrap(), isFetchTimedOutError)
		default:
			return false
		}
	}
	return false
}

// internalError marks an error caused by a local operation.
type internalError struct{ err error }

// Error implements [error].
func (e *internalError) Error() string { return e.err.Error() }

// Unwrap returns the underlying error.
func (e *internalError) Unwrap() error { return e.err }

// notExistError is like [fs.ErrNotExist] but with a custom underlying error.
type notExistError struct{ err error }

// Error implements [error].
func (e *notExistError) Error() string { return e.err.Error() }

// Unwrap returns the underlying error.
func (e *notExistError) Unwrap() error { return e.err }

// Is reports whether the target is [fs.ErrNotExist].
func (notExistError) Is(target error) bool { return target == fs.ErrNotExist }

// notExistErrorf formats according to a format specifier and returns the string
// as a value that satisfies error that is equivalent to [fs.ErrNotExist].
func notExistErrorf(format string, v ...any) error {
	return &notExistError{err: fmt.Errorf(format, v...)}
}

// uncacheableError marks an error whose response must not be cached.
type uncacheableError struct{ err error }

// Error implements [error].
func (e *uncacheableError) Error() string { return e.err.Error() }

// Unwrap returns the underlying error.
func (e *uncacheableError) Unwrap() error { return e.err }
