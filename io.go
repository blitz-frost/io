// Package io slightly redefines and extends the standard io package.
package io

import (
	"io"
)

var EOF = io.EOF

type Closer = io.Closer

// Unlike the standard specification, a Reader should always strive to fill the given byte slice, and always return an error on shorter reads.
// It is the caller's responsibility to decide on appropriate fragmented streaming, which they can control through slice length; Readers should not make assumptions in their place.
// In order to accomodate for time sensitive operations, or guard against infinite blocking, Reader implementations should provide explicit timeout mechanisms.
// Conversely, Read calls should not return an error if the read is complete.
type Reader = io.Reader

// Unlike the standard specification, ReaderFrom doesn't have to read until EOF.
type ReaderFrom = io.ReaderFrom

type ReadWriter = io.ReadWriter

type Writer = io.Writer
