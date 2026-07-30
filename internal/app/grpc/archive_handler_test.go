package grpc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
)

type fakeSender struct {
	mu   sync.Mutex
	msgs []*pb.DaemonMessage
}

func (f *fakeSender) Send(msg *pb.DaemonMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, msg)
}

func (f *fakeSender) archiveResponses(requestID string) []*pb.ArchiveResponse {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []*pb.ArchiveResponse
	for _, m := range f.msgs {
		if r := m.GetArchiveResponse(); r != nil && r.GetRequestId() == requestID {
			out = append(out, r)
		}
	}

	return out
}

func (f *fakeSender) allProgress() []*pb.ArchiveProgress {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []*pb.ArchiveProgress
	for _, m := range f.msgs {
		if p := m.GetArchiveProgress(); p != nil {
			out = append(out, p)
		}
	}

	return out
}

func (f *fakeSender) messageCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.msgs)
}

func (f *fakeSender) waitFinalResponse(t *testing.T, requestID string) *pb.ArchiveResponse {
	t.Helper()
	return f.waitFinalResponses(t, requestID, 1)[0]
}

func (f *fakeSender) waitFinalResponses(t *testing.T, requestID string, n int) []*pb.ArchiveResponse {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		resps := f.archiveResponses(requestID)
		if len(resps) >= n {
			return resps
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d final ArchiveResponse(s) for %q, got %d", n, requestID, len(resps))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func setupArchiveWorkDir(t *testing.T, files int) string {
	t.Helper()

	workDir := t.TempDir()
	srcDir := filepath.Join(workDir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))

	for i := 0; i < files; i++ {
		name := filepath.Join(srcDir, fmt.Sprintf("file_%04d.txt", i))
		content := fmt.Sprintf("content of file %d\n", i)
		require.NoError(t, os.WriteFile(name, []byte(content), 0o644))
	}

	return workDir
}

func createArchiveRequest(requestID, archivePath string) *pb.ArchiveRequest {
	return &pb.ArchiveRequest{
		RequestId: requestID,
		Operation: &pb.ArchiveRequest_Create{
			Create: &pb.CreateArchiveParams{
				ArchivePath: archivePath,
				Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
				BasePath:    "src",
				Sources:     []string{"."},
			},
		},
	}
}

func TestGRPCArchiveHandler_CreateZip(t *testing.T) {
	workDir := setupArchiveWorkDir(t, 2)

	sender := &fakeSender{}
	h := NewGRPCArchiveHandler(workDir, sender, 4)

	req := createArchiveRequest("create-1", "out.zip")
	req.ProgressInterval = durationpb.New(time.Millisecond)
	h.HandleArchiveRequest(context.Background(), req)

	resp := sender.waitFinalResponse(t, "create-1")
	require.True(t, resp.Success, resp.Error)
	assert.GreaterOrEqual(t, resp.FilesProcessed, uint32(2))
	assert.Greater(t, resp.ArchiveSize, uint64(0))
	assert.Equal(t, pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP, resp.Format)

	for _, p := range sender.allProgress() {
		assert.Equal(t, "create-1", p.GetRequestId())
	}
}

func TestGRPCArchiveHandler_ExtractMissingArchive(t *testing.T) {
	workDir := t.TempDir()

	sender := &fakeSender{}
	h := NewGRPCArchiveHandler(workDir, sender, 4)

	h.HandleArchiveRequest(context.Background(), &pb.ArchiveRequest{
		RequestId: "extract-1",
		Operation: &pb.ArchiveRequest_Extract{
			Extract: &pb.ExtractArchiveParams{
				ArchivePath: "missing.zip",
				Destination: "dst",
				Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
			},
		},
	})

	resp := sender.waitFinalResponse(t, "extract-1")
	assert.False(t, resp.Success)
	assert.NotEmpty(t, resp.Error)
}

func TestGRPCArchiveHandler_Cancel(t *testing.T) {
	workDir := setupArchiveWorkDir(t, 500)

	sender := &fakeSender{}
	h := NewGRPCArchiveHandler(workDir, sender, 4)
	ctx := context.Background()

	h.HandleArchiveRequest(ctx, createArchiveRequest("cancel-1", "cancel.zip"))
	h.HandleArchiveCancel(ctx, &pb.ArchiveCancel{RequestId: "cancel-1", Reason: "test"})

	resp := sender.waitFinalResponse(t, "cancel-1")
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "canceled")
}

func TestGRPCArchiveHandler_DuplicateRejected(t *testing.T) {
	workDir := setupArchiveWorkDir(t, 500)

	sender := &fakeSender{}
	h := NewGRPCArchiveHandler(workDir, sender, 4)
	ctx := context.Background()

	h.HandleArchiveRequest(ctx, createArchiveRequest("dup-1", "dup.zip"))
	h.HandleArchiveRequest(ctx, createArchiveRequest("dup-1", "dup2.zip"))

	resps := sender.waitFinalResponses(t, "dup-1", 2)

	var duplicate *pb.ArchiveResponse
	for _, r := range resps {
		if strings.Contains(r.Error, "already active") {
			duplicate = r
		}
	}
	require.NotNil(t, duplicate, "expected an immediate 'already active' rejection")
	assert.False(t, duplicate.Success)
	assert.Contains(t, duplicate.Error, "dup-1")
}

func TestGRPCArchiveHandler_CancelUnknown(t *testing.T) {
	sender := &fakeSender{}
	h := NewGRPCArchiveHandler(t.TempDir(), sender, 4)

	h.HandleArchiveCancel(context.Background(), &pb.ArchiveCancel{RequestId: "nope", Reason: "x"})

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 0, sender.messageCount())
}

func TestGRPCArchiveHandler_NoOperation(t *testing.T) {
	sender := &fakeSender{}
	h := NewGRPCArchiveHandler(t.TempDir(), sender, 4)

	h.HandleArchiveRequest(context.Background(), &pb.ArchiveRequest{RequestId: "noop-1"})

	resp := sender.waitFinalResponse(t, "noop-1")
	assert.False(t, resp.Success)
	assert.Equal(t, "extract or create operation required", resp.Error)
}
