package grpc

import (
	"context"
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app/build"
	"github.com/gameap/daemon/internal/app/config"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

const (
	outboundBufferSize = 100
)

type TaskHandler interface {
	HandleTask(ctx context.Context, task *pb.DaemonTask) error
	HandleTaskCancel(ctx context.Context, cancel *pb.TaskCancel) error
}

type CommandHandler interface {
	HandleCommand(ctx context.Context, cmd *pb.CommandRequest) (*pb.CommandResult, error)
}

type FileHandler interface {
	HandleFileRead(ctx context.Context, requestID string, req *pb.FileReadRequest) (*pb.FileReadResponse, error)
	HandleFileWrite(ctx context.Context, requestID string, req *pb.FileWriteRequest) (*pb.FileWriteResponse, error)
	HandleFileList(ctx context.Context, requestID string, req *pb.FileListRequest) (*pb.FileListResponse, error)
}

type ServerHandler interface {
	HandleServerUpdate(ctx context.Context, srv *pb.Server) error
}

type InFlightTasksProvider interface {
	InFlightTasks() []*pb.InFlightTask
}

type GatewayClient struct {
	cfg    *config.Config
	stream pb.DaemonGateway_ConnectClient
	mu     sync.RWMutex

	taskHandler          TaskHandler
	commandHandler       CommandHandler
	fileHandler          FileHandler
	serverHandler        ServerHandler
	inFlightTaskProvider InFlightTasksProvider
	gameStore            *GameStore

	heartbeatCollector *HeartbeatCollector
	statusReporter     *ServerStatusReporter
	heartbeatInterval  time.Duration

	outbound chan *pb.DaemonMessage
	shutdown chan struct{}
	wg       sync.WaitGroup
}

func NewGatewayClient(
	cfg *config.Config,
	taskHandler TaskHandler,
	commandHandler CommandHandler,
	fileHandler FileHandler,
	serverHandler ServerHandler,
	heartbeatCollector *HeartbeatCollector,
	statusReporter *ServerStatusReporter,
	inFlightTaskProvider InFlightTasksProvider,
	gameStore *GameStore,
) *GatewayClient {
	return &GatewayClient{
		cfg:                  cfg,
		taskHandler:          taskHandler,
		commandHandler:       commandHandler,
		fileHandler:          fileHandler,
		serverHandler:        serverHandler,
		heartbeatCollector:   heartbeatCollector,
		statusReporter:       statusReporter,
		inFlightTaskProvider: inFlightTaskProvider,
		gameStore:            gameStore,
		heartbeatInterval:    cfg.GRPC.HeartbeatInterval,
		outbound:             make(chan *pb.DaemonMessage, outboundBufferSize),
		shutdown:             make(chan struct{}),
	}
}

func (c *GatewayClient) Run(ctx context.Context, conn *grpc.ClientConn) error {
	client := pb.NewDaemonGatewayClient(conn)

	stream, err := client.Connect(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to establish stream")
	}

	c.mu.Lock()
	c.stream = stream
	c.shutdown = make(chan struct{})
	c.mu.Unlock()

	if err := c.register(ctx); err != nil {
		return errors.Wrap(err, "failed to register with panel")
	}

	log.Info("Successfully registered with panel")

	c.wg.Add(3)
	go c.sendLoop(ctx)
	go c.receiveLoop(ctx)
	go c.heartbeatLoop(ctx)

	c.wg.Wait()

	return nil
}

func (c *GatewayClient) register(ctx context.Context) error {
	c.mu.RLock()
	stream := c.stream
	c.mu.RUnlock()

	var inFlightTasks []*pb.InFlightTask
	if c.inFlightTaskProvider != nil {
		inFlightTasks = c.inFlightTaskProvider.InFlightTasks()
	}

	registerReq := &pb.DaemonMessage{
		Payload: &pb.DaemonMessage_Register{
			Register: &pb.RegisterRequest{
				NodeId:        uint64(c.cfg.NodeID),
				ApiKey:        c.cfg.APIKey,
				Version:       build.Version,
				Capabilities:  []string{"grpc", "file_transfer", "server_status"},
				InFlightTasks: inFlightTasks,
			},
		},
	}

	if err := stream.Send(registerReq); err != nil {
		return errors.Wrap(err, "failed to send register request")
	}

	msg, err := stream.Recv()
	if err != nil {
		return errors.Wrap(err, "failed to receive register response")
	}

	ack := msg.GetRegisterAck()
	if ack == nil {
		return errors.New("expected RegisterAck, got different message type")
	}

	if !ack.Success {
		return errors.Errorf("registration failed: %s", ack.ErrorMessage)
	}

	c.processRegisterAck(ctx, ack)

	return nil
}

func (c *GatewayClient) processRegisterAck(ctx context.Context, ack *pb.RegisterAck) {
	if ack.HeartbeatIntervalSeconds > 0 {
		c.heartbeatInterval = time.Duration(ack.HeartbeatIntervalSeconds) * time.Second
	}

	if len(ack.Games) > 0 {
		c.gameStore.UpdateGames(ack.Games)
	}

	if len(ack.GameMods) > 0 {
		c.gameStore.UpdateGameMods(ack.GameMods)
	}

	for _, srv := range ack.Servers {
		if err := c.serverHandler.HandleServerUpdate(ctx, srv); err != nil {
			log.WithError(err).WithField("server_id", srv.Id).
				Warn("Failed to sync server from RegisterAck")
		}
	}

	for _, task := range ack.PendingTasks {
		if err := c.taskHandler.HandleTask(ctx, task); err != nil {
			log.WithError(err).WithField("task_id", task.Id).
				Warn("Failed to queue pending task from RegisterAck")
		}
	}
}

func (c *GatewayClient) sendLoop(ctx context.Context) {
	defer c.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.shutdown:
			return
		case msg := <-c.outbound:
			c.mu.RLock()
			stream := c.stream
			c.mu.RUnlock()

			if err := stream.Send(msg); err != nil {
				log.WithError(err).Error("Failed to send message")
				c.closeStream()
				return
			}
		}
	}
}

func (c *GatewayClient) receiveLoop(ctx context.Context) {
	defer c.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.shutdown:
			return
		default:
			c.mu.RLock()
			stream := c.stream
			c.mu.RUnlock()

			msg, err := stream.Recv()
			if err != nil {
				log.WithError(err).Error("Failed to receive message")
				c.closeStream()
				return
			}

			c.handleMessage(ctx, msg)
		}
	}
}

func (c *GatewayClient) heartbeatLoop(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.shutdown:
			return
		case <-ticker.C:
			c.sendHeartbeat()
		}
	}
}

func (c *GatewayClient) sendHeartbeat() {
	stats := c.heartbeatCollector.CollectStats()

	c.Send(&pb.DaemonMessage{
		Payload: &pb.DaemonMessage_Heartbeat{
			Heartbeat: &pb.Heartbeat{
				TimestampUnix: time.Now().Unix(),
				SystemStats:   stats,
			},
		},
	})
}

func (c *GatewayClient) handleMessage(ctx context.Context, msg *pb.GatewayMessage) {
	switch payload := msg.Payload.(type) {
	case *pb.GatewayMessage_Task:
		if err := c.taskHandler.HandleTask(ctx, payload.Task); err != nil {
			log.WithError(err).Error("Failed to handle task")
		}

	case *pb.GatewayMessage_TaskCancel:
		if err := c.taskHandler.HandleTaskCancel(ctx, payload.TaskCancel); err != nil {
			log.WithError(err).Error("Failed to handle task cancel")
		}

	case *pb.GatewayMessage_Command:
		resp, err := c.commandHandler.HandleCommand(ctx, payload.Command)
		if err != nil {
			log.WithError(err).Error("Failed to handle command")
			return
		}
		c.Send(&pb.DaemonMessage{
			Payload: &pb.DaemonMessage_CommandResult{
				CommandResult: resp,
			},
		})

	case *pb.GatewayMessage_FileRead:
		resp, err := c.fileHandler.HandleFileRead(ctx, msg.RequestId, payload.FileRead)
		if err != nil {
			log.WithError(err).Error("Failed to handle file read")
			return
		}
		c.Send(&pb.DaemonMessage{
			Payload: &pb.DaemonMessage_FileReadResponse{
				FileReadResponse: resp,
			},
		})

	case *pb.GatewayMessage_FileWrite:
		resp, err := c.fileHandler.HandleFileWrite(ctx, msg.RequestId, payload.FileWrite)
		if err != nil {
			log.WithError(err).Error("Failed to handle file write")
			return
		}
		c.Send(&pb.DaemonMessage{
			Payload: &pb.DaemonMessage_FileWriteResponse{
				FileWriteResponse: resp,
			},
		})

	case *pb.GatewayMessage_FileList:
		resp, err := c.fileHandler.HandleFileList(ctx, msg.RequestId, payload.FileList)
		if err != nil {
			log.WithError(err).Error("Failed to handle file list")
			return
		}
		c.Send(&pb.DaemonMessage{
			Payload: &pb.DaemonMessage_FileListResponse{
				FileListResponse: resp,
			},
		})

	case *pb.GatewayMessage_ServerConfig:
		if err := c.serverHandler.HandleServerUpdate(ctx, payload.ServerConfig); err != nil {
			log.WithError(err).Error("Failed to handle server update")
		}

	case *pb.GatewayMessage_ServerConfigBatch:
		if payload.ServerConfigBatch != nil {
			for _, srv := range payload.ServerConfigBatch.Servers {
				if err := c.serverHandler.HandleServerUpdate(ctx, srv); err != nil {
					log.WithError(err).WithField("server_id", srv.Id).Error("Failed to handle server update from batch")
				}
			}
		}

	case *pb.GatewayMessage_Shutdown:
		log.WithField("reason", payload.Shutdown.Reason).
			WithField("reconnect_delay", payload.Shutdown.ReconnectDelaySeconds).
			Warn("Received shutdown notification from panel")
		c.closeStream()

	case *pb.GatewayMessage_FileUploadTask:
		log.WithField("transfer_id", payload.FileUploadTask.TransferId).
			WithField("path", payload.FileUploadTask.Path).
			Warn("FileUploadTask not yet implemented")

	default:
		log.WithField("type", msg.Payload).Warn("Unknown message type received")
	}
}

func (c *GatewayClient) Send(msg *pb.DaemonMessage) {
	select {
	case c.outbound <- msg:
	default:
		log.Warn("Outbound buffer full, dropping message")
	}
}

func (c *GatewayClient) SendTaskStatus(taskID int, status string, message string) {
	c.Send(&pb.DaemonMessage{
		Payload: &pb.DaemonMessage_TaskStatus{
			TaskStatus: &pb.TaskStatusUpdate{
				TaskId:  uint64(taskID),
				Status:  stringStatusToProto(status),
				Message: message,
			},
		},
	})
}

func (c *GatewayClient) SendTaskOutput(taskID int, output []byte, isFinal bool) {
	c.Send(&pb.DaemonMessage{
		Payload: &pb.DaemonMessage_TaskOutput{
			TaskOutput: &pb.TaskOutput{
				TaskId:      uint64(taskID),
				OutputChunk: output,
				IsFinal:     isFinal,
			},
		},
	})
}

func (c *GatewayClient) SendServerStatuses(statuses []*pb.ServerStatus) {
	c.Send(&pb.DaemonMessage{
		Payload: &pb.DaemonMessage_ServerStatuses{
			ServerStatuses: &pb.ServerStatusBatch{
				Statuses: statuses,
			},
		},
	})
}

func (c *GatewayClient) closeStream() {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.shutdown:
	default:
		close(c.shutdown)
	}
}

func (c *GatewayClient) Close() error {
	c.closeStream()
	return nil
}
