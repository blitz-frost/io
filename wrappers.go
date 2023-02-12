package io

import (
	"io"
)

type reader struct {
	r io.Reader
}

func (x reader) Close() error {
	if c, ok := x.r.(Closer); ok {
		return c.Close()
	}
	return nil
}

func (x reader) Read(b []byte) (int, error) {
	for o := 0; o < len(b); {
		n, err := x.r.Read(b[o:])
		o += n
		// discard error if read is actually full
		if err != nil && o < len(b) {
			return o, err
		}
	}
	return len(b), nil
}

type readWriter struct {
	reader    // provides Read + Close
	io.Writer // unmodified Write
}

type writer struct {
	io.Writer
}

func (x writer) Close() error {
	return nil
}

// ReaderOf adapts an [io.Reader] to this package's conventions: full reads and no error on full read.
// If it is not a Closer, it will also get a NoOp Close method.
func ReaderOf(r io.Reader) Reader {
	return reader{r}
}

// ReadWriterOf is the ReadWrite equivalent of ReaderOf.
func ReadWriterOf(rw io.ReadWriter) ReadWriter {
	return readWriter{
		reader{rw},
		rw,
	}
}

// WriterOf attaches a NoOp Close method to an [io.Writer], if it doesn't already have one.
func WriterOf(w io.Writer) Writer {
	if same, ok := w.(Writer); ok {
		return same
	}
	return writer{w}
}
