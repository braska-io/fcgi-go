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
	"sync"
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
// stdinLen is the length of stdin; Do does not read stdin after it returns.
func Do(ctx context.Context, conn net.Conn, params []Param, stdin io.Reader, stdinLen int64, stdout, stderr io.Writer) (Result, error) {
	stop := context.AfterFunc(ctx, func() { abort(conn) })
	defer func() {
		stop()
		_ = conn.Close()
	}()

	res, err := roundTrip(conn, params, stdin, stdinLen, stdout, stderr)
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

// abort closes conn so that the responder's next write fails. A plain close
// sends a FIN, and the first write after a FIN still succeeds; a reset makes
// it fail at once, which is what lets a responder roll back its work.
func abort(conn net.Conn) {
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetLinger(0)
	}
	_ = conn.Close()
}

// inlineStdin is the largest stdin sent before reading the response, on the
// calling goroutine. A request that small fits the socket buffers, so the
// write cannot block on a responder that answers before reading its input.
const inlineStdin = 8 << 10

func roundTrip(conn net.Conn, params []Param, stdin io.Reader, stdinLen int64, stdout, stderr io.Writer) (Result, error) {
	if stdinLen <= inlineStdin {
		sendE := sendRequest(conn, params, stdin)
		// A responder may answer and close without reading its input; its
		// answer still counts, and explains a failed write better.
		res, err := readResponse(conn, stdout, stderr)
		if err != nil && sendE != nil && !isClosed(sendE) {
			return res, fmt.Errorf("send request: %w (then: %w)", sendE, err)
		}
		return res, err
	}
	// A larger request is sent in the background so a responder that
	// answers before it has read all of stdin does not deadlock us.
	sendErr := make(chan error, 1)
	go func() { sendErr <- sendRequest(conn, params, stdin) }()

	res, err := readResponse(conn, stdout, stderr)
	// A responder may end the request before reading all of stdin. Closing
	// makes the sender's next write fail; waiting for it means stdin is no
	// longer read once we return, so the caller may free it.
	_ = conn.Close()
	sendE := <-sendErr
	if err != nil {
		return res, err
	}
	if sendE != nil && !isClosed(sendE) {
		return res, fmt.Errorf("send request: %w", sendE)
	}
	return res, nil
}

func isClosed(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, io.ErrClosedPipe)
}

// The 64 KiB buffers match the largest record, so a full STDIN or STDOUT
// record moves in one system call. Pooling them keeps a small request from
// paying for them.
var (
	writers = sync.Pool{New: func() any { return bufio.NewWriterSize(nil, 64<<10) }}
	readers = sync.Pool{New: func() any { return bufio.NewReaderSize(nil, 64<<10) }}
)

func sendRequest(w io.Writer, params []Param, stdin io.Reader) error {
	bw := writers.Get().(*bufio.Writer)
	bw.Reset(w)
	defer func() {
		bw.Reset(nil)
		writers.Put(bw)
	}()
	// flags stay 0: no FCGI_KEEP_CONN, the responder closes after the request.
	begin := [8]byte{0, roleResponder}
	if err := writeRecord(bw, typeBeginRequest, begin[:]); err != nil {
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
		b := buffers.Get().(*buffer)
		defer buffers.Put(b)
		buf := b[:maxContent]
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
	br := readers.Get().(*bufio.Reader)
	br.Reset(r)
	defer func() {
		br.Reset(nil)
		readers.Put(br)
	}()
	b := buffers.Get().(*buffer)
	defer buffers.Put(b)
	scratch := b[:0]
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
