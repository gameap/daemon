//go:build windows

package processmanager

import (
	"context"
	"sync"
	"time"
	"unsafe"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
)

const (
	processSnapshotInitialSize = 512 * 1024
	processSnapshotMaxSize     = 64 * 1024 * 1024
	processSnapshotAttempts    = 5

	// processSnapshotMaxAge lets every server of one metrics tick share a snapshot. It is half the
	// shortest collection interval, so two ticks never do.
	processSnapshotMaxAge = config.MetricsMinCollectionInterval / 2
)

var nativeProcessRecordLayout = processRecordLayout{
	size:              int(unsafe.Sizeof(windows.SYSTEM_PROCESS_INFORMATION{})),
	threads:           int(unsafe.Offsetof(windows.SYSTEM_PROCESS_INFORMATION{}.NumberOfThreads)),
	privateWorkingSet: int(unsafe.Offsetof(windows.SYSTEM_PROCESS_INFORMATION{}.WorkingSetPrivateSize)),
	createTime:        int(unsafe.Offsetof(windows.SYSTEM_PROCESS_INFORMATION{}.CreateTime)),
	userTime:          int(unsafe.Offsetof(windows.SYSTEM_PROCESS_INFORMATION{}.UserTime)),
	kernelTime:        int(unsafe.Offsetof(windows.SYSTEM_PROCESS_INFORMATION{}.KernelTime)),
	pid:               int(unsafe.Offsetof(windows.SYSTEM_PROCESS_INFORMATION{}.UniqueProcessID)),
	parentPID:         int(unsafe.Offsetof(windows.SYSTEM_PROCESS_INFORMATION{}.InheritedFromUniqueProcessID)),
	readBytes:         int(unsafe.Offsetof(windows.SYSTEM_PROCESS_INFORMATION{}.ReadTransferCount)),
	writeBytes:        int(unsafe.Offsetof(windows.SYSTEM_PROCESS_INFORMATION{}.WriteTransferCount)),
}

// processSnapshotter reads the process list of the host. The list covers every process and thread
// on the host, hundreds of kilobytes, so one read serves every server of a metrics tick and the
// buffer is kept for the next tick.
type processSnapshotter struct {
	mu      sync.Mutex
	buf     []uint64
	procs   []processInfo
	takenAt time.Time
}

// snapshot returns the process list and the time it was read. The list is shared between callers
// and must not be modified.
func (s *processSnapshotter) snapshot() ([]processInfo, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.procs != nil && time.Since(s.takenAt) < processSnapshotMaxAge {
		return s.procs, s.takenAt, nil
	}

	procs, takenAt, err := s.read()
	if err != nil {
		return nil, time.Time{}, err
	}

	s.procs, s.takenAt = procs, takenAt

	return procs, takenAt, nil
}

// read queries the process list. The buffer is a []uint64 because the records carry 64-bit fields,
// which the kernel writes aligned.
func (s *processSnapshotter) read() ([]processInfo, time.Time, error) {
	if len(s.buf) == 0 {
		s.buf = make([]uint64, processSnapshotInitialSize/8)
	}

	for range processSnapshotAttempts {
		size := len(s.buf) * 8
		takenAt := time.Now()

		var written uint32

		err := windows.NtQuerySystemInformation(
			windows.SystemProcessInformation, unsafe.Pointer(&s.buf[0]), uint32(size), &written,
		)
		if errors.Is(err, windows.STATUS_INFO_LENGTH_MISMATCH) {
			// Processes start between two calls, so the buffer gets more room than the kernel
			// asked for a moment ago.
			grown := max(2*size, int(written)+int(written)/4)
			if grown > processSnapshotMaxSize {
				return nil, time.Time{}, errors.WithMessagef(
					ErrProcessSnapshotTooLarge, "%d bytes needed, at most %d allowed", written, processSnapshotMaxSize,
				)
			}

			s.buf = make([]uint64, (grown+7)/8)

			continue
		}
		if err != nil {
			return nil, time.Time{}, errors.Wrap(err, "failed to query the process list")
		}

		buf := unsafe.Slice((*byte)(unsafe.Pointer(&s.buf[0])), min(int(written), size))

		procs, err := parseProcessSnapshot(buf, nativeProcessRecordLayout)
		if err != nil {
			return nil, time.Time{}, err
		}

		return procs, takenAt, nil
	}

	return nil, time.Time{}, errors.WithMessagef(
		ErrProcessSnapshotTooLarge, "the process list kept growing over %d attempts", processSnapshotAttempts,
	)
}

// serviceProcessMetrics reports what the processes a Windows service started are using: the game
// server and whatever it runs through, such as cmd.exe for a script. The service process itself is
// a supervisor, and systemd and container runtimes keep theirs out of a unit's or container's
// accounting as well.
//
// It returns nothing while the service has no process; the caller reports liveness on its own.
func serviceProcessMetrics(
	ctx context.Context, serviceName string, snapshots *processSnapshotter, usage *processTreeSampler,
) []domain.Metric {
	status, err := queryService(serviceName)
	if err != nil || status.ProcessId == 0 {
		usage.forget(serviceName)

		if err != nil && !errors.Is(err, ErrServiceNotFound) {
			logger.WithError(ctx, err).Debug("Failed to query service " + serviceName + " for metrics")
		}

		return nil
	}

	procs, takenAt, err := snapshots.snapshot()
	if err != nil {
		logger.WithError(ctx, err).Debug("Failed to read the process list for metrics")

		return nil
	}

	// The snapshot can be up to processSnapshotMaxAge older than the query. A service that started
	// in between is not in it yet and is measured on the next tick; in the rare case that its
	// process ID was still held by another process then, that process is reported for one tick.
	tree, found := processDescendants(procs, status.ProcessId)
	if !found {
		usage.forget(serviceName)

		return nil
	}

	return processTreeMetrics(takenAt, serviceName, usage.observe(serviceName, tree, takenAt))
}
