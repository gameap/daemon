package domain

import (
	"encoding/json"
	"strconv"
	"strings"
)

type SteamAppID int

func (appID *SteamAppID) UnmarshalJSON(bytes []byte) error {
	if bytes[0] == '"' {
		id := strings.Trim(string(bytes), "\"")

		sID, err := strconv.Atoi(id)
		if err != nil {
			return err
		}

		*appID = SteamAppID(sID)
	} else {
		var id int
		err := json.Unmarshal(bytes, &id)
		if err != nil {
			return err
		}

		*appID = SteamAppID(id)
	}

	return nil
}

func (appID SteamAppID) String() string {
	return strconv.Itoa(int(appID))
}

type Game struct {
	Code              string     `json:"code"`
	StartCode         string     `json:"start_code"`
	Name              string     `json:"name"`
	Engine            string     `json:"engine"`
	EngineVersion     string     `json:"engine_version"`
	SteamAppSetConfig string     `json:"steam_app_set_config"`
	RemoteRepository  string     `json:"remote_repository"`
	LocalRepository   string     `json:"local_repository"`
	SteamAppID        SteamAppID `json:"steam_app_id"`
}

type SteamSettings struct {
	AppSetConfig string
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

	switch defaultValue := v.Default.(type) {
	case string:
		g.DefaultValue = defaultValue
	case int:
		g.DefaultValue = strconv.Itoa(defaultValue)
	case float64:
		g.DefaultValue = strconv.Itoa(int(defaultValue))
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
	Name                   string               `json:"name"`
	RemoteRepository       string               `json:"remote_repository"`
	LocalRepository        string               `json:"local_repository"`
	DefaultStartCMDLinux   string               `json:"default_start_cmd_linux"`
	DefaultStartCMDWindows string               `json:"default_start_cmd_windows"`
	Vars                   []GameModVarTemplate `json:"vars"`
	ID                     int                  `json:"id"`
}
