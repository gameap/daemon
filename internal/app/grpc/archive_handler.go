package grpc

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	daemonarchive "github.com/gameap/daemon/internal/app/archive"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/semaphore"
)

const (
	defaultMaxConcurrentArchives = 4
	defaultArchiveTimeout        = time.Hour
	defaultProgressInterval      = time.Second

	maxSkippedEntries = 1000
)

// activeArchive tracks one in-flight archive operation so an ArchiveCancel can
// find it and abort its context.
type activeArchive struct {
	cancel context.CancelFunc
	reason atomic.Pointer[string]
}

type archiveProgressState struct {
	filesProcessed int64
	bytesProcessed int64
	currentEntry   string
}

type GRPCArchiveHandler struct {
	workDir        string
	responseSender ResponseSender
	sem            *semaphore.Weighted
	activeArchives sync.Map // map[string]*activeArchive
}

func NewGRPCArchiveHandler(workDir string, responseSender ResponseSender, maxConcurrent int64) *GRPCArchiveHandler {
	if maxConcurrent <= 0 {
		maxConcurrent = defaultMaxConcurrentArchives
	}

	return &GRPCArchiveHandler{
		workDir:        workDir,
		responseSender: responseSender,
		sem:            semaphore.NewWeighted(maxConcurrent),
	}
}

// HandleArchiveRequest handles an archive create/extract request from the API.
// The operation runs in the background; progress and the single final
// ArchiveResponse are delivered through the response sender.
func (h *GRPCArchiveHandler) HandleArchiveRequest(ctx context.Context, req *pb.ArchiveRequest) {
	requestID := req.GetRequestId()
	l := log.WithField("request_id", requestID)

	if requestID == "" {
		l.Error("Archive request with empty request_id, dropping")
		return
	}

	format := archiveRequestFormat(req)

	if req.GetExtract() == nil && req.GetCreate() == nil {
		l.Warn("Archive request without extract or create operation")
		h.sendResponse(&pb.ArchiveResponse{
			RequestId: requestID,
			Error:     "extract or create operation required",
			Format:    format,
		})
		return
	}

	timeout := req.GetTimeout().AsDuration()
	if timeout <= 0 {
		timeout = defaultArchiveTimeout
	}

	opCtx, cancel := context.WithTimeout(ctx, timeout)
	entry := &activeArchive{cancel: cancel}

	// Registered synchronously so an ArchiveCancel arriving right after this
	// request still finds the operation; the goroutine removes the entry.
	if _, loaded := h.activeArchives.LoadOrStore(requestID, entry); loaded {
		cancel()
		l.Warn("Archive request already active, rejecting duplicate")
		h.sendResponse(&pb.ArchiveResponse{
			RequestId: requestID,
			Error:     "archive request already active: " + requestID,
			Format:    format,
		})
		return
	}

	l.Info("Handling archive request")

	go h.run(opCtx, entry, requestID, req, format, l)
}

// HandleArchiveCancel cancels an active archive operation. No response is sent
// here: the operation itself answers with the final ArchiveResponse.
func (h *GRPCArchiveHandler) HandleArchiveCancel(_ context.Context, cancel *pb.ArchiveCancel) {
	requestID := cancel.GetRequestId()

	v, ok := h.activeArchives.Load(requestID)
	if !ok {
		log.WithField("request_id", requestID).Warn("Archive cancel for unknown request")
		return
	}

	entry := v.(*activeArchive)
	if reason := cancel.GetReason(); reason != "" {
		entry.reason.Store(&reason)
	}
	entry.cancel()
}

func (h *GRPCArchiveHandler) run(
	ctx context.Context,
	entry *activeArchive,
	requestID string,
	req *pb.ArchiveRequest,
	format pb.ArchiveFormat,
	l *log.Entry,
) {
	defer h.activeArchives.Delete(requestID)
	defer entry.cancel()

	if err := h.sem.Acquire(ctx, 1); err != nil {
		l.WithError(err).Warn("Failed to acquire archive semaphore")
		h.sendErrorResponse(ctx, entry, requestID, format, err)
		return
	}
	defer h.sem.Release(1)

	var progress atomic.Pointer[archiveProgressState]
	progressFn := func(filesProcessed, bytesProcessed int64, currentEntry string) {
		progress.Store(&archiveProgressState{
			filesProcessed: filesProcessed,
			bytesProcessed: bytesProcessed,
			currentEntry:   currentEntry,
		})
	}

	progressInterval := req.GetProgressInterval().AsDuration()
	if progressInterval <= 0 {
		progressInterval = defaultProgressInterval
	}

	progressDone := make(chan struct{})
	go h.progressLoop(ctx, progressDone, progressInterval, requestID, &progress)

	var result *daemonarchive.Result
	var err error
	if create := req.GetCreate(); create != nil {
		l.WithField("archive_path", create.GetArchivePath()).Info("Creating archive")
		result, err = daemonarchive.Create(ctx, h.workDir, create, progressFn)
	} else {
		extract := req.GetExtract()
		l.WithField("archive_path", extract.GetArchivePath()).Info("Extracting archive")
		result, err = daemonarchive.Extract(ctx, h.workDir, extract, progressFn)
	}

	close(progressDone)

	if err != nil {
		l.WithError(err).Warn("Archive operation failed")
		h.sendErrorResponse(ctx, entry, requestID, format, err)
		return
	}

	skipped := result.Skipped
	if len(skipped) > maxSkippedEntries {
		skipped = skipped[:maxSkippedEntries]
	}

	h.sendResponse(&pb.ArchiveResponse{
		RequestId:      requestID,
		Success:        true,
		FilesProcessed: uint32(result.FilesProcessed),
		BytesProcessed: uint64(result.BytesProcessed),
		ArchiveSize:    uint64(result.ArchiveSize),
		Skipped:        skipped,
		SkippedCount:   uint32(len(result.Skipped)),
		Format:         format,
	})

	l.WithFields(log.Fields{
		"files_processed": result.FilesProcessed,
		"bytes_processed": result.BytesProcessed,
		"archive_size":    result.ArchiveSize,
	}).Info("Archive operation completed")
}

// progressLoop reports the last progress snapshot on every tick until done is
// closed. Totals stay at zero (unknown) as the proto allows.
func (h *GRPCArchiveHandler) progressLoop(
	ctx context.Context,
	done chan struct{},
	interval time.Duration,
	requestID string,
	progress *atomic.Pointer[archiveProgressState],
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			msg := &pb.ArchiveProgress{RequestId: requestID}
			if state := progress.Load(); state != nil {
				msg.FilesProcessed = uint32(state.filesProcessed)
				msg.BytesProcessed = uint64(state.bytesProcessed)
				msg.CurrentEntry = state.currentEntry
			}
			h.responseSender.Send(&pb.DaemonMessage{
				RequestId: requestID,
				Payload: &pb.DaemonMessage_ArchiveProgress{
					ArchiveProgress: msg,
				},
			})
		}
	}
}

func (h *GRPCArchiveHandler) sendErrorResponse(
	ctx context.Context,
	entry *activeArchive,
	requestID string,
	format pb.ArchiveFormat,
	err error,
) {
	errMsg := err.Error()

	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		errMsg = "canceled"
		if reason := entry.reason.Load(); reason != nil && *reason != "" {
			errMsg = "canceled: " + *reason
		}
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		errMsg = "timeout exceeded"
	}

	h.sendResponse(&pb.ArchiveResponse{
		RequestId: requestID,
		Error:     errMsg,
		Format:    format,
	})
}

func (h *GRPCArchiveHandler) sendResponse(resp *pb.ArchiveResponse) {
	h.responseSender.Send(&pb.DaemonMessage{
		RequestId: resp.RequestId,
		Payload: &pb.DaemonMessage_ArchiveResponse{
			ArchiveResponse: resp,
		},
	})
}

func archiveRequestFormat(req *pb.ArchiveRequest) pb.ArchiveFormat {
	if create := req.GetCreate(); create != nil {
		return create.GetFormat()
	}
	if extract := req.GetExtract(); extract != nil {
		return extract.GetFormat()
	}

	return pb.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED
}
