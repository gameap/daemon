package serversscheduler

const (
	outputInlineMax = 64 * 1024
	outputChunkSize = 32 * 1024
	errMessageMax   = 4 * 1024
)

// splitOutput decides how the command output ships to the API.
//
// Small output (≤ 64 KB) is returned inline; the caller emits no
// ServerTaskExecutionLog chunks. Larger output is split into 32 KB
// chunks for streaming and the last 64 KB tail is also returned inline
// so the API has a quick-read snippet even when the full payload was
// streamed.
func splitOutput(buf []byte) ([][]byte, []byte, bool) {
	if len(buf) <= outputInlineMax {
		return nil, buf, false
	}

	chunks := make([][]byte, 0, (len(buf)+outputChunkSize-1)/outputChunkSize)
	for i := 0; i < len(buf); i += outputChunkSize {
		end := i + outputChunkSize
		if end > len(buf) {
			end = len(buf)
		}
		chunks = append(chunks, buf[i:end])
	}

	return chunks, buf[len(buf)-outputInlineMax:], true
}

func truncateError(s string) string {
	if len(s) <= errMessageMax {
		return s
	}
	return s[:errMessageMax]
}
