package status

import (
	"github.com/et-nik/binngo/decode"
	"github.com/pkg/errors"
)

type Operation uint8

const (
	Version       Operation = 1
	StatusBase    Operation = 2
	StatusDetails Operation = 3
)

var errInvalidOperationMessage = errors.New("unknown binn value, cannot be presented as operation")

func (o *Operation) UnmarshalBINN(bytes []byte) error {
	var v []uint8

	err := decode.Unmarshal(bytes, &v)
	if err != nil {
		return err
	}
	if len(v) < 1 {
		return errInvalidOperationMessage
	}

	*o = Operation(v[0])

	return nil
}
