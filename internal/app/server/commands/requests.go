package commands

import (
	"errors"

	"github.com/et-nik/binngo/decode"
)

var errInvalidCommandExecMessage = errors.New("unknown binn value, cannot be presented as execute command message")

type commandExec struct {
	Kind uint8
	Command string
	WorkDir string
}

func (s *commandExec) UnmarshalBINN(bytes []byte) error {
	var v []interface{}

	err := decode.Unmarshal(bytes, &v)
	if err != nil {
		return err
	}
	if len(v) < 3 {
		return errInvalidCommandExecMessage
	}

	kind, ok := v[0].(uint8)
	if !ok {
		return errInvalidCommandExecMessage
	}

	command, ok := v[1].(string)
	if !ok {
		return errInvalidCommandExecMessage
	}

	workDir, ok := v[2].(string)
	if !ok {
		return errInvalidCommandExecMessage
	}

	s.Kind = kind
	s.Command = command
	s.WorkDir = workDir

	return nil
}
