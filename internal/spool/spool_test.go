package spool

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestSpoolSpillsAfterLimit(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, 4)
	defer s.Close()

	_, _ = s.Write([]byte("abc"))
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("spilled below the limit")
	}
	_, _ = s.Write([]byte("defg"))
	if s.Len() != 7 {
		t.Fatalf("Len = %d, want 7", s.Len())
	}
	got, _ := io.ReadAll(s.Reader(2))
	if string(got) != "cdefg" {
		t.Fatalf("read %q, want cdefg", got)
	}
	// The file is unlinked at creation, so nothing is left to clean up.
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("spool file visible in %s", dir)
	}
}

func TestSpoolInMemory(t *testing.T) {
	s := New(t.TempDir(), 10)
	defer s.Close()
	_, _ = s.Write([]byte("hello"))
	got, _ := io.ReadAll(s.Reader(0))
	if string(got) != "hello" {
		t.Fatalf("read %q", got)
	}
}

// readContent returns everything the spool holds, from offset.
func readContent(t *testing.T, s *Spool, offset int64) []byte {
	t.Helper()
	got, err := io.ReadAll(s.Reader(offset))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

func TestSpoolReadFromAcrossChunks(t *testing.T) {
	want := pattern(3*chunkSize + 17)
	s := New(t.TempDir(), 1<<20)
	defer s.Close()
	n, err := s.ReadFrom(io.LimitReader(bytes.NewReader(want), int64(len(want))))
	if err != nil || n != int64(len(want)) {
		t.Fatalf("ReadFrom = %d, %v", n, err)
	}
	if got := readContent(t, s, 5); !bytes.Equal(got, want[5:]) {
		t.Fatalf("content differs: got %d bytes, want %d", len(got), len(want)-5)
	}
	if s.file != nil {
		t.Fatal("spilled below the limit")
	}
}

func TestSpoolReadFromExactlyLimitStaysInMemory(t *testing.T) {
	want := pattern(chunkSize + 10)
	s := New(t.TempDir(), int64(len(want)))
	defer s.Close()
	if _, err := s.ReadFrom(bytes.NewReader(want)); err != nil {
		t.Fatal(err)
	}
	if s.file != nil {
		t.Fatal("a body of exactly the limit spilled")
	}
	if got := readContent(t, s, 0); !bytes.Equal(got, want) {
		t.Fatal("content differs")
	}
}

func TestSpoolReadFromSpillsPastLimit(t *testing.T) {
	want := pattern(2*chunkSize + 3)
	s := New(t.TempDir(), chunkSize)
	defer s.Close()
	if _, err := s.ReadFrom(bytes.NewReader(want)); err != nil {
		t.Fatal(err)
	}
	if s.file == nil {
		t.Fatal("did not spill past the limit")
	}
	if got := readContent(t, s, 1); !bytes.Equal(got, want[1:]) {
		t.Fatal("content differs after spill")
	}
}

func TestSpoolExpectSpillsAtOnce(t *testing.T) {
	s := New(t.TempDir(), 10)
	defer s.Close()
	if err := s.Expect(11); err != nil {
		t.Fatal(err)
	}
	if s.file == nil {
		t.Fatal("Expect above the limit kept the body in memory")
	}
	_, _ = s.Write([]byte("hello world"))
	if got := readContent(t, s, 0); string(got) != "hello world" {
		t.Fatalf("read %q", got)
	}
}

func TestSpoolWriteAcrossChunks(t *testing.T) {
	want := pattern(2*chunkSize + 99)
	s := New(t.TempDir(), 1<<20)
	defer s.Close()
	for p := want; len(p) > 0; {
		n := min(len(p), 7000)
		_, _ = s.Write(p[:n])
		p = p[n:]
	}
	var out bytes.Buffer
	if _, err := io.Copy(&out, s.Reader(0)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), want) {
		t.Fatal("content differs")
	}
}
