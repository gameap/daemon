package restart

import (
	"testing"

	"github.com/gameap/daemon/test/functional/servers_command"
	"github.com/stretchr/testify/suite"
)

type Suite struct {
	servers_command.InstalledServerSuite
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(Suite))
}
