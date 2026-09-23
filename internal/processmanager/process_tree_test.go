package processmanager

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The layouts of SYSTEM_PROCESS_INFORMATION that x/sys has on 64-bit Windows and on 386.
var (
	processRecordLayout64 = processRecordLayout{
		size: 256, threads: 4, privateWorkingSet: 8, createTime: 32, userTime: 40, kernelTime: 48,
		pid: 80, parentPID: 88, readBytes: 232, writeBytes: 240,
	}
	processRecordLayout386 = processRecordLayout{
		size: 184, threads: 4, privateWorkingSet: 8, createTime: 32, userTime: 40, kernelTime: 48,
		pid: 68, parentPID: 72, readBytes: 160, writeBytes: 168,
	}
)

// systemThreadInformationSize is the size of the thread records the kernel writes after every
// process record.
const systemThreadInformationSize = 80

// encodeProcessSnapshot lays procs out the way NtQuerySystemInformation does: every process record
// is followed by one record per thread, and the last one has no next offset. Bytes the parser must
// not read are filled with garbage, and CPU time is split between the user and kernel fields.
func encodeProcessSnapshot(layout processRecordLayout, procs ...processInfo) []byte {
	pointerSize := layout.parentPID - layout.pid

	putID := func(b []byte, id uint32) {
		if pointerSize == 8 {
			binary.LittleEndian.PutUint64(b, uint64(id))

			return
		}

		binary.LittleEndian.PutUint32(b, id)
	}

	var buf []byte

	for i, p := range procs {
		record := bytes.Repeat([]byte{0xAB}, layout.size+int(p.Threads)*systemThreadInformationSize)

		next := uint32(0)
		if i < len(procs)-1 {
			next = uint32(len(record))
		}

		userTime := p.CPUTime / 3

		binary.LittleEndian.PutUint32(record, next)
		binary.LittleEndian.PutUint32(record[layout.threads:], p.Threads)
		binary.LittleEndian.PutUint64(record[layout.privateWorkingSet:], p.PrivateWorkingSet)
		binary.LittleEndian.PutUint64(record[layout.createTime:], uint64(p.CreateTime))
		binary.LittleEndian.PutUint64(record[layout.userTime:], uint64(userTime))
		binary.LittleEndian.PutUint64(record[layout.kernelTime:], uint64(p.CPUTime-userTime))
		putID(record[layout.pid:], p.PID)
		putID(record[layout.parentPID:], p.ParentPID)
		binary.LittleEndian.PutUint64(record[layout.readBytes:], p.ReadBytes)
		binary.LittleEndian.PutUint64(record[layout.writeBytes:], p.WriteBytes)

		buf = append(buf, record...)
	}

	return buf
}

func TestParseProcessSnapshot(t *testing.T) {
	system := processInfo{
		PID: 4, CreateTime: 133_700_000_000_000_000, CPUTime: 91_234_567, Threads: 3,
		PrivateWorkingSet: 196 << 10, ReadBytes: 1 << 20, WriteBytes: 3 << 20,
	}
	shawl := processInfo{
		PID: 7112, ParentPID: 640, CreateTime: 133_700_000_100_000_000, CPUTime: 1_500_000, Threads: 5,
		PrivateWorkingSet: 2 << 20, ReadBytes: 40_960, WriteBytes: 81_920,
	}
	game := processInfo{
		PID: 7340, ParentPID: 7112, CreateTime: 133_700_000_100_000_001, CPUTime: 7_200_000_000, Threads: 41,
		PrivateWorkingSet: 1 << 30, ReadBytes: 5 << 30, WriteBytes: 12_345,
	}

	layouts := []struct {
		name   string
		layout processRecordLayout
	}{
		{name: "64_bit", layout: processRecordLayout64},
		{name: "386", layout: processRecordLayout386},
	}

	tests := []struct {
		name  string
		procs []processInfo
	}{
		{name: "one_record", procs: []processInfo{system}},
		{name: "several_records", procs: []processInfo{system, shawl, game}},
	}

	for _, l := range layouts {
		for _, tt := range tests {
			t.Run(l.name+"_"+tt.name, func(t *testing.T) {
				got, err := parseProcessSnapshot(encodeProcessSnapshot(l.layout, tt.procs...), l.layout)

				require.NoError(t, err)
				assert.Equal(t, tt.procs, got)
			})
		}
	}
}

func TestParseProcessSnapshot_Malformed(t *testing.T) {
	layout := processRecordLayout64

	// 416 bytes for the first process and its two threads, 336 for the second and its one thread.
	valid := encodeProcessSnapshot(layout,
		processInfo{PID: 4, Threads: 2},
		processInfo{PID: 88, ParentPID: 4, Threads: 1},
	)
	require.Len(t, valid, 752)

	withFirstNext := func(next uint32) []byte {
		buf := bytes.Clone(valid)
		binary.LittleEndian.PutUint32(buf, next)

		return buf
	}

	tests := []struct {
		name      string
		buf       []byte
		wantError string
	}{
		{
			name:      "empty_buffer",
			buf:       nil,
			wantError: "record at offset 0 overruns the 0 bytes returned",
		},
		{
			name:      "first_record_cut_short",
			buf:       valid[:layout.size-1],
			wantError: "record at offset 0 overruns the 255 bytes returned",
		},
		{
			name:      "last_record_cut_short",
			buf:       valid[:416+layout.size-1],
			wantError: "record at offset 416 overruns the 671 bytes returned",
		},
		{
			name:      "next_record_inside_this_one",
			buf:       withFirstNext(uint32(layout.size - 8)),
			wantError: "record at offset 0 puts the next one 248 bytes on, in a 752-byte buffer",
		},
		{
			name:      "next_record_past_the_end",
			buf:       withFirstNext(760),
			wantError: "record at offset 0 puts the next one 760 bytes on, in a 752-byte buffer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseProcessSnapshot(tt.buf, layout)

			require.ErrorIs(t, err, ErrProcessSnapshotMalformed)
			assert.Contains(t, err.Error(), tt.wantError)
			assert.Nil(t, got)
		})
	}
}

func TestProcessDescendants(t *testing.T) {
	hour := int64(time.Hour / filetimeTick)

	services := processInfo{PID: 640, ParentPID: 520, CreateTime: 1 * hour}
	shawl := processInfo{PID: 1200, ParentPID: 640, CreateTime: 10 * hour}
	cmd := processInfo{PID: 3100, ParentPID: 1200, CreateTime: 10*hour + 5}
	game := processInfo{PID: 3180, ParentPID: 3100, CreateTime: 10*hour + 9}
	conhost := processInfo{PID: 3192, ParentPID: 3180, CreateTime: 10*hour + 9}
	otherShawl := processInfo{PID: 1300, ParentPID: 640, CreateTime: 11 * hour}
	otherGame := processInfo{PID: 4400, ParentPID: 1300, CreateTime: 11*hour + 3}

	// Started by an earlier process that held the ID shawl has now.
	orphan := processInfo{PID: 2900, ParentPID: 1200, CreateTime: 9 * hour}
	orphanChild := processInfo{PID: 2950, ParentPID: 2900, CreateTime: 10*hour + 7}

	idle := processInfo{PID: 0, ParentPID: 0, CreateTime: 0}
	system := processInfo{PID: 4, ParentPID: 0, CreateTime: 0}
	smss := processInfo{PID: 380, ParentPID: 4, CreateTime: 1}

	tests := []struct {
		name      string
		procs     []processInfo
		rootPID   uint32
		want      []processInfo
		wantFound bool
	}{
		{
			name:      "root_without_children",
			procs:     []processInfo{services, shawl, otherShawl},
			rootPID:   shawl.PID,
			want:      nil,
			wantFound: true,
		},
		{
			name:      "children_and_grandchildren_but_nothing_else",
			procs:     []processInfo{services, otherGame, conhost, game, shawl, otherShawl, cmd},
			rootPID:   shawl.PID,
			want:      []processInfo{cmd, game, conhost},
			wantFound: true,
		},
		{
			name:      "root_missing",
			procs:     []processInfo{services, cmd, game},
			rootPID:   shawl.PID,
			want:      nil,
			wantFound: false,
		},
		{
			name:      "orphan_under_a_reused_id_is_skipped_with_its_children",
			procs:     []processInfo{services, shawl, orphan, orphanChild, cmd, game},
			rootPID:   shawl.PID,
			want:      []processInfo{cmd, game},
			wantFound: true,
		},
		{
			name:      "child_started_in_the_same_clock_step_as_its_parent",
			procs:     []processInfo{shawl, {PID: 3500, ParentPID: shawl.PID, CreateTime: shawl.CreateTime}},
			rootPID:   shawl.PID,
			want:      []processInfo{{PID: 3500, ParentPID: shawl.PID, CreateTime: shawl.CreateTime}},
			wantFound: true,
		},
		{
			name:      "root_that_is_its_own_parent",
			procs:     []processInfo{idle, system, smss},
			rootPID:   idle.PID,
			want:      []processInfo{system, smss},
			wantFound: true,
		},
		{
			name: "parents_that_point_at_each_other",
			procs: []processInfo{
				{PID: 10, ParentPID: 20, CreateTime: 5},
				{PID: 20, ParentPID: 10, CreateTime: 5},
			},
			rootPID:   10,
			want:      []processInfo{{PID: 20, ParentPID: 10, CreateTime: 5}},
			wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := processDescendants(tt.procs, tt.rootPID)

			assert.Equal(t, tt.wantFound, found)
			assert.Equal(t, tt.want, got)
		})
	}
}
