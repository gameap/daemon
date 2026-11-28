package archive

import (
	"bytes"
	"io"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readCounter counts how many times the reader under it is asked for data.
type readCounter struct {
	reader io.Reader
	calls  int
}

func (c *readCounter) Read(p []byte) (int, error) {
	c.calls++

	return c.reader.Read(p)
}

// An archive is decompressed straight from the file it is in, so every call
// the decompressor makes for its input is a read(2). A decoder that takes its
// input a byte a call turns an archive of hundreds of megabytes into hundreds
// of millions of them.
func TestDecompressReader_InputIsReadInBlocks(t *testing.T) {
	t.Parallel()

	const (
		contentBytes = 256 << 10
		// A decompressor may read in blocks as small as this.
		smallestBlockBytes = 512
	)

	// Random bytes out of eight values. They compress, so a decoder has to
	// decode them: what does not compress is stored as it is and copied out in
	// bulk. And they compress to a good third of their size and no further, so
	// the stream stays long enough for a call a byte to show.
	content := make([]byte, contentBytes)
	_, err := rand.NewChaCha8([32]byte{1}).Read(content)
	require.NoError(t, err)

	for i := range content {
		content[i] &= 0x07
	}

	tests := []struct {
		name string
		comp compression
	}{
		{name: "gzip", comp: compGzip},
		{name: "bzip2", comp: compBzip2},
		{name: "xz", comp: compXz},
		{name: "zstd", comp: compZstd},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var stream bytes.Buffer

			writer, closer, err := compressWriter(&stream, test.comp, nil)
			require.NoError(t, err)

			_, err = writer.Write(content)
			require.NoError(t, err)
			require.NoError(t, closer.Close())

			streamBytes := stream.Len()
			require.Less(t, streamBytes, contentBytes*3/4, "the content was meant to compress")
			require.Greater(t, streamBytes, contentBytes/4, "the stream was meant to stay long")

			counter := &readCounter{reader: &stream}

			decompressed, err := decompressReader(counter, test.comp)
			require.NoError(t, err)

			got, err := io.ReadAll(decompressed)
			require.NoError(t, err)
			require.NoError(t, decompressed.Close())

			require.True(t, bytes.Equal(content, got), "decompressed content differs from what was compressed")
			assert.LessOrEqual(t, counter.calls, streamBytes/smallestBlockBytes,
				"a stream of %d bytes took %d reads", streamBytes, counter.calls)
		})
	}
}
