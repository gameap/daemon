package components

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatProgress_WithKnownSize(t *testing.T) {
	tests := []struct {
		name     string
		current  int64
		total    int64
		speed    float64
		contains []string
	}{
		{
			name:     "0%",
			current:  0,
			total:    100 * 1024 * 1024,
			speed:    1024 * 1024,
			contains: []string{"0%", "0 B", "100 MiB", "1.0 MiB/s"},
		},
		{
			name:     "50%",
			current:  50 * 1024 * 1024,
			total:    100 * 1024 * 1024,
			speed:    2 * 1024 * 1024,
			contains: []string{"50%", "50 MiB", "100 MiB", "2.0 MiB/s"},
		},
		{
			name:     "100%",
			current:  100 * 1024 * 1024,
			total:    100 * 1024 * 1024,
			speed:    5 * 1024 * 1024,
			contains: []string{"100%", "100 MiB", "5.0 MiB/s"},
		},
		{
			name:     "small file",
			current:  512,
			total:    1024,
			speed:    256,
			contains: []string{"50%", "512 B", "1.0 KiB", "256 B/s"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatProgress(tt.current, tt.total, tt.speed)
			for _, substr := range tt.contains {
				assert.Contains(t, result, substr, "expected %q to contain %q", result, substr)
			}
		})
	}
}

func TestFormatProgress_WithUnknownSize(t *testing.T) {
	tests := []struct {
		name     string
		current  int64
		speed    float64
		contains []string
	}{
		{
			name:     "basic",
			current:  50 * 1024 * 1024,
			speed:    2 * 1024 * 1024,
			contains: []string{"50 MiB", "downloaded", "2.0 MiB/s"},
		},
		{
			name:     "zero speed",
			current:  1024,
			speed:    0,
			contains: []string{"1.0 KiB", "downloaded", "0 B/s"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatProgress(tt.current, 0, tt.speed)
			for _, substr := range tt.contains {
				assert.Contains(t, result, substr)
			}
			assert.NotContains(t, result, "%", "should not contain percentage when size unknown")
		})
	}
}

func TestNewDownloadProgressTracker(t *testing.T) {
	var buf bytes.Buffer
	interval := 5 * time.Second

	tracker := NewDownloadProgressTracker(&buf, interval)

	assert.NotNil(t, tracker)
	assert.Equal(t, interval, tracker.interval)
}

func TestTrackProgress_WrapsStream(t *testing.T) {
	var buf bytes.Buffer
	tracker := NewDownloadProgressTracker(&buf, 10*time.Millisecond)

	data := []byte("test data for download simulation")
	reader := io.NopCloser(bytes.NewReader(data))

	wrapped := tracker.TrackProgress("http://example.com/file.zip", 0, int64(len(data)), reader)

	require.NotNil(t, wrapped)

	// Read all data
	result, err := io.ReadAll(wrapped)
	require.NoError(t, err)
	assert.Equal(t, data, result)

	// Close should write final progress
	err = wrapped.Close()
	require.NoError(t, err)

	output := buf.String()
	assert.NotEmpty(t, output)
	assert.Contains(t, output, "100%")
}

func TestProgressReader_PeriodicUpdates(t *testing.T) {
	var buf bytes.Buffer
	tracker := NewDownloadProgressTracker(&buf, 10*time.Millisecond)

	// 100 bytes total
	data := bytes.Repeat([]byte("x"), 100)
	reader := io.NopCloser(bytes.NewReader(data))

	wrapped := tracker.TrackProgress("http://example.com/file.zip", 0, 100, reader)

	// Read in small chunks with delays to trigger periodic updates
	chunk := make([]byte, 25)
	for {
		_, err := wrapped.Read(chunk)
		if err == io.EOF {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	wrapped.Close()

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Should have multiple progress lines due to interval
	assert.GreaterOrEqual(t, len(lines), 2, "expected multiple progress updates")
}

func TestProgressReader_ResumedDownload(t *testing.T) {
	var buf bytes.Buffer
	tracker := NewDownloadProgressTracker(&buf, time.Millisecond)

	// Simulate resumed download: already have 50 bytes, downloading 50 more
	data := bytes.Repeat([]byte("x"), 50)
	reader := io.NopCloser(bytes.NewReader(data))

	wrapped := tracker.TrackProgress("http://example.com/file.zip", 50, 100, reader)

	_, err := io.ReadAll(wrapped)
	require.NoError(t, err)
	wrapped.Close()

	output := buf.String()
	assert.Contains(t, output, "100%")
	assert.Contains(t, output, "100 B")
}

func TestProgressReader_UnknownSize(t *testing.T) {
	var buf bytes.Buffer
	tracker := NewDownloadProgressTracker(&buf, time.Millisecond)

	data := bytes.Repeat([]byte("x"), 1024)
	reader := io.NopCloser(bytes.NewReader(data))

	// totalSize = 0 means unknown
	wrapped := tracker.TrackProgress("http://example.com/file.zip", 0, 0, reader)

	_, err := io.ReadAll(wrapped)
	require.NoError(t, err)
	wrapped.Close()

	output := buf.String()
	assert.Contains(t, output, "downloaded")
	assert.Contains(t, output, "1.0 KiB")
	assert.NotContains(t, output, "%")
}

func TestProgressReader_ConcurrentAccess(t *testing.T) {
	var buf bytes.Buffer
	tracker := NewDownloadProgressTracker(&buf, time.Millisecond)

	data := bytes.Repeat([]byte("x"), 10000)
	reader := io.NopCloser(bytes.NewReader(data))

	wrapped := tracker.TrackProgress("http://example.com/file.zip", 0, 10000, reader)

	// Concurrent reads
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			chunk := make([]byte, 100)
			for {
				_, err := wrapped.Read(chunk)
				if err != nil {
					break
				}
			}
			done <- true
		}()
	}

	// Wait for all goroutines (with timeout)
	timeout := time.After(5 * time.Second)
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-timeout:
			t.Fatal("timeout waiting for concurrent reads")
		}
	}

	wrapped.Close()

	// Should not panic and should have output
	assert.NotEmpty(t, buf.String())
}

func BenchmarkFormatProgress_KnownSize(b *testing.B) {
	for i := 0; i < b.N; i++ {
		formatProgress(50*1024*1024, 100*1024*1024, 2*1024*1024)
	}
}

func BenchmarkFormatProgress_UnknownSize(b *testing.B) {
	for i := 0; i < b.N; i++ {
		formatProgress(50*1024*1024, 0, 2*1024*1024)
	}
}

func BenchmarkProgressReader_Read(b *testing.B) {
	data := bytes.Repeat([]byte("x"), 1024*1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		tracker := NewDownloadProgressTracker(&buf, time.Hour) // long interval to avoid writes
		reader := io.NopCloser(bytes.NewReader(data))
		wrapped := tracker.TrackProgress("http://example.com/file.zip", 0, int64(len(data)), reader)

		chunk := make([]byte, 32*1024)
		for {
			_, err := wrapped.Read(chunk)
			if err == io.EOF {
				break
			}
		}
		wrapped.Close()
	}
}
