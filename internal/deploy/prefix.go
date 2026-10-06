package deploy

import (
	"bytes"
	"io"
	"sync"
)

// PrefixWriter writes complete lines to its destination, each prefixed, so
// that the output of hosts deployed in parallel does not interleave inside a
// line. Writers sharing a destination must share its mutex.
type PrefixWriter struct {
	mu     *sync.Mutex
	dst    io.Writer
	prefix []byte
	buf    []byte
}

// NewPrefixWriter returns a PrefixWriter writing to dst under mu.
func NewPrefixWriter(dst io.Writer, mu *sync.Mutex, prefix string) *PrefixWriter {
	return &PrefixWriter{mu: mu, dst: dst, prefix: []byte(prefix)}
}

// Write buffers p and writes out its complete lines.
func (w *PrefixWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		if err := w.emit(w.buf[:i+1]); err != nil {
			return 0, err
		}
		w.buf = w.buf[i+1:]
	}
}

// Flush writes out the last incomplete line, if any.
func (w *PrefixWriter) Flush() error {
	if len(w.buf) == 0 {
		return nil
	}
	err := w.emit(append(w.buf, '\n'))
	w.buf = nil
	return err
}

func (w *PrefixWriter) emit(line []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := w.dst.Write(append(append([]byte{}, w.prefix...), line...))
	return err
}
