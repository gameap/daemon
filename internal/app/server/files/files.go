package files

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/et-nik/binngo/decode"
	"github.com/gameap/daemon/internal/app/server/response"
	servercommon "github.com/gameap/daemon/internal/app/server/server_common"
	"github.com/otiai10/copy"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type operationHandlerFunc func(message message, readWriter io.ReadWriter) error

type Files struct {
	handlers map[Operation]operationHandlerFunc
}

func NewFiles() *Files {
	handlers := map[Operation]operationHandlerFunc{
		FileSend:   fileSend,
		ReadDir:    readDir,
		MakeDir:    makeDir,
		FileMove:   moveCopy,
		FileRemove: remove,
		FileInfo:   fileInfo,
		FileChmod:  chmod,
	}

	return &Files{
		handlers: handlers,
	}
}

func (f *Files) Handle(ctx context.Context, readWriter io.ReadWriter) error {
	var msg message

	decoder := decode.NewDecoder(readWriter)
	err := decoder.Decode(&msg)
	if errors.Is(err, io.EOF) {
		return io.EOF
	}
	if err != nil {
		return response.WriteResponse(readWriter, response.Response{
			Code: response.StatusError,
			Info: "Failed to decode message: " + err.Error(),
		})
	}

	if len(msg) == 0 {
		return response.WriteResponse(readWriter, response.Response{
			Code: response.StatusError,
			Info: "Invalid message",
		})
	}

	op, err := convertToCode(msg[0])
	if err != nil {
		return response.WriteResponse(readWriter, response.Response{
			Code: response.StatusError,
			Info: "Invalid message",
		})
	}

	handler, ok := f.handlers[Operation(op)]
	if !ok {
		return response.WriteResponse(readWriter, response.Response{
			Code: response.StatusError,
			Info: "Invalid operation",
		})
	}

	return handler(msg, readWriter)
}

func writeError(readWriter io.Writer, message string) error {
	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusError,
		Info: message,
	})
}

func readDir(m message, readWriter io.ReadWriter) error {
	message, err := createReadDirMessage(m)
	if message == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	dir, err := os.ReadDir(message.Directory)
	if err != nil {
		return err
	}

	resp := make([]*fileInfoResponse, len(dir))

	for i, f := range dir {
		fi, err := f.Info()
		if err != nil {
			continue
		}

		resp[i] = createFileInfoResponse(fi)
	}

	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
		Data: resp,
	})
}

func makeDir(m message, readWriter io.ReadWriter) error {
	message, err := createMkDirMessage(m)
	if message == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	err = os.MkdirAll(message.Directory, os.ModePerm)
	if err != nil {
		log.Error(err)
		return writeError(readWriter, "Failed to make directory")
	}

	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
	})
}

func moveCopy(m message, readWriter io.ReadWriter) error {
	message, err := createMoveMessage(m)
	if message == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	if _, err := os.Stat(message.Source); os.IsNotExist(err) {
		return writeError(readWriter, fmt.Sprintf("Source \"%s\" not found", message.Source))
	}

	if _, err := os.Stat(message.Destination); !os.IsNotExist(err) {
		return writeError(readWriter, fmt.Sprintf("Destination \"%s\" already exists", message.Destination))
	}

	if message.Copy {
		err := copy.Copy(
			message.Source,
			message.Destination,
			copy.Options{
				OnSymlink: func(src string) copy.SymlinkAction {
					return copy.Shallow
				},
			},
		)
		if err != nil {
			log.Error(err)
			return writeError(readWriter, "Failed to copy")
		}
	} else {
		err := os.Rename(message.Source, message.Destination)
		if err != nil {
			log.Error(err)
			return writeError(readWriter, "Failed to move")
		}
	}

	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
	})
}

func fileSend(m message, readWriter io.ReadWriter) error {
	if len(m) < 2 {
		return writeError(readWriter, "Invalid message")
	}

	op, err := convertToCode(m[1])
	if err != nil {
		return writeError(readWriter, "Invalid message")
	}

	err = servercommon.ReadEndBytes(context.TODO(), readWriter)
	if err != nil {
		return errors.WithMessage(err, "failed to read end bytes")
	}

	switch op {
	case SendFileToClient:
		return uploadFileToClient(m, readWriter)
	case GetFileFromClient:
		return downloadFileFromClient(m, readWriter)
	default:
		return writeError(readWriter, "Invalid file send operation")
	}
}

func uploadFileToClient(m message, readWriter io.ReadWriter) error {
	message, err := createSendFileToClientMessage(m)
	if message == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	fi, err := os.Stat(message.FilePath)
	if err != nil {
		log.Error(err)
		return writeError(readWriter, fmt.Sprintf("File \"%s\" error", message.FilePath))
	}
	if os.IsNotExist(err) {
		return writeError(readWriter, fmt.Sprintf("File \"%s\" not found", message.FilePath))
	}

	if !fi.Mode().IsRegular() {
		return writeError(readWriter, fmt.Sprintf("\"%s\" is not a file", message.FilePath))
	}

	file, err := os.Open(message.FilePath)

	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			log.Error(err)
		}
	}(file)

	if err != nil {
		log.Error(err)
		return writeError(readWriter, fmt.Sprintf("Failed to open file \"%s\"", message.FilePath))
	}

	err = response.WriteResponse(readWriter, response.Response{
		Code: response.StatusReadyToTransfer,
		Info: "File is ready to transfer",
		Data: uint64(fi.Size()),
	})
	if err != nil {
		return err
	}

	_, err = io.Copy(readWriter, file)
	if err != nil {
		log.Error(err)
		return writeError(readWriter, "Failed to transfer file")
	}

	return nil
}

func downloadFileFromClient(m message, readWriter io.ReadWriter) error {
	message, err := createGetFileFromClientMessage(m)
	if message == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	log.Debugf("Starting transferring file from client: filepath: %s, filesize: %d", message.FilePath, message.FileSize)

	dir := path.Dir(message.FilePath)
	_, err = os.Stat(dir)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		if message.MakeDirs {
			err := os.MkdirAll(dir, 0755)
			if err != nil {
				log.Error(err)
				return writeError(readWriter, fmt.Sprintf("Failed to make directory \"%s\"", dir))
			}
		} else {
			return writeError(readWriter, fmt.Sprintf("File path \"%s\" not found", dir))
		}
	} else if err != nil {
		log.Error(err)
		return writeError(readWriter, fmt.Sprintf("Directory \"%s\" error", dir))
	}

	file, err := os.OpenFile(message.FilePath, os.O_CREATE|os.O_WRONLY, message.Perms)
	if err != nil {
		log.Error(err)
		return writeError(readWriter, "Failed to open file")
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			log.Error(err)
		}
	}(file)

	err = response.WriteResponse(readWriter, response.Response{
		Code: response.StatusReadyToTransfer,
		Info: "File is ready to transfer",
	})
	if err != nil {
		return err
	}

	_, err = io.CopyN(file, readWriter, int64(message.FileSize))
	if err != nil {
		log.Error(err)
		return writeError(readWriter, "Failed to transfer file")
	}

	log.Debug("File successfully transfered")

	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
	})
}

func remove(m message, readWriter io.ReadWriter) error {
	message, err := createRemoveMessage(m)
	if message == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	cleanedPath := path.Clean(message.Path)

	if cleanedPath == "." || cleanedPath == "/" {
		return writeError(readWriter, "Invalid path")
	}

	if _, err := os.Stat(cleanedPath); errors.Is(err, os.ErrNotExist) {
		return writeError(readWriter, "Path not exist")
	}

	if message.Recursive {
		err = os.RemoveAll(cleanedPath)
	} else {
		err = os.Remove(cleanedPath)
	}

	if err != nil {
		log.Error(err)
		return writeError(readWriter, "Failed to remove")
	}

	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
	})
}

func fileInfo(m message, readWriter io.ReadWriter) error {
	message, err := createFileInfoMessage(m)
	if message == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	r, err := createfileDetailsResponse(message.Path)
	if err != nil {
		return writeError(readWriter, "Failed to read file details")
	}

	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
		Data: r,
	})
}

func chmod(m message, readWriter io.ReadWriter) error {
	message, err := createChmodMessage(m)
	if message == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	err = os.Chmod(message.Path, os.FileMode(message.Perm))
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return writeError(readWriter, "Path not exist")
	}
	if err != nil {
		return writeError(readWriter, "Failed to change permissions")
	}

	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
	})
}
