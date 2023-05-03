package files

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/et-nik/binngo/decode"
	"github.com/gameap/daemon/internal/app/server/response"
	servercommon "github.com/gameap/daemon/internal/app/server/server_common"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/otiai10/copy"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type operationHandlerFunc func(ctx context.Context, message anyMessage, readWriter io.ReadWriter) error

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
	var msg anyMessage

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

	if Operation(op) == FileSend {
		err = handler(ctx, msg, readWriter)
		if err != nil {
			return err
		}

		return f.Handle(ctx, readWriter)
	}

	return handler(ctx, msg, readWriter)
}

func writeError(readWriter io.Writer, message string) error {
	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusError,
		Info: message,
	})
}

func readDir(_ context.Context, m anyMessage, readWriter io.ReadWriter) error {
	message, err := createReadDirMessage(m)
	if message == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	dir, err := os.ReadDir(message.Directory)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return writeError(readWriter, "Directory does not exist")
	}
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

func makeDir(ctx context.Context, m anyMessage, readWriter io.ReadWriter) error {
	message, err := createMkDirMessage(m)
	if message == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	err = os.MkdirAll(message.Directory, os.ModePerm)
	if err != nil {
		logger.Error(ctx, err)
		return writeError(readWriter, "Failed to make directory")
	}

	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
	})
}

func moveCopy(ctx context.Context, m anyMessage, readWriter io.ReadWriter) error {
	message, err := createMoveMessage(m)
	if message == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	if _, err := os.Stat(message.Source); errors.Is(err, os.ErrNotExist) {
		return writeError(readWriter, fmt.Sprintf("Source \"%s\" not found", message.Source))
	}

	if _, err := os.Stat(message.Destination); !errors.Is(err, os.ErrNotExist) {
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
			logger.Error(ctx, err)
			return writeError(readWriter, "Failed to copy")
		}
	} else {
		err := os.Rename(message.Source, message.Destination)
		if err != nil {
			logger.Error(ctx, err)
			return writeError(readWriter, "Failed to move")
		}
	}

	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
	})
}

func fileSend(ctx context.Context, m anyMessage, readWriter io.ReadWriter) error {
	if len(m) < 2 {
		return writeError(readWriter, "Invalid message")
	}

	op, err := convertToCode(m[1])
	if err != nil {
		return writeError(readWriter, "Invalid message")
	}

	err = servercommon.ReadEndBytes(ctx, readWriter)
	if err != nil {
		return errors.WithMessage(err, "failed to read end bytes")
	}

	switch op {
	case SendFileToClient:
		return sendFileToClient(ctx, m, readWriter)
	case GetFileFromClient:
		return getFileFromClient(ctx, m, readWriter)
	default:
		return writeError(readWriter, "Invalid file send operation")
	}
}

func sendFileToClient(ctx context.Context, m anyMessage, readWriter io.ReadWriter) error {
	message, err := createSendFileToClientMessage(m)
	if message == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithFields(log.Fields{
		"filepath": message.FilePath,
	}))

	fi, err := os.Stat(message.FilePath)
	if err != nil {
		logger.Error(ctx, err)
		return writeError(readWriter, fmt.Sprintf("File \"%s\" error", message.FilePath))
	}
	if errors.Is(err, os.ErrNotExist) {
		return writeError(readWriter, fmt.Sprintf("File \"%s\" not found", message.FilePath))
	}

	if !fi.Mode().IsRegular() {
		return writeError(readWriter, fmt.Sprintf("\"%s\" is not a file", message.FilePath))
	}

	file, err := os.Open(message.FilePath)

	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			logger.Error(ctx, err)
		}
	}(file)

	if err != nil {
		logger.Error(ctx, err)
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

	logger.Debug(ctx, "Starting file transfer")

	_, err = io.Copy(readWriter, file)
	if err != nil {
		logger.Error(ctx, err)
		return writeError(readWriter, "Failed to transfer file")
	}

	return nil
}

//nolint:funlen
func getFileFromClient(ctx context.Context, m anyMessage, readWriter io.ReadWriter) error {
	message, err := createGetFileFromClientMessage(m)
	if message == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithFields(log.Fields{
		"filepath": message.FilePath,
		"filesize": message.FileSize,
	}))

	logger.Debug(ctx, "Starting transferring file from client")

	dir := filepath.Dir(message.FilePath)
	_, err = os.Stat(dir)

	//nolint:nestif
	if err != nil && errors.Is(err, os.ErrNotExist) {
		if message.MakeDirs {
			err := os.MkdirAll(dir, 0755)
			if err != nil {
				logger.Error(ctx, err)
				return writeError(readWriter, fmt.Sprintf("Failed to make directory \"%s\"", dir))
			}
		} else {
			return writeError(readWriter, fmt.Sprintf("File path \"%s\" not found", dir))
		}
	} else if err != nil {
		logger.Error(ctx, errors.WithMessagef(err, "failed to stat directory \"%s\"", dir))
		return writeError(readWriter, fmt.Sprintf("Directory \"%s\" error", dir))
	}

	tmpFile, err := os.CreateTemp("", filepath.Base(message.FilePath))
	if err != nil {
		logger.Error(ctx, errors.WithMessage(err, "failed to create temp file"))
		return writeError(readWriter, "Failed to create temp file")
	}
	defer func(file *os.File) {
		err = file.Close()
		if err != nil {
			logger.Error(ctx, errors.WithMessage(err, "failed to close temp file"))
		}
		err = os.Remove(file.Name())
		if err != nil {
			logger.Error(ctx, errors.WithMessage(err, "failed to remove temp file"))
		}
	}(tmpFile)

	err = response.WriteResponse(readWriter, response.Response{
		Code: response.StatusReadyToTransfer,
		Info: "File is ready to transfer",
	})
	if err != nil {
		return errors.WithMessage(err, "failed to write ready to transfer response")
	}

	_, err = io.CopyN(tmpFile, readWriter, int64(message.FileSize))
	if err != nil {
		logger.Error(ctx, err)
		return writeError(readWriter, "Failed to transfer file")
	}

	err = copy.Copy(tmpFile.Name(), message.FilePath)
	if err != nil {
		logger.Error(ctx, errors.WithMessage(err, "failed to copy tmp file"))
		return writeError(readWriter, "Failed to copy tmp file")
	}

	logger.Debug(ctx, "File successfully transferred")

	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
	})
}

func remove(ctx context.Context, m anyMessage, readWriter io.ReadWriter) error {
	msg, err := createRemoveMessage(m)
	if msg == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	cleanedPath := filepath.Clean(msg.Path)

	if cleanedPath == "." || cleanedPath == "/" {
		return writeError(readWriter, "Invalid path")
	}

	if _, err = os.Stat(cleanedPath); errors.Is(err, os.ErrNotExist) {
		return writeError(readWriter, "Path not exist")
	}

	if msg.Recursive {
		err = os.RemoveAll(cleanedPath)
	} else {
		err = os.Remove(cleanedPath)
	}

	if err != nil {
		logger.Error(ctx, err)
		return writeError(readWriter, "Failed to remove")
	}

	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
	})
}

func fileInfo(_ context.Context, m anyMessage, readWriter io.ReadWriter) error {
	msg, err := createFileInfoMessage(m)
	if msg == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	r, err := createfileDetailsResponse(msg.Path)
	if err != nil {
		return writeError(readWriter, "Failed to read file details")
	}

	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
		Data: r,
	})
}

func chmod(_ context.Context, m anyMessage, readWriter io.ReadWriter) error {
	msg, err := createChmodMessage(m)
	if msg == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	err = os.Chmod(msg.Path, os.FileMode(msg.Perm))
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
