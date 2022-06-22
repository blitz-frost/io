// Package msg focuses on message based io, i.e. discrete data packets, instead of continuous data streams as when using a simple io.Writer or io.Reader.
// While this could also be achieved through buffering, the approach here is to work with an appropriate io.Closer.
// For example: instead of writing directly to a io.Writer, we request a Writer from a WriterSource, write to it, then close it to signal that the message is finished.
//
// This package exists in order to not overload the root io package, and mirrors its terminology.
package msg

import (
	"github.com/blitz-frost/io"
)

type ReadChainer interface {
	ChainRead(ReaderFrom) error
}

// A Reader may generally self close when done reading, but it shouldn't relly on the user reading until EOF.
type Reader = io.ReadCloser

type ReaderFrom interface {
	ReadFrom(Reader) error
}

type ReaderSource interface {
	Reader() (Reader, error)
}

type WriteChainer interface {
	ChainWrite(WriterTo) error
}

type Writer interface {
	io.WriteCloser
	Cancel() // allow users to cancel a message without sending it
}

type WriterSource interface {
	Writer() (Writer, error)
}

type WriterTo interface {
	WriteTo(Writer) error
}
