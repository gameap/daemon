package domain

import "context"

type GDTaskStatus string

const (
	GDTaskStatusWaiting  GDTaskStatus = "waiting"
	GDTaskStatusWorking               = "working"
	GDTaskStatusError                 = "error"
	GDTaskStatusSuccess               = "success"
	GDTaskStatusCanceled              = "canceled"
)

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

func (task *GDTask) Status() GDTaskStatus {
	return task.status
}

func (task *GDTask) Server() *Server {
	return task.server
}

func (task *GDTask) SetStatus(status GDTaskStatus) error {
	task.status = status

	return nil
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
