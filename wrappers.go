package io

import (
	"io"
)

// ReaderAdapter adapts an [io.Reader] to this package's conventions: full reads and no error on full read.
type ReaderAdapter struct {
	R io.Reader
}

func (x ReaderAdapter) Read(b []byte) *Error {
	for o := 0; o < len(b); {
		n, err := x.R.Read(b[o:])
		o += n
		// discard error if read is actually full
		if err != nil && o < len(b) {
			return &Error{
				N:   o,
				Err: err,
			}
		}
	}
	return nil
}

// ReadWriterAdapter is the ReadWrite equivalent of ReaderAdapter.
type ReadWriterAdapter struct {
	ReaderAdapter
	WriterAdapter
}

// StdReader adapts a [Reader] to a standard [io.Reader].
type StdReader struct {
	R Reader
}

func (x StdReader) Read(b []byte) (int, error) {
	err := x.R.Read(b)
	if err != nil {
		return err.N, err.Err
	}
	return len(b), nil
}

type StdWriter struct {
	W Writer
}

func (x StdWriter) Write(b []byte) (int, error) {
	err := x.W.Write(b)
	if err != nil {
		return err.N, err.Err
	}
	return len(b), nil
}

type WriterAdapter struct {
	W io.Writer
}

func (x WriterAdapter) Write(b []byte) *Error {
	n, err := x.W.Write(b)
	if err != nil {
		return &Error{n, err}
	}
	return nil
}
