package atemconnection

import (
	"encoding/binary"
	"strconv"
)

// PktFlag defines flags used in ATEM protocol packets.
//
// Multiple flags can be combined using bitwise OR.
type PktFlag uint8

const (
	// AckRequest indicates that the packet expects an acknowledgment.
	AckRequest PktFlag = 1 << iota

	// NewSessionID signals that the sender is requesting or starting a new session.
	NewSessionID

	// IsResend marks the packet as a retransmission.
	IsResend

	// ResendRequest requests the receiver to resend missing packets.
	ResendRequest

	// AckReply is sent in response to AckRequest to confirm receipt.
	AckReply
)

const (
	// MaxPayloadSize is the maximum number of bytes allowed in the payload of a single packet.
	MaxPayloadSize = maxPacketSize - headerLen

	// MaxPacketID is the maximum valid packet ID (15-bit range).
	MaxPacketID = 1 << 15

	maxPacketSize = 1416
	headerLen     = 12
	headerPad     = "\x00\x00\x00\x00\x00\x00"
	ackOpNLen     = (uint16(AckReply) << 11) | headerLen // it'll be used a lot
)

// Packet represents a parsed ATEM protocol packet.
type Packet struct {
	Flags     PktFlag // Flags indicating packet control information.
	ID        uint16  // ID is the packet's unique identifier.
	AckedID   uint16  // AckedID is the highest ID this packet acknowledges.
	ResendID  uint16  // ResendID indicates which packet is requested for retransmission.
	SessionID uint16  // SessionID identifies the communication session.
	Payload   []byte  // Payload is the data carried by this packet.
}

// ParsePacket parses raw packet data into the given Packet struct.
//
// It returns an error if the packet is too short or the length header
// does not match the actual length.
//
// The parsed data will be stored in the provided Packet pointer.
func ParsePacket(data []byte, p *Packet) error {
	if len(data) < headerLen {
		return ErrTooShortMsg
	}
	flags := PktFlag(data[0] >> 3)
	l := int(binary.BigEndian.Uint16(data[0:]) & 0x07ff)
	if l != len(data) {
		return ErrPktInvalidLen
	}

	sessionID := binary.BigEndian.Uint16(data[2:])
	ackedID := binary.BigEndian.Uint16(data[4:])
	resendID := binary.BigEndian.Uint16(data[6:])
	remotePacketID := binary.BigEndian.Uint16(data[10:])

	p.Flags = flags
	p.ID = remotePacketID
	p.AckedID = ackedID
	p.ResendID = resendID
	p.SessionID = sessionID
	p.Payload = data[headerLen:]
	return nil
}

// Write serializes the Packet into the provided buffer.
//
// The buffer must be large enough to hold the entire packet, including the header.
// Returns the number of bytes written (equal to the payload length), or an error
// if the buffer is too small.
func (p *Packet) Write(buff []byte) (int, error) {
	l := headerLen + uint16(len(p.Payload))
	if uint16(len(buff)) < l {
		return 0, ErrBuffTooShort
	}

	opcode := uint16(p.Flags) << 11
	binary.BigEndian.PutUint16(buff[0:], opcode|l)
	binary.BigEndian.PutUint16(buff[2:], p.SessionID)
	binary.BigEndian.PutUint16(buff[4:], p.AckedID)
	binary.BigEndian.PutUint16(buff[6:], p.ResendID)
	binary.BigEndian.PutUint16(buff[10:], p.ID)

	return copy(buff[headerLen:], p.Payload), nil
}

func writeAck(id, sessID uint16, buff []byte) {
	if len(buff) < headerLen {
		panic(ErrBuffTooShort)
	}
	binary.BigEndian.PutUint16(buff[0:], ackOpNLen)
	binary.BigEndian.PutUint16(buff[2:], sessID)
	binary.BigEndian.PutUint16(buff[4:], id)
	copy(buff[6:], headerPad)
}

// String returns a human-readable string representation of a packet.
func (p *Packet) String() string {
	return "Atem packet: flags[" + p.Flags.String() + "], " +
		"ID:" + strconv.Itoa(int(p.ID)) +
		", acked ID:" + strconv.Itoa(int(p.AckedID)) +
		", resend ID:" + strconv.Itoa(int(p.ResendID)) +
		", session ID:" + strconv.Itoa(int(p.SessionID)) +
		", len:" + strconv.Itoa(len(p.Payload))
}

// String returns a space-separated string representation of the packet flags.
// For example: "ackReq isRetry ackReply"
func (f PktFlag) String() string {
	res := ""
	if f&AckRequest > 0 {
		res += "ackReq "
	}
	if f&NewSessionID > 0 {
		res += "newSessionID "
	}
	if f&IsResend > 0 {
		res += "isRetry "
	}
	if f&ResendRequest > 0 {
		res += "retryReq "
	}
	if f&AckReply > 0 {
		res += "ackReply "
	}
	if len(res) > 0 {
		res = res[:len(res)-1] // trim space at the end
	}

	return res
}
