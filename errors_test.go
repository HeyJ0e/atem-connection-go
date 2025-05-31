package atemconnection

import (
	"testing"
)

func TestAtemError(t *testing.T) {
	tests := []struct {
		name       string
		err        *atemError
		wantError  string
		wantUnwrap error
		wantFatal  bool
	}{
		{
			name:       "simple error message",
			err:        &atemError{msg: "test error", fatal: false},
			wantError:  "test error",
			wantUnwrap: ErrAtemConn,
			wantFatal:  false,
		},
		{
			name:       "empty error message",
			err:        &atemError{msg: "", fatal: true},
			wantError:  "",
			wantUnwrap: ErrAtemConn,
			wantFatal:  true,
		},
		{
			name:       "fatal error message",
			err:        &atemError{msg: "fatal error", fatal: true},
			wantError:  "fatal error",
			wantUnwrap: ErrAtemConn,
			wantFatal:  true,
		},
		{
			name:       "non-fatal error",
			err:        &atemError{msg: "not fatal", fatal: false},
			wantError:  "not fatal",
			wantUnwrap: ErrAtemConn,
			wantFatal:  false,
		},
		{
			name:       "empty message, non-fatal",
			err:        &atemError{msg: "", fatal: false},
			wantError:  "",
			wantUnwrap: ErrAtemConn,
			wantFatal:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.wantError {
				t.Errorf("Error() = %q, want %q", got, tt.wantError)
			}
			if got := tt.err.Unwrap(); got != tt.wantUnwrap {
				t.Errorf("Unwrap() = %v, want %v", got, tt.wantUnwrap)
			}
			if got := tt.err.IsFatal(); got != tt.wantFatal {
				t.Errorf("IsFatal() = %v, want %v", got, tt.wantFatal)
			}
		})
	}
}

func TestErrHandshake(t *testing.T) {
	input := "foo"
	wantError := "handshake error: foo"

	err := ErrHandshake(input)
	ae, ok := err.(*atemError)
	if !ok {
		t.Fatalf("ErrHandshake() did not return *atemError, got %T", err)
	}
	if got := ae.Error(); got != wantError {
		t.Errorf("Error() = %q, want %q", got, wantError)
	}
	if got := ae.IsFatal(); got != true {
		t.Errorf("IsFatal() = %v, want %v", got, true)
	}
	if got := ae.Unwrap(); got != ErrAtemConn {
		t.Errorf("Unwrap() = %v, want %v", got, ErrAtemConn)
	}
}
