package gameservercommands

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/processmanager"
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
	workPath, err := os.MkdirTemp("/tmp", "delete-server-test")
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
	deleteServerCommand := newDefaultDeleteServer(
		cfg,
		components.NewExecutor(),
		processmanager.NewSimple(cfg, components.NewExecutor()),
	)
	ctx := context.Background()

	err := deleteServerCommand.Execute(ctx, server)

	suite.Require().Nil(err)
	suite.Assert().Equal(SuccessResult, deleteServerCommand.Result())
	suite.Assert().NoFileExists(server.WorkDir(cfg))
	suite.Assert().NoFileExists(filepath.Join(server.WorkDir(cfg), "run.sh"))
	suite.Assert().NoFileExists(filepath.Join(server.WorkDir(cfg), "run2.sh"))
}

func (suite *deleteSuite) TestDeleteServerByScriptSuccess() {
	workPath := suite.givenWorkPath()
	var deleteCommand string
	if runtime.GOOS == "windows" {
		deleteCommand = "cmd /c rmdir /S /Q simple"
	} else {
		deleteCommand = "rm -rf ./simple"
	}
	cfg := &config.Config{
		WorkPath: workPath,
		Scripts: config.Scripts{
			Delete: deleteCommand,
		},
	}
	server := givenServerWithStartCommand(suite.T(), "./run.sh")
	installSimpleServerFiles(suite.T(), cfg, server)
	deleteServerCommand := newDefaultDeleteServer(
		cfg,
		components.NewExecutor(),
		processmanager.NewSimple(cfg, components.NewExecutor()),
	)
	ctx := context.Background()

	err := deleteServerCommand.Execute(ctx, server)

	suite.Require().Nil(err)
	suite.Assert().Equal(SuccessResult, deleteServerCommand.Result())
	suite.Assert().NoFileExists(server.WorkDir(cfg))
	suite.Assert().NoFileExists(filepath.Join(server.WorkDir(cfg), "run.sh"))
	suite.Assert().NoFileExists(filepath.Join(server.WorkDir(cfg), "run2.sh"))
}

func (suite *deleteSuite) TestDeleteServerByScript_CommandFail() {
	workPath := suite.givenWorkPath()
	var deleteCommand string
	if runtime.GOOS == "windows" {
		deleteCommand = "cmd /c fail.bat"
	} else {
		deleteCommand = "./fail.sh"
	}
	cfg := &config.Config{
		WorkPath: workPath,
		Scripts: config.Scripts{
			Delete: deleteCommand,
		},
	}
	server := givenServerWithStartCommand(suite.T(), "")
	installScripts(suite.T(), cfg)
	deleteServerCommand := newDefaultDeleteServer(
		cfg,
		components.NewCleanExecutor(),
		processmanager.NewSimple(cfg, components.NewExecutor()),
	)
	ctx := context.Background()

	err := deleteServerCommand.Execute(ctx, server)

	suite.Require().Nil(err)
	suite.Assert().Equal(ErrorResult, deleteServerCommand.Result())
	if runtime.GOOS == "windows" {
		suite.Assert().Equal("command failed\r\n", string(deleteServerCommand.ReadOutput()))
	} else {
		suite.Assert().Equal("command failed\n", string(deleteServerCommand.ReadOutput()))
	}
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

func givenServerWithStartCommand(t *testing.T, startCommand string) *domain.Server {
	t.Helper()

	return domain.NewServer(
		1337,
		true,
		domain.ServerInstalled,
		false,
		"name",
		"759b875e-d910-11eb-aff7-d796d7fcf7ef",
		"759b875e",
		domain.Game{
			StartCode: "cstrike",
		},
		domain.GameMod{
			Name: "public",
		},
		"1.3.3.7",
		1337,
		1338,
		1339,
		"paS$w0rD",
		"simple",
		"gameap-user",
		startCommand,
		"",
		"",
		"",
		true,
		time.Now(),
		map[string]string{
			"default_map": "de_dust2",
			"tickrate":    "1000",
		},
		map[string]string{},
		time.Now(),
	)
}
