package files

import (
	"testing"

	"github.com/gameap/daemon/test/functional/servertest"
	"github.com/stretchr/testify/suite"
)

type Suite struct {
	servertest.Suite
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(Suite))
}
