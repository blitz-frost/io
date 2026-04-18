package msg

import "github.com/blitz-frost/errors"

func Demultiplex(dst MultiplexInputTaker, in Input) error {
	var ch Int
	if _, err := ch.ReadFrom(in.Read); err != nil {
		oErr := &errors.T{
			Message: "channel ID read",
			Sub:     err,
		}
		oErr.Link(in.Close())
		return oErr
	}

	return dst(int(ch), in)
}
