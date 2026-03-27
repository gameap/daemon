package grpc

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/processmanager"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	defaultMaxSessionsPerServer = 10
	defaultMaxSessionsTotal     = 50
	defaultIdleTimeout          = 30 * time.Minute
	idleCheckInterval           = 1 * time.Minute
	outputBufSize               = 4096
	outputFlushInterval         = 100 * time.Millisecond
)

type attachSession struct {
	sessionID string
	serverID  uint64
	stdinPW   *io.PipeWriter
	cancel    context.CancelFunc
	lastInput atomic.Value // time.Time
}

type GRPCAttachHandler struct {
	serverRepo     domain.ServerRepository
	processManager contracts.ProcessManager
	sender         ResponseSender

	sessions map[string]*attachSession
	mu       sync.Mutex

	maxPerServer int
	maxTotal     int
	idleTimeout  time.Duration
}

func NewGRPCAttachHandler(
	serverRepo domain.ServerRepository,
	processManager contracts.ProcessManager,
	sender ResponseSender,
) *GRPCAttachHandler {
	return &GRPCAttachHandler{
		serverRepo:     serverRepo,
		processManager: processManager,
		sender:         sender,
		sessions:       make(map[string]*attachSession),
		maxPerServer:   defaultMaxSessionsPerServer,
		maxTotal:       defaultMaxSessionsTotal,
		idleTimeout:    defaultIdleTimeout,
	}
}

func (h *GRPCAttachHandler) RunIdleChecker(ctx context.Context) {
	ticker := time.NewTicker(idleCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.checkIdleSessions()
		}
	}
}

func (h *GRPCAttachHandler) checkIdleSessions() {
	h.mu.Lock()
	var toCancel []*attachSession
	for _, s := range h.sessions {
		if last, ok := s.lastInput.Load().(time.Time); ok {
			if time.Since(last) > h.idleTimeout {
				toCancel = append(toCancel, s)
			}
		}
	}
	h.mu.Unlock()

	for _, s := range toCancel {
		log.WithField("session_id", s.sessionID).Info("Closing idle attach session")
		s.cancel()
		s.stdinPW.Close()
	}
}

func (h *GRPCAttachHandler) HandleAttachRequest(ctx context.Context, req *pb.AttachRequest) {
	sessionID := req.GetSessionId()
	serverID := req.GetServerId()

	logEntry := log.WithFields(log.Fields{
		"session_id": sessionID,
		"server_id":  serverID,
	})

	// Check limits.
	h.mu.Lock()
	if len(h.sessions) >= h.maxTotal {
		h.mu.Unlock()
		logEntry.Warn("Attach session limit exceeded (total)")
		h.sendAttachClosed(sessionID, "too many sessions", -1)
		return
	}

	perServer := 0
	for _, s := range h.sessions {
		if s.serverID == serverID {
			perServer++
		}
	}
	if perServer >= h.maxPerServer {
		h.mu.Unlock()
		logEntry.Warn("Attach session limit exceeded (per server)")
		h.sendAttachClosed(sessionID, "too many sessions", -1)
		return
	}
	h.mu.Unlock()

	server, err := h.serverRepo.FindByID(ctx, int(serverID))
	if err != nil {
		logEntry.WithError(err).Error("Failed to find server for attach")
		h.sendAttachClosed(sessionID, "server not found", -1)
		return
	}
	if server == nil {
		logEntry.Warn("Server not found for attach")
		h.sendAttachClosed(sessionID, "server not found", -1)
		return
	}

	stdinPR, stdinPW := io.Pipe()
	outputWriter := newAttachOutputWriter(sessionID, h.sender)

	sessionCtx, sessionCancel := context.WithCancel(ctx)

	sess := &attachSession{
		sessionID: sessionID,
		serverID:  serverID,
		stdinPW:   stdinPW,
		cancel:    sessionCancel,
	}
	sess.lastInput.Store(time.Now())

	h.mu.Lock()
	h.sessions[sessionID] = sess
	h.mu.Unlock()

	h.sender.Send(&pb.DaemonMessage{
		Payload: &pb.DaemonMessage_AttachStarted{
			AttachStarted: &pb.AttachStarted{
				SessionId: sessionID,
				ServerId:  serverID,
			},
		},
	})

	logEntry.Info("Attach session started")

	go func() {
		defer stdinPR.Close()
		defer stdinPW.Close()
		defer outputWriter.Close()
		defer sessionCancel()

		attachErr := h.processManager.Attach(sessionCtx, server, stdinPR, outputWriter)

		h.mu.Lock()
		delete(h.sessions, sessionID)
		h.mu.Unlock()

		reason, exitCode := h.resolveCloseReason(attachErr)
		h.sendAttachClosed(sessionID, reason, exitCode)

		logEntry.WithField("reason", reason).Info("Attach session closed")
	}()
}

func (h *GRPCAttachHandler) resolveCloseReason(err error) (string, int32) {
	if err == nil {
		return "process exited", 0
	}
	if errors.Is(err, context.Canceled) {
		return "detached", 0
	}
	if errors.Is(err, processmanager.ErrNotImplemented) {
		return "attach not supported", -1
	}
	if errors.Is(err, processmanager.ErrContainerNotRunning) {
		return "server not running", -1
	}
	return err.Error(), -1
}

func (h *GRPCAttachHandler) HandleAttachInput(_ context.Context, input *pb.AttachInput) {
	sessionID := input.GetSessionId()

	h.mu.Lock()
	sess, ok := h.sessions[sessionID]
	h.mu.Unlock()

	if !ok {
		return
	}

	sess.lastInput.Store(time.Now())

	if _, err := sess.stdinPW.Write(input.GetData()); err != nil {
		log.WithField("session_id", sessionID).WithError(err).Debug("Failed to write attach input")
	}
}

func (h *GRPCAttachHandler) HandleAttachDetach(_ context.Context, detach *pb.AttachDetach) {
	sessionID := detach.GetSessionId()

	h.mu.Lock()
	sess, ok := h.sessions[sessionID]
	h.mu.Unlock()

	if !ok {
		return
	}

	log.WithFields(log.Fields{
		"session_id": sessionID,
		"reason":     detach.GetReason(),
	}).Info("Attach detach requested")

	sess.cancel()
	sess.stdinPW.Close()
}

func (h *GRPCAttachHandler) CloseAllSessions(reason string) {
	h.mu.Lock()
	sessions := make([]*attachSession, 0, len(h.sessions))
	for _, s := range h.sessions {
		sessions = append(sessions, s)
	}
	h.mu.Unlock()

	for _, s := range sessions {
		s.cancel()
		s.stdinPW.Close()
	}

	log.WithField("reason", reason).WithField("count", len(sessions)).Info("Closed all attach sessions")
}

func (h *GRPCAttachHandler) sendAttachClosed(sessionID, reason string, exitCode int32) {
	h.sender.Send(&pb.DaemonMessage{
		Payload: &pb.DaemonMessage_AttachClosed{
			AttachClosed: &pb.AttachClosed{
				SessionId: sessionID,
				Reason:    reason,
				ExitCode:  exitCode,
			},
		},
	})
}

// attachOutputWriter adapts ResponseSender to io.Writer with buffering.
type attachOutputWriter struct {
	sessionID string
	sender    ResponseSender
	buf       []byte
	mu        sync.Mutex
	timer     *time.Timer
	closed    chan struct{}
}

func newAttachOutputWriter(sessionID string, sender ResponseSender) *attachOutputWriter {
	return &attachOutputWriter{
		sessionID: sessionID,
		sender:    sender,
		buf:       make([]byte, 0, outputBufSize),
		closed:    make(chan struct{}),
	}
}

func (w *attachOutputWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	select {
	case <-w.closed:
		return 0, io.ErrClosedPipe
	default:
	}

	w.buf = append(w.buf, p...)

	if len(w.buf) >= outputBufSize {
		w.flushLocked()
		return len(p), nil
	}

	if w.timer == nil {
		w.timer = time.AfterFunc(outputFlushInterval, func() {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.flushLocked()
		})
	}

	return len(p), nil
}

func (w *attachOutputWriter) flushLocked() {
	if len(w.buf) == 0 {
		return
	}

	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}

	data := make([]byte, len(w.buf))
	copy(data, w.buf)
	w.buf = w.buf[:0]

	w.sender.Send(&pb.DaemonMessage{
		Payload: &pb.DaemonMessage_AttachOutput{
			AttachOutput: &pb.AttachOutput{
				SessionId: w.sessionID,
				Data:      data,
			},
		},
	})
}

func (w *attachOutputWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	select {
	case <-w.closed:
		return nil
	default:
	}

	w.flushLocked()
	close(w.closed)

	return nil
}
