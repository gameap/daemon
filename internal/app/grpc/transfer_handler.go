package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sync"

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

// HandleFileUploadTask handles a file upload task from the API.
// The API wants the daemon to download a file FROM the API and save it locally.
func (h *GRPCTransferHandler) HandleFileUploadTask(ctx context.Context, requestID string, task *pb.FileUploadTask) {
	l := log.WithFields(log.Fields{
		"transfer_id": task.TransferId,
		"path":        task.Path,
		"request_id":  requestID,
	})

	l.Info("Handling file upload task (download from API)")

	targetPath, err := ResolvePath(h.workDir, task.Path)
	if err != nil {
		l.WithError(err).Error("Failed to resolve path")
		h.sendResponse(requestID, false, err.Error())
		return
	}

	tempPath := targetPath + ".tmp_" + task.TransferId

	// Idempotency check: if target file already exists with matching checksum, skip.
	if task.ChecksumSha256 != "" {
		if existingChecksum, checksumErr := computeFileChecksum(targetPath); checksumErr == nil {
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

	if info, statErr := os.Stat(tempPath); statErr == nil && info.Size() > 0 {
		offset = info.Size()
		hasher, err = hashFileRange(tempPath, offset)
		if err != nil {
			l.WithError(err).Warn("Failed to hash partial temp file, starting fresh")
			offset = 0
			hasher = sha256.New()
			os.Remove(tempPath)
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

	if mkErr := os.MkdirAll(filepath.Dir(targetPath), 0755); mkErr != nil {
		l.WithError(mkErr).Error("Failed to create parent directory")
		h.sendResponse(requestID, false, mkErr.Error())
		return
	}

	file, err := os.OpenFile(tempPath, fileFlags, 0644)
	if err != nil {
		// If append failed (file was removed between stat and open), create fresh.
		if offset > 0 {
			hasher = sha256.New()
			file, err = os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		}
		if err != nil {
			l.WithError(err).Error("Failed to open temp file")
			h.sendResponse(requestID, false, err.Error())
			return
		}
	}

	var receivedChecksum string

	// Stream chunks.
	for {
		chunk, recvErr := stream.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			file.Close()
			if ctx.Err() != nil {
				l.Info("Transfer interrupted by context cancellation, temp file preserved")
				return // temp file preserved for resume, no error response
			}
			l.WithError(recvErr).Error("Failed to receive chunk")
			h.sendResponse(requestID, false, recvErr.Error())
			return
		}

		if chunk.ChecksumSha256 != "" {
			receivedChecksum = chunk.ChecksumSha256
		}

		if chunk.IsFinal {
			continue
		}

		if len(chunk.Data) > 0 {
			if _, writeErr := file.Write(chunk.Data); writeErr != nil {
				file.Close()
				l.WithError(writeErr).Error("Failed to write chunk to temp file")
				h.sendResponse(requestID, false, writeErr.Error())
				return // keep temp file for resume
			}
			hasher.Write(chunk.Data)
		}
	}

	if err := file.Close(); err != nil {
		l.WithError(err).Error("Failed to close temp file")
		h.sendResponse(requestID, false, err.Error())
		return
	}

	// Verify checksum.
	computedChecksum := hex.EncodeToString(hasher.Sum(nil))

	if task.ChecksumSha256 != "" && computedChecksum != task.ChecksumSha256 {
		os.Remove(tempPath)
		errMsg := "checksum mismatch: expected " + task.ChecksumSha256 + ", got " + computedChecksum
		l.Error(errMsg)
		h.sendResponse(requestID, false, errMsg)
		return
	}

	if receivedChecksum != "" && computedChecksum != receivedChecksum {
		os.Remove(tempPath)
		errMsg := "stream checksum mismatch: expected " + receivedChecksum + ", got " + computedChecksum
		l.Error(errMsg)
		h.sendResponse(requestID, false, errMsg)
		return
	}

	// Atomic rename.
	if err := os.Rename(tempPath, targetPath); err != nil {
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

	localPath, err := ResolvePath(h.workDir, task.Path)
	if err != nil {
		l.WithError(err).Error("Failed to resolve path")
		h.sendResponse(requestID, false, err.Error())
		return
	}

	info, err := os.Stat(localPath)
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
		localChecksum, checksumErr := computeFileChecksum(localPath)
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
	file, err := os.Open(localPath)
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

// computeFileChecksum computes the SHA256 checksum of an entire file.
func computeFileChecksum(path string) (string, error) {
	file, err := os.Open(path)
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

// hashFileRange reads the first n bytes of a file into a SHA256 hasher.
// Used to restore hash state when resuming a partial download.
func hashFileRange(path string, n int64) (hash.Hash, error) {
	file, err := os.Open(path)
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
