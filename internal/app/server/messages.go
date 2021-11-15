package server

import (
	"errors"

	"github.com/et-nik/binngo/decode"
)

var errUnknownValueAuthMessage = errors.New("cannot be presented as authMessage")

type authMessage struct {
	Login    string
	Password string
	Mode     Mode
}

func createAuthMessageFromSliceInterface(v []interface{}) (*authMessage, error) {
	if len(v) < 4 {
		return nil, errUnknownValueAuthMessage
	}

	login, ok := v[1].(string)
	if !ok {
		return nil, errUnknownValueAuthMessage
	}

	password, ok := v[2].(string)
	if !ok {
		return nil, errUnknownValueAuthMessage
	}

	md := convertToMode(v[3])
	if md == ModeUnknown {
		return nil, errUnknownValueAuthMessage
	}

	return &authMessage{
		login,
		password,
		md,
	}, nil
}

func (am *authMessage) UnmarshalBINN(bytes []byte) error {
	var v []interface{}

	err := decode.Unmarshal(bytes, &v)
	if err != nil {
		return err
	}

	a, err := createAuthMessageFromSliceInterface(v)
	if err != nil {
		return err
	}

	am.Login = a.Login
	am.Password = a.Password
	am.Mode = a.Mode

	return nil
}

func convertToMode(val interface{}) Mode {
	switch v := val.(type) {
	case uint8:
		return Mode(v)
	case uint16:
		return Mode(v)
	case uint32:
		return Mode(v)
	default:
		return ModeUnknown
	}
}
