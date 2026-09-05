package serversloop

import (
	"context"
	"runtime/debug"
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	commands "github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	loopDuration = 5 * time.Second

	statusCommandTimeout = 10 * time.Second

	// A start is not a quick call: it can pull a container image, run the
	// configured update-before-start, or block for as long as the game server
	// lives when the process manager does not detach. It therefore runs off the
	// tick, and the budget only exists to keep a wedged start from holding the
	// server's lease forever.
	startCommandTimeout = 1 * time.Hour

	// How often a server is probed, chosen by how long it has been up. A server
	// that just came up is watched closely, one that has been up for a while is
	// polled at the slow rate.
	minProbeInterval = loopDuration
	maxProbeInterval = 30 * time.Second
	stableUptime     = 5 * time.Minute

	// A probe always finishes some time after the tick that started it, so the
	// gap measured at the next tick is a little short of a whole interval.
	// Compared strictly, every threshold would fall just after a tick and the
	// probe would slip to the one after it, halving the real cadence. The
	// tolerance is half a tick, which is far more than a liveness check costs
	// and still well short of the next tick, so no interval is ever doubled and
	// none is ever probed twice.
	probeIntervalTolerance = loopDuration / 2

	// Restart backoff. Delays grow 5s, 15s, 45s, ... up to the cap, and reset
	// once the server has stayed up for settleUptime.
	initialRestartDelay = 5 * time.Second
	maxRestartDelay     = 10 * time.Minute
	restartDelayFactor  = 3
	settleUptime        = 1 * time.Minute
)

type ServerStatusReporter interface {
	Report(server *domain.Server)
}

type probeResult int

const (
	// probeUndetermined means the liveness check could not be evaluated: the
	// command errored, timed out or returned a result that is neither success
	// nor failure. It must never be treated as a stopped server, because the
	// loop restarts servers it believes to be down.
	probeUndetermined probeResult = iota
	probeRunning
	probeStopped
)

type ServersLoop struct {
	cfg                  *config.Config
	serverRepo           domain.ServerRepository
	serverCommandFactory *commands.ServerCommandFactory
	statusReporter       ServerStatusReporter

	nowFn func() time.Time

	// starts is held by the asynchronous start goroutines so shutdown can wait
	// for them instead of leaving them behind.
	starts sync.WaitGroup
}

func NewServersLoop(
	serverRepo domain.ServerRepository,
	serverCommandFactory *commands.ServerCommandFactory,
	cfg *config.Config,
) *ServersLoop {
	return &ServersLoop{
		cfg:                  cfg,
		serverRepo:           serverRepo,
		serverCommandFactory: serverCommandFactory,
		nowFn:                time.Now,
	}
}

func (l *ServersLoop) SetStatusReporter(reporter ServerStatusReporter) {
	l.statusReporter = reporter
}

func (l *ServersLoop) Run(ctx context.Context) error {
	return l.loop(ctx)
}

func (l *ServersLoop) now() time.Time {
	return l.nowFn()
}

func (l *ServersLoop) loop(ctx context.Context) error {
	ticker := time.NewTicker(loopDuration)
	defer ticker.Stop()
	defer l.starts.Wait()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			l.tick(ctx)
		}
	}
}

func (l *ServersLoop) tick(ctx context.Context) {
	ids, err := l.serverRepo.IDs(ctx)
	if err != nil {
		log.Error(err)
		return
	}

	if len(ids) == 0 {
		log.Debug("No servers found to check status")

		return
	}

	for i := range ids {
		// Once the daemon is shutting down every probe is killed on the spot and
		// reports nothing useful, so the remaining servers are left alone rather
		// than marked down and restarted against a dead context.
		if ctx.Err() != nil {
			return
		}

		l.processServer(ctx, ids[i])
	}
}

// processServer runs one server through the tick. A panic here would otherwise
// take the whole daemon down with it, since the loop is one of the top-level
// services: a process manager that trips over an unexpected response must cost
// one server one tick, not every server its supervision.
func (l *ServersLoop) processServer(ctx context.Context, id int) {
	ctx = logger.WithLogger(ctx, logger.WithField(ctx, "gameServerID", id))

	defer func() {
		if r := recover(); r != nil {
			logger.Logger(ctx).
				WithField("panic", r).
				WithField("stack", string(debug.Stack())).
				Error("Panic while checking game server")
		}
	}()

	server, err := l.serverRepo.FindByID(ctx, id)
	if err != nil {
		logger.Error(ctx, err)
		return
	}
	if server == nil {
		logger.Debug(ctx, "Server disappeared from the cache before it could be checked")
		return
	}

	if server.InstallationStatus() != domain.ServerInstalled {
		return
	}

	if !l.dueForProbe(server) {
		return
	}

	result, err := l.checkStatus(ctx, server)
	if err != nil {
		logger.Error(ctx, err)
		return
	}

	if result == probeRunning {
		l.noticeRunning(server)
		return
	}

	l.startIfNeeded(ctx, server)
}

// dueForProbe decides whether it is time to run the liveness check again.
//
// The cadence follows how long the server has been continuously up, not when it
// last ran a task: a server nobody touches is exactly the one whose crash has to
// be noticed. A task that finished recently still forces a probe, because the
// server's state has just changed.
func (l *ServersLoop) dueForProbe(server *domain.Server) bool {
	now := l.now()

	elapsed := now.Sub(server.LastStatusCheck())
	if elapsed >= maxProbeInterval-probeIntervalTolerance {
		return true
	}
	if elapsed < minProbeInterval-probeIntervalTolerance {
		return false
	}

	if now.Sub(server.LastTaskCompletedAt()) <= settleUptime {
		return true
	}

	if !server.IsActive() {
		// A stopped server that the loop is allowed to bring back is watched at
		// the fast rate; one it will not touch only needs its status reported.
		return server.CanAutoStart()
	}

	runningSince := server.RunningSince()
	if runningSince.IsZero() {
		// The server is flagged running, but that flag came from the panel and
		// not from a check of our own — right after a reconnect, say. There is no
		// uptime to slow the cadence down with yet.
		return true
	}

	return now.Sub(runningSince) < stableUptime
}

func (l *ServersLoop) checkStatus(ctx context.Context, server *domain.Server) (probeResult, error) {
	statusCmd := l.serverCommandFactory.LoadServerCommand(domain.Status, server)
	if statusCmd == nil {
		return probeUndetermined, errors.New("status command is not implemented for this server")
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, statusCommandTimeout)
	defer cancel()

	err := statusCmd.Execute(ctxWithTimeout, server)
	if err != nil {
		return probeUndetermined, errors.WithMessage(err, "failed to execute status command")
	}

	var running bool

	switch statusCmd.Result() {
	case commands.SuccessResult:
		running = true
	case commands.ErrorResult:
		running = false
	default:
		return probeUndetermined, errors.Errorf(
			"status command returned an indeterminate result %d", statusCmd.Result(),
		)
	}

	server.SetStatusAt(l.now(), running)

	if err := l.serverRepo.Save(ctx, server); err != nil {
		logger.Error(ctx, errors.WithMessage(err, "failed to save game server"))
	}

	if l.statusReporter != nil {
		l.statusReporter.Report(server)
	}

	if running {
		return probeRunning, nil
	}

	return probeStopped, nil
}

// noticeRunning clears the restart backoff once the server has held on long
// enough to count as recovered. Resetting on the start itself would not work:
// tmux reports a session created even when the command inside it dies a moment
// later, and systemd reports a unit started while it is still activating.
func (l *ServersLoop) noticeRunning(server *domain.Server) {
	if server.StartAttempts() == 0 {
		return
	}

	runningSince := server.RunningSince()
	if runningSince.IsZero() || l.now().Sub(runningSince) < settleUptime {
		return
	}

	server.ResetStartAttempts()
}

func (l *ServersLoop) startIfNeeded(ctx context.Context, server *domain.Server) {
	if !server.CanAutoStart() {
		return
	}

	now := l.now()
	if now.Before(server.NextStartAllowedAt()) {
		return
	}

	release, ok := l.serverCommandFactory.TryLockServer(server.ID())
	if !ok {
		logger.Debug(ctx, "Skipping automatic start, another command is running for this server")
		return
	}

	attempt := server.StartAttempts() + 1
	server.NoticeStartAttempt(now, restartDelay(attempt))

	logger.Logger(ctx).WithField("attempt", attempt).Info("Starting game server automatically")

	// The start runs off the tick so that a slow one — a container image pull, an
	// update before start, or a process manager whose start call only returns
	// when the game server exits — does not hold up the status checks of every
	// other server on the node. Whether it worked is decided by the next probe,
	// which is better evidence than the start command's exit code anyway.
	l.starts.Add(1)

	go func() {
		defer l.starts.Done()
		defer release()

		l.startServer(ctx, server, attempt)
	}()
}

func (l *ServersLoop) startServer(ctx context.Context, server *domain.Server, attempt int) {
	defer func() {
		if r := recover(); r != nil {
			logger.Logger(ctx).
				WithField("panic", r).
				WithField("stack", string(debug.Stack())).
				Error("Panic while starting game server")
		}
	}()

	startCMD := l.serverCommandFactory.LoadServerCommand(domain.Start, server)
	if startCMD == nil {
		logger.Error(ctx, errors.New("start command is not implemented for this server"))
		return
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, startCommandTimeout)
	defer cancel()

	err := startCMD.Execute(ctxWithTimeout, server)
	if err != nil {
		logger.Logger(ctx).WithError(err).WithField("attempt", attempt).
			Error("Automatic start failed")
		return
	}

	if startCMD.Result() != commands.SuccessResult {
		logger.Logger(ctx).
			WithField("attempt", attempt).
			WithField("result", startCMD.Result()).
			WithField("output", string(startCMD.ReadOutput())).
			Error("Automatic start was rejected by the process manager")
	}
}

// restartDelay returns how long to wait before the given attempt may be
// followed by another one. Without it a server that dies immediately after every
// start is restarted every five seconds forever, which for the container process
// managers means destroying and recreating the container that often.
//
// The delay is capped rather than the number of attempts: giving up entirely
// would leave the server down with nothing but a daemon log to say why, and the
// panel is never told.
func restartDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	delay := initialRestartDelay
	for i := 1; i < attempt; i++ {
		delay *= restartDelayFactor
		if delay >= maxRestartDelay {
			return maxRestartDelay
		}
	}

	return delay
}
