// Package msg defines the io variants of the github.com/blitz-frost/msg interfaces, as well as several implementation utilities.
//
// See the Packet family for a quick way to adapt io, stream based functionality to a discrete message based one.
//
// Reader.Data can implement io.WriterTakerCaster, and Writer.Data can implement io.ReaderTakerCaster, in order to facilitate direct data transfer without intermediary buffers. Note that these implementations won't be responsible for closing either side.
// This functionality is attributed to Readers/Writers, as they are what is passed along data chains. Thus, it is sufficient for the origin (and possible intermediary wrappers) to provide the necessary functionality, in order for it to be available at every level.
package msg

import (
	"github.com/blitz-frost/io"
	"github.com/blitz-frost/msg"
)

type Canceler = msg.Canceler

type Closer = msg.Closer

type ExchangeInlet func() (ExchangeInput, error)

type ExchangeInput struct {
	Read   Reader
	Close  Closer
	Output Outlet   // independent of Close, i.e. must be able to start writing before finishing to read
	Cancel Canceler // callable at any time before Output

	// optional
	WriteTo WriterTo
}

type ExchangeInputTaker func(ExchangeInput) error

type ExchangeOutlet func() (ExchangeOutput, error)

type ExchangeOutput struct {
	Write  Writer
	Close  Closer // callable before Input, but not mandatory
	Input  Inlet
	Cancel Canceler // callable even during an Input call, unblocking it

	// optional
	ReadFrom ReaderFrom
}

type Inlet func() (Input, error)

type Input struct {
	Read  Reader
	Close Closer

	// optional
	WriteTo WriterTo
}

type InputTaker func(Input) error

type MultiplexInlet func() (int, Input, error)

type MultiplexOutlet func(int) (Output, error)

type MultiplexInputTaker func(int, Input) error

type MultiplexSocket struct {
	Input  MultiplexInlet
	Output MultiplexOutlet
}

type Outlet func() (Output, error)

type Output struct {
	Write Writer
	Close Closer

	// optional
	Cancel   Canceler
	ReadFrom ReaderFrom
}

// Abort is a convenience method that cancels the message if possible, or closes it otherwise.
func (x Output) Abort() error {
	if x.Cancel != nil {
		return x.Cancel()
	}

	return x.Close()
}

type Reader func([]byte) (int, error)

func (x Reader) Read(b []byte) (int, error) {
	return x(b)
}

type ReaderFrom func(Reader) (int64, error)

func ReaderFromOf(rf io.ReaderFrom) ReaderFrom {
	return func(r Reader) (int64, error) {
		return rf.ReadFrom(r)
	}
}

type Socket struct {
	Input  Inlet
	Output Outlet
}

type Writer func([]byte) (int, error)

type WriterTo func(Writer) (int64, error)
