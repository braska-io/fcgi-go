// Package fastcgi is a FastCGI 1.0 client for the Responder role.
package fastcgi

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	version1 = 1

	typeBeginRequest = 1
	typeEndRequest   = 3
	typeParams       = 4
	typeStdin        = 5
	typeStdout       = 6
	typeStderr       = 7

	roleResponder = 1

	// maxContent is the largest content length a record header can carry.
	maxContent = 65535
	headerLen  = 8
	// requestID is fixed because a connection carries exactly one request.
	requestID = 1
)

// ErrProtocol marks a violation of the FastCGI framing by the responder.
var ErrProtocol = errors.New("fastcgi: protocol violation")

type recordHeader struct {
	Version       uint8
	Type          uint8
	RequestID     uint16
	ContentLength uint16
	PaddingLength uint8
	Reserved      uint8
}

func writeRecord(w io.Writer, typ uint8, content []byte) error {
	if len(content) > maxContent {
		return fmt.Errorf("record content of %d bytes exceeds %d", len(content), maxContent)
	}
	pad := (8 - len(content)%8) % 8
	buf := make([]byte, headerLen, headerLen+len(content)+pad)
	buf[0] = version1
	buf[1] = typ
	binary.BigEndian.PutUint16(buf[2:], requestID)
	binary.BigEndian.PutUint16(buf[4:], uint16(len(content)))
	buf[6] = uint8(pad)
	buf = append(buf, content...)
	buf = append(buf, make([]byte, pad)...)
	_, err := w.Write(buf)
	return err
}

// writeStream writes data as records of at most maxContent bytes, followed by
// the empty record that ends the stream.
func writeStream(w io.Writer, typ uint8, data []byte) error {
	for len(data) > 0 {
		n := min(len(data), maxContent)
		if err := writeRecord(w, typ, data[:n]); err != nil {
			return err
		}
		data = data[n:]
	}
	return writeRecord(w, typ, nil)
}

// readRecord reads one record. The content slice is only valid until the next
// call.
func readRecord(r io.Reader, scratch []byte) (recordHeader, []byte, error) {
	var raw [headerLen]byte
	var h recordHeader
	if _, err := io.ReadFull(r, raw[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return h, nil, fmt.Errorf("%w: connection closed before end of request", ErrProtocol)
		}
		return h, nil, fmt.Errorf("read record header: %w", err)
	}
	h = recordHeader{
		Version:       raw[0],
		Type:          raw[1],
		RequestID:     binary.BigEndian.Uint16(raw[2:]),
		ContentLength: binary.BigEndian.Uint16(raw[4:]),
		PaddingLength: raw[6],
	}
	if h.Version != version1 {
		return h, nil, fmt.Errorf("%w: version %d", ErrProtocol, h.Version)
	}
	if h.RequestID != requestID {
		return h, nil, fmt.Errorf("%w: request id %d", ErrProtocol, h.RequestID)
	}
	total := int(h.ContentLength) + int(h.PaddingLength)
	if cap(scratch) < total {
		scratch = make([]byte, total)
	}
	scratch = scratch[:total]
	if _, err := io.ReadFull(r, scratch); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return h, nil, fmt.Errorf("%w: truncated record", ErrProtocol)
		}
		return h, nil, fmt.Errorf("read record body: %w", err)
	}
	return h, scratch[:h.ContentLength], nil
}

// appendLength appends a name or value length in the 1- or 4-byte encoding.
func appendLength(b []byte, n int) []byte {
	if n < 128 {
		return append(b, byte(n))
	}
	return binary.BigEndian.AppendUint32(b, uint32(n)|1<<31)
}

func encodeParams(params []Param) []byte {
	var b []byte
	for _, p := range params {
		b = appendLength(b, len(p.Name))
		b = appendLength(b, len(p.Value))
		b = append(b, p.Name...)
		b = append(b, p.Value...)
	}
	return b
}
