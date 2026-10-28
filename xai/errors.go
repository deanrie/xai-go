package xai

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Error is the error type returned for failed API calls. It wraps the gRPC
// status so callers can switch on Code or use errors.Is with the sentinel
// values below.
type Error struct {
	Code    codes.Code
	Message string
	err     error
}

func (e *Error) Error() string {
	return fmt.Sprintf("xai: %s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying gRPC status error.
func (e *Error) Unwrap() error { return e.err }

// Is lets errors.Is match an *Error against the sentinel errors by code.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code && t.Message == ""
}

// Sentinels for errors.Is. Each matches any *Error with that code.
var (
	ErrInvalidArgument    = &Error{Code: codes.InvalidArgument}
	ErrNotFound           = &Error{Code: codes.NotFound}
	ErrPermissionDenied   = &Error{Code: codes.PermissionDenied}
	ErrUnauthenticated    = &Error{Code: codes.Unauthenticated}
	ErrResourceExhausted  = &Error{Code: codes.ResourceExhausted}
	ErrDeadlineExceeded   = &Error{Code: codes.DeadlineExceeded}
	ErrUnavailable        = &Error{Code: codes.Unavailable}
	ErrCanceled           = &Error{Code: codes.Canceled}
	ErrFailedPrecondition = &Error{Code: codes.FailedPrecondition}
)

// wrapErr converts a gRPC error into *Error. Non-status errors (context
// cancellation that never reached the transport, local validation) are
// returned unchanged. A nil error stays nil.
func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	var already *Error
	if errors.As(err, &already) {
		return err
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	return &Error{Code: st.Code(), Message: st.Message(), err: err}
}

// ValidationError is returned before a request is sent when an argument is
// outside the values the API accepts. It names the parameter and the
// accepted values, so a misspelled literal never falls back to a default.
type ValidationError struct {
	Param    string
	Value    any
	Accepted []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("xai: invalid %s %v: must be one of %v", e.Param, e.Value, e.Accepted)
}

// Is reports a ValidationError as ErrInvalidArgument for callers who only
// distinguish by class.
func (e *ValidationError) Is(target error) bool { return target == ErrInvalidArgument }
