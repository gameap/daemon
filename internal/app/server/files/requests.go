package files

import (
	"errors"
	"os"
)

var errInvalidMessage = errors.New("unknown binn value, cannot be presented as struct")

type message []interface{}

func convertToCode(val interface{}) (uint8, error) {
	switch v := val.(type) {
	case uint8:
		return v, nil
	case int8:
		return uint8(v), nil
	default:
		return 0, errInvalidMessage
	}
}

func convertToUint64(val interface{}) (uint64, error) {
	switch v := val.(type) {
	case uint:
		return uint64(v), nil
	case int:
		return uint64(v), nil
	case uint8:
		return uint64(v), nil
	case int8:
		return uint64(v), nil
	case uint16:
		return uint64(v), nil
	case int16:
		return uint64(v), nil
	case uint32:
		return uint64(v), nil
	case int32:
		return uint64(v), nil
	case uint64:
		return v, nil
	case int64:
		return uint64(v), nil
	default:
		return 0, errInvalidMessage
	}
}

type readDirMessage struct {
	Directory   string
	DetailsMode bool
}

func createReadDirMessage(m message) (*readDirMessage, error) {
	if len(m) < 3 {
		return nil, errInvalidMessage
	}

	directory, ok := m[1].(string)
	if !ok {
		return nil, errInvalidMessage
	}

	detailsMode, err := convertToCode(m[2])
	if err != nil {
		return nil, err
	}

	return &readDirMessage{
		directory,
		detailsMode != 0,
	}, nil
}

type mkDirMessage struct {
	Directory string
}

func createMkDirMessage(m message) (*mkDirMessage, error) {
	if len(m) < 2 {
		return nil, errInvalidMessage
	}

	directory, ok := m[1].(string)
	if !ok {
		return nil, errInvalidMessage
	}

	return &mkDirMessage{directory}, nil
}

type moveMessage struct {
	Source      string
	Destination string
	Copy        bool
}

func createMoveMessage(m message) (*moveMessage, error) {
	if len(m) < 3 {
		return nil, errInvalidMessage
	}

	source, ok := m[1].(string)
	if !ok {
		return nil, errInvalidMessage
	}

	destination, ok := m[2].(string)
	if !ok {
		return nil, errInvalidMessage
	}

	cp, ok := m[3].(bool)
	if !ok {
		return nil, errInvalidMessage
	}

	return &moveMessage{source, destination, cp}, nil
}

type sendFileToClientMessage struct {
	FilePath string
}

func createSendFileToClientMessage(m message) (*sendFileToClientMessage, error) {
	if len(m) < 3 {
		return nil, errInvalidMessage
	}

	filePath, ok := m[2].(string)
	if !ok {
		return nil, errInvalidMessage
	}

	return &sendFileToClientMessage{filePath}, nil
}

type getFileFromClientMessage struct {
	FilePath string
	FileSize uint64
	MakeDirs bool
	Perms    os.FileMode
}

func createGetFileFromClientMessage(m message) (*getFileFromClientMessage, error) {
	if len(m) < 6 {
		return nil, errInvalidMessage
	}

	filePath, ok := m[2].(string)
	if !ok {
		return nil, errInvalidMessage
	}

	fileSize, err := convertToUint64(m[3])
	if err != nil {
		return nil, errInvalidMessage
	}

	makeDirs, ok := m[4].(bool)
	if !ok {
		return nil, errInvalidMessage
	}

	perms, err := convertToUint64(m[5])
	if err != nil {
		return nil, errInvalidMessage
	}

	return &getFileFromClientMessage{
		FilePath: filePath,
		FileSize: fileSize,
		MakeDirs: makeDirs,
		Perms:    os.FileMode(perms),
	}, nil
}

type removeMessage struct {
	Path      string
	Recursive bool
}

func createRemoveMessage(m message) (*removeMessage, error) {
	if len(m) < 3 {
		return nil, errInvalidMessage
	}

	path, ok := m[1].(string)
	if !ok {
		return nil, errInvalidMessage
	}

	recursive, ok := m[2].(bool)
	if !ok {
		return nil, errInvalidMessage
	}

	return &removeMessage{path, recursive}, nil
}

type fileInfoMessage struct {
	Path string
}

func createFileInfoMessage(m message) (*fileInfoMessage, error) {
	if len(m) < 2 {
		return nil, errInvalidMessage
	}

	path, ok := m[1].(string)
	if !ok {
		return nil, errInvalidMessage
	}

	return &fileInfoMessage{path}, nil
}

type chmodMessage struct {
	Path string
	Perm uint32
}

func createChmodMessage(m message) (*chmodMessage, error) {
	if len(m) < 3 {
		return nil, errInvalidMessage
	}

	path, ok := m[1].(string)
	if !ok {
		return nil, errInvalidMessage
	}

	perm, err := convertToUint64(m[2])
	if err != nil {
		return nil, err
	}

	return &chmodMessage{path, uint32(perm)}, nil
}
