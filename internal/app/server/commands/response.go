package commands

import (
	"github.com/et-nik/binngo"
	"github.com/gameap/daemon/internal/app/server/response"
)

type Response struct {
	Code response.Code
	ExitCode int
	Output string
}

func (r Response) MarshalBINN() ([]byte, error) {
	resp := []interface{}{r.Code, r.ExitCode, r.Output}
	return binngo.Marshal(&resp)
}
