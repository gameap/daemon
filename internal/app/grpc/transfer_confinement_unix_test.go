//go:build linux || darwin

package grpc

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gameap/daemon/internal/app/fsutil"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// fakeTransferService stands in for the panel side of FileTransferService: it
// serves one payload to download and records what is uploaded to it.
type fakeTransferService struct {
	pb.FileTransferServiceClient

	payload  []byte
	uploaded []byte
	uploads  int

	// downloadCtx is the context the last download stream was opened with.
	downloadCtx context.Context
}

func (f *fakeTransferService) DownloadFile(
	ctx context.Context, _ *pb.DownloadRequest, _ ...grpc.CallOption,
) (grpc.ServerStreamingClient[pb.DownloadChunk], error) {
	f.downloadCtx = ctx

	return &fakeDownloadStream{
		fakeChunkReceiver: &fakeChunkReceiver{
			chunks: []*pb.DownloadChunk{{Data: f.payload, IsFinal: true, ChecksumSha256: sha256Hex(f.payload)}},
		},
	}, nil
}

func (f *fakeTransferService) UploadFile(
	_ context.Context, _ ...grpc.CallOption,
) (grpc.ClientStreamingClient[pb.UploadChunk, pb.UploadResult], error) {
	f.uploads++

	return &fakeUploadStream{service: f}, nil
}

type fakeDownloadStream struct {
	grpc.ClientStream
	*fakeChunkReceiver
}

type fakeUploadStream struct {
	grpc.ClientStream

	service *fakeTransferService
}

func (s *fakeUploadStream) Send(chunk *pb.UploadChunk) error {
	s.service.uploaded = append(s.service.uploaded, chunk.GetData()...)

	return nil
}

func (s *fakeUploadStream) CloseAndRecv() (*pb.UploadResult, error) {
	return &pb.UploadResult{Success: true}, nil
}

func transferResponse(t *testing.T, sender *fakeSender) *pb.FileWriteResponse {
	t.Helper()

	sender.mu.Lock()
	defer sender.mu.Unlock()

	require.Len(t, sender.msgs, 1)

	resp := sender.msgs[0].GetFileWriteResponse()
	require.NotNil(t, resp)

	return resp
}

func TestGRPCTransferHandler_UploadTask_Confinement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		path      string
		wantError string
		// wantFile is where the payload must land, relative to the work directory.
		wantFile string
	}{
		{
			name:     "own_file",
			path:     "servers/a/addons/new/uploaded.txt",
			wantFile: "servers/a/addons/new/uploaded.txt",
		},
		{
			name:     "own_file_behind_a_link_staying_inside",
			path:     "servers/a/in-link/uploaded.cfg",
			wantFile: "servers/a/cstrike/uploaded.cfg",
		},
		{
			name:      "into_the_neighbour_through_a_link",
			path:      "servers/a/servers-link/b/planted.txt",
			wantError: "path escapes from parent",
		},
		{
			name:      "over_a_node_file_through_a_link",
			path:      "servers/a/node-link/steamcmd/steamcmd.sh",
			wantError: "path escapes from parent",
		},
		{
			// The finished upload is renamed into place, which replaces the
			// link itself: the file it pointed at is left alone.
			name:     "link_to_a_neighbour_file_is_replaced_not_followed",
			path:     "servers/a/file-link",
			wantFile: "servers/a/file-link",
		},
	}

	for modeName, mode := range confinementModes() {
		for _, tt := range tests {
			t.Run(modeName+"/"+tt.name, func(t *testing.T) {
				t.Parallel()

				workDir := tenantWorkDir(t)
				outside := []string{"servers/b", "steamcmd", ".plugins"}

				before := map[string]map[string]string{}
				for _, dir := range outside {
					before[dir] = snapshotTree(t, filepath.Join(workDir, filepath.FromSlash(dir)))
				}

				sender := &fakeSender{}
				service := &fakeTransferService{payload: []byte("uploaded payload")}
				h := NewGRPCTransferHandler(workDir, &FileTransferClient{client: service}, sender, 1, mode.opts...)

				h.HandleFileUploadTask(context.Background(), "req", &pb.FileUploadTask{
					TransferId: "tid",
					Path:       tt.path,
					Root:       mode.root,
				})

				resp := transferResponse(t, sender)

				for _, dir := range outside {
					assert.Equal(t, before[dir], snapshotTree(t, filepath.Join(workDir, filepath.FromSlash(dir))), dir)
				}

				if tt.wantError != "" {
					assert.False(t, resp.GetSuccess())
					assert.Contains(t, resp.GetError(), tt.wantError)

					return
				}

				require.True(t, resp.GetSuccess(), resp.GetError())

				info, err := os.Lstat(filepath.Join(workDir, filepath.FromSlash(tt.wantFile)))
				require.NoError(t, err)
				assert.True(t, info.Mode().IsRegular(), info.Mode().String())

				content, err := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(tt.wantFile)))
				require.NoError(t, err)
				assert.Equal(t, "uploaded payload", string(content))

				_, err = os.Lstat(filepath.Join(workDir, filepath.FromSlash(tt.wantFile)+".tmp_tid"))
				assert.ErrorIs(t, err, os.ErrNotExist)
			})
		}
	}
}

func TestGRPCTransferHandler_DownloadTask_Confinement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		wantError  string
		wantUpload string
	}{
		{
			name:       "own_file",
			path:       "servers/a/cstrike/server.cfg",
			wantUpload: "hostname a",
		},
		{
			name:       "own_file_behind_a_link_staying_inside",
			path:       "servers/a/in-link/server.cfg",
			wantUpload: "hostname a",
		},
		{
			name:      "neighbour_config_through_a_link",
			path:      "servers/a/servers-link/b/cstrike/server.cfg",
			wantError: "path escapes from parent",
		},
		{
			name:      "neighbour_config_through_a_file_link",
			path:      "servers/a/file-link",
			wantError: "path escapes from parent",
		},
		{
			name:      "plugin_secret_through_a_link",
			path:      "servers/a/users-link/srv.yaml",
			wantError: "path escapes from parent",
		},
	}

	for modeName, mode := range confinementModes() {
		for _, tt := range tests {
			t.Run(modeName+"/"+tt.name, func(t *testing.T) {
				t.Parallel()

				sender := &fakeSender{}
				service := &fakeTransferService{}
				h := NewGRPCTransferHandler(
					tenantWorkDir(t), &FileTransferClient{client: service}, sender, 1, mode.opts...,
				)

				h.HandleFileDownloadTask(context.Background(), "req", &pb.FileDownloadTask{
					TransferId: "tid",
					Path:       tt.path,
					Root:       mode.root,
				})

				resp := transferResponse(t, sender)

				if tt.wantError != "" {
					assert.False(t, resp.GetSuccess())
					assert.Contains(t, resp.GetError(), tt.wantError)
					assert.Zero(t, service.uploads)
					assert.Empty(t, service.uploaded)

					return
				}

				require.True(t, resp.GetSuccess(), resp.GetError())
				assert.Equal(t, tt.wantUpload, string(service.uploaded))
			})
		}
	}
}

func TestGRPCTransferHandler_PathOutsideTheRequestRoot(t *testing.T) {
	t.Parallel()

	workDir := tenantWorkDir(t)
	service := &fakeTransferService{payload: []byte("x")}

	t.Run("upload_task", func(t *testing.T) {
		t.Parallel()

		sender := &fakeSender{}
		h := NewGRPCTransferHandler(workDir, &FileTransferClient{client: service}, sender, 1)

		h.HandleFileUploadTask(context.Background(), "req", &pb.FileUploadTask{
			TransferId: "up", Path: "servers/b/planted.txt", Root: "servers/a",
		})

		resp := transferResponse(t, sender)
		assert.False(t, resp.GetSuccess())
		assert.Equal(t, "path is outside its confinement root", resp.GetError())

		_, err := os.Lstat(filepath.Join(workDir, "servers", "b", "planted.txt"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("download_task", func(t *testing.T) {
		t.Parallel()

		sender := &fakeSender{}
		h := NewGRPCTransferHandler(workDir, &FileTransferClient{client: service}, sender, 1)

		h.HandleFileDownloadTask(context.Background(), "req", &pb.FileDownloadTask{
			TransferId: "down", Path: "servers/b/cstrike/server.cfg", Root: "servers/a",
		})

		resp := transferResponse(t, sender)
		assert.False(t, resp.GetSuccess())
		assert.Equal(t, "path is outside its confinement root", resp.GetError())
	})
}

// A gRPC stream is released by draining it or by ending its context. A task
// that fails after the stream is opened never drains it, so the handler has to
// end the context itself rather than leave the stream to the connection.
func TestGRPCTransferHandler_UploadTask_ReleasesTheDownloadStream(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		path        string
		wantSuccess bool
	}{
		{name: "after_a_finished_download", path: "servers/a/uploaded.txt", wantSuccess: true},
		{name: "when_the_parent_directory_cannot_be_inspected", path: "servers/a/servers-link/b/planted.txt"},
		{name: "when_the_parent_directory_cannot_be_created", path: "servers/a/own.txt/uploaded.txt"},
		{name: "when_the_temp_file_cannot_be_opened", path: "servers/a/cstrike"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			workDir := tenantWorkDir(t)
			if tt.path == "servers/a/cstrike" {
				// The temp file name is taken by a directory.
				require.NoError(t, os.Mkdir(filepath.Join(workDir, "servers", "a", "cstrike.tmp_tid"), 0o755))
			}

			sender := &fakeSender{}
			service := &fakeTransferService{payload: []byte("uploaded payload")}
			h := NewGRPCTransferHandler(
				workDir, &FileTransferClient{client: service}, sender, 1,
				fsutil.WithServerDirs(knownServerDirs{"servers/a", "servers/b"}),
			)

			ctx := t.Context()

			h.HandleFileUploadTask(ctx, "req", &pb.FileUploadTask{TransferId: "tid", Path: tt.path})

			assert.Equal(t, tt.wantSuccess, transferResponse(t, sender).GetSuccess())

			require.NotNil(t, service.downloadCtx, "the task has to get as far as opening the stream")
			require.ErrorIs(t, service.downloadCtx.Err(), context.Canceled)
			require.NoError(t, ctx.Err(), "the stream is released on its own, not by the connection going away")
		})
	}
}
