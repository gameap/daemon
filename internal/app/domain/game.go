package domain

type Game struct {
	Code             string `json:"code"`
	StartCode        string `json:"start_code"`
	Name             string `json:"name"`
	Engine           string `json:"engine"`
	EngineVersion    string `json:"engine_version"`
	SteamAppID       int    `json:"steam_app_id"`
	SteamSettings    SteamSettings
	RemoteRepository string `json:"remote_repository"`
	LocalRepository  string `json:"local_repository"`
}

type SteamSettings struct {
	SteamAPPID        int
	SteamAPPSetConfig string
}

type GameMod struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	RemoteRepository string `json:"remote_repository"`
	LocalRepository  string `json:"local_repository"`

	DefaultStartCMDLinux   string `json:"default_start_cmd_linux"`
	DefaultStartCMDWindows string `json:"default_start_cmd_windows"`
}
