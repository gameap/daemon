package domain

import (
	"strconv"
	"strings"
)

type ErrInvalidResponseFromAPI struct {
	body []byte
	code int
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
