package status

import "github.com/et-nik/binngo/decode"

type command struct {
	Operation Operation
}

func (s *command) UnmarshalBINN(bytes []byte) error {
	var v []interface{}

	err := decode.Unmarshal(bytes, &v)
	if err != nil {
		return err
	}

	return nil
}
