package msg

import "github.com/blitz-frost/errors"

func Demultiplex(dst MultiplexInputTaker, in Input) error {
	var ch Int
	if _, err := ch.ReadFrom(in.Read); err != nil {
		err = &errors.T{
			Message: "channel ID read",
			Sub:     err,
		}
		return errors.Join(err, in.Close())
	}

	return dst(int(ch), in)
}
