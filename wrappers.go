package io

import (
	"io"
)

type reader struct {
	io.Reader
}

func (x reader) Close() error {
	return nil
}

type writer struct {
	io.Writer
}

func (x writer) Close() error {
	return nil
}

// ReaderOf attaches a NoOp Close method to an io.Reader.
func ReaderOf(r io.Reader) Reader {
	return reader{r}
}

// WriterOf attaches a NoOp Close method to an io.Writer.
func WriterOf(w io.Writer) Writer {
	return writer{w}
}
