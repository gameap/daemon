package gameservercommands

import (
	"context"
	"io/ioutil"
	"os"
	"testing"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	copyPkg "github.com/otiai10/copy"
	"github.com/stretchr/testify/suite"
)

type deleteSuite struct {
	suite.Suite

	workPaths []string
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(deleteSuite))
}

func (suite *deleteSuite) TearDownSuite() {
	for _, p := range suite.workPaths {
		err := os.RemoveAll(p)
		if err != nil {
			suite.T().Log(err)
		}
	}
}

func (suite *deleteSuite) givenWorkPath() string {
	suite.T().Helper()
	workPath, err := ioutil.TempDir("/tmp", "delete-server-test")
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.workPaths = append(suite.workPaths, workPath)

	return workPath
}

func (suite *deleteSuite) TestDeleteServerByFilesystemSuccess() {
	workPath := suite.givenWorkPath()
	cfg := &config.Config{
		WorkPath: workPath,
	}
	server := givenServerWithStartCommand(suite.T(), "./run.sh")
	installSimpleServerFiles(suite.T(), cfg, server)
	deleteServerCommand := newDeleteServer(cfg, components.NewExecutor())
	ctx := context.Background()

	err := deleteServerCommand.Execute(ctx, server)

	suite.Require().Nil(err)
	suite.Assert().Equal(SuccessResult, deleteServerCommand.Result())
	suite.Assert().NoFileExists(server.WorkDir(cfg))
	suite.Assert().NoFileExists(server.WorkDir(cfg) + "/" + "run.sh")
	suite.Assert().NoFileExists(server.WorkDir(cfg) + "/" + "run2.sh")
}

func (suite *deleteSuite) TestDeleteServerByScriptSuccess() {
	workPath := suite.givenWorkPath()
	cfg := &config.Config{
		WorkPath: workPath,
		Scripts: config.Scripts{
			Delete: "rm -rf ./simple",
		},
	}
	server := givenServerWithStartCommand(suite.T(), "./run.sh")
	installSimpleServerFiles(suite.T(), cfg, server)
	deleteServerCommand := newDeleteServer(cfg, components.NewExecutor())
	ctx := context.Background()

	err := deleteServerCommand.Execute(ctx, server)

	suite.Require().Nil(err)
	suite.Assert().Equal(SuccessResult, deleteServerCommand.Result())
	suite.Assert().NoFileExists(server.WorkDir(cfg))
	suite.Assert().NoFileExists(server.WorkDir(cfg) + "/" + "run.sh")
	suite.Assert().NoFileExists(server.WorkDir(cfg) + "/" + "run2.sh")
}

func (suite *deleteSuite) TestDeleteServerByScript_CommandFail() {
	workPath := suite.givenWorkPath()
	cfg := &config.Config{
		WorkPath: workPath,
		Scripts: config.Scripts{
			Delete: "./fail.sh",
		},
	}
	server := givenServerWithStartCommand(suite.T(), "")
	installScripts(suite.T(), cfg)
	deleteServerCommand := newDeleteServer(cfg, components.NewCleanExecutor())
	ctx := context.Background()

	err := deleteServerCommand.Execute(ctx, server)

	suite.Require().Nil(err)
	suite.Assert().Equal(ErrorResult, deleteServerCommand.Result())
	suite.Assert().Equal("command failed\n", string(deleteServerCommand.ReadOutput()))
}

func installSimpleServerFiles(t *testing.T, cfg *config.Config, server *domain.Server) {
	t.Helper()
	err := copyPkg.Copy("../../../test/servers/simple/", server.WorkDir(cfg))
	if err != nil {
		t.Fatal(err)
	}
}

func installScripts(t *testing.T, cfg *config.Config) {
	t.Helper()
	err := copyPkg.Copy("../../../test/servers/scripts/", cfg.WorkPath)
	if err != nil {
		t.Fatal(err)
	}
}
