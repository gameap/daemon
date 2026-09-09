//go:build linux || darwin

package processmanager

import (
	"testing"

	"github.com/gameap/daemon/pkg/shellquote"
	"github.com/stretchr/testify/assert"
)

func Test_withEnvPrefix(t *testing.T) {
	args := []string{"./start.sh", "-port", "27015"}

	t.Run("an empty environment leaves the command untouched", func(t *testing.T) {
		assert.Equal(t, args, withEnvPrefix(nil, args))
		assert.Equal(t, args, withEnvPrefix(map[string]string{}, args))
	})

	t.Run("variables are sorted so the command line is deterministic", func(t *testing.T) {
		got := withEnvPrefix(map[string]string{"PORT": "27015", "HOME": "/srv/gameap/servers/gb"}, args)

		assert.Equal(t, []string{
			"env", "--",
			"HOME=/srv/gameap/servers/gb",
			"PORT=27015",
			"./start.sh", "-port", "27015",
		}, got)
	})

	t.Run("a value with spaces survives the shell tmux runs the session command through", func(t *testing.T) {
		got := shellquote.Join(withEnvPrefix(map[string]string{"HOSTNAME": "My Server"}, args)...)

		assert.Equal(t, `env -- 'HOSTNAME=My Server' ./start.sh -port 27015`, got)
	})
}
