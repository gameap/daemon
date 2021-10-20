package domain

type Game struct {
	Code             string
	StartCode        string
	Name             string
	Engine           string
	EngineVersion    string
	SteamAppID	     int
	SteamSettings    SteamSettings
	RemoteRepository string
	LocalRepository  string
}
