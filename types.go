package io

import (
	"fmt"
)

const defaultSize = 4096

// A BytesReader attaches a simple Reader interface to a byte slice.
type BytesReader []byte

func (x *BytesReader) Close() error {
	return nil
}

func (x *BytesReader) Read(b []byte) (int, error) {
	n := copy(b, *x)

	var err error
	if n < len(b) {
		err = EOF
	}
	*x = (*x)[n:]

	return n, err
}

type ErrorEntry struct {
	Source any // error source; can provide more meaningful error messages by implementing Stringer
	Err    error
}

func (x ErrorEntry) Error() string {
	return fmt.Sprintf("{source: %v; error: %v}", x.Source, x.Err)
}

// A MultiError bundles multiple errors together.
// Useful when a process can continue execution despite multiple potential error points, but cannot deal with the errors itself.
type MultiError []ErrorEntry

func (x MultiError) Error() string {
	var s string
	for _, e := range x {
		s += e.Error() + "\n"
	}
	return s
}

// A MultiWriter bundles multiple Writers together.
type MultiWriter []Writer

func (x MultiWriter) Close() error {
	var err MultiError

	for _, w := range x {
		if e := w.Close(); e != nil {
			err = append(err, ErrorEntry{w, e})
		}
	}

	if err != nil {
		return err
	}
	return nil
}

// Write will write b to all bundled Writers sequentially. Will always return len(b).
// If there is an error, it will be a [MultiError]. Errors do not stop the Write sequence.
func (x MultiWriter) Write(b []byte) (int, error) {
	var err MultiError

	for _, w := range x {
		if _, e := w.Write(b); e != nil {
			err = append(err, ErrorEntry{w, e})
		}
	}

	if err != nil {
		return len(b), err
	}
	return len(b), nil
}

// A ReadBuffer pulls data in buffered chunks, minimizing the number of read calls to the underlying source Reader.
type ReadBuffer struct {
	buf []byte
	w   int // last buffered data index
	r   int // next read index
	src Reader
}

// NewReadBuffer returns a ReadBuffer from [src].
// [buf] may be nil, in which case a default sized buffer will be allocated.
func NewReadBuffer(src Reader, buf []byte) *ReadBuffer {
	if buf == nil {
		buf = make([]byte, defaultSize)
	}
	return &ReadBuffer{
		buf: buf,
		src: src,
	}
}

func (x *ReadBuffer) Close() error {
	x.r = x.w
	return x.src.Close()
}

func (x *ReadBuffer) Read(b []byte) (int, error) {
	if x.w-x.r >= len(b) {
		// have enough buffered data
		copy(b, x.buf[x.r:x.w])
		x.r += len(b)
		return len(b), nil
	}

	// first copy what we have
	n := copy(b, x.buf[x.r:x.w])
	b = b[n:]

	if len(b) >= len(x.buf) {
		// large read, no need to buffer
		x.r = x.w
		r, err := x.src.Read(b)
		return n + r, err
	}

	// refill buffer; transfer what is needed
	var err error
	x.w, err = x.src.Read(x.buf)
	if x.w > len(b) {
		// silently drop error if we have enough data for another read
		err = nil
	}

	x.r = copy(b, x.buf[:x.w])

	return n + x.r, err
}

// Reset discards any pending data and sets the ReadBuffer to start reading from [src].
func (x *ReadBuffer) Reset(src Reader) {
	x.w = 0
	x.r = 0
	x.src = src
}

// A WriteBuffer accumulates Write calls before forwarding them, minimizing calls to the destination.
type WriteBuffer struct {
	buf []byte
	r   int // already transfered bytes; needed to return the correct data after a write error
	w   int // currently written bytes
	dst Writer
}

// NewWriteBuffer returns a WriteBuffer to [dst]. If [buf] is nil, a default will be allocated.
func NewWriteBuffer(dst Writer, buf []byte) *WriteBuffer {
	if buf == nil {
		buf = make([]byte, defaultSize)
	}
	return &WriteBuffer{
		buf: buf,
		dst: dst,
	}
}

// Bytes returns the currently buffered data.
func (x *WriteBuffer) Bytes() []byte {
	return x.buf[x.r:x.w]
}

// Close flushes any remaining data before closing the destination Writer.
func (x *WriteBuffer) Close() error {
	// x.r will be non zero if a write error occured; don't flush in this case
	if x.r == 0 && x.w > 0 {
		if _, err := x.dst.Write(x.buf[:x.w]); err != nil {
			return err
		}
		x.w = 0
	}
	return x.dst.Close()
}

// Flush writes out any pending data.
func (x *WriteBuffer) Flush() error {
	if x.r == x.w {
		return nil
	}
	n, err := x.dst.Write(x.buf[x.r:x.w])
	x.r += n
	if x.r == x.w {
		x.r = 0
		x.w = 0
	}
	return err
}

func (x *WriteBuffer) Write(b []byte) (int, error) {
	var (
		o   = 0 // transfered byte count from b
		err error
	)
	for len(b) > 0 {
		var n int
		if x.w == 0 && len(b) >= len(x.buf) {
			// large write with no pending data; transfer directly
			n, err = x.dst.Write(b)
			o += n
			return o, err
		}

		n = copy(x.buf[x.w:], b)
		b = b[n:]
		x.w += n
		o += n

		if x.w == len(x.buf) {
			// buffer is full, flush it
			n, err = x.dst.Write(x.buf)
			if err != nil {
				x.r = n
				return o, err
			}
			x.w = 0
		}
	}

	return o, err
}
