package gameservercommands

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallationRuleDefiner_CopyFromLocalDirectory(t *testing.T) {
	rulesDefiner := installationRulesDefiner{}
	game := &domain.Game{
		LocalRepository: "/tmp",
	}

	rules := rulesDefiner.DefineGameRules(game)

	require.Len(t, rules, 1)
	assert.Equal(t, "/tmp", rules[0].SourceValue)
	assert.Equal(t, copyDirectoryFromLocalRepository, rules[0].Action)
}

func TestGameRulesDefiner_ValidLocalRepositoryValue_LocalRepositoryUnpackArchive(t *testing.T) {
	rulesDefiner := installationRulesDefiner{}
	game := &domain.Game{
		LocalRepository: "../../../test/files/file.zip",
	}

	rules := rulesDefiner.DefineGameRules(game)

	require.Len(t, rules, 1)
	assert.Equal(t, "../../../test/files/file.zip", rules[0].SourceValue)
	assert.Equal(t, unpackFromLocalRepository, rules[0].Action)
}

func TestGameRulesDefiner_DefineGameRules_RemoteRepository(t *testing.T) {
	rulesDefiner := installationRulesDefiner{}
	game := &domain.Game{
		RemoteRepository: "https://example.com/file.zip",
	}

	rules := rulesDefiner.DefineGameRules(game)

	require.Len(t, rules, 1)
	assert.Equal(t, "https://example.com/file.zip", rules[0].SourceValue)
	assert.Equal(t, downloadAnUnpackFromRemoteRepository, rules[0].Action)
}

func TestGameRulesDefiner_InvalidRepositoryValue_NoSourceExpected(t *testing.T) {
	rulesDefiner := installationRulesDefiner{}
	game := &domain.Game{
		RemoteRepository: "invalid-value",
	}

	rules := rulesDefiner.DefineGameRules(game)

	require.Empty(t, rules)
}

func TestGameRulesDefiner_DefineGameRules_FromSteam(t *testing.T) {
	rulesDefiner := installationRulesDefiner{}
	game := &domain.Game{
		SteamAppID: domain.SteamAppID(90),
	}

	rules := rulesDefiner.DefineGameRules(game)

	require.Len(t, rules, 1)
	assert.Equal(t, "90", rules[0].SourceValue)
	assert.Equal(t, installFromSteam, rules[0].Action)
}

func TestInstallationRuleDefiner_InvalidRemoteValueButValidLocalValue_FromLocalRepository(t *testing.T) {
	rulesDefiner := installationRulesDefiner{}
	game := &domain.Game{
		RemoteRepository: "invalid-value",
		LocalRepository:  "../../../test/files/file.zip",
	}

	rules := rulesDefiner.DefineGameRules(game)

	require.Len(t, rules, 1)
	assert.Equal(t, "../../../test/files/file.zip", rules[0].SourceValue)
	assert.Equal(t, unpackFromLocalRepository, rules[0].Action)
}

func TestInstallationRuleDefiner_InvalidLocalValueButValidRemoteValue_FromRemoteRepository(t *testing.T) {
	rulesDefiner := installationRulesDefiner{}
	game := &domain.Game{
		RemoteRepository: "https://example.com/file.zip",
		LocalRepository:  "invalid-value",
	}

	rules := rulesDefiner.DefineGameRules(game)

	require.Len(t, rules, 1)
	assert.Equal(t, "https://example.com/file.zip", rules[0].SourceValue)
	assert.Equal(t, downloadAnUnpackFromRemoteRepository, rules[0].Action)
}

func TestInstallationRuleDefiner_InvalidRepositories_FromSteam(t *testing.T) {
	rulesDefiner := installationRulesDefiner{}
	game := &domain.Game{
		RemoteRepository: "invalid-value",
		LocalRepository:  "invalid-value",
		SteamAppID:       domain.SteamAppID(90),
	}

	rules := rulesDefiner.DefineGameRules(game)

	require.Len(t, rules, 1)
	assert.Equal(t, "90", rules[0].SourceValue)
	assert.Equal(t, installFromSteam, rules[0].Action)
}

func TestInstallationRuleDefiner_ValidRepositories_ExpectRemoteLocalAndSteamRepo(t *testing.T) {
	rulesDefiner := installationRulesDefiner{}
	game := &domain.Game{
		RemoteRepository: "https://example.com/file.zip",
		LocalRepository:  "/tmp",
		SteamAppID:       domain.SteamAppID(90),
	}

	rules := rulesDefiner.DefineGameRules(game)

	require.Len(t, rules, 3)
	assert.Equal(t, "/tmp", rules[0].SourceValue)
	assert.Equal(t, copyDirectoryFromLocalRepository, rules[0].Action)
	assert.Equal(t, "https://example.com/file.zip", rules[1].SourceValue)
	assert.Equal(t, downloadAnUnpackFromRemoteRepository, rules[1].Action)
	assert.Equal(t, "90", rules[2].SourceValue)
	assert.Equal(t, installFromSteam, rules[2].Action)
}

func TestInstallationRuleDefiner_GameModValidRepositories_ExpectRemoteLocalRepos(t *testing.T) {
	rulesDefiner := installationRulesDefiner{}
	gameMod := &domain.GameMod{
		RemoteRepository: "https://example.com/file.zip",
		LocalRepository:  "/tmp",
	}

	rules := rulesDefiner.DefineGameModRules(gameMod)

	require.Len(t, rules, 2)
	assert.Equal(t, "/tmp", rules[0].SourceValue)
	assert.Equal(t, copyDirectoryFromLocalRepository, rules[0].Action)
	assert.Equal(t, "https://example.com/file.zip", rules[1].SourceValue)
	assert.Equal(t, downloadAnUnpackFromRemoteRepository, rules[1].Action)
}

func TestInstallationRuleDefiner_GameModInvalidLocalRepository_ExpectRemoteRepo(t *testing.T) {
	rulesDefiner := installationRulesDefiner{}
	gameMod := &domain.GameMod{
		RemoteRepository: "https://example.com/file.zip",
		LocalRepository:  "invalid",
	}

	rules := rulesDefiner.DefineGameModRules(gameMod)

	require.Len(t, rules, 1)
	assert.Equal(t, "https://example.com/file.zip", rules[0].SourceValue)
	assert.Equal(t, downloadAnUnpackFromRemoteRepository, rules[0].Action)
}

func TestInstallationRuleDefiner_GameModInvalidRemoteRepository_ExpectLocalRepo(t *testing.T) {
	rulesDefiner := installationRulesDefiner{}
	gameMod := &domain.GameMod{
		RemoteRepository: "invalid",
		LocalRepository:  "/tmp",
	}

	rules := rulesDefiner.DefineGameModRules(gameMod)

	require.Len(t, rules, 1)
	assert.Equal(t, "/tmp", rules[0].SourceValue)
	assert.Equal(t, copyDirectoryFromLocalRepository, rules[0].Action)
}

func TestInstallation_ServerInstalledFromRemoterRepository(t *testing.T) {
	workPath, err := ioutil.TempDir("/tmp", "gameap-daemon-test")
	defer os.RemoveAll(workPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		WorkPath: workPath,
	}
	install := newInstallServer(cfg, components.NewExecutor(), mocks.NewServerRepository())

	err = install.Execute(context.Background(), givenRemoteInstallationServer(t))

	require.Nil(t, err)
	assert.FileExists(t, workPath+"/test-server/run.sh")
	assert.FileExists(t, workPath+"/test-server/.gamemodinstalled")
}

func TestInstallation_ServerInstalledFromLocalRepository(t *testing.T) {
	workPath, err := ioutil.TempDir("/tmp", "gameap-daemon-test")
	defer os.RemoveAll(workPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		WorkPath: workPath,
	}
	install := newInstallServer(cfg, components.NewExecutor(), mocks.NewServerRepository())

	err = install.Execute(context.Background(), givenLocalInstallationServer(t))

	require.Nil(t, err)
	assert.FileExists(t, workPath+"/test-server/file.txt")
	assert.FileExists(t, workPath+"/test-server/directory_file.txt")
}

func givenRemoteInstallationServer(t *testing.T) *domain.Server {
	t.Helper()

	game := domain.Game{
		StartCode:        "test",
		RemoteRepository: "https://files.gameap.ru/test/test.tar.xz",
	}

	gameMod := domain.GameMod{
		Name:             "test",
		RemoteRepository: "https://files.gameap.ru/mod-game.tar.gz",
	}

	return givenServer(t, game, gameMod)
}

func givenLocalInstallationServer(t *testing.T) *domain.Server {
	t.Helper()

	pathToFileZip, err := filepath.Abs("../../../test/files/file.zip")
	if err != nil {
		t.Fatal(err)
	}

	pathToDirectory, err := filepath.Abs("../../../test/files/directory")
	if err != nil {
		t.Fatal(err)
	}

	game := domain.Game{
		StartCode:        "test",
		LocalRepository:  pathToFileZip,
		RemoteRepository: "https://files.gameap.ru/test/test.tar.xz",
	}

	gameMod := domain.GameMod{
		Name:             "test",
		LocalRepository:  pathToDirectory,
		RemoteRepository: "https://files.gameap.ru/mod-game.tar.gz",
	}

	return givenServer(t, game, gameMod)
}

func givenServer(t *testing.T, game domain.Game, gameMod domain.GameMod) *domain.Server {
	t.Helper()

	return domain.NewServer(
		1,
		true,
		domain.ServerInstalled,
		false,
		"name",
		"759b875e-d910-11eb-aff7-d796d7fcf7ef",
		"759b875e",
		game,
		gameMod,
		"1.3.3.7",
		1337,
		1338,
		1339,
		"paS$w0rD",
		"test-server",
		"gameap-user",
		"./run.sh",
		"",
		"",
		"",
		false,
		time.Now(),
		map[string]string{},
		map[string]string{},
		time.Now(),
	)
}
