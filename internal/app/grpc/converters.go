package grpc

import (
	"encoding/json"
	"runtime"

	"github.com/gameap/daemon/internal/app/domain"
	pb "github.com/gameap/gameap/pkg/proto"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func ProtoTaskToDomain(task *pb.DaemonTask, server *domain.Server) *domain.GDTask {
	return domain.NewGDTask(
		int(task.GetId()),
		int(task.GetRunAfterId()),
		server,
		protoTaskCommandToDomain(task.GetTaskType()),
		task.GetCmd(),
		protoTaskStatusToDomain(task.GetStatus()),
	)
}

func protoTaskCommandToDomain(cmd pb.DaemonTaskType) domain.GDTaskCommand {
	switch cmd {
	case pb.DaemonTaskType_DAEMON_TASK_TYPE_SERVER_START:
		return domain.GDTaskGameServerStart
	case pb.DaemonTaskType_DAEMON_TASK_TYPE_SERVER_STOP:
		return domain.GDTaskGameServerStop
	case pb.DaemonTaskType_DAEMON_TASK_TYPE_SERVER_RESTART:
		return domain.GDTaskGameServerRestart
	case pb.DaemonTaskType_DAEMON_TASK_TYPE_SERVER_INSTALL:
		return domain.GDTaskGameServerInstall
	case pb.DaemonTaskType_DAEMON_TASK_TYPE_SERVER_UPDATE:
		return domain.GDTaskGameServerUpdate
	case pb.DaemonTaskType_DAEMON_TASK_TYPE_SERVER_DELETE:
		return domain.GDTaskGameServerDelete
	case pb.DaemonTaskType_DAEMON_TASK_TYPE_SERVER_MOVE:
		return domain.GDTaskGameServerMove
	case pb.DaemonTaskType_DAEMON_TASK_TYPE_CMD_EXEC:
		return domain.GDTaskCommandExecute
	}

	return domain.GDTaskCommandExecute
}

func protoTaskStatusToDomain(status pb.DaemonTaskStatus) domain.GDTaskStatus {
	switch status {
	case pb.DaemonTaskStatus_DAEMON_TASK_STATUS_WAITING:
		return domain.GDTaskStatusWaiting
	case pb.DaemonTaskStatus_DAEMON_TASK_STATUS_WORKING:
		return domain.GDTaskStatusWorking
	case pb.DaemonTaskStatus_DAEMON_TASK_STATUS_SUCCESS:
		return domain.GDTaskStatusSuccess
	case pb.DaemonTaskStatus_DAEMON_TASK_STATUS_ERROR:
		return domain.GDTaskStatusError
	case pb.DaemonTaskStatus_DAEMON_TASK_STATUS_CANCELED:
		return domain.GDTaskStatusCanceled
	default:
		return domain.GDTaskStatusWaiting
	}
}

func DomainTaskStatusToProto(status domain.GDTaskStatus) pb.DaemonTaskStatus {
	switch status {
	case domain.GDTaskStatusWaiting:
		return pb.DaemonTaskStatus_DAEMON_TASK_STATUS_WAITING
	case domain.GDTaskStatusWorking:
		return pb.DaemonTaskStatus_DAEMON_TASK_STATUS_WORKING
	case domain.GDTaskStatusSuccess:
		return pb.DaemonTaskStatus_DAEMON_TASK_STATUS_SUCCESS
	case domain.GDTaskStatusError:
		return pb.DaemonTaskStatus_DAEMON_TASK_STATUS_ERROR
	case domain.GDTaskStatusCanceled:
		return pb.DaemonTaskStatus_DAEMON_TASK_STATUS_CANCELED
	default:
		return pb.DaemonTaskStatus_DAEMON_TASK_STATUS_WAITING
	}
}

func stringStatusToProto(status string) pb.DaemonTaskStatus {
	switch status {
	case "waiting":
		return pb.DaemonTaskStatus_DAEMON_TASK_STATUS_WAITING
	case "working":
		return pb.DaemonTaskStatus_DAEMON_TASK_STATUS_WORKING
	case "success":
		return pb.DaemonTaskStatus_DAEMON_TASK_STATUS_SUCCESS
	case "error":
		return pb.DaemonTaskStatus_DAEMON_TASK_STATUS_ERROR
	case "canceled":
		return pb.DaemonTaskStatus_DAEMON_TASK_STATUS_CANCELED
	default:
		return pb.DaemonTaskStatus_DAEMON_TASK_STATUS_WAITING
	}
}

func DomainServerToProtoStatus(server *domain.Server) *pb.ServerStatus {
	return &pb.ServerStatus{
		ServerId:  uint64(server.ID()),
		IsRunning: server.IsActive(),
		LastCheck: timestamppb.New(server.LastStatusCheck()),
	}
}

func ProtoInstalledStatusToDomain(status pb.ServerInstalledStatus) domain.InstallationStatus {
	switch status {
	case pb.ServerInstalledStatus_SERVER_INSTALLED_STATUS_NOT_INSTALLED:
		return domain.ServerNotInstalled
	case pb.ServerInstalledStatus_SERVER_INSTALLED_STATUS_INSTALLED:
		return domain.ServerInstalled
	case pb.ServerInstalledStatus_SERVER_INSTALLED_STATUS_INSTALLATION_IN_PROGRESS:
		return domain.ServerInstallInProcess
	default:
		return domain.ServerNotInstalled
	}
}

func ProtoGameToDomain(g *pb.Game) domain.Game {
	remoteRepo := g.GetRemoteRepositoryLinux()
	localRepo := g.GetLocalRepositoryLinux()
	steamAppID := domain.SteamAppID(g.GetSteamAppIdLinux())

	if runtime.GOOS == "windows" {
		remoteRepo = g.GetRemoteRepositoryWindows()
		localRepo = g.GetLocalRepositoryWindows()
		steamAppID = domain.SteamAppID(g.GetSteamAppIdWindows())
	}

	return domain.Game{
		Code:              g.Code,
		Name:              g.Name,
		Engine:            g.Engine,
		EngineVersion:     g.EngineVersion,
		SteamAppSetConfig: g.GetSteamAppSetConfig(),
		RemoteRepository:  remoteRepo,
		LocalRepository:   localRepo,
		SteamAppID:        steamAppID,
		Metadata:          protoAnyMapToMetadata(g.GetMetadata()),
	}
}

func ProtoGameModToDomain(m *pb.GameMod) domain.GameMod {
	remoteRepo := m.GetRemoteRepositoryLinux()
	localRepo := m.GetLocalRepositoryLinux()

	if runtime.GOOS == "windows" {
		remoteRepo = m.GetRemoteRepositoryWindows()
		localRepo = m.GetLocalRepositoryWindows()
	}

	vars := make([]domain.GameModVarTemplate, 0, len(m.Vars))
	for _, v := range m.Vars {
		vars = append(vars, domain.GameModVarTemplate{
			Key:          v.Var,
			DefaultValue: v.Default,
		})
	}

	return domain.GameMod{
		ID:                     int(m.Id),
		Name:                   m.Name,
		RemoteRepository:       remoteRepo,
		LocalRepository:        localRepo,
		DefaultStartCMDLinux:   m.GetStartCmdLinux(),
		DefaultStartCMDWindows: m.GetStartCmdWindows(),
		Vars:                   vars,
		Metadata:               protoAnyMapToMetadata(m.GetMetadata()),
	}
}

func protoAnyMapToMetadata(m map[string]*anypb.Any) map[string]any {
	if len(m) == 0 {
		return nil
	}

	result := make(map[string]any, len(m))
	for k, v := range m {
		if v == nil {
			continue
		}

		msg, err := anypb.UnmarshalNew(v, proto.UnmarshalOptions{})
		if err != nil {
			continue
		}

		switch tv := msg.(type) {
		case *wrapperspb.StringValue:
			result[k] = tv.GetValue()
		case *wrapperspb.Int64Value:
			result[k] = tv.GetValue()
		case *wrapperspb.Int32Value:
			result[k] = tv.GetValue()
		case *wrapperspb.UInt64Value:
			result[k] = tv.GetValue()
		case *wrapperspb.UInt32Value:
			result[k] = tv.GetValue()
		case *wrapperspb.DoubleValue:
			result[k] = tv.GetValue()
		case *wrapperspb.FloatValue:
			result[k] = tv.GetValue()
		case *wrapperspb.BoolValue:
			result[k] = tv.GetValue()
		case *structpb.Value:
			result[k] = tv.AsInterface()
		}
	}

	return result
}

func parseVarsJSON(varsJSON string) map[string]string {
	if varsJSON == "" {
		return make(map[string]string)
	}

	vars := make(map[string]string)
	if err := json.Unmarshal([]byte(varsJSON), &vars); err != nil {
		return make(map[string]string)
	}

	return vars
}
