package grpc

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/pkg/errors"
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

	var files []*pb.FileInfo

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
) ([]*pb.FileInfo, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	files := make([]*pb.FileInfo, 0, len(entries))
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

		files = append(files, &pb.FileInfo{
			Name:         entry.Name(),
			Path:         filepath.Join(requestPath, entry.Name()),
			IsDir:        entry.IsDir(),
			Size:         info.Size(),
			Mode:         int32(info.Mode()),
			ModifiedUnix: info.ModTime().Unix(),
		})
	}

	return files, nil
}

func (h *GRPCFileHandler) listRecursive(
	dirPath, requestPath, pattern string,
) ([]*pb.FileInfo, error) {
	var files []*pb.FileInfo

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

		files = append(files, &pb.FileInfo{
			Name:         d.Name(),
			Path:         filepath.Join(requestPath, relPath),
			IsDir:        d.IsDir(),
			Size:         info.Size(),
			Mode:         int32(info.Mode()),
			ModifiedUnix: info.ModTime().Unix(),
		})

		return nil
	})

	return files, err
}

func (h *GRPCFileHandler) resolvePath(path string) (string, error) {
	resolved := filepath.Clean(filepath.Join(h.workDir, path))
	if !strings.HasPrefix(resolved, h.workDir) {
		return "", errPathOutsideWorkDir
	}

	return resolved, nil
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
