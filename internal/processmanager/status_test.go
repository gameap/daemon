package processmanager

import (
	"testing"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusFromExitCode(t *testing.T) {
	t.Run("zero means running", func(t *testing.T) {
		result, err := statusFromExitCode(0)

		require.NoError(t, err)
		assert.Equal(t, domain.SuccessResult, result)
	})

	t.Run("a positive code means stopped", func(t *testing.T) {
		result, err := statusFromExitCode(1)

		require.NoError(t, err)
		assert.Equal(t, domain.ErrorResult, result)
	})

	// A negative code is what os/exec reports for a process killed by a signal,
	// which includes the probe being cut short by its own deadline. Reading that
	// as a stopped server would make the daemon restart a healthy one.
	t.Run("a negative code leaves the status undetermined", func(t *testing.T) {
		result, err := statusFromExitCode(-1)

		assert.ErrorIs(t, err, ErrStatusUndetermined)
		assert.Equal(t, domain.UnknownResult, result)
	})
}
