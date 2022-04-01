// Package io contains types and functions centered on transmiting data through Write and WriteTo.
// Simplifies Writer and WriterTo standard interfaces, as the returned number of bytes is rarely needed and in some cases hard or impossible to meaningfully interpret (ex: MultiWriter).
// An error implementation is provided for those cases when the number of bytes is actually needed.
package io

import (
	"io"
)

type CascadeWriter interface {
	Writer
	WriterTo
}

// A Chainer can dynamically establish where to write its available data.
type Chainer interface {
	Chain(Writer)
	ChainGet() Writer
}

type ChainWriter interface {
	Writer
	Chainer
}

type Closer = io.Closer

type Error struct {
	error     // underlying write error
	N     int // number of written bytes
}

// Link wraps a Writer and is the building block for this package.
type Link struct {
	dst Writer
}

func (x *Link) Chain(w Writer) {
	x.dst = w
}

func (x Link) Close() error {
	if o, ok := x.dst.(io.Closer); ok {
		return o.Close()
	}
	return nil
}

func (x Link) ChainGet() Writer {
	return x.dst
}

func (x Link) Write(b []byte) error {
	return x.dst.Write(b)
}

// A MultiLink writes incoming data to multiple destinations.
// Destinations are written to in the order that they are chained.
type MultiLink []Writer

func NewMultiLink(w ...Writer) *MultiLink {
	x := make(MultiLink, len(w))
	copy(x, w)
	return &x
}

func (x *MultiLink) Chain(w Writer) {
	*x = append(*x, w)
}

func (x *MultiLink) Unchain(w Writer) {
	index := -1
	for i, v := range *x {
		if v == w {
			index = i
			break
		}
	}

	if index == -1 {
		return
	}

	n := len(*x) - 1
	if index < n {
		copy((*x)[index:], (*x)[index+1:])
	}

	*x = (*x)[:n-1]
}

// Write returns the first error encountered when forwarding.
func (x MultiLink) Write(b []byte) error {
	for _, v := range x {
		if err := v.Write(b); err != nil {
			return err
		}
	}

	return nil
}

// A Processor can be conceptualized as a Writer with an automatic Read.
type Processor interface {
	Process([]byte) ([]byte, error)
}

type Reader interface {
	Read() ([]byte, error)
}

type ReadWriter interface {
	Reader
	Writer
}

// A VoidWriter is used to consume data without actually doing anything with it.
type VoidWriter struct {
}

func (x VoidWriter) Write(b []byte) error {
	return nil
}

type Writer interface {
	Write([]byte) error
}

// WriterFunc enable usage of functions with appropriate signature as Writers
type WriterFunc func([]byte) error

func (x WriterFunc) Write(b []byte) error {
	return x(b)
}

type WriterTo interface {
	WriteTo(Writer) error
}

type stdWrapper struct {
	w Writer
}

func (x stdWrapper) Write(b []byte) (int, error) {
	err := x.w.Write(b)
	n := len(b)
	if err != nil {
		n = 0
	}

	return n, x.w.Write(b)
}

// AsStdWriter wraps a Writer as defined in this package to behave as a standard library one.
func AsStdWriter(w Writer) io.Writer {
	// avoid stacked wrapping
	if o, ok := w.(wrapper); ok {
		return o.w
	}
	return stdWrapper{w}
}

type wrapper struct {
	w io.Writer
}

func (x wrapper) Write(b []byte) error {
	_, err := x.w.Write(b)
	return err
}

// FromStdWriter wraps a standard library writer.
func FromStdWriter(w io.Writer) Writer {
	// avoid stacked wrapping
	if o, ok := w.(stdWrapper); ok {
		return o.w
	}
	return wrapper{w}
}
