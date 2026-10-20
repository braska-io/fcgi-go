package fcgi

import (
	"net/http"
	"time"
)

// Origin names who decided the status of a call.
type Origin int

const (
	// OriginResponder: the responder's reply reached the client.
	OriginResponder Origin = iota
	// OriginGateway: the library answered with 400, 413, 502, 503 or 504.
	OriginGateway
	// OriginClient: the client left before a status was written.
	OriginClient
)

func (o Origin) String() string {
	switch o {
	case OriginResponder:
		return "responder"
	case OriginGateway:
		return "gateway"
	case OriginClient:
		return "client"
	}
	return "unknown"
}

// CallInfo describes one finished call of a Script handler.
type CallInfo struct {
	Script string
	// Status is the status written to the client; 0 when the client left.
	Status int
	Origin Origin
	// Err is nil on success. For OriginGateway it is the classified failure,
	// for OriginClient it wraps context.Canceled, and for OriginResponder it
	// is the failure to write the body, if any.
	Err error
	// SlotWait is the time spent waiting for a WithMaxConns slot.
	SlotWait time.Duration
	// Duration runs from the start of the slot wait to the last byte written.
	// Both are 0 for a call that ended before the slot wait, such as a 413.
	Duration time.Duration
	// RequestBytes counts request body bytes read from the client, also when
	// the read failed.
	RequestBytes int64
	// ResponseBytes counts body bytes written to the client, including the
	// library's error text.
	ResponseBytes int64
}

// WithObserver sets f to be called exactly once per call, synchronously,
// after the response is written or the call is abandoned. It runs after the
// responder slot is released, but the handler and Shutdown wait for it, so f
// must not block. A panic in f is recovered and logged. A script's observer
// replaces the gateway's; nil turns it off.
func WithObserver(f func(r *http.Request, info CallInfo)) Option {
	return bothOption(func(c *config) { c.observer = f })
}
