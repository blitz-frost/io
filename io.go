// Package io slightly redefines and extends the standard io package.
//
// The Reader and Writer base interfaces are redefined to also be Closers by default, as stream users should always have the clear option of closing the stream.
// It should generally be less awkward to implement NoOp Close methods when not needed, rather than always having to check for Closers to ensure clean termination.
//
// Unlike the standard specification, Readers should always strive to fill the given byte slice, and always return an error on shorter reads.
// It is the caller's responsibility to decide on appropriate fragmented streaming, which they can control through slice length; Readers should not make assumptions in their place.
// In order to accomodate for time sensitive operations, or guard against infinite blocking, Reader implementations should provide explicit timeout mechanisms.
// Conversely, Read calls should not return an error if the read is complete.
package io

import (
	"io"
)

var EOF = io.EOF

type Closer = io.Closer

type Reader = io.ReadCloser

type ReadWriter = io.ReadWriteCloser

type Writer = io.WriteCloser
