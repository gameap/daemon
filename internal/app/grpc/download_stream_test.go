package grpc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"

	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errFakeStreamFailure = errors.New("network gone")

type fakeChunkReceiver struct {
	chunks []*pb.DownloadChunk
	err    error
	calls  int
}

func (f *fakeChunkReceiver) Recv() (*pb.DownloadChunk, error) {
	if f.err != nil && f.calls == len(f.chunks) {
		f.calls++

		return nil, f.err
	}
	if f.calls >= len(f.chunks) {
		return nil, io.EOF
	}
	chunk := f.chunks[f.calls]
	f.calls++

	return chunk, nil
}

func TestConsumeDownloadStream(t *testing.T) {
	const payload = "0123456789abcdef0123456789abcdef" // 32 bytes
	expectedChecksum := sha256Hex([]byte(payload))

	tests := []struct {
		name             string
		chunks           []*pb.DownloadChunk
		recvErr          error
		wantData         string
		wantChecksum     string
		wantErrSubstring string
	}{
		{
			// Regression: API may emit a single chunk carrying both Data and
			// IsFinal=true when the underlying io.Reader returns the last
			// bytes alongside io.EOF (S3 GetObject, bytes.Reader, ...).
			// Earlier code skipped Data when IsFinal=true, producing a
			// deterministic-but-wrong hash. The data must be written and
			// hashed regardless of IsFinal.
			name: "data_and_is_final_in_same_chunk",
			chunks: []*pb.DownloadChunk{
				{
					Data:           []byte(payload),
					IsFinal:        true,
					ChecksumSha256: expectedChecksum,
				},
			},
			wantData:     payload,
			wantChecksum: expectedChecksum,
		},
		{
			// Standard case: data chunks followed by an empty IsFinal chunk
			// carrying the checksum (the path taken when reader.Read signals
			// EOF on a separate call after the last data, e.g. *os.File).
			name: "data_chunks_then_empty_final",
			chunks: []*pb.DownloadChunk{
				{Data: []byte(payload[:16])},
				{Data: []byte(payload[16:])},
				{IsFinal: true, ChecksumSha256: expectedChecksum},
			},
			wantData:     payload,
			wantChecksum: expectedChecksum,
		},
		{
			name: "single_non_final_chunk_then_eof",
			chunks: []*pb.DownloadChunk{
				{Data: []byte(payload)},
			},
			wantData:     payload,
			wantChecksum: "",
		},
		{
			name:             "recv_error_propagates",
			chunks:           []*pb.DownloadChunk{{Data: []byte(payload[:8])}},
			recvErr:          errFakeStreamFailure,
			wantData:         payload[:8],
			wantErrSubstring: errFakeStreamFailure.Error(),
		},
		{
			// Empty body — no data at all, just the final marker. The hasher
			// stays at sha256("") so the caller can still verify against the
			// declared checksum.
			name: "only_empty_final_chunk",
			chunks: []*pb.DownloadChunk{
				{IsFinal: true, ChecksumSha256: sha256Hex([]byte(""))},
			},
			wantData:     "",
			wantChecksum: sha256Hex([]byte("")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			receiver := &fakeChunkReceiver{chunks: tt.chunks, err: tt.recvErr}
			sink := &bytes.Buffer{}
			hasher := sha256.New()

			gotChecksum, err := consumeDownloadStream(receiver, sink, hasher)

			if tt.wantErrSubstring != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrSubstring)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantData, sink.String(), "sink must contain all chunk data")
			assert.Equal(t, tt.wantChecksum, gotChecksum, "received checksum must be propagated")
			assert.Equal(t, sha256Hex([]byte(tt.wantData)), hex.EncodeToString(hasher.Sum(nil)),
				"hasher must observe the same bytes that were written to sink")
		})
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}
