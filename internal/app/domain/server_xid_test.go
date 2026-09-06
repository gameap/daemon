package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	testXIDForCanonicalUUID = "dejrg44tll0t3g5k0300"
	testXIDForServerID1     = "00000080000000000000"
)

func newTestServerForXID(id int, rawUUID string) *Server {
	return NewServer(
		id,
		true,
		ServerInstalled,
		false,
		"test",
		rawUUID,
		"test",
		Game{},
		GameMod{},
		"127.0.0.1",
		27015,
		27016,
		27017,
		"rconpass",
		"server-dir",
		"",
		"",
		"",
		"",
		"",
		false,
		time.Time{},
		map[string]string{},
		Settings{},
		time.Time{},
		0,
		0,
	)
}

func pushServerUUID(server *Server, rawUUID string) {
	server.Set(
		true,
		ServerInstalled,
		false,
		"test",
		rawUUID,
		"test",
		Game{},
		GameMod{},
		"127.0.0.1",
		27015,
		27016,
		27017,
		"rconpass",
		"server-dir",
		"",
		"",
		"",
		"",
		"",
		false,
		time.Time{},
		map[string]string{},
		Settings{},
		time.Time{},
		0,
		0,
	)
}

func TestServer_XID(t *testing.T) {
	tests := []struct {
		name        string
		uuid        string
		expectedXID string
	}{
		{
			name:        "canonical lowercase",
			uuid:        "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
			expectedXID: testXIDForCanonicalUUID,
		},
		{
			name:        "uppercase URN form",
			uuid:        "URN:UUID:6BA7B810-9DAD-11D1-80B4-00C04FD430C8",
			expectedXID: testXIDForCanonicalUUID,
		},
		{
			name:        "lowercase URN form",
			uuid:        "urn:uuid:6ba7b810-9dad-11d1-80b4-00c04fd430c8",
			expectedXID: testXIDForCanonicalUUID,
		},
		{
			name:        "braced form",
			uuid:        "{6ba7b810-9dad-11d1-80b4-00c04fd430c8}",
			expectedXID: testXIDForCanonicalUUID,
		},
		{
			name:        "hex without dashes",
			uuid:        "6ba7b8109dad11d180b400c04fd430c8",
			expectedXID: testXIDForCanonicalUUID,
		},
		{
			name:        "non-UUID string falls back to server id",
			uuid:        "test-uuid",
			expectedXID: testXIDForServerID1,
		},
		{
			name:        "empty string falls back to server id",
			uuid:        "",
			expectedXID: testXIDForServerID1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestServerForXID(1, tt.uuid)

			assert.Equal(t, tt.expectedXID, server.XID())
			// Process managers build the legacy unit, container and session
			// names from the raw value, so it has to be kept exactly as received.
			assert.Equal(t, tt.uuid, server.UUID())
		})
	}
}

func TestServer_XID_StableAcrossSet(t *testing.T) {
	const (
		canonical    = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
		uppercaseURN = "URN:UUID:6BA7B810-9DAD-11D1-80B4-00C04FD430C8"
	)

	tests := []struct {
		name        string
		initialUUID string
		pushedUUID  string
	}{
		{
			name:        "canonical then uppercase URN",
			initialUUID: canonical,
			pushedUUID:  uppercaseURN,
		},
		{
			name:        "uppercase URN then canonical",
			initialUUID: uppercaseURN,
			pushedUUID:  canonical,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestServerForXID(1, tt.initialUUID)
			assert.Equal(t, testXIDForCanonicalUUID, server.XID())

			pushServerUUID(server, tt.pushedUUID)

			assert.Equal(t, testXIDForCanonicalUUID, server.XID())
			assert.Equal(t, tt.pushedUUID, server.UUID())
		})
	}
}
