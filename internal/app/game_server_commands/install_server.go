package game_server_commands

import (
	"context"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/emirpasic/gods/sets/hashset"
	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/hashicorp/go-getter"
	"github.com/otiai10/copy"
	log "github.com/sirupsen/logrus"
)

type installAction int8

const (
	unknownAction installAction = iota
	downloadAnUnpackFromRemoteRepository
	unpackFromLocalRepository
	copyDirectoryFromLocalRepository
	installFromSteam
)

const maxSteamCMDInstallTries = 3

var repeatableSteamCMDInstallResults = hashset.New(7, 8)

type installationRule struct {
	SourceValue string
	Action      installAction
}

type installServer struct {
	baseCommand
	bufCommand

	installator *installator
}

func newInstallServer(cfg *config.Config) *installServer {
	buffer := components.NewSafeBuffer()
	installator := newInstallator(cfg, buffer)

	return &installServer{
		baseCommand{
			cfg:      cfg,
			complete: false,
			result:   UnknownResult,
		},
		bufCommand{output: buffer},
		installator,
	}
}

func (cmd *installServer) Execute(ctx context.Context, server *domain.Server) error {
	defer func() {
		cmd.complete = true
	}()

	err := cmd.install(ctx, server)
	if err != nil {
		return err
	}

	return nil
}

func (cmd *installServer) install(ctx context.Context, server *domain.Server) error {
	sd := installationRulesDefiner{}

	game := server.Game()
	gameMod := server.GameMod()

	gameRules := sd.DefineGameRules(&game)
	gameModRules := sd.DefineGameModRules(&gameMod)

	err := cmd.installator.Install(ctx, server, gameRules)
	if err != nil {
		return err
	}

	err = cmd.installator.Install(ctx, server, gameModRules)
	if err != nil {
		return err
	}

	return nil
}

type installationRulesDefiner struct{}

func (d *installationRulesDefiner) DefineGameRules(game *domain.Game) []*installationRule {
	var rules []*installationRule

	if game.LocalRepository != "" {
		rule := d.defineLocalRepositoryRule(game.LocalRepository)
		if rule != nil {
			rules = append(rules, rule)
		}
	}

	if game.RemoteRepository != "" {
		rule := d.defineRemoteRepositoryRule(game.RemoteRepository)
		if rule != nil {
			rules = append(rules, rule)
		}
	}

	if game.SteamAppID > 0 {
		rule := &installationRule{
			SourceValue: strconv.Itoa(game.SteamAppID),
			Action: installFromSteam,
		}
		rules = append(rules, rule)
	}

	return rules
}

func (d *installationRulesDefiner) defineLocalRepositoryRule(localRepository string) *installationRule {
	rule := &installationRule{
		SourceValue: localRepository,
	}

	stat, err := os.Stat(localRepository)
	if err != nil {
		log.Warning(err)
		return nil
	}

	if stat.Mode().IsDir() {
		rule.Action = copyDirectoryFromLocalRepository
	} else if stat.Mode().IsRegular() {
		rule.Action = unpackFromLocalRepository
	}

	return rule
}


func (d *installationRulesDefiner) defineRemoteRepositoryRule(remoteRepository string) *installationRule {
	rule := &installationRule{
		SourceValue: remoteRepository,
	}

	parsedURL, err := url.Parse(remoteRepository)
	if err != nil {
		log.Warning(err)
		return nil
	}

	if parsedURL.Host == "" {
		return nil
	}

	rule.Action = downloadAnUnpackFromRemoteRepository

	return rule
}

func (d *installationRulesDefiner) DefineGameModRules(gameMod *domain.GameMod) []*installationRule {
	var rules []*installationRule

	if gameMod.LocalRepository != "" {
		rule := d.defineLocalRepositoryRule(gameMod.LocalRepository)
		if rule != nil {
			rules = append(rules, rule)
		}
	}

	if gameMod.RemoteRepository != "" {
		rule := d.defineRemoteRepositoryRule(gameMod.RemoteRepository)
		if rule != nil {
			rules = append(rules, rule)
		}
	}

	return rules
}

type installator struct{
	cfg *config.Config
	output io.ReadWriter
}

func newInstallator(cfg *config.Config, output io.ReadWriter) *installator {
	return &installator{
		cfg, output,
	}
}

func (in *installator) Install(ctx context.Context, server *domain.Server, rules []*installationRule) error {
	var err error
	for _, rule := range rules {
		err = in.install(ctx, server, *rule)
		if err != nil {
			return err
		}
	}

	return nil
}

func (in *installator) install(ctx context.Context, server *domain.Server, rule installationRule) error {
	switch rule.Action {
	case downloadAnUnpackFromRemoteRepository, unpackFromLocalRepository:
		return in.getAndUnpackFiles(ctx, server, rule.SourceValue)
	case copyDirectoryFromLocalRepository:
		return in.copyDirectoryFromLocalRepository(ctx, server, rule.SourceValue)
	case installFromSteam:
		return in.installFromSteam(ctx, in.cfg, in.output, server, rule.SourceValue)
	}

	return nil
}

func (in *installator) getAndUnpackFiles(
	ctx context.Context,
	server *domain.Server,
	source string,
) error {
	c := getter.Client{
		Ctx: ctx,
		Src: source,
		Dst: makeFullServerPath(in.cfg, server.Dir()),
		Mode: getter.ClientModeAny,
	}

	err := c.Get()
	if err != nil {
		return err
	}

	return nil
}

func (in *installator) copyDirectoryFromLocalRepository(
	ctx context.Context,
	server *domain.Server,
	source string,
) error {
	dst := makeFullServerPath(in.cfg, server.Dir())

	err := copy.Copy(source, dst)
	if err != nil {
		return err
	}

	return nil
}

func (in *installator) installFromSteam(
	ctx context.Context,
	cfg *config.Config,
	output io.Writer,
	server *domain.Server,
	source string,
) error {
	execCmd := strings.Builder{}

	execCmd.WriteString(cfg.SteamCMDPath)
	execCmd.WriteString("/")
	execCmd.WriteString(config.SteamCMDExecutableFile)
	execCmd.WriteString(" +login anonymous")

	execCmd.WriteString(" +force_install_dir \"")
	execCmd.WriteString(makeFullServerPath(cfg, server.Dir()))
	execCmd.WriteString("\"")

	execCmd.WriteString(" +app_update ")
	execCmd.WriteString(source)
	execCmd.WriteString("  +quit")

	var installTries uint8 = 0
	for installTries < maxSteamCMDInstallTries {
		result, err := components.ExecWithWriter(
			ctx,
			execCmd.String(),
			cfg.WorkPath,
			output,
		)
		if err != nil {
			return err
		}

		if result == SuccessResult {
			break;
		}

		if !repeatableSteamCMDInstallResults.Contains(int8(result)) {
			break;
		}

		installTries++
	}

	return nil
}
