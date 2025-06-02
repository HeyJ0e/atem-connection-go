package atemconnection

import (
	"context"
	"math"
	"net"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// ConnectHello is the first packet sent by the client to the ATEM switcher to initiate the connection.
	ConnectHello      = "\x10\x14\x53\xab\x00\x00\x00\x00\x00\x3a\x00\x00\x01\x00\x00\x00\x00\x00\x00\x00"
	bufferSize        = 2048
	maxQueueLen       = 8
	ackTimeout        = 128 * time.Millisecond
	ackReplyTimeout   = 6 * time.Millisecond
	minResendTimeout  = 32 * 1000 // µsec
	connectionTimeout = 5 * time.Second
	maxPacketRetries  = 16
	maxPacketPerAck   = 16
	ackTolerance      = 1024 // any ack packet covers the last packets within this tolerance
	defaultPort       = 9910
)

type sentQueueItem struct {
	inUse     atomic.Bool
	packet    *Packet
	sendCount atomic.Uint32
	lastSent  atomic.Int64
	ackErrCh  chan error
}

type atemConn struct {
	writeLock      sync.Mutex    // protects the write buffer
	ackLock        sync.Mutex    // protects the ack buffer
	closed         atomic.Bool   // the connection is closed
	checkingResend atomic.Bool   // checkForResend goroutine is running
	resending      atomic.Bool   // rescending old or requested packets
	ackRunning     atomic.Bool   // ackTimeout goroutine is running
	sessionID      atomic.Uint32 // connection session ID provided by atem
	lastReadPkt    atomic.Uint32 // last packet got from atem
	lastAckedPkt   atomic.Uint32 // last packet acked for atem
	woAckCnt       atomic.Int32  // packets not (yet) acked for atem
	packetID       uint16        // packet ID sent by the go program
	readDeadline   time.Time
	closeAckTimer  chan struct{}
	conn           net.Conn // underlying udp connection
	readBuffer     []byte
	writeBuffer    []byte
	ackBuffer      [headerLen]byte
	sent           []*sentQueueItem
	closeErr       error
	closeCh        chan struct{}
}

// Dial connects to an ATEM switcher at the specified address.
//
// The address must be in the form "host[:port]". If no port is specified,
// the default port 9910 is used.
//
// Example:
//
//	conn, err := Dial("192.168.1.100")
//	conn, err := Dial("192.168.1.110:9910")
//
// Dial uses [context.Background] internally. To provide a custom context,
// use [DialContext] instead.
func Dial(address string) (net.Conn, error) {
	return DialContext(context.Background(), address)
}

// DialContext connects to the given address using the provided context.
//
// The context must be non-nil and is used only for establishing the connection.
// It does not affect the connection after it has been established.
//
// The address must be in the form "host[:port]". If no port is specified,
// the default port 9910 is used.
//
// Example:
//
//	deadline := time.Now().Add(3 * time.Second)
//	ctx := context.WithDeadline(context.Background(), deadline)
//	conn, err := DialContext(ctx, "192.168.1.100")
//
// If context control is not needed, use the [Dial] shorthand.
func DialContext(ctx context.Context, address string) (net.Conn, error) {
	if !strings.Contains(address, ":") {
		address += ":" + strconv.Itoa(defaultPort)
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "udp", address)
	if err != nil {
		return nil, newDialErr(err)
	}

	atemConn := newConn(conn)
	err = atemConn.connect(ctx)
	return atemConn, err
}

func newConn(conn net.Conn) *atemConn {
	c := &atemConn{
		conn:        conn,
		packetID:    1,
		readBuffer:  make([]byte, bufferSize),
		writeBuffer: make([]byte, maxPacketSize),
		sent:        make([]*sentQueueItem, maxQueueLen),
		closeCh:     make(chan struct{}),
	}

	for i := range c.sent {
		c.sent[i] = &sentQueueItem{packet: &Packet{}}
	}

	c.closed.Store(true)
	return c
}

func (a *atemConn) connect(ctx context.Context) error {
	connCtx, connDone := context.WithTimeoutCause(ctx, connectionTimeout, ErrConnTimeout)
	defer connDone()

	go func() {
		defer recover()
		select {
		case <-ctx.Done():
			a.conn.Close()
		case <-connCtx.Done():
			if connCtx.Err() == context.DeadlineExceeded {
				a.conn.Close()
			}
		}
	}()

	checkErr := func(err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-connCtx.Done():
			if connCtx.Err() == context.Canceled {
				return err
			}
			return context.Cause(connCtx)
		default:
			return err
		}
	}

	if dl, ok := ctx.Deadline(); ok {
		a.conn.SetDeadline(dl)
	}

	// send hello
	if _, err := a.conn.Write([]byte(ConnectHello)); err != nil {
		return checkErr(newWriteErr(err))
	}

	// read hello
	n, err := a.conn.Read(a.readBuffer)
	if err != nil {
		a.conn.Close()
		return checkErr(newHandshakeErr(err))
	}

	// parse hello
	p := Packet{}
	err = ParsePacket(a.readBuffer[:n], &p)
	if err != nil {
		a.conn.Close()
		return checkErr(newHandshakeErr(err))
	}
	if p.Flags&NewSessionID == 0 {
		a.conn.Close()
		return checkErr(newHandshakeErr(ErrPacketFlags))
	}
	a.lastReadPkt.Store(uint32(p.ID))

	// send ack
	if err = a.sendAck(p.ID, p.SessionID); err != nil {
		a.conn.Close()
		return checkErr(newHandshakeErr(err))
	}

	a.closed.Store(false)
	a.conn.SetDeadline(time.Time{})

	return checkErr(nil)
}

// Read reads the next payload from the ATEM connection into b buffer.
//
// This method implements the net.Conn interface. It returns only the command(s)
// portion of the received ATEM packet. Multiple commands may be received
// in a single payload.
//
// The session handling implemented inside [Read]. The ATEM expects the client
// to regularly answer for ack requests. If [Read] is delayed or blocked
// for too long, the ATEM switcher may close the session.
//
// The provided buffer b must be large enough to hold the entire payload
// (up to [MaxPayloadSize]).
//
// It returns the number of bytes copied into b and any error encountered.
// If the connection is already closed, ErrClosed is returned.
//
// Read may also return write-related errors if the internal session mechanism
// detects a failure while responding to ATEM polling.
//
// Example:
//
//	// read to buffer
//	buff := make([]byte, MaxPayloadSize)
//	n, _ := conn.Read(buff)
//	// extract first command data
//	l := int(binary.BigEndian.Uint16(buff))
//	cmd := string(buff[4:8])
//	data := buff[8:l]
func (a *atemConn) Read(b []byte) (int, error) {
	if a.closed.Load() {
		return 0, ErrClosed
	}

	p := Packet{}
	for {
		deadline := time.Now().Add(connectionTimeout)
		if !a.readDeadline.IsZero() && a.readDeadline.Before(deadline) {
			deadline = a.readDeadline
		}
		a.conn.SetReadDeadline(deadline)

		n, err := a.conn.Read(a.readBuffer)
		if err != nil {
			if a.closeErr != nil {
				err = a.closeErr
			} else {
				err = newReadErr(err)
			}
			return n, err
		}
		if err = ParsePacket(a.readBuffer[:n], &p); err != nil {
			return n, err
		}
		a.sessionID.Store(uint32(p.SessionID))

		if p.Flags&NewSessionID > 0 { // session start
			a.lastReadPkt.Store(uint32(p.ID))
			a.sendAck(p.ID, p.SessionID)
			continue
		}
		if p.Flags&ResendRequest > 0 { // atem needs the sent packets again
			if err := a.resendFrom(p.ResendID % MaxPacketID); err != nil {
				a.conn.Close()
				return 0, err
			}
		}
		if p.Flags&AckReply > 0 { // atem acked our sent package(s)
			a.lastAckedPkt.Store(uint32(p.AckedID))
			for i := range a.sent {
				if a.sent[i].inUse.Load() && isAcked(a.sent[i].packet.ID, p.AckedID) {
					if a.sent[i].ackErrCh != nil {
						close(a.sent[i].ackErrCh)
						a.sent[i].ackErrCh = nil
					}
				}
			}
		}
		if p.Flags&AckRequest > 0 { // new packet from atem
			// if this is the next packet we should have
			nextPkt := (uint16(a.lastReadPkt.Load()) + 1) % MaxPacketID
			if p.ID == nextPkt {
				a.lastReadPkt.Store(uint32(nextPkt))
				a.triggerAck(false)

				if len(p.Payload) > 0 {
					break // we got useful payload
				}
			} else if isAcked(p.ID, uint16(a.lastAckedPkt.Load())) {
				// we already got this message
				a.triggerAck(false)
			}
		}
	}

	return copy(b, p.Payload), nil
}

func (a *atemConn) triggerAck(force bool) {
	// send ack if we exceeded the maximum packet without ack
	if force || a.woAckCnt.Add(1) > maxPacketPerAck {
		a.sendAck(uint16(a.lastReadPkt.Load()), uint16(a.sessionID.Load()))
		a.woAckCnt.Store(0)
		if a.closeAckTimer != nil {
			close(a.closeAckTimer)
			a.closeAckTimer = nil
		}
	} else if a.ackRunning.CompareAndSwap(false, true) {
		// after some timeout we send ack anyway
		a.closeAckTimer = make(chan struct{})
		go a.ackTimeout()
	}
}

func (a *atemConn) ackTimeout() {
	defer func() {
		recover()
		a.ackRunning.Store(false)
	}()
	defer a.ackRunning.Store(false)

	select {
	case <-a.closeAckTimer:
	case <-time.After(ackReplyTimeout):
		a.triggerAck(true)
	}
}

func (a *atemConn) sendAck(id, sessionID uint16) error {
	a.ackLock.Lock()
	defer a.ackLock.Unlock()

	writeAck(id, sessionID, a.ackBuffer[:])
	if _, err := a.conn.Write(a.ackBuffer[:]); err != nil {
		return newWriteErr(err)
	}

	return nil
}

// Write sends raw ATEM command(s) to the switcher.
//
// This method implements the net.Conn interface. The input slice b must contain
// one or more complete and properly formatted ATEM commands, including length,
// padding, command string, and payload.
//
// No validation is performed on the contents of b; the caller is responsible
// for constructing valid ATEM command structures.
//
// Multiple commands can be sent in a single call, as long as the total size
// does not exceed [MaxPayloadSize].
//
// Write returns the number of bytes sent, which will be equal to len(b)
// or an error if the transmission fails.
//
// Example:
//
//	cmd := make([]byte, 12)
//	binary.BigEndian.PutUint16(cmd[0:], 12) // Command length
//	copy(cmd[4:], "CAuS")                   // Command string
//	cmd[8] = 0x01                           // Set mask
//	cmd[9] = 0x03                           // AUX channel 4
//	binary.BigEndian.PutUint16(cmd[10:], 5) // Input 6
//
//	if _, err := conn.Write(cmd); err != nil {
//		panic(err)
//	}
func (a *atemConn) Write(b []byte) (int, error) {
	if a.closed.Load() {
		return 0, ErrClosed
	}
	if len(b) > MaxPayloadSize {
		return 0, ErrTooLargePayload
	}

	// find an empty slot in the send queue
	for _, it := range a.sent {
		if it.inUse.CompareAndSwap(false, true) {
			it.sendCount.Store(0)
			defer it.inUse.Store(false)
			it.ackErrCh = make(chan error)

			it.packet.Flags = AckRequest
			it.packet.AckedID = 0
			it.packet.ResendID = 0
			it.packet.SessionID = uint16(a.sessionID.Load())
			it.packet.ID = a.packetID
			it.packet.Payload = b

			a.packetID = (a.packetID + 1) % MaxPacketID

			n, err := a.writeItem(it)
			if err != nil {
				return n, err
			}

			go a.checkForResend()
			return n, <-it.ackErrCh
		}
	}

	return 0, ErrQueueFull
}

func (a *atemConn) writeItem(qi *sentQueueItem) (int, error) {
	n, err := a.write(qi.packet)
	if err != nil {
		return n, err
	}

	qi.sendCount.Add(1)
	qi.lastSent.Store(time.Now().UnixMicro())
	return n, nil
}

func (a *atemConn) write(p *Packet) (int, error) {
	a.writeLock.Lock()
	defer a.writeLock.Unlock()

	n, err := p.Write(a.writeBuffer)
	if err != nil {
		return 0, err
	}

	n, err = a.conn.Write(a.writeBuffer[:n+headerLen])
	if err != nil {
		err = newWriteErr(err)
	}

	return max(n-headerLen, 0), err
}

func (a *atemConn) checkForResend() {
	defer func() {
		recover()
		a.checkingResend.Store(false)
	}()

	if !a.checkingResend.CompareAndSwap(false, true) {
		return
	}
	defer a.checkingResend.Store(false)

	for {
		if a.closed.Load() {
			return
		}

		now := time.Now().UnixMicro()
		minTime := now // minimum packet sent time
		deadline := now - ackTimeout.Microseconds()
		minID := uint16(math.MaxUint16)
		allSent := true

		for _, it := range a.sent {
			if it.inUse.Load() {
				allSent = false
				lastSent := it.lastSent.Load()
				minTime = min(minTime, lastSent)

				if lastSent < deadline && it.sendCount.Load() > 0 {
					if !isAcked(it.packet.ID, uint16(a.lastAckedPkt.Load())) {
						minID = min(minID, it.packet.ID)
					}
				}
			}
		}

		if allSent {
			return
		}
		if minID != uint16(math.MaxUint16) {
			a.resendFrom(minID)
		}

		micro := min(minTime+ackTimeout.Microseconds()-now, minResendTimeout)
		time.Sleep(time.Duration(micro) * time.Microsecond)
	}
}

func (a *atemConn) resendFrom(id uint16) error {
	if !a.resending.CompareAndSwap(false, true) {
		return nil
	}
	defer a.resending.Store(false)

	sent := a.sent[:]
	sort.Slice(sent, func(i, j int) bool {
		return sent[i].packet.ID < sent[j].packet.ID
	})
	startIdx := slices.IndexFunc(sent, func(qi *sentQueueItem) bool {
		return qi.packet.ID == id && qi.inUse.Load()
	})
	if startIdx == -1 {
		return ErrResendTooOld
	}

	for i := startIdx; i < len(sent); i++ {
		it := sent[i]

		if it.inUse.Load() && it.sendCount.Load() > maxPacketRetries {
			it.ackErrCh <- ErrTooManyRetries
			if it.ackErrCh != nil {
				close(it.ackErrCh)
				it.ackErrCh = nil
			}
		}

		_, err := a.writeItem(it)
		if err != nil {
			return err
		}
	}

	return nil
}

// Close gracefully shuts down the ATEM connection.
//
// If the connection is already closed, it returns ErrClosed. Otherwise, it
// performs cleanup by canceling all queued packets and closing the underlying
// network connection.
func (a *atemConn) Close() error {
	if a.closed.Load() {
		return ErrClosed
	}

	a.close(nil)
	return a.conn.Close()
}

func (a *atemConn) close(e error) {
	ackErr := ErrClosed
	if e != nil {
		ackErr = e
	}

	for _, it := range a.sent {
		if it.inUse.Load() && it.ackErrCh != nil {
			it.ackErrCh <- ackErr
			close(it.ackErrCh)
			it.ackErrCh = nil
		}
	}

	a.closeErr = e
	a.closed.Store(true)
}

func isAcked(pktID, ackID uint16) bool {
	// check if the pkt is within the (ack - tolerance; ack] interval
	// max packet id + tolerance is smaller than max uint16
	// so we shift the ids by tolerance
	pktID = ((pktID - ackTolerance) % MaxPacketID) + ackTolerance
	ackID = ((ackID - ackTolerance) % MaxPacketID) + ackTolerance
	return (pktID <= ackID) && (ackID-ackTolerance < pktID)
}

// LocalAddr returns the local network address of the underlying connection.
func (a *atemConn) LocalAddr() net.Addr {
	return a.conn.LocalAddr()
}

// RemoteAddr returns the remote network address of the underlying connection.
func (a *atemConn) RemoteAddr() net.Addr {
	return a.conn.RemoteAddr()
}

// SetDeadline sets the read and write deadlines for the connection.
//
// A zero value for t disables the deadlines. Note that the read deadline
// has its own specific behavior — see [SetReadDeadline] for details.
func (a *atemConn) SetDeadline(t time.Time) error {
	a.readDeadline = t
	return wrapDeadlineErr(a.conn.SetDeadline(t))
}

// SetReadDeadline sets the deadline for future [Read] calls.
//
// A zero value for t means no deadline is set on the [Read] call itself.
// However, the ATEM protocol still enforces its own timing requirements,
// and blocking [Read] operations for too long may cause it to time out.
func (a *atemConn) SetReadDeadline(t time.Time) error {
	a.readDeadline = t
	return wrapDeadlineErr(a.conn.SetReadDeadline(t))
}

// SetWriteDeadline sets the deadline for future Write calls.
//
// A zero value for t means no write deadline is set.
func (a *atemConn) SetWriteDeadline(t time.Time) error {
	return wrapDeadlineErr(a.conn.SetWriteDeadline(t))
}
