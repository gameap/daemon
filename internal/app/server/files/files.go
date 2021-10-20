package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/et-nik/binngo/decode"
	"github.com/gameap/daemon/internal/app/server/response"
	"github.com/otiai10/copy"
	log "github.com/sirupsen/logrus"
)

type operationHandlerFunc func(message message, readWriter io.ReadWriter)

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

func (f *Files) Handle(_ context.Context, readWriter io.ReadWriter) {
	var msg message

	decoder := decode.NewDecoder(readWriter)
	err := decoder.Decode(&msg)
	if err != nil {
		response.WriteResponse(readWriter, response.Response{
			Code: response.StatusError,
			Info: "Failed to decode message",
		})
		return
	}

	if len(msg) == 0 {
		response.WriteResponse(readWriter, response.Response{
			Code: response.StatusError,
			Info: "Invalid message",
		})
		return
	}

	op, err := convertToCode(msg[0])
	if err != nil {
		response.WriteResponse(readWriter, response.Response{
			Code: response.StatusError,
			Info: "Invalid message",
		})
		return
	}

	handler, ok := f.handlers[Operation(op)]
	if !ok {
		response.WriteResponse(readWriter, response.Response{
			Code: response.StatusError,
			Info: "Invalid operation",
		})
		return
	}

	handler(msg, readWriter)
}

func writeError(readWriter io.Writer, message string) {
	response.WriteResponse(readWriter, response.Response{
		Code: response.StatusError,
		Info: message,
	})
}

func readDir(m message, readWriter io.ReadWriter) {
	message, err := createReadDirMessage(m)
	if message == nil || err != nil {
		writeError(readWriter, "Invalid message")
		return
	}

	dir, err := os.ReadDir(message.Directory)
	if err != nil {
		return
	}

	resp := make([]*fileInfoResponse, len(dir))

	for i, f := range dir {
		fi, err := f.Info()
		if err != nil {
			continue
		}

		resp[i] = createFileInfoResponse(fi)
	}

	response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
		Data: resp,
	})
}

func makeDir(m message, readWriter io.ReadWriter) {
	message, err := createMkDirMessage(m)
	if message == nil || err != nil {
		writeError(readWriter, "Invalid message")
		return
	}

	err = os.MkdirAll(message.Directory, os.ModePerm)
	if err != nil {
		log.Error(err)
		writeError(readWriter, "Failed to make directory")
		return
	}

	response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
	})
}

func moveCopy(m message, readWriter io.ReadWriter) {
	message, err := createMoveMessage(m)
	if message == nil || err != nil {
		writeError(readWriter, "Invalid message")
		return
	}

	if _, err := os.Stat(message.Source); os.IsNotExist(err) {
		writeError(readWriter, fmt.Sprintf("Source \"%s\" not found", message.Source))
		return
	}

	if _, err := os.Stat(message.Destination); !os.IsNotExist(err) {
		writeError(readWriter, fmt.Sprintf("Destination \"%s\" already exists", message.Destination))
		return
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
			writeError(readWriter, "Failed to copy")
			return
		}
	} else {
		err := os.Rename(message.Source, message.Destination)
		if err != nil {
			log.Error(err)
			writeError(readWriter, "Failed to move")
			return
		}
	}

	response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
	})
}

func fileSend(m message, readWriter io.ReadWriter) {
	if len(m) < 2 {
		writeError(readWriter, "Invalid message")
		return
	}

	op, err := convertToCode(m[1])
	if err != nil {
		writeError(readWriter, "Invalid message")
		return
	}

	switch op {
	case SendFileToClient:
		uploadFileToClient(m, readWriter)
	case GetFileFromClient:
		downloadFileFromClient(m, readWriter)
	default:
		writeError(readWriter, "Invalid file send operation")
	}
}

func uploadFileToClient(m message, readWriter io.ReadWriter) {
	message, err := createSendFileToClientMessage(m)
	if message == nil || err != nil {
		writeError(readWriter, "Invalid message")
		return
	}

	fi, err := os.Stat(message.FilePath)
	if err != nil {
		log.Error(err)
		writeError(readWriter, fmt.Sprintf("File \"%s\" error", message.FilePath))
		return
	}
	if os.IsNotExist(err) {
		writeError(readWriter, fmt.Sprintf("File \"%s\" not found", message.FilePath))
		return
	}

	if !fi.Mode().IsRegular() {
		writeError(readWriter, fmt.Sprintf("\"%s\" is not a file", message.FilePath))
		return
	}

	file, err := os.Open(message.FilePath)
	defer file.Close()
	if err != nil {
		log.Error(err)
		writeError(readWriter, fmt.Sprintf("Failed to open file \"%s\"", message.FilePath))
		return
	}

	response.WriteResponse(readWriter, response.Response{
		Code: response.StatusReadyToTransfer,
		Info: "File is ready to transfer",
		Data: uint64(fi.Size()),
	})

	_, err = io.Copy(readWriter, file)
	if err != nil {
		log.Error(err)
		writeError(readWriter, "Failed to transfer file")
		return
	}
}

func downloadFileFromClient(m message, readWriter io.ReadWriter) {
	message, err := createGetFileFromClientMessage(m)
	if message == nil || err != nil {
		writeError(readWriter, "Invalid message")
		return
	}

	dir := path.Dir(message.FilePath)
	_, err = os.Stat(dir)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		if message.MakeDirs {
			err := os.MkdirAll(dir, 0755)
			if err != nil {
				log.Error(err)
				writeError(readWriter, fmt.Sprintf("Failed to make directory \"%s\"", dir))
				return
			}
		} else {
			writeError(readWriter, fmt.Sprintf("File path \"%s\" not found", dir))
			return
		}
	} else if err != nil {
		log.Error(err)
		writeError(readWriter, fmt.Sprintf("Directory \"%s\" error", dir))
		return
	}

	file, err := os.OpenFile(message.FilePath, os.O_CREATE|os.O_WRONLY, message.Perms)
	if err != nil {
		log.Error(err)
		writeError(readWriter, "Failed to open file")
		return
	}
	defer file.Close()

	response.WriteResponse(readWriter, response.Response{
		Code: response.StatusReadyToTransfer,
		Info: "File is ready to transfer",
	})

	_, err = io.CopyN(file, readWriter, int64(message.FileSize))
	if err != nil {
		log.Error(err)
		writeError(readWriter, "Failed to transfer file")
		return
	}

	response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
	})
}

func remove(m message, readWriter io.ReadWriter) {
	message, err := createRemoveMessage(m)
	if message == nil || err != nil {
		writeError(readWriter, "Invalid message")
		return
	}

	cleanedPath := path.Clean(message.Path)

	if cleanedPath == "." || cleanedPath == "/" {
		writeError(readWriter, "Invalid path")
		return
	}

	if _, err := os.Stat(cleanedPath); errors.Is(err, os.ErrNotExist) {
		writeError(readWriter, "Path not exist")
		return
	}

	if message.Recursive {
		err = os.RemoveAll(cleanedPath)
	} else {
		err = os.Remove(cleanedPath)
	}

	if err != nil {
		log.Error(err)
		writeError(readWriter, "Failed to remove")
		return
	}

	response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
	})
}

func fileInfo(m message, readWriter io.ReadWriter) {
	message, err := createFileInfoMessage(m)
	if message == nil || err != nil {
		writeError(readWriter, "Invalid message")
		return
	}

	r, err := createfileDetailsResponse(message.Path)
	if err != nil {
		writeError(readWriter, "Failed to read file details")
		return
	}

	response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
		Data: r,
	})
}

func chmod(m message, readWriter io.ReadWriter) {
	message, err := createChmodMessage(m)
	if message == nil || err != nil {
		writeError(readWriter, "Invalid message")
		return
	}

	err = os.Chmod(message.Path, os.FileMode(message.Perm))
	if err != nil && errors.Is(err, os.ErrNotExist) {
		writeError(readWriter, "Path not exist")
		return
	}
	if err != nil {
		writeError(readWriter, "Failed to change permissions")
		return
	}

	response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
	})
}
