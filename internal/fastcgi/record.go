// Package fastcgi is a FastCGI 1.0 client for the Responder role.
package fastcgi

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
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
	// Header, content and padding go out as three writes: w is buffered,
	// and copying the content into one slice first would cost an
	// allocation per record.
	h := [headerLen]byte{version1, typ, 0, requestID, byte(len(content) >> 8), byte(len(content)), byte(pad), 0}
	if _, err := w.Write(h[:]); err != nil {
		return err
	}
	if _, err := w.Write(content); err != nil {
		return err
	}
	_, err := w.Write(zeros[:pad])
	return err
}

var zeros [8]byte

// buffer holds one record's content and padding. Pooled buffers keep the
// per-call cost of the client independent of the record size.
type buffer [maxContent + 255]byte

var buffers = sync.Pool{New: func() any { return new(buffer) }}

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

// writeParams writes the parameters as PARAMS records, then the empty record
// that ends the stream. Pairs are never split across records unless a single
// pair exceeds one record: php-fpm parses each record on its own and drops a
// pair that spans two, although the specification allows it.
func writeParams(w io.Writer, params []Param) error {
	rb := buffers.Get().(*buffer)
	defer buffers.Put(rb)
	rec := rb[:0]
	for _, p := range params {
		if n := pairLen(p); len(rec) > 0 && len(rec)+n > maxContent {
			if err := writeRecord(w, typeParams, rec); err != nil {
				return err
			}
			rec = rec[:0]
		}
		rec = appendPair(rec, p)
		for len(rec) > maxContent {
			if err := writeRecord(w, typeParams, rec[:maxContent]); err != nil {
				return err
			}
			rec = append(rec[:0], rec[maxContent:]...)
		}
	}
	if len(rec) > 0 {
		if err := writeRecord(w, typeParams, rec); err != nil {
			return err
		}
	}
	return writeRecord(w, typeParams, nil)
}

func pairLen(p Param) int {
	n := len(p.Name) + len(p.Value) + 2
	if len(p.Name) >= 128 {
		n += 3
	}
	if len(p.Value) >= 128 {
		n += 3
	}
	return n
}

func appendPair(b []byte, p Param) []byte {
	b = appendLength(b, len(p.Name))
	b = appendLength(b, len(p.Value))
	b = append(b, p.Name...)
	return append(b, p.Value...)
}
