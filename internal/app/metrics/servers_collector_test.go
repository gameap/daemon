package metrics

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errFakeBoom = errors.New("boom")

type fakeServerLister struct {
	servers map[int]*domain.Server
}

func (f *fakeServerLister) IDsFromCache() []int {
	ids := make([]int, 0, len(f.servers))
	for id := range f.servers {
		ids = append(ids, id)
	}
	return ids
}

func (f *fakeServerLister) FindByIDFromCache(id int) (*domain.Server, bool) {
	s, ok := f.servers[id]
	return s, ok
}

// fakeProcessManager satisfies contracts.ProcessManager for tests; only the
// Metrics method is wired up, the rest are no-ops returning sensible zero values.
type fakeProcessManager struct {
	byServerID map[int][]domain.Metric
	errByID    map[int]error
}

func (f *fakeProcessManager) Install(_ context.Context, _ *domain.Server, _ io.Writer) (domain.Result, error) {
	return domain.SuccessResult, nil
}
func (f *fakeProcessManager) Uninstall(_ context.Context, _ *domain.Server, _ io.Writer) (domain.Result, error) {
	return domain.SuccessResult, nil
}
func (f *fakeProcessManager) Start(_ context.Context, _ *domain.Server, _ io.Writer) (domain.Result, error) {
	return domain.SuccessResult, nil
}
func (f *fakeProcessManager) Stop(_ context.Context, _ *domain.Server, _ io.Writer) (domain.Result, error) {
	return domain.SuccessResult, nil
}
func (f *fakeProcessManager) Restart(_ context.Context, _ *domain.Server, _ io.Writer) (domain.Result, error) {
	return domain.SuccessResult, nil
}
func (f *fakeProcessManager) Status(_ context.Context, _ *domain.Server, _ io.Writer) (domain.Result, error) {
	return domain.SuccessResult, nil
}
func (f *fakeProcessManager) GetOutput(_ context.Context, _ *domain.Server, _ io.Writer) (domain.Result, error) {
	return domain.SuccessResult, nil
}
func (f *fakeProcessManager) SendInput(_ context.Context, _ string, _ *domain.Server, _ io.Writer) (domain.Result, error) {
	return domain.SuccessResult, nil
}
func (f *fakeProcessManager) Attach(_ context.Context, _ *domain.Server, _ io.Reader, _ io.Writer) error {
	return nil
}
func (f *fakeProcessManager) HasOwnInstallation(_ *domain.Server) bool { return false }

func (f *fakeProcessManager) Metrics(_ context.Context, server *domain.Server) ([]domain.Metric, error) {
	if err, ok := f.errByID[server.ID()]; ok {
		return nil, err
	}
	return f.byServerID[server.ID()], nil
}

func newTestServer(id int, uuid string) *domain.Server {
	return domain.NewServer(
		id,
		true,
		domain.ServerInstalled,
		false,
		"test",
		uuid,
		uuid,
		domain.Game{},
		domain.GameMod{},
		"127.0.0.1",
		27015, 27016, 27020,
		"",
		"/tmp",
		"gameap",
		"start", "stop", "kill", "restart",
		true,
		time.Now(),
		nil,
		nil,
		time.Now(),
		0, 0,
	)
}

func TestServersCollector_AddsServerIDLabel(t *testing.T) {
	now := time.Now()
	srv := newTestServer(7, "uuid-7")

	lister := &fakeServerLister{servers: map[int]*domain.Server{7: srv}}
	pm := &fakeProcessManager{
		byServerID: map[int][]domain.Metric{
			7: {{
				Name:      "gameap_server_cpu_usage_percent",
				Type:      domain.MetricTypeGauge,
				Unit:      domain.MetricUnitPercent,
				Timestamp: now,
				Value:     domain.Float64Value(15),
			}},
		},
	}

	c := NewServersMetricsCollector(lister, pm)
	got, err := c.Collect(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "7", got[0].Labels[labelServerID])
	assert.Equal(t, "uuid-7", got[0].Labels["server_uuid"])
}

func TestServersCollector_SkipsFailingServerButCollectsOthers(t *testing.T) {
	srv1 := newTestServer(1, "uuid-1")
	srv2 := newTestServer(2, "uuid-2")

	lister := &fakeServerLister{servers: map[int]*domain.Server{1: srv1, 2: srv2}}
	pm := &fakeProcessManager{
		byServerID: map[int][]domain.Metric{
			2: {{
				Name:      "gameap_server_cpu_usage_percent",
				Type:      domain.MetricTypeGauge,
				Unit:      domain.MetricUnitPercent,
				Timestamp: time.Now(),
				Value:     domain.Float64Value(20),
			}},
		},
		errByID: map[int]error{
			1: errFakeBoom,
		},
	}

	c := NewServersMetricsCollector(lister, pm)
	got, err := c.Collect(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1, "metrics from server 2 collected even though server 1 failed")
	assert.Equal(t, "2", got[0].Labels[labelServerID])
}

func TestServersCollector_RespectsExistingServerIDLabel(t *testing.T) {
	srv := newTestServer(5, "uuid-5")

	lister := &fakeServerLister{servers: map[int]*domain.Server{5: srv}}
	pm := &fakeProcessManager{
		byServerID: map[int][]domain.Metric{
			5: {{
				Name:      "gameap_server_cpu_usage_percent",
				Type:      domain.MetricTypeGauge,
				Unit:      domain.MetricUnitPercent,
				Labels:    map[string]string{labelServerID: "explicit"},
				Timestamp: time.Now(),
				Value:     domain.Float64Value(15),
			}},
		},
	}

	c := NewServersMetricsCollector(lister, pm)
	got, err := c.Collect(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "explicit", got[0].Labels[labelServerID])
}
