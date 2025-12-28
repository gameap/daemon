package components

import (
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gameap/daemon/pkg/humanize"
	"github.com/hashicorp/go-getter"
)

// Compile-time check that DownloadProgressTracker implements getter.ProgressTracker.
var _ getter.ProgressTracker = (*DownloadProgressTracker)(nil)

// DownloadProgressTracker implements go-getter's ProgressTracker interface
// and outputs periodic progress updates to an io.Writer.
type DownloadProgressTracker struct {
	output   io.Writer
	interval time.Duration
	mu       sync.Mutex
}

// NewDownloadProgressTracker creates a new progress tracker
// that writes updates to the given output writer at the specified interval.
func NewDownloadProgressTracker(output io.Writer, interval time.Duration) *DownloadProgressTracker {
	return &DownloadProgressTracker{
		output:   output,
		interval: interval,
	}
}

// TrackProgress implements getter.ProgressTracker interface.
// It wraps the download stream and periodically outputs progress information.
func (pt *DownloadProgressTracker) TrackProgress(
	src string,
	currentSize, totalSize int64,
	stream io.ReadCloser,
) io.ReadCloser {
	return &progressReader{
		reader:      stream,
		tracker:     pt,
		src:         src,
		currentSize: currentSize,
		totalSize:   totalSize,
		startTime:   time.Now(),
		lastUpdate:  time.Time{},
	}
}

// progressReader wraps an io.ReadCloser and tracks bytes read.
type progressReader struct {
	reader      io.ReadCloser
	tracker     *DownloadProgressTracker
	src         string
	currentSize int64 // bytes already downloaded (for resumed downloads)
	totalSize   int64 // total expected size (0 if unknown)
	bytesRead   int64 // bytes read in this session
	startTime   time.Time
	lastUpdate  time.Time
	mu          sync.Mutex
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 {
		pr.mu.Lock()
		pr.bytesRead += int64(n)
		shouldUpdate := time.Since(pr.lastUpdate) >= pr.tracker.interval
		pr.mu.Unlock()

		if shouldUpdate {
			pr.writeProgress()
		}
	}
	return n, err
}

func (pr *progressReader) Close() error {
	pr.writeProgress()
	return pr.reader.Close()
}

func (pr *progressReader) writeProgress() {
	pr.mu.Lock()
	pr.lastUpdate = time.Now()
	currentBytes := pr.currentSize + pr.bytesRead
	elapsed := time.Since(pr.startTime)
	bytesRead := pr.bytesRead
	totalSize := pr.totalSize
	pr.mu.Unlock()

	var speedBytesPerSec float64
	if elapsed.Seconds() > 0 {
		speedBytesPerSec = float64(bytesRead) / elapsed.Seconds()
	}

	progressMsg := formatProgress(currentBytes, totalSize, speedBytesPerSec)

	pr.tracker.mu.Lock()
	defer pr.tracker.mu.Unlock()

	_, _ = pr.tracker.output.Write([]byte(progressMsg + "\n"))
}

// formatProgress creates a human-readable progress string.
// Format: "15% (15 MiB / 100 MiB) - 2.5 MiB/s" or "15 MiB downloaded - 2.5 MiB/s" if total unknown.
func formatProgress(current, total int64, speedBytesPerSec float64) string {
	currentFormatted := humanize.IBytes(uint64(current))
	speedFormatted := humanize.IBytes(uint64(speedBytesPerSec))

	var sb strings.Builder
	sb.Grow(48)

	if total > 0 {
		percentage := current * 100 / total
		totalFormatted := humanize.IBytes(uint64(total))

		sb.WriteString(strconv.FormatInt(percentage, 10))
		sb.WriteString("% (")
		sb.WriteString(currentFormatted)
		sb.WriteString(" / ")
		sb.WriteString(totalFormatted)
		sb.WriteString(") - ")
		sb.WriteString(speedFormatted)
		sb.WriteString("/s")
	} else {
		sb.WriteString(currentFormatted)
		sb.WriteString(" downloaded - ")
		sb.WriteString(speedFormatted)
		sb.WriteString("/s")
	}

	return sb.String()
}
