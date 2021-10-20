package gdaemon_scheduler

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
