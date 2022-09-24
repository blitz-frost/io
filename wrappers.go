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
	var (
		o, n int
		err  error
	)
	for o < len(b) && err == nil {
		n, err = x.r.Read(b[o:])
		o += n
	}
	return o, err
}

type writer struct {
	io.Writer
}

func (x writer) Close() error {
	return nil
}

// ReaderOf adapts an [io.Reader] to this package's full read convention.
// If it is not a Closer, it will also get a NoOp Close method.
func ReaderOf(r io.Reader) Reader {
	return reader{r}
}

// WriterOf attaches a NoOp Close method to an [io.Writer].
func WriterOf(w io.Writer) Writer {
	return writer{w}
}
