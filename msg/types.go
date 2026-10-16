package msg

import (
	"math"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/blitz-frost/errors"
	"github.com/blitz-frost/io"
)

type ExchangeInletAdapter struct {
	dst MultiplexOutlet
	ch  chan *exchangeInput
}

// The underlying MultiplexOutlet must be concurrent safe, for the produced Outputs to be concurrent safe.
func ExchangeInletAdapterMake(dst MultiplexOutlet) ExchangeInletAdapter {
	return ExchangeInletAdapter{
		dst: dst,
		ch:  make(chan *exchangeInput),
	}
}

// Close can be used to unblock potentially stuck InputTake() or Input() calls.
//
// Further InputTake() calls will panic.
func (x ExchangeInletAdapter) Close() error {
	ok := true
	for ok {
		select {
		case ein := <-x.ch:
			err := ein.in.Close()
			ein.req.deliver(err)
		default:
			ok = false
		}
	}

	close(x.ch)
	return nil
}

func (x ExchangeInletAdapter) InputTake(id int, in Input) error {
	ein := &exchangeInput{
		in:  in,
		req: requestMake(),
		id:  id,
		dst: x.dst,
	}
	x.ch <- ein

	return <-ein.req.done
}

// Returns *errors.T marked with errors.Break if closed.
func (x ExchangeInletAdapter) Input() (ExchangeInput, error) {
	ein, ok := <-x.ch
	if !ok {
		return ExchangeInput{}, &errors.T{
			Mark: errors.Break,
		}
	}

	return ExchangeInput{
		Read:    ein.in.Read,
		Close:   ein.Close,
		Output:  ein.Output,
		Cancel:  ein.Cancel,
		WriteTo: ein.in.WriteTo,
	}, nil
}

type ExchangeOutletAdapter struct {
	dst  MultiplexOutlet
	disp *dispatch
}

// The underlying MultiplexOutlet must be concurrent safe for concurrent safe ExchangeOutputs to be produced.
func ExchangeOutletAdapterMake(dst MultiplexOutlet) ExchangeOutletAdapter {
	return ExchangeOutletAdapter{
		dst:  dst,
		disp: dispatchMake(math.MaxInt), // reserve sign bit for cancellation messages
	}
}

// Close can be called to unblock goroutines waiting on produced ExchangeOutput.Input() methods.
//
// To be used when the incoming data source has died before delivering all expected responses.
func (x ExchangeOutletAdapter) Close() error {
	x.disp.close()
	return nil
}

func (x ExchangeOutletAdapter) InputTake(id int, in Input) error {
	// check cancellation
	if id < 0 {
		id = -id

		var err errors.Group

		err.Add(in.Close())
		err.Add(x.disp.resolve(id, Input{}))

		return err.Resolve()
	}

	return x.disp.resolve(id, in)
}

func (x ExchangeOutletAdapter) Output() (ExchangeOutput, error) {
	id, resp, err := x.disp.provision()
	if err != nil {
		return ExchangeOutput{}, err
	}

	out, err := x.dst(id)
	if err != nil {
		x.disp.release(id)
		return ExchangeOutput{}, err
	}

	eout := &exchangeOutput{
		out:  out,
		id:   id,
		resp: resp,
		disp: x.disp,
	}

	return ExchangeOutput{
		Write:    out.Write,
		Close:    eout.Close,
		Input:    eout.Input,
		Cancel:   eout.Cancel,
		ReadFrom: out.ReadFrom,
	}, nil
}

type ExchangeSocket struct {
	in  ExchangeInletAdapter
	out ExchangeOutletAdapter
}

func ExchangeSocketMake(dst Outlet) ExchangeSocket {
	const maxId = math.MaxInt >> 1 // reserve second bit for request/response

	// don't need to do anything special on the sending side
	outlet := Multiplexer(dst)

	return ExchangeSocket{
		in: ExchangeInletAdapterMake(outlet),
		out: ExchangeOutletAdapter{
			dst:  outlet,
			disp: dispatchMake(maxId),
		},
	}
}

func (x ExchangeSocket) Close() error {
	var err errors.Group

	err.Add(x.in.Close())
	err.Add(x.out.Close())

	return err.Resolve()
}

func (x ExchangeSocket) Input() (ExchangeInput, error) {
	return x.in.Input()
}

func (x ExchangeSocket) InputTake(in Input) error {
	var id Int
	if _, err := id.ReadFrom(in.Read); err != nil {
		err = &errors.T{
			Message: "exchange ID read",
			Sub:     err,
		}
		return errors.Join(err, in.Close())
	}

	// check request/response bit
	const bit = math.MaxInt>>1 + 1
	if id&bit == 0 {
		// this is an incoming request, must answer with the flipped bit
		id += bit
		return x.in.InputTake(int(id), in)
	} else {
		// this is a response, flip the bit back so that it's recognized by the original sender
		id -= bit
		return x.out.InputTake(int(id), in)
	}
}

func (x ExchangeSocket) Output() (ExchangeOutput, error) {
	return x.out.Output()
}

type Int int

func (x *Int) ReadFrom(r Reader) (int64, error) {
	var i int64 // this makes the encoding more portable
	b := unsafe.Slice((*byte)(unsafe.Pointer(&i)), 8)
	if err := r(b); err != nil {
		return int64(err.N), err.Err
	}
	*x = Int(i)
	return int64(len(b)), nil
}

func (x Int) WriteTo(w Writer) (int64, error) {
	i := int64(x) // for portability
	b := unsafe.Slice((*byte)(unsafe.Pointer(&i)), 8)
	if err := w(b); err != nil {
		return int64(err.N), err.Err
	}
	return int64(len(b)), nil
}

type MultiplexMap map[int]InputTaker

func (x MultiplexMap) InputTake(ch int, in Input) error {
	it, ok := x[ch]
	if !ok {
		err := &errors.T{
			Message: "invalid channel",
			Info:    ch,
		}
		return errors.Join(err, in.Close())
	}

	return it(in)
}

type MultiplexSlice []InputTaker

func (x MultiplexSlice) InputTake(ch int, in Input) error {
	if ch >= len(x) {
		err := &errors.T{
			Message: "invalid channel",
			Info:    ch,
		}
		return errors.Join(err, in.Close())
	}

	return x[ch](in)
}

// A PacketReader can be used to read a discrete message through a regular io.Reader.
// It must be used in tandem with a PacketWriter.
//
// A literal is directly usable, but only for a single message.
type PacketReader struct {
	R io.Reader

	n    int // remaining bytes in current packet
	done bool
}

// Close discards any unread message data.
//
// Can be safely ommited if it is known that the message has been fully read.
func (x *PacketReader) Close() error {
	// done becomes true when the Read method encounters a message end marker
	if x.done {
		return nil
	}

	// under normal conditions, a message should have been fully read, so that the next Read call will simply return EOF
	b := make([]byte, 1)
	if err := x.Read(b); err != nil {
		if err.Err == io.EOF {
			return nil
		} else {
			// any other error is related to the underlying Reader
			return err.Err
		}
	}

	// no error means the current message end has not been reached
	// dump all remaining data
	b = make([]byte, 4096)
	for {
		if err := x.Read(b); err != nil {
			if err.Err == io.EOF {
				return nil
			} else {
				return err
			}
		}
	}
}

// Read reads from the first encountered message into b.
//
// Internally, a PacketReader will process as many packets as necessary to fulfill a Read.
func (x *PacketReader) Read(b []byte) *io.Error {
	if len(b) == 0 {
		return nil
	}
	if x.done {
		return &io.Error{
			N:   0,
			Err: io.EOF,
		}
	}

	var o int
	for {
		if x.n == 0 {
			// this is the beginning of a new packet
			var n int64
			nBytes := unsafe.Slice((*byte)(unsafe.Pointer(&n)), 8)
			if err := x.R.Read(nBytes); err != nil {
				err.N = o
				return err
			}

			if n == 0 {
				x.done = true
				return &io.Error{
					N:   o,
					Err: io.EOF,
				}
			}

			x.n = int(n)
		}

		if x.n >= len(b) {
			// current packet has enough data
			err := x.R.Read(b)
			if err != nil {
				x.n -= err.N
				err.N += o
			} else {
				x.n -= len(b)
			}
			return err
		}

		// current packet does not have enough data; read what we can and move to next
		err := x.R.Read(b[:x.n])
		if err != nil {
			x.n -= err.N
			err.N += o
			return err
		}

		o += x.n
		b = b[x.n:] // move up the slice
		x.n = 0
	}
}

// A PacketWriter can be used to write a discrete message through a regular io.Writer.
// It must be used in tandem with a PacketReader.
//
// A literal is directly usable, but only for a single message.
type PacketWriter struct {
	W io.Writer

	used   bool
	closed bool
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
	if err := x.W.Write(b); err != nil {
		return err
	}

	return nil
}

// Write writes a new packet with content b. The caller controls packet size through len(b).
// Does nothing if len(b) == 0.
func (x *PacketWriter) Write(b []byte) *io.Error {
	if x.closed {
		return &io.Error{0, io.ErrClosed}
	}

	n := int64(len(b))
	if n == 0 {
		return nil
	}

	// prepend packet length
	nBytes := unsafe.Slice((*byte)(unsafe.Pointer(&n)), 8)
	if err := x.W.Write(nBytes); err != nil {
		err.N = 0 // didn't actually write anything from b
		return err
	}
	x.used = true

	return x.W.Write(b)
}

// A dispatch generates unique message IDs and delivers back answer messages with matching ID.
type dispatch struct {
	max     int
	next    int
	pending map[int]response
	mux     sync.Mutex
}

func dispatchMake(maxId int) *dispatch {
	return &dispatch{
		max:     maxId,
		pending: make(map[int]response),
	}
}

// close closes all pending responses
func (x *dispatch) close() {
	x.mux.Lock()

	for _, resp := range x.pending {
		resp.close()
	}

	// prevent any trailing resolve() calls
	clear(x.pending)

	x.mux.Unlock()
}

func (x *dispatch) nextAfter(i int) int {
	if i < x.max {
		return i + 1
	} else {
		return 0
	}
}

// provision returns a unique ID and a means of receiving an asynchronous response.
// It is the caller's responsibility to resolve or release the provided ID.
func (x *dispatch) provision() (int, response, error) {
	resp := responseMake()

	x.mux.Lock()

	// a bit of an unnecessary check, but just to be clean
	id := x.next
	for {
		if _, ok := x.pending[id]; !ok {
			break
		}
		id = x.nextAfter(id)
	}
	x.pending[id] = resp

	x.next = x.nextAfter(id)

	x.mux.Unlock()

	return id, resp, nil
}

// release cleans up the specified ID.
func (x *dispatch) release(id int) {
	x.mux.Lock()

	delete(x.pending, id)

	x.mux.Unlock()
}

// resolve delivers an expected response.
func (x *dispatch) resolve(id int, in Input) error {
	x.mux.Lock()

	resp, ok := x.pending[id]
	delete(x.pending, id)

	x.mux.Unlock()

	if in.Read == nil {
		// cancellation message received
		resp.close()
		return nil
	}

	if !ok {
		// nothing to deliver to
		return in.Close()
	}

	resp.deliver(in)

	err, ok := <-resp.done
	if !ok {
		// the response was closed, meaning nobody received the Input
		return in.Close()
	}

	return err
}

type exchangeInput struct {
	in  Input
	req request

	id  int
	dst MultiplexOutlet

	closed bool
}

// Cancel must be called if a response Output is not needed.
// It may be called before or after Close.
func (x *exchangeInput) Cancel() error {
	var err errors.Group
	if !x.closed {
		err.Add(x.Close())
	}

	err.Add(x.cancel())

	return err.Resolve()
}

// Close must be called when done reading.
// Must not be called after Cancel.
func (x *exchangeInput) Close() error {
	err := x.in.Close()
	x.req.deliver(err)
	x.closed = true
	return err
}

// Writer returns a corresponding response Output.
// May be called independent of Close, but not after Cancel.
func (x *exchangeInput) Output() (Output, error) {
	out, err := x.dst(x.id)
	if err != nil {
		return out, err
	}

	// if the message can be canceled, send a cancellation message when doing so
	if c := out.Cancel; c != nil {
		out.Cancel = func() error {
			if err := c(); err != nil {
				return err
			}

			return x.cancel()
		}
	}

	return out, nil
}

// cancel sends a cancellation message
func (x *exchangeInput) cancel() error {
	id := -x.id // set the sign (cancellation) bit

	out, err := x.dst(id)
	if err != nil {
		return err
	}

	return out.Close()
}

type exchangeOutput struct {
	out  Output
	id   int
	resp response
	disp *dispatch

	closed bool
}

// Cancel must be called if a response Input is not needed.
// Can be used to interrupt a pending Input() method call.
func (x *exchangeOutput) Cancel() error {
	x.disp.release(x.id)
	x.resp.close()

	if !x.closed {
		return x.out.Abort()
	}

	return nil
}

// Close may be called when done writing.
func (x *exchangeOutput) Close() error {
	x.closed = true
	err := x.out.Close()
	if err != nil {
		// failure to send the message should not need any additional logistics
		x.disp.release(x.id)
	}
	return err
}

// Input returns a corresponding response Input.
// Automatically calls Close if has not been already called.
// Must not be called after Cancel.
func (x *exchangeOutput) Input() (Input, error) {
	if !x.closed {
		if err := x.Close(); err != nil {
			return Input{}, err
		}
	}

	in := <-x.resp.ch
	if in.Read == nil {
		return Input{}, errors.Simple("no response")
	}

	c := in.Close
	in.Close = func() error {
		err := c()
		x.resp.done <- err
		return err
	}

	return in, nil
}

type response struct {
	ch        chan Input
	delivered *atomic.Bool

	done chan error
}

func responseMake() response {
	return response{
		ch:        make(chan Input, 1),
		delivered: new(atomic.Bool),
		done:      make(chan error),
	}
}

func (x response) close() {
	if !x.delivered.Swap(true) {
		x.ch <- Input{}

		// if already delivered, let the receiver handle it
		// if already closed, not needed
		close(x.done)
	}
}

func (x response) deliver(in Input) {
	if !x.delivered.Swap(true) {
		x.ch <- in
	}
}

type request struct {
	done      chan error
	delivered *atomic.Bool
}

func requestMake() request {
	return request{
		done:      make(chan error),
		delivered: new(atomic.Bool),
	}
}

func (x request) deliver(err error) {
	if !x.delivered.Swap(true) {
		x.done <- err
	}
}

func ChannelOutlet(ch int, dst MultiplexOutlet) Outlet {
	return func() (Output, error) {
		return dst(ch)
	}
}

func Demultiplexer(src Inlet) MultiplexInlet {
	return func() (int, Input, error) {
		in, err := src()
		if err != nil {
			return 0, Input{}, &errors.T{
				Message: "input",
				Sub:     err,
			}
		}

		var ch Int
		if _, err := ch.ReadFrom(in.Read); err != nil {
			err = &errors.T{
				Message: "channel ID read",
				Sub:     err,
			}
			return 0, Input{}, errors.Join(err, in.Close())
		}

		return int(ch), in, nil
	}
}

func DemultiplexInputTaker(dst MultiplexInputTaker) InputTaker {
	return func(in Input) error {
		return Demultiplex(dst, in)
	}
}

func Multiplexer(dst Outlet) MultiplexOutlet {
	return func(ch int) (Output, error) {
		out, err := dst()
		if err != nil {
			return Output{}, &errors.T{
				Message: "output",
				Sub:     err,
			}
		}

		if _, err := Int(ch).WriteTo(out.Write); err != nil {
			err = &errors.T{
				Message: "channel ID write",
				Sub:     err,
			}
			return Output{}, errors.Join(err, out.Close())
		}

		return out, nil
	}
}

func MultiplexSocketOf(socket Socket) MultiplexSocket {
	return MultiplexSocket{
		Input:  Demultiplexer(socket.Input),
		Output: Multiplexer(socket.Output),
	}
}

func PacketInlet(src io.Reader) Inlet {
	return func() (Input, error) {
		pr := &PacketReader{
			R: src,
		}

		return Input{
			Read:  pr.Read,
			Close: pr.Close,
		}, nil
	}
}

func PacketOutlet(dst io.Writer) Outlet {
	return func() (Output, error) {
		pw := &PacketWriter{
			W: dst,
		}

		return Output{
			Write: pw.Write,
			Close: pw.Close,
		}, nil
	}
}
