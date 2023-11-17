package gameservercommands

import (
	"context"
	"io"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/emirpasic/gods/sets/hashset"
	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/pkg/logger"
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

type installatorKind uint8

const (
	installer installatorKind = iota + 1
	updater
)

const maxSteamCMDInstallTries = 20

var repeatableSteamCMDInstallResults = hashset.New(7, 8)

var ErrDefinedNoGameInstallationRulesError = errors.New("could not determine the rules for installing the game")

var (
	errAllInstallationMethodsFailed = errors.New("all installation methods failed")
	errInstallViaSteamCMDFailed     = errors.New("failed to install via steamcmd")
	errFailedToExecuteAfterScript   = errors.New("failed to execute after installation script")
)

type installationRule struct {
	SourceValue string
	Action      installAction
}

type installServer struct {
	baseCommand

	installator *installator
	serverRepo  domain.ServerRepository
	kind        installatorKind

	installOutput                     io.ReadWriter
	serverWasActiveBeforeInstallation bool
	statusCommand                     contracts.GameServerCommand
	stopCommand                       contracts.GameServerCommand
	startCommand                      contracts.GameServerCommand
}

func newUpdateServer(
	cfg *config.Config,
	executor contracts.Executor,
	serverRepo domain.ServerRepository,
	statusCommand contracts.GameServerCommand,
	stopCommand contracts.GameServerCommand,
	startCommand contracts.GameServerCommand,
) *installServer {
	buffer := components.NewSafeBuffer()
	inst := newUpdater(cfg, executor, buffer)

	return &installServer{
		baseCommand:   newBaseCommand(cfg, executor),
		installator:   inst,
		serverRepo:    serverRepo,
		kind:          updater,
		installOutput: buffer,
		statusCommand: statusCommand,
		stopCommand:   stopCommand,
		startCommand:  startCommand,
	}
}

func newInstallServer(
	cfg *config.Config,
	executor contracts.Executor,
	serverRepo domain.ServerRepository,
	statusCommand contracts.GameServerCommand,
	stopCommand contracts.GameServerCommand,
	startCommand contracts.GameServerCommand,
) *installServer {
	buffer := components.NewSafeBuffer()
	inst := newInstallator(cfg, executor, buffer)

	return &installServer{
		baseCommand:   newBaseCommand(cfg, executor),
		installator:   inst,
		serverRepo:    serverRepo,
		kind:          installer,
		installOutput: buffer,
		statusCommand: statusCommand,
		stopCommand:   stopCommand,
		startCommand:  startCommand,
	}
}

func (cmd *installServer) Execute(ctx context.Context, server *domain.Server) error {
	defer func() {
		cmd.SetComplete()
	}()

	server.AffectInstall()

	var err error

	err = cmd.stopServerIfNeeded(ctx, server)
	if err != nil {
		return err
	}

	if cmd.cfg.Scripts.Install != "" {
		err = cmd.installByScript(ctx, server)
	} else {
		err = cmd.install(ctx, server)
	}

	if err != nil {
		cmd.SetResult(ErrorResult)
		return errors.WithMessage(err, "[game_server_commands.installServer] failed to install game server")
	}

	return cmd.startServerIfNeeded(ctx, server)
}

func (cmd *installServer) ReadOutput() []byte {
	var out []byte

	if cmd.statusCommand != nil {
		out = append(out, cmd.statusCommand.ReadOutput()...)
	}

	if cmd.stopCommand != nil {
		out = append(out, cmd.stopCommand.ReadOutput()...)
	}

	installOutput, err := io.ReadAll(cmd.installOutput)
	if err != nil {
		return nil
	}
	out = append(out, installOutput...)

	if cmd.startCommand != nil {
		out = append(out, cmd.startCommand.ReadOutput()...)
	}

	return out
}

func (cmd *installServer) stopServerIfNeeded(ctx context.Context, server *domain.Server) error {
	if server.InstallationStatus() != domain.ServerInstalled {
		return nil
	}

	err := cmd.statusCommand.Execute(ctx, server)
	if err != nil {
		return errors.WithMessage(
			err,
			"[game_server_commands.installServer] failed to check server status before installation/updating",
		)
	}

	if cmd.statusCommand.Result() != SuccessResult {
		cmd.serverWasActiveBeforeInstallation = false
		return nil
	}

	cmd.serverWasActiveBeforeInstallation = true

	err = cmd.stopCommand.Execute(ctx, server)
	if err != nil {
		return errors.WithMessage(
			err,
			"[game_server_commands.installServer] failed to stop server before installation/updating",
		)
	}

	return nil
}

func (cmd *installServer) startServerIfNeeded(ctx context.Context, server *domain.Server) error {
	if !cmd.serverWasActiveBeforeInstallation {
		return nil
	}

	err := cmd.startCommand.Execute(ctx, server)
	if err != nil {
		return errors.WithMessage(
			err,
			"[game_server_commands.installServer] failed to start server after installation/updating",
		)
	}

	return nil
}

func (cmd *installServer) installByScript(ctx context.Context, server *domain.Server) error {
	command := makeFullCommand(cmd.cfg, server, cmd.cfg.Scripts.Install, "")

	_, _ = cmd.installOutput.Write([]byte("Executing install script ...\n"))

	result, err := cmd.executor.ExecWithWriter(ctx, command, cmd.installOutput, contracts.ExecutorOptions{
		WorkDir: cmd.cfg.WorkPath,
	})

	cmd.SetComplete()
	cmd.SetResult(result)

	if err != nil {
		return errors.WithMessage(err, "[game_server_commands.installServer] failed to install by script")
	}

	if result == SuccessResult {
		_, _ = cmd.installOutput.Write([]byte("\nExecuting install script successfully completed\n"))
	} else {
		_, _ = cmd.installOutput.Write([]byte("\nExecuting script ended with an error\n"))
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

	_, _ = cmd.installOutput.Write([]byte("Installing game files ...\n"))

	err := cmd.installator.Install(ctx, server, gameRules)
	if err != nil {
		cmd.SetResult(ErrorResult)
		return err
	}

	if cmd.kind != updater {
		gameModRules := sd.DefineGameModRules(&gameMod)

		if len(gameModRules) > 0 {
			_, _ = cmd.installOutput.Write([]byte("\n\n"))
			_, _ = cmd.installOutput.Write([]byte("Installing game mod files ...\n"))

			err = cmd.installator.Install(ctx, server, gameModRules)
			if err != nil {
				cmd.SetResult(ErrorResult)
				return err
			}
		}
	}

	cmd.SetResult(SuccessResult)

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
			SourceValue: game.SteamAppID.String(),
			Action:      installFromSteam,
		}
		if game.SteamAppSetConfig != "" {
			rule.SourceValue += " " + game.SteamAppSetConfig
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

type installator struct {
	cfg      *config.Config
	executor contracts.Executor
	output   io.ReadWriter
	kind     installatorKind
}

func newInstallator(cfg *config.Config, executor contracts.Executor, output io.ReadWriter) *installator {
	return &installator{
		cfg:      cfg,
		executor: executor,
		output:   output,
		kind:     installer,
	}
}

func newUpdater(cfg *config.Config, executor contracts.Executor, output io.ReadWriter) *installator {
	return &installator{
		cfg:      cfg,
		executor: executor,
		output:   output,
		kind:     updater,
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
	dst := server.WorkDir(in.cfg)

	var err error
	switch rule.Action {
	case downloadAnUnpackFromRemoteRepository, unpackFromLocalRepository:
		err = in.getAndUnpackFiles(ctx, dst, rule.SourceValue)
	case copyDirectoryFromLocalRepository:
		err = in.copyDirectoryFromLocalRepository(ctx, dst, rule.SourceValue)
	case installFromSteam:
		err = in.installFromSteam(ctx, in.cfg, in.output, server, rule.SourceValue)
	}

	if err != nil {
		return err
	}

	err = in.chown(ctx, dst, server.User())
	if err != nil {
		err = errors.WithMessage(err, "[game_server_commands.installator] failed to chown files")
		in.writeOutput(ctx, err.Error())
		return err
	}

	err = in.runAfterInstallScript(ctx, dst)
	if err != nil {
		in.writeOutput(ctx, err.Error())
		return err
	}

	return nil
}

func (in *installator) getAndUnpackFiles(
	ctx context.Context,
	dst string,
	source string,
) error {
	c := getter.Client{
		Ctx:  ctx,
		Src:  source,
		Dst:  dst,
		Mode: getter.ClientModeAny,
	}

	in.writeOutput(ctx, "Downloading and unpacking from "+source+" to "+dst+" ...")

	err := c.Get()
	if err != nil {
		return errors.WithMessage(err, "[game_server_commands.installator] failed to download files")
	}

	in.writeOutput(ctx, "Downloading successfully completed")

	return nil
}

func (in *installator) copyDirectoryFromLocalRepository(
	ctx context.Context,
	dst string,
	source string,
) error {
	in.writeOutput(ctx, "Copying files from "+source+" to "+dst+" ...")

	err := copy.Copy(source, dst)
	if err != nil {
		err = errors.WithMessage(err, "[game_server_commands.installator] failed to copy files")
		in.writeOutput(ctx, err.Error())
		return err
	}

	in.writeOutput(ctx, "Copying files successfully completed")

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

	in.writeOutput(ctx, "Installing from steam ...")

	var installTries uint8

	executorOptions := contracts.ExecutorOptions{
		WorkDir: cfg.WorkPath,
	}

	if isRootUser() && server.User() != "" {
		systemUser, err := user.Lookup(server.User())
		if err != nil {
			err = errors.WithMessage(err, "[game_server_commands.installator] failed to lookup user")
			in.writeOutput(ctx, err.Error())
			return err
		}

		executorOptions.UID = systemUser.Uid
		executorOptions.GID = systemUser.Gid
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

	execCmd.WriteString(" +force_install_dir \"")
	execCmd.WriteString(server.WorkDir(in.cfg))
	execCmd.WriteString("\"")

	if in.cfg.SteamConfig.Login != "" && in.cfg.SteamConfig.Password != "" {
		execCmd.WriteString(" +login ")
		execCmd.WriteString(in.cfg.SteamConfig.Login)
		execCmd.WriteString(" ")
		execCmd.WriteString(in.cfg.SteamConfig.Password)
	} else {
		execCmd.WriteString(" +login anonymous")
	}

	execCmd.WriteString(" +app_update ")
	execCmd.WriteString(appID)

	if in.kind == installer {
		execCmd.WriteString(" validate")
	}

	execCmd.WriteString(" +quit")

	return execCmd.String()
}

func (in *installator) chown(ctx context.Context, dst string, userName string) error {
	if !isRootUser() {
		return nil
	}

	systemUser, err := user.Lookup(userName)
	if err != nil {
		err = errors.WithMessage(err, "[game_server_commands.installator] failed to lookup user")
		in.writeOutput(ctx, err.Error())
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

func (in *installator) runAfterInstallScript(
	ctx context.Context,
	serverPath string,
) error {
	scriptFullPath := filepath.Join(serverPath, domain.AfterInstallScriptName)
	_, err := os.Stat(scriptFullPath)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	commandScriptPath := scriptFullPath
	//nolint:goconst
	if runtime.GOOS == "windows" {
		commandScriptPath = "powershell " + commandScriptPath //nolint
	}

	if in.kind == installer {
		in.writeOutput(ctx, "Executing after install script")
		result, err := in.executor.ExecWithWriter(ctx, scriptFullPath, in.output, contracts.ExecutorOptions{
			WorkDir: serverPath,
		})
		if err != nil {
			return err
		}
		if result != SuccessResult {
			return errFailedToExecuteAfterScript
		}
	}

	err = os.Remove(scriptFullPath)
	if err != nil {
		return err
	}

	return nil
}

func (in *installator) writeOutput(ctx context.Context, line string) {
	_, err := in.output.Write([]byte(line))
	if err != nil {
		logger.Error(ctx, err)
	}

	_, err = in.output.Write([]byte("\n"))
	if err != nil {
		logger.Error(ctx, err)
	}
}
