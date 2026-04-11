package grpc

import (
	"context"
	"sync"

	"github.com/gameap/daemon/internal/app/domain"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type TaskQueue interface {
	InsertTask(task *domain.GDTask)
	CancelTask(taskID int) error
	WorkingTasks() ([]int, []*domain.GDTask)
}

type GRPCTaskHandler struct {
	taskQueue        TaskQueue
	serverRepository domain.ServerRepository
	processedTasks   sync.Map
}

func NewGRPCTaskHandler(
	taskQueue TaskQueue,
	serverRepository domain.ServerRepository,
) *GRPCTaskHandler {
	return &GRPCTaskHandler{
		taskQueue:        taskQueue,
		serverRepository: serverRepository,
	}
}

func (h *GRPCTaskHandler) HandleTask(ctx context.Context, task *pb.DaemonTask) error {
	taskID := int(task.Id)

	if _, exists := h.processedTasks.LoadOrStore(taskID, struct{}{}); exists {
		log.WithField("taskID", taskID).Debug("Task already processed, skipping")
		return nil
	}

	var server *domain.Server
	if task.GetServerId() > 0 {
		var err error
		server, err = h.serverRepository.FindByID(ctx, int(task.GetServerId()))
		if err != nil {
			h.processedTasks.Delete(taskID)
			return errors.Wrapf(err, "failed to find server %d for task %d", task.GetServerId(), taskID)
		}
	}

	domainTask := ProtoTaskToDomain(task, server)

	h.taskQueue.InsertTask(domainTask)

	log.WithFields(log.Fields{
		"taskID":  taskID,
		"command": task.GetTaskType().String(),
	}).Info("Task received and queued")

	return nil
}

func (h *GRPCTaskHandler) InFlightTasks() []*pb.InFlightTask {
	_, tasks := h.taskQueue.WorkingTasks()

	result := make([]*pb.InFlightTask, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, &pb.InFlightTask{
			TaskId:    uint64(task.ID()),
			Status:    DomainTaskStatusToProto(task.Status()),
			StartedAt: timestamppb.Now(),
		})
	}

	return result
}

func (h *GRPCTaskHandler) HandleTaskCancel(ctx context.Context, cancel *pb.TaskCancel) error {
	taskID := int(cancel.TaskId)

	if err := h.taskQueue.CancelTask(taskID); err != nil {
		return errors.Wrapf(err, "failed to cancel task %d", taskID)
	}

	h.processedTasks.Delete(taskID)

	log.WithField("taskID", taskID).Info("Task cancelled")

	return nil
}

func (h *GRPCTaskHandler) TaskCompleted(taskID int) {
	h.processedTasks.Delete(taskID)
}
