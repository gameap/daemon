//go:build windows
// +build windows

package sys

import (
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
)

func ChownR(_ string, _, _ int) error {
	return nil
}

func IsRootUser() (bool, error) {
	var sid *windows.SID

	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid)
	if err != nil {
		return false, errors.WithMessage(err, "failed to AllocateAndInitializeSid")
	}
	defer windows.FreeSid(sid)

	token := windows.Token(0)

	isMember, err := token.IsMember(sid)
	if err != nil {
		return false, errors.WithMessage(err, "failed to check that user is a member of the provided SID")
	}

	return isMember, nil
}
