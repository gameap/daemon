package domain

import (
	"encoding/json"
	"strconv"
)

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

type GameModVarTemplate struct {
	Key          string
	DefaultValue string
}

func (g *GameModVarTemplate) UnmarshalJSON(bytes []byte) error {
	v := struct {
		Var      string      `json:"var"`
		Default  interface{} `json:"default"`
		Info     string      `json:"info"`
		AdminVar bool        `json:"admin_var"`
	}{}

	err := json.Unmarshal(bytes, &v)
	if err != nil {
		return err
	}

	g.Key = v.Var

	switch v.Default.(type) {
	case string:
		g.DefaultValue = v.Default.(string)
	case int:
		g.DefaultValue = strconv.Itoa(v.Default.(int))
	case float64:
		g.DefaultValue = strconv.Itoa(int(v.Default.(float64)))
	case bool:
		if v.Default.(bool) {
			g.DefaultValue = "1"
		} else {
			g.DefaultValue = "0"
		}
	default:
		g.DefaultValue = ""
	}

	return nil
}

type GameMod struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	RemoteRepository string `json:"remote_repository"`
	LocalRepository  string `json:"local_repository"`

	Vars []GameModVarTemplate `json:"vars"`

	DefaultStartCMDLinux   string `json:"default_start_cmd_linux"`
	DefaultStartCMDWindows string `json:"default_start_cmd_windows"`
}
