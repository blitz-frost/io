package msg

import "github.com/blitz-frost/msg"

type ExchangeLoop struct {
	v msg.Loop

	in ExchangeInput
}

func (x *ExchangeLoop) Close() {
	if x.v.Final == nil {
		x.v.Final = func() error {
			return x.in.Cancel()
		}
	}
	x.v.Close()
}

func (x *ExchangeLoop) Final(taker ExchangeInputTaker) {
	x.v.Final = func() error {
		return taker(x.in)
	}
}

func (x *ExchangeLoop) Run(inlet ExchangeInlet, taker ExchangeInputTaker) error {
	f1 := func() error {
		var err error
		x.in, err = inlet()
		return err
	}

	f2 := func() error {
		return taker(x.in)
	}

	return x.v.Run(f1, f2)
}

type Loop struct {
	v msg.Loop

	in Input
}

func (x *Loop) Close() {
	if x.v.Final == nil {
		x.v.Final = func() error {
			return x.in.Close()
		}
	}
	x.v.Close()
}

// Final arranges what to do with a potential final message that arrives after closing.
// Must be called before Close, or not at all.
// The default is to just close the final message and return the resulting error.
func (x *Loop) Final(taker InputTaker) {
	x.v.Final = func() error {
		return taker(x.in)
	}
}

func (x *Loop) Run(inlet Inlet, taker InputTaker) error {
	f1 := func() error {
		var err error
		x.in, err = inlet()
		return err
	}

	f2 := func() error {
		return taker(x.in)
	}

	return x.v.Run(f1, f2)
}

type MultiplexLoop struct {
	v msg.Loop

	ch int
	in Input
}

func (x *MultiplexLoop) Close() {
	if x.v.Final == nil {
		x.v.Final = func() error {
			return x.in.Close()
		}
	}
	x.v.Close()
}

func (x *MultiplexLoop) Final(taker MultiplexInputTaker) {
	x.v.Final = func() error {
		return taker(x.ch, x.in)
	}
}

func (x *MultiplexLoop) Run(inlet MultiplexInlet, taker MultiplexInputTaker) error {
	f1 := func() error {
		var err error
		x.ch, x.in, err = inlet()
		return err
	}

	f2 := func() error {
		return taker(x.ch, x.in)
	}

	return x.v.Run(f1, f2)
}
