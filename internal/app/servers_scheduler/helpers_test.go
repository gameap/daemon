package serversscheduler

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	pb "github.com/gameap/gameap/pkg/proto"
)

type fakeSender struct {
	mu   sync.Mutex
	msgs []*pb.DaemonMessage
}

func newFakeSender() *fakeSender {
	return &fakeSender{}
}

func (s *fakeSender) Send(msg *pb.DaemonMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.msgs = append(s.msgs, msg)
}

func (s *fakeSender) Messages() []*pb.DaemonMessage {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]*pb.DaemonMessage, len(s.msgs))
	copy(out, s.msgs)
	return out
}

func (s *fakeSender) Started() []*pb.ServerTaskExecutionStarted {
	out := []*pb.ServerTaskExecutionStarted{}
	for _, m := range s.Messages() {
		if v := m.GetServerTaskExecutionStarted(); v != nil {
			out = append(out, v)
		}
	}
	return out
}

func (s *fakeSender) Finished() []*pb.ServerTaskExecutionFinished {
	out := []*pb.ServerTaskExecutionFinished{}
	for _, m := range s.Messages() {
		if v := m.GetServerTaskExecutionFinished(); v != nil {
			out = append(out, v)
		}
	}
	return out
}

func (s *fakeSender) Logs() []*pb.ServerTaskExecutionLog {
	out := []*pb.ServerTaskExecutionLog{}
	for _, m := range s.Messages() {
		if v := m.GetServerTaskExecutionLog(); v != nil {
			out = append(out, v)
		}
	}
	return out
}

func (s *fakeSender) ResyncRequests() []*pb.ServerTaskResyncRequest {
	out := []*pb.ServerTaskResyncRequest{}
	for _, m := range s.Messages() {
		if v := m.GetServerTaskResyncRequest(); v != nil {
			out = append(out, v)
		}
	}
	return out
}

type fakeServerRepo struct {
	mu      sync.Mutex
	servers map[int]*domain.Server
}

func newFakeServerRepo(servers ...*domain.Server) *fakeServerRepo {
	r := &fakeServerRepo{servers: make(map[int]*domain.Server)}
	for _, s := range servers {
		r.servers[s.ID()] = s
	}
	return r
}

func (r *fakeServerRepo) FindByID(_ context.Context, id int) (*domain.Server, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.servers[id], nil
}

func (r *fakeServerRepo) Save(_ context.Context, _ *domain.Server) error {
	return nil
}

func (r *fakeServerRepo) IDs(_ context.Context) ([]int, error) {
	return nil, nil
}

// fakeCommand is a deterministic GameServerCommand stub.
type fakeCommand struct {
	output    []byte
	result    int
	execError error
	// block is non-nil when the command should pause until the channel is
	// closed (used by overlap/cancel tests to keep an execution in flight).
	block <-chan struct{}
}

func (c *fakeCommand) Execute(ctx context.Context, _ *domain.Server) error {
	if c.block != nil {
		select {
		case <-c.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return c.execError
}

func (c *fakeCommand) Result() int {
	if c.result == 0 {
		return gameservercommands.SuccessResult
	}
	return c.result
}

func (c *fakeCommand) IsComplete() bool {
	return true
}

func (c *fakeCommand) ReadOutput() []byte {
	return c.output
}

// fakeLoader returns the same fakeCommand for every load, recording call count.
type fakeLoader struct {
	mu    sync.Mutex
	cmd   *fakeCommand
	calls int
}

func (l *fakeLoader) LoadServerCommand(_ domain.ServerCommand, _ *domain.Server) contracts.GameServerCommand {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.calls++
	return l.cmd
}

func (l *fakeLoader) Calls() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.calls
}

func newServerForTask(id int) *domain.Server {
	return domain.NewServer(
		id,
		true,
		domain.ServerInstalled,
		false,
		"server-"+strconv.Itoa(id),
		"uuid-"+strconv.Itoa(id),
		"short-"+strconv.Itoa(id),
		domain.Game{},
		domain.GameMod{},
		"127.0.0.1",
		25565, 25565, 25565,
		"",
		"/srv/test/"+strconv.Itoa(id),
		"",
		"", "", "", "",
		false,
		time.Unix(0, 0),
		map[string]string{},
		domain.Settings{},
		time.Unix(0, 0),
		0, 0,
	)
}

func newTestScheduler(loader CommandLoader, repo domain.ServerRepository, sender ServerTaskSender) *Scheduler {
	return NewScheduler(nil, loader, repo, sender)
}

func freezeTime(s *Scheduler, now time.Time) {
	s.nowFn = func() time.Time { return now }
}
