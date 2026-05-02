package grpc

import (
	"hash"
	"io"

	pb "github.com/gameap/gameap/pkg/proto"
)

// downloadChunkReceiver is the minimal subset of grpc.ServerStreamingClient
// that consumeDownloadStream needs. It exists so the chunk-processing loop
// can be exercised by unit tests without a real gRPC stream.
type downloadChunkReceiver interface {
	Recv() (*pb.DownloadChunk, error)
}

// chunkSink abstracts the file write so tests can substitute an in-memory
// destination. It is a strict subset of *os.File.
type chunkSink interface {
	Write(p []byte) (int, error)
}

// consumeDownloadStream reads chunks from the receiver, writes their payload to
// sink, and feeds the same payload into hasher. It returns the stream-level
// SHA-256 carried on the final chunk (empty string if the server never sent one)
// so the caller can verify integrity end-to-end.
//
// The final chunk may carry payload bytes alongside IsFinal=true and the stream
// checksum. This happens when the API's storage backend reports EOF together
// with the last bytes — e.g. the minio S3 client, or any io.Reader implemented
// in the EOF-with-data style. Earlier versions of this loop short-circuited on
// IsFinal=true and dropped that final buffer, producing a deterministic-but-
// wrong hash. Always persist Data first; IsFinal is informational, the loop
// terminates on the next Recv() returning io.EOF.
func consumeDownloadStream(stream downloadChunkReceiver, sink chunkSink, hasher hash.Hash) (string, error) {
	var receivedChecksum string

	for {
		chunk, recvErr := stream.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			return "", recvErr
		}

		if chunk.GetChecksumSha256() != "" {
			receivedChecksum = chunk.GetChecksumSha256()
		}

		data := chunk.GetData()
		if len(data) > 0 {
			if _, writeErr := sink.Write(data); writeErr != nil {
				return "", writeErr
			}
			hasher.Write(data)
		}
	}

	return receivedChecksum, nil
}
