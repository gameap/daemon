package config

import (
	"strings"

	"github.com/pkg/errors"
)

var (
	ErrEmptyNodeID             = errors.New("empty node ID")
	ErrEmptyAPIHost            = errors.New("empty API Host")
	ErrEmptyAPIKey             = errors.New("empty API Key")
	ErrConfigNotFound          = errors.New("configuration file not found")
	ErrUnsupportedConfigFormat = errors.New("unsupported configuration file format")
	ErrNoCACertificate         = errors.New("either ca_certificate or ca_certificate_file must be set")
	ErrNoCertificateChain      = errors.New("either certificate_chain or certificate_chain_file must be set")
	ErrNoPrivateKey            = errors.New("either private_key or private_key_file must be set")
)

type InvalidFileError struct {
	Previous error
	Message  string
}

func NewInvalidFileError(message string, previous error) *InvalidFileError {
	return &InvalidFileError{Previous: previous, Message: message}
}

func (err *InvalidFileError) Error() string {
	text := strings.Builder{}

	text.WriteString(err.Message)
	text.WriteString(": ")
	text.WriteString(err.Previous.Error())

	return text.String()
}
