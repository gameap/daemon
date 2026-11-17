//go:build linux || darwin

package grpc

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/gameap/daemon/internal/app/fsutil"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type knownServerDirs []string

func (d knownServerDirs) ServerDirs() []string {
	return d
}

// tenantWorkDir lays out a node with two game servers. The directory of server
// "a" holds the symlinks its tenant can create from inside the game process:
// into the parent directory, into the work directory, at a neighbour's file,
// and one that stays inside its own directory.
func tenantWorkDir(t *testing.T) string {
	t.Helper()

	workDir := t.TempDir()

	for name, content := range map[string]string{
		"servers/a/own.txt":               "own",
		"servers/a/cstrike/server.cfg":    "hostname a",
		"servers/a/cstrike/maps/de.bsp":   "map",
		"servers/b/victim.txt":            "victim",
		"servers/b/cstrike/server.cfg":    "rcon_password secret",
		"servers/b/doomed/keep.txt":       "keep",
		"steamcmd/steamcmd.sh":            "#!/bin/sh",
		".plugins/files/users.d/srv.yaml": "password: hunter2",
	} {
		p := filepath.Join(workDir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}

	serverDir := filepath.Join(workDir, "servers", "a")

	for name, target := range map[string]string{
		"servers-link": "..",
		"node-link":    "../..",
		"file-link":    "../b/cstrike/server.cfg",
		"users-link":   "../../.plugins/files/users.d",
		"in-link":      "cstrike",
	} {
		require.NoError(t, os.Symlink(target, filepath.Join(serverDir, name)))
	}

	return workDir
}

// snapshotTree records every entry below dir with its mode and content, so a
// test can prove an operation left it alone.
func snapshotTree(t *testing.T, dir string) map[string]string {
	t.Helper()

	snapshot := map[string]string{}

	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}

		entry := info.Mode().String()
		if info.Mode().IsRegular() {
			content, readErr := os.ReadFile(p)
			if readErr != nil {
				return readErr
			}

			entry += " " + string(content)
		}

		snapshot[filepath.ToSlash(rel)] = entry

		return nil
	})
	require.NoError(t, err)

	return snapshot
}

type opResult struct {
	success bool
	err     string
}

func fileOp(
	t *testing.T, h *GRPCFileHandler, root string, op pb.FileOperationType, mutate func(*pb.FileOperationRequest),
) *pb.FileOperationResponse {
	t.Helper()

	req := &pb.FileOperationRequest{RequestId: "op", Operation: op, Root: root}
	mutate(req)

	resp, err := h.HandleFileOperation(context.Background(), req)
	require.NoError(t, err)

	return resp
}

func opOutcome(resp *pb.FileOperationResponse) opResult {
	return opResult{success: resp.GetSuccess(), err: resp.GetError()}
}

// escapingOperations is every way a tenant of server "a" can ask the daemon to
// reach through one of the planted symlinks.
func escapingOperations(t *testing.T) map[string]func(h *GRPCFileHandler, root string) opResult {
	t.Helper()

	ctx := context.Background()

	return map[string]func(h *GRPCFileHandler, root string) opResult{
		"list_neighbours": func(h *GRPCFileHandler, root string) opResult {
			resp, err := h.HandleFileList(ctx, "l", &pb.FileListRequest{Path: "servers/a/servers-link", Root: root})
			require.NoError(t, err)
			assert.Empty(t, resp.GetFiles())

			return opResult{resp.GetSuccess(), resp.GetError()}
		},
		"list_node_recursively": func(h *GRPCFileHandler, root string) opResult {
			resp, err := h.HandleFileList(ctx, "l", &pb.FileListRequest{
				Path: "servers/a/node-link", Recursive: true, Root: root,
			})
			require.NoError(t, err)
			assert.Empty(t, resp.GetFiles())

			return opResult{resp.GetSuccess(), resp.GetError()}
		},
		"list_plugin_files": func(h *GRPCFileHandler, root string) opResult {
			resp, err := h.HandleFileList(ctx, "l", &pb.FileListRequest{Path: "servers/a/users-link", Root: root})
			require.NoError(t, err)
			assert.Empty(t, resp.GetFiles())

			return opResult{resp.GetSuccess(), resp.GetError()}
		},
		"read_neighbour_config": func(h *GRPCFileHandler, root string) opResult {
			resp, err := h.HandleFileRead(ctx, "r", &pb.FileReadRequest{
				Path: "servers/a/servers-link/b/cstrike/server.cfg", Root: root,
			})
			require.NoError(t, err)
			assert.Empty(t, resp.GetContent())

			return opResult{resp.GetSuccess(), resp.GetError()}
		},
		"read_neighbour_config_range": func(h *GRPCFileHandler, root string) opResult {
			resp, err := h.HandleFileRead(ctx, "r", &pb.FileReadRequest{
				Path: "servers/a/servers-link/b/cstrike/server.cfg", Offset: 5, Length: 8, Root: root,
			})
			require.NoError(t, err)
			assert.Empty(t, resp.GetContent())

			return opResult{resp.GetSuccess(), resp.GetError()}
		},
		"read_through_file_link": func(h *GRPCFileHandler, root string) opResult {
			resp, err := h.HandleFileRead(ctx, "r", &pb.FileReadRequest{Path: "servers/a/file-link", Root: root})
			require.NoError(t, err)
			assert.Empty(t, resp.GetContent())

			return opResult{resp.GetSuccess(), resp.GetError()}
		},
		"read_plugin_secret": func(h *GRPCFileHandler, root string) opResult {
			resp, err := h.HandleFileRead(ctx, "r", &pb.FileReadRequest{
				Path: "servers/a/users-link/srv.yaml", Root: root,
			})
			require.NoError(t, err)
			assert.Empty(t, resp.GetContent())

			return opResult{resp.GetSuccess(), resp.GetError()}
		},
		"overwrite_steamcmd": func(h *GRPCFileHandler, root string) opResult {
			resp, err := h.HandleFileWrite(ctx, "w", &pb.FileWriteRequest{
				Path: "servers/a/node-link/steamcmd/steamcmd.sh", Content: []byte("id > /tmp/pwned"), Mode: 0o755,
				Root: root,
			})
			require.NoError(t, err)

			return opResult{resp.GetSuccess(), resp.GetError()}
		},
		"write_into_neighbour_creating_dirs": func(h *GRPCFileHandler, root string) opResult {
			resp, err := h.HandleFileWrite(ctx, "w", &pb.FileWriteRequest{
				Path: "servers/a/servers-link/b/addons/planted.txt", Content: []byte("x"), CreateDirs: true,
				Root: root,
			})
			require.NoError(t, err)

			return opResult{resp.GetSuccess(), resp.GetError()}
		},
		"overwrite_through_file_link": func(h *GRPCFileHandler, root string) opResult {
			resp, err := h.HandleFileWrite(ctx, "w", &pb.FileWriteRequest{
				Path: "servers/a/file-link", Content: []byte("rcon_password owned"), Root: root,
			})
			require.NoError(t, err)

			return opResult{resp.GetSuccess(), resp.GetError()}
		},
		"stat_neighbour_file": func(h *GRPCFileHandler, root string) opResult {
			resp := fileOp(t, h, root, pb.FileOperationType_FILE_OPERATION_TYPE_STAT, func(r *pb.FileOperationRequest) {
				r.Parameters = &pb.FileOperationRequest_StatParams{
					StatParams: &pb.StatParams{Path: "servers/a/servers-link/b/victim.txt"},
				}
			})
			assert.Nil(t, resp.GetStatResult())

			return opOutcome(resp)
		},
		"delete_neighbour_dir": func(h *GRPCFileHandler, root string) opResult {
			return opOutcome(fileOp(
				t, h, root, pb.FileOperationType_FILE_OPERATION_TYPE_DELETE, func(r *pb.FileOperationRequest) {
					r.Parameters = &pb.FileOperationRequest_DeleteParams{
						DeleteParams: &pb.DeleteParams{Path: "servers/a/servers-link/b/doomed", Recursive: true},
					}
				},
			))
		},
		"delete_neighbour_file": func(h *GRPCFileHandler, root string) opResult {
			return opOutcome(fileOp(
				t, h, root, pb.FileOperationType_FILE_OPERATION_TYPE_DELETE, func(r *pb.FileOperationRequest) {
					r.Parameters = &pb.FileOperationRequest_DeleteParams{
						DeleteParams: &pb.DeleteParams{Path: "servers/a/servers-link/b/victim.txt"},
					}
				},
			))
		},
		"steal_neighbour_file": func(h *GRPCFileHandler, root string) opResult {
			return opOutcome(fileOp(
				t, h, root, pb.FileOperationType_FILE_OPERATION_TYPE_MOVE, func(r *pb.FileOperationRequest) {
					r.Parameters = &pb.FileOperationRequest_MoveParams{
						MoveParams: &pb.MoveParams{
							Source:      "servers/a/servers-link/b/victim.txt",
							Destination: "servers/a/stolen.txt",
						},
					}
				},
			))
		},
		"move_own_file_into_neighbour": func(h *GRPCFileHandler, root string) opResult {
			return opOutcome(fileOp(
				t, h, root, pb.FileOperationType_FILE_OPERATION_TYPE_MOVE, func(r *pb.FileOperationRequest) {
					r.Parameters = &pb.FileOperationRequest_MoveParams{
						MoveParams: &pb.MoveParams{
							Source:      "servers/a/own.txt",
							Destination: "servers/a/servers-link/b/planted.txt",
						},
					}
				},
			))
		},
		"copy_neighbour_config_out": func(h *GRPCFileHandler, root string) opResult {
			return opOutcome(fileOp(
				t, h, root, pb.FileOperationType_FILE_OPERATION_TYPE_COPY, func(r *pb.FileOperationRequest) {
					r.Parameters = &pb.FileOperationRequest_CopyParams{
						CopyParams: &pb.CopyParams{
							Source:      "servers/a/servers-link/b/cstrike/server.cfg",
							Destination: "servers/a/leaked.cfg",
						},
					}
				},
			))
		},
		"copy_neighbour_tree_out": func(h *GRPCFileHandler, root string) opResult {
			return opOutcome(fileOp(
				t, h, root, pb.FileOperationType_FILE_OPERATION_TYPE_COPY, func(r *pb.FileOperationRequest) {
					r.Parameters = &pb.FileOperationRequest_CopyParams{
						CopyParams: &pb.CopyParams{
							Source:      "servers/a/servers-link/b",
							Destination: "servers/a/leaked",
						},
					}
				},
			))
		},
		"copy_own_file_into_neighbour": func(h *GRPCFileHandler, root string) opResult {
			return opOutcome(fileOp(
				t, h, root, pb.FileOperationType_FILE_OPERATION_TYPE_COPY, func(r *pb.FileOperationRequest) {
					r.Parameters = &pb.FileOperationRequest_CopyParams{
						CopyParams: &pb.CopyParams{
							Source:      "servers/a/own.txt",
							Destination: "servers/a/servers-link/b/planted.txt",
						},
					}
				},
			))
		},
		"chmod_neighbour_config": func(h *GRPCFileHandler, root string) opResult {
			return opOutcome(fileOp(
				t, h, root, pb.FileOperationType_FILE_OPERATION_TYPE_CHMOD, func(r *pb.FileOperationRequest) {
					r.Parameters = &pb.FileOperationRequest_ChmodParams{
						ChmodParams: &pb.ChmodParams{Path: "servers/a/servers-link/b/cstrike/server.cfg", Mode: 0o777},
					}
				},
			))
		},
		"chmod_through_file_link": func(h *GRPCFileHandler, root string) opResult {
			return opOutcome(fileOp(
				t, h, root, pb.FileOperationType_FILE_OPERATION_TYPE_CHMOD, func(r *pb.FileOperationRequest) {
					r.Parameters = &pb.FileOperationRequest_ChmodParams{
						ChmodParams: &pb.ChmodParams{Path: "servers/a/file-link", Mode: 0o777},
					}
				},
			))
		},
		"chown_neighbour_config": func(h *GRPCFileHandler, root string) opResult {
			return opOutcome(fileOp(
				t, h, root, pb.FileOperationType_FILE_OPERATION_TYPE_CHOWN, func(r *pb.FileOperationRequest) {
					r.Parameters = &pb.FileOperationRequest_ChownParams{
						ChownParams: &pb.ChownParams{
							Path: "servers/a/servers-link/b/cstrike/server.cfg",
							Uid:  int32(os.Getuid()),
							Gid:  int32(os.Getgid()),
						},
					}
				},
			))
		},
		"mkdir_in_neighbour": func(h *GRPCFileHandler, root string) opResult {
			return opOutcome(fileOp(
				t, h, root, pb.FileOperationType_FILE_OPERATION_TYPE_MKDIR, func(r *pb.FileOperationRequest) {
					r.Parameters = &pb.FileOperationRequest_MkdirParams{
						MkdirParams: &pb.MkdirParams{Path: "servers/a/servers-link/b/planted"},
					}
				},
			))
		},
		"mkdir_all_in_neighbour": func(h *GRPCFileHandler, root string) opResult {
			return opOutcome(fileOp(
				t, h, root, pb.FileOperationType_FILE_OPERATION_TYPE_MKDIR, func(r *pb.FileOperationRequest) {
					r.Parameters = &pb.FileOperationRequest_MkdirParams{
						MkdirParams: &pb.MkdirParams{Path: "servers/a/servers-link/b/deep/planted", Recursive: true},
					}
				},
			))
		},
		"touch_new_file_in_neighbour": func(h *GRPCFileHandler, root string) opResult {
			return opOutcome(fileOp(
				t, h, root, pb.FileOperationType_FILE_OPERATION_TYPE_TOUCH, func(r *pb.FileOperationRequest) {
					r.Parameters = &pb.FileOperationRequest_TouchParams{
						TouchParams: &pb.TouchParams{Path: "servers/a/servers-link/b/planted.txt"},
					}
				},
			))
		},
		"touch_neighbour_file": func(h *GRPCFileHandler, root string) opResult {
			return opOutcome(fileOp(
				t, h, root, pb.FileOperationType_FILE_OPERATION_TYPE_TOUCH, func(r *pb.FileOperationRequest) {
					r.Parameters = &pb.FileOperationRequest_TouchParams{
						TouchParams: &pb.TouchParams{Path: "servers/a/servers-link/b/victim.txt"},
					}
				},
			))
		},
	}
}

// confinementModes are the two ways a request ends up confined to server "a":
// the daemon knows the game server directories, or the panel names the root.
func confinementModes() map[string]struct {
	opts []fsutil.ConfineOption
	root string
} {
	return map[string]struct {
		opts []fsutil.ConfineOption
		root string
	}{
		"known_server_dirs": {
			opts: []fsutil.ConfineOption{fsutil.WithServerDirs(knownServerDirs{"servers/a", "servers/b"})},
		},
		"request_root": {
			root: "servers/a",
		},
		"request_root_and_known_server_dirs": {
			opts: []fsutil.ConfineOption{fsutil.WithServerDirs(knownServerDirs{"servers/a", "servers/b"})},
			root: "/servers/a",
		},
	}
}

func TestGRPCFileHandler_SymlinkIntoAnotherServerIsRefused(t *testing.T) {
	t.Parallel()

	for modeName, mode := range confinementModes() {
		for opName, op := range escapingOperations(t) {
			t.Run(modeName+"/"+opName, func(t *testing.T) {
				t.Parallel()

				workDir := tenantWorkDir(t)
				outside := []string{"servers/b", "steamcmd", ".plugins"}

				before := map[string]map[string]string{}
				for _, dir := range outside {
					before[dir] = snapshotTree(t, filepath.Join(workDir, filepath.FromSlash(dir)))
				}

				got := op(NewGRPCFileHandler(workDir, mode.opts...), mode.root)

				assert.False(t, got.success)
				assert.Contains(t, got.err, "path escapes from parent")

				for _, dir := range outside {
					assert.Equal(t, before[dir], snapshotTree(t, filepath.Join(workDir, filepath.FromSlash(dir))), dir)
				}

				for _, name := range []string{"stolen.txt", "leaked.cfg", "leaked"} {
					_, err := os.Lstat(filepath.Join(workDir, "servers", "a", name))
					assert.ErrorIs(t, err, os.ErrNotExist, name)
				}
			})
		}
	}
}

// Before the confinement every one of these went through: the links stay
// inside the work directory, which was the only boundary.
func TestGRPCFileHandler_SymlinkIntoAnotherServerWithoutConfinement(t *testing.T) {
	t.Parallel()

	workDir := tenantWorkDir(t)
	h := NewGRPCFileHandler(workDir)

	resp, err := h.HandleFileRead(context.Background(), "r", &pb.FileReadRequest{
		Path: "servers/a/servers-link/b/cstrike/server.cfg",
	})

	require.NoError(t, err)
	require.True(t, resp.GetSuccess(), resp.GetError())
	assert.Equal(t, "rcon_password secret", string(resp.GetContent()))
}

func TestGRPCFileHandler_ExistsDoesNotProbeThroughSymlink(t *testing.T) {
	t.Parallel()

	for modeName, mode := range confinementModes() {
		t.Run(modeName, func(t *testing.T) {
			t.Parallel()

			h := NewGRPCFileHandler(tenantWorkDir(t), mode.opts...)

			exists := func(p string) bool {
				resp := fileOp(
					t, h, mode.root, pb.FileOperationType_FILE_OPERATION_TYPE_EXISTS, func(r *pb.FileOperationRequest) {
						r.Parameters = &pb.FileOperationRequest_ExistsParams{ExistsParams: &pb.ExistsParams{Path: p}}
					},
				)
				require.True(t, resp.GetSuccess(), resp.GetError())

				return resp.GetExistsResult().GetExists()
			}

			assert.True(t, exists("servers/a/own.txt"))
			assert.True(t, exists("servers/a/in-link/server.cfg"))
			assert.False(t, exists("servers/a/servers-link/b/victim.txt"))
			assert.False(t, exists("servers/a/node-link/steamcmd/steamcmd.sh"))
			assert.False(t, exists("servers/a/file-link"))
		})
	}
}

func TestGRPCFileHandler_HashDoesNotReadThroughSymlink(t *testing.T) {
	t.Parallel()

	for modeName, mode := range confinementModes() {
		t.Run(modeName, func(t *testing.T) {
			t.Parallel()

			h := NewGRPCFileHandler(tenantWorkDir(t), mode.opts...)

			resp := fileOp(t, h, mode.root, pb.FileOperationType_FILE_OPERATION_TYPE_HASH, func(r *pb.FileOperationRequest) {
				r.Parameters = &pb.FileOperationRequest_HashParams{
					HashParams: &pb.HashParams{
						Algorithm: pb.HashAlgorithm_HASH_ALGORITHM_SHA256,
						Paths: []string{
							"servers/a/own.txt",
							"servers/a/servers-link/b/cstrike/server.cfg",
							"servers/a/node-link/steamcmd/steamcmd.sh",
						},
					},
				}
			})

			require.True(t, resp.GetSuccess(), resp.GetError())

			hashes := resp.GetHashResult().GetHashes()
			require.Len(t, hashes, 3)

			assert.Equal(t, "servers/a/own.txt", hashes[0].GetPath())
			assert.Empty(t, hashes[0].GetError())
			assert.Equal(t, sha256Hex([]byte("own")), hashes[0].GetHash())

			for _, refused := range hashes[1:] {
				assert.Contains(t, refused.GetError(), "path escapes from parent", refused.GetPath())
				assert.Empty(t, refused.GetHash(), refused.GetPath())
				assert.Zero(t, refused.GetSize(), refused.GetPath())
			}
		})
	}
}

func TestGRPCFileHandler_OwnFilesKeepWorkingWhenConfined(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	for modeName, mode := range confinementModes() {
		t.Run(modeName, func(t *testing.T) {
			t.Parallel()

			workDir := tenantWorkDir(t)
			h := NewGRPCFileHandler(workDir, mode.opts...)
			serverDir := filepath.Join(workDir, "servers", "a")

			list, err := h.HandleFileList(ctx, "l", &pb.FileListRequest{Path: "servers/a", Root: mode.root})
			require.NoError(t, err)
			require.True(t, list.GetSuccess(), list.GetError())

			listed := map[string]pb.FileType{}
			for _, f := range list.GetFiles() {
				listed[f.GetPath()] = f.GetType()
			}
			assert.Equal(t, map[string]pb.FileType{
				"servers/a/own.txt":      pb.FileType_FILE_TYPE_REGULAR,
				"servers/a/cstrike":      pb.FileType_FILE_TYPE_DIRECTORY,
				"servers/a/servers-link": pb.FileType_FILE_TYPE_SYMLINK,
				"servers/a/node-link":    pb.FileType_FILE_TYPE_SYMLINK,
				"servers/a/file-link":    pb.FileType_FILE_TYPE_SYMLINK,
				"servers/a/users-link":   pb.FileType_FILE_TYPE_SYMLINK,
				"servers/a/in-link":      pb.FileType_FILE_TYPE_SYMLINK,
			}, listed)

			recursive, err := h.HandleFileList(ctx, "l", &pb.FileListRequest{
				Path: "servers/a/cstrike", Recursive: true, Root: mode.root,
			})
			require.NoError(t, err)
			require.True(t, recursive.GetSuccess(), recursive.GetError())

			walked := make([]string, 0, len(recursive.GetFiles()))
			for _, f := range recursive.GetFiles() {
				walked = append(walked, f.GetPath())
			}
			assert.ElementsMatch(t, []string{
				"servers/a/cstrike/server.cfg",
				"servers/a/cstrike/maps",
				"servers/a/cstrike/maps/de.bsp",
			}, walked)

			throughLink, err := h.HandleFileRead(ctx, "r", &pb.FileReadRequest{
				Path: "servers/a/in-link/server.cfg", Root: mode.root,
			})
			require.NoError(t, err)
			require.True(t, throughLink.GetSuccess(), throughLink.GetError())
			assert.Equal(t, "hostname a", string(throughLink.GetContent()))

			written, err := h.HandleFileWrite(ctx, "w", &pb.FileWriteRequest{
				Path: "servers/a/addons/new/plugin.cfg", Content: []byte("cfg"), CreateDirs: true, Root: mode.root,
			})
			require.NoError(t, err)
			require.True(t, written.GetSuccess(), written.GetError())

			content, err := os.ReadFile(filepath.Join(serverDir, "addons", "new", "plugin.cfg"))
			require.NoError(t, err)
			assert.Equal(t, "cfg", string(content))

			moved := fileOp(t, h, mode.root, pb.FileOperationType_FILE_OPERATION_TYPE_MOVE, func(r *pb.FileOperationRequest) {
				r.Parameters = &pb.FileOperationRequest_MoveParams{
					MoveParams: &pb.MoveParams{Source: "servers/a/own.txt", Destination: "servers/a/cstrike/renamed.txt"},
				}
			})
			require.True(t, moved.GetSuccess(), moved.GetError())

			copied := fileOp(t, h, mode.root, pb.FileOperationType_FILE_OPERATION_TYPE_COPY, func(r *pb.FileOperationRequest) {
				r.Parameters = &pb.FileOperationRequest_CopyParams{
					CopyParams: &pb.CopyParams{Source: "servers/a/cstrike", Destination: "servers/a/backup"},
				}
			})
			require.True(t, copied.GetSuccess(), copied.GetError())

			content, err = os.ReadFile(filepath.Join(serverDir, "backup", "renamed.txt"))
			require.NoError(t, err)
			assert.Equal(t, "own", string(content))

			// Removing a planted link removes the link, never what it points at.
			unlinked := fileOp(
				t, h, mode.root, pb.FileOperationType_FILE_OPERATION_TYPE_DELETE, func(r *pb.FileOperationRequest) {
					r.Parameters = &pb.FileOperationRequest_DeleteParams{
						DeleteParams: &pb.DeleteParams{Path: "servers/a/servers-link", Recursive: true},
					}
				},
			)
			require.True(t, unlinked.GetSuccess(), unlinked.GetError())

			_, err = os.Lstat(filepath.Join(serverDir, "servers-link"))
			require.ErrorIs(t, err, os.ErrNotExist)

			content, err = os.ReadFile(filepath.Join(workDir, "servers", "b", "victim.txt"))
			require.NoError(t, err)
			assert.Equal(t, "victim", string(content))

			linkStat := fileOp(
				t, h, mode.root, pb.FileOperationType_FILE_OPERATION_TYPE_STAT, func(r *pb.FileOperationRequest) {
					r.Parameters = &pb.FileOperationRequest_StatParams{
						StatParams: &pb.StatParams{Path: "servers/a/node-link"},
					}
				},
			)
			require.True(t, linkStat.GetSuccess(), linkStat.GetError())
			assert.Equal(t, pb.FileType_FILE_TYPE_SYMLINK, linkStat.GetStatResult().GetStat().GetType())
			assert.Equal(t, "node-link", linkStat.GetStatResult().GetStat().GetName())
		})
	}
}

func TestGRPCFileHandler_StatOfTheServerDirKeepsItsName(t *testing.T) {
	t.Parallel()

	for modeName, mode := range confinementModes() {
		t.Run(modeName, func(t *testing.T) {
			t.Parallel()

			h := NewGRPCFileHandler(tenantWorkDir(t), mode.opts...)

			resp := fileOp(t, h, mode.root, pb.FileOperationType_FILE_OPERATION_TYPE_STAT, func(r *pb.FileOperationRequest) {
				r.Parameters = &pb.FileOperationRequest_StatParams{StatParams: &pb.StatParams{Path: "servers/a"}}
			})

			require.True(t, resp.GetSuccess(), resp.GetError())

			stat := resp.GetStatResult().GetStat()
			assert.Equal(t, "a", stat.GetName())
			assert.Equal(t, "servers/a", stat.GetPath())
			assert.Equal(t, pb.FileType_FILE_TYPE_DIRECTORY, stat.GetType())
		})
	}
}

func TestGRPCFileHandler_PathOutsideTheRequestRoot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workDir := tenantWorkDir(t)
	h := NewGRPCFileHandler(workDir, fsutil.WithServerDirs(knownServerDirs{"servers/a", "servers/b"}))

	const wantError = "path is outside its confinement root"

	read, err := h.HandleFileRead(ctx, "r", &pb.FileReadRequest{Path: "servers/b/victim.txt", Root: "servers/a"})
	require.NoError(t, err)
	assert.False(t, read.GetSuccess())
	assert.Equal(t, wantError, read.GetError())
	assert.Empty(t, read.GetContent())

	list, err := h.HandleFileList(ctx, "l", &pb.FileListRequest{Path: "servers", Root: "servers/a"})
	require.NoError(t, err)
	assert.False(t, list.GetSuccess())
	assert.Equal(t, wantError, list.GetError())
	assert.Empty(t, list.GetFiles())

	write, err := h.HandleFileWrite(ctx, "w", &pb.FileWriteRequest{
		Path: "steamcmd/steamcmd.sh", Content: []byte("x"), Root: "servers/a",
	})
	require.NoError(t, err)
	assert.False(t, write.GetSuccess())
	assert.Equal(t, wantError, write.GetError())

	move := fileOp(t, h, "servers/a", pb.FileOperationType_FILE_OPERATION_TYPE_MOVE, func(r *pb.FileOperationRequest) {
		r.Parameters = &pb.FileOperationRequest_MoveParams{
			MoveParams: &pb.MoveParams{Source: "servers/a/own.txt", Destination: "servers/b/planted.txt"},
		}
	})
	assert.False(t, move.GetSuccess())
	assert.Equal(t, wantError, move.GetError())

	content, err := os.ReadFile(filepath.Join(workDir, "steamcmd", "steamcmd.sh"))
	require.NoError(t, err)
	assert.Equal(t, "#!/bin/sh", string(content))

	_, err = os.Lstat(filepath.Join(workDir, "servers", "b", "planted.txt"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

// A caller acting for the node rather than for one server names no root. It
// may work across game server directories, and each half of what it does is
// still confined to the directory it touches.
func TestGRPCFileHandler_NodeLevelOperationsAcrossServerDirs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	newHandler := func(t *testing.T) (*GRPCFileHandler, string) {
		t.Helper()

		workDir := tenantWorkDir(t)

		return NewGRPCFileHandler(workDir, fsutil.WithServerDirs(knownServerDirs{"servers/a", "servers/b"})), workDir
	}

	t.Run("list_the_directory_holding_the_servers", func(t *testing.T) {
		t.Parallel()

		h, _ := newHandler(t)

		resp, err := h.HandleFileList(ctx, "l", &pb.FileListRequest{Path: "servers"})
		require.NoError(t, err)
		require.True(t, resp.GetSuccess(), resp.GetError())

		names := make([]string, 0, len(resp.GetFiles()))
		for _, f := range resp.GetFiles() {
			names = append(names, f.GetName())
		}
		assert.ElementsMatch(t, []string{"a", "b"}, names)
	})

	t.Run("move_between_servers", func(t *testing.T) {
		t.Parallel()

		h, workDir := newHandler(t)

		resp := fileOp(t, h, "", pb.FileOperationType_FILE_OPERATION_TYPE_MOVE, func(r *pb.FileOperationRequest) {
			r.Parameters = &pb.FileOperationRequest_MoveParams{
				MoveParams: &pb.MoveParams{Source: "servers/b/victim.txt", Destination: "servers/a/cstrike/moved.txt"},
			}
		})
		require.True(t, resp.GetSuccess(), resp.GetError())

		content, err := os.ReadFile(filepath.Join(workDir, "servers", "a", "cstrike", "moved.txt"))
		require.NoError(t, err)
		assert.Equal(t, "victim", string(content))

		_, err = os.Lstat(filepath.Join(workDir, "servers", "b", "victim.txt"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("move_between_servers_does_not_follow_a_planted_link", func(t *testing.T) {
		t.Parallel()

		h, workDir := newHandler(t)

		resp := fileOp(t, h, "", pb.FileOperationType_FILE_OPERATION_TYPE_MOVE, func(r *pb.FileOperationRequest) {
			r.Parameters = &pb.FileOperationRequest_MoveParams{
				MoveParams: &pb.MoveParams{
					Source:      "servers/b/victim.txt",
					Destination: "servers/a/node-link/steamcmd/steamcmd.sh",
				},
			}
		})
		assert.False(t, resp.GetSuccess())
		assert.Contains(t, resp.GetError(), "path escapes from parent")

		content, err := os.ReadFile(filepath.Join(workDir, "steamcmd", "steamcmd.sh"))
		require.NoError(t, err)
		assert.Equal(t, "#!/bin/sh", string(content))
	})

	t.Run("copy_a_server_dir_to_a_new_one", func(t *testing.T) {
		t.Parallel()

		h, workDir := newHandler(t)

		resp := fileOp(t, h, "", pb.FileOperationType_FILE_OPERATION_TYPE_COPY, func(r *pb.FileOperationRequest) {
			r.Parameters = &pb.FileOperationRequest_CopyParams{
				CopyParams: &pb.CopyParams{Source: "servers/a", Destination: "servers/clone"},
			}
		})
		require.True(t, resp.GetSuccess(), resp.GetError())

		content, err := os.ReadFile(filepath.Join(workDir, "servers", "clone", "cstrike", "server.cfg"))
		require.NoError(t, err)
		assert.Equal(t, "hostname a", string(content))

		// The links are copied as links, not followed into what they point at.
		target, err := os.Readlink(filepath.Join(workDir, "servers", "clone", "node-link"))
		require.NoError(t, err)
		assert.Equal(t, "../..", target)
	})

	t.Run("copy_into_a_server_does_not_follow_a_planted_link", func(t *testing.T) {
		t.Parallel()

		h, workDir := newHandler(t)

		resp := fileOp(t, h, "", pb.FileOperationType_FILE_OPERATION_TYPE_COPY, func(r *pb.FileOperationRequest) {
			r.Parameters = &pb.FileOperationRequest_CopyParams{
				CopyParams: &pb.CopyParams{
					Source:      "steamcmd/steamcmd.sh",
					Destination: "servers/a/servers-link/b/cstrike/server.cfg",
				},
			}
		})
		assert.False(t, resp.GetSuccess())
		assert.Contains(t, resp.GetError(), "path escapes from parent")

		content, err := os.ReadFile(filepath.Join(workDir, "servers", "b", "cstrike", "server.cfg"))
		require.NoError(t, err)
		assert.Equal(t, "rcon_password secret", string(content))
	})

	t.Run("delete_a_whole_server_dir", func(t *testing.T) {
		t.Parallel()

		h, workDir := newHandler(t)

		resp := fileOp(t, h, "", pb.FileOperationType_FILE_OPERATION_TYPE_DELETE, func(r *pb.FileOperationRequest) {
			r.Parameters = &pb.FileOperationRequest_DeleteParams{
				DeleteParams: &pb.DeleteParams{Path: "servers/a", Recursive: true},
			}
		})
		require.True(t, resp.GetSuccess(), resp.GetError())

		_, err := os.Lstat(filepath.Join(workDir, "servers", "a"))
		require.ErrorIs(t, err, os.ErrNotExist)

		// Removing the tree removed the links in it, not their targets.
		content, err := os.ReadFile(filepath.Join(workDir, "servers", "b", "victim.txt"))
		require.NoError(t, err)
		assert.Equal(t, "victim", string(content))

		content, err = os.ReadFile(filepath.Join(workDir, "steamcmd", "steamcmd.sh"))
		require.NoError(t, err)
		assert.Equal(t, "#!/bin/sh", string(content))
	})

	t.Run("write_into_a_server_dir_that_is_not_on_disk_yet", func(t *testing.T) {
		t.Parallel()

		workDir := tenantWorkDir(t)
		h := NewGRPCFileHandler(workDir, fsutil.WithServerDirs(knownServerDirs{"servers/a", "servers/fresh"}))

		for _, root := range []string{"", "servers/fresh"} {
			resp, err := h.HandleFileWrite(ctx, "w", &pb.FileWriteRequest{
				Path: "servers/fresh/cfg/server.cfg", Content: []byte("fresh"), CreateDirs: true, Root: root,
			})
			require.NoError(t, err)
			require.True(t, resp.GetSuccess(), resp.GetError())

			content, err := os.ReadFile(filepath.Join(workDir, "servers", "fresh", "cfg", "server.cfg"))
			require.NoError(t, err)
			assert.Equal(t, "fresh", string(content))
		}
	})
}
