package domain

import "context"

type GDTaskStatus string

const (
	GDTaskStatusWaiting  GDTaskStatus = "waiting"
	GDTaskStatusWorking  GDTaskStatus = "working"
	GDTaskStatusError    GDTaskStatus = "error"
	GDTaskStatusSuccess  GDTaskStatus = "success"
	GDTaskStatusCanceled GDTaskStatus = "canceled"
)

var GDTaskStatusNumMap = map[GDTaskStatus]uint8{
	GDTaskStatusWaiting:  1,
	GDTaskStatusWorking:  2,
	GDTaskStatusError:    3,
	GDTaskStatusSuccess:  4,
	GDTaskStatusCanceled: 5,
}

type GDTaskCommand string

const (
	GDTaskGameServerStart     GDTaskCommand = "gsstart"
	GDTaskGameServerPause     GDTaskCommand = "gspause" // NOT Implemented
	GDTaskGameServerStop      GDTaskCommand = "gsstop"
	GDTaskGameServerKill      GDTaskCommand = "gskill" // NOT Implemented
	GDTaskGameServerRestart   GDTaskCommand = "gsrest"
	GDTaskGameServerInstall   GDTaskCommand = "gsinst"
	GDTaskGameServerReinstall GDTaskCommand = "gsreinst" // NOT Implemented
	GDTaskGameServerUpdate    GDTaskCommand = "gsupd"
	GDTaskGameServerDelete    GDTaskCommand = "gsdel"
	GDTaskGameServerMove      GDTaskCommand = "gsdel"
	GDTaskCommandExecute      GDTaskCommand = "cmdexec"
)

type GDTaskRepository interface {
	FindByStatus(ctx context.Context, status GDTaskStatus) ([]*GDTask, error)
	FindByID(ctx context.Context, id int) (*GDTask, error)
	Save(ctx context.Context, task *GDTask) error
	AppendOutput(ctx context.Context, gdtask *GDTask, output []byte) error
}

type GDTask struct {
	id         int
	runAfterID int
	server     *Server
	task       GDTaskCommand
	cmd        string
	status     GDTaskStatus
}

func NewGDTask(
	id int,
	runAfterID int,
	server *Server,
	task GDTaskCommand,
	cmd string,
	status GDTaskStatus,
) *GDTask {
	return &GDTask{
		id,
		runAfterID,
		server,
		task,
		cmd,
		status,
	}
}

func (task *GDTask) ID() int {
	return task.id
}

func (task *GDTask) RunAfterID() int {
	return task.runAfterID
}

func (task *GDTask) Task() GDTaskCommand {
	return task.task
}

func (task *GDTask) Command() string {
	return task.cmd
}

func (task *GDTask) Status() GDTaskStatus {
	return task.status
}

func (task *GDTask) StatusNum() uint8 {
	return GDTaskStatusNumMap[task.status]
}

func (task *GDTask) Server() *Server {
	return task.server
}

func (task *GDTask) SetStatus(status GDTaskStatus) error {
	task.status = status

	task.affectServer()

	return nil
}

func (task *GDTask) affectServer() {
	if task.IsInstallation() {
		switch task.status {
		case GDTaskStatusError, GDTaskStatusWaiting:
			task.server.SetInstallationStatus(ServerNotInstalled)
		case GDTaskStatusSuccess:
			task.server.SetInstallationStatus(ServerInstalled)
		case GDTaskStatusWorking:
			task.server.SetInstallationStatus(ServerInstallInProcess)
		}
	}
}

func (task *GDTask) IsWaiting() bool {
	return task.status == GDTaskStatusWaiting
}

func (task *GDTask) IsWorking() bool {
	return task.status == GDTaskStatusWorking
}

func (task *GDTask) IsComplete() bool {
	return task.status == GDTaskStatusError ||
		task.status == GDTaskStatusSuccess ||
		task.status == GDTaskStatusCanceled
}

func (task *GDTask) IsInstallation() bool {
	return task.task == GDTaskGameServerInstall ||
		task.task == GDTaskGameServerUpdate ||
		task.task == GDTaskGameServerReinstall
}
