package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"os"
	"path"
	"sync"

	"github.com/gameap/daemon/internal/app/fsutil"
	"github.com/gameap/daemon/internal/app/osowner"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/semaphore"
)

const (
	defaultMaxConcurrentTransfers = 4
)

type GRPCTransferHandler struct {
	workDir         string
	fileTransfer    *FileTransferClient
	responseSender  ResponseSender
	sem             *semaphore.Weighted
	activeTransfers sync.Map // map[string]context.CancelFunc
}

func NewGRPCTransferHandler(
	workDir string,
	fileTransfer *FileTransferClient,
	responseSender ResponseSender,
	maxConcurrent int64,
) *GRPCTransferHandler {
	if maxConcurrent <= 0 {
		maxConcurrent = defaultMaxConcurrentTransfers
	}

	return &GRPCTransferHandler{
		workDir:        workDir,
		fileTransfer:   fileTransfer,
		responseSender: responseSender,
		sem:            semaphore.NewWeighted(maxConcurrent),
	}
}

// openRoot opens an os.Root at the work directory so every caller-supplied
// path is resolved component-by-component without symlink/".." escapes or
// TOCTOU races. Opened per request because workDir is provisioned from the
// API after construction.
func (h *GRPCTransferHandler) openRoot() (*os.Root, error) {
	root, err := os.OpenRoot(h.workDir)
	if err != nil {
		return nil, errors.Wrap(err, "work directory unavailable")
	}

	return root, nil
}

// HandleFileUploadTask handles a file upload task from the API.
// The API wants the daemon to download a file FROM the API and save it locally.
func (h *GRPCTransferHandler) HandleFileUploadTask(ctx context.Context, requestID string, task *pb.FileUploadTask) {
	l := log.WithFields(log.Fields{
		"transfer_id": task.TransferId,
		"path":        task.Path,
		"request_id":  requestID,
	})

	l.Info("Handling file upload task (download from API)")

	root, err := h.openRoot()
	if err != nil {
		l.WithError(err).Error("Failed to open work directory")
		h.sendResponse(requestID, false, err.Error())
		return
	}
	defer root.Close()

	rel, err := fsutil.RootRel(task.Path)
	if err != nil {
		l.WithError(err).Error("Failed to resolve path")
		h.sendResponse(requestID, false, err.Error())
		return
	}

	tempRel := rel + ".tmp_" + task.TransferId

	// Idempotency check: if target file already exists with matching checksum, skip.
	if task.ChecksumSha256 != "" {
		if existingChecksum, checksumErr := computeFileChecksum(root, rel); checksumErr == nil {
			if existingChecksum == task.ChecksumSha256 {
				l.Info("File already exists with matching checksum, skipping download")
				h.sendResponse(requestID, true, "")
				return
			}
		}
	}

	// Duplicate check: if transfer is already active, skip.
	if _, loaded := h.activeTransfers.LoadOrStore(task.TransferId, struct{}{}); loaded {
		l.Warn("Transfer already active, skipping duplicate")
		return
	}
	defer h.activeTransfers.Delete(task.TransferId)

	// Resume check: if partial temp file exists, determine offset and re-hash.
	var offset int64
	var hasher hash.Hash

	if info, statErr := root.Stat(tempRel); statErr == nil && info.Size() > 0 {
		offset = info.Size()
		hasher, err = hashFileRange(root, tempRel, offset)
		if err != nil {
			l.WithError(err).Warn("Failed to hash partial temp file, starting fresh")
			offset = 0
			hasher = sha256.New()
			_ = root.Remove(tempRel)
		} else {
			l.WithField("offset", offset).Info("Resuming download from offset")
		}
	} else {
		hasher = sha256.New()
	}

	// Acquire semaphore.
	if err := h.sem.Acquire(ctx, 1); err != nil {
		l.WithError(err).Warn("Failed to acquire transfer semaphore")
		h.sendResponse(requestID, false, "transfer semaphore busy: "+err.Error())

		return
	}
	defer h.sem.Release(1)

	// Start download stream.
	stream, err := h.fileTransfer.DownloadFileStream(ctx, task.TransferId, offset)
	if err != nil {
		l.WithError(err).Error("Failed to start download stream")
		h.sendResponse(requestID, false, errors.Wrap(err, "filetransfer service unavailable").Error())
		return
	}

	// Open temp file.
	var fileFlags int
	if offset > 0 {
		fileFlags = os.O_WRONLY | os.O_APPEND
	} else {
		fileFlags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}

	owner := osowner.Options{
		User: task.OwnerUser,
		UID:  task.OwnerUid,
		GID:  task.OwnerGid,
	}

	parentRel := path.Dir(rel)
	newDirs, segErr := osowner.MissingSegmentsInRoot(root, parentRel)
	if segErr != nil {
		l.WithError(segErr).Error("Failed to stat parent directory")
		h.sendResponse(requestID, false, segErr.Error())
		return
	}

	if mkErr := root.MkdirAll(parentRel, 0755); mkErr != nil {
		l.WithError(mkErr).Error("Failed to create parent directory")
		h.sendResponse(requestID, false, mkErr.Error())
		return
	}

	for _, dir := range newDirs {
		if chErr := osowner.ApplyToPathInRoot(root, dir, owner); chErr != nil {
			l.WithError(chErr).WithField("dir", dir).Error("Failed to chown new parent directory")
			h.sendResponse(requestID, false, chErr.Error())
			return
		}
	}

	fileMode := os.FileMode(0644)
	if task.Mode != 0 {
		fileMode = os.FileMode(task.Mode) & os.ModePerm
	}

	file, err := root.OpenFile(tempRel, fileFlags, fileMode)
	if err != nil {
		if offset > 0 {
			hasher = sha256.New()
			file, err = root.OpenFile(tempRel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode)
		}
		if err != nil {
			l.WithError(err).Error("Failed to open temp file")
			h.sendResponse(requestID, false, err.Error())
			return
		}
	}

	receivedChecksum, streamErr := consumeDownloadStream(stream, file, hasher)
	if streamErr != nil {
		file.Close()
		if ctx.Err() != nil {
			l.Info("Transfer interrupted by context cancellation, temp file preserved")
			return // temp file preserved for resume, no error response
		}
		l.WithError(streamErr).Error("Failed to receive chunk")
		h.sendResponse(requestID, false, streamErr.Error())
		return
	}

	if err := file.Close(); err != nil {
		l.WithError(err).Error("Failed to close temp file")
		h.sendResponse(requestID, false, err.Error())
		return
	}

	// Verify checksum.
	computedChecksum := hex.EncodeToString(hasher.Sum(nil))

	if task.ChecksumSha256 != "" && computedChecksum != task.ChecksumSha256 {
		_ = root.Remove(tempRel)
		errMsg := "checksum mismatch: expected " + task.ChecksumSha256 + ", got " + computedChecksum
		l.Error(errMsg)
		h.sendResponse(requestID, false, errMsg)
		return
	}

	if receivedChecksum != "" && computedChecksum != receivedChecksum {
		_ = root.Remove(tempRel)
		errMsg := "stream checksum mismatch: expected " + receivedChecksum + ", got " + computedChecksum
		l.Error(errMsg)
		h.sendResponse(requestID, false, errMsg)
		return
	}

	if chErr := osowner.ApplyToPathInRoot(root, tempRel, owner); chErr != nil {
		l.WithError(chErr).Error("Failed to chown temp file before rename")
		_ = root.Remove(tempRel)
		h.sendResponse(requestID, false, chErr.Error())
		return
	}

	if err := root.Rename(tempRel, rel); err != nil {
		l.WithError(err).Error("Failed to rename temp file to target")
		h.sendResponse(requestID, false, err.Error())
		return
	}

	l.Info("File upload task completed successfully")
	h.sendResponse(requestID, true, "")
}

// HandleFileDownloadTask handles a file download task from the API.
// The API wants the daemon to upload a local file TO the API.
func (h *GRPCTransferHandler) HandleFileDownloadTask(ctx context.Context, requestID string, task *pb.FileDownloadTask) {
	l := log.WithFields(log.Fields{
		"transfer_id": task.TransferId,
		"path":        task.Path,
		"request_id":  requestID,
	})

	l.Info("Handling file download task (upload to API)")

	root, err := h.openRoot()
	if err != nil {
		l.WithError(err).Error("Failed to open work directory")
		h.sendResponse(requestID, false, err.Error())
		return
	}
	defer root.Close()

	rel, err := fsutil.RootRel(task.Path)
	if err != nil {
		l.WithError(err).Error("Failed to resolve path")
		h.sendResponse(requestID, false, err.Error())
		return
	}

	info, err := root.Stat(rel)
	if err != nil {
		l.WithError(err).Error("File not found")
		h.sendResponse(requestID, false, err.Error())
		return
	}

	if info.IsDir() {
		h.sendResponse(requestID, false, "path is a directory")
		return
	}

	// Dedup check: if API provided an expected checksum and local file matches, skip upload.
	if task.ChecksumSha256 != "" {
		localChecksum, checksumErr := computeFileChecksum(root, rel)
		if checksumErr == nil && localChecksum == task.ChecksumSha256 {
			l.Info("File checksum matches expected, skipping upload (dedup)")
			h.sendResponse(requestID, true, "")
			return
		}
	}

	// Duplicate check.
	if _, loaded := h.activeTransfers.LoadOrStore(task.TransferId, struct{}{}); loaded {
		l.Warn("Transfer already active, skipping duplicate")
		return
	}
	defer h.activeTransfers.Delete(task.TransferId)

	// Acquire semaphore.
	if err := h.sem.Acquire(ctx, 1); err != nil {
		l.WithError(err).Warn("Failed to acquire transfer semaphore")
		h.sendResponse(requestID, false, "transfer semaphore busy: "+err.Error())

		return
	}
	defer h.sem.Release(1)

	// Open local file.
	file, err := root.Open(rel)
	if err != nil {
		l.WithError(err).Error("Failed to open file")
		h.sendResponse(requestID, false, err.Error())
		return
	}
	defer file.Close()

	// Upload via FileTransferService.
	_, err = h.fileTransfer.UploadFileForTransfer(ctx, task.TransferId, file, info.Size(), info.Mode())
	if err != nil {
		if ctx.Err() != nil {
			l.Info("Upload interrupted by context cancellation")
			return
		}
		l.WithError(err).Error("Failed to upload file")
		h.sendResponse(requestID, false, err.Error())
		return
	}

	l.Info("File download task completed successfully")
	h.sendResponse(requestID, true, "")
}

func (h *GRPCTransferHandler) sendResponse(requestID string, success bool, errMsg string) {
	h.responseSender.Send(&pb.DaemonMessage{
		RequestId: requestID,
		Payload: &pb.DaemonMessage_FileWriteResponse{
			FileWriteResponse: &pb.FileWriteResponse{
				RequestId: requestID,
				Success:   success,
				Error:     errMsg,
			},
		},
	})
}

// computeFileChecksum computes the SHA256 checksum of an entire file inside root.
func computeFileChecksum(root *os.Root, rel string) (string, error) {
	file, err := root.Open(rel)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// hashFileRange reads the first n bytes of a file inside root into a SHA256
// hasher. Used to restore hash state when resuming a partial download.
func hashFileRange(root *os.Root, rel string, n int64) (hash.Hash, error) {
	file, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.CopyN(hasher, file, n); err != nil {
		return nil, err
	}

	return hasher, nil
}
