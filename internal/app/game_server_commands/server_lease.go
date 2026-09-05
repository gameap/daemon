package gameservercommands

import "sync"

// serverLeases tracks which game servers currently have a mutating command
// running against them.
//
// Three components issue commands for the same server concurrently and share no
// other state: the servers loop, the gdaemon task manager (which runs each game
// command on its own goroutine) and the servers scheduler (whose in-flight map
// is keyed by task, not by server). Without a shared lease a panel stop can
// overlap an automatic start, two scheduled tasks can run at once, and the
// docker process manager — which force-removes the container before recreating
// it — can destroy a server that another command has just brought up.
//
// The factory owns the lease because it is the single object all three already
// hold, so honouring it costs each of them one call and no new wiring.
type serverLeases struct {
	mu   sync.Mutex
	busy map[int]struct{}
}

func newServerLeases() *serverLeases {
	return &serverLeases{busy: make(map[int]struct{})}
}

func (l *serverLeases) acquire(serverID int) (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, taken := l.busy[serverID]; taken {
		return nil, false
	}

	l.busy[serverID] = struct{}{}

	var once sync.Once

	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			delete(l.busy, serverID)
		})
	}, true
}

func (l *serverLeases) isBusy(serverID int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	_, taken := l.busy[serverID]

	return taken
}

// TryLockServer takes the lease for a mutating command on the given server. It
// returns a release function and true on success, and nil and false when
// another component is already running one. Status checks must not take the
// lease: they are read-only and have to keep working while a start is in flight.
//
// The caller is responsible for calling release, normally with defer.
func (factory *ServerCommandFactory) TryLockServer(serverID int) (func(), bool) {
	return factory.leases.acquire(serverID)
}

// ServerIsBusy reports whether a mutating command is running for the server.
func (factory *ServerCommandFactory) ServerIsBusy(serverID int) bool {
	return factory.leases.isBusy(serverID)
}
