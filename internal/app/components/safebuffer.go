package components

// https://gist.github.com/arkan/5924e155dbb4254b64614069ba0afd81

import (
	"bytes"
	"sync"
)

// SafeBuffer is a goroutine safe bytes.Buffer.
type SafeBuffer struct {
	buffer bytes.Buffer
	mu     sync.Mutex
}

func NewSafeBuffer() *SafeBuffer {
	return &SafeBuffer{buffer: bytes.Buffer{}}
}

// Write appends the contents of p to the buffer, growing the buffer as needed. It returns
// the number of bytes written.
func (s *SafeBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buffer.Write(p)
}

func (s *SafeBuffer) Read(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buffer.Read(p)
}

// String returns the contents of the unread portion of the buffer
// as a string.  If the buffer is a nil pointer, it returns "<nil>".
func (s *SafeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buffer.String()
}
