package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func hashOpRequest(p *pb.HashParams) *pb.FileOperationRequest {
	return &pb.FileOperationRequest{
		RequestId: "req-1",
		Operation: pb.FileOperationType_FILE_OPERATION_TYPE_HASH,
		Parameters: &pb.FileOperationRequest_HashParams{
			HashParams: p,
		},
	}
}

func TestHandleFileOperation_HashAlgorithms(t *testing.T) {
	tests := []struct {
		name      string
		algorithm pb.HashAlgorithm
		content   string
		expected  string
	}{
		{
			name:      "md5",
			algorithm: pb.HashAlgorithm_HASH_ALGORITHM_MD5,
			content:   "",
			expected:  "d41d8cd98f00b204e9800998ecf8427e",
		},
		{
			name:      "sha1",
			algorithm: pb.HashAlgorithm_HASH_ALGORITHM_SHA1,
			content:   "",
			expected:  "da39a3ee5e6b4b0d3255bfef95601890afd80709",
		},
		{
			name:      "sha256",
			algorithm: pb.HashAlgorithm_HASH_ALGORITHM_SHA256,
			content:   "",
			expected:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:      "sha512",
			algorithm: pb.HashAlgorithm_HASH_ALGORITHM_SHA512,
			content:   "",
			expected: "cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce" +
				"47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e",
		},
		{
			name:      "crc32",
			algorithm: pb.HashAlgorithm_HASH_ALGORITHM_CRC32,
			content:   "123456789",
			expected:  "cbf43926",
		},
		{
			name:      "crc64",
			algorithm: pb.HashAlgorithm_HASH_ALGORITHM_CRC64,
			content:   "123456789",
			expected:  "995dc9bbdf1939fa",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			workDir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(workDir, "file.bin"), []byte(tc.content), 0o644))
			h := NewGRPCFileHandler(workDir)

			resp, err := h.HandleFileOperation(context.Background(), hashOpRequest(&pb.HashParams{
				Paths:     []string{"file.bin"},
				Algorithm: tc.algorithm,
			}))

			require.NoError(t, err)
			require.True(t, resp.Success, resp.Error)

			result := resp.GetHashResult()
			require.NotNil(t, result)
			assert.Equal(t, tc.algorithm, result.GetAlgorithm())
			require.Len(t, result.GetHashes(), 1)

			fh := result.GetHashes()[0]
			assert.Equal(t, "file.bin", fh.GetPath())
			assert.Empty(t, fh.GetError())
			assert.Equal(t, tc.expected, fh.GetHash())
			assert.Equal(t, fh.GetHash(), strings.ToLower(fh.GetHash()), "hash must be lowercase hex")
			assert.Equal(t, uint64(len(tc.content)), fh.GetSize())
		})
	}
}

func TestHandleFileOperation_HashDirectory(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "subdir"), 0o755))
	h := NewGRPCFileHandler(workDir)

	resp, err := h.HandleFileOperation(context.Background(), hashOpRequest(&pb.HashParams{
		Paths:     []string{"subdir"},
		Algorithm: pb.HashAlgorithm_HASH_ALGORITHM_SHA256,
	}))

	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)

	result := resp.GetHashResult()
	require.NotNil(t, result)
	require.Len(t, result.GetHashes(), 1)

	fh := result.GetHashes()[0]
	assert.Equal(t, "subdir", fh.GetPath())
	assert.Equal(t, "is a directory", fh.GetError())
	assert.Empty(t, fh.GetHash())
	assert.Zero(t, fh.GetSize())
}

func TestHandleFileOperation_HashMissingFile(t *testing.T) {
	h := NewGRPCFileHandler(t.TempDir())

	resp, err := h.HandleFileOperation(context.Background(), hashOpRequest(&pb.HashParams{
		Paths:     []string{"no/such/file.txt"},
		Algorithm: pb.HashAlgorithm_HASH_ALGORITHM_SHA256,
	}))

	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)

	result := resp.GetHashResult()
	require.NotNil(t, result)
	require.Len(t, result.GetHashes(), 1)

	fh := result.GetHashes()[0]
	assert.Equal(t, "no/such/file.txt", fh.GetPath())
	assert.NotEmpty(t, fh.GetError())
	assert.Empty(t, fh.GetHash())
}

func TestHandleFileOperation_HashMultiplePaths(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "ok.txt"), []byte("data"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "dir"), 0o755))
	h := NewGRPCFileHandler(workDir)

	resp, err := h.HandleFileOperation(context.Background(), hashOpRequest(&pb.HashParams{
		Paths:     []string{"ok.txt", "dir", "missing.txt"},
		Algorithm: pb.HashAlgorithm_HASH_ALGORITHM_SHA256,
	}))

	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)

	result := resp.GetHashResult()
	require.NotNil(t, result)
	require.Len(t, result.GetHashes(), 3)

	sum := sha256.Sum256([]byte("data"))

	ok := result.GetHashes()[0]
	assert.Equal(t, "ok.txt", ok.GetPath())
	assert.Empty(t, ok.GetError())
	assert.Equal(t, hex.EncodeToString(sum[:]), ok.GetHash())
	assert.Equal(t, uint64(4), ok.GetSize())

	dir := result.GetHashes()[1]
	assert.Equal(t, "dir", dir.GetPath())
	assert.Equal(t, "is a directory", dir.GetError())
	assert.Empty(t, dir.GetHash())

	missing := result.GetHashes()[2]
	assert.Equal(t, "missing.txt", missing.GetPath())
	assert.NotEmpty(t, missing.GetError())
	assert.Empty(t, missing.GetHash())
}

func TestHandleFileOperation_HashNilParams(t *testing.T) {
	h := NewGRPCFileHandler(t.TempDir())

	resp, err := h.HandleFileOperation(context.Background(), &pb.FileOperationRequest{
		RequestId: "req-1",
		Operation: pb.FileOperationType_FILE_OPERATION_TYPE_HASH,
	})

	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "hash_params")
}

func TestHandleFileOperation_HashUnspecifiedAlgorithm(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "f.txt"), []byte("data"), 0o644))
	h := NewGRPCFileHandler(workDir)

	resp, err := h.HandleFileOperation(context.Background(), hashOpRequest(&pb.HashParams{
		Paths:     []string{"f.txt"},
		Algorithm: pb.HashAlgorithm_HASH_ALGORITHM_UNSPECIFIED,
	}))

	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.NotEmpty(t, resp.Error)
}

func TestHandleFileOperation_HashPathTraversal(t *testing.T) {
	h := NewGRPCFileHandler(t.TempDir())

	resp, err := h.HandleFileOperation(context.Background(), hashOpRequest(&pb.HashParams{
		Paths:     []string{"../outside"},
		Algorithm: pb.HashAlgorithm_HASH_ALGORITHM_SHA256,
	}))

	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)

	result := resp.GetHashResult()
	require.NotNil(t, result)
	require.Len(t, result.GetHashes(), 1)

	fh := result.GetHashes()[0]
	assert.Equal(t, "../outside", fh.GetPath())
	assert.Contains(t, fh.GetError(), "outside work directory")
	assert.Empty(t, fh.GetHash())
}
