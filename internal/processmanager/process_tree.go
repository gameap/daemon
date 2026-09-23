package processmanager

import (
	"encoding/binary"

	"github.com/pkg/errors"
)

// processInfo is one process as a system-wide snapshot reports it. Times are FILETIME ticks of
// 100ns; CreateTime counts them from 1601-01-01 UTC, CPUTime is user and kernel time together.
type processInfo struct {
	PID               uint32
	ParentPID         uint32
	CreateTime        int64
	CPUTime           int64
	Threads           uint32
	PrivateWorkingSet uint64
	ReadBytes         uint64
	WriteBytes        uint64
}

// processRecordLayout is the size of one SYSTEM_PROCESS_INFORMATION record and the offsets of the
// fields the daemon reads. The fields after ImageName move with the pointer size, so the Windows
// build takes every offset from x/sys instead of keeping a table of its own.
//
// Records are read field by field rather than cast to the x/sys struct: the struct has pointer
// fields that the kernel fills with values the Go runtime must never treat as pointers.
type processRecordLayout struct {
	size              int
	threads           int
	privateWorkingSet int
	createTime        int
	userTime          int
	kernelTime        int
	pid               int
	parentPID         int
	readBytes         int
	writeBytes        int
}

// parseProcessSnapshot reads the records NtQuerySystemInformation(SystemProcessInformation) wrote to
// buf. Every record starts with the distance to the next one, zero on the last, and the thread
// records between two process records are skipped with that distance.
func parseProcessSnapshot(buf []byte, layout processRecordLayout) ([]processInfo, error) {
	procs := make([]processInfo, 0, 256)

	for offset := 0; ; {
		if len(buf)-offset < layout.size {
			return nil, errors.WithMessagef(
				ErrProcessSnapshotMalformed,
				"record at offset %d overruns the %d bytes returned", offset, len(buf),
			)
		}

		record := buf[offset : offset+layout.size]

		// The IDs sit in HANDLE-sized fields but are DWORDs, and on a little-endian machine the low
		// 32 bits come first whatever the pointer size.
		procs = append(procs, processInfo{
			PID:        binary.LittleEndian.Uint32(record[layout.pid:]),
			ParentPID:  binary.LittleEndian.Uint32(record[layout.parentPID:]),
			CreateTime: int64(binary.LittleEndian.Uint64(record[layout.createTime:])),
			CPUTime: int64(binary.LittleEndian.Uint64(record[layout.userTime:])) +
				int64(binary.LittleEndian.Uint64(record[layout.kernelTime:])),
			Threads:           binary.LittleEndian.Uint32(record[layout.threads:]),
			PrivateWorkingSet: binary.LittleEndian.Uint64(record[layout.privateWorkingSet:]),
			ReadBytes:         binary.LittleEndian.Uint64(record[layout.readBytes:]),
			WriteBytes:        binary.LittleEndian.Uint64(record[layout.writeBytes:]),
		})

		next := uint64(binary.LittleEndian.Uint32(record))
		if next == 0 {
			return procs, nil
		}

		if next < uint64(layout.size) || next > uint64(len(buf)-offset) {
			return nil, errors.WithMessagef(
				ErrProcessSnapshotMalformed,
				"record at offset %d puts the next one %d bytes on, in a %d-byte buffer", offset, next, len(buf),
			)
		}

		offset += int(next)
	}
}

// processDescendants returns every process that rootPID started, directly or through its children,
// and reports whether rootPID itself is in the snapshot. The root is not part of the result.
//
// A process keeps the ID of the parent that created it for its whole life, while Windows hands a
// freed ID to the next process that starts. A process that names the parent's ID but is older than
// the parent was created by an earlier holder of that ID, so it is skipped with everything it
// started. Equal times are accepted because CreateTime comes from a clock that advances in steps of
// several milliseconds. A wall clock set back by more than a parent's age would make a child started
// afterwards look older than its parent; such a child is skipped too.
func processDescendants(procs []processInfo, rootPID uint32) ([]processInfo, bool) {
	root := -1
	children := make(map[uint32][]int, len(procs))

	for i := range procs {
		if procs[i].PID == rootPID {
			root = i
		}

		children[procs[i].ParentPID] = append(children[procs[i].ParentPID], i)
	}

	if root < 0 {
		return nil, false
	}

	var tree []processInfo

	visited := map[uint32]struct{}{rootPID: {}}
	queue := []int{root}

	for len(queue) > 0 {
		parent := procs[queue[0]]
		queue = queue[1:]

		for _, i := range children[parent.PID] {
			child := procs[i]

			if _, seen := visited[child.PID]; seen || child.CreateTime < parent.CreateTime {
				continue
			}

			visited[child.PID] = struct{}{}
			tree = append(tree, child)
			queue = append(queue, i)
		}
	}

	return tree, true
}
