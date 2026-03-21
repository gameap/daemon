package grpc

import (
	"runtime"

	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	log "github.com/sirupsen/logrus"
)

type HeartbeatCollector struct {
	workDir string
}

func NewHeartbeatCollector(workDir string) *HeartbeatCollector {
	return &HeartbeatCollector{
		workDir: workDir,
	}
}

func (c *HeartbeatCollector) CollectStats() *pb.SystemStats {
	stats := &pb.SystemStats{}

	if cpuPercent, err := cpu.Percent(0, false); err == nil && len(cpuPercent) > 0 {
		stats.CpuUsagePercent = cpuPercent[0]
	} else if err != nil {
		log.WithError(err).Debug("Failed to get CPU usage")
	}

	if memInfo, err := mem.VirtualMemory(); err == nil {
		stats.MemoryTotalBytes = memInfo.Total
		stats.MemoryUsedBytes = memInfo.Used
	} else {
		log.WithError(err).Debug("Failed to get memory info")
	}

	if diskInfo, err := disk.Usage(c.workDir); err == nil {
		stats.DiskTotalBytes = diskInfo.Total
		stats.DiskUsedBytes = diskInfo.Used
	} else {
		log.WithError(err).Debug("Failed to get disk info")
	}

	if runtime.GOOS != "windows" {
		if loadInfo, err := load.Avg(); err == nil {
			stats.LoadAverage_1M = loadInfo.Load1
			stats.LoadAverage_5M = loadInfo.Load5
			stats.LoadAverage_15M = loadInfo.Load15
		} else {
			log.WithError(err).Debug("Failed to get load average")
		}
	}

	return stats
}
