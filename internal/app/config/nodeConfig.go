package config

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/interfaces"
	"github.com/pkg/errors"
)

type NodeConfigInitializer struct {
	client interfaces.APIRequestMaker
}

func NewNodeConfigInitializer(client interfaces.APIRequestMaker) *NodeConfigInitializer {
	return &NodeConfigInitializer{client: client}
}

type nodeInitial struct {
	WorkPath            string `json:"work_path"`
	SteamCMDPath        string `json:"steamcmd_path"`
	PreferInstallMethod string `json:"prefer_install_method"`
	ScriptInstall       string `json:"script_install"`
	ScriptReinstall     string `json:"script_reinstall"`
	ScriptUpdate        string `json:"script_update"`
	ScriptStart         string `json:"script_start"`
	ScriptPause         string `json:"script_pause"`
	ScriptUnpause       string `json:"script_unpause"`
	ScriptStop          string `json:"script_stop"`
	ScriptKill          string `json:"script_kill"`
	ScriptRestart       string `json:"script_restart"`
	ScriptStatus        string `json:"script_status"`
	ScriptGetConsole    string `json:"script_get_console"`
	ScriptSendCommand   string `json:"script_send_command"`
	ScriptDelete        string `json:"script_delete"`
}

func (ncu *NodeConfigInitializer) Initialize(ctx context.Context, cfg *Config) error {
	ncu.initDefault(cfg)

	resp, err := ncu.client.Request(ctx, domain.APIRequest{
		Method: http.MethodGet,
		URL:    "gdaemon_api/dedicated_servers/get_init_data/{id}",
		PathParams: map[string]string{
			"id": strconv.FormatInt(int64(cfg.NodeID), 10),
		},
	})

	if err != nil {
		return errors.WithMessage(err, "[app.nodeConfigInitializer] failed to get node config")
	}

	if resp.StatusCode() != http.StatusOK {
		return errors.New("[app.nodeConfigInitializer] failed to get node initialization data")
	}

	initial := nodeInitial{}

	err = json.Unmarshal(resp.Body(), &initial)
	if err != nil {
		return errors.WithMessage(err, "[app.nodeConfigInitializer] failed to unmarshal node initialization data")
	}

	cfg.WorkPath = initial.WorkPath
	cfg.SteamCMDPath = initial.SteamCMDPath

	if cfg.Scripts.Install == "" {
		cfg.Scripts.Install = initial.ScriptInstall
	}

	if cfg.Scripts.Reinstall == "" {
		cfg.Scripts.Reinstall = initial.ScriptReinstall
	}

	if cfg.Scripts.Update == "" {
		cfg.Scripts.Update = initial.ScriptUpdate
	}

	if cfg.Scripts.Start == "" {
		cfg.Scripts.Start = initial.ScriptStart
	}

	if cfg.Scripts.Pause == "" {
		cfg.Scripts.Pause = initial.ScriptPause
	}

	if cfg.Scripts.Unpause == "" {
		cfg.Scripts.Unpause = initial.ScriptUnpause
	}

	if cfg.Scripts.Stop == "" {
		cfg.Scripts.Stop = initial.ScriptStop
	}

	if cfg.Scripts.Kill == "" {
		cfg.Scripts.Kill = initial.ScriptKill
	}

	if cfg.Scripts.Restart == "" {
		cfg.Scripts.Restart = initial.ScriptRestart
	}

	if cfg.Scripts.Start == "" {
		cfg.Scripts.Status = initial.ScriptStatus
	}

	if cfg.Scripts.GetConsole == "" {
		cfg.Scripts.GetConsole = initial.ScriptGetConsole
	}

	if cfg.Scripts.SendCommand == "" {
		cfg.Scripts.SendCommand = initial.ScriptSendCommand
	}

	if cfg.Scripts.Delete == "" {
		cfg.Scripts.Delete = initial.ScriptDelete
	}

	return nil
}

func (ncu *NodeConfigInitializer) initDefault(cfg *Config) {
	cfg.Scripts.Start = DefaultGameServerScriptStart
	cfg.Scripts.Stop = DefaultGameServerScriptStop
	cfg.Scripts.Restart = DefaultGameServerScriptRestart
	cfg.Scripts.Status = DefaultGameServerScriptStatus
	cfg.Scripts.GetConsole = DefaultGameServerScriptGetOutput
	cfg.Scripts.SendCommand = DefaultGameServerScriptSendInput
}
