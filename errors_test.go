package atemconnection

import (
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
)

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		err      error
		expected string
		fatal    bool
	}{
		{ErrConnTimeout, "connect timeout", true},
		{ErrQueueFull, "send queue is full", false},
		{ErrHandshake, "handshake error", true},
		{ErrSetDeadline, "unable to set deadline", false},
		{ErrWrite, "write error", false},
		{ErrRead, "read error", false},
		{ErrDial, "dial error", true},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			ae, ok := tt.err.(Error)
			if !ok {
				t.Fatalf("expected error to implement Error interface")
			}
			if ae.Error() != tt.expected {
				t.Errorf("unexpected error message: got %q, want %q", ae.Error(), tt.expected)
			}
			if ae.IsFatal() != tt.fatal {
				t.Errorf("unexpected IsFatal: got %v, want %v", ae.IsFatal(), tt.fatal)
			}
		})
	}
}

func TestWrappingError(t *testing.T) {
	udpErr := &net.OpError{Op: "dial", Net: "udp", Err: io.EOF}
	err := newDialErr(udpErr)

	if !errors.Is(err, ErrDial) {
		t.Error("Is did not match ErrDial")
	}

	if !errors.Is(err, ErrAtemConn) {
		t.Error("Is did not match ErrAtemConn")
	}

	if !errors.Is(err, io.EOF) {
		t.Error("Is did not match underlying cause (io.EOF)")
	}

	if !errors.As(err, &udpErr) {
		t.Error("As failed to extract *net.OpError")
	}

	if err.(Error).Is(nil) {
		t.Error("Is did match nil")
	}

	if errors.Is(err, io.ErrUnexpectedEOF) {
		t.Error("Is did match different cause")
	}

	ae, ok := err.(Error)
	if !ok {
		t.Fatal("wrapped error does not implement Error interface")
	}

	if !ae.IsFatal() {
		t.Error("wrapped dial error should be fatal")
	}

	if !errors.Is(ae.Unwrap(), io.EOF) {
		t.Error("Unwrap did not return expected cause")
	}

	expected := "dial error: dial udp: EOF"
	if ae.Error() != expected {
		t.Errorf("unexpected error message: got %q, want %q", ae.Error(), expected)
	}
}

func TestWrapDeadlineNil(t *testing.T) {
	err := wrapDeadlineErr(nil)
	if err != nil {
		t.Errorf("wrapDeadlineErr(nil) should return nil, got: %v", err)
	}
}

func TestWrapDeadlineNonNil(t *testing.T) {
	src := fmt.Errorf("boom")
	err := wrapDeadlineErr(src)

	if !errors.Is(err, ErrSetDeadline) {
		t.Error("Is did not match ErrSetDeadline")
	}

	if !errors.Is(err, src) {
		t.Error("Is did not match wrapped cause")
	}
}

func TestCreators(t *testing.T) {
	src := fmt.Errorf("something bad")

	tests := []struct {
		creator  func(error) error
		expected error
	}{
		{newHandshakeErr, ErrHandshake},
		{newDialErr, ErrDial},
		{newWriteErr, ErrWrite},
		{newReadErr, ErrRead},
	}

	for _, tt := range tests {
		t.Run(tt.expected.Error(), func(t *testing.T) {
			err := tt.creator(src)
			if !errors.Is(err, src) {
				t.Fatalf("expected error to match src")
			}

			if !errors.Is(err, tt.expected) {
				t.Fatalf("expected error to match it's root")
			}

			if !errors.Is(err, ErrAtemConn) {
				t.Fatalf("expected error to match ErrAtemConn")
			}

			if _, ok := err.(Error); !ok {
				t.Fatalf("expected error to implement Error interface")
			}
		})
	}
}
