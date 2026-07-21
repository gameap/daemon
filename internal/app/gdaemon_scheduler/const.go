package gdaemonscheduler

import "time"

// predecessorMissingTimeout bounds how long a task waits for a predecessor that
// is neither queued nor tracked as completed before it is failed.
const predecessorMissingTimeout = 5 * time.Minute

const (
	TaskWaiting = iota + 1
	TaskWorking
	TaskError
	TaskSuccess
	TaskCancelled
)

const (
	GameServerStart     = "gsstart"
	GameServerPause     = "gspause"
	GameServerStop      = "gsstop"
	GameServerKill      = "gskill"
	GameServerRestart   = "gsrest"
	GameServerInstall   = "gsinst"
	GameServerReinstall = "gsreinst"
	GameServerUpdate    = "gsupd"
	GameServerDelete    = "gsdel"
	GameServerMove      = "gsmove"
	CommandExecute      = "cmdexec"
)
