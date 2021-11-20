package repositories

import (
	"github.com/pkg/errors"
)

var errInvalidServerID = errors.New("server not found, invalid id")
