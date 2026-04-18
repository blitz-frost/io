// Package io slightly redefines and extends the standard io package.
package io

import (
	"io"
	"strconv"

	"github.com/blitz-frost/errors"
)

var (
	EOF       = io.EOF
	ErrClosed = errors.Simple("closed")
)

type Closer = io.Closer

type Error struct {
	// Actually read/written bytes, if any.
	N int

	// Error description.
	// May be nil if the implementation doesn't actually consider it an error, typically when briding between a standard io.Reader and this package's specification.
	Err error
}

func (x *Error) Error() string {
	o := "partial IO: " + strconv.Itoa(x.N) + " bytes"
	if x.Err != nil {
		o += "; err: " + x.Err.Error()
	}
	return o
}

// Reader is an interface specialized in data transmission using well defined data size.
// It does not fully replace standard library io.Reader, which should remain the prefered choice when dealing with unknown sizes.
//
// Unlike the standard specification, a Reader should always strive to fill the given byte slice, and always return an error on shorter reads.
// It is the caller's responsibility to decide on appropriate fragmented streaming, which they can control through slice length; Readers should not make assumptions in their place.
// In order to accomodate for time sensitive operations, or guard against infinite blocking, Reader implementations should provide explicit timeout mechanisms.
// Conversely, Read calls should not return an error if the read is complete.
//
// These conventions prevent having to check both the error and length return value, which might not seem as a big deal at first glance, but can quickly become noisy in code bases dealing extensively with IO.
// There is also the performance consideration of returning and checking an integer value that is usually not needed, but this should be insignificant for most applications.
//
// Returns the concrete Error type, in order to facilitate briding between this and the standard specification, which might otherwise be awkward or even impossible with a general error type.
type Reader interface {
	Read([]byte) *Error
}

// Unlike the standard specification, ReaderFrom doesn't have to read until EOF.
type ReaderFrom interface {
	ReadFrom(r Reader) (int64, error)
}

type ReadWriter interface {
	Reader
	Writer
}

// Writer mirrors the Reader interface, but should functionally not really differ from the standard io.Writer, as that one already specifies that partial writes should return an error.
type Writer interface {
	Write([]byte) *Error
}

type WriterTo interface {
	WriteTo(w Writer) (int64, error)
}
