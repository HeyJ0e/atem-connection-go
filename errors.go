package atemconn

import (
	"errors"
	"net"
)

type Error interface {
	error
	IsFatal() bool
}

var (
	ErrAtemConn        = errors.New("atem connection error")
	ErrConnTimeout     = &atemError{"connect timeout", true}
	ErrTooLargePayload = &atemError{"too large payload", false}
	ErrQueueFull       = &atemError{"send queue is full", false}
	ErrTooShortMsg     = &atemError{"message is too short", false}
	ErrPktInvalidLen   = &atemError{"invalid packet length", false}
	ErrBuffTooShort    = &atemError{"buffer is too short", false}
	ErrResendTooOld    = &atemError{"trying to re-send too old packet", true}
	ErrTooManyRetries  = &atemError{"too many retries", false}
	ErrClosed          = net.ErrClosed
)

type atemError struct {
	msg   string
	fatal bool
}

func (e *atemError) Error() string { return e.msg }
func (e *atemError) Unwrap() error { return ErrAtemConn }
func (e *atemError) IsFatal() bool { return e.fatal }

func ErrHandshake[T ~string](s T) error {
	return &atemError{"handshake error: " + string(s), true}
}
