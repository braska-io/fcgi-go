package fastcgi

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
)

// Param is one FastCGI name-value pair.
type Param struct{ Name, Value string }

// Dial opens a connection to address, either "unix:<path>" or
// "tcp:<host:port>".
func Dial(ctx context.Context, address string) (net.Conn, error) {
	network, addr, ok := strings.Cut(address, ":")
	if !ok || (network != "unix" && network != "tcp") || addr == "" {
		return nil, fmt.Errorf("invalid responder address %q", address)
	}
	var d net.Dialer
	c, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("dial responder: %w", err)
	}
	return c, nil
}

// Result is what the responder reported in FCGI_END_REQUEST.
type Result struct {
	AppStatus      uint32
	ProtocolStatus uint8
}

// Do runs one request on conn and always closes it. php-fpm ignores
// FCGI_ABORT_REQUEST, so closing the connection is the only way to cancel:
// it happens as soon as ctx ends. Stdout and stderr stream into the writers.
func Do(ctx context.Context, conn net.Conn, params []Param, stdin io.Reader, stdout, stderr io.Writer) (Result, error) {
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer func() {
		stop()
		_ = conn.Close()
	}()

	res, err := roundTrip(conn, params, stdin, stdout, stderr)
	if err != nil {
		// A context that ended explains the closed connection better than
		// the I/O error it caused.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return res, ctxErr
		}
		return res, err
	}
	return res, nil
}

func roundTrip(conn net.Conn, params []Param, stdin io.Reader, stdout, stderr io.Writer) (Result, error) {
	// The request is sent in the background so a responder that answers
	// before it has read all of stdin does not deadlock us.
	sendErr := make(chan error, 1)
	go func() { sendErr <- sendRequest(conn, params, stdin) }()

	res, err := readResponse(conn, stdout, stderr)
	if err != nil {
		return res, err
	}
	// A responder may end the request before reading all of stdin.
	_ = conn.Close()
	if err := <-sendErr; err != nil && !isClosed(err) {
		return res, fmt.Errorf("send request: %w", err)
	}
	return res, nil
}

func isClosed(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, io.ErrClosedPipe)
}

func sendRequest(w io.Writer, params []Param, stdin io.Reader) error {
	bw := bufio.NewWriterSize(w, 64<<10)
	begin := make([]byte, 8)
	binary.BigEndian.PutUint16(begin, roleResponder)
	// flags stay 0: no FCGI_KEEP_CONN, the responder closes after the request.
	if err := writeRecord(bw, typeBeginRequest, begin); err != nil {
		return err
	}
	if err := writeParams(bw, params); err != nil {
		return err
	}
	if err := copyStdin(bw, stdin); err != nil {
		return err
	}
	return bw.Flush()
}

func copyStdin(w io.Writer, stdin io.Reader) error {
	if stdin != nil {
		buf := make([]byte, maxContent)
		for {
			n, err := io.ReadFull(stdin, buf)
			if n > 0 {
				if werr := writeRecord(w, typeStdin, buf[:n]); werr != nil {
					return werr
				}
			}
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			if err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}
		}
	}
	return writeRecord(w, typeStdin, nil)
}

func readResponse(r io.Reader, stdout, stderr io.Writer) (Result, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	var scratch []byte
	for {
		h, content, err := readRecord(br, scratch)
		if err != nil {
			return Result{}, err
		}
		scratch = content[:0]
		switch h.Type {
		case typeStdout:
			if _, err := stdout.Write(content); err != nil {
				return Result{}, fmt.Errorf("write stdout: %w", err)
			}
		case typeStderr:
			if _, err := stderr.Write(content); err != nil {
				return Result{}, fmt.Errorf("write stderr: %w", err)
			}
		case typeEndRequest:
			if len(content) < 8 {
				return Result{}, fmt.Errorf("%w: short end request record", ErrProtocol)
			}
			return Result{
				AppStatus:      binary.BigEndian.Uint32(content),
				ProtocolStatus: content[4],
			}, nil
		default:
			// Unknown record types from a responder are ignored, as the
			// specification asks for management records we never request.
		}
	}
}
