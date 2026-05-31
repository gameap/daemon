package files

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"time"

	"github.com/et-nik/binngo/decode"
	"github.com/gameap/daemon/internal/app/fsutil"
	"github.com/gameap/daemon/internal/app/server/response"
	servercommon "github.com/gameap/daemon/internal/app/server/server_common"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	// uploadStreamIdleTimeout caps the gap between two successful Reads from
	// the client. As long as bytes keep flowing it never trips, regardless of
	// total upload duration. Tuned for slow networks (Tailscale, NAT) where
	// 13 MB at 17 KB/s legitimately takes 13+ minutes.
	uploadStreamIdleTimeout = 60 * time.Second
)

type connDeadlineSetter interface {
	SetDeadline(t time.Time) error
}

type operationHandlerFunc func(ctx context.Context, message anyMessage, readWriter io.ReadWriter) error

type Files struct {
	workPath string
	handlers map[Operation]operationHandlerFunc
}

func NewFiles(workPath string) *Files {
	f := &Files{workPath: workPath}

	f.handlers = map[Operation]operationHandlerFunc{
		FileSend:   f.fileSend,
		ReadDir:    f.readDir,
		MakeDir:    f.makeDir,
		FileMove:   f.moveCopy,
		FileRemove: f.remove,
		FileInfo:   f.fileInfo,
		FileChmod:  f.chmod,
	}

	return f
}

// openRoot opens an os.Root at the configured work directory. All legacy file
// operations are confined to it: paths supplied by the client are resolved
// component-by-component through this root, which refuses symlink and ".."
// escapes without TOCTOU races on both Linux and Windows.
func (f *Files) openRoot() (*os.Root, error) {
	root, err := os.OpenRoot(f.workPath)
	if err != nil {
		return nil, errors.Wrap(err, "work directory unavailable")
	}

	return root, nil
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

func (f *Files) readDir(ctx context.Context, m anyMessage, readWriter io.ReadWriter) error {
	message, err := createReadDirMessage(m)
	if message == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	root, err := f.openRoot()
	if err != nil {
		return writeError(readWriter, err.Error())
	}
	defer root.Close()

	rel, err := fsutil.RootRel(message.Directory)
	if err != nil {
		return writeError(readWriter, err.Error())
	}

	dir, err := fs.ReadDir(root.FS(), rel)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		logger.Logger(ctx).WithFields(
			log.Fields{
				"operation": "readDir",
				"directory": message.Directory,
			},
		).Debug(
			"Directory does not exist",
		)

		return writeError(readWriter, "Directory does not exist")
	}
	if err != nil {
		return err
	}

	resp := make([]*fileInfoResponse, len(dir))

	for i, entry := range dir {
		fi, err := entry.Info()
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

func (f *Files) makeDir(ctx context.Context, m anyMessage, readWriter io.ReadWriter) error {
	message, err := createMkDirMessage(m)
	if message == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	root, err := f.openRoot()
	if err != nil {
		return writeError(readWriter, err.Error())
	}
	defer root.Close()

	rel, err := fsutil.RootRel(message.Directory)
	if err != nil {
		return writeError(readWriter, err.Error())
	}

	err = root.MkdirAll(rel, os.ModePerm)
	if err != nil {
		logger.Error(ctx, err)
		return writeError(readWriter, "Failed to make directory")
	}

	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
	})
}

func (f *Files) moveCopy(ctx context.Context, m anyMessage, readWriter io.ReadWriter) error {
	message, err := createMoveMessage(m)
	if message == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	root, err := f.openRoot()
	if err != nil {
		return writeError(readWriter, err.Error())
	}
	defer root.Close()

	srcRel, err := fsutil.RootRel(message.Source)
	if err != nil {
		return writeError(readWriter, err.Error())
	}

	dstRel, err := fsutil.RootRel(message.Destination)
	if err != nil {
		return writeError(readWriter, err.Error())
	}

	if _, err := root.Stat(srcRel); errors.Is(err, os.ErrNotExist) {
		return writeError(readWriter, fmt.Sprintf("Source \"%s\" not found", message.Source))
	}

	if _, err := root.Stat(dstRel); !errors.Is(err, os.ErrNotExist) {
		return writeError(readWriter, fmt.Sprintf("Destination \"%s\" already exists", message.Destination))
	}

	if message.Copy {
		err := fsutil.CopyInRoot(root, srcRel, dstRel, fsutil.CopyOptions{Symlink: fsutil.SymlinkShallow})
		if err != nil {
			logger.Error(ctx, err)
			return writeError(readWriter, "Failed to copy")
		}
	} else {
		err := root.Rename(srcRel, dstRel)
		if err != nil {
			logger.Error(ctx, err)
			return writeError(readWriter, "Failed to move")
		}
	}

	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
	})
}

func (f *Files) fileSend(ctx context.Context, m anyMessage, readWriter io.ReadWriter) error {
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
		return f.sendFileToClient(ctx, m, readWriter)
	case GetFileFromClient:
		return f.getFileFromClient(ctx, m, readWriter)
	default:
		return writeError(readWriter, "Invalid file send operation")
	}
}

func (f *Files) sendFileToClient(ctx context.Context, m anyMessage, readWriter io.ReadWriter) error {
	message, err := createSendFileToClientMessage(m)
	if message == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithFields(log.Fields{
		"filepath": message.FilePath,
	}))

	root, err := f.openRoot()
	if err != nil {
		return writeError(readWriter, err.Error())
	}
	defer root.Close()

	rel, err := fsutil.RootRel(message.FilePath)
	if err != nil {
		return writeError(readWriter, err.Error())
	}

	fi, err := root.Stat(rel)
	if err != nil {
		logger.Error(ctx, err)
		return writeError(readWriter, fmt.Sprintf("File \"%s\" error", message.FilePath))
	}

	if !fi.Mode().IsRegular() {
		return writeError(readWriter, fmt.Sprintf("\"%s\" is not a file", message.FilePath))
	}

	file, err := root.Open(rel)

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
func (f *Files) getFileFromClient(ctx context.Context, m anyMessage, readWriter io.ReadWriter) error {
	message, err := createGetFileFromClientMessage(m)
	if message == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithFields(log.Fields{
		"filepath": message.FilePath,
		"filesize": message.FileSize,
	}))

	logger.Debug(ctx, "Starting transferring file from client")

	root, err := f.openRoot()
	if err != nil {
		return writeError(readWriter, err.Error())
	}
	defer root.Close()

	rel, err := fsutil.RootRel(message.FilePath)
	if err != nil {
		return writeError(readWriter, err.Error())
	}

	dir := path.Dir(rel)
	_, err = root.Stat(dir)

	//nolint:nestif
	if err != nil && errors.Is(err, os.ErrNotExist) {
		if message.MakeDirs {
			err := root.MkdirAll(dir, 0755)
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

	var permissions os.FileMode = 0o666
	if stat, statErr := root.Stat(rel); statErr == nil {
		permissions = stat.Mode().Perm()
	}

	tmpRel := rel + ".upload_tmp"
	tmpFile, err := root.OpenFile(tmpRel, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, permissions)
	if err != nil {
		logger.Error(ctx, errors.WithMessage(err, "failed to create temp file"))
		return writeError(readWriter, "Failed to create temp file")
	}
	defer func() {
		// On the success path the temp file has already been closed and
		// renamed away; both calls then fail harmlessly.
		_ = tmpFile.Close()
		_ = root.Remove(tmpRel)
	}()

	logger.Logger(ctx).WithFields(log.Fields{
		"filepath":    message.FilePath,
		"filesize":    message.FileSize,
		"writer_type": fmt.Sprintf("%T", readWriter),
	}).Debug("upload: ready to transfer, sending response")

	err = response.WriteResponse(readWriter, response.Response{
		Code: response.StatusReadyToTransfer,
		Info: "File is ready to transfer",
	})
	if err != nil {
		return errors.WithMessage(err, "failed to write ready to transfer response")
	}

	if d, ok := readWriter.(connDeadlineSetter); ok {
		if dErr := d.SetDeadline(time.Now().Add(uploadStreamIdleTimeout)); dErr != nil {
			logger.Error(ctx, errors.WithMessage(dErr, "failed to set initial upload deadline"))
		} else {
			logger.Logger(ctx).WithFields(log.Fields{
				"idle_timeout": uploadStreamIdleTimeout.String(),
			}).Debug("upload: deadline set with idle-refresh policy")
		}
	} else {
		logger.Logger(ctx).WithFields(log.Fields{
			"writer_type": fmt.Sprintf("%T", readWriter),
		}).Warn("upload: readWriter does not implement SetDeadline; using inherited 5s deadline")
	}

	copyStart := time.Now()
	n, err := copyWithProgress(ctx, tmpFile, readWriter, int64(message.FileSize), uploadStreamIdleTimeout)
	copyDuration := time.Since(copyStart)
	logger.Logger(ctx).WithFields(log.Fields{
		"copied":   n,
		"expected": message.FileSize,
		"duration": copyDuration.String(),
		"rate_kbps": func() int64 {
			secs := int64(copyDuration.Seconds())
			if secs <= 0 {
				secs = 1
			}
			return n / 1024 / secs
		}(),
	}).Debug("upload: io.CopyN finished")
	if err != nil {
		logger.Error(ctx, err)
		return writeError(readWriter, "Failed to transfer file")
	}

	if closeErr := tmpFile.Close(); closeErr != nil {
		logger.Error(ctx, errors.WithMessage(closeErr, "failed to close temp file"))
		return writeError(readWriter, "Failed to finalize upload")
	}

	if chErr := root.Chmod(tmpRel, permissions); chErr != nil {
		logger.Error(ctx, errors.WithMessage(chErr, "failed to set file permissions"))
		return writeError(readWriter, "Failed to set file permissions")
	}

	if mvErr := root.Rename(tmpRel, rel); mvErr != nil {
		logger.Error(ctx, errors.WithMessage(mvErr, "failed to move uploaded file into place"))
		return writeError(readWriter, "Failed to copy tmp file")
	}

	logger.Debug(ctx, "File successfully transferred")

	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
	})
}

func (f *Files) remove(ctx context.Context, m anyMessage, readWriter io.ReadWriter) error {
	msg, err := createRemoveMessage(m)
	if msg == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	root, err := f.openRoot()
	if err != nil {
		return writeError(readWriter, err.Error())
	}
	defer root.Close()

	rel, err := fsutil.RootRel(msg.Path)
	if err != nil {
		return writeError(readWriter, err.Error())
	}

	if rel == "." {
		return writeError(readWriter, "Invalid path")
	}

	if _, err = root.Stat(rel); errors.Is(err, os.ErrNotExist) {
		return writeError(readWriter, "Path not exist")
	}

	if msg.Recursive {
		err = root.RemoveAll(rel)
	} else {
		err = root.Remove(rel)
	}

	if err != nil {
		logger.Error(ctx, err)
		return writeError(readWriter, "Failed to remove")
	}

	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
	})
}

func (f *Files) fileInfo(_ context.Context, m anyMessage, readWriter io.ReadWriter) error {
	msg, err := createFileInfoMessage(m)
	if msg == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	root, err := f.openRoot()
	if err != nil {
		return writeError(readWriter, err.Error())
	}
	defer root.Close()

	rel, err := fsutil.RootRel(msg.Path)
	if err != nil {
		return writeError(readWriter, err.Error())
	}

	r, err := createfileDetailsResponse(root, rel)
	if err != nil {
		return writeError(readWriter, "Failed to read file details")
	}

	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusOK,
		Data: r,
	})
}

func (f *Files) chmod(_ context.Context, m anyMessage, readWriter io.ReadWriter) error {
	msg, err := createChmodMessage(m)
	if msg == nil || err != nil {
		return writeError(readWriter, "Invalid message")
	}

	root, err := f.openRoot()
	if err != nil {
		return writeError(readWriter, err.Error())
	}
	defer root.Close()

	rel, err := fsutil.RootRel(msg.Path)
	if err != nil {
		return writeError(readWriter, err.Error())
	}

	err = root.Chmod(rel, os.FileMode(msg.Perm))
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

// copyWithProgress mirrors io.CopyN with two extras:
//   - logs throughput every 5 seconds so we can tell stalls from slow streams.
//   - if src is a connDeadlineSetter, refreshes the deadline on every successful
//     Read so genuinely-slow networks (e.g. Tailscale, VPN) can take as long as
//     they need provided bytes keep flowing. Idle stalls still trigger timeout.
func copyWithProgress(
	ctx context.Context,
	dst io.Writer,
	src io.Reader,
	n int64,
	idleTimeout time.Duration,
) (int64, error) {
	const (
		bufSize     = 32 * 1024
		logInterval = 5 * time.Second
	)

	var deadlineConn connDeadlineSetter
	if idleTimeout > 0 {
		if c, ok := src.(connDeadlineSetter); ok {
			deadlineConn = c
		}
	}

	buf := make([]byte, bufSize)
	var copied int64
	lastLog := time.Now()
	lastBytes := int64(0)

	for copied < n {
		toRead := int64(bufSize)
		if remaining := n - copied; remaining < toRead {
			toRead = remaining
		}

		readStart := time.Now()
		nr, rerr := src.Read(buf[:toRead])
		if nr > 0 {
			if deadlineConn != nil {
				_ = deadlineConn.SetDeadline(time.Now().Add(idleTimeout))
			}

			nw, werr := dst.Write(buf[:nr])
			copied += int64(nw)
			if werr != nil {
				return copied, werr
			}
			if nw != nr {
				return copied, io.ErrShortWrite
			}
		}

		if time.Since(lastLog) >= logInterval {
			elapsed := time.Since(lastLog)
			delta := copied - lastBytes
			rateKBps := float64(delta) / elapsed.Seconds() / 1024.0
			logger.Logger(ctx).WithFields(log.Fields{
				"copied":            copied,
				"expected":          n,
				"delta_since_last":  delta,
				"interval":          elapsed.String(),
				"rate_kbps":         fmt.Sprintf("%.1f", rateKBps),
				"last_read_size":    nr,
				"last_read_latency": time.Since(readStart).String(),
			}).Debug("upload: progress")
			lastLog = time.Now()
			lastBytes = copied
		}

		if rerr != nil {
			if rerr == io.EOF && copied == n {
				return copied, nil
			}

			return copied, rerr
		}
	}

	return copied, nil
}
