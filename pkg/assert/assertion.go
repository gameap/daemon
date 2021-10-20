package assert

import (
	"errors"
	"strings"
)

type FailedError struct {
	err     error
	message string
}

func (err *FailedError) Error() string {
	return "assertion failed: " + err.String()
}

func (err *FailedError) Unwrap() error {
	return err.err
}

func (err *FailedError) String() string {
	if err.err != nil {
		return err.err.Error()
	}

	return err.message
}

// NoErrors check that all errors are nil.
func NoErrors(errorList ...error) error {
	var s strings.Builder

	for _, err := range errorList {
		if err != nil {
			if s.Len() > 0 {
				s.WriteString("; ")
			}
			s.WriteString(formatError(err))
		}
	}

	if s.Len() > 0 {
		return &FailedError{message: s.String()}
	}

	return nil
}

type IfAssertion struct{}

// If creates new conditional assertion.
func If(ok bool) *IfAssertion {
	if ok {
		return &IfAssertion{}
	}

	return nil
}

// Then checks that result of expression is true. Otherwise, returns assertion error.
func (assertion *IfAssertion) Then(ok bool, message string) error {
	if assertion == nil {
		return nil
	}

	return That(ok, message)
}

func That(ok bool, message string) error {
	if ok {
		return nil
	}

	return &FailedError{message: message}
}

func formatError(err error) string {
	var failed *FailedError
	if errors.As(err, &failed) {
		return failed.String()
	}

	return err.Error()
}
