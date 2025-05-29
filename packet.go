package atemconnection

import (
	"encoding/binary"
	"strconv"
)

type PktFlag uint8

const (
	AckRequest PktFlag = 1 << iota
	NewSessionID
	IsResend
	ResendRequest
	AckReply
)

const (
	MaxPayloadSize = maxPacketSize - headerLen
	MaxPacketID    = 1 << 15
	maxPacketSize  = 1416
	headerLen      = 12
	headerPad      = "\x00\x00\x00\x00\x00\x00"
	ackOpNLen      = (uint16(AckReply) << 11) | headerLen // it'll be used a lot
)

type Packet struct {
	Flags     PktFlag
	ID        uint16
	AckedID   uint16
	ResendID  uint16
	SessionID uint16
	Payload   []byte
}

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

func (p *Packet) String() string {
	return "Atem packet: flags[" + p.Flags.String() + "], " +
		"ID:" + strconv.Itoa(int(p.ID)) +
		", acked ID:" + strconv.Itoa(int(p.AckedID)) +
		", resend ID:" + strconv.Itoa(int(p.ResendID)) +
		", session ID:" + strconv.Itoa(int(p.SessionID)) +
		", len:" + strconv.Itoa(len(p.Payload))
}

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
