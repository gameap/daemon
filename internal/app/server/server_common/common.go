package servercommon

import (
	"bytes"
	"context"
	"io"

	"github.com/pkg/errors"
)

var ErrInvalidEndBytes = errors.New("invalid message end bytes")

func ReadEndBytes(_ context.Context, reader io.Reader) error {
	endBytes := make([]byte, 4)
	_, err := reader.Read(endBytes)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}

	if !bytes.Equal(endBytes, []byte{0xFF, 0xFF, 0xFF, 0xFF}) {
		return ErrInvalidEndBytes
	}

	return nil
}
