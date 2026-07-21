//go:build linux || darwin
// +build linux darwin

package components

import (
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"

	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func setCMDSysProcCredential(cmd *exec.Cmd, options contracts.ExecutorOptions) (*exec.Cmd, error) {
	u, err := user.LookupId(options.UID)
	if err != nil {
		return nil, errors.WithMessage(err, "[game_server_commands.installator] invalid user uid")
	}

	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return nil, errors.WithMessage(err, "[game_server_commands.installator] invalid user uid")
	}

	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return nil, errors.WithMessage(err, "[game_server_commands.installator] invalid user gid")
	}

	credential := &syscall.Credential{
		Uid: uint32(uid),
		Gid: uint32(gid),
	}

	if groups := supplementaryGroups(u); len(groups) > 0 {
		credential.Groups = groups
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{}
	cmd.SysProcAttr.Credential = credential

	env := os.Environ()
	for key, value := range options.Env {
		env = upsertEnv(env, key, value)
	}
	if _, ok := options.Env["HOME"]; !ok {
		env = upsertEnv(env, "HOME", u.HomeDir)
	}
	cmd.Env = env

	return cmd, nil
}

// supplementaryGroups resolves the user's full group membership so the
// privilege-dropped process keeps access to shared groups (e.g. the steamcmd
// group). Without it the child would call setgroups([]) and lose them.
func supplementaryGroups(u *user.User) []uint32 {
	groupIDs, err := u.GroupIds()
	if err != nil {
		log.Error(errors.Wrapf(err, "failed to resolve supplementary groups for user %s", u.Username))

		return nil
	}

	groups := make([]uint32, 0, len(groupIDs))
	for _, g := range groupIDs {
		n, convErr := strconv.Atoi(g)
		if convErr != nil {
			log.Error(errors.Wrapf(convErr, "invalid supplementary group id %q for user %s", g, u.Username))

			continue
		}
		groups = append(groups, uint32(n))
	}

	return groups
}

// upsertEnv replaces the KEY=... entry in env or appends it, keeping a single
// definition per key so a caller-supplied value (e.g. HOME) is not shadowed.
func upsertEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value

			return env
		}
	}

	return append(env, prefix+value)
}
