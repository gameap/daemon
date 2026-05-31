package grpc

import (
	"context"
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
)

type GRPCFileHandler struct {
	workDir string
}

func NewGRPCFileHandler(workDir string) *GRPCFileHandler {
	return &GRPCFileHandler{
		workDir: workDir,
	}
}

// openRoot opens an os.Root at the work directory. Every path supplied by the
// caller is then resolved through this root, which refuses symlink and ".."
// escapes per path component without TOCTOU races. The root is opened per
// request because workDir is provisioned from the API after construction and
// may not exist yet at startup.
func (h *GRPCFileHandler) openRoot() (*os.Root, error) {
	root, err := os.OpenRoot(h.workDir)
	if err != nil {
		return nil, errors.Wrap(err, "work directory unavailable")
	}

	return root, nil
}

func (h *GRPCFileHandler) HandleFileRead(
	_ context.Context, requestID string, req *pb.FileReadRequest,
) (*pb.FileReadResponse, error) {
	root, err := h.openRoot()
	if err != nil {
		return &pb.FileReadResponse{RequestId: requestID, Success: false, Error: err.Error()}, nil
	}
	defer root.Close()

	rel, err := fsutil.RootRel(req.Path)
	if err != nil {
		return &pb.FileReadResponse{RequestId: requestID, Success: false, Error: err.Error()}, nil
	}

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
	root, err := h.openRoot()
	if err != nil {
		return &pb.FileWriteResponse{RequestId: requestID, Success: false, Error: err.Error()}, nil
	}
	defer root.Close()

	rel, err := fsutil.RootRel(req.Path)
	if err != nil {
		return &pb.FileWriteResponse{RequestId: requestID, Success: false, Error: err.Error()}, nil
	}

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

	mode := os.FileMode(req.Mode)
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
	root, err := h.openRoot()
	if err != nil {
		return &pb.FileListResponse{RequestId: requestID, Success: false, Error: err.Error()}, nil
	}
	defer root.Close()

	rel, err := fsutil.RootRel(req.Path)
	if err != nil {
		return &pb.FileListResponse{RequestId: requestID, Success: false, Error: err.Error()}, nil
	}

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
	var files []*pb.FileStat

	err := fs.WalkDir(root.FS(), rel, func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
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
	_ context.Context, req *pb.FileOperationRequest,
) (*pb.FileOperationResponse, error) {
	rid := req.GetRequestId()

	root, err := h.openRoot()
	if err != nil {
		return fileOpErrResp(rid, err)
	}
	defer root.Close()

	switch req.GetOperation() {
	case pb.FileOperationType_FILE_OPERATION_TYPE_STAT:
		p := req.GetStatParams()
		if p == nil {
			return fileOpErrResp(rid, errors.New("stat_params required"))
		}
		rel, relErr := fsutil.RootRel(p.GetPath())
		if relErr != nil {
			return fileOpErrResp(rid, relErr)
		}
		info, statErr := root.Lstat(rel)
		if statErr != nil {
			return fileOpErrResp(rid, statErr)
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

	case pb.FileOperationType_FILE_OPERATION_TYPE_EXISTS:
		p := req.GetExistsParams()
		if p == nil {
			return fileOpErrResp(rid, errors.New("exists_params required"))
		}
		rel, relErr := fsutil.RootRel(p.GetPath())
		if relErr != nil {
			return fileOpErrResp(rid, relErr)
		}
		_, statErr := root.Stat(rel)
		return &pb.FileOperationResponse{
			RequestId: rid,
			Success:   true,
			Result: &pb.FileOperationResponse_ExistsResult{
				ExistsResult: &pb.ExistsResult{
					Exists: statErr == nil,
				},
			},
		}, nil

	case pb.FileOperationType_FILE_OPERATION_TYPE_DELETE:
		return h.handleDeleteOp(root, rid, req.GetDeleteParams())

	case pb.FileOperationType_FILE_OPERATION_TYPE_MOVE:
		return h.handleMoveOp(root, rid, req.GetMoveParams())

	case pb.FileOperationType_FILE_OPERATION_TYPE_COPY:
		return h.handleCopyOp(root, rid, req.GetCopyParams())

	case pb.FileOperationType_FILE_OPERATION_TYPE_CHMOD:
		p := req.GetChmodParams()
		if p == nil {
			return fileOpErrResp(rid, errors.New("chmod_params required"))
		}
		rel, relErr := fsutil.RootRel(p.GetPath())
		if relErr != nil {
			return fileOpErrResp(rid, relErr)
		}
		if err := root.Chmod(rel, os.FileMode(p.GetMode())); err != nil {
			return fileOpErrResp(rid, err)
		}
		return fileOpOkResp(rid)

	case pb.FileOperationType_FILE_OPERATION_TYPE_CHOWN:
		p := req.GetChownParams()
		if p == nil {
			return fileOpErrResp(rid, errors.New("chown_params required"))
		}
		rel, relErr := fsutil.RootRel(p.GetPath())
		if relErr != nil {
			return fileOpErrResp(rid, relErr)
		}
		if err := root.Chown(rel, int(p.GetUid()), int(p.GetGid())); err != nil {
			return fileOpErrResp(rid, err)
		}
		return fileOpOkResp(rid)

	case pb.FileOperationType_FILE_OPERATION_TYPE_MKDIR:
		return h.handleMkdirOp(root, rid, req.GetMkdirParams())

	case pb.FileOperationType_FILE_OPERATION_TYPE_TOUCH:
		return h.handleTouchOp(root, rid, req.GetTouchParams())

	default:
		return fileOpErrResp(rid, errors.Errorf("unsupported file operation: %s", req.GetOperation()))
	}
}

func (h *GRPCFileHandler) handleDeleteOp(
	root *os.Root, rid string, p *pb.DeleteParams,
) (*pb.FileOperationResponse, error) {
	if p == nil {
		return fileOpErrResp(rid, errors.New("delete_params required"))
	}
	rel, err := fsutil.RootRel(p.GetPath())
	if err != nil {
		return fileOpErrResp(rid, err)
	}
	if p.GetRecursive() {
		err = root.RemoveAll(rel)
	} else {
		err = root.Remove(rel)
	}
	if err != nil {
		return fileOpErrResp(rid, err)
	}
	return fileOpOkResp(rid)
}

func (h *GRPCFileHandler) handleMoveOp(
	root *os.Root, rid string, p *pb.MoveParams,
) (*pb.FileOperationResponse, error) {
	if p == nil {
		return fileOpErrResp(rid, errors.New("move_params required"))
	}
	src, err := fsutil.RootRel(p.GetSource())
	if err != nil {
		return fileOpErrResp(rid, err)
	}
	dst, err := fsutil.RootRel(p.GetDestination())
	if err != nil {
		return fileOpErrResp(rid, err)
	}
	if err := root.Rename(src, dst); err != nil {
		return fileOpErrResp(rid, err)
	}
	return fileOpOkResp(rid)
}

func (h *GRPCFileHandler) handleCopyOp(
	root *os.Root, rid string, p *pb.CopyParams,
) (*pb.FileOperationResponse, error) {
	if p == nil {
		return fileOpErrResp(rid, errors.New("copy_params required"))
	}
	src, err := fsutil.RootRel(p.GetSource())
	if err != nil {
		return fileOpErrResp(rid, err)
	}
	dst, err := fsutil.RootRel(p.GetDestination())
	if err != nil {
		return fileOpErrResp(rid, err)
	}
	if err := fsutil.CopyInRoot(root, src, dst, fsutil.CopyOptions{}); err != nil {
		return fileOpErrResp(rid, err)
	}
	return fileOpOkResp(rid)
}

func (h *GRPCFileHandler) handleMkdirOp(
	root *os.Root, rid string, p *pb.MkdirParams,
) (*pb.FileOperationResponse, error) {
	if p == nil {
		return fileOpErrResp(rid, errors.New("mkdir_params required"))
	}
	rel, err := fsutil.RootRel(p.GetPath())
	if err != nil {
		return fileOpErrResp(rid, err)
	}

	owner := osowner.Options{
		User: p.GetOwnerUser(),
		UID:  p.GetOwnerUid(),
		GID:  p.GetOwnerGid(),
	}

	mode := os.FileMode(p.GetMode())
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

func (h *GRPCFileHandler) handleTouchOp(
	root *os.Root, rid string, p *pb.TouchParams,
) (*pb.FileOperationResponse, error) {
	if p == nil {
		return fileOpErrResp(rid, errors.New("touch_params required"))
	}
	rel, err := fsutil.RootRel(p.GetPath())
	if err != nil {
		return fileOpErrResp(rid, err)
	}
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
