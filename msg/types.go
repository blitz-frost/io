package msg

import (
	"sync"
)

// A Buffer accumulates writes before forwading them in a single call when closing.
// The internal buffer will be grown as needed. It is generally more efficient to reuse a Buffer rather than always allocating a new one.
type Buffer struct {
	dst Writer
	buf []byte
}

func NewBuffer(dst Writer) *Buffer {
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
func MakeBufferGiver(wg WriterGiver, buf *Buffer) BufferGiver {
	if buf == nil {
		buf = NewBuffer(nil)
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

// ReaderChainerAsync wraps a ReaderChainer to chain asynchronously.
// Its ReaderTake method will return once the chained Reader is closed.
// Must not be used if the ReaderChainer can start multiple concurrent chains.
type ReaderChainerAsync struct {
	rt ReaderTaker
	ch chan struct{}
}

func NewReaderChainerAsync(rc ReaderChainer) (*ReaderChainerAsync, error) {
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

func NewWriterGiverMutex(wg WriterGiver) *WriterGiverMutex {
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
	return writerMutex{
		Writer: w,
		mux:    &x.mux,
	}, nil
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

func newDispatch() *dispatch {
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
	mux *sync.Mutex
}

func (x writerMutex) Close() error {
	err := x.Writer.Close()
	x.mux.Unlock()
	return err
}
