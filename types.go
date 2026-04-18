package io

import (
	"github.com/blitz-frost/errors"
)

const defaultSize = 4096

// A MultiWriter bundles multiple Writers together.
type MultiWriter []Writer

// Write will write b to all bundled Writers sequentially. Will always return len(b).
// Errors do not stop the Write sequence.
//
// The returned errors will be marked with their respective writer index (int).
func (x MultiWriter) Write(b []byte) errors.G {
	var oErr errors.G

	for i, w := range x {
		if err := w.Write(b); err != nil {
			oErr = append(oErr, &errors.T{
				Mark: i,
				Sub:  err,
			})
		}
	}

	return oErr
}

// A ReadBuffer pulls data in buffered chunks, minimizing the number of read calls to the underlying source Reader.
type ReadBuffer struct {
	buf []byte
	w   int // last buffered data index
	r   int // next read index
	src Reader
}

// ReadBufferMake returns a ReadBuffer from [src].
// [buf] may be nil, in which case a default sized buffer will be allocated.
func ReadBufferMake(src Reader, buf []byte) *ReadBuffer {
	if buf == nil {
		buf = make([]byte, defaultSize)
	}
	return &ReadBuffer{
		buf: buf,
		src: src,
	}
}

// Any returned error is from underlying Read calls.
func (x *ReadBuffer) Read(b []byte) *Error {
	if x.w-x.r >= len(b) {
		// have enough buffered data
		copy(b, x.buf[x.r:x.w])
		x.r += len(b)
		return nil
	}

	// first, copy what we have
	n := copy(b, x.buf[x.r:x.w])
	b = b[n:]

	if len(b) >= len(x.buf) {
		// large read, no need to buffer
		x.r = x.w
		err := x.src.Read(b)
		if err != nil {
			err.N += n
		}
		return err
	}

	// refill buffer; transfer what is needed
	err := x.src.Read(x.buf)
	if err != nil {
		x.w = err.N

		// silently drop error if we have enough data for another read
		if x.w > len(b) {
			err = nil
		} else {
			// we'll return this already existing error value, but with the actual total read length
			err.N += n
		}
	} else {
		x.w = len(x.buf)
	}

	x.r = copy(b, x.buf[:x.w])

	return err
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

// WriteBufferMake returns a WriteBuffer to [dst]. If [buf] is nil, a default will be allocated.
func WriteBufferMake(dst Writer, buf []byte) *WriteBuffer {
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

// Flush writes out any pending data.
func (x *WriteBuffer) Flush() error {
	if x.r == x.w {
		return nil
	}

	if err := x.dst.Write(x.buf[x.r:x.w]); err != nil {
		x.r += err.N
		return err.Err
	}

	x.r = 0
	x.w = 0
	return nil
}

// Any returned error is from underlying Write calls.
func (x *WriteBuffer) Write(b []byte) *Error {
	var n int // transfered byte count from b

	for len(b) > 0 {
		if x.w == 0 && len(b) >= len(x.buf) {
			// large write with no pending data; transfer directly
			if err := x.dst.Write(b); err != nil {
				err.N += n
				return err
			}

			return nil
		}

		m := copy(x.buf[x.w:], b)
		b = b[m:]
		x.w += m
		n += m

		if x.w == len(x.buf) {
			// buffer is full, flush it
			if err := x.dst.Write(x.buf); err != nil {
				x.r = err.N
				err.N = n
				return err
			}

			x.w = 0
		}
	}

	return nil
}
