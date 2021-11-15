package start

import (
	"testing"

	"github.com/gameap/daemon/test/functional/serverscommand"
	"github.com/stretchr/testify/suite"
)

type Suite struct {
	serverscommand.InstalledServerSuite
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(Suite))
}
