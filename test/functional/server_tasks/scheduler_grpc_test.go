package server_tasks_test

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	serversscheduler "github.com/gameap/daemon/internal/app/servers_scheduler"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// End-to-end smoke test: drive the scheduler via its gRPC-facing public API
// only (ApplySnapshot / ApplyDelta / CancelExecution + the ServerTaskSender
// the GatewayClient implements). Verifies the messaging contract the daemon
// emits in response to inputs the panel would push over the bidi stream.
func TestServerTaskScheduler_PublicAPI_SnapshotToFinished(t *testing.T) {
	server := newServer(42)
	repo := &serverRepoStub{servers: map[int]*domain.Server{server.ID(): server}}
	sender := newSender()
	loader := &loaderStub{cmd: &cmdStub{output: []byte("done")}}

	scheduler := serversscheduler.NewScheduler(nil, loader, repo, sender)

	now := time.Now()
	scheduler.ApplySnapshot(&pb.ServerTaskSnapshot{
		Tasks: []*pb.ServerTask{{
			Id:            1,
			ServerId:      42,
			Version:       1,
			Command:       pb.ServerTaskCommand_SERVER_TASK_COMMAND_RESTART,
			ExecuteDate:   timestamppb.New(now.Add(-30 * time.Second)),
			RepeatPeriod:  durationpb.New(time.Hour),
			Enabled:       true,
			OverlapPolicy: pb.ServerTaskOverlapPolicy_SERVER_TASK_OVERLAP_POLICY_SKIP,
			CatchupPolicy: pb.ServerTaskCatchupPolicy_SERVER_TASK_CATCHUP_POLICY_SKIP,
		}},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	go func() { _ = scheduler.Run(ctx) }()

	finished := waitForFinishedExternal(t, sender, 1, 15*time.Second)
	require.Len(t, finished, 1)
	assert.Equal(t, pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_SUCCESS, finished[0].Status)
	assert.Equal(t, []byte("done"), finished[0].OutputInline)

	started := sender.snapshot().started
	require.Len(t, started, 1)
	assert.Equal(t, started[0].ExecutionId, finished[0].ExecutionId)
}

func TestServerTaskScheduler_PublicAPI_DeleteThenNoFire(t *testing.T) {
	server := newServer(42)
	repo := &serverRepoStub{servers: map[int]*domain.Server{server.ID(): server}}
	sender := newSender()
	loader := &loaderStub{cmd: &cmdStub{}}

	scheduler := serversscheduler.NewScheduler(nil, loader, repo, sender)

	now := time.Now()
	scheduler.ApplySnapshot(&pb.ServerTaskSnapshot{
		Tasks: []*pb.ServerTask{{
			Id:            2,
			ServerId:      42,
			Version:       1,
			Command:       pb.ServerTaskCommand_SERVER_TASK_COMMAND_RESTART,
			ExecuteDate:   timestamppb.New(now.Add(time.Hour)),
			RepeatPeriod:  durationpb.New(time.Hour),
			Enabled:       true,
			OverlapPolicy: pb.ServerTaskOverlapPolicy_SERVER_TASK_OVERLAP_POLICY_SKIP,
			CatchupPolicy: pb.ServerTaskCatchupPolicy_SERVER_TASK_CATCHUP_POLICY_SKIP,
		}},
	})

	scheduler.ApplyDelta(&pb.ServerTaskDelta{
		Kind: &pb.ServerTaskDelta_Deleted{
			Deleted: &pb.ServerTaskDeleted{Id: 2, Version: 2},
		},
	})

	assert.Empty(t, sender.snapshot().started, "deleted task must not fire")
	assert.Equal(t, 0, loader.Calls())
}

// --- minimal test doubles (kept local to this _test package) ---

type sentSnapshot struct {
	started  []*pb.ServerTaskExecutionStarted
	finished []*pb.ServerTaskExecutionFinished
	logs     []*pb.ServerTaskExecutionLog
	resync   []*pb.ServerTaskResyncRequest
}

type sender struct {
	mu  sync.Mutex
	all []*pb.DaemonMessage
}

func newSender() *sender { return &sender{} }

func (s *sender) Send(m *pb.DaemonMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.all = append(s.all, m)
}

func (s *sender) snapshot() sentSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out sentSnapshot
	for _, m := range s.all {
		switch {
		case m.GetServerTaskExecutionStarted() != nil:
			out.started = append(out.started, m.GetServerTaskExecutionStarted())
		case m.GetServerTaskExecutionFinished() != nil:
			out.finished = append(out.finished, m.GetServerTaskExecutionFinished())
		case m.GetServerTaskExecutionLog() != nil:
			out.logs = append(out.logs, m.GetServerTaskExecutionLog())
		case m.GetServerTaskResyncRequest() != nil:
			out.resync = append(out.resync, m.GetServerTaskResyncRequest())
		}
	}
	return out
}

type serverRepoStub struct {
	mu      sync.Mutex
	servers map[int]*domain.Server
}

func (r *serverRepoStub) FindByID(_ context.Context, id int) (*domain.Server, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.servers[id], nil
}

func (r *serverRepoStub) Save(_ context.Context, _ *domain.Server) error { return nil }

func (r *serverRepoStub) IDs(_ context.Context) ([]int, error) { return nil, nil }

type cmdStub struct {
	output []byte
}

func (c *cmdStub) Execute(_ context.Context, _ *domain.Server) error { return nil }
func (c *cmdStub) Result() int                                       { return gameservercommands.SuccessResult }
func (c *cmdStub) IsComplete() bool                                  { return true }
func (c *cmdStub) ReadOutput() []byte                                { return c.output }

type loaderStub struct {
	mu    sync.Mutex
	cmd   *cmdStub
	calls int
}

func (l *loaderStub) LoadServerCommand(_ domain.ServerCommand, _ *domain.Server) contracts.GameServerCommand {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	return l.cmd
}

func (l *loaderStub) Calls() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

func newServer(id int) *domain.Server {
	return domain.NewServer(
		id, true, domain.ServerInstalled, false,
		"server-"+strconv.Itoa(id),
		"uuid-"+strconv.Itoa(id),
		"short-"+strconv.Itoa(id),
		domain.Game{}, domain.GameMod{},
		"127.0.0.1", 25565, 25565, 25565,
		"", "/srv/test/"+strconv.Itoa(id),
		"", "", "", "", "",
		false, time.Unix(0, 0),
		map[string]string{}, domain.Settings{},
		time.Unix(0, 0), 0, 0,
	)
}

func waitForFinishedExternal(t *testing.T, s *sender, want int, max time.Duration) []*pb.ServerTaskExecutionFinished {
	t.Helper()
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		fin := s.snapshot().finished
		if len(fin) >= want {
			return fin
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected %d Finished events; got %d", want, len(s.snapshot().finished))
	return nil
}
