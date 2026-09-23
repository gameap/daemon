package grpc

import (
	"context"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"

	"github.com/gameap/daemon/internal/app/fsutil"
	"github.com/gameap/daemon/internal/app/osowner"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	defaultFileChunkSize = 64 * 1024
	maxFileSize          = 100 * 1024 * 1024

	// maxHashPaths caps one hash request. Each path costs a full file read, and
	// every result is carried in a single response message, so an unbounded
	// list is both a work amplifier and a way to outgrow the gRPC frame limit.
	maxHashPaths = 1000
)

// GRPCFileHandler serves the panel's file operations. Every caller-supplied
// path goes through the resolver, which confines it to the work directory
// (and, when configured, the allowed symlink targets) — see fsutil.Resolver.
type GRPCFileHandler struct {
	resolver *fsutil.Resolver
}

func NewGRPCFileHandler(workDir string, opts ...fsutil.ResolveOption) *GRPCFileHandler {
	return &GRPCFileHandler{
		resolver: fsutil.NewResolver(workDir, opts...),
	}
}

func (h *GRPCFileHandler) HandleFileRead(
	_ context.Context, requestID string, req *pb.FileReadRequest,
) (*pb.FileReadResponse, error) {
	res, err := h.resolver.Resolve(req.Path, fsutil.FollowLeaf)
	if err != nil {
		return &pb.FileReadResponse{RequestId: requestID, Success: false, Error: err.Error()}, nil
	}
	defer res.Close()

	root, rel := res.Root, res.Rel

	info, err := root.Stat(rel)
	if err != nil {
		return &pb.FileReadResponse{RequestId: requestID, Success: false, Error: err.Error()}, nil
	}

	if info.IsDir() {
		return &pb.FileReadResponse{
			RequestId: requestID,
			Success:   false,
			Error:     "path is a directory",
		}, nil
	}

	offset := req.GetOffset()
	length := req.GetLength()

	if offset > 0 || length > 0 {
		file, openErr := root.Open(rel)
		if openErr != nil {
			return &pb.FileReadResponse{RequestId: requestID, Success: false, Error: openErr.Error()}, nil
		}
		defer file.Close()

		if offset > 0 {
			if _, seekErr := file.Seek(offset, io.SeekStart); seekErr != nil {
				return &pb.FileReadResponse{RequestId: requestID, Success: false, Error: seekErr.Error()}, nil
			}
		}

		var reader io.Reader
		if length > 0 {
			reader = io.LimitReader(file, length)
		} else {
			reader = io.LimitReader(file, maxFileSize)
		}

		data, readErr := io.ReadAll(reader)
		if readErr != nil {
			return &pb.FileReadResponse{RequestId: requestID, Success: false, Error: readErr.Error()}, nil
		}

		return &pb.FileReadResponse{RequestId: requestID, Success: true, Content: data}, nil
	}

	if info.Size() > maxFileSize {
		return &pb.FileReadResponse{
			RequestId: requestID,
			Success:   false,
			Error:     "file too large",
		}, nil
	}

	data, err := root.ReadFile(rel)
	if err != nil {
		return &pb.FileReadResponse{RequestId: requestID, Success: false, Error: err.Error()}, nil
	}

	return &pb.FileReadResponse{RequestId: requestID, Success: true, Content: data}, nil
}

func (h *GRPCFileHandler) HandleFileWrite(
	_ context.Context, requestID string, req *pb.FileWriteRequest,
) (*pb.FileWriteResponse, error) {
	res, err := h.resolver.Resolve(req.Path, fsutil.FollowLeaf)
	if err != nil {
		return &pb.FileWriteResponse{RequestId: requestID, Success: false, Error: err.Error()}, nil
	}
	defer res.Close()

	root, rel := res.Root, res.Rel

	owner := osowner.Options{
		User: req.OwnerUser,
		UID:  req.OwnerUid,
		GID:  req.OwnerGid,
	}

	if req.CreateDirs {
		dir := path.Dir(rel)
		newDirs, segErr := osowner.MissingSegmentsInRoot(root, dir)
		if segErr != nil {
			return &pb.FileWriteResponse{
				RequestId: requestID,
				Success:   false,
				Error:     errors.Wrap(segErr, "failed to inspect target directory").Error(),
			}, nil
		}
		if err = root.MkdirAll(dir, 0755); err != nil {
			return &pb.FileWriteResponse{
				RequestId: requestID,
				Success:   false,
				Error:     errors.Wrap(err, "failed to create directory").Error(),
			}, nil
		}
		for _, segment := range newDirs {
			if chErr := osowner.ApplyToPathInRoot(root, segment, owner); chErr != nil {
				return &pb.FileWriteResponse{
					RequestId: requestID,
					Success:   false,
					Error:     errors.Wrap(chErr, "failed to chown new parent directory").Error(),
				}, nil
			}
		}
	}

	mode := permMode(req.Mode)
	if mode == 0 {
		mode = 0644
	}

	if err := root.WriteFile(rel, req.Content, mode); err != nil {
		return &pb.FileWriteResponse{RequestId: requestID, Success: false, Error: err.Error()}, nil
	}

	if chErr := osowner.ApplyToPathInRoot(root, rel, owner); chErr != nil {
		return &pb.FileWriteResponse{
			RequestId: requestID,
			Success:   false,
			Error:     errors.Wrap(chErr, "failed to chown written file").Error(),
		}, nil
	}

	return &pb.FileWriteResponse{RequestId: requestID, Success: true}, nil
}

func (h *GRPCFileHandler) HandleFileList(
	_ context.Context, requestID string, req *pb.FileListRequest,
) (*pb.FileListResponse, error) {
	res, err := h.resolver.Resolve(req.Path, fsutil.FollowLeaf)
	if err != nil {
		return &pb.FileListResponse{RequestId: requestID, Success: false, Error: err.Error()}, nil
	}
	defer res.Close()

	root, rel := res.Root, res.Rel

	var files []*pb.FileStat

	if req.Recursive {
		files, err = listRecursive(root, rel, req.Path, req.Pattern)
	} else {
		files, err = listFlat(root, rel, req.Path, req.Pattern)
	}

	if err != nil {
		return &pb.FileListResponse{RequestId: requestID, Success: false, Error: err.Error()}, nil
	}

	return &pb.FileListResponse{RequestId: requestID, Success: true, Files: files}, nil
}

func listFlat(root *os.Root, rel, requestPath, pattern string) ([]*pb.FileStat, error) {
	entries, err := fs.ReadDir(root.FS(), rel)
	if err != nil {
		return nil, err
	}

	files := make([]*pb.FileStat, 0, len(entries))
	for _, entry := range entries {
		if pattern != "" {
			if matched, _ := path.Match(pattern, entry.Name()); !matched {
				continue
			}
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		files = append(files, fileInfoToStat(path.Join(requestPath, entry.Name()), info))
	}

	return files, nil
}

func listRecursive(root *os.Root, rel, requestPath, pattern string) ([]*pb.FileStat, error) {
	// fs.WalkDir reports a missing start directory through the callback, which
	// swallows it below along with unreadable entries, so a non-existent path
	// would answer with an empty success. Stat first to fail explicitly.
	rootInfo, err := fs.Stat(root.FS(), rel)
	if err != nil {
		return nil, err
	}
	if !rootInfo.IsDir() {
		// A file start path walks a single entry that relUnder drops, which would
		// also answer with an empty success. The flat listing fails here, so match it.
		return nil, errors.Errorf("path %q is not a directory", requestPath)
	}

	var files []*pb.FileStat

	err = fs.WalkDir(root.FS(), rel, func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if name == rel {
				return walkErr
			}

			return nil //nolint:nilerr // skip unreadable entries
		}

		sub, ok := relUnder(rel, name)
		if !ok {
			return nil
		}

		if pattern != "" {
			if matched, _ := path.Match(pattern, d.Name()); !matched {
				if !d.IsDir() {
					return nil
				}
			}
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}

		files = append(files, fileInfoToStat(path.Join(requestPath, sub), info))

		return nil
	})

	return files, err
}

// relUnder returns name expressed relative to base, or ok=false when name is
// base itself (the walk start, which is not a listed entry). os.Root + the
// io/fs walk are inherently confined, so no extra containment check is needed.
func relUnder(base, name string) (string, bool) {
	if name == base {
		return "", false
	}
	if base == "." {
		return name, true
	}

	prefix := base + "/"
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}

	return name[len(prefix):], true
}

// permMode keeps only the permission bits of a caller-supplied mode. Go maps
// os.ModeSetuid/Setgid/Sticky onto the real S_ISUID/S_ISGID/S_ISVTX bits, and
// umask does not strip them, so an unmasked mode would let an API caller ask a
// root daemon to create a setuid file inside a game-server directory.
func permMode(mode int32) os.FileMode {
	return os.FileMode(mode).Perm() //nolint:gosec // masked to 0777 by Perm
}

func fileOpErrResp(requestID string, err error) (*pb.FileOperationResponse, error) {
	return &pb.FileOperationResponse{
		RequestId: requestID,
		Success:   false,
		Error:     err.Error(),
	}, nil
}

func fileOpOkResp(requestID string) (*pb.FileOperationResponse, error) {
	return &pb.FileOperationResponse{
		RequestId: requestID,
		Success:   true,
	}, nil
}

func (h *GRPCFileHandler) HandleFileOperation(
	ctx context.Context, req *pb.FileOperationRequest,
) (*pb.FileOperationResponse, error) {
	rid := req.GetRequestId()

	// Each operation resolves its own path with the leaf mode of the os.Root
	// primitive it ends in: operations that act on a symlink itself (Lstat,
	// Remove, Rename, the copy's Lstat) must not have the link expanded.
	switch req.GetOperation() {
	case pb.FileOperationType_FILE_OPERATION_TYPE_STAT:
		return h.handleStatOp(rid, req.GetStatParams())

	case pb.FileOperationType_FILE_OPERATION_TYPE_EXISTS:
		return h.handleExistsOp(rid, req.GetExistsParams())

	case pb.FileOperationType_FILE_OPERATION_TYPE_DELETE:
		return h.handleDeleteOp(rid, req.GetDeleteParams())

	case pb.FileOperationType_FILE_OPERATION_TYPE_MOVE:
		return h.handleMoveOp(rid, req.GetMoveParams())

	case pb.FileOperationType_FILE_OPERATION_TYPE_COPY:
		return h.handleCopyOp(rid, req.GetCopyParams())

	case pb.FileOperationType_FILE_OPERATION_TYPE_CHMOD:
		return h.handleChmodOp(rid, req.GetChmodParams())

	case pb.FileOperationType_FILE_OPERATION_TYPE_CHOWN:
		return h.handleChownOp(rid, req.GetChownParams())

	case pb.FileOperationType_FILE_OPERATION_TYPE_MKDIR:
		return h.handleMkdirOp(rid, req.GetMkdirParams())

	case pb.FileOperationType_FILE_OPERATION_TYPE_TOUCH:
		return h.handleTouchOp(rid, req.GetTouchParams())

	case pb.FileOperationType_FILE_OPERATION_TYPE_HASH:
		return h.handleHashOp(ctx, rid, req.GetHashParams())

	default:
		return fileOpErrResp(rid, errors.Errorf("unsupported file operation: %s", req.GetOperation()))
	}
}

func (h *GRPCFileHandler) handleStatOp(rid string, p *pb.StatParams) (*pb.FileOperationResponse, error) {
	if p == nil {
		return fileOpErrResp(rid, errors.New("stat_params required"))
	}
	res, err := h.resolver.Resolve(p.GetPath(), fsutil.NoFollowLeaf)
	if err != nil {
		return fileOpErrResp(rid, err)
	}
	defer res.Close()

	info, err := res.Root.Lstat(res.Rel)
	if err != nil {
		return fileOpErrResp(rid, err)
	}
	return &pb.FileOperationResponse{
		RequestId: rid,
		Success:   true,
		Result: &pb.FileOperationResponse_StatResult{
			StatResult: &pb.StatResult{
				Stat: fileInfoToStat(p.GetPath(), info),
			},
		},
	}, nil
}

func (h *GRPCFileHandler) handleExistsOp(rid string, p *pb.ExistsParams) (*pb.FileOperationResponse, error) {
	if p == nil {
		return fileOpErrResp(rid, errors.New("exists_params required"))
	}

	exists := false

	res, err := h.resolver.Resolve(p.GetPath(), fsutil.FollowLeaf)
	switch {
	case err == nil:
		_, statErr := res.Root.Stat(res.Rel)
		res.Close()
		exists = statErr == nil
	case errors.As(err, new(*fsutil.SymlinkRefusedError)):
		// A link the daemon will not follow has nothing behind it as far as
		// the caller is concerned — the answer os.Root gave for it before.
	default:
		return fileOpErrResp(rid, err)
	}

	return &pb.FileOperationResponse{
		RequestId: rid,
		Success:   true,
		Result: &pb.FileOperationResponse_ExistsResult{
			ExistsResult: &pb.ExistsResult{
				Exists: exists,
			},
		},
	}, nil
}

func (h *GRPCFileHandler) handleChmodOp(rid string, p *pb.ChmodParams) (*pb.FileOperationResponse, error) {
	if p == nil {
		return fileOpErrResp(rid, errors.New("chmod_params required"))
	}
	res, err := h.resolver.Resolve(p.GetPath(), fsutil.FollowLeaf)
	if err != nil {
		return fileOpErrResp(rid, err)
	}
	defer res.Close()

	if err := res.Root.Chmod(res.Rel, permMode(p.GetMode())); err != nil {
		return fileOpErrResp(rid, err)
	}
	return fileOpOkResp(rid)
}

func (h *GRPCFileHandler) handleChownOp(rid string, p *pb.ChownParams) (*pb.FileOperationResponse, error) {
	if p == nil {
		return fileOpErrResp(rid, errors.New("chown_params required"))
	}
	res, err := h.resolver.Resolve(p.GetPath(), fsutil.FollowLeaf)
	if err != nil {
		return fileOpErrResp(rid, err)
	}
	defer res.Close()

	if err := res.Root.Chown(res.Rel, int(p.GetUid()), int(p.GetGid())); err != nil {
		return fileOpErrResp(rid, err)
	}
	return fileOpOkResp(rid)
}

func (h *GRPCFileHandler) handleDeleteOp(rid string, p *pb.DeleteParams) (*pb.FileOperationResponse, error) {
	if p == nil {
		return fileOpErrResp(rid, errors.New("delete_params required"))
	}
	res, err := h.resolver.Resolve(p.GetPath(), fsutil.NoFollowLeaf)
	if err != nil {
		return fileOpErrResp(rid, err)
	}
	defer res.Close()

	if p.GetRecursive() {
		err = res.Root.RemoveAll(res.Rel)
	} else {
		err = res.Root.Remove(res.Rel)
	}
	if err != nil {
		return fileOpErrResp(rid, err)
	}
	return fileOpOkResp(rid)
}

func (h *GRPCFileHandler) handleMoveOp(rid string, p *pb.MoveParams) (*pb.FileOperationResponse, error) {
	if p == nil {
		return fileOpErrResp(rid, errors.New("move_params required"))
	}
	src, err := h.resolver.Resolve(p.GetSource(), fsutil.NoFollowLeaf)
	if err != nil {
		return fileOpErrResp(rid, err)
	}
	defer src.Close()

	dst, err := h.resolver.Resolve(p.GetDestination(), fsutil.NoFollowLeaf)
	if err != nil {
		return fileOpErrResp(rid, err)
	}
	defer dst.Close()

	// os.Root.Rename works within one root; both paths are relative to the
	// same directory when the anchors match.
	if !src.SameAnchor(dst) {
		return fileOpErrResp(rid, errors.Errorf(
			"cannot move %q to %q: the paths are under different storage roots", p.GetSource(), p.GetDestination(),
		))
	}

	if err := src.Root.Rename(src.Rel, dst.Rel); err != nil {
		return fileOpErrResp(rid, err)
	}
	return fileOpOkResp(rid)
}

func (h *GRPCFileHandler) handleCopyOp(rid string, p *pb.CopyParams) (*pb.FileOperationResponse, error) {
	if p == nil {
		return fileOpErrResp(rid, errors.New("copy_params required"))
	}
	src, err := h.resolver.Resolve(p.GetSource(), fsutil.NoFollowLeaf)
	if err != nil {
		return fileOpErrResp(rid, err)
	}
	defer src.Close()

	dst, err := h.resolver.Resolve(p.GetDestination(), fsutil.NoFollowLeaf)
	if err != nil {
		return fileOpErrResp(rid, err)
	}
	defer dst.Close()

	if err := fsutil.CopyTree(src.Root, src.Rel, dst.Root, dst.Rel, fsutil.CopyOptions{}); err != nil {
		return fileOpErrResp(rid, err)
	}
	return fileOpOkResp(rid)
}

func (h *GRPCFileHandler) handleMkdirOp(rid string, p *pb.MkdirParams) (*pb.FileOperationResponse, error) {
	if p == nil {
		return fileOpErrResp(rid, errors.New("mkdir_params required"))
	}

	// mkdir never follows a final symlink, and MkdirAll on one stats it
	// without ever creating the target; a directory that already exists
	// behind an allowed link is therefore checked for first.
	if p.GetRecursive() && h.dirExistsBehindLink(p.GetPath()) {
		return fileOpOkResp(rid)
	}

	res, err := h.resolver.Resolve(p.GetPath(), fsutil.NoFollowLeaf)
	if err != nil {
		return fileOpErrResp(rid, err)
	}
	defer res.Close()

	root, rel := res.Root, res.Rel

	owner := osowner.Options{
		User: p.GetOwnerUser(),
		UID:  p.GetOwnerUid(),
		GID:  p.GetOwnerGid(),
	}

	mode := permMode(p.GetMode())
	if mode == 0 {
		mode = 0755
	}

	var newDirs []string
	if p.GetRecursive() {
		newDirs, err = osowner.MissingSegmentsInRoot(root, rel)
		if err != nil {
			return fileOpErrResp(rid, errors.Wrap(err, "failed to inspect target directory"))
		}
		err = root.MkdirAll(rel, mode)
	} else {
		if _, statErr := root.Lstat(rel); statErr == nil {
			newDirs = nil
		} else if errors.Is(statErr, os.ErrNotExist) {
			newDirs = []string{rel}
		} else {
			return fileOpErrResp(rid, errors.Wrap(statErr, "failed to inspect target directory"))
		}
		err = root.Mkdir(rel, mode)
	}
	if err != nil {
		return fileOpErrResp(rid, err)
	}

	for _, segment := range newDirs {
		if chErr := osowner.ApplyToPathInRoot(root, segment, owner); chErr != nil {
			return fileOpErrResp(rid, errors.Wrap(chErr, "failed to chown new directory"))
		}
	}

	return fileOpOkResp(rid)
}

// dirExistsBehindLink reports whether p, with a final symlink followed, is an
// existing directory.
func (h *GRPCFileHandler) dirExistsBehindLink(p string) bool {
	res, err := h.resolver.Resolve(p, fsutil.FollowLeaf)
	if err != nil {
		return false
	}
	defer res.Close()

	info, err := res.Root.Stat(res.Rel)

	return err == nil && info.IsDir()
}

func (h *GRPCFileHandler) handleTouchOp(rid string, p *pb.TouchParams) (*pb.FileOperationResponse, error) {
	if p == nil {
		return fileOpErrResp(rid, errors.New("touch_params required"))
	}
	res, err := h.resolver.Resolve(p.GetPath(), fsutil.FollowLeaf)
	if err != nil {
		return fileOpErrResp(rid, err)
	}
	defer res.Close()

	root, rel := res.Root, res.Rel
	if _, statErr := root.Stat(rel); os.IsNotExist(statErr) {
		f, createErr := root.Create(rel)
		if createErr != nil {
			return fileOpErrResp(rid, createErr)
		}
		f.Close()
	} else if statErr != nil {
		return fileOpErrResp(rid, statErr)
	} else {
		now := time.Now()
		if err := root.Chtimes(rel, now, now); err != nil {
			return fileOpErrResp(rid, err)
		}
	}
	return fileOpOkResp(rid)
}

func (h *GRPCFileHandler) handleHashOp(
	ctx context.Context, rid string, p *pb.HashParams,
) (*pb.FileOperationResponse, error) {
	if p == nil {
		return fileOpErrResp(rid, errors.New("hash_params required"))
	}
	if _, err := hasherForAlgorithm(p.GetAlgorithm()); err != nil {
		return fileOpErrResp(rid, err)
	}
	if len(p.GetPaths()) > maxHashPaths {
		return fileOpErrResp(rid, errors.Errorf(
			"too many paths to hash: %d, limit is %d", len(p.GetPaths()), maxHashPaths,
		))
	}

	hashes := make([]*pb.FileHash, 0, len(p.GetPaths()))
	for _, pth := range p.GetPaths() {
		if err := ctx.Err(); err != nil {
			return fileOpErrResp(rid, errors.Wrap(err, "hash operation canceled"))
		}

		hashes = append(hashes, h.hashFile(ctx, pth, p.GetAlgorithm()))
	}

	return &pb.FileOperationResponse{
		RequestId: rid,
		Success:   true,
		Result: &pb.FileOperationResponse_HashResult{
			HashResult: &pb.HashResult{
				Algorithm: p.GetAlgorithm(),
				Hashes:    hashes,
			},
		},
	}, nil
}

// hashFile hashes a single file. Any failure is reported in the returned
// FileHash.Error; per-file failures must not fail the operation.
func (h *GRPCFileHandler) hashFile(ctx context.Context, path string, algorithm pb.HashAlgorithm) *pb.FileHash {
	fh := &pb.FileHash{Path: path}

	res, err := h.resolver.Resolve(path, fsutil.NoFollowLeaf)
	if err != nil {
		fh.Error = err.Error()
		return fh
	}
	defer res.Close()

	root, rel := res.Root, res.Rel

	info, err := root.Lstat(rel)
	if err != nil {
		fh.Error = err.Error()
		return fh
	}

	if info.IsDir() {
		fh.Error = "is a directory"
		return fh
	}

	if !info.Mode().IsRegular() {
		fh.Error = "not a regular file"
		return fh
	}

	hasher, err := hasherForAlgorithm(algorithm)
	if err != nil {
		fh.Error = err.Error()
		return fh
	}

	f, err := root.Open(rel)
	if err != nil {
		fh.Error = err.Error()
		return fh
	}
	defer f.Close()

	n, err := io.Copy(hasher, &ctxReader{ctx: ctx, r: f})
	if err != nil {
		fh.Error = err.Error()
		return fh
	}

	fh.Hash = hex.EncodeToString(hasher.Sum(nil))
	fh.Size = uint64(n)

	return fh
}

// ctxReader aborts a streaming read when ctx is done. Hashing a multi-gigabyte
// file otherwise runs to completion no matter what happens to the connection.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *ctxReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, errors.Wrap(err, "read canceled")
	}

	return r.r.Read(p)
}

func fileInfoToStat(path string, info os.FileInfo) *pb.FileStat {
	ft := pb.FileType_FILE_TYPE_REGULAR
	switch {
	case info.IsDir():
		ft = pb.FileType_FILE_TYPE_DIRECTORY
	case info.Mode()&os.ModeSymlink != 0:
		ft = pb.FileType_FILE_TYPE_SYMLINK
	case info.Mode()&os.ModeSocket != 0:
		ft = pb.FileType_FILE_TYPE_SOCKET
	case info.Mode()&os.ModeNamedPipe != 0:
		ft = pb.FileType_FILE_TYPE_FIFO
	case info.Mode()&os.ModeDevice != 0 && info.Mode()&os.ModeCharDevice == 0:
		ft = pb.FileType_FILE_TYPE_BLOCK_DEVICE
	case info.Mode()&os.ModeCharDevice != 0:
		ft = pb.FileType_FILE_TYPE_CHAR_DEVICE
	}

	return &pb.FileStat{
		Name:       info.Name(),
		Path:       path,
		Size:       uint64(info.Size()),
		Mode:       uint32(info.Mode().Perm()),
		ModifiedAt: timestamppb.New(info.ModTime()),
		Type:       ft,
	}
}
