// Package spool buffers a body in memory and moves it to a temporary file
// once it grows past a limit.
package spool

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// chunkSize is the unit of in-memory storage. Fixed-size chunks from a pool
// let a busy gateway reuse the same memory instead of growing and dropping a
// buffer per request.
const chunkSize = 32 << 10

var chunks = sync.Pool{New: func() any { return new([chunkSize]byte) }}

// Spool is a write-once, read-many body buffer. Close returns its memory to
// the pool, so no reader may be used after Close.
type Spool struct {
	dir   string
	limit int64
	mem   []*[chunkSize]byte
	file  *os.File
	size  int64
}

// New returns a spool that keeps up to limit bytes in memory and spills to a
// file in dir beyond that.
func New(dir string, limit int64) *Spool {
	return &Spool{dir: dir, limit: limit}
}

// Expect tells the spool how many bytes will follow. A body known to exceed
// the memory limit goes to the file at once instead of being copied there
// later.
func (s *Spool) Expect(n int64) error {
	if s.file == nil && s.size == 0 && n > s.limit {
		return s.spill()
	}
	return nil
}

// Write implements io.Writer.
func (s *Spool) Write(p []byte) (int, error) {
	if s.file == nil && s.size+int64(len(p)) > s.limit {
		if err := s.spill(); err != nil {
			return 0, err
		}
	}
	if s.file != nil {
		n, err := s.file.Write(p)
		s.size += int64(n)
		return n, err
	}
	written := len(p)
	for len(p) > 0 {
		n := copy(s.tail(), p)
		s.size += int64(n)
		p = p[n:]
	}
	return written, nil
}

// ReadFrom implements io.ReaderFrom, so io.Copy fills the chunks directly
// instead of going through a buffer of its own.
func (s *Spool) ReadFrom(r io.Reader) (int64, error) {
	var (
		total int64
		extra *[chunkSize]byte
	)
	for s.file == nil {
		var buf []byte
		atLimit := s.size >= s.limit
		if atLimit {
			// One more read decides whether the body really exceeds the
			// limit; a body of exactly limit bytes stays in memory.
			if extra == nil {
				extra = chunks.Get().(*[chunkSize]byte)
				defer chunks.Put(extra)
			}
			buf = extra[:]
		} else {
			buf = s.tail()
			buf = buf[:min(int64(len(buf)), s.limit-s.size)]
		}
		n, err := r.Read(buf)
		if atLimit && n > 0 {
			if serr := s.spill(); serr != nil {
				return total, serr
			}
			if _, werr := s.file.Write(buf[:n]); werr != nil {
				return total, fmt.Errorf("write spool file: %w", werr)
			}
		}
		s.size += int64(n)
		total += int64(n)
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
	n, err := io.Copy(s.file, r)
	s.size += n
	return total + n, err
}

// tail returns the free space of the last chunk, adding a chunk when the
// last one is full.
func (s *Spool) tail() []byte {
	if int64(len(s.mem))*chunkSize == s.size {
		s.mem = append(s.mem, chunks.Get().(*[chunkSize]byte))
	}
	return s.mem[len(s.mem)-1][s.size%chunkSize:]
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
	if _, err := s.reader(0).WriteTo(f); err != nil {
		_ = f.Close()
		return fmt.Errorf("write spool file: %w", err)
	}
	s.release()
	s.file = f
	return nil
}

func (s *Spool) release() {
	for i, c := range s.mem {
		chunks.Put(c)
		s.mem[i] = nil
	}
	s.mem = s.mem[:0]
}

// Len returns the number of bytes written.
func (s *Spool) Len() int64 { return s.size }

// Reader returns a reader over the whole content, starting at offset.
func (s *Spool) Reader(offset int64) io.Reader {
	if s.file != nil {
		return io.NewSectionReader(s.file, offset, s.size-offset)
	}
	return s.reader(offset)
}

func (s *Spool) reader(offset int64) *memReader {
	return &memReader{s: s, off: offset}
}

// memReader reads the in-memory chunks. It implements io.WriterTo, so
// io.Copy hands each chunk to the destination without another copy.
type memReader struct {
	s   *Spool
	off int64
}

func (r *memReader) next() []byte {
	if r.off >= r.s.size {
		return nil
	}
	start := r.off % chunkSize
	end := min(chunkSize, start+r.s.size-r.off)
	return r.s.mem[r.off/chunkSize][start:end]
}

func (r *memReader) Read(p []byte) (int, error) {
	b := r.next()
	if b == nil {
		return 0, io.EOF
	}
	n := copy(p, b)
	r.off += int64(n)
	return n, nil
}

func (r *memReader) WriteTo(w io.Writer) (int64, error) {
	var total int64
	for b := r.next(); b != nil; b = r.next() {
		n, err := w.Write(b)
		r.off += int64(n)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// Close returns the memory to the pool and releases the spool file, if any.
func (s *Spool) Close() error {
	s.release()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}
