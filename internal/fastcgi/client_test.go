package fastcgi

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// serve runs h against the first connection that arrives on a loopback
// listener and returns its address.
func serve(t *testing.T, h func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		h(c)
	}()
	return "tcp:" + ln.Addr().String()
}

// readRequest consumes BEGIN_REQUEST, PARAMS and STDIN and returns the
// begin flags, the raw params stream and the stdin bytes.
func readRequest(t *testing.T, c net.Conn) (flags byte, params, stdin []byte) {
	t.Helper()
	for done := 0; done < 2; {
		h, content, err := readRecord(c, nil)
		if err != nil {
			t.Errorf("fake responder: %v", err)
			return
		}
		switch h.Type {
		case typeBeginRequest:
			flags = content[2]
		case typeParams:
			params = append(params, content...)
			if len(content) == 0 {
				done++
			}
		case typeStdin:
			stdin = append(stdin, content...)
			if len(content) == 0 {
				done++
			}
		}
	}
	return flags, params, stdin
}

func endRequest(t *testing.T, c net.Conn, appStatus uint32) {
	t.Helper()
	body := make([]byte, 8)
	binary.BigEndian.PutUint32(body, appStatus)
	if err := writeRecord(c, typeEndRequest, body); err != nil {
		t.Error(err)
	}
}

func dial(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := Dial(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDoRoundTrip(t *testing.T) {
	var gotFlags byte
	var gotParams, gotStdin []byte
	addr := serve(t, func(c net.Conn) {
		gotFlags, gotParams, gotStdin = readRequest(t, c)
		_ = writeRecord(c, typeStdout, []byte("out"))
		_ = writeRecord(c, typeStderr, []byte("err"))
		_ = writeRecord(c, typeStdout, nil)
		endRequest(t, c, 3)
	})
	var stdout, stderr bytes.Buffer
	body := strings.Repeat("b", 70000)
	res, err := Do(context.Background(), dial(t, addr), []Param{{"K", "v"}}, strings.NewReader(body), &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if res.AppStatus != 3 || stdout.String() != "out" || stderr.String() != "err" {
		t.Fatalf("got %+v out %q err %q", res, stdout.String(), stderr.String())
	}
	if gotFlags != 0 {
		t.Errorf("flags = %d, want 0 (no keep-alive)", gotFlags)
	}
	if want := encodePair(Param{"K", "v"}); !bytes.Equal(gotParams, want) {
		t.Errorf("params = %x, want %x", gotParams, want)
	}
	if string(gotStdin) != body {
		t.Errorf("stdin is %d bytes, want %d", len(gotStdin), len(body))
	}
}

func TestDoRejectsEOFBeforeEndRequest(t *testing.T) {
	addr := serve(t, func(c net.Conn) {
		readRequest(t, c)
		_ = writeRecord(c, typeStdout, []byte("partial"))
	})
	_, err := Do(context.Background(), dial(t, addr), nil, nil, io.Discard, io.Discard)
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("got %v, want ErrProtocol", err)
	}
}

func TestDoClosesConnectionWhenContextEnds(t *testing.T) {
	closed := make(chan struct{})
	addr := serve(t, func(c net.Conn) {
		readRequest(t, c)
		_, _ = io.Copy(io.Discard, c) // returns when the gateway closes
		close(closed)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := Do(ctx, dial(t, addr), nil, nil, io.Discard, io.Discard)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want deadline exceeded", err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("responder never saw the connection close")
	}
}

func TestDialRejectsBadAddress(t *testing.T) {
	for _, a := range []string{"", "tcp", "http:host:80", "unix:"} {
		if _, err := Dial(context.Background(), a); err == nil {
			t.Errorf("Dial(%q) succeeded", a)
		}
	}
}

// slowReader hands out stdin slowly and records whether a read was still
// running after the caller declared stdin released.
type slowReader struct {
	released atomic.Bool
	late     atomic.Bool
}

func (r *slowReader) Read(p []byte) (int, error) {
	time.Sleep(5 * time.Millisecond)
	if r.released.Load() {
		r.late.Store(true)
	}
	return copy(p, make([]byte, min(len(p), 4096))), nil
}

// The caller may free stdin as soon as Do returns, so no read of it may
// happen after that, also when the responder fails mid-request.
func TestDoStopsReadingStdinBeforeReturning(t *testing.T) {
	addr := serve(t, func(c net.Conn) {
		var h [8]byte
		_, _ = io.ReadFull(c, h[:]) // then close without END_REQUEST
	})
	stdin := &slowReader{}
	_, err := Do(context.Background(), dial(t, addr), nil, stdin, io.Discard, io.Discard)
	stdin.released.Store(true)
	if err == nil {
		t.Fatal("want an error for a responder that closes early")
	}
	time.Sleep(50 * time.Millisecond)
	if stdin.late.Load() {
		t.Fatal("stdin was still read after Do returned")
	}
}
