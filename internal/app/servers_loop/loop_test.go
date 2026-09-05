package serversloop

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/test/mocks"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProcessManager stands in for a real process manager so the tests exercise
// the actual command factory, including the intent bookkeeping that start and
// stop commands perform on the server.
type fakeProcessManager struct {
	mu sync.Mutex

	running bool

	statusResult domain.Result
	statusErr    error

	startResult domain.Result
	startErr    error

	startCalls  int
	statusCalls int

	// startPanics makes the start command panic, to prove one broken process
	// manager cannot take the daemon down with it.
	startPanics bool

	// startBlocked holds the start command until the test releases it, which is
	// how the tests observe a start that is still in flight.
	startBlocked chan struct{}
}

func newFakeProcessManager() *fakeProcessManager {
	return &fakeProcessManager{
		statusResult: domain.ErrorResult,
		startResult:  domain.SuccessResult,
	}
}

func (pm *fakeProcessManager) Status(
	_ context.Context, _ *domain.Server, _ io.Writer,
) (domain.Result, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.statusCalls++

	if pm.statusErr != nil {
		return domain.UnknownResult, pm.statusErr
	}
	if pm.running {
		return domain.SuccessResult, nil
	}

	return pm.statusResult, nil
}

func (pm *fakeProcessManager) Start(
	_ context.Context, _ *domain.Server, _ io.Writer,
) (domain.Result, error) {
	if pm.startPanics {
		panic("process manager exploded")
	}

	if pm.startBlocked != nil {
		<-pm.startBlocked
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.startCalls++

	return pm.startResult, pm.startErr
}

func (pm *fakeProcessManager) StartCalls() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	return pm.startCalls
}

func (pm *fakeProcessManager) StatusCalls() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	return pm.statusCalls
}

func (pm *fakeProcessManager) setRunning(running bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.running = running
}

func (pm *fakeProcessManager) Install(
	_ context.Context, _ *domain.Server, _ io.Writer,
) (domain.Result, error) {
	return domain.SuccessResult, nil
}

func (pm *fakeProcessManager) Uninstall(
	_ context.Context, _ *domain.Server, _ io.Writer,
) (domain.Result, error) {
	return domain.SuccessResult, nil
}

func (pm *fakeProcessManager) Stop(
	_ context.Context, _ *domain.Server, _ io.Writer,
) (domain.Result, error) {
	return domain.SuccessResult, nil
}

func (pm *fakeProcessManager) Restart(
	_ context.Context, _ *domain.Server, _ io.Writer,
) (domain.Result, error) {
	return domain.SuccessResult, nil
}

func (pm *fakeProcessManager) GetOutput(
	_ context.Context, _ *domain.Server, _ io.Writer,
) (domain.Result, error) {
	return domain.SuccessResult, nil
}

func (pm *fakeProcessManager) SendInput(
	_ context.Context, _ string, _ *domain.Server, _ io.Writer,
) (domain.Result, error) {
	return domain.SuccessResult, nil
}

func (pm *fakeProcessManager) Attach(
	_ context.Context, _ *domain.Server, _ io.Reader, _ io.Writer,
) error {
	return nil
}

func (pm *fakeProcessManager) Metrics(_ context.Context, _ *domain.Server) ([]domain.Metric, error) {
	return nil, nil
}

func (pm *fakeProcessManager) HasOwnInstallation(_ *domain.Server) bool {
	return false
}

type recordingReporter struct {
	mu       sync.Mutex
	reported []bool
}

func (r *recordingReporter) Report(server *domain.Server) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.reported = append(r.reported, server.IsActive())
}

func (r *recordingReporter) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.reported)
}

const testServerID = 1337

type serverOption func(*serverParams)

type serverParams struct {
	installStatus domain.InstallationStatus
	enabled       bool
	blocked       bool
	settings      domain.Settings
	lastCheck     time.Time
}

func withSettings(settings domain.Settings) serverOption {
	return func(p *serverParams) { p.settings = settings }
}

func withBlocked() serverOption {
	return func(p *serverParams) { p.blocked = true }
}

func withDisabled() serverOption {
	return func(p *serverParams) { p.enabled = false }
}

func withInstallationStatus(status domain.InstallationStatus) serverOption {
	return func(p *serverParams) { p.installStatus = status }
}

func givenServer(opts ...serverOption) *domain.Server {
	params := serverParams{
		installStatus: domain.ServerInstalled,
		enabled:       true,
		settings:      domain.Settings{"autostart": "1"},
		lastCheck:     time.Unix(0, 0),
	}

	for _, opt := range opts {
		opt(&params)
	}

	return domain.NewServer(
		testServerID,
		params.enabled,
		params.installStatus,
		params.blocked,
		"test server",
		"759b875e-d910-11eb-aff7-d796d7fcf7ef",
		"759b875e",
		domain.Game{StartCode: "cstrike"},
		domain.GameMod{Name: "public"},
		"1.3.3.7",
		1337,
		1338,
		1339,
		"rcon",
		"server",
		"gameap-user",
		"./start.sh",
		"./stop.sh",
		"",
		"",
		false,
		params.lastCheck,
		map[string]string{},
		params.settings,
		time.Now(),
		0,
		0,
	)
}

type loopFixture struct {
	loop     *ServersLoop
	pm       *fakeProcessManager
	repo     *mocks.ServerRepository
	reporter *recordingReporter
	factory  *gameservercommands.ServerCommandFactory

	now time.Time
}

func newLoopFixture(t *testing.T, server *domain.Server) *loopFixture {
	t.Helper()

	cfg := &config.Config{WorkPath: t.TempDir()}
	pm := newFakeProcessManager()
	repo := mocks.NewServerRepository()
	repo.Set([]*domain.Server{server})

	factory := gameservercommands.NewFactory(cfg, repo, nil, pm)

	f := &loopFixture{
		loop:     NewServersLoop(repo, factory, cfg),
		pm:       pm,
		repo:     repo,
		reporter: &recordingReporter{},
		factory:  factory,
		now:      time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
	}

	f.loop.SetStatusReporter(f.reporter)
	f.loop.nowFn = func() time.Time { return f.now }

	return f
}

// tick runs one iteration and waits for any start it launched, since starts run
// off the tick on their own goroutine.
func (f *loopFixture) tick(t *testing.T) {
	t.Helper()

	f.loop.tick(context.Background())
	f.loop.starts.Wait()
}

func (f *loopFixture) advance(d time.Duration) {
	f.now = f.now.Add(d)
}

func TestTick_CrashedServerWithAutostartIsStarted(t *testing.T) {
	server := givenServer()
	f := newLoopFixture(t, server)

	f.tick(t)

	assert.Equal(t, 1, f.pm.StartCalls())
	assert.False(t, server.IsActive())
	assert.Equal(t, 1, f.reporter.count())
}

func TestTick_RunningServerIsNotStarted(t *testing.T) {
	server := givenServer()
	f := newLoopFixture(t, server)
	f.pm.setRunning(true)

	f.tick(t)

	assert.Equal(t, 0, f.pm.StartCalls())
	assert.True(t, server.IsActive())
}

func TestTick_AutostartDisabledServerIsNotStarted(t *testing.T) {
	server := givenServer(withSettings(domain.Settings{"autostart": "0"}))
	f := newLoopFixture(t, server)

	f.tick(t)

	assert.Equal(t, 0, f.pm.StartCalls())
	assert.False(t, server.IsActive())
	assert.Equal(t, 1, f.reporter.count(), "status is still reported for a server the loop will not start")
}

// A server stopped on purpose carries autostart_current=0, which must win over
// the persistent autostart setting.
func TestTick_DeliberatelyStoppedServerIsNotStarted(t *testing.T) {
	server := givenServer(withSettings(domain.Settings{
		"autostart":         "1",
		"autostart_current": "0",
	}))
	f := newLoopFixture(t, server)

	f.tick(t)

	assert.Equal(t, 0, f.pm.StartCalls())
}

func TestTick_BlockedServerIsNotStarted(t *testing.T) {
	server := givenServer(withBlocked())
	f := newLoopFixture(t, server)

	f.tick(t)

	assert.Equal(t, 0, f.pm.StartCalls())
}

func TestTick_DisabledServerIsNotStarted(t *testing.T) {
	server := givenServer(withDisabled())
	f := newLoopFixture(t, server)

	f.tick(t)

	assert.Equal(t, 0, f.pm.StartCalls())
}

func TestTick_NotInstalledServerIsSkippedEntirely(t *testing.T) {
	server := givenServer(withInstallationStatus(domain.ServerNotInstalled))
	f := newLoopFixture(t, server)

	f.tick(t)

	assert.Equal(t, 0, f.pm.StatusCalls())
	assert.Equal(t, 0, f.pm.StartCalls())
}

// A status check that could not be evaluated is not evidence that the server is
// down, so it must neither restart the server nor overwrite its known state.
func TestTick_UndeterminedStatusNeitherReportsNorStarts(t *testing.T) {
	server := givenServer()
	server.SetStatusAt(time.Unix(0, 0), true)

	f := newLoopFixture(t, server)
	f.pm.statusErr = errors.New("probe timed out")

	f.tick(t)

	assert.Equal(t, 0, f.pm.StartCalls())
	assert.True(t, server.IsActive(), "the last known state must survive an unevaluated probe")
	assert.Equal(t, 0, f.reporter.count())
}

func TestTick_IndeterminateResultDoesNotStartServer(t *testing.T) {
	server := givenServer()
	f := newLoopFixture(t, server)
	f.pm.statusResult = domain.UnknownResult

	f.tick(t)

	assert.Equal(t, 0, f.pm.StartCalls())
	assert.Equal(t, 0, f.reporter.count())
}

// A server that dies straight after every start must not be restarted every
// tick: the delay between attempts grows.
func TestTick_RepeatedCrashesBackOff(t *testing.T) {
	server := givenServer()
	f := newLoopFixture(t, server)

	f.tick(t)
	require.Equal(t, 1, f.pm.StartCalls())

	// The first delay is one loop interval, so the very next tick is still too
	// early for a second attempt.
	f.advance(initialRestartDelay - time.Second)
	f.tick(t)
	assert.Equal(t, 1, f.pm.StartCalls(), "a second attempt must wait for the backoff")

	f.advance(time.Second)
	f.tick(t)
	require.Equal(t, 2, f.pm.StartCalls())

	// The second delay is three times the first, so waiting one delay again is
	// not enough.
	f.advance(initialRestartDelay)
	f.tick(t)
	assert.Equal(t, 2, f.pm.StartCalls())

	f.advance(initialRestartDelay * (restartDelayFactor - 1))
	f.tick(t)
	assert.Equal(t, 3, f.pm.StartCalls())
}

func TestRestartDelay_GrowsAndIsCapped(t *testing.T) {
	assert.Equal(t, initialRestartDelay, restartDelay(1))
	assert.Equal(t, initialRestartDelay*restartDelayFactor, restartDelay(2))
	assert.Equal(t, initialRestartDelay*restartDelayFactor*restartDelayFactor, restartDelay(3))
	assert.Equal(t, maxRestartDelay, restartDelay(100))
	assert.Equal(t, initialRestartDelay, restartDelay(0), "an out of range attempt falls back to the first delay")
}

// Once the server has held on for the settle period the backoff is cleared, so
// the next crash is handled at full speed again.
func TestTick_BackoffResetsAfterServerSettles(t *testing.T) {
	server := givenServer()
	f := newLoopFixture(t, server)

	f.tick(t)
	require.Equal(t, 1, server.StartAttempts())

	f.pm.setRunning(true)
	f.advance(maxProbeInterval)
	f.tick(t)
	assert.Equal(t, 1, server.StartAttempts(), "not settled yet")

	f.advance(settleUptime)
	f.tick(t)
	assert.Equal(t, 0, server.StartAttempts())
	assert.True(t, server.NextStartAllowedAt().IsZero())
}

// A server that has been up for a long time is polled at the slow rate; one that
// just came up is watched closely.
func TestDueForProbe_FollowsUptime(t *testing.T) {
	server := givenServer()
	f := newLoopFixture(t, server)

	server.SetStatusAt(f.now, true)

	f.advance(minProbeInterval)
	assert.True(t, f.loop.dueForProbe(server), "a server that just came up is watched closely")

	f.advance(stableUptime)
	server.SetStatusAt(f.now, true)
	f.advance(minProbeInterval)
	assert.False(t, f.loop.dueForProbe(server), "a stable server is not probed every tick")

	f.advance(maxProbeInterval)
	assert.True(t, f.loop.dueForProbe(server), "a stable server is still probed within the slow interval")
}

func TestDueForProbe_StoppedServerAwaitingRestartIsWatchedClosely(t *testing.T) {
	server := givenServer()
	f := newLoopFixture(t, server)

	server.SetStatusAt(f.now, false)
	server.NoticeTaskCompleted()
	f.advance(settleUptime * 2)
	server.SetStatusAt(f.now, false)

	f.advance(minProbeInterval)

	assert.True(t, f.loop.dueForProbe(server))
}

func TestDueForProbe_StoppedServerWithoutAutostartIsPolledSlowly(t *testing.T) {
	server := givenServer(withSettings(domain.Settings{"autostart": "0"}))
	f := newLoopFixture(t, server)

	f.advance(settleUptime * 2)
	server.SetStatusAt(f.now, false)

	f.advance(minProbeInterval)

	assert.False(t, f.loop.dueForProbe(server))
}

// The loop shares the game server with the task manager and the task scheduler,
// so it must not start a server that one of them is already working on.
func TestTick_DoesNotStartWhileAnotherCommandHoldsTheServer(t *testing.T) {
	server := givenServer()
	f := newLoopFixture(t, server)

	release, ok := f.factory.TryLockServer(server.ID())
	require.True(t, ok)
	defer release()

	f.tick(t)

	assert.Equal(t, 0, f.pm.StartCalls())
	assert.Equal(t, 0, server.StartAttempts(), "a skipped start is not an attempt")
}

func TestTick_SecondStartIsNotLaunchedWhileTheFirstIsStillRunning(t *testing.T) {
	server := givenServer()
	f := newLoopFixture(t, server)
	f.pm.startBlocked = make(chan struct{})

	f.loop.tick(context.Background())

	f.advance(maxRestartDelay * 2)
	f.loop.tick(context.Background())

	close(f.pm.startBlocked)
	f.loop.starts.Wait()

	assert.Equal(t, 1, f.pm.StartCalls())
}

func TestTick_PanicInProcessManagerDoesNotEscape(t *testing.T) {
	server := givenServer()
	f := newLoopFixture(t, server)
	f.pm.startPanics = true

	assert.NotPanics(t, func() {
		f.tick(t)
	})
}

func TestTick_MissingServerIsSkipped(t *testing.T) {
	f := newLoopFixture(t, givenServer())
	f.repo.Clear()

	assert.NotPanics(t, func() {
		f.loop.processServer(context.Background(), testServerID)
	})
}

func TestTick_StopsWhenContextIsDone(t *testing.T) {
	server := givenServer()
	f := newLoopFixture(t, server)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	f.loop.tick(ctx)
	f.loop.starts.Wait()

	assert.Equal(t, 0, f.pm.StatusCalls())
	assert.Equal(t, 0, f.pm.StartCalls())
}
