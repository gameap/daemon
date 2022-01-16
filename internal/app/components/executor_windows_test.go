//go:build windows
// +build windows

package components

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUserPassword(t *testing.T) {
	tests := []struct {
		name         string
		userName     string
		expectedPass string
	}{
		{"base64 password", "gameapb64", "gam_eap0121"},
		{"as is password", "gameap", "gameap"},
	}

	usersFileLocations = []string{"../../../config/users.yaml"}
	userPass = map[string]string{}
	userPassLoaded = false

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pass, err := getUserPassword(test.userName)

			require.Nil(t, err)
			assert.Equal(t, test.expectedPass, pass)
		})
	}
}
