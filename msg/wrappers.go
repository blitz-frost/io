package msg

import (
	"errors"
	"unsafe"

	"github.com/blitz-frost/msg"
)

type demultiplexer struct {
	b  []byte
	rt map[byte]ReaderTaker
}

func demultiplexerMake() demultiplexer {
	return demultiplexer{
		b:  make([]byte, 1),
		rt: make(map[byte]ReaderTaker),
	}
}

func (x demultiplexer) ReaderTake(r Reader) error {
	n, err := r.Read(x.b)
	if n < 1 {
		return err
	}

	rt, ok := x.rt[x.b[0]]
	if !ok {
		return errors.New("unexpected channel")
	}

	return rt.ReaderTake(r)
}

func (x demultiplexer) ReaderChain(i byte, rt ReaderTaker) error {
	x.rt[i] = rt
	return nil
}

type exchangeReader struct {
	id uint64

	r  Reader
	wg WriterGiver
}

func (x exchangeReader) Close() error {
	return x.r.Close()
}

func (x exchangeReader) Read(b []byte) (int, error) {
	return x.r.Read(b)
}

func (x exchangeReader) Writer() (Writer, error) {
	return writerWithId(x.wg, x.id)
}

type exchangeReaderChainer struct {
	ert ExchangeReaderTaker
	wg  WriterGiver
}

func exchangeReaderChainerNew(wg WriterGiver) *exchangeReaderChainer {
	return &exchangeReaderChainer{
		wg: wg,
	}
}

func (x *exchangeReaderChainer) ReaderTake(r Reader) error {
	id, err := readId(r)
	if err != nil {
		return err
	}

	er := exchangeReader{
		id: id,
		r:  r,
		wg: x.wg,
	}

	return x.ert.ReaderTake(er)
}

func (x *exchangeReaderChainer) ReaderChain(ert ExchangeReaderTaker) error {
	x.ert = ert
	return nil
}

type exchangeWriter struct {
	id   uint64
	disp *dispatch // needed for id release when closing

	w  Writer
	ch chan answer
}

func (x exchangeWriter) Close() error {
	x.disp.release(x.id)
	if c, ok := x.w.(msg.Canceler); ok {
		return c.Cancel()
	}
	return x.w.Close()
}

func (x exchangeWriter) Reader() (Reader, error) {
	x.w.Close()
	ans := <-x.ch
	return ans.r, ans.err
}

func (x exchangeWriter) Write(b []byte) (int, error) {
	return x.w.Write(b)
}

type exchangeWriterGiver struct {
	wg   WriterGiver
	disp *dispatch
}

func exchangeWriterGiverMake(wg WriterGiver) exchangeWriterGiver {
	return exchangeWriterGiver{
		wg:   wg,
		disp: dispatchNew(),
	}
}

func (x exchangeWriterGiver) ReaderTake(r Reader) error {
	id, err := readId(r)
	if err != nil {
		return err
	}

	x.disp.resolve(id, answer{r, nil})
	return nil
}

func (x exchangeWriterGiver) Writer() (ExchangeWriter, error) {
	id, ch := x.disp.provision()

	w, err := writerWithId(x.wg, id)
	if err != nil {
		return nil, err
	}

	return exchangeWriter{
		id:   id,
		disp: x.disp,
		w:    w,
		ch:   ch,
	}, nil
}

type multiplexConn struct {
	demultiplexer
	multiplexer
}

type multiplexer struct {
	wg WriterGiver
}

func (x multiplexer) Writer(i byte) (Writer, error) {
	w, err := x.wg.Writer()
	if err != nil {
		return nil, err
	}
	_, err = w.Write([]byte{i})
	return w, err
}

type readerChainer struct {
	d Demultiplexer
	i byte
}

func (x readerChainer) ReaderChain(rt ReaderTaker) error {
	return x.d.ReaderChain(x.i, rt)
}

type writerGiver struct {
	m Multiplexer
	i byte
}

func (x writerGiver) Writer() (Writer, error) {
	return x.m.Writer(x.i)
}

func ConnOf(mc MultiplexConn, ch byte) msg.ConnBlock[Reader, Writer] {
	return msg.ConnBlock[Reader, Writer]{readerChainer{mc, ch}, writerGiver{mc, ch}}
}

// DemultiplexerOf wraps rc as a simple Demultiplexer implementation.
// [MultiplexerOf] should be used on the sending end.
// The returned value will also be a [ReaderTaker].
func DemultiplexerOf(rc ReaderChainer) (Demultiplexer, error) {
	x := demultiplexerMake()
	if err := rc.ReaderChain(x); err != nil {
		return nil, err
	}
	return x, nil
}

// The connections for the reader and writer side must be distinct.
func ExchangeConnOf(rExc, wExc Conn) (msg.ConnBlock[ExchangeReader, ExchangeWriter], error) {
	erc := exchangeReaderChainerNew(rExc)
	ewg := exchangeWriterGiverMake(wExc)
	x := msg.ConnBlock[ExchangeReader, ExchangeWriter]{erc, ewg}
	if err := rExc.ReaderChain(erc); err != nil {
		return x, err
	}
	return x, wExc.ReaderChain(ewg)
}

// Note that an ExchangeReaderChainer and an ExchangeWriterGiver cannot be formed onto the same Conn simultaneously.
// Instead, the Conn must first be converted to a MultiplexConn, of which separate channels must be used.
// The returned value is also a [ReaderTaker].
func ExchangeReaderChainerOf(c Conn) (ExchangeReaderChainer, error) {
	x := exchangeReaderChainerNew(c)
	return x, c.ReaderChain(x)
}

// Note that an ExchangeReaderChainer and an ExchangeWriterGiver cannot be formed onto the same Conn simultaneously.
// Instead, the Conn must first be converted to a MultiplexConn, of which separate channels must be used.
// The returned value is also a [ReaderTaker].
func ExchangeWriterGiverOf(c Conn) (ExchangeWriterGiver, error) {
	x := exchangeWriterGiverMake(c)
	return x, c.ReaderChain(x)
}

// The returned value will also be a [ReaderTaker].
func MultiplexConnOf(c Conn) (MultiplexConn, error) {
	x := multiplexConn{demultiplexerMake(), multiplexer{c}}
	return x, c.ReaderChain(x)
}

// MultiplexerOf wraps wg as a simple Multiplexer implementation.
// [DemultiplexerOf] should be used on the receiving end.
func MultiplexerOf(wg WriterGiver) Multiplexer {
	return multiplexer{wg}
}

func ReaderChainerOf(d Demultiplexer, ch byte) ReaderChainer {
	return readerChainer{d, ch}
}

func WriterGiverOf(m Multiplexer, ch byte) WriterGiver {
	return writerGiver{m, ch}
}

func readId(x Reader) (uint64, error) {
	var o uint64
	b := unsafe.Slice((*byte)(unsafe.Pointer(&o)), 8)

	n, err := x.Read(b)
	if n < 8 {
		return 0, err
	}

	return o, nil
}

func writerWithId(x WriterGiver, id uint64) (Writer, error) {
	o, err := x.Writer()
	if err != nil {
		return nil, err
	}

	b := *(*[8]byte)(unsafe.Pointer(&id))
	if _, err = o.Write(b[:]); err != nil {
		return nil, err
	}

	return o, nil
}
