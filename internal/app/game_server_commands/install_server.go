package gameservercommands

import (
	"context"
	"crypto/rand"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/emirpasic/gods/sets/hashset"
	"github.com/gameap/daemon/internal/app/archive"
	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/fetch"
	"github.com/gameap/daemon/internal/app/fsutil"
	"github.com/gameap/daemon/internal/app/osowner"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/pkg/errors"

	pb "github.com/gameap/gameap/pkg/proto"
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

const (
	// The server directory is created with these modes when an installation
	// finds none. steamcmd runs as the game server user and gets a directory
	// nobody else looks into.
	serverDirPerm      os.FileMode = 0o755
	steamServerDirPerm os.FileMode = 0o750

	// downloadedFilePerm is the mode of a downloaded file that is kept as it is.
	downloadedFilePerm os.FileMode = 0o644

	// maxReportedSkippedLinks bounds the lines one archive adds to the
	// installation output for the symlinks it was not allowed to create.
	maxReportedSkippedLinks = 20

	// A repository file that is kept as it is, is written under a name made
	// of these and a random part between them until it is complete, see
	// storeFile.
	stagedFilePrefix = ".gameap-install-"
	stagedFileSuffix = ".part"
)

var repeatableSteamCMDInstallResults = hashset.New(7, 8)

var ErrDefinedNoGameInstallationRulesError = errors.New("could not determine the rules for installing the game")

var (
	errAllInstallationMethodsFailed = errors.New("all installation methods failed")
	errInstallViaSteamCMDFailed     = errors.New("failed to install via steamcmd")
	errFailedToExecuteAfterScript   = errors.New("failed to execute after installation script")
	errUnknownInstallationAction    = errors.New("unknown installation action")
	errEmptyArchive                 = errors.New("the archive holds no files")
)

type installationRule struct {
	SourceValue string
	Action      installAction
}

type installServer struct {
	serverRepo    domain.ServerRepository
	installOutput io.ReadWriter
	statusCommand contracts.GameServerCommand
	stopCommand   contracts.GameServerCommand
	startCommand  contracts.GameServerCommand
	installator   *installator
	baseCommand
	kind                              installatorKind
	serverWasActiveBeforeInstallation bool
}

func newUpdateServer(
	cfg *config.Config,
	executor contracts.Executor,
	processManager contracts.ProcessManager,
	serverRepo domain.ServerRepository,
	statusCommand contracts.GameServerCommand,
	stopCommand contracts.GameServerCommand,
	startCommand contracts.GameServerCommand,
	confineOpts ...fsutil.ConfineOption,
) *installServer {
	buffer := components.NewSafeBuffer()
	inst := newUpdater(cfg, executor, buffer, confineOpts...)

	return &installServer{
		baseCommand:   newBaseCommand(cfg, executor, processManager),
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
	processManager contracts.ProcessManager,
	serverRepo domain.ServerRepository,
	statusCommand contracts.GameServerCommand,
	stopCommand contracts.GameServerCommand,
	startCommand contracts.GameServerCommand,
	confineOpts ...fsutil.ConfineOption,
) *installServer {
	buffer := components.NewSafeBuffer()
	inst := newInstallator(cfg, executor, buffer, confineOpts...)

	return &installServer{
		baseCommand:   newBaseCommand(cfg, executor, processManager),
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

	_, err = cmd.processManager.Install(ctx, server, cmd.installOutput)
	if err != nil {
		cmd.SetResult(ErrorResult)
		return errors.WithMessage(err, "failed to execute process manager install")
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

	// The start command would refuse anyway, but as an error, and a scheduled
	// update that did everything it was asked to would be reported as failed.
	if server.IsSuspended() {
		logger.Info(ctx, "Game server is suspended, it is not started again after installation/updating")
		_, _ = cmd.installOutput.Write([]byte(suspendedServerNotStartedAgain))

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
	command, err := makeFullCommand(cmd.cfg, server, cmd.cfg.Scripts.Install, "")
	if err != nil {
		cmd.SetComplete()
		cmd.SetResult(ErrorResult)
		_, _ = cmd.installOutput.Write([]byte(err.Error()))

		return errors.WithMessage(err, "[game_server_commands.installServer] failed to build install script command")
	}

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
	sd := installationRulesDefiner{replacements: cmd.cfg.RemoteRepositoryReplacements}

	game := server.Game()
	gameMod := server.GameMod()

	gameRules := sd.DefineGameRules(&game)
	if len(gameRules) == 0 && !cmd.processManager.HasOwnInstallation(server) {
		return ErrDefinedNoGameInstallationRulesError
	}

	if len(gameRules) > 0 {
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
	}

	cmd.SetResult(SuccessResult)

	return nil
}

type installationRulesDefiner struct {
	replacements config.RepositoryReplacements
}

func (d *installationRulesDefiner) DefineGameRules(game *domain.Game) []*installationRule {
	var rules []*installationRule

	if game.LocalRepository != "" {
		rule := d.defineLocalRepositoryRule(game.LocalRepository)
		if rule != nil {
			rules = append(rules, rule)
		}
	}

	if game.RemoteRepository != "" {
		rules = append(rules, d.defineRemoteRepositoryRules(game.RemoteRepository)...)
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

func (d *installationRulesDefiner) defineRemoteRepositoryRules(remoteRepository string) []*installationRule {
	parsedURL, err := url.Parse(remoteRepository)
	if err != nil {
		log.Warning(err)
		return nil
	}

	if parsedURL.Host == "" {
		return nil
	}

	candidates := buildRemoteRepositoryCandidates(remoteRepository, d.replacements)

	rules := make([]*installationRule, 0, len(candidates))
	for _, candidate := range candidates {
		rules = append(rules, &installationRule{
			SourceValue: candidate,
			Action:      downloadAnUnpackFromRemoteRepository,
		})
	}

	return rules
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
		rules = append(rules, d.defineRemoteRepositoryRules(gameMod.RemoteRepository)...)
	}

	return rules
}

// installator puts the files of a game server into its directory.
//
// The directory belongs to the tenant of the server, and where the daemon runs
// as root the installation does not: it unpacks archives and hands the tree to
// the game server user with privileges the tenant has no claim to. Every step
// taken with them therefore goes through one *os.Root, opened at the server
// directory by way of the work directory (fsutil.OpenDir), and never through a
// path. A path is resolved again by every call that takes it, through whatever
// the tenant has put in the way by then: a symlink under the name of an archive
// entry, in place of one of its directories, or in place of the server
// directory itself.
type installator struct {
	cfg      *config.Config
	executor contracts.Executor
	output   io.ReadWriter
	kind     installatorKind
	// confineOpts name the boundaries the server directory is opened within,
	// in addition to the directory itself.
	confineOpts []fsutil.ConfineOption
}

func newInstallator(
	cfg *config.Config, executor contracts.Executor, output io.ReadWriter, confineOpts ...fsutil.ConfineOption,
) *installator {
	return &installator{
		cfg:         cfg,
		executor:    executor,
		output:      output,
		kind:        installer,
		confineOpts: confineOpts,
	}
}

func newUpdater(
	cfg *config.Config, executor contracts.Executor, output io.ReadWriter, confineOpts ...fsutil.ConfineOption,
) *installator {
	return &installator{
		cfg:         cfg,
		executor:    executor,
		output:      output,
		kind:        updater,
		confineOpts: confineOpts,
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

	perm := serverDirPerm
	if rule.Action == installFromSteam {
		perm = steamServerDirPerm
	}

	root, err := fsutil.OpenDir(in.cfg.WorkDir(), server.Dir(), perm, in.confineOpts...)
	if err != nil {
		return errors.WithMessagef(
			err,
			"[game_server_commands.installator] failed to open server directory %q inside the work directory, "+
				"symlinks that lead out of it are not followed",
			server.Dir(),
		)
	}
	defer root.Close()

	switch rule.Action {
	case downloadAnUnpackFromRemoteRepository:
		err = in.downloadAndUnpackFiles(ctx, root, dst, rule.SourceValue, server.User())
	case unpackFromLocalRepository:
		err = in.unpackLocalFile(ctx, root, dst, rule.SourceValue, server.User())
	case copyDirectoryFromLocalRepository:
		err = in.copyDirectoryFromLocalRepository(ctx, root, dst, rule.SourceValue)
	case installFromSteam:
		err = in.installFromSteam(ctx, in.cfg, in.output, server, root, rule.SourceValue)
	default:
		err = errUnknownInstallationAction
	}

	if err != nil {
		return err
	}

	err = in.chown(ctx, root, server.User())
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

// unpacking says what becomes of a repository file once it is at hand: an
// archive is unpacked, anything else is stored under its own name.
type unpacking struct {
	archive bool
	// format is what an archive is unpacked as. Left unspecified, the archive
	// tells by its content and its name.
	format pb.ArchiveFormat
}

// unpackingByName reads off the name of a file whether it is an archive. The
// content does not tell a game distributed as an archive from one distributed
// as a jar, which is an archive only to whatever looks inside.
func unpackingByName(name string) unpacking {
	return unpacking{archive: archive.IsArchiveName(name)}
}

// unpackingOf reads off a source what becomes of its file. The name decides
// unless the source says otherwise with its archive parameter: false keeps a
// file with the name of an archive as it is, true unpacks one whose name does
// not tell, and the name of a format unpacks it as that format, whatever the
// file is called and looks like.
func unpackingOf(source *fetch.Source) (unpacking, error) {
	value := source.Archive()
	if value == "" {
		return unpackingByName(source.Name()), nil
	}

	if unpack, err := strconv.ParseBool(value); err == nil {
		return unpacking{archive: unpack}, nil
	}

	format, ok := archive.FormatByName(value)
	if !ok {
		return unpacking{}, errors.Errorf(
			"unsupported archive parameter %q of the source: true, false or a format is expected, one of %s",
			value, strings.Join(archive.FormatNames(), ", "),
		)
	}

	return unpacking{archive: true, format: format}, nil
}

// downloadAndUnpackFiles fetches a remote repository file and installs it. The
// download is kept in the temporary directory, where no tenant can reach it,
// until it is complete and, for a source with a checksum, verified: the server
// directory sees a finished file or none.
func (in *installator) downloadAndUnpackFiles(
	ctx context.Context,
	root *os.Root,
	dst string,
	rawSource string,
	userName string,
) error {
	source, err := fetch.Parse(rawSource)
	if err != nil {
		return errors.WithMessage(err, "[game_server_commands.installator] failed to download files")
	}

	// Before the download: a file is not fetched to find out that the source
	// asks for what cannot be done with it.
	how, err := unpackingOf(source)
	if err != nil {
		return errors.WithMessage(err, "[game_server_commands.installator] failed to download files")
	}

	in.writeOutput(ctx, "Downloading and unpacking from "+rawSource+" to "+dst+" ...")

	downloaded, err := os.CreateTemp("", "gameap-install-*")
	if err != nil {
		return errors.Wrap(err, "[game_server_commands.installator] failed to create a file to download to")
	}
	defer func() {
		_ = downloaded.Close()
		_ = os.Remove(downloaded.Name())
	}()

	progressTracker := components.NewDownloadProgressTracker(in.output, 5*time.Second)

	_, err = source.Download(ctx, downloaded, progressTracker)
	if err != nil {
		return errors.WithMessage(err, "[game_server_commands.installator] failed to download files")
	}

	err = in.installFile(ctx, root, downloaded, source.Name(), how, downloadedFilePerm, userName)
	if err != nil {
		return err
	}

	in.writeOutput(ctx, "Downloading successfully completed")

	return nil
}

func (in *installator) unpackLocalFile(
	ctx context.Context,
	root *os.Root,
	dst string,
	source string,
	userName string,
) error {
	in.writeOutput(ctx, "Unpacking from "+source+" to "+dst+" ...")

	file, err := os.Open(source)
	if err != nil {
		return errors.Wrap(err, "[game_server_commands.installator] failed to open the local repository file")
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return errors.Wrap(err, "[game_server_commands.installator] failed to stat the local repository file")
	}

	name := filepath.Base(source)

	err = in.installFile(ctx, root, file, name, unpackingByName(name), info.Mode().Perm(), userName)
	if err != nil {
		return err
	}

	in.writeOutput(ctx, "Unpacking successfully completed")

	return nil
}

// installFile puts a repository file into the server directory: an archive is
// unpacked, anything else is stored under its own name with perm.
func (in *installator) installFile(
	ctx context.Context,
	root *os.Root,
	file *os.File,
	name string,
	how unpacking,
	perm os.FileMode,
	userName string,
) error {
	if !how.archive {
		err := storeFile(root, name, file, perm)
		if err != nil {
			return errors.WithMessagef(err, "[game_server_commands.installator] failed to store %q", name)
		}

		return nil
	}

	result, err := archive.ExtractTree(ctx, file, name, root, archive.TreeOptions{
		Owner:  osowner.Options{User: userName},
		Format: how.format,
	})
	if err != nil {
		return errors.WithMessagef(err, "[game_server_commands.installator] failed to unpack %q", name)
	}

	// Before the archive is turned away as empty: the links left out are the
	// likely reason it holds nothing else.
	in.reportSkippedLinks(ctx, result.SkippedLinks)

	if result.FilesProcessed == 0 {
		return errors.WithMessagef(errEmptyArchive, "[game_server_commands.installator] failed to unpack %q", name)
	}

	return nil
}

// reportSkippedLinks tells in the installation output which symlinks of the
// archive were not created. The installation goes on without them, and a game
// that misses one of them is otherwise hard to explain.
func (in *installator) reportSkippedLinks(ctx context.Context, skipped []archive.SkippedLink) {
	for i, link := range skipped {
		if i == maxReportedSkippedLinks {
			in.writeOutput(ctx, "... and "+strconv.Itoa(len(skipped)-i)+" more symlinks were not created")

			return
		}

		in.writeOutput(
			ctx,
			"Warning: symlink "+strconv.Quote(link.Name)+" -> "+strconv.Quote(link.Target)+
				" was not created: "+link.Reason,
		)
	}
}

// storeFile writes src into the directory root is opened at, under name and in
// place of what stands there.
//
// The file is written next to its destination and renamed onto it once it is
// complete. What stood under the name stays until then, so a write that fails
// leaves the server with the file it had. The rename takes the place of a
// symlink under that name and does not write through it. A directory there is
// not replaced, as with an archive entry: the file is not stored then.
func storeFile(root *os.Root, name string, src io.ReadSeeker, perm os.FileMode) error {
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return errors.Wrap(err, "failed to rewind the source")
	}

	// A name nobody can know beforehand, created anew: a symlink put under it
	// would otherwise be written through.
	staged := stagedFilePrefix + rand.Text() + stagedFileSuffix

	out, err := root.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return errors.Wrap(err, "failed to create the file")
	}

	_, copyErr := io.Copy(out, src)

	// The mode asked for is set on the file itself: creating it is subject to
	// the umask, and its name may lead elsewhere by now.
	chmodErr := out.Chmod(perm)
	closeErr := out.Close()

	switch {
	case copyErr != nil:
		err = errors.Wrap(copyErr, "failed to write the file")
	case chmodErr != nil:
		err = errors.Wrap(chmodErr, "failed to set the file mode")
	case closeErr != nil:
		err = errors.Wrap(closeErr, "failed to close the file")
	default:
		err = errors.Wrap(root.Rename(staged, name), "failed to put the file in place of what stands under its name")
	}

	if err != nil {
		_ = root.Remove(staged)
	}

	return err
}

func (in *installator) copyDirectoryFromLocalRepository(
	ctx context.Context,
	root *os.Root,
	dst string,
	source string,
) error {
	in.writeOutput(ctx, "Copying files from "+source+" to "+dst+" ...")

	sourceRoot, err := os.OpenRoot(source)
	if err != nil {
		err = errors.Wrap(err, "[game_server_commands.installator] failed to open the local repository directory")
		in.writeOutput(ctx, err.Error())
		return err
	}
	defer sourceRoot.Close()

	// As with an archive, a file of the repository replaces a symlink standing
	// under its name instead of being written to where the link leads.
	err = fsutil.CopyTree(sourceRoot, ".", root, ".", fsutil.CopyOptions{ReplaceSymlinks: true})
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
	root *os.Root,
	source string,
) error {
	execCmd := in.makeSteamCMDCommand(source, server)

	in.writeOutput(ctx, "Installing from steam ...")

	steamCMDDir, err := in.openSteamCMDDir()
	if err != nil {
		err = errors.WithMessagef(err, "steamcmd directory %s is not accessible", cfg.SteamCMDPath)
		in.writeOutput(ctx, err.Error())

		return err
	}

	err = in.shareSteamCMDDir(ctx, steamCMDDir, server)
	_ = steamCMDDir.Close()

	if err != nil {
		err = errors.Wrapf(err, "failed to share steamcmd directory %s with the game server group", cfg.SteamCMDPath)
		in.writeOutput(ctx, err.Error())

		return err
	}

	executorOptions := contracts.ExecutorOptions{
		WorkDir:         cfg.SteamCMDPath,
		FallbackWorkDir: server.WorkDir(in.cfg),
		Env:             map[string]string{"HOME": cfg.SteamCMDPath},
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

	// steamcmd runs as the game server user and has to own the directory it
	// installs into before it starts.
	err = in.chown(ctx, root, server.User())
	if err != nil {
		err = errors.Wrapf(err, "failed to chown server work directory %s", server.WorkDir(in.cfg))
		in.writeOutput(ctx, err.Error())

		return err
	}

	installTries := maxSteamCMDInstallTries
	var result int
	for installTries > 0 {
		result, err = in.executor.ExecWithWriter(
			ctx,
			execCmd,
			output,
			executorOptions,
		)
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			return errors.WithMessage(err, "failed to execute steamcmd")
		}
		if exitErr != nil {
			result = exitErr.ExitCode()
		}

		if result == SuccessResult {
			break
		}

		if !repeatableSteamCMDInstallResults.Contains(result) {
			break
		}

		installTries--
	}

	if result != SuccessResult {
		return errInstallViaSteamCMDFailed
	}

	return nil
}

// shareSteamCMDDir makes the steamcmd tree writable for the group of the game
// servers, see osowner.ApplyGroupSharedRecursiveInRoot.
//
// The tree is all there is below the directory, and below a directory that is
// the work directory or holds it there are the game servers of the node.
// Sharing it would let every user of that group write to the files of every
// server. Such a directory is left as it is, and the installation output says
// so: steamcmd is given a directory of its own.
func (in *installator) shareSteamCMDDir(ctx context.Context, steamCMDDir *os.Root, server *domain.Server) error {
	holdsWorkDir, err := holdsDir(steamCMDDir, in.cfg.WorkDir())
	if err != nil {
		return errors.WithMessage(err, "failed to tell whether it holds the work directory")
	}

	if holdsWorkDir {
		// A daemon that is not root shares nothing, and neither does one on
		// Windows: there is nothing to say then.
		if isRootUser() && runtime.GOOS != "windows" {
			in.writeOutput(
				ctx,
				"Warning: steamcmd directory "+in.cfg.SteamCMDPath+" holds the work directory "+in.cfg.WorkDir()+
					" and is not shared with the game server group: the game servers would be shared along with it. "+
					"Point steamcmd_path to a directory of its own",
			)
		}

		return nil
	}

	return osowner.ApplyGroupSharedRecursiveInRoot(steamCMDDir, osowner.Options{
		User:  server.User(),
		Group: in.cfg.SteamConfig.Group,
	})
}

// holdsDir reports whether dir is the directory root is opened at or lies
// below it. They are compared as directories and not by name: a directory can
// be spelled in more than one way.
func holdsDir(root *os.Root, dir string) (bool, error) {
	rootInfo, err := root.Stat(".")
	if err != nil {
		return false, errors.Wrap(err, "failed to stat the directory")
	}

	// The directories dir lies in are the ones its name leads through once the
	// links in it are resolved. A directory that is not there yet has only the
	// name it is configured by.
	current := dir
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		current = resolved
	}

	current, err = filepath.Abs(current)
	if err != nil {
		return false, errors.Wrapf(err, "failed to make %q absolute", dir)
	}

	for {
		if info, err := os.Stat(current); err == nil && os.SameFile(rootInfo, info) {
			return true, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			return false, nil
		}

		current = parent
	}
}

func (in *installator) makeSteamCMDCommand(appID string, server *domain.Server) string {
	execCmd := strings.Builder{}

	execCmd.WriteString(filepath.Join(in.cfg.SteamCMDPath, config.SteamCMDExecutableFile))

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

// openSteamCMDDir opens the steamcmd directory as the root its tree is shared
// with the game server group through.
//
// The directory normally lies in the work directory, next to the game servers,
// where the user they run as can put a symlink in its place. It is reached
// through the work directory then, like a server directory. A directory the
// operator keeps elsewhere is opened by its path: nothing a tenant writes
// leads there.
func (in *installator) openSteamCMDDir() (*os.Root, error) {
	if rel, ok := relativeToWorkDir(in.cfg.WorkDir(), in.cfg.SteamCMDPath); ok {
		steamCMDDir, err := fsutil.OpenExistingDir(in.cfg.WorkDir(), rel, in.confineOpts...)
		if err != nil {
			return nil, errors.WithMessage(
				err,
				"failed to open it inside the work directory, symlinks that lead out of it are not followed",
			)
		}

		return steamCMDDir, nil
	}

	steamCMDDir, err := os.OpenRoot(in.cfg.SteamCMDPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open directory")
	}

	return steamCMDDir, nil
}

// relativeToWorkDir expresses dir relative to workDir when it lies inside.
//
// The two are compared by their names, and a directory has more than one: the
// operator may have configured either of them through a symlink of the node,
// /srv for /mnt/data/srv. The work directory is therefore tried as configured
// and with its links resolved, and dir as configured and with the links of the
// directory holding it resolved. One match is enough: a directory that can be
// reached inside the work directory is one a tenant may have got to.
//
// The last element of dir is never resolved. What stands under that name is
// what the caller is about to open.
//
// A relative name is compared as the directory it names for whoever opens it:
// from the directory the daemon runs in, which may well be the work directory.
func relativeToWorkDir(workDir, dir string) (string, bool) {
	if dir == "" {
		return "", false
	}

	if abs, err := filepath.Abs(workDir); err == nil {
		workDir = abs
	}

	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}

	bases := []string{workDir}
	if resolved, err := filepath.EvalSymlinks(workDir); err == nil {
		bases = append(bases, resolved)
	}

	dirs := []string{dir}
	if parent, err := filepath.EvalSymlinks(filepath.Dir(dir)); err == nil {
		dirs = append(dirs, filepath.Join(parent, filepath.Base(dir)))
	}

	for _, base := range bases {
		for _, candidate := range dirs {
			rel, err := filepath.Rel(base, candidate)
			if err == nil && filepath.IsLocal(rel) {
				return rel, true
			}
		}
	}

	return "", false
}

func (in *installator) chown(ctx context.Context, root *os.Root, userName string) error {
	err := osowner.ApplyRecursiveInRoot(root, osowner.Options{User: userName})
	if err != nil {
		err = errors.WithMessage(err, "[game_server_commands.installator] chown failed")
		in.writeOutput(ctx, err.Error())

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
