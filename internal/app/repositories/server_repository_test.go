package repositories

import (
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/stretchr/testify/assert"
)

func serverWithDir(id int, dir string) *domain.Server {
	return domain.NewServer(
		id,
		true,
		domain.ServerInstalled,
		false,
		"test",
		"uuid",
		"uuid",
		domain.Game{},
		domain.GameMod{},
		"127.0.0.1",
		27015, 27016, 27020,
		"",
		dir,
		"gameap",
		"start", "stop", "kill", "restart",
		false,
		time.Now(),
		nil,
		nil,
		time.Now(),
		0, 0,
	)
}

func TestServerRepository_ServerDirs(t *testing.T) {
	t.Parallel()

	repo := NewServerRepository()

	assert.Empty(t, repo.ServerDirs())

	repo.SaveToCache(serverWithDir(1, "servers/a"))
	repo.SaveToCache(serverWithDir(2, "servers/b"))

	assert.ElementsMatch(t, []string{"servers/a", "servers/b"}, repo.ServerDirs())

	// A config push for a server already cached replaces its directory: the old
	// one stops being a boundary together with the server leaving it.
	repo.SaveToCache(serverWithDir(2, "servers/moved"))

	assert.ElementsMatch(t, []string{"servers/a", "servers/moved"}, repo.ServerDirs())
}
