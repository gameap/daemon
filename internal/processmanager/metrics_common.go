package processmanager

import (
	"time"

	"github.com/gameap/daemon/internal/app/domain"
)

// Prometheus-style names: snake_case, gameap_<scope>_<subsystem>_<unit>,
// _total suffix on counters, receive/transmit for network direction,
// _up for liveness (1 = process running, 0 = not).
const (
	metricServerUp                        = "gameap_server_up"
	metricServerCPUUsagePercent           = "gameap_server_cpu_usage_percent"
	metricServerMemoryUsageBytes          = "gameap_server_memory_usage_bytes"
	metricServerMemoryLimitBytes          = "gameap_server_memory_limit_bytes"
	metricServerMemoryUsagePercent        = "gameap_server_memory_usage_percent"
	metricServerNetworkReceiveBytesTotal  = "gameap_server_network_receive_bytes_total"
	metricServerNetworkTransmitBytesTotal = "gameap_server_network_transmit_bytes_total"
	metricServerBlockIOReadBytesTotal     = "gameap_server_block_io_read_bytes_total"
	metricServerBlockIOWriteBytesTotal    = "gameap_server_block_io_write_bytes_total"
	metricServerProcessPIDs               = "gameap_server_process_pids"

	metricLabelContainer = "container"
)

// livenessMetric returns a single 0/1 gauge reflecting the cached
// process-active flag for a server. Used by process managers that don't yet
// expose CPU/memory stats so the panel still gets a heartbeat per server.
func livenessMetric(server *domain.Server, ts time.Time) domain.Metric {
	value := uint64(0)
	if server != nil && server.IsActive() {
		value = 1
	}
	return domain.Metric{
		Name:      metricServerUp,
		Type:      domain.MetricTypeGauge,
		Unit:      domain.MetricUnitCount,
		Timestamp: ts,
		Value:     domain.Uint64Value(value),
	}
}
