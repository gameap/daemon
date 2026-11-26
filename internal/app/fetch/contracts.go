package fetch

import "io"

// ProgressTracker is told about a download as its body is read. It wraps the
// stream it is given and returns the one the download reads from; closing that
// stream ends the tracking.
type ProgressTracker interface {
	TrackProgress(src string, currentSize, totalSize int64, stream io.ReadCloser) io.ReadCloser
}
