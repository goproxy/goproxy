package goproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"testing"
)

func TestIsBadUpstreamError(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{"Nil", nil, false},
		{"Direct", errBadUpstream, true},
		{"Wrapped", fmt.Errorf("fetch failed: %w", errBadUpstream), true},
		{"Joined", errors.Join(io.EOF, errBadUpstream), true},
		{"NotExist", &notExistError{err: errBadUpstream}, true},
		{"UncacheableNotExist", &uncacheableNotExistError{notExistError{err: errBadUpstream}}, true},
		{"SameMessage", errors.New("bad upstream"), false},
		{"MessageContains", notExistErrorf("unknown revision bad upstream"), false},
		{"FetchTimedOut", errFetchTimedOut, false},
		{"OtherError", io.EOF, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got, want := isBadUpstreamError(tt.err), tt.want; got != want {
				t.Errorf("got %t, want %t", got, want)
			}
		})
	}
}

type testTimeoutError struct {
	error
	timeout bool
}

func (e testTimeoutError) Timeout() bool { return e.timeout }

type testAsError struct{ error }

func (e testAsError) As(target any) bool { return errors.As(e.error, target) }

func TestIsFetchTimedOutError(t *testing.T) {
	timeoutErr := testTimeoutError{errors.New("operation timed out"), true}
	nonTimeoutErr := testTimeoutError{errors.New("operation failed"), false}
	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{"Nil", nil, false},
		{"Direct", errFetchTimedOut, true},
		{"Wrapped", fmt.Errorf("fetch failed: %w", errFetchTimedOut), true},
		{"Joined", errors.Join(io.EOF, errFetchTimedOut), true},
		{"NotExist", &notExistError{err: errFetchTimedOut}, true},
		{"UncacheableNotExist", &uncacheableNotExistError{notExistError{err: errFetchTimedOut}}, true},
		{"Timeout", timeoutErr, true},
		{"WrappedTimeout", fmt.Errorf("fetch failed: %w", timeoutErr), true},
		{"JoinedTimeout", errors.Join(io.EOF, timeoutErr), true},
		{"JoinedNonTimeoutFirst", errors.Join(nonTimeoutErr, timeoutErr), true},
		{"JoinedTimeoutFirst", errors.Join(timeoutErr, nonTimeoutErr), true},
		{"NestedJoinedTimeout", errors.Join(nonTimeoutErr, errors.Join(nonTimeoutErr, timeoutErr)), true},
		{"MultipleWrappedTimeout", fmt.Errorf("fetch failed: %w, %w", nonTimeoutErr, timeoutErr), true},
		{"URLWrappedTimeout", &url.Error{Op: "Get", URL: "https://example.com", Err: fmt.Errorf("read failed: %w", os.ErrDeadlineExceeded)}, true},
		{"CustomAsTimeout", testAsError{timeoutErr}, true},
		{"JoinedCustomAsTimeout", errors.Join(nonTimeoutErr, testAsError{timeoutErr}), true},
		{"NonTimeout", nonTimeoutErr, false},
		{"WrappedNonTimeout", fmt.Errorf("fetch failed: %w", nonTimeoutErr), false},
		{"JoinedNonTimeouts", errors.Join(nonTimeoutErr, nonTimeoutErr), false},
		{"URLWrappedNonTimeout", &url.Error{Op: "Get", URL: "https://example.com", Err: fmt.Errorf("read failed: %w", nonTimeoutErr)}, false},
		{"CustomAsNonTimeout", testAsError{nonTimeoutErr}, false},
		{"CustomAsOtherError", testAsError{io.EOF}, false},
		{"ContextDeadline", context.DeadlineExceeded, true},
		{"WrappedContextDeadline", fmt.Errorf("fetch failed: %w", context.DeadlineExceeded), true},
		{"IODeadline", os.ErrDeadlineExceeded, true},
		{"WrappedIODeadline", fmt.Errorf("read failed: %w", os.ErrDeadlineExceeded), true},
		{"ContextCanceled", context.Canceled, false},
		{"WrappedContextCanceled", fmt.Errorf("fetch failed: %w", context.Canceled), false},
		{"SameMessage", errors.New("fetch timed out"), false},
		{"MessageContains", notExistErrorf("unknown revision fetch timed out"), false},
		{"BadUpstream", errBadUpstream, false},
		{"OtherError", fs.ErrNotExist, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got, want := isFetchTimedOutError(tt.err), tt.want; got != want {
				t.Errorf("got %t, want %t", got, want)
			}
		})
	}
}

func TestInternalError(t *testing.T) {
	cause := &fs.PathError{Op: "open", Path: "file", Err: fs.ErrNotExist}
	e := &internalError{err: cause}
	if got, want := e.Error(), cause.Error(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := errors.Unwrap(e); got != cause {
		t.Errorf("got %v, want the original path error", got)
	}
	for _, tt := range []struct {
		name string
		err  error
	}{
		{"Direct", e},
		{"Wrapped", fmt.Errorf("fetch failed: %w", e)},
		{"Joined", errors.Join(io.EOF, e)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := errors.AsType[*internalError](tt.err); !ok || got != e {
				t.Errorf("got %v, want the original internal error", got)
			}
			if got, ok := errors.AsType[*fs.PathError](tt.err); !ok || got != cause {
				t.Errorf("got %v, want the original path error", got)
			}
			if !errors.Is(tt.err, fs.ErrNotExist) {
				t.Errorf("got error %v, want an error matching %v", tt.err, fs.ErrNotExist)
			}
			if errors.Is(tt.err, errBadUpstream) {
				t.Errorf("unexpected error matching %v: %v", errBadUpstream, tt.err)
			}
		})
	}
}

func TestNotExistError(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			err     error
			wantErr error
		}{
			{"EmptyMessage", notExistErrorf(""), errors.New("")},
			{"CustomMessage", notExistErrorf("foobar"), errors.New("foobar")},
			{"NotExist", notExistErrorf("foobar"), fs.ErrNotExist},
		} {
			t.Run(tt.name, func(t *testing.T) {
				if got, want := tt.err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			})
		}
	})

	t.Run("ErrorIs", func(t *testing.T) {
		e := &notExistError{err: errors.New("foobar")}
		for _, tt := range []struct {
			name   string
			err    error
			wantIs bool
		}{
			{"NotExist", fs.ErrNotExist, true},
			{"EOF", io.EOF, false},
		} {
			t.Run(tt.name, func(t *testing.T) {
				if got, want := e.Is(tt.err), tt.wantIs; got != want {
					t.Errorf("got %t, want %t", got, want)
				}
				if got, want := errors.Is(e, tt.err), tt.wantIs; got != want {
					t.Errorf("got %t, want %t", got, want)
				}
			})
		}
	})
}

func TestUncacheableNotExistError(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		for _, tt := range []struct {
			name        string
			err         error
			wantMessage string
		}{
			{"EmptyMessage", uncacheableNotExistErrorf(""), ""},
			{"CustomMessage", uncacheableNotExistErrorf("foobar"), "foobar"},
			{"FormattedMessage", uncacheableNotExistErrorf("module %s@%s", "example.com", "v1.0.0"), "module example.com@v1.0.0"},
			{"PercentMessage", uncacheableNotExistErrorf("%s", "module %s unavailable"), "module %s unavailable"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				if got, want := tt.err.Error(), tt.wantMessage; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if _, ok := errors.AsType[*uncacheableNotExistError](tt.err); !ok {
					t.Errorf("got %T, want *uncacheableNotExistError", tt.err)
				}
				if _, ok := errors.AsType[*notExistError](tt.err); !ok {
					t.Errorf("got %T, want an error wrapping *notExistError", tt.err)
				}
				if !errors.Is(tt.err, fs.ErrNotExist) {
					t.Error("expected match for fs.ErrNotExist")
				}
			})
		}
	})

	t.Run("ErrorChain", func(t *testing.T) {
		cause := &fs.PathError{Op: "open", Path: "file", Err: fs.ErrNotExist}
		err := &uncacheableNotExistError{notExistError{err: cause}}
		if got, want := err.Error(), cause.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if got := errors.Unwrap(err); got != &err.notExistError {
			t.Errorf("got %v, want the embedded not-exist error", got)
		}
		if got := errors.Unwrap(&err.notExistError); got != cause {
			t.Errorf("got %v, want the original path error", got)
		}
		for _, tt := range []struct {
			name string
			err  error
		}{
			{"Direct", err},
			{"Wrapped", fmt.Errorf("request failed: %w", err)},
			{"Nested", &notExistError{err: err}},
			{"Joined", errors.Join(errors.New("request failed"), err)},
			{"JoinedAbsence", errors.Join(notExistErrorf("module unavailable"), err)},
		} {
			t.Run(tt.name, func(t *testing.T) {
				if got, ok := errors.AsType[*uncacheableNotExistError](tt.err); !ok || got != err {
					t.Errorf("got %v, want the original uncacheable error", got)
				}
				if _, ok := errors.AsType[*notExistError](tt.err); !ok {
					t.Errorf("got %T, want an error wrapping *notExistError", tt.err)
				}
				if got, ok := errors.AsType[*fs.PathError](tt.err); !ok || got != cause {
					t.Errorf("got %v, want the original path error", got)
				}
				if !errors.Is(tt.err, fs.ErrNotExist) {
					t.Error("expected match for fs.ErrNotExist")
				}
				if errors.Is(tt.err, fs.ErrPermission) {
					t.Error("unexpected match for fs.ErrPermission")
				}
			})
		}
	})
}

func TestHTTPError(t *testing.T) {
	cause := &url.Error{Op: "Get", URL: "https://example.com", Err: fs.ErrNotExist}
	e := &httpError{err: cause}
	if got, want := e.Error(), cause.Error(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := errors.Unwrap(e); got != cause {
		t.Errorf("got %v, want the original URL error", got)
	}
	for _, tt := range []struct {
		name string
		err  error
	}{
		{"Direct", e},
		{"Wrapped", fmt.Errorf("fetch failed: %w", e)},
		{"Joined", errors.Join(io.EOF, e)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := errors.AsType[*httpError](tt.err); !ok || got != e {
				t.Errorf("got %v, want the original HTTP error", got)
			}
			if got, ok := errors.AsType[*url.Error](tt.err); !ok || got != cause {
				t.Errorf("got %v, want the original URL error", got)
			}
			if !errors.Is(tt.err, fs.ErrNotExist) {
				t.Errorf("got error %v, want an error matching %v", tt.err, fs.ErrNotExist)
			}
			if errors.Is(tt.err, errBadUpstream) {
				t.Errorf("unexpected error matching %v: %v", errBadUpstream, tt.err)
			}
		})
	}
}
