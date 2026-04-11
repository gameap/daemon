package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"

	"github.com/gameap/daemon/internal/app/config"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

const (
	fileChunkSize = 64 * 1024
)

type FileTransferClient struct {
	cfg    *config.Config
	client pb.FileTransferServiceClient
}

func NewFileTransferClient(cfg *config.Config) *FileTransferClient {
	return &FileTransferClient{
		cfg: cfg,
	}
}

func (c *FileTransferClient) SetConnection(conn *grpc.ClientConn) {
	c.client = pb.NewFileTransferServiceClient(conn)
}

func (c *FileTransferClient) UploadFile(ctx context.Context, localPath, remotePath string) error {
	if c.client == nil {
		return errors.New("file transfer client not connected")
	}

	file, err := os.Open(localPath)
	if err != nil {
		return errors.Wrap(err, "failed to open file")
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return errors.Wrap(err, "failed to stat file")
	}

	stream, err := c.client.UploadFile(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to create upload stream")
	}

	hasher := sha256.New()

	firstChunk := true
	buf := make([]byte, fileChunkSize)
	for {
		n, err := file.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Wrap(err, "failed to read file")
		}

		hasher.Write(buf[:n])

		chunk := &pb.UploadChunk{
			Data: buf[:n],
		}

		if firstChunk {
			chunk.Metadata = &pb.UploadMetadata{
				Path:      remotePath,
				TotalSize: info.Size(),
				Mode:      int32(info.Mode()),
			}
			firstChunk = false
		}

		if err := stream.Send(chunk); err != nil {
			return errors.Wrap(err, "failed to send chunk")
		}
	}

	// Send final chunk with checksum
	if err := stream.Send(&pb.UploadChunk{
		Metadata: &pb.UploadMetadata{
			ChecksumSha256: hex.EncodeToString(hasher.Sum(nil)),
		},
	}); err != nil {
		return errors.Wrap(err, "failed to send final chunk with checksum")
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		return errors.Wrap(err, "failed to close stream")
	}

	if !resp.Success {
		return errors.Errorf("upload failed: %s", resp.Error)
	}

	log.WithFields(log.Fields{
		"localPath":  localPath,
		"remotePath": remotePath,
		"size":       info.Size(),
	}).Info("File uploaded successfully")

	return nil
}

func (c *FileTransferClient) UploadFileForTransfer(
	ctx context.Context,
	transferID string,
	file *os.File,
	fileSize int64,
	fileMode os.FileMode,
) (string, error) {
	if c.client == nil {
		return "", errors.New("file transfer client not connected")
	}

	stream, err := c.client.UploadFile(ctx)
	if err != nil {
		return "", errors.Wrap(err, "failed to create upload stream")
	}

	hasher := sha256.New()

	firstChunk := true
	buf := make([]byte, fileChunkSize)
	for {
		n, readErr := file.Read(buf)
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", errors.Wrap(readErr, "failed to read file")
		}

		hasher.Write(buf[:n])

		chunk := &pb.UploadChunk{
			Data: buf[:n],
		}

		if firstChunk {
			chunk.Metadata = &pb.UploadMetadata{
				TransferId: transferID,
				Path:       transferID,
				TotalSize:  fileSize,
				Mode:       int32(fileMode),
			}
			firstChunk = false
		}

		if sendErr := stream.Send(chunk); sendErr != nil {
			return "", errors.Wrap(sendErr, "failed to send chunk")
		}
	}

	if firstChunk {
		if sendErr := stream.Send(&pb.UploadChunk{
			Metadata: &pb.UploadMetadata{
				TransferId: transferID,
				Path:       transferID,
				TotalSize:  fileSize,
				Mode:       int32(fileMode),
			},
		}); sendErr != nil {
			return "", errors.Wrap(sendErr, "failed to send metadata for empty file")
		}
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))

	if err := stream.Send(&pb.UploadChunk{
		Metadata: &pb.UploadMetadata{
			ChecksumSha256: checksum,
		},
	}); err != nil {
		return "", errors.Wrap(err, "failed to send final chunk with checksum")
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		return "", errors.Wrap(err, "failed to close stream")
	}

	if !resp.Success {
		return "", errors.Errorf("upload failed: %s", resp.Error)
	}

	return checksum, nil
}

func (c *FileTransferClient) DownloadFileStream(
	ctx context.Context,
	transferID string,
	offset int64,
) (grpc.ServerStreamingClient[pb.DownloadChunk], error) {
	if c.client == nil {
		return nil, errors.New("file transfer client not connected")
	}

	stream, err := c.client.DownloadFile(ctx, &pb.DownloadRequest{
		Path:   transferID,
		Offset: offset,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create download stream")
	}

	return stream, nil
}

func (c *FileTransferClient) DownloadFile(ctx context.Context, remotePath, localPath string) error {
	if c.client == nil {
		return errors.New("file transfer client not connected")
	}

	stream, err := c.client.DownloadFile(ctx, &pb.DownloadRequest{
		Path: remotePath,
	})
	if err != nil {
		return errors.Wrap(err, "failed to create download stream")
	}

	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errors.Wrap(err, "failed to create directory")
	}

	file, err := os.Create(localPath)
	if err != nil {
		return errors.Wrap(err, "failed to create file")
	}
	defer file.Close()

	hasher := sha256.New()
	var receivedChecksum string

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			os.Remove(localPath)
			return errors.Wrap(err, "failed to receive chunk")
		}

		if chunk.ChecksumSha256 != "" {
			receivedChecksum = chunk.ChecksumSha256
		}

		if chunk.IsFinal {
			continue
		}

		if len(chunk.Data) > 0 {
			if _, err := file.Write(chunk.Data); err != nil {
				os.Remove(localPath)
				return errors.Wrap(err, "failed to write chunk")
			}
			hasher.Write(chunk.Data)
		}
	}

	if receivedChecksum != "" {
		calculatedChecksum := hex.EncodeToString(hasher.Sum(nil))
		if calculatedChecksum != receivedChecksum {
			os.Remove(localPath)
			return errors.Errorf("checksum mismatch: expected %s, got %s", receivedChecksum, calculatedChecksum)
		}
	}

	log.WithFields(log.Fields{
		"remotePath": remotePath,
		"localPath":  localPath,
	}).Info("File downloaded successfully")

	return nil
}
