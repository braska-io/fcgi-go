package spool

import (
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
