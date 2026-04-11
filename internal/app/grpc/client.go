package grpc

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gameap/daemon/internal/app/build"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	outboundBufferSize = 500
)

type TaskHandler interface {
	HandleTask(ctx context.Context, task *pb.DaemonTask) error
	HandleTaskCancel(ctx context.Context, cancel *pb.TaskCancel) error
}

type CommandHandler interface {
	HandleCommand(ctx context.Context, requestID string, cmd *pb.CommandRequest) (*pb.CommandResult, error)
}

type OnlineServerCounter interface {
	CountOnlineServers() int
}

type FileHandler interface {
	HandleFileRead(ctx context.Context, requestID string, req *pb.FileReadRequest) (*pb.FileReadResponse, error)
	HandleFileWrite(ctx context.Context, requestID string, req *pb.FileWriteRequest) (*pb.FileWriteResponse, error)
	HandleFileList(ctx context.Context, requestID string, req *pb.FileListRequest) (*pb.FileListResponse, error)
	HandleFileOperation(ctx context.Context, req *pb.FileOperationRequest) (*pb.FileOperationResponse, error)
}

type ServerHandler interface {
	HandleServerUpdate(ctx context.Context, srv *pb.Server) error
	HandleServerConfigUpdate(ctx context.Context, srv *pb.Server, settings []*pb.ServerSetting) error
}

type TransferHandler interface {
	HandleFileUploadTask(ctx context.Context, requestID string, task *pb.FileUploadTask)
	HandleFileDownloadTask(ctx context.Context, requestID string, task *pb.FileDownloadTask)
}

type AttachHandler interface {
	HandleAttachRequest(ctx context.Context, req *pb.AttachRequest)
	HandleAttachInput(ctx context.Context, input *pb.AttachInput)
	HandleAttachDetach(ctx context.Context, detach *pb.AttachDetach)
	CloseAllSessions(reason string)
}

type ConsoleLogHandler interface {
	HandleConsoleLogRequest(
		ctx context.Context, requestID string, req *pb.ConsoleLogRequest,
	) (*pb.ConsoleLogResponse, error)
}

type HTTPProxyHandler interface {
	HandleHTTPProxy(ctx context.Context, requestID string, req *pb.HTTPProxyRequest) (*pb.HTTPProxyResponse, error)
}

type ResponseSender interface {
	Send(msg *pb.DaemonMessage)
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
	transferHandler      TransferHandler
	attachHandler        AttachHandler
	consoleLogHandler    ConsoleLogHandler
	httpProxyHandler     HTTPProxyHandler
	inFlightTaskProvider InFlightTasksProvider
	gameStore            *GameStore

	heartbeatCollector  *HeartbeatCollector
	statusReporter      *ServerStatusReporter
	heartbeatInterval   time.Duration
	taskStatsReader     domain.GDTaskStatsReader
	onlineServerCounter OnlineServerCounter

	outbound      chan *pb.DaemonMessage
	shutdown      chan struct{}
	wg            sync.WaitGroup
	shutdownDelay atomic.Pointer[time.Duration]
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
	taskStatsReader domain.GDTaskStatsReader,
	onlineServerCounter OnlineServerCounter,
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
		taskStatsReader:      taskStatsReader,
		onlineServerCounter:  onlineServerCounter,
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
				Capabilities:  []string{"grpc", "file_transfer", "server_status", "attach", "http_proxy"},
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
	log.WithFields(log.Fields{
		"servers":         len(ack.Servers),
		"tasks":           len(ack.PendingTasks),
		"games":           len(ack.Games),
		"gameMods":        len(ack.GameMods),
		"server_settings": len(ack.ServerSettings),
	}).Info("Processing RegisterAck from panel")

	if ack.HeartbeatInterval != nil {
		c.heartbeatInterval = ack.HeartbeatInterval.AsDuration()
	}

	if len(ack.Games) > 0 {
		c.gameStore.UpdateGames(ack.Games)
	}

	if len(ack.GameMods) > 0 {
		c.gameStore.UpdateGameMods(ack.GameMods)
	}

	settingsByServer := groupSettingsByServerID(ack.ServerSettings)

	for _, srv := range ack.Servers {
		if ctx.Err() != nil {
			break
		}

		serverSettings := settingsByServer[srv.Id]

		if err := c.serverHandler.HandleServerConfigUpdate(ctx, srv, serverSettings); err != nil {
			log.WithError(err).WithField("server_id", srv.Id).
				Warn("Failed to sync server from RegisterAck")
		}
	}

	for _, task := range ack.PendingTasks {
		if ctx.Err() != nil {
			break
		}
		if err := c.taskHandler.HandleTask(ctx, task); err != nil {
			log.WithError(err).WithField("task_id", task.Id).
				Warn("Failed to queue pending task from RegisterAck")
		}
	}
}

func groupSettingsByServerID(settings []*pb.ServerSetting) map[uint64][]*pb.ServerSetting {
	result := make(map[uint64][]*pb.ServerSetting)
	for _, s := range settings {
		result[s.ServerId] = append(result[s.ServerId], s)
	}

	return result
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
				Timestamp:   timestamppb.Now(),
				SystemStats: stats,
			},
		},
	})
}

//nolint:gocyclo // message router switch
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
		resp, err := c.commandHandler.HandleCommand(ctx, msg.RequestId, payload.Command)
		if err != nil {
			log.WithError(err).Error("Failed to handle command")
			return
		}
		c.Send(&pb.DaemonMessage{
			RequestId: msg.RequestId,
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

	case *pb.GatewayMessage_ServerConfigUpdate:
		update := payload.ServerConfigUpdate
		if err := c.serverHandler.HandleServerConfigUpdate(ctx, update.Server, update.Settings); err != nil {
			log.WithError(err).Error("Failed to handle server config update")
		}

	case *pb.GatewayMessage_ServerConfigBatch:
		c.handleServerConfigBatch(ctx, payload.ServerConfigBatch)

	case *pb.GatewayMessage_Shutdown:
		c.handleShutdownMessage(payload.Shutdown)

	case *pb.GatewayMessage_FileOperation:
		resp, err := c.fileHandler.HandleFileOperation(ctx, payload.FileOperation)
		if err != nil {
			log.WithError(err).Error("Failed to handle file operation")
			return
		}
		c.Send(&pb.DaemonMessage{
			Payload: &pb.DaemonMessage_FileOperationResponse{
				FileOperationResponse: resp,
			},
		})

	case *pb.GatewayMessage_FileUploadTask:
		c.runFileTransfer("FileUploadTask", func() {
			c.transferHandler.HandleFileUploadTask(ctx, msg.RequestId, payload.FileUploadTask)
		})

	case *pb.GatewayMessage_FileDownloadTask:
		c.runFileTransfer("FileDownloadTask", func() {
			c.transferHandler.HandleFileDownloadTask(ctx, msg.RequestId, payload.FileDownloadTask)
		})

	case *pb.GatewayMessage_AttachRequest:
		if c.attachHandler != nil {
			c.attachHandler.HandleAttachRequest(ctx, payload.AttachRequest)
		}

	case *pb.GatewayMessage_AttachInput:
		if c.attachHandler != nil {
			c.attachHandler.HandleAttachInput(ctx, payload.AttachInput)
		}

	case *pb.GatewayMessage_AttachDetach:
		if c.attachHandler != nil {
			c.attachHandler.HandleAttachDetach(ctx, payload.AttachDetach)
		}

	case *pb.GatewayMessage_ConsoleLogRequest:
		c.handleConsoleLog(ctx, msg.RequestId, payload.ConsoleLogRequest)

	case *pb.GatewayMessage_StatusRequest:
		c.Send(&pb.DaemonMessage{
			RequestId: msg.RequestId,
			Payload: &pb.DaemonMessage_StatusResponse{
				StatusResponse: c.buildStatusResponse(msg.RequestId),
			},
		})

	case *pb.GatewayMessage_HttpProxy:
		c.handleHTTPProxy(ctx, msg.RequestId, payload.HttpProxy)

	default:
		log.WithField("type", msg.Payload).Warn("Unknown message type received")
	}
}

func (c *GatewayClient) handleServerConfigBatch(ctx context.Context, batch *pb.ServerConfigBatch) {
	if batch == nil {
		return
	}

	settingsByServer := groupSettingsByServerID(batch.ServerSettings)

	for _, srv := range batch.Servers {
		if ctx.Err() != nil {
			break
		}

		serverSettings := settingsByServer[srv.Id]

		if err := c.serverHandler.HandleServerConfigUpdate(ctx, srv, serverSettings); err != nil {
			log.WithError(err).WithField("server_id", srv.Id).Error("Failed to handle server update from batch")
		}
	}
}

func (c *GatewayClient) runFileTransfer(name string, fn func()) {
	if c.transferHandler == nil {
		log.Warnf("%s received but no transfer handler configured", name)
		return
	}
	go fn()
}

func (c *GatewayClient) handleShutdownMessage(shutdown *pb.ShutdownNotification) {
	log.WithField("reason", shutdown.Reason).
		WithField("reconnect_delay", shutdown.ReconnectDelay).
		Warn("Received shutdown notification from panel")
	if shutdown.ReconnectDelay != nil {
		delay := shutdown.ReconnectDelay.AsDuration()
		c.shutdownDelay.Store(&delay)
	}
	c.closeStream()
}

func (c *GatewayClient) handleConsoleLog(
	ctx context.Context, requestID string, req *pb.ConsoleLogRequest,
) {
	if c.consoleLogHandler == nil {
		return
	}
	resp, err := c.consoleLogHandler.HandleConsoleLogRequest(ctx, requestID, req)
	if err != nil {
		log.WithError(err).Error("Failed to handle console log request")
		return
	}
	c.Send(&pb.DaemonMessage{
		RequestId: requestID,
		Payload: &pb.DaemonMessage_ConsoleLogResponse{
			ConsoleLogResponse: resp,
		},
	})
}

func (c *GatewayClient) handleHTTPProxy(
	ctx context.Context, requestID string, req *pb.HTTPProxyRequest,
) {
	if c.httpProxyHandler == nil {
		return
	}
	resp, err := c.httpProxyHandler.HandleHTTPProxy(ctx, requestID, req)
	if err != nil {
		log.WithError(err).Error("Failed to handle http proxy request")
		c.Send(&pb.DaemonMessage{
			RequestId: requestID,
			Payload: &pb.DaemonMessage_HttpProxyResponse{
				HttpProxyResponse: &pb.HTTPProxyResponse{
					RequestId: requestID,
					Error:     err.Error(),
				},
			},
		})
		return
	}
	c.Send(&pb.DaemonMessage{
		RequestId: requestID,
		Payload: &pb.DaemonMessage_HttpProxyResponse{
			HttpProxyResponse: resp,
		},
	})
}

func (c *GatewayClient) buildStatusResponse(requestID string) *pb.StatusResponse {
	resp := &pb.StatusResponse{
		RequestId:     requestID,
		Success:       true,
		Version:       build.Version,
		BuildDate:     build.BuildDate,
		UptimeSeconds: int64(time.Since(domain.StartTime).Seconds()),
	}

	if c.taskStatsReader != nil {
		stats := c.taskStatsReader.Stats()
		resp.WorkingTasks = int32(stats.WorkingCount)
		resp.WaitingTasks = int32(stats.WaitingCount)
	}

	if c.onlineServerCounter != nil {
		resp.OnlineServers = int32(c.onlineServerCounter.CountOnlineServers())
	}

	return resp
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

func (c *GatewayClient) PendingShutdownDelay() (time.Duration, bool) {
	d := c.shutdownDelay.Swap(nil)
	if d != nil {
		return *d, true
	}
	return 0, false
}

func (c *GatewayClient) closeStream() {
	if c.attachHandler != nil {
		c.attachHandler.CloseAllSessions("daemon disconnected")
	}

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

func (c *GatewayClient) SetTransferHandler(h TransferHandler) {
	c.transferHandler = h
}

func (c *GatewayClient) SetAttachHandler(h AttachHandler) {
	c.attachHandler = h
}

func (c *GatewayClient) SetConsoleLogHandler(h ConsoleLogHandler) {
	c.consoleLogHandler = h
}

func (c *GatewayClient) SetHTTPProxyHandler(h HTTPProxyHandler) {
	c.httpProxyHandler = h
}
