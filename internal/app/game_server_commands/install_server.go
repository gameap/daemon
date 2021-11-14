package game_server_commands

import (
	"context"
	"io"
	"net/url"
	"os"
	"os/user"
	"strconv"
	"strings"

	"github.com/emirpasic/gods/sets/hashset"
	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/interfaces"
	"github.com/hashicorp/go-getter"
	"github.com/otiai10/copy"
	"github.com/pkg/errors"
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

var ErrDefinedNoGameInstallationRulesError = errors.New("could not determine the rules for installing the game")

var (
	errAllInstallationMethodsFailed = errors.New("all installation methods failed")
	errInstallViaSteamCMDFailed     = errors.New("failed to install via steamcmd")
)

type installationRule struct {
	SourceValue string
	Action      installAction
}

type installServer struct {
	baseCommand
	bufCommand

	installator *installator
	serverRepo  domain.ServerRepository
}

func newInstallServer(cfg *config.Config, executor interfaces.Executor, serverRepo domain.ServerRepository) *installServer {
	buffer := components.NewSafeBuffer()
	inst := newInstallator(cfg, executor, buffer)

	return &installServer{
		baseCommand{
			cfg:      cfg,
			executor: executor,
			complete: false,
			result:   UnknownResult,
		},
		bufCommand{output: buffer},
		inst,
		serverRepo,
	}
}

func (cmd *installServer) Execute(ctx context.Context, server *domain.Server) error {
	defer func() {
		err := cmd.defineAndSaveServerStatus(ctx, server)
		if err != nil {
			log.Error(err)
		}
	}()
	defer func() {
		cmd.complete = true
	}()

	var err error

	err = cmd.setServerIntallationStatus(ctx, server, domain.ServerInstallInProcess)
	if err != nil {
		return err
	}

	if cmd.cfg.Scripts.Install != "" {
		err = cmd.installByScript(ctx, server)
	} else {
		err = cmd.install(ctx, server)
	}

	if err != nil {
		cmd.result = ErrorResult
		return errors.WithMessage(err, "[game_server_commands.installServer] failed to install game server")
	}

	return nil
}

func (cmd *installServer) installByScript(ctx context.Context, server *domain.Server) error {
	command := makeFullCommand(cmd.cfg, server, cmd.cfg.Scripts.Install, "")

	_, _ = cmd.output.Write([]byte("Executing install script ...\n"))

	var err error
	cmd.result, err = cmd.executor.ExecWithWriter(ctx, command, cmd.output, components.ExecutorOptions{
		WorkDir: cmd.cfg.WorkPath,
	})

	cmd.complete = true
	if err != nil {
		return errors.WithMessage(err, "[game_server_commands.installServer] failed to install by script")
	}

	if cmd.result == SuccessResult {
		_, _ = cmd.output.Write([]byte("\nExecuting install script successfully completed\n"))
	} else {
		_, _ = cmd.output.Write([]byte("\nExecuting script ended with an error\n"))
	}

	return nil
}

func (cmd *installServer) install(ctx context.Context, server *domain.Server) error {
	sd := installationRulesDefiner{}

	game := server.Game()
	gameMod := server.GameMod()

	gameRules := sd.DefineGameRules(&game)
	if len(gameRules) == 0 {
		return ErrDefinedNoGameInstallationRulesError
	}

	_, _ = cmd.output.Write([]byte("Installing game files ...\n"))

	err := cmd.installator.Install(ctx, server, gameRules)
	if err != nil {
		cmd.result = ErrorResult
		return err
	}

	gameModRules := sd.DefineGameModRules(&gameMod)

	if len(gameModRules) > 0 {
		_, _ = cmd.output.Write([]byte("\n\n"))
		_, _ = cmd.output.Write([]byte("Installing game mod files ...\n"))

		err = cmd.installator.Install(ctx, server, gameModRules)
		if err != nil {
			cmd.result = ErrorResult
			return err
		}
	}

	cmd.result = SuccessResult

	return nil
}

func (cmd *installServer) setServerIntallationStatus(
	ctx context.Context,
	server *domain.Server,
	status domain.InstallationStatus,
) error {
	server.SetInstallationStatus(status)
	return cmd.serverRepo.Save(ctx, server)
}

func (cmd *installServer) defineAndSaveServerStatus(ctx context.Context, server *domain.Server) error {
	switch {
	case !cmd.IsComplete():
		server.SetInstallationStatus(domain.ServerInstallInProcess)
	case cmd.Result() == 0:
		server.SetInstallationStatus(domain.ServerInstalled)
	case cmd.Result() != 0:
		server.SetInstallationStatus(domain.ServerNotInstalled)
	}

	return cmd.serverRepo.Save(ctx, server)
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
			Action:      installFromSteam,
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

type installatorKind uint8

const (
	installer installatorKind = iota + 1
	updater
	reinstaller
)

type installator struct {
	cfg      *config.Config
	executor interfaces.Executor
	output   io.ReadWriter
	kind     installatorKind
}

func newInstallator(cfg *config.Config, executor interfaces.Executor, output io.ReadWriter) *installator {
	return &installator{
		cfg:      cfg,
		executor: executor,
		output:   output,
		kind:     installer,
	}
}

func (in *installator) Install(ctx context.Context, server *domain.Server, rules []*installationRule) error {
	var err error
	var success bool

	for _, rule := range rules {
		if rule.Action == unknownAction {
			log.Error("Unknown action found suddenly")
			continue
		}

		err = in.install(ctx, server, *rule)
		if err != nil {
			_, _ = in.output.Write([]byte(err.Error() + "\n"))
			log.Error(err)
			continue
		}

		success = true
		break
	}

	if err != nil {
		return err
	}

	if !success {
		return errAllInstallationMethodsFailed
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
	dst := makeFullServerPath(in.cfg, server.Dir())
	c := getter.Client{
		Ctx:  ctx,
		Src:  source,
		Dst:  dst,
		Mode: getter.ClientModeAny,
	}

	in.writeOutput("Downloading and unpacking from " + source + " to " + dst + " ...")

	err := c.Get()
	if err != nil {
		return errors.WithMessage(err, "[game_server_commands.installator] failed to download files")
	}

	err = in.chown(dst, server.User())
	if err != nil {
		err = errors.WithMessage(err, "[game_server_commands.installator] failed to chown files")
		in.writeOutput(err.Error())
		return err
	}

	in.writeOutput("Downloading successfully completed")

	return nil
}

func (in *installator) copyDirectoryFromLocalRepository(
	_ context.Context,
	server *domain.Server,
	source string,
) error {
	dst := makeFullServerPath(in.cfg, server.Dir())

	in.writeOutput("Copying files from " + source + " to " + dst + " ...")

	err := copy.Copy(source, dst)
	if err != nil {
		err = errors.WithMessage(err, "[game_server_commands.installator] failed to copy files")
		in.writeOutput(err.Error())
		return err
	}

	err = in.chown(dst, server.User())
	if err != nil {
		err = errors.WithMessage(err, "[game_server_commands.installator] failed to chown files")
		in.writeOutput(err.Error())
		return err
	}

	in.writeOutput("Copying files successfully completed")

	return nil
}

func (in *installator) installFromSteam(
	ctx context.Context,
	cfg *config.Config,
	output io.Writer,
	server *domain.Server,
	source string,
) error {
	execCmd := in.makeSteamCMDCommand(source, server)

	in.writeOutput("Installing from steam ...")

	var installTries uint8 = 0

	executorOptions := components.ExecutorOptions{
		WorkDir: cfg.WorkPath,
	}

	if isRootUser() && server.User() != "" {
		systemUser, err := user.Lookup(server.User())
		if err != nil {
			err = errors.WithMessage(err, "[game_server_commands.installator] failed to lookup user")
			in.writeOutput(err.Error())
			return err
		}

		executorOptions.Uid = systemUser.Uid
		executorOptions.Gid = systemUser.Gid
	}

	var result int
	var err error
	for installTries < maxSteamCMDInstallTries {
		result, err = in.executor.ExecWithWriter(
			ctx,
			execCmd,
			output,
			executorOptions,
		)
		if err != nil {
			return err
		}

		if result == SuccessResult {
			break
		}

		if !repeatableSteamCMDInstallResults.Contains(int8(result)) {
			break
		}

		installTries++
	}

	if result != SuccessResult {
		return errInstallViaSteamCMDFailed
	}

	return nil
}

func (in *installator) makeSteamCMDCommand(appID string, server *domain.Server) string {
	execCmd := strings.Builder{}

	execCmd.WriteString(in.cfg.SteamCMDPath)
	execCmd.WriteString("/")
	execCmd.WriteString(config.SteamCMDExecutableFile)
	execCmd.WriteString(" +login anonymous")

	execCmd.WriteString(" +force_install_dir \"")
	execCmd.WriteString(makeFullServerPath(in.cfg, server.Dir()))
	execCmd.WriteString("\"")

	execCmd.WriteString(" +app_update ")
	execCmd.WriteString(appID)
	execCmd.WriteString(" +quit")

	return execCmd.String()
}

func (in *installator) chown(dst string, userName string) error {
	if !isRootUser() {
		return nil
	}

	systemUser, err := user.Lookup(userName)
	if err != nil {
		err = errors.WithMessage(err, "[game_server_commands.installator] failed to lookup user")
		in.writeOutput(err.Error())
		return err
	}

	uid, err := strconv.Atoi(systemUser.Uid)
	if err != nil {
		return errors.WithMessage(err, "[game_server_commands.installator] invalid user uid")
	}
	gid, err := strconv.Atoi(systemUser.Uid)
	if err != nil {
		return errors.WithMessage(err, "[game_server_commands.installator] invalid user gid")
	}
	err = chownR(dst, uid, gid)
	if err != nil {
		return err
	}

	return nil
}

func (in *installator) writeOutput(line string) {
	_, err := in.output.Write([]byte(line + "\n"))
	if err != nil {
		log.Error(err)
	}
}
