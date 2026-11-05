package fcgi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// The sentinels name the rows of the failure table in docs/design.md. They
// are the Err of an *Error and match with errors.Is.
var (
	ErrBodyTooLarge     = errors.New("fcgi: request body exceeds the limit")     // 413
	ErrBadBody          = errors.New("fcgi: request body unreadable")            // 400
	ErrUnreachable      = errors.New("fcgi: responder unreachable")              // 502
	ErrNoSlot           = errors.New("fcgi: no free responder slot")             // 503
	ErrTimeout          = errors.New("fcgi: deadline reached")                   // 504
	ErrBadReply         = errors.New("fcgi: bad responder reply")                // 502
	ErrResponseTooLarge = errors.New("fcgi: responder output exceeds the limit") // 502
	ErrShuttingDown     = errors.New("fcgi: gateway shutting down")              // 503
)

// Error is a failure of the library, not of the script. Status is what a
// Script handler answers in the same situation; Err is one of the sentinels,
// possibly wrapping the cause.
type Error struct {
	Status int
	Err    error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// newError wraps cause, if any, under the sentinel.
func newError(status int, sentinel, cause error) *Error {
	if cause == nil {
		return &Error{Status: status, Err: sentinel}
	}
	return &Error{Status: status, Err: fmt.Errorf("%w: %w", sentinel, cause)}
}

// classify maps a failure during the dial or the exchange: a reached
// deadline is ErrTimeout, anything else the given sentinel.
func classify(ctx context.Context, err error, sentinel error) *Error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		if !errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("%w: %w", context.DeadlineExceeded, err)
		}
		return newError(http.StatusGatewayTimeout, ErrTimeout, err)
	}
	return newError(http.StatusBadGateway, sentinel, err)
}

// failure decides who ended a failed call. A cancelled ctx means no status
// was decided, so the result is not an *Error. The cause is flattened to text
// for the same reason.
func failure(ctx context.Context, err error) (Origin, int, error) {
	if errors.Is(ctx.Err(), context.Canceled) {
		return OriginClient, 0, fmt.Errorf("fcgi: call cancelled: %w: %v", ctx.Err(), err)
	}
	e, ok := errors.AsType[*Error](err)
	if !ok {
		e = newError(http.StatusBadGateway, ErrBadReply, err)
	}
	return OriginGateway, e.Status, e
}
