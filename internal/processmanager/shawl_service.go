package processmanager

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gameap/gameapctl/pkg/oscore"
)

const (
	shawlServicesConfigPath = `C:\gameap\services`
	shawlServicePrefix      = "gameapServer"
	shawlStopTimeout        = "10000"
	shawlLogRotate          = "daily"
	shawlLogRetain          = "7"
)

// shawlServiceConfigVersion is stamped as the first line of every generated marker file.
// Bumping it invalidates the markers written by older daemons, so each installation recreates
// its services once and picks up changes to the way they are registered.
const shawlServiceConfigVersion = 2

// shawlServiceFingerprint is everything about a game server service that requires the Windows
// service to be registered again when it changes.
//
// It deliberately carries no password material, not even a digest: the marker file sits in
// C:\gameap\services and a digest there would be offline-crackable for no real benefit. A
// password that changed in the config without anything else changing is recovered instead by
// the ERROR_SERVICE_LOGON_FAILED branch in Shawl.Start, which recreates the service and retries.
type shawlServiceFingerprint struct {
	ServiceName        string
	Account            string
	NetworkServiceUser bool
	WorkDir            string
	BinaryPathName     string
	Command            string
}

func (f shawlServiceFingerprint) String() string {
	var b strings.Builder

	b.WriteString("version=" + strconv.Itoa(shawlServiceConfigVersion) + "\n")
	b.WriteString("service=" + f.ServiceName + "\n")
	b.WriteString("account=" + f.Account + "\n")
	b.WriteString("network_service_user=" + strconv.FormatBool(f.NetworkServiceUser) + "\n")
	b.WriteString("workdir=" + f.WorkDir + "\n")
	b.WriteString("binpath=" + f.BinaryPathName + "\n")
	b.WriteString("command=" + f.Command + "\n")

	return b.String()
}

// buildShawlRunArgs builds the argument vector shawl is registered with. The service control
// manager quotes the arguments itself, so they are passed through unquoted.
func buildShawlRunArgs(serviceName, workDir, logDir string, cmdArr []string) ([]string, error) {
	if len(cmdArr) == 0 {
		return nil, ErrEmptyCommand
	}

	executable := cmdArr[0]
	var cmdArgs []string

	if strings.EqualFold(filepath.Ext(executable), ".bat") {
		executable = "cmd.exe"
		cmdArgs = append(cmdArgs, "/c", cmdArr[0])
		cmdArgs = append(cmdArgs, cmdArr[1:]...)
	} else {
		cmdArgs = cmdArr[1:]
	}

	args := make([]string, 0, 18+len(cmdArgs))
	args = append(args,
		"run",
		"--name", serviceName,
		"--restart",
		"--stop-timeout", shawlStopTimeout,
		"--cwd", workDir,
		"--log-dir", logDir,
		"--log-as", serviceName+".log",
		"--log-rotate", shawlLogRotate,
		"--log-retain", shawlLogRetain,
		"--",
		executable,
	)

	return append(args, cmdArgs...), nil
}

// serviceStateName names a SERVICE_* state. The numbers come from the service control manager
// and are locale-invariant, unlike the words sc.exe prints for them.
func serviceStateName(state uint32) string {
	switch state {
	case 1:
		return "STOPPED"
	case 2:
		return "START_PENDING"
	case 3:
		return "STOP_PENDING"
	case 4:
		return "RUNNING"
	case 5:
		return "CONTINUE_PENDING"
	case 6:
		return "PAUSE_PENDING"
	case 7:
		return "PAUSED"
	default:
		return "UNKNOWN(" + strconv.FormatUint(uint64(state), 10) + ")"
	}
}

// serviceErrorSymbol names a Win32 service error. The daemon prints this instead of relying on
// the message the OS formats for the code, which is translated into the system language and so
// cannot be searched for or matched against.
func serviceErrorSymbol(code uint32) string {
	switch code {
	case 1053:
		return "ERROR_SERVICE_REQUEST_TIMEOUT"
	case 1056:
		return "ERROR_SERVICE_ALREADY_RUNNING"
	case 1057:
		return "ERROR_INVALID_SERVICE_ACCOUNT"
	case 1060:
		return "ERROR_SERVICE_DOES_NOT_EXIST"
	case 1062:
		return "ERROR_SERVICE_NOT_ACTIVE"
	case 1068:
		return "ERROR_SERVICE_DEPENDENCY_FAIL"
	case 1069:
		return "ERROR_SERVICE_LOGON_FAILED"
	case 1072:
		return "ERROR_SERVICE_MARKED_FOR_DELETE"
	case 1073:
		return "ERROR_SERVICE_EXISTS"
	default:
		return ""
	}
}

// serviceErrorHint explains what a service error means for a game server service, in English,
// so a report from a non-English system stays actionable.
func serviceErrorHint(code uint32, account, logDir string) string {
	switch code {
	case 1053:
		return "The service did not report back in time. " +
			"Check the shawl log in " + logDir + "."
	case 1057, 1069:
		return "The service control manager rejected the account " + quoteName(account) + ". " +
			"Well-known accounts must be spelled exactly " + quoteName(oscore.WindowsNetworkServiceAccount) +
			"; localized and display names are not accepted. " +
			"For a local user, check that the password in the daemon config matches the Windows one."
	case 1068:
		return "The service process could not be started. Check that shawl is installed and that " +
			quoteName(account) + " may read it and write to " + logDir + "."
	case 1072:
		return "The service is still marked for deletion. It is removed once every handle to it is closed."
	default:
		return ""
	}
}

// quoteName wraps a Windows account or service name in quotes for a message. strconv.Quote is
// not used because it escapes the backslash in a name like "NT AUTHORITY\\NetworkService",
// which is exactly the part of the message the reader has to compare against.
func quoteName(name string) string {
	return `"` + name + `"`
}

// parseShawlLogLine extracts the message content from a shawl log line.
// Input format: 2025-11-29 00:07:35 [DEBUG] stdout: "message"
// Output: message
func parseShawlLogLine(line string) string {
	bracketEnd := strings.Index(line, "] ")
	if bracketEnd == -1 {
		return line
	}

	rest := line[bracketEnd+2:]

	colonPos := strings.Index(rest, ": ")
	if colonPos == -1 {
		return rest
	}

	msg := rest[colonPos+2:]

	if len(msg) >= 2 && msg[0] == '"' && msg[len(msg)-1] == '"' {
		msg = msg[1 : len(msg)-1]
	}

	return msg
}
