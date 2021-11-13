package repositories

import (
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

var errInvalidServerID = errors.New("server not found, invalid id")

type ErrInvalidResponseFromAPI struct {
	code int
	body []byte
}

func NewErrInvalidResponseFromAPI(code int, response []byte) ErrInvalidResponseFromAPI {
	return ErrInvalidResponseFromAPI{
		code: code,
		body: response,
	}
}

func (err ErrInvalidResponseFromAPI) Error() string {
	builder := strings.Builder{}

	builder.WriteString("invalid response from api server: ")
	builder.WriteString("(")
	builder.WriteString(strconv.Itoa(err.code))
	builder.WriteString(") ")
	builder.Write(err.body)

	return builder.String()
}
