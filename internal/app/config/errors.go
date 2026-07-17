package config

import (
	"strings"

	"github.com/pkg/errors"
)

var (
	ErrEmptyNodeID   = errors.New("empty node ID")
	ErrEmptyAPIKey   = errors.New("empty API Key")
	ErrNoGRPCAddress = errors.New(
		"gRPC address is not configured: set grpc.address (or api_host as a deprecated fallback)",
	)
	ErrConfigNotFound          = errors.New("configuration file not found")
	ErrUnsupportedConfigFormat = errors.New("unsupported configuration file format")
	ErrNoCACertificate         = errors.New("either ca_certificate or ca_certificate_file must be set")
	ErrNoCertificateChain      = errors.New("either certificate_chain or certificate_chain_file must be set")
	ErrNoPrivateKey            = errors.New("either private_key or private_key_file must be set")
	ErrInvalidSystemDScope     = errors.New(
		"process_manager.config.scope must be 'user' or 'system'",
	)
	ErrScopeOnlyForSystemD = errors.New(
		"process_manager.config.scope is only valid for process_manager.name=systemd",
	)
	ErrEmptyReplacementKey           = errors.New("host key is empty")
	ErrDuplicateReplacementKey       = errors.New("duplicate host key")
	ErrNoReplacementTargets          = errors.New("no replacement targets")
	ErrEmptyReplacementTarget        = errors.New("replacement target is empty")
	ErrEmptyReplacementHost          = errors.New("replacement host is empty")
	ErrReplacementHasQueryOrFragment = errors.New("replacement must not contain a query or a fragment")
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
