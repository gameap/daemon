package response

import (
	"io"

	"github.com/et-nik/binngo"
	"github.com/et-nik/binngo/decode"
	"github.com/et-nik/binngo/encode"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var errUnknownBinn = errors.New("unknown binn value, cannot be presented as status")

type Response struct {
	Code Code
	Info string
	Data interface{}
}

type Code uint8

const (
	StatusError           Code = 1
	StatusCriticalError   Code = 2
	StatusUnknownCommand  Code = 3
	StatusOK              Code = 100
	StatusReadyToTransfer Code = 101
)

func (r Response) MarshalBINN() ([]byte, error) {
	response := []interface{}{r.Code, r.Info}

	if r.Data != nil {
		response = append(response, r.Data)
	}

	return binngo.Marshal(&response)
}

func (r *Response) UnmarshalBINN(bytes []byte) error {
	var v []interface{}

	err := decode.Unmarshal(bytes, &v)
	if err != nil {
		return err
	}
	if len(v) < 2 {
		return errUnknownBinn
	}

	var code Code

	switch v[0].(type) {
	case uint8:
		code = Code(int(v[0].(uint8)))
	case uint16:
		code = Code(int(v[0].(uint16)))
	case uint32:
		code = Code(int(v[0].(uint32)))
	default:
		return errUnknownBinn
	}

	info, ok := v[1].(string)
	if !ok {
		return errUnknownBinn
	}

	r.Code = code
	r.Info = info

	return nil
}

func WriteResponse(writer io.Writer, r encode.Marshaler) {
	writeBytes, err := binngo.Marshal(&r)
	if err != nil {
		log.Error(err)
		return
	}

	writeBytes = append(writeBytes, []byte{0xFF, 0xFF, 0xFF, 0xFF}...)

	n, err := writer.Write(writeBytes)
	if err != nil {
		log.Warnln(n, errors.WithMessage(err, "failed to write response"))
		return
	}
}
