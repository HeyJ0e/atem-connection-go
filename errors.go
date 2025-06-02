package atemconnection

import (
	"errors"
)

// Error represents an ATEM connection error.
type Error interface {
	error
	// IsFatal returns true if an unrecoverable error occurred, so the connection must be closed.
	IsFatal() bool
	// Is returns true if the target equals this error, its root or its cause.
	Is(error) bool
	// Unwrap allows the inspection of the underlying cause using errors.Unwrap.
	Unwrap() error
}

// Sentinel errors returned by the package for specific failure conditions.
//
// These can be used with errors.Is to detect particular error categories,
// regardless of the underlying cause. For example:
//
//	if errors.Is(err, ErrWrite) {
//	    log.Println("write error to socket")
//	}
//
// Most errors wrap a lower-level cause (like a network error),
// which can be inspected using errors.Unwrap or errors.As.
var (
	// Generic ATEM connection error. A root for every other ATEM error.
	ErrAtemConn = errors.New("atem connection error")
	// Timed out while connecting.
	ErrConnTimeout = ae("connect timeout", true)
	// Payload exceeds the allowed size.
	ErrTooLargePayload = ae("too large payload", false)
	// Too many packets are waiting to be sent.
	ErrQueueFull = ae("send queue is full", false)
	// Not enough data was received to parse as an ATEM packet.
	ErrTooShortMsg = ae("message is too short", false)
	// Received message size doesn't match with the header length.
	ErrPktInvalidLen = ae("invalid packet length", false)
	// Wrong type of packet got upon protocol negotiation.
	ErrPacketFlags = ae("invalid packet flags", false)
	// Not enough buffer provided to create a package.
	ErrBuffTooShort = ae("buffer is too short", false)
	// ATEM asked to resend a packet outside the send queue.
	ErrResendTooOld = ae("trying to re-send too old packet", true)
	// Asked to resend the same packet too many times.
	ErrTooManyRetries = ae("too many retries", false)
	// Operation on a closed connection.
	ErrClosed = ae("the connection is already closed", true)
	// Establishing connection failure.
	ErrDial = ae("dial error", true)
	// Error reading from the underlying UDP socket.
	ErrRead = ae("read error", false)
	// Error writing to the underlying UDP socket.
	ErrWrite = ae("write error", false)
	// ATEM answered a wrong packet during protocol handshake.
	ErrHandshake = ae("handshake error", true)
	// Failed to set deadline on the underlying UDP socket.
	ErrSetDeadline = ae("unable to set deadline", false)
)

// ae creates a base atemError with no wrapped cause, only root = ErrAtemConn.
func ae(m string, f bool) error {
	return &atemError{m, f, nil, ErrAtemConn}
}

type atemError struct {
	msg   string
	fatal bool  // should the connection be closed
	cause error // wrapped error (e.g. underlying net.UDPConn error)
	root  error // sentinel, e.g. ErrDial
}

func (e *atemError) IsFatal() bool { return e.fatal }

func (e *atemError) Is(err error) bool {
	if err == nil {
		return false
	}
	if err == e || err == e.root || err == e.cause || err == ErrAtemConn {
		return true
	}
	return errors.Is(e.cause, err) || errors.Is(e.root, err)
}

func (e *atemError) Unwrap() error {
	if e.cause != nil {
		return e.cause
	}
	return e.root
}

func (e *atemError) Error() string {
	msg := e.msg
	if e.cause != nil {
		msg += ": " + e.cause.Error()
	}
	return msg
}

func newHandshakeErr(e error) error {
	return &atemError{"handshake error", true, e, ErrHandshake}
}

func newDialErr(e error) error {
	return &atemError{"dial error", true, e, ErrDial}
}

func newWriteErr(e error) error {
	return &atemError{"write error", false, e, ErrWrite}
}

func newReadErr(e error) error {
	return &atemError{"read error", false, e, ErrRead}
}

func wrapDeadlineErr(e error) error {
	if e != nil {
		return &atemError{"unable to set deadline", false, e, ErrSetDeadline}
	}
	return nil
}
