// Package smartdevice serves calibre's wireless device protocol, the
// "smart device app" one, so readers that connect to calibre over the
// network, as KOReader's calibre plugin does, can connect to Dalibre and
// be sent books.
//
// The protocol has no specification beyond calibre's own driver,
// src/calibre/devices/smart_device_app/driver.py; KOReader's side is
// plugins/calibre.koplugin/wireless.lua. Dalibre plays calibre: it
// answers the reader's discovery broadcast, the reader connects, and from
// then on Dalibre asks and the reader answers.
package smartdevice

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// Opcodes, as calibre numbers them.
const (
	opOK                    = 0
	opSetCalibreDeviceInfo  = 1
	opGetDeviceInformation  = 3
	opFreeSpace             = 5
	opGetBookCount          = 6
	opSendBooklists         = 7
	opSendBook              = 8
	opGetInitializationInfo = 9
	opNoop                  = 12
	opDeleteBook            = 13
	opSendBookMetadata      = 16
	opDisplayMessage        = 17
	opCalibreBusy           = 18
	opError                 = 20
)

// messagePasswordError is DISPLAY_MESSAGE's kind telling the reader its
// password is wrong.
const messagePasswordError = 1

// BroadcastPorts are the UDP ports readers send their discovery
// broadcast to, in the order they try them. calibre listens on the first
// it can have, and so does Dalibre.
var BroadcastPorts = []int{54982, 48123, 39001, 44044, 59678}

// DefaultPort is the TCP port calibre listens on first.
const DefaultPort = 9090

// discoveryPrefix starts the answer to a discovery broadcast. KOReader
// matches it word for word, so it names calibre whoever answers.
const discoveryPrefix = "calibre wireless device client"

// maxFrame bounds a message from the reader. Book lists are streamed a
// book a message, so none comes near.
const maxFrame = 16 << 20

// writeFrame writes [op, arg] as JSON, after its length in bytes in
// decimal: "34[0, {...}]".
func writeFrame(w io.Writer, op int, arg any) error {
	if arg == nil {
		arg = struct{}{}
	}
	js, err := json.Marshal([]any{op, arg})
	if err != nil {
		return err
	}
	buf := strconv.AppendInt(make([]byte, 0, len(js)+10), int64(len(js)), 10)
	_, err = w.Write(append(buf, js...))
	return err
}

// frame is a message from the reader.
type frame struct {
	op  int
	arg json.RawMessage
}

// readFrame reads a message written as writeFrame writes them. The length
// counts the JSON from its "[".
func readFrame(r *bufio.Reader) (frame, error) {
	n, digits := 0, 0
	for {
		b, err := r.ReadByte()
		if err != nil {
			return frame{}, err
		}
		if b == '[' {
			break
		}
		if b < '0' || b > '9' || digits == 9 {
			return frame{}, fmt.Errorf("smartdevice: bad message length")
		}
		n, digits = n*10+int(b-'0'), digits+1
	}
	if digits == 0 || n < 2 || n > maxFrame {
		return frame{}, fmt.Errorf("smartdevice: bad message length %d", n)
	}
	buf := make([]byte, n)
	buf[0] = '['
	if _, err := io.ReadFull(r, buf[1:]); err != nil {
		return frame{}, err
	}
	var msg []json.RawMessage
	if err := json.Unmarshal(buf, &msg); err != nil {
		return frame{}, fmt.Errorf("smartdevice: bad message: %w", err)
	}
	if len(msg) != 2 {
		return frame{}, errors.New("smartdevice: bad message")
	}
	var f frame
	if err := json.Unmarshal(msg[0], &f.op); err != nil {
		return frame{}, fmt.Errorf("smartdevice: bad opcode: %w", err)
	}
	f.arg = msg[1]
	return f, nil
}
