package io

import (
	"io"
)

type reader struct {
	r io.Reader
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
	reader    // provides Read
	io.Writer // unmodified Write
}

// ReaderOf adapts an [io.Reader] to this package's conventions: full reads and no error on full read.
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
