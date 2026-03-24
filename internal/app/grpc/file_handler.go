package grpc

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	cp "github.com/otiai10/copy"

	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	defaultFileChunkSize = 64 * 1024
	maxFileSize          = 100 * 1024 * 1024
)

var errPathOutsideWorkDir = errors.New("path is outside work directory")

type GRPCFileHandler struct {
	workDir string
}

func NewGRPCFileHandler(workDir string) *GRPCFileHandler {
	return &GRPCFileHandler{
		workDir: workDir,
	}
}

func (h *GRPCFileHandler) HandleFileRead(
	_ context.Context, requestID string, req *pb.FileReadRequest,
) (*pb.FileReadResponse, error) {
	filePath, err := h.resolvePath(req.Path)
	if err != nil {
		return &pb.FileReadResponse{
			RequestId: requestID,
			Success:   false,
			Error:     err.Error(),
		}, nil
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return &pb.FileReadResponse{
			RequestId: requestID,
			Success:   false,
			Error:     err.Error(),
		}, nil
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
		file, openErr := os.Open(filePath)
		if openErr != nil {
			return &pb.FileReadResponse{
				RequestId: requestID,
				Success:   false,
				Error:     openErr.Error(),
			}, nil
		}
		defer file.Close()

		if offset > 0 {
			if _, seekErr := file.Seek(offset, io.SeekStart); seekErr != nil {
				return &pb.FileReadResponse{
					RequestId: requestID,
					Success:   false,
					Error:     seekErr.Error(),
				}, nil
			}
		}

		var reader io.Reader = file
		if length > 0 {
			reader = io.LimitReader(file, length)
		} else {
			reader = io.LimitReader(file, maxFileSize)
		}

		data, readErr := io.ReadAll(reader)
		if readErr != nil {
			return &pb.FileReadResponse{
				RequestId: requestID,
				Success:   false,
				Error:     readErr.Error(),
			}, nil
		}

		return &pb.FileReadResponse{
			RequestId: requestID,
			Success:   true,
			Content:   data,
		}, nil
	}

	if info.Size() > maxFileSize {
		return &pb.FileReadResponse{
			RequestId: requestID,
			Success:   false,
			Error:     "file too large",
		}, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return &pb.FileReadResponse{
			RequestId: requestID,
			Success:   false,
			Error:     err.Error(),
		}, nil
	}

	return &pb.FileReadResponse{
		RequestId: requestID,
		Success:   true,
		Content:   data,
	}, nil
}

func (h *GRPCFileHandler) HandleFileWrite(
	_ context.Context, requestID string, req *pb.FileWriteRequest,
) (*pb.FileWriteResponse, error) {
	filePath, err := h.resolvePath(req.Path)
	if err != nil {
		return &pb.FileWriteResponse{
			RequestId: requestID,
			Success:   false,
			Error:     err.Error(),
		}, nil
	}

	if req.CreateDirs {
		dir := filepath.Dir(filePath)
		if err = os.MkdirAll(dir, 0755); err != nil {
			return &pb.FileWriteResponse{
				RequestId: requestID,
				Success:   false,
				Error:     errors.Wrap(err, "failed to create directory").Error(),
			}, nil
		}
	}

	mode := os.FileMode(req.Mode)
	if mode == 0 {
		mode = 0644
	}

	if err := os.WriteFile(filePath, req.Content, mode); err != nil {
		return &pb.FileWriteResponse{
			RequestId: requestID,
			Success:   false,
			Error:     err.Error(),
		}, nil
	}

	return &pb.FileWriteResponse{
		RequestId: requestID,
		Success:   true,
	}, nil
}

func (h *GRPCFileHandler) HandleFileList(
	_ context.Context, requestID string, req *pb.FileListRequest,
) (*pb.FileListResponse, error) {
	dirPath, err := h.resolvePath(req.Path)
	if err != nil {
		return &pb.FileListResponse{
			RequestId: requestID,
			Success:   false,
			Error:     err.Error(),
		}, nil
	}

	var files []*pb.FileStat

	if req.Recursive {
		files, err = h.listRecursive(dirPath, req.Path, req.Pattern)
	} else {
		files, err = h.listFlat(dirPath, req.Path, req.Pattern)
	}

	if err != nil {
		return &pb.FileListResponse{
			RequestId: requestID,
			Success:   false,
			Error:     err.Error(),
		}, nil
	}

	return &pb.FileListResponse{
		RequestId: requestID,
		Success:   true,
		Files:     files,
	}, nil
}

func (h *GRPCFileHandler) listFlat(
	dirPath, requestPath, pattern string,
) ([]*pb.FileStat, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	files := make([]*pb.FileStat, 0, len(entries))
	for _, entry := range entries {
		if pattern != "" {
			if matched, _ := filepath.Match(pattern, entry.Name()); !matched {
				continue
			}
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		files = append(files, fileInfoToStat(filepath.Join(requestPath, entry.Name()), info))
	}

	return files, nil
}

func (h *GRPCFileHandler) listRecursive(
	dirPath, requestPath, pattern string,
) ([]*pb.FileStat, error) {
	var files []*pb.FileStat

	err := filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip unreadable entries
		}

		if path == dirPath {
			return nil
		}

		relPath, relErr := filepath.Rel(dirPath, path)
		if relErr != nil {
			return nil
		}

		if !strings.HasPrefix(filepath.Clean(path), dirPath) {
			return filepath.SkipDir
		}

		if pattern != "" {
			if matched, _ := filepath.Match(pattern, d.Name()); !matched {
				if !d.IsDir() {
					return nil
				}
			}
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}

		files = append(files, fileInfoToStat(filepath.Join(requestPath, relPath), info))

		return nil
	})

	return files, err
}

func (h *GRPCFileHandler) HandleFileOperation(
	_ context.Context, req *pb.FileOperationRequest,
) (*pb.FileOperationResponse, error) {
	errResp := func(err error) (*pb.FileOperationResponse, error) {
		return &pb.FileOperationResponse{
			RequestId: req.GetRequestId(),
			Success:   false,
			Error:     err.Error(),
		}, nil
	}

	okResp := func() (*pb.FileOperationResponse, error) {
		return &pb.FileOperationResponse{
			RequestId: req.GetRequestId(),
			Success:   true,
		}, nil
	}

	switch req.GetOperation() {
	case pb.FileOperationType_FILE_OPERATION_TYPE_STAT:
		p := req.GetStatParams()
		if p == nil {
			return errResp(errors.New("stat_params required"))
		}
		resolved, err := h.resolvePath(p.GetPath())
		if err != nil {
			return errResp(err)
		}
		info, err := os.Lstat(resolved)
		if err != nil {
			return errResp(err)
		}
		return &pb.FileOperationResponse{
			RequestId: req.GetRequestId(),
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
			return errResp(errors.New("exists_params required"))
		}
		resolved, err := h.resolvePath(p.GetPath())
		if err != nil {
			return errResp(err)
		}
		_, err = os.Stat(resolved)
		return &pb.FileOperationResponse{
			RequestId: req.GetRequestId(),
			Success:   true,
			Result: &pb.FileOperationResponse_ExistsResult{
				ExistsResult: &pb.ExistsResult{
					Exists: err == nil,
				},
			},
		}, nil

	case pb.FileOperationType_FILE_OPERATION_TYPE_DELETE:
		p := req.GetDeleteParams()
		if p == nil {
			return errResp(errors.New("delete_params required"))
		}
		resolved, err := h.resolvePath(p.GetPath())
		if err != nil {
			return errResp(err)
		}
		if p.GetRecursive() {
			err = os.RemoveAll(resolved)
		} else {
			err = os.Remove(resolved)
		}
		if err != nil {
			return errResp(err)
		}
		return okResp()

	case pb.FileOperationType_FILE_OPERATION_TYPE_MOVE:
		p := req.GetMoveParams()
		if p == nil {
			return errResp(errors.New("move_params required"))
		}
		src, err := h.resolvePath(p.GetSource())
		if err != nil {
			return errResp(err)
		}
		dst, err := h.resolvePath(p.GetDestination())
		if err != nil {
			return errResp(err)
		}
		if err := os.Rename(src, dst); err != nil {
			return errResp(err)
		}
		return okResp()

	case pb.FileOperationType_FILE_OPERATION_TYPE_COPY:
		p := req.GetCopyParams()
		if p == nil {
			return errResp(errors.New("copy_params required"))
		}
		src, err := h.resolvePath(p.GetSource())
		if err != nil {
			return errResp(err)
		}
		dst, err := h.resolvePath(p.GetDestination())
		if err != nil {
			return errResp(err)
		}
		if err := cp.Copy(src, dst); err != nil {
			return errResp(err)
		}
		return okResp()

	case pb.FileOperationType_FILE_OPERATION_TYPE_CHMOD:
		p := req.GetChmodParams()
		if p == nil {
			return errResp(errors.New("chmod_params required"))
		}
		resolved, err := h.resolvePath(p.GetPath())
		if err != nil {
			return errResp(err)
		}
		if err := os.Chmod(resolved, os.FileMode(p.GetMode())); err != nil {
			return errResp(err)
		}
		return okResp()

	case pb.FileOperationType_FILE_OPERATION_TYPE_CHOWN:
		p := req.GetChownParams()
		if p == nil {
			return errResp(errors.New("chown_params required"))
		}
		resolved, err := h.resolvePath(p.GetPath())
		if err != nil {
			return errResp(err)
		}
		if err := os.Chown(resolved, int(p.GetUid()), int(p.GetGid())); err != nil {
			return errResp(err)
		}
		return okResp()

	case pb.FileOperationType_FILE_OPERATION_TYPE_MKDIR:
		p := req.GetMkdirParams()
		if p == nil {
			return errResp(errors.New("mkdir_params required"))
		}
		resolved, err := h.resolvePath(p.GetPath())
		if err != nil {
			return errResp(err)
		}
		mode := os.FileMode(p.GetMode())
		if mode == 0 {
			mode = 0755
		}
		if p.GetRecursive() {
			err = os.MkdirAll(resolved, mode)
		} else {
			err = os.Mkdir(resolved, mode)
		}
		if err != nil {
			return errResp(err)
		}
		return okResp()

	case pb.FileOperationType_FILE_OPERATION_TYPE_TOUCH:
		p := req.GetTouchParams()
		if p == nil {
			return errResp(errors.New("touch_params required"))
		}
		resolved, err := h.resolvePath(p.GetPath())
		if err != nil {
			return errResp(err)
		}
		if _, err := os.Stat(resolved); os.IsNotExist(err) {
			f, createErr := os.Create(resolved)
			if createErr != nil {
				return errResp(createErr)
			}
			f.Close()
		} else if err != nil {
			return errResp(err)
		} else {
			now := time.Now()
			if err := os.Chtimes(resolved, now, now); err != nil {
				return errResp(err)
			}
		}
		return okResp()

	default:
		return errResp(errors.Errorf("unsupported file operation: %s", req.GetOperation()))
	}
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

// ResolvePath resolves a relative path against workDir and validates it stays within bounds.
func ResolvePath(workDir, path string) (string, error) {
	resolved := filepath.Clean(filepath.Join(workDir, path))
	if !strings.HasPrefix(resolved, workDir) {
		return "", errPathOutsideWorkDir
	}

	return resolved, nil
}

func (h *GRPCFileHandler) resolvePath(path string) (string, error) {
	return ResolvePath(h.workDir, path)
}

type streamingFileReader struct {
	file      *os.File
	chunkSize int
}

func newStreamingFileReader(path string, chunkSize int) (*streamingFileReader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	if chunkSize <= 0 {
		chunkSize = defaultFileChunkSize
	}

	return &streamingFileReader{
		file:      file,
		chunkSize: chunkSize,
	}, nil
}

func (r *streamingFileReader) ReadChunk() ([]byte, error) {
	buf := make([]byte, r.chunkSize)
	n, err := r.file.Read(buf)
	if err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, err
	}
	return buf[:n], nil
}

func (r *streamingFileReader) Close() error {
	return r.file.Close()
}
