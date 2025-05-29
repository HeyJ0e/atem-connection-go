package atemconnection

import (
	"context"
	"encoding/binary"
	"os"
	"testing"
	"time"
)

func TestConnect(t *testing.T) {
	a, err := DialContext(context.Background(), "192.168.34.170:9910")
	if err != nil {
		panic(err)
	}

	buff := make([]byte, maxPacketSize)
	// a.SetReadDeadline(time.Now().Add(time.Second * 6))
	cmd := createCmd(createAux())
	go func() {
		for {
			_, err := a.Write(cmd)
			if err != nil {
				println("Write error:", err.Error())
				time.Sleep(3 * time.Second)
			}
			time.Sleep(2000 * time.Millisecond)
		}
	}()

	for {
		n, err := a.Read(buff)
		if err != nil {
			println("Read error:", err.Error())
			break
		}

		if n > 8 {
			println("cmd:", string(buff[4:8]), "len:", n)
		} else {
			println("WUT")
		}
	}

	println("exit")
	os.Exit(0)
}

func createCmd(payload []byte) []byte {
	res := make([]byte, 0, len(payload)+4)

	res = binary.BigEndian.AppendUint16(res, uint16(len(payload)+4))
	res = append(res, 0x00, 0x00)
	res = append(res, payload...)
	return res
}

func createAux() []byte {
	out := uint8(9 - 1)
	in := uint16(23)
	const name = "CAuS"
	res := make([]byte, 8)
	copy(res, name)
	res[4] = 0x01
	res[5] = out
	binary.BigEndian.PutUint16(res[6:], in)
	return res
}
