package msg

import (
	"github.com/blitz-frost/io"
	"github.com/blitz-frost/msg"
)

type Conn = msg.Conn[Reader, Writer]

// A Demultiplexer chains incoming messages from particular channels to a respective destination.
type Demultiplexer interface {
	ReaderChain(byte, ReaderTaker) error
}

type MultiplexConn interface {
	Demultiplexer
	Multiplexer
}

// A Multiplexer provides Writers to particular channels of a connection.
// A single byte is chosen for mapping, as it should be the easiest to implement while providing more than enough channels for most use cases.
type Multiplexer interface {
	Writer(byte) (Writer, error)
}

type ExchangeConn = msg.Conn[ExchangeReader, ExchangeWriter]

type ExchangeReader interface {
	Reader
	WriterGiver
}

type ExchangeReaderChainer = msg.ReaderChainer[ExchangeReader]

type ExchangeReaderTaker = msg.ReaderTaker[ExchangeReader]

type ExchangeWriter interface {
	Writer
	ReaderGiver
}

type ExchangeWriterGiver = msg.WriterGiver[ExchangeWriter]

type Reader = io.Reader

type ReaderChainer = msg.ReaderChainer[Reader]

type ReaderGiver = msg.ReaderGiver[Reader]

type ReaderTaker = msg.ReaderTaker[Reader]

type Writer = io.Writer

type WriteCanceler interface {
	Writer
	msg.Canceler
}

type WriterGiver = msg.WriterGiver[Writer]
