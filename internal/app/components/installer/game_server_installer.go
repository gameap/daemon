package installer

import "github.com/gameap/daemon/internal/app/domain"

type sourceType int

type installationType int

const (
	instNoSource = iota
	instFromLocalRepo
	instFromRemoteRepo
	instFromSteam
)

const (
	instTypeInvalid = iota
	instTypeFile
	instTypeDir
)

const (
	LocalRepo = iota
	LemoteRepo
)

type RepoType int

type Repo struct {
	Type  RepoType
	Value string
}

type GameServerInstaller struct {
	GameRepo Repo
	ModRepo  Repo

	SteamSettings domain.SteamSettings

	ServerAbsolutePath string
	User               string

	gameSourceType sourceType
	modSourceType  sourceType

	gameInstallationType installationType
	modInstallationType  installationType
}

func NewGameServerInstaller(
	gameRepo Repo,
	modRepo Repo,
	steamSettings domain.SteamSettings,
	absolutePath string,
	user string,
) *GameServerInstaller {
	return &GameServerInstaller{
		GameRepo: gameRepo,
		ModRepo:  modRepo,
		SteamSettings: steamSettings,
		ServerAbsolutePath: absolutePath,
		User: user,

		gameSourceType:       instNoSource,
		modSourceType:        instNoSource,
		gameInstallationType: instTypeInvalid,
		modInstallationType:  instTypeInvalid,
	}
}

func (gsi *GameServerInstaller) Install() error {
	err := gsi.detectSources()
	if err != nil {
		return nil
	}

	return nil
}

func (gsi *GameServerInstaller) detectSources() error {
	gameSourceDetected := false

	for !gameSourceDetected {
		if gsi.gameSourceType == instNoSource {
			return noSourceToInstallGame
		}


		gameSourceDetected = true;
	}

	return nil
}

func (gsi *GameServerInstaller) detectGameSource() {

}

func (gsi *GameServerInstaller) detectModSource() {

}
