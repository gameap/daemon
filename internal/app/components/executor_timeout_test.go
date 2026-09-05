//go:build !windows

package components_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A command killed because its context ran out must not look like a command
// that exited on its own. os/exec reports the kill as exit code -1, and callers
// that pass the code straight through would read it as "the game server is not
// running" and restart a healthy server.
func TestExecWithWriterArgs_ContextDeadlineIsAnError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	code, err := components.ExecWithWriterArgs(
		ctx,
		[]string{"sleep", "5"},
		io.Discard,
		contracts.ExecutorOptions{WorkDir: t.TempDir()},
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, -1, code)
}

func TestExecWithWriterArgs_ExitCodeIsReportedWithoutError(t *testing.T) {
	code, err := components.ExecWithWriterArgs(
		context.Background(),
		[]string{"sh", "-c", "exit 3"},
		io.Discard,
		contracts.ExecutorOptions{WorkDir: t.TempDir()},
	)

	require.NoError(t, err)
	assert.Equal(t, 3, code)
}
