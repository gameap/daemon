package metrics

import (
	"context"
	"runtime"
	"strings"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/pkg/errors"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	log "github.com/sirupsen/logrus"
)

// Prometheus-style names: snake_case, gameap_<scope>_<subsystem>_<unit>,
// _total suffix on counters, receive/transmit for network direction.
const (
	nodeMetricCPUUsagePercent           = "gameap_node_cpu_usage_percent"
	nodeMetricMemoryUsageBytes          = "gameap_node_memory_usage_bytes"
	nodeMetricMemoryTotalBytes          = "gameap_node_memory_total_bytes"
	nodeMetricMemoryUsagePercent        = "gameap_node_memory_usage_percent"
	nodeMetricSwapUsageBytes            = "gameap_node_swap_usage_bytes"
	nodeMetricSwapTotalBytes            = "gameap_node_swap_total_bytes"
	nodeMetricDiskUsageBytes            = "gameap_node_disk_usage_bytes"
	nodeMetricDiskTotalBytes            = "gameap_node_disk_total_bytes"
	nodeMetricDiskUsagePercent          = "gameap_node_disk_usage_percent"
	nodeMetricNetworkReceiveBytesTotal  = "gameap_node_network_receive_bytes_total"
	nodeMetricNetworkTransmitBytesTotal = "gameap_node_network_transmit_bytes_total"
	nodeMetricLoad1                     = "gameap_node_load1"
	nodeMetricLoad5                     = "gameap_node_load5"
	nodeMetricLoad15                    = "gameap_node_load15"
	nodeMetricUptimeSecondsTotal        = "gameap_node_uptime_seconds_total"

	labelInterface = "interface"
	labelMount     = "mount"
)

// NodeMetricsCollector collects host-level metrics (cpu, mem, swap, disk,
// network, load average, uptime) using gopsutil. Disks are reported per
// cfg.DrivesList and interfaces per cfg.IFList. When a list is empty the
// collector falls back to a sensible default instead of reporting everything:
// the root filesystem plus the work_path drive for disks, and physical
// (non-loopback, non-virtual) interfaces for network.
type NodeMetricsCollector struct {
	cfg *config.Config

	ifFilter    map[string]struct{}
	driveFilter map[string]struct{}
}

func NewNodeMetricsCollector(cfg *config.Config) *NodeMetricsCollector {
	return &NodeMetricsCollector{
		cfg:         cfg,
		ifFilter:    sliceToSet(cfg.IFList),
		driveFilter: sliceToSet(cfg.DrivesList),
	}
}

func sliceToSet(in []string) map[string]struct{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(in))
	for _, v := range in {
		out[v] = struct{}{}
	}
	return out
}

// virtualInterfacePrefixes lists name prefixes of virtual / container / VPN
// interfaces that are skipped by default (when if_list is not configured).
// It is a heuristic; an explicit if_list always overrides it.
var virtualInterfacePrefixes = []string{
	"docker", "veth", "br-", "virbr", "vnet", "vmnet", "tap", "tun",
	"wg", "cni", "flannel", "cali", "kube", "zt", "tailscale", "utun",
	"awdl", "llw", "gif", "stf", "dummy", "ifb",
}

// selectDrive decides whether a partition is reported. With an explicit filter
// (drives_list set) it keeps the partition only when its mountpoint or device
// is listed. Without a filter it defaults to the root filesystem and the drive
// that holds work_path, so unrelated/pseudo mounts are not reported.
func selectDrive(filter map[string]struct{}, mountpoint, device, workMount string) bool {
	if filter != nil {
		if _, ok := filter[mountpoint]; ok {
			return true
		}
		_, ok := filter[device]
		return ok
	}

	if mountpoint == "/" {
		return true
	}
	return workMount != "" && mountpoint == workMount
}

// longestMountPrefix returns the mountpoint from mountpoints that contains path
// and sits closest to it (the longest path-prefix match), or "" when path is
// empty or nothing matches.
func longestMountPrefix(path string, mountpoints []string) string {
	if path == "" {
		return ""
	}

	best := ""
	for _, mp := range mountpoints {
		if mp == "" {
			continue
		}
		if mp == path || strings.HasPrefix(path, strings.TrimRight(mp, "/")+"/") {
			if len(mp) > len(best) {
				best = mp
			}
		}
	}
	return best
}

// selectInterface decides whether a network interface is reported. With an
// explicit filter (if_list set) it keeps only listed interfaces; otherwise it
// keeps physical, non-loopback, non-virtual interfaces.
func selectInterface(filter map[string]struct{}, name string) bool {
	if filter != nil {
		_, ok := filter[name]
		return ok
	}
	return !isVirtualInterface(name)
}

func isVirtualInterface(name string) bool {
	if name == "" || name == "lo" || name == "lo0" {
		return true
	}

	lower := strings.ToLower(name)
	for _, p := range virtualInterfacePrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

// Collect returns one snapshot of node metrics. Errors from individual
// gopsutil calls are logged at debug level and the corresponding metrics
// are simply omitted — partial data is preferable to a hard failure.
func (c *NodeMetricsCollector) Collect(_ context.Context) ([]domain.Metric, error) {
	now := time.Now()
	out := make([]domain.Metric, 0, 32)

	out = append(out, c.collectCPU(now)...)
	out = append(out, c.collectMemory(now)...)
	out = append(out, c.collectSwap(now)...)
	out = append(out, c.collectDisks(now)...)
	out = append(out, c.collectNetwork(now)...)
	out = append(out, c.collectLoad(now)...)
	out = append(out, c.collectUptime(now)...)

	return out, nil
}

func (c *NodeMetricsCollector) collectCPU(now time.Time) []domain.Metric {
	percents, err := cpu.Percent(0, false)
	if err != nil {
		log.WithError(errors.Wrap(err, "cpu.Percent")).Debug("collect cpu metrics failed")
		return nil
	}
	if len(percents) == 0 {
		return nil
	}

	return []domain.Metric{
		{
			Name:      nodeMetricCPUUsagePercent,
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitPercent,
			Timestamp: now,
			Value:     domain.Float64Value(percents[0]),
		},
	}
}

func (c *NodeMetricsCollector) collectMemory(now time.Time) []domain.Metric {
	info, err := mem.VirtualMemory()
	if err != nil {
		log.WithError(errors.Wrap(err, "mem.VirtualMemory")).Debug("collect memory metrics failed")
		return nil
	}

	return []domain.Metric{
		{
			Name:      nodeMetricMemoryTotalBytes,
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitBytes,
			Timestamp: now,
			Value:     domain.Uint64Value(info.Total),
		},
		{
			Name:      nodeMetricMemoryUsageBytes,
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitBytes,
			Timestamp: now,
			Value:     domain.Uint64Value(info.Used),
		},
		{
			Name:      nodeMetricMemoryUsagePercent,
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitPercent,
			Timestamp: now,
			Value:     domain.Float64Value(info.UsedPercent),
		},
	}
}

func (c *NodeMetricsCollector) collectSwap(now time.Time) []domain.Metric {
	info, err := mem.SwapMemory()
	if err != nil {
		log.WithError(errors.Wrap(err, "mem.SwapMemory")).Debug("collect swap metrics failed")
		return nil
	}

	return []domain.Metric{
		{
			Name:      nodeMetricSwapTotalBytes,
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitBytes,
			Timestamp: now,
			Value:     domain.Uint64Value(info.Total),
		},
		{
			Name:      nodeMetricSwapUsageBytes,
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitBytes,
			Timestamp: now,
			Value:     domain.Uint64Value(info.Used),
		},
	}
}

func (c *NodeMetricsCollector) collectDisks(now time.Time) []domain.Metric {
	partitions, err := disk.Partitions(false)
	if err != nil {
		log.WithError(errors.Wrap(err, "disk.Partitions")).Debug("collect disk partitions failed")
		return nil
	}

	out := make([]domain.Metric, 0, len(partitions)*3)
	seen := make(map[string]struct{}, len(partitions))

	var workMount string
	if c.driveFilter == nil {
		mountpoints := make([]string, len(partitions))
		for i := range partitions {
			mountpoints[i] = partitions[i].Mountpoint
		}
		workMount = longestMountPrefix(c.cfg.WorkPath, mountpoints)
	}

	for i := range partitions {
		p := partitions[i]
		if _, dup := seen[p.Mountpoint]; dup {
			continue
		}
		seen[p.Mountpoint] = struct{}{}

		if !selectDrive(c.driveFilter, p.Mountpoint, p.Device, workMount) {
			continue
		}

		usage, err := disk.Usage(p.Mountpoint)
		if err != nil {
			log.WithError(errors.Wrap(err, "disk.Usage")).
				WithField("mount", p.Mountpoint).
				Debug("collect disk usage failed")
			continue
		}

		labels := map[string]string{labelMount: p.Mountpoint}
		out = append(out,
			domain.Metric{
				Name:      nodeMetricDiskTotalBytes,
				Type:      domain.MetricTypeGauge,
				Unit:      domain.MetricUnitBytes,
				Labels:    labels,
				Timestamp: now,
				Value:     domain.Uint64Value(usage.Total),
			},
			domain.Metric{
				Name:      nodeMetricDiskUsageBytes,
				Type:      domain.MetricTypeGauge,
				Unit:      domain.MetricUnitBytes,
				Labels:    labels,
				Timestamp: now,
				Value:     domain.Uint64Value(usage.Used),
			},
			domain.Metric{
				Name:      nodeMetricDiskUsagePercent,
				Type:      domain.MetricTypeGauge,
				Unit:      domain.MetricUnitPercent,
				Labels:    labels,
				Timestamp: now,
				Value:     domain.Float64Value(usage.UsedPercent),
			},
		)
	}

	return out
}

func (c *NodeMetricsCollector) collectNetwork(now time.Time) []domain.Metric {
	counters, err := net.IOCounters(true)
	if err != nil {
		log.WithError(errors.Wrap(err, "net.IOCounters")).Debug("collect network metrics failed")
		return nil
	}

	out := make([]domain.Metric, 0, len(counters)*2)
	for i := range counters {
		ifc := counters[i]
		if !selectInterface(c.ifFilter, ifc.Name) {
			continue
		}

		labels := map[string]string{labelInterface: ifc.Name}
		out = append(out,
			domain.Metric{
				Name:      nodeMetricNetworkReceiveBytesTotal,
				Type:      domain.MetricTypeCounter,
				Unit:      domain.MetricUnitBytes,
				Labels:    labels,
				Timestamp: now,
				Value:     domain.Uint64Value(ifc.BytesRecv),
			},
			domain.Metric{
				Name:      nodeMetricNetworkTransmitBytesTotal,
				Type:      domain.MetricTypeCounter,
				Unit:      domain.MetricUnitBytes,
				Labels:    labels,
				Timestamp: now,
				Value:     domain.Uint64Value(ifc.BytesSent),
			},
		)
	}
	return out
}

func (c *NodeMetricsCollector) collectLoad(now time.Time) []domain.Metric {
	if runtime.GOOS == "windows" {
		return nil
	}

	avg, err := load.Avg()
	if err != nil {
		log.WithError(errors.Wrap(err, "load.Avg")).Debug("collect load metrics failed")
		return nil
	}

	return []domain.Metric{
		{
			Name:      nodeMetricLoad1,
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitCount,
			Timestamp: now,
			Value:     domain.Float64Value(avg.Load1),
		},
		{
			Name:      nodeMetricLoad5,
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitCount,
			Timestamp: now,
			Value:     domain.Float64Value(avg.Load5),
		},
		{
			Name:      nodeMetricLoad15,
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitCount,
			Timestamp: now,
			Value:     domain.Float64Value(avg.Load15),
		},
	}
}

func (c *NodeMetricsCollector) collectUptime(now time.Time) []domain.Metric {
	uptime, err := host.Uptime()
	if err != nil {
		log.WithError(errors.Wrap(err, "host.Uptime")).Debug("collect uptime failed")
		return nil
	}

	return []domain.Metric{
		{
			Name:      nodeMetricUptimeSecondsTotal,
			Type:      domain.MetricTypeCounter,
			Unit:      domain.MetricUnitSeconds,
			Timestamp: now,
			Value:     domain.Uint64Value(uptime),
		},
	}
}
