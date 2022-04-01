package io

import (
	"errors"
	"io"
	"sync"
)

// Copy reads from src to dst until there is a read error.
// Uses an intermediary buffer that is enlarged to always fit incoming data in a single read call.
func Copy(dst Writer, src io.Reader) error {
	b := make([]byte, 100000) // initial buffer size estimate

	for {
		n, err := src.Read(b)
		if err != nil {
			return err
		}

		if err = dst.Write(b[:n]); err != nil {
			return err
		}

		if n == len(b) {
			b = make([]byte, 2*len(b))
		}
	}
}

// A Prefixer attaches a predefined prefix to incoming data.
type Prefixer struct {
	*Link
	buf []byte
	n   int // prefix size
}

func NewPrefixer(prefix []byte) *Prefixer {
	// preload prefix into internal buffer
	b := make([]byte, len(prefix))
	copy(b, prefix)

	pf := &Prefixer{
		Link: &Link{},
		buf:  b,
		n:    len(prefix),
	}

	return pf
}

func (x *Prefixer) Write(b []byte) error {
	x.buf = append(x.buf, b...)

	err := x.Link.Write(x.buf)

	x.buf = x.buf[:x.n]

	return err
}

// A Deviation is used to temporarily modify data flow between a chainer and its target, interposing a ChainerWriter between them.
// Closing it restores the old data flow.
type Deviation struct {
	tmp     ChainWriter
	src     Chainer
	closing bool
	closed  chan bool
}

// NewDeviation chains the given source into a new deviation and returns a pointer to it.
func NewDeviation(tmp ChainWriter, src Chainer) *Deviation {
	dev := &Deviation{
		tmp:    tmp,
		src:    src,
		closed: make(chan bool),
	}

	dev.tmp.Chain(src.ChainGet())
	src.Chain(dev)

	return dev
}

func (x *Deviation) Write(b []byte) error {
	if x.closing {
		<-x.closed
		return x.tmp.ChainGet().Write(b)
	}

	return x.tmp.Write(b)
}

func (x *Deviation) Chain(dst Writer) {
	x.tmp.Chain(dst)
}

func (x *Deviation) GetChain() Writer {
	return x.tmp.ChainGet()
}

func (x *Deviation) Close() error {
	x.closing = true
	var err error
	if v, ok := x.tmp.(io.Closer); ok {
		err = v.Close()
	}
	x.src.Chain(x.tmp.ChainGet())
	close(x.closed)
	return err
}

// A LimitedSwitch can be inserted between a Chainer and its target Writer.
// It will remove itself from the data flow after a set number of Write calls.
type LimitedSwitch struct {
	old   Writer
	tmp   Writer
	src   Chainer
	limit int
}

// NewLimitedSwitch redirects data flow from the source Chainer to a temporary destination, reverting back after limit number of Write calls.
func NewLimitedSwitch(tmp Writer, src Chainer, limit int) *LimitedSwitch {
	swc := &LimitedSwitch{
		old:   src.ChainGet(),
		tmp:   tmp,
		src:   src,
		limit: limit,
	}

	src.Chain(swc)

	return swc
}

func (x *LimitedSwitch) Write(b []byte) error {
	x.limit--
	if x.limit == 0 {
		x.src.Chain(x.old)
	}

	return x.tmp.Write(b)
}

// Chain modifies the normal flow, that will be reverted to when the limit is reached. The switched flow does not change.
func (x *LimitedSwitch) Chain(w Writer) {
	x.old = w
}

// GetChain returns the normal flow destination, not the switched one.
func (x *LimitedSwitch) GetChain() Writer {
	return x.old
}

// An assembler takes arbitrarily cut data, and forwards it as cleanly cut packets.
// TODO replace buffer with a circular one
type Assembler struct {
	*Link
	buf []byte                     // internal buffer, expanded as needed
	fn  func([]byte) ([]byte, int) // defines clean cuts from raw data
}

// NewAssembler returns a new assembler that uses f for the extraction logic.
//
// f should return the clean data as first value, and the number of consumed bytes as second value.
// This second return value may differ from the length of the returned slice, allowing f to preprocess the data packet.
func NewAssembler(f func([]byte) ([]byte, int)) *Assembler {
	return &Assembler{
		Link: &Link{},
		fn:   f,
	}
}

// Write adds the given data to the internal buffer and processes it.
// Forward assembled data to the Link member.
func (x *Assembler) Write(b []byte) error {
	x.buf = append(x.buf, b...)

	n := 0 // used data

	for {
		pk, m := x.fn(x.buf[n:])
		if len(pk) == 0 {
			break
		}
		n += m
		if err := x.Link.Write(pk); err != nil {
			return err
		}
	}

	// shift buffer to clear forwaded data
	// waste of time if n == 0
	if n > 0 {
		copy(x.buf, x.buf[n:])
		x.buf = x.buf[:len(x.buf)-n]
	}

	return nil
}

// An accumulatorTransferRequest holds pending transfer information.
type accumulatorTransferRequest struct {
	i, j int    // start and end index of data in buffer
	w    Writer // destination
}

// An Accumulator buffers incoming data packets into a circular buffer.
// Asynchronously transfers these data packets in fifo order.
type Accumulator struct {
	buf     []byte
	first   int                             // first occupied index
	last    int                             // last occupied index; i == j is interpreted as no data
	next    int                             // first index of last written data
	jump    int                             // last occupied index before jumping to start
	dispo   int                             // available write space
	trans   int                             // transfer size since last WriteTo
	ding    chan int                        // used by transfer routine to communicate read progress
	dong    chan accumulatorTransferRequest // used to signal transfer requests
	closing chan struct{}
	closed  bool
	err     error
	mux     sync.Mutex
	nextW   Writer
}

// NewAccumulator returns a usable Assumulator with internal buffer with given size.
// Transfers can be at most maxDesync calls behind writes.
// maxDesync must be > 0, otherwise it doesn't even make sense to use an accumulator.
func NewAccumulator(size, maxDesync int, next Writer) *Accumulator {
	ac := &Accumulator{
		buf:     make([]byte, size),
		ding:    make(chan int, maxDesync),
		dong:    make(chan accumulatorTransferRequest, maxDesync-1), // total capacity is cap(dong) + 1 pending read
		closing: make(chan struct{}),
		dispo:   size,
		nextW:   next,
	}

	// transfer routine
	go func() {
		for ord := range ac.dong {
			ac.err = ord.w.Write(ac.buf[ord.i:ord.j])
			ac.ding <- ord.j - ord.i
		}
		close(ac.closing)
	}()

	return ac
}

// advance updates internal buffer info by n bytes.
// n must correspond exactly to the first package length.
func (x *Accumulator) advance(n int) {
	x.first += n
	// move to beginning if reaching the jump position
	if x.first == x.jump {
		x.first = 0
		x.jump = len(x.buf)
		x.dispo = len(x.buf) - x.last
	}

	if x.first > x.last {
		x.dispo = x.first - x.last
	}
}

// sync reads from the finished transfer channel and advances internal buffer state.
// Will block until at least one read is made.
func (x *Accumulator) sync() {
	x.advance(<-x.ding)
	for len(x.ding) > 0 {
		x.advance(<-x.ding)
	}
}

// Write copies data to internal buffer.
// Blocks until there is enough space.
// Asynchronously writes it to the chain target.
func (x *Accumulator) Write(b []byte) error {
	x.mux.Lock()
	defer x.mux.Unlock()

	if x.err != nil {
		return x.err
	}
	if x.closed {
		return x.nextW.Write(b)
	}

	if len(b) > len(x.buf) {
		x.err = errors.New("accumulator too small")
		return x.err
	}

	if x.dispo < len(b) {
		// move to beginning if won't fit at the end
		if len(x.buf)-x.last < len(b) {
			x.jump = x.last
			x.last = 0
			x.dispo = x.first
		}
		for x.dispo < len(b) {
			x.sync()
		}
	}

	copy(x.buf[x.last:], b)
	x.next = x.last
	x.last += len(b)
	x.dispo -= len(b)

	// sync if write channel is full
	if len(x.dong) == cap(x.dong) {
		x.sync()
	}
	x.dong <- accumulatorTransferRequest{x.next, x.last, x.nextW}

	return nil
}

func (x *Accumulator) Chain(w Writer) {
	x.nextW = w
}

func (x *Accumulator) GetChain() Writer {
	return x.nextW
}

// Close blocks until all buffered data has been transfered.
// Write blocks until this process is ready, and then becomes synchronous. This allows any dangling writes to complete in a predictible manner.
func (x *Accumulator) Close() error {
	x.mux.Lock()
	defer x.mux.Unlock()

	close(x.dong)
	<-x.closing
	x.closed = true

	return nil
}
