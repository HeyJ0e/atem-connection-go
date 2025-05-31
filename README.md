# ATEM connection Go

A pure Go implementation of the low-level communication protocol used by Blackmagic Design ATEM switchers. This library handles session negotiation and connection management but **does not interpret or parse ATEM commands or events**.

It has no external dependencies — only the Go standard library — and was actively used in production with the **ATEM 4 M/E Constellation 4K**.

## ⚠️ Important

This package implements a transparent `net.Conn` interface. While convenient, it comes with specific behavior you need to be aware of:
- `Write` and `Read` deal only with the **raw command(s)**.
- `Read` must **run continuously** with minimal delay or interruptions.
- `Read` can return errors triggered by **write/send failures** as well.

See [Protocol Details](#protocol-details) for deeper insight.

## 🚀 Getting Started

### Installation

```bash
go get github.com/HeyJ0e/atem-connection-go
```

### Example

```go
package main

import (
	"encoding/binary"
	"time"

	atemconn "github.com/HeyJ0e/atem-connection-go"
)

func main() {
  const atemAddr = "192.168.1.100"

  // Connect to the ATEM switcher
  conn, err := atemconn.Dial(atemAddr)
  if err != nil {
    panic(err)
  }

  // Start a goroutine to receive incoming messages
  go func() {
    buff := make([]byte, atemconn.MaxPayloadSize)

    for {
      n, err := conn.Read(buff)
      if err != nil {
        panic(err)
      }

      println("Received:", string(buff[:n]))
    }
  }()

  // Wait for ATEM to send its initial state
  time.Sleep(3 * time.Second)

  // Construct a command to switch IN 6 -> AUX 4
  cmd := make([]byte, 12)
  binary.BigEndian.PutUint16(cmd[0:], 12) // Command length
  copy(cmd[4:], "CAuS")                   // Command string
  cmd[8] = 0x01                           // Set mask
  cmd[9] = 0x03                           // AUX channel 4
  binary.BigEndian.PutUint16(cmd[10:], 5) // Input 6

  // Send the command
  if _, err := conn.Write(cmd); err != nil {
    panic(err)
  }

  time.Sleep(1 * time.Second)  // Let ATEM respond

  // Close the connection
  if err := conn.Close(); err != nil {
    panic(err)
  }
}
```

## 🔬 Protocol Details

The ATEM protocol is a stateful system built on top of UDP. It includes:
- Session handling and packet sequencing.
- Periodic keep-alive/polling from the ATEM device every few hundred milliseconds.
- Immediate disconnection if no response is received in time.

**This is why the `Read` loop must run continuously.** The connection is sustained by replying to ATEM's polling messages, handled internally in `Read`.

### Command Format

Each ATEM command consists of a header and a binary payload, structured as follows:

```
+--------+--------+------------------+--------------------+
| 00–01  | 02–03  | 04–07            | 08...              |
| Length | Zeroes | Command (ASCII)  | Payload (binary)   |
+--------+--------+------------------+--------------------+
```

- **Length** (`uint16`) — total command length (header + payload)
- **Zeroes** (`[2]byte`) — reserved/padding bytes
- **Command** (`[4]byte`) — command name (e.g., `CPgI`)
- **Payload** (`[]byte`) — command-specific binary data

A single packet can include multiple commands, up to a maximum combined size of **1404 bytes**. Most ATEM indices (e.g., source, AUX, M/E) are **zero-based**.

### Example Commands

| Function | Command | Payload Length | Payload Breakdown |
| --- | --- | --- | --- |
| **Program Input** | `CPgI` | 4 bytes | `0`: M/E (uint8)<br>`1`: Reserved (?)<br>`2-3`: Source (uint16) |
| **Preview Input** | `CPvI` | 4 bytes | `0`: M/E (uint8)<br>`1`: Reserved (?)<br>`2-3`: Source (uint16) |
| **AUX Source**    | `CAuS` | 4 bytes | `0`: Set mask (0x01)<br>`1`: AUX channel (uint8)<br>`2-3`: Source (uint16) |

### Decode Example

```go
func decode(b []byte) {
	for len(b) > 0 {
		l := int(binary.BigEndian.Uint16(b))
		cmd := string(b[4:8])
		data := b[8:l]

		println("cmd: " + cmd + " data: `" + hex.EncodeToString(data) + "`")

		b = b[l:]
	}
}
```

> 📝 **Note**: This package provides raw protocol access and does not parse, interpret, or define the full ATEM command set. Please refer to Blackmagic's community resources or reverse-engineered documentation for details on specific command structures.
