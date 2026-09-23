//go:build windows

package processmanager

import (
	"os"
	"os/exec"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeProcessRecordLayout(t *testing.T) {
	want := processRecordLayout64
	if unsafe.Sizeof(uintptr(0)) == 4 {
		want = processRecordLayout386
	}

	assert.Equal(t, want, nativeProcessRecordLayout)
}

func TestProcessSnapshotterRead_ContainsCurrentProcess(t *testing.T) {
	procs, takenAt, err := (&processSnapshotter{}).read()
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now(), takenAt, time.Minute)

	var self *processInfo

	for i := range procs {
		if procs[i].PID == uint32(os.Getpid()) {
			self = &procs[i]
		}
	}

	require.NotNil(t, self, "the snapshot must contain the test process")
	assert.Equal(t, uint32(os.Getppid()), self.ParentPID)
	assert.Positive(t, self.Threads)
	assert.Positive(t, self.PrivateWorkingSet)
	assert.Positive(t, self.CPUTime)
	assert.Less(t, self.CreateTime, filetimeTicks(time.Now()))
}

func TestProcessSnapshotterRead_GrowsBuffer(t *testing.T) {
	snapshots := &processSnapshotter{buf: make([]uint64, 1)}

	procs, _, err := snapshots.read()

	require.NoError(t, err)
	assert.NotEmpty(t, procs)
	assert.Greater(t, len(snapshots.buf), 1)
}

func TestProcessSnapshotterSnapshot_SharesRecentRead(t *testing.T) {
	snapshots := &processSnapshotter{}

	_, first, err := snapshots.snapshot()
	require.NoError(t, err)

	_, second, err := snapshots.snapshot()
	require.NoError(t, err)

	assert.Equal(t, first, second)
}

func TestProcessDescendants_FindsChildAndGrandchild(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "ping", "-n", "3", "127.0.0.1")
	require.NoError(t, cmd.Start())

	t.Cleanup(func() {
		_ = cmd.Wait()
	})

	cmdPID := uint32(cmd.Process.Pid)

	assert.Eventually(t, func() bool {
		procs, _, err := (&processSnapshotter{}).read()
		if err != nil {
			return false
		}

		tree, found := processDescendants(procs, uint32(os.Getpid()))
		if !found {
			return false
		}

		var hasCmd, hasPing bool

		for _, p := range tree {
			hasCmd = hasCmd || p.PID == cmdPID
			hasPing = hasPing || p.ParentPID == cmdPID
		}

		return hasCmd && hasPing
	}, 5*time.Second, 100*time.Millisecond, "cmd.exe and the ping it runs must be in the test process's tree")
}
