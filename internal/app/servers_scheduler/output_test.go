package serversscheduler

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSplitOutput_SmallPayload_GoesInline(t *testing.T) {
	buf := bytes.Repeat([]byte("a"), 1024)

	chunks, inline, streamed := splitOutput(buf)

	assert.Nil(t, chunks)
	assert.Equal(t, buf, inline)
	assert.False(t, streamed)
}

func TestSplitOutput_ExactInlineMax_GoesInline(t *testing.T) {
	buf := bytes.Repeat([]byte("a"), outputInlineMax)

	chunks, inline, streamed := splitOutput(buf)

	assert.Nil(t, chunks)
	assert.Equal(t, buf, inline)
	assert.False(t, streamed)
}

func TestSplitOutput_JustOverInlineMax_StreamsAndTrims(t *testing.T) {
	buf := bytes.Repeat([]byte("a"), outputInlineMax+1)

	chunks, inline, streamed := splitOutput(buf)

	assert.True(t, streamed)
	assert.Len(t, inline, outputInlineMax, "inline must hold last 64KB")
	totalChunked := 0
	for _, c := range chunks {
		totalChunked += len(c)
	}
	assert.Equal(t, len(buf), totalChunked, "chunks must cover entire buffer")
}

func TestSplitOutput_LargePayload_ChunksAreOrderedAndBounded(t *testing.T) {
	buf := make([]byte, 200*1024)
	for i := range buf {
		buf[i] = byte(i % 256)
	}

	chunks, inline, streamed := splitOutput(buf)

	assert.True(t, streamed)
	assert.Equal(t, 7, len(chunks), "200KB / 32KB = 6 full + 1 partial = 7 chunks")
	for i, c := range chunks {
		if i == len(chunks)-1 {
			assert.LessOrEqual(t, len(c), outputChunkSize)
		} else {
			assert.Equal(t, outputChunkSize, len(c))
		}
	}
	rebuilt := make([]byte, 0, len(buf))
	for _, c := range chunks {
		rebuilt = append(rebuilt, c...)
	}
	assert.Equal(t, buf, rebuilt)
	assert.Equal(t, buf[len(buf)-outputInlineMax:], inline)
}

func TestTruncateError_LongMessage(t *testing.T) {
	msg := string(bytes.Repeat([]byte("x"), errMessageMax+100))

	got := truncateError(msg)

	assert.Len(t, got, errMessageMax)
}

func TestTruncateError_ShortMessage_Unchanged(t *testing.T) {
	msg := "boom"

	got := truncateError(msg)

	assert.Equal(t, msg, got)
}
