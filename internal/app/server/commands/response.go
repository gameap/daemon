package commands

import (
	"github.com/et-nik/binngo"
	"github.com/gameap/daemon/internal/app/server/response"
)

type Response struct {
	Output   string
	ExitCode int
	Code     response.Code
}

func (r Response) MarshalBINN() ([]byte, error) {
	resp := []interface{}{r.Code, r.ExitCode, r.Output}
	return binngo.Marshal(&resp)
}
