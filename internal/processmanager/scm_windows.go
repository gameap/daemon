//go:build windows

package processmanager

import (
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gameap/gameapctl/pkg/oscore"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceFailureResetPeriod = time.Hour

// serviceSpec describes a service to register in the Windows service control manager.
type serviceSpec struct {
	Name     string
	ExePath  string
	Args     []string
	Account  string
	Password string
}

// serviceError carries the Win32 status of a failed service operation. The numeric code and
// its symbol are the only parts of a service failure that do not depend on the system
// language, so they are what the daemon reports.
type serviceError struct {
	Op   string
	Name string
	Code uint32
	err  error
}

func (e *serviceError) Error() string {
	msg := e.Op + " " + strconv.Quote(e.Name) + " failed: " + strconv.FormatUint(uint64(e.Code), 10)

	if symbol := serviceErrorSymbol(e.Code); symbol != "" {
		msg += " " + symbol
	}

	if e.err != nil {
		msg += ": " + e.err.Error()
	}

	return msg
}

func (e *serviceError) Unwrap() error {
	return e.err
}

func newServiceError(op, name string, err error) error {
	if err == nil {
		return nil
	}

	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return errors.Wrapf(err, "%s %q failed", op, name)
	}

	return &serviceError{Op: op, Name: name, Code: uint32(errno), err: err}
}

// serviceErrorCode returns the Win32 status of err, if it carries one.
func serviceErrorCode(err error) (uint32, bool) {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return uint32(errno), true
	}

	return 0, false
}

func isServiceErrno(err error, errno syscall.Errno) bool {
	code, ok := serviceErrorCode(err)

	return ok && code == uint32(errno)
}

// openSCM connects to the service control manager with no more access than the caller needs.
// mgr.Connect asks for SC_MANAGER_ALL_ACCESS, which would make even a status query require
// administrator rights.
func openSCM(access uint32) (*mgr.Mgr, error) {
	handle, err := windows.OpenSCManager(nil, nil, access)
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to service control manager")
	}

	return &mgr.Mgr{Handle: handle}, nil
}

// withService opens one service with the given access mask and hands it to fn. mgr.OpenService
// is not used for the same reason as mgr.Connect: it always asks for SERVICE_ALL_ACCESS.
func withService(name string, serviceAccess uint32, fn func(*mgr.Service) error) error {
	manager, err := openSCM(windows.SC_MANAGER_CONNECT)
	if err != nil {
		return err
	}
	defer func() {
		_ = manager.Disconnect()
	}()

	namePointer, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return errors.Wrapf(err, "invalid service name %q", name)
	}

	handle, err := windows.OpenService(manager.Handle, namePointer, serviceAccess)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return errors.WithMessagef(ErrServiceNotFound, "service %q", name)
		}

		return newServiceError("open service", name, err)
	}

	service := &mgr.Service{Name: name, Handle: handle}
	defer func() {
		_ = service.Close()
	}()

	return fn(service)
}

// createService registers a new auto start service.
//
// The service control manager API is used instead of sc.exe because sc reports failures as
// localized text with an exit code, and because CreateService quotes the binary path and its
// arguments itself.
func createService(spec serviceSpec) error {
	manager, err := openSCM(windows.SC_MANAGER_CONNECT | windows.SC_MANAGER_CREATE_SERVICE)
	if err != nil {
		return err
	}
	defer func() {
		_ = manager.Disconnect()
	}()

	// Normalizing here as well as in the caller keeps the invariant impossible to bypass: a
	// localized or display spelling resolves to the right SID but is not accepted by the SCM.
	account := oscore.NormalizeWindowsServiceAccount(spec.Account)

	service, err := manager.CreateService(spec.Name, spec.ExePath, mgr.Config{
		DisplayName:      spec.Name,
		StartType:        mgr.StartAutomatic,
		ErrorControl:     mgr.ErrorNormal,
		ServiceStartName: account,
		Password:         spec.Password,
	}, spec.Args...)
	if err != nil {
		return newServiceError("create service", spec.Name, err)
	}
	defer func() {
		_ = service.Close()
	}()

	// shawl restarts the game process on its own, but nothing restarts shawl itself. This
	// mirrors the failure policy the winsw manager writes into its service definition.
	err = service.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: time.Second},
		{Type: mgr.ServiceRestart, Delay: 2 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
	}, uint32(serviceFailureResetPeriod.Seconds()))
	if err != nil {
		// A service without its failure actions would be kept by the next start, which compares
		// against a configuration it does not have, so it is removed along with the error.
		_ = service.Delete()

		return newServiceError("configure failure actions for service", spec.Name, err)
	}

	return nil
}

func deleteService(name string) error {
	return withService(
		name,
		windows.DELETE,
		func(service *mgr.Service) error {
			return newServiceError("delete service", name, service.Delete())
		},
	)
}

func startService(name string) error {
	return withService(
		name,
		windows.SERVICE_START|windows.SERVICE_QUERY_STATUS,
		func(service *mgr.Service) error {
			// Arguments passed here reach the service's own ServiceMain, not its command line,
			// which already carries everything shawl needs.
			return newServiceError("start service", name, service.Start())
		},
	)
}

func stopService(name string) error {
	return withService(
		name,
		windows.SERVICE_STOP|windows.SERVICE_QUERY_STATUS,
		func(service *mgr.Service) error {
			_, err := service.Control(svc.Stop)

			return newServiceError("stop service", name, err)
		},
	)
}

func queryService(name string) (svc.Status, error) {
	var status svc.Status

	err := withService(
		name,
		windows.SERVICE_QUERY_STATUS,
		func(service *mgr.Service) error {
			queried, err := service.Query()
			if err != nil {
				return newServiceError("query service", name, err)
			}

			status = queried

			return nil
		},
	)

	return status, err
}

func readServiceConfig(name string) (mgr.Config, error) {
	var config mgr.Config

	err := withService(
		name,
		windows.SERVICE_QUERY_CONFIG,
		func(service *mgr.Service) error {
			read, err := service.Config()
			if err != nil {
				return newServiceError("read configuration of service", name, err)
			}

			config = read

			return nil
		},
	)

	return config, err
}

func serviceInstalled(name string) (bool, error) {
	_, err := queryService(name)

	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrServiceNotFound):
		return false, nil
	default:
		return false, err
	}
}

// expectedBinaryPathName reproduces the command line mgr.CreateService stores for a service, so
// a registered service can be compared against the configuration it should have. It calls the
// same primitive CreateService uses, so the two cannot diverge.
func expectedBinaryPathName(exePath string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, syscall.EscapeArg(exePath))

	for _, arg := range args {
		parts = append(parts, syscall.EscapeArg(arg))
	}

	return strings.Join(parts, " ")
}

// sameServiceAccount reports whether the account a service is registered for is the one it
// should run as. The service control manager stores an unqualified local user as ".\user", so
// a local-machine qualifier on either side is ignored.
func sameServiceAccount(stored, expected string) bool {
	return strings.EqualFold(trimLocalAccountQualifier(stored), trimLocalAccountQualifier(expected))
}

func trimLocalAccountQualifier(account string) string {
	qualifier, name, found := strings.Cut(account, `\`)
	if !found {
		return account
	}

	if qualifier == "." {
		return name
	}

	if hostname, err := windows.ComputerName(); err == nil && strings.EqualFold(qualifier, hostname) {
		return name
	}

	return account
}
