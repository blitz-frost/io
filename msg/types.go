package msg

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/blitz-frost/io"
)

// A Buffer accumulates writes before forwading them in a single call when closing.
// The internal buffer will be grown as needed. It is generally more efficient to reuse a Buffer rather than always allocating a new one.
type Buffer struct {
	dst Writer
	buf []byte
}

func BufferMake(dst Writer) *Buffer {
	return &Buffer{
		dst: dst,
		buf: []byte{}, // avoids panic if canceled with no writes
	}
}

// Cancel discards the internal buffer and closes the destination Writer.
func (x *Buffer) Cancel() error {
	x.buf = x.buf[:0]
	return x.dst.Close()
}

// Close flushes the internal buffer and then closes the destination Writer.
func (x *Buffer) Close() error {
	_, err := x.dst.Write(x.buf)
	x.buf = x.buf[:0]
	if err != nil {
		return err
	}
	return x.dst.Close()
}

// Never returns an error.
func (x *Buffer) Write(b []byte) (int, error) {
	x.buf = append(x.buf, b...)
	return len(b), nil
}

// WriterTake sets the Buffer to write to a new destination.
// It does not reset the internal buffer, nor close the old destination.
// Never returns an error.
func (x *Buffer) WriterTake(dst Writer) error {
	x.dst = dst
	return nil
}

// A BufferGiver wraps a WriterGiver to give a Buffer instead.
// Reuses the same Buffer between calls.
type BufferGiver struct {
	wg  WriterGiver
	buf *Buffer
}

// buf may be nil, in which case a new *Buffer will be allocated.
func BufferGiverMake(wg WriterGiver, buf *Buffer) BufferGiver {
	if buf == nil {
		buf = BufferMake(nil)
	}
	return BufferGiver{
		wg:  wg,
		buf: buf,
	}
}

// The returned value is a *Buffer.
// Returns an interface in order to satisfy the WriterGiver interface.
func (x BufferGiver) Writer() (Writer, error) {
	w, err := x.wg.Writer()
	if err != nil {
		return nil, err
	}
	x.buf.WriterTake(w)
	return x.buf, nil
}

// PacketConn is a convenience type to quickly obtain a Conn from an io.ReadWriter.
type PacketConn struct {
	*PacketReaderChainer
	PacketWriterGiver
}

func PacketConnMake(rw io.ReadWriter) PacketConn {
	return PacketConn{
		PacketReaderChainerMake(rw),
		PacketWriterGiver{rw},
	}
}

// PacketReader is a basic Reader implementation that can be used to adapt an io.Reader to this package's conventions.
// It must be used in tandem with a PacketWriter.
//
// A PacketReader can be used to read a single message, and then must be discarded.
// Closing it does not close the underlying io.Reader.
//
// It is an error to neither Close the PacketReader, nor read from it until EOF, and will very likely lead to undefined behaviour.
type PacketReader struct {
	r      io.Reader
	n      int // remaining bytes in current packet
	closed bool
}

func PacketReaderMake(r io.Reader) *PacketReader {
	return &PacketReader{r: r}
}

// Close discards any unread message data.
func (x *PacketReader) Close() error {
	// closed becomes true when the Read method encounters a message end marker
	if x.closed {
		return nil
	}

	// under normal conditions, a message should have been fully read, so that the next Read call will simply return EOF
	b := make([]byte, 1)
	if _, err := x.Read(b); err == io.EOF {
		return nil
	} else if err != nil {
		// any other error is related to the underlying Reader
		return err
	}

	// no error means the current message end has not been reached
	// dump all remaining data
	b = make([]byte, 4096)
	for {
		if _, err := x.Read(b); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
	}

	return nil // should never be reached
}

// Read reads from the first encountered message into b.
//
// Internally, a PacketReader will process as many packets as necessary to fulfill a Read. Callers need not concern themselves with packet size considerations.
func (x *PacketReader) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	if x.closed {
		return 0, io.EOF
	}

	var o int
	for {
		if x.n == 0 {
			// this is the beginning of a new packet
			var n int64
			nBytes := unsafe.Slice((*byte)(unsafe.Pointer(&n)), 8)
			if _, err := x.r.Read(nBytes); err != nil {
				return o, err
			}

			if n == 0 {
				x.closed = true
				return o, io.EOF
			}

			x.n = int(n)
		}

		if x.n >= len(b) {
			// current packet has enough data
			n, err := x.r.Read(b)
			o += n
			x.n -= n
			return o, err
		}

		// current packet does not have enough data; read what we can and move to next
		n, err := x.r.Read(b[:x.n])
		o += n
		x.n = 0
		if err != nil {
			return o, err
		}
		b = b[n:] // move up the slice
	}

	return 0, nil // this should never be reached
}

// A PacketReaderChainer adapts an io.Reader to a ReaderChainer.
// The passed Readers will be PacketReaders, thus the write side must use PacketWriters.
//
// Inactive until the Listen method is used.
type PacketReaderChainer struct {
	r  io.Reader // data source
	rt ReaderTaker
}

func PacketReaderChainerMake(r io.Reader) *PacketReaderChainer {
	return &PacketReaderChainer{r: r}
}

// Listen executes a message receiving loop. It will only return on chained ReaderTaker error.
func (x *PacketReaderChainer) Listen() error {
	for {
		r := PacketReaderMake(x.r)
		if err := x.rt.ReaderTake(r); err != nil {
			return err
		}
	}
	return nil
}

func (x *PacketReaderChainer) ReaderChain(rt ReaderTaker) error {
	x.rt = rt
	return nil
}

// PacketWriter is a basic Writer implementation that can be used to adapt an io.Writer to this package's conventions.
// It must be used in tandem with a PacketReader.
//
// A PacketWriter can be used to write a single message (up to the first Close method call), and then must be discarded.
// Closing it does not close the underling io.Writer.
type PacketWriter struct {
	w      io.Writer
	used   bool
	closed bool
}

func PacketWriterMake(w io.Writer) *PacketWriter {
	return &PacketWriter{w: w}
}

// Close terminates the message. Any subsequent Write calls will NoOp.
// If no valid Write calls had ever been made before closing, the message is effectively aborted (i.e. a PacketReader will not see a empty messages).
func (x *PacketWriter) Close() error {
	if x.closed {
		return nil
	}
	x.closed = true

	// if there were no Write calls do nothing rather than terminate an empty message
	if !x.used {
		return nil
	}

	// 0 length marks message end
	b := make([]byte, 8)
	_, err := x.w.Write(b)
	return err
}

// Write writes a new packet with content b. The caller controls packet size through len(b).
// Does nothing if len(b) == 0.
func (x *PacketWriter) Write(b []byte) (int, error) {
	if x.closed {
		return 0, io.EOF // conventionally a read error, but its meaning should also be rather intuitive in a write context
	}

	n := int64(len(b))
	if n == 0 {
		return 0, nil
	}

	// prepend packet length
	nBytes := unsafe.Slice((*byte)(unsafe.Pointer(&n)), 8)
	if _, err := x.w.Write(nBytes); err != nil {
		return 0, err
	}
	x.used = true

	return x.w.Write(b)
}

// A PacketWriterGiver wraps an io.Writer to function as a WriterGiver.
// The returned Writers will be PacketWriters, thus the read side must use PacketReaders.
type PacketWriterGiver struct {
	W io.Writer
}

func (x PacketWriterGiver) Writer() (Writer, error) {
	return PacketWriterMake(x.W), nil
}

// ReaderChainerAsync wraps a ReaderChainer to chain asynchronously.
// Its ReaderTake method will return once the chained Reader is closed.
// Must not be used if the ReaderChainer can start multiple concurrent chains.
type ReaderChainerAsync struct {
	rt ReaderTaker
	ch chan struct{}
}

func ReaderChainerAsyncMake(rc ReaderChainer) (*ReaderChainerAsync, error) {
	x := &ReaderChainerAsync{
		ch: make(chan struct{}),
	}
	return x, rc.ReaderChain(x)
}

// Never returns an error.
func (x *ReaderChainerAsync) ReaderTake(r Reader) error {
	go x.rt.ReaderTake(readerAsync{r, x.ch})
	<-x.ch
	return nil
}

// Never returns an error.
func (x *ReaderChainerAsync) ReaderChain(rt ReaderTaker) error {
	x.rt = rt
	return nil
}

type Void struct{}

func (x Void) Close() error {
	return nil
}

func (x Void) ReaderTake(r Reader) error {
	return r.Close()
}

func (x Void) Write(b []byte) (int, error) {
	return len(b), nil
}

// WriterGiverMutex wraps a WriterGiver to block while a provided Writer is in active use, making it concurrent safe.
// Unblocks when the Writer is closed.
type WriterGiverMutex struct {
	wg  WriterGiver
	mux sync.Mutex
}

func WriterGiverMutexMake(wg WriterGiver) *WriterGiverMutex {
	return &WriterGiverMutex{
		wg: wg,
	}
}

func (x *WriterGiverMutex) Writer() (Writer, error) {
	x.mux.Lock()
	w, err := x.wg.Writer()
	if err != nil {
		x.mux.Unlock()
		return nil, err
	}
	return writerMutexMake(w, &x.mux), nil
}

type answer struct {
	r   Reader
	err error
}

// A dispatch generates unique message IDs and delivers back answer messages with matching ID.
type dispatch struct {
	next    uint64
	pending map[uint64]chan answer
	mux     sync.Mutex
}

func dispatchMake() *dispatch {
	return &dispatch{
		pending: make(map[uint64]chan answer),
	}
}

// provision returns a unique ID and a channel to receive an asynchronous response.
// It is the caller's responsibility to resolve or release the provided ID.
func (x *dispatch) provision() (uint64, chan answer) {
	ch := make(chan answer, 1) // cap of 1 so the resolver doesn't block if the provisioning side goroutine exits prematurely

	x.mux.Lock()

	// a bit of an unnecessary check, but just to be clean
	id := x.next
	for _, ok := x.pending[id]; ok; id++ {
		_, ok = x.pending[id]
	}
	x.pending[id] = ch
	x.next = id + 1

	x.mux.Unlock()

	return id, ch
}

// release cleans up the specified ID.
func (x *dispatch) release(id uint64) {
	x.mux.Lock()
	delete(x.pending, id)
	x.mux.Unlock()
}

// resolve delivers a response to the appropriate channel.
func (x *dispatch) resolve(id uint64, resp answer) {
	x.mux.Lock()
	ch, ok := x.pending[id]
	delete(x.pending, id)
	x.mux.Unlock()

	if ok {
		ch <- resp
	}
}

type readerAsync struct {
	r  Reader
	ch chan struct{}
}

func (x readerAsync) Close() error {
	// close reader before allowing the chain to return
	// otherwise there might be a race condition with the r.Close call and the underlying chainer moving on to the next reader (which might force close or need it to be already closed)
	err := x.r.Close()
	x.ch <- struct{}{}
	return err
}

func (x readerAsync) Read(b []byte) (int, error) {
	return x.r.Read(b)
}

type writerMutex struct {
	Writer
	closed *atomic.Bool
	mux    *sync.Mutex
}

func writerMutexMake(w Writer, mux *sync.Mutex) writerMutex {
	return writerMutex{
		Writer: w,
		closed: &atomic.Bool{},
		mux:    mux,
	}
}

func (x writerMutex) Close() error {
	if !x.closed.CompareAndSwap(false, true) {
		return nil
	}
	err := x.Writer.Close()
	x.mux.Unlock()
	return err
}
