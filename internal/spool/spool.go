// Package spool buffers a body in memory and moves it to a temporary file
// once it grows past a limit.
package spool

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

// Spool is a write-once, read-many body buffer.
type Spool struct {
	dir   string
	limit int64
	mem   bytes.Buffer
	file  *os.File
	size  int64
}

// New returns a spool that keeps up to limit bytes in memory and spills to a
// file in dir beyond that.
func New(dir string, limit int64) *Spool {
	return &Spool{dir: dir, limit: limit}
}

// Write implements io.Writer.
func (s *Spool) Write(p []byte) (int, error) {
	if s.file == nil && s.size+int64(len(p)) > s.limit {
		if err := s.spill(); err != nil {
			return 0, err
		}
	}
	var n int
	var err error
	if s.file != nil {
		n, err = s.file.Write(p)
	} else {
		n, err = s.mem.Write(p)
	}
	s.size += int64(n)
	return n, err
}

func (s *Spool) spill() error {
	f, err := os.CreateTemp(s.dir, "fcgi-spool-*")
	if err != nil {
		return fmt.Errorf("create spool file: %w", err)
	}
	// Unlinking now keeps the file out of reach and leak-free after a crash.
	if err := os.Remove(f.Name()); err != nil {
		_ = f.Close()
		return fmt.Errorf("unlink spool file: %w", err)
	}
	if _, err := f.Write(s.mem.Bytes()); err != nil {
		_ = f.Close()
		return fmt.Errorf("write spool file: %w", err)
	}
	s.file = f
	s.mem = bytes.Buffer{}
	return nil
}

// Len returns the number of bytes written.
func (s *Spool) Len() int64 { return s.size }

// Reader returns a reader over the whole content, starting at offset.
func (s *Spool) Reader(offset int64) io.Reader {
	if s.file == nil {
		return bytes.NewReader(s.mem.Bytes()[offset:])
	}
	return io.NewSectionReader(s.file, offset, s.size-offset)
}

// Close releases the spool file, if any.
func (s *Spool) Close() error {
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}
