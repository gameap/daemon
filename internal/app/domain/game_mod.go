package domain

type GameMod struct {
	ID               int
	Name             string
	RemoteRepository string
	LocalRepository  string

	DefaultStartCMDLinux   string
	DefaultStartCMDWindows string
}
