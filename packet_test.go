package atemconnection

import (
	"encoding/binary"
	"errors"
	"testing"
)

func makePacketBytes(flags PktFlag, sessionID, ackedID, resendID, packetID uint16, payload []byte) []byte {
	l := headerLen + len(payload)
	buff := make([]byte, l)
	opcode := uint16(flags) << 11
	binary.BigEndian.PutUint16(buff[0:], opcode|uint16(l))
	binary.BigEndian.PutUint16(buff[2:], sessionID)
	binary.BigEndian.PutUint16(buff[4:], ackedID)
	binary.BigEndian.PutUint16(buff[6:], resendID)
	binary.BigEndian.PutUint16(buff[10:], packetID)
	copy(buff[headerLen:], payload)
	return buff
}

func TestParsePacket_Success(t *testing.T) {
	payload := []byte{1, 2, 3, 4}
	flags := AckRequest | IsResend
	sessionID := uint16(0x1234)
	ackedID := uint16(0x2345)
	resendID := uint16(0x3456)
	packetID := uint16(0x4567)
	data := makePacketBytes(flags, sessionID, ackedID, resendID, packetID, payload)

	var pkt Packet
	err := ParsePacket(data, &pkt)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if pkt.Flags != flags {
		t.Errorf("expected flags %v, got %v", flags, pkt.Flags)
	}
	if pkt.SessionID != sessionID {
		t.Errorf("expected sessionID %v, got %v", sessionID, pkt.SessionID)
	}
	if pkt.AckedID != ackedID {
		t.Errorf("expected ackedID %v, got %v", ackedID, pkt.AckedID)
	}
	if pkt.ResendID != resendID {
		t.Errorf("expected resendID %v, got %v", resendID, pkt.ResendID)
	}
	if pkt.ID != packetID {
		t.Errorf("expected ID %v, got %v", packetID, pkt.ID)
	}
	if len(pkt.Payload) != len(payload) || string(pkt.Payload) != string(payload) {
		t.Errorf("expected payload %v, got %v", payload, pkt.Payload)
	}
}

func TestParsePacket_TooShort(t *testing.T) {
	data := make([]byte, headerLen-1)
	var pkt Packet
	err := ParsePacket(data, &pkt)
	if !errors.Is(err, ErrTooShortMsg) {
		t.Errorf("expected ErrTooShortMsg, got %v", err)
	}
}

func TestParsePacket_InvalidLength(t *testing.T) {
	payload := []byte{1, 2, 3}
	flags := AckReply
	sessionID := uint16(0x1111)
	ackedID := uint16(0x2222)
	resendID := uint16(0x3333)
	packetID := uint16(0x4444)
	data := makePacketBytes(flags, sessionID, ackedID, resendID, packetID, payload)
	// Corrupt the length header to be too large
	binary.BigEndian.PutUint16(data[0:], (uint16(flags)<<11)|uint16(len(data)+1))

	var pkt Packet
	err := ParsePacket(data, &pkt)
	if !errors.Is(err, ErrPktInvalidLen) {
		t.Errorf("expected ErrPktInvalidLen, got %v", err)
	}
}

func TestParsePacket_EmptyPayload(t *testing.T) {
	flags := ResendRequest
	sessionID := uint16(0xAAAA)
	ackedID := uint16(0xBBBB)
	resendID := uint16(0xCCCC)
	packetID := uint16(0xDDDD)
	data := makePacketBytes(flags, sessionID, ackedID, resendID, packetID, nil)

	var pkt Packet
	err := ParsePacket(data, &pkt)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(pkt.Payload) != 0 {
		t.Errorf("expected empty payload, got %v", pkt.Payload)
	}
}

func TestPacket_Write_Success(t *testing.T) {
	payload := []byte{10, 20, 30, 40}
	pkt := Packet{
		Flags:     AckRequest | IsResend,
		ID:        0x1234,
		AckedID:   0x2345,
		ResendID:  0x3456,
		SessionID: 0x4567,
		Payload:   payload,
	}
	buff := make([]byte, headerLen+len(payload))
	n, err := pkt.Write(buff)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if n != len(payload) {
		t.Errorf("expected %d bytes written, got %d", len(payload), n)
	}
	// Parse back to verify correctness
	var parsed Packet
	if err := ParsePacket(buff, &parsed); err != nil {
		t.Fatalf("ParsePacket failed: %v", err)
	}
	if parsed.Flags != pkt.Flags ||
		parsed.ID != pkt.ID ||
		parsed.AckedID != pkt.AckedID ||
		parsed.ResendID != pkt.ResendID ||
		parsed.SessionID != pkt.SessionID ||
		string(parsed.Payload) != string(pkt.Payload) {
		t.Errorf("parsed packet does not match original: got %+v, want %+v", parsed, pkt)
	}
}

func TestPacket_Write_BufferTooSmall(t *testing.T) {
	payload := []byte{1, 2, 3}
	pkt := Packet{
		Flags:     AckReply,
		ID:        0x1111,
		AckedID:   0x2222,
		ResendID:  0x3333,
		SessionID: 0x4444,
		Payload:   payload,
	}
	buff := make([]byte, headerLen+len(payload)-1) // too small
	n, err := pkt.Write(buff)
	if n != 0 {
		t.Errorf("expected 0 bytes written, got %d", n)
	}
	if !errors.Is(err, ErrBuffTooShort) {
		t.Errorf("expected ErrBuffTooShort, got %v", err)
	}
}

func TestPacket_Write_EmptyPayload(t *testing.T) {
	pkt := Packet{
		Flags:     ResendRequest,
		ID:        0xAAAA,
		AckedID:   0xBBBB,
		ResendID:  0xCCCC,
		SessionID: 0xDDDD,
		Payload:   nil,
	}
	buff := make([]byte, headerLen)
	n, err := pkt.Write(buff)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 bytes written for empty payload, got %d", n)
	}
	// Parse back to verify correctness
	var parsed Packet
	if err := ParsePacket(buff, &parsed); err != nil {
		t.Fatalf("ParsePacket failed: %v", err)
	}
	if parsed.Flags != pkt.Flags ||
		parsed.ID != pkt.ID ||
		parsed.AckedID != pkt.AckedID ||
		parsed.ResendID != pkt.ResendID ||
		parsed.SessionID != pkt.SessionID ||
		len(parsed.Payload) != 0 {
		t.Errorf("parsed packet does not match original: got %+v, want %+v", parsed, pkt)
	}
}

func TestWriteAck_Success(t *testing.T) {
	var (
		id     uint16 = 0x1234
		sessID uint16 = 0x5678
	)
	buff := make([]byte, headerLen)
	writeAck(id, sessID, buff)

	// Check opcode and length
	gotOpLen := binary.BigEndian.Uint16(buff[0:])
	if gotOpLen != ackOpNLen {
		t.Errorf("expected opcode/len %04x, got %04x", ackOpNLen, gotOpLen)
	}
	// Check session ID
	gotSessID := binary.BigEndian.Uint16(buff[2:])
	if gotSessID != sessID {
		t.Errorf("expected sessionID %04x, got %04x", sessID, gotSessID)
	}
	// Check acked ID
	gotAckedID := binary.BigEndian.Uint16(buff[4:])
	if gotAckedID != id {
		t.Errorf("expected ackedID %04x, got %04x", id, gotAckedID)
	}
	// Check header padding
	if string(buff[6:]) != headerPad {
		t.Errorf("expected headerPad %q, got %q", headerPad, buff[6:])
	}
}

func TestWriteAck_BufferTooShort(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic for short buffer, but did not panic")
		}
	}()
	buff := make([]byte, headerLen-1)
	writeAck(0x1111, 0x2222, buff)
}

func TestPacket_String(t *testing.T) {
	tests := []struct {
		name     string
		packet   Packet
		expected string
	}{
		{
			name: "All fields set, non-empty payload",
			packet: Packet{
				Flags:     AckRequest | NewSessionID | IsResend | ResendRequest | AckReply,
				ID:        123,
				AckedID:   456,
				ResendID:  789,
				SessionID: 321,
				Payload:   []byte{1, 2, 3, 4, 5},
			},
			expected: "Atem packet: flags[ackReq newSessionID isRetry retryReq ackReply], ID:123, acked ID:456, resend ID:789, session ID:321, len:5",
		},
		{
			name: "No flags, empty payload",
			packet: Packet{
				Flags:     0,
				ID:        0,
				AckedID:   0,
				ResendID:  0,
				SessionID: 0,
				Payload:   nil,
			},
			expected: "Atem packet: flags[], ID:0, acked ID:0, resend ID:0, session ID:0, len:0",
		},
		{
			name: "Single flag, short payload",
			packet: Packet{
				Flags:     AckReply,
				ID:        42,
				AckedID:   43,
				ResendID:  44,
				SessionID: 45,
				Payload:   []byte{9},
			},
			expected: "Atem packet: flags[ackReply], ID:42, acked ID:43, resend ID:44, session ID:45, len:1",
		},
		{
			name: "Multiple flags, empty payload",
			packet: Packet{
				Flags:     AckRequest | IsResend,
				ID:        1000,
				AckedID:   2000,
				ResendID:  3000,
				SessionID: 4000,
				Payload:   []byte{},
			},
			expected: "Atem packet: flags[ackReq isRetry], ID:1000, acked ID:2000, resend ID:3000, session ID:4000, len:0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.packet.String()
			if got != tt.expected {
				t.Errorf("Packet.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestPktFlag_String(t *testing.T) {
	tests := []struct {
		name     string
		flag     PktFlag
		expected string
	}{
		{
			name:     "No flags",
			flag:     0,
			expected: "",
		},
		{
			name:     "AckRequest only",
			flag:     AckRequest,
			expected: "ackReq",
		},
		{
			name:     "NewSessionID only",
			flag:     NewSessionID,
			expected: "newSessionID",
		},
		{
			name:     "IsResend only",
			flag:     IsResend,
			expected: "isRetry",
		},
		{
			name:     "ResendRequest only",
			flag:     ResendRequest,
			expected: "retryReq",
		},
		{
			name:     "AckReply only",
			flag:     AckReply,
			expected: "ackReply",
		},
		{
			name:     "All flags",
			flag:     AckRequest | NewSessionID | IsResend | ResendRequest | AckReply,
			expected: "ackReq newSessionID isRetry retryReq ackReply",
		},
		{
			name:     "Some flags (AckRequest | IsResend)",
			flag:     AckRequest | IsResend,
			expected: "ackReq isRetry",
		},
		{
			name:     "Some flags (NewSessionID | AckReply)",
			flag:     NewSessionID | AckReply,
			expected: "newSessionID ackReply",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.flag.String()
			if got != tt.expected {
				t.Errorf("PktFlag.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}
