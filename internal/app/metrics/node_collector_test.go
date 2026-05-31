package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSelectDrive_ExplicitFilter(t *testing.T) {
	filter := map[string]struct{}{
		"/":          {},
		"/dev/sdb1":  {},
		"/home/data": {},
	}

	tests := []struct {
		name       string
		mountpoint string
		device     string
		want       bool
	}{
		{"mountpoint listed", "/", "/dev/sda1", true},
		{"device listed", "/mnt/extra", "/dev/sdb1", true},
		{"deep mountpoint listed", "/home/data", "/dev/sdc1", true},
		{"neither listed", "/boot", "/dev/sda2", false},
		{"pseudo mount not listed", "/snap/core/1", "/dev/loop0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// workMount is ignored when an explicit filter is set.
			assert.Equal(t, tt.want, selectDrive(filter, tt.mountpoint, tt.device, "/work"))
		})
	}
}

func TestSelectDrive_DefaultFallback(t *testing.T) {
	tests := []struct {
		name       string
		mountpoint string
		workMount  string
		want       bool
	}{
		{"root always reported", "/", "/home/server", true},
		{"work_path drive reported", "/home/server", "/home/server", true},
		{"unrelated mount skipped", "/boot", "/home/server", false},
		{"pseudo mount skipped", "/snap/core/1", "/home/server", false},
		{"no work mount, only root", "/data", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, selectDrive(nil, tt.mountpoint, "/dev/x", tt.workMount))
		})
	}
}

func TestLongestMountPrefix(t *testing.T) {
	mounts := []string{"/", "/boot", "/home", "/home/server", "/home2"}

	tests := []struct {
		name  string
		path  string
		ments []string
		want  string
	}{
		{"deepest match wins", "/home/server/srv/gameap", mounts, "/home/server"},
		{"falls back to root", "/srv/gameap", mounts, "/"},
		{"exact mountpoint", "/home", mounts, "/home"},
		{"no false prefix home2", "/home2/data", mounts, "/home2"},
		{"empty path", "", mounts, ""},
		{"no match without root", "/srv", []string{"/boot", "/home"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, longestMountPrefix(tt.path, tt.ments))
		})
	}
}

func TestIsVirtualInterface(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"eth0", false},
		{"ens3", false},
		{"enp0s3", false},
		{"wlan0", false},
		{"lo", true},
		{"lo0", true},
		{"docker0", true},
		{"veth9a2b", true},
		{"br-1a2b3c", true},
		{"virbr0", true},
		{"tun0", true},
		{"wg0", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isVirtualInterface(tt.name))
		})
	}
}

func TestSelectInterface(t *testing.T) {
	t.Run("explicit filter overrides heuristic", func(t *testing.T) {
		filter := map[string]struct{}{"lo": {}, "eth0": {}}
		assert.True(t, selectInterface(filter, "lo"))
		assert.True(t, selectInterface(filter, "eth0"))
		assert.False(t, selectInterface(filter, "eth1"))
	})

	t.Run("default keeps physical only", func(t *testing.T) {
		assert.True(t, selectInterface(nil, "eth0"))
		assert.False(t, selectInterface(nil, "lo"))
		assert.False(t, selectInterface(nil, "docker0"))
	})
}
