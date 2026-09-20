package grpc

import (
	"context"
	"testing"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/test/mocks"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testServerID  = 1
	testGameModID = 10
)

func newTestServerHandler(vars []*pb.GameModVar) (*GRPCServerHandler, *mocks.ServerRepository) {
	store := NewGameStore()
	store.UpdateGames([]*pb.Game{{Code: "q2", Name: "Quake 2"}})
	store.UpdateGameMods([]*pb.GameMod{{Id: testGameModID, GameCode: "q2", Name: "Default", Vars: vars}})

	repo := mocks.NewServerRepository()

	return NewGRPCServerHandler(repo, store), repo
}

func newTestProtoServer(startCommand, varsJSON string) *pb.Server {
	srv := &pb.Server{
		Id:           testServerID,
		Uuid:         "759b875e-d910-11eb-aff7-d796d7fcf7ef",
		UuidShort:    "759b875e",
		Enabled:      true,
		Installed:    pb.ServerInstalledStatus_SERVER_INSTALLED_STATUS_INSTALLED,
		Name:         "Quake 2 server",
		GameId:       "q2",
		GameModId:    testGameModID,
		ServerIp:     "127.0.0.1",
		ServerPort:   27910,
		Dir:          "servers/q2",
		StartCommand: new(startCommand),
	}

	if varsJSON != "" {
		srv.Vars = new(varsJSON)
	}

	return srv
}

func handledServer(
	t *testing.T,
	handler *GRPCServerHandler,
	repo *mocks.ServerRepository,
	srv *pb.Server,
	settings []*pb.ServerSetting,
) *domain.Server {
	t.Helper()

	require.NoError(t, handler.HandleServerConfigUpdate(context.Background(), srv, settings))

	server, found := repo.FindByIDFromCache(testServerID)
	require.True(t, found, "server must be cached after a config update")

	return server
}

func TestHandleServerConfigUpdate_ValuesAreUsedVerbatim(t *testing.T) {
	cfg := &config.Config{WorkPath: "/work-path"}

	tests := []struct {
		name         string
		modVars      []*pb.GameModVar
		varsJSON     string
		settings     []*pb.ServerSetting
		startCommand string
		wantRendered string
		wantArgs     []string
		wantEnv      map[string]string
	}{
		{
			name:         "bool_true_value_text",
			modVars:      []*pb.GameModVar{{Var: "ase", Default: "0"}},
			settings:     []*pb.ServerSetting{{Name: "ase", Value: "1"}},
			startCommand: "./run.sh --ase={ase}",
			wantRendered: "./run.sh --ase=1",
			wantArgs:     []string{"./run.sh", "--ase=1"},
			wantEnv:      map[string]string{"ASE": "1"},
		},
		{
			name:         "bool_false_value_text_is_not_parsed_as_truthiness",
			modVars:      []*pb.GameModVar{{Var: "ase", Default: "1"}},
			settings:     []*pb.ServerSetting{{Name: "ase", Value: "0"}},
			startCommand: "./run.sh --ase={ase}",
			wantRendered: "./run.sh --ase=0",
			wantArgs:     []string{"./run.sh", "--ase=0"},
			wantEnv:      map[string]string{"ASE": "0"},
		},
		{
			name:         "empty_setting_overrides_a_non_empty_default",
			modVars:      []*pb.GameModVar{{Var: "map", Default: "q2dm1"}},
			settings:     []*pb.ServerSetting{{Name: "map", Value: ""}},
			startCommand: "./run.sh +map {map}",
			wantRendered: "./run.sh +map ",
			wantArgs:     []string{"./run.sh", "+map", ""},
			wantEnv:      map[string]string{"MAP": ""},
		},
		{
			name: "numeric_and_boolean_looking_text_is_untouched",
			modVars: []*pb.GameModVar{
				{Var: "code", Default: "1"},
				{Var: "ratio", Default: "1"},
				{Var: "debug", Default: "false"},
			},
			settings: []*pb.ServerSetting{
				{Name: "code", Value: "007"},
				{Name: "ratio", Value: "1.50"},
				{Name: "debug", Value: "true"},
			},
			startCommand: "./run.sh -code {code} -ratio {ratio} -debug {debug}",
			wantRendered: "./run.sh -code 007 -ratio 1.50 -debug true",
			wantArgs:     []string{"./run.sh", "-code", "007", "-ratio", "1.50", "-debug", "true"},
			wantEnv:      map[string]string{"CODE": "007", "RATIO": "1.50", "DEBUG": "true"},
		},
		{
			name:         "numeric_text_from_server_vars_is_untouched",
			modVars:      []*pb.GameModVar{{Var: "code", Default: "1"}},
			varsJSON:     `{"code":"007"}`,
			startCommand: "./run.sh -code {code}",
			wantRendered: "./run.sh -code 007",
			wantArgs:     []string{"./run.sh", "-code", "007"},
			wantEnv:      map[string]string{"CODE": "007"},
		},
		{
			name:         "uppercase_pelican_variable_name",
			modVars:      []*pb.GameModVar{{Var: "SERVER_TOKEN", Default: ""}},
			settings:     []*pb.ServerSetting{{Name: "SERVER_TOKEN", Value: "abc"}},
			startCommand: "./run.sh +token {SERVER_TOKEN} +token {server_token}",
			wantRendered: "./run.sh +token abc +token abc",
			wantArgs:     []string{"./run.sh", "+token", "abc", "+token", "abc"},
			wantEnv:      map[string]string{"SERVER_TOKEN": "abc"},
		},
		{
			name:         "empty_default_stays_an_empty_argument",
			modVars:      []*pb.GameModVar{{Var: "server_token", Default: ""}},
			startCommand: "./srcds_run +sv_setsteamaccount {server_token} +map de_dust2",
			wantRendered: "./srcds_run +sv_setsteamaccount  +map de_dust2",
			wantArgs:     []string{"./srcds_run", "+sv_setsteamaccount", "", "+map", "de_dust2"},
			wantEnv:      map[string]string{"SERVER_TOKEN": ""},
		},
		{
			name:         "quake2_game_variable_is_not_shadowed_by_the_game_placeholder",
			modVars:      []*pb.GameModVar{{Var: "game", Default: "baseq2"}},
			startCommand: "./run.sh +set game {game} +map q2dm1",
			wantRendered: "./run.sh +set game baseq2 +map q2dm1",
			wantArgs:     []string{"./run.sh", "+set", "game", "baseq2", "+map", "q2dm1"},
			wantEnv:      map[string]string{"GAME": "baseq2"},
		},
		{
			name:         "password_with_spaces_stays_a_single_argument",
			modVars:      []*pb.GameModVar{{Var: "password", Default: ""}},
			settings:     []*pb.ServerSetting{{Name: "password", Value: "p@ss w0rd"}},
			startCommand: `./run.sh --password="{password}"`,
			wantRendered: `./run.sh --password="p@ss w0rd"`,
			wantArgs:     []string{"./run.sh", "--password=p@ss w0rd"},
			wantEnv:      map[string]string{"PASSWORD": "p@ss w0rd"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, repo := newTestServerHandler(tt.modVars)

			server := handledServer(t, handler, repo, newTestProtoServer(tt.startCommand, tt.varsJSON), tt.settings)

			rendered, err := domain.ReplaceShortCodes(server.StartCommand(), cfg, server)
			require.NoError(t, err)
			assert.Equal(t, tt.wantRendered, rendered)

			args, err := domain.BuildCommandArgs(cfg, server, "{command}", server.StartCommand())
			require.NoError(t, err)
			assert.Equal(t, tt.wantArgs, args)

			env, err := server.EnvironmentVars(cfg)
			require.NoError(t, err)
			for key, want := range tt.wantEnv {
				assert.Equal(t, want, env[key], "env %s", key)
			}
		})
	}
}

func TestHandleServerConfigUpdate_SettingsWinOverVarsWinOverDefaults(t *testing.T) {
	handler, repo := newTestServerHandler([]*pb.GameModVar{
		{Var: "from_default", Default: "default"},
		{Var: "from_vars", Default: "default"},
		{Var: "from_settings", Default: "default"},
	})
	srv := newTestProtoServer("./run.sh", `{"from_vars":"vars","from_settings":"vars"}`)

	server := handledServer(t, handler, repo, srv, []*pb.ServerSetting{{Name: "from_settings", Value: "settings"}})

	assert.Equal(t, map[string]string{
		"from_default":  "default",
		"from_vars":     "vars",
		"from_settings": "settings",
	}, server.Vars())
}

func TestHandleServerConfigUpdate_SettingsArePreservedWhenNotSent(t *testing.T) {
	handler, repo := newTestServerHandler([]*pb.GameModVar{{Var: "map", Default: "q2dm1"}})
	srv := newTestProtoServer("./run.sh", "")
	initial := []*pb.ServerSetting{{Name: "map", Value: "q2dm2"}}

	server := handledServer(t, handler, repo, srv, initial)
	require.Equal(t, domain.Settings{"map": "q2dm2"}, server.AllSettings())

	require.NoError(t, handler.HandleServerUpdate(context.Background(), srv))
	assert.Equal(t, domain.Settings{"map": "q2dm2"}, server.AllSettings(),
		"a server_config message without settings keeps the existing ones")

	server = handledServer(t, handler, repo, srv, []*pb.ServerSetting{})
	assert.Equal(t, domain.Settings{"map": "q2dm2"}, server.AllSettings(),
		"an empty settings list keeps the existing ones")

	server = handledServer(t, handler, repo, srv, []*pb.ServerSetting{{Name: "other", Value: "x"}})
	assert.Equal(t, domain.Settings{"other": "x"}, server.AllSettings(),
		"a non-empty settings list replaces the existing ones wholesale")
}
