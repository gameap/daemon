//go:build windows
// +build windows

package components

import (
	"encoding/base64"
	"os"
	"os/exec"
	"sync"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/itchio/ox/winox"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
	"gopkg.in/yaml.v3"
)

func setCMDSysProcAttr(cmd *exec.Cmd, options domain.ExecutorOptions) (*exec.Cmd, error) {
	//nolint:revive,stylecheck
	const _DETACHED_PROCESS = 0x00000008
	// const _CREATE_NEW_CONSOLE = 0x00000010

	cmd.SysProcAttr = &windows.SysProcAttr{
		HideWindow:       true,
		CreationFlags:    _DETACHED_PROCESS,
		NoInheritHandles: true,
	}

	if options.Username != "" {
		password, err := getUserPassword(options.Username)
		if err != nil {
			return nil, err
		}

		if password != "" {
			token, err := winox.Logon(options.Username, ".", password)
			if err != nil {
				return nil, err
			}

			cmd.SysProcAttr.Token = token
		}
	}

	return cmd, nil
}

var mutex sync.Mutex
var userPass = map[string]string{}
var userPassLoaded bool

func getUserPassword(userName string) (string, error) {
	mutex.Lock()
	defer mutex.Unlock()

	if !userPassLoaded {
		err := loadUserPassMap()
		if err != nil {
			return "", err
		}

		userPassLoaded = true
	}

	pass, exists := userPass[userName]
	if !exists {
		return "", nil
	}

	return pass, nil
}

func loadUserPassMap() error {
	filePath := findUsersFile()
	if filePath == "" {
		return nil
	}

	contents, err := os.ReadFile(filePath)
	if err != nil {
		return errors.WithMessage(err, "failed to read users passwords config")
	}

	var userPassMap map[string]string

	err = yaml.Unmarshal(contents, &userPassMap)
	if err != nil {
		return errors.WithMessage(err, "failed to unmarshal users passwords config")
	}

	for userName, encodedPass := range userPassMap {
		pass, err := decodePassword(encodedPass)
		if err != nil {
			return err
		}

		userPass[userName] = pass
	}

	return nil
}

func decodePassword(encodedPass string) (string, error) {
	if len(encodedPass) > 7 {
		if encodedPass[:7] == "base64:" {
			decodedPass, err := base64.StdEncoding.DecodeString(encodedPass[7:])
			if err != nil {
				return "", errors.WithMessage(err, "failed to decode base64 password")
			}

			return string(decodedPass), nil
		}
	}

	return encodedPass, nil
}

var usersFileLocations = []string{
	"./users.yaml",
	"./users.yml",
	"./daemon/users.yaml",
	"./daemon/users.yml",
	"./daemon/config/users.yaml",
	"./daemon/config/users.yml",
	"./gameap-daemon/users.yaml",
	"./gameap-daemon/users.yml",
	"./gameap-daemon/config/users.yaml",
	"./gameap-daemon/config/users.yml",
}

func findUsersFile() string {
	for _, path := range usersFileLocations {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}
