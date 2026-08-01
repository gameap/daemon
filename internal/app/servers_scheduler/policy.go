package serversscheduler

import (
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	pb "github.com/gameap/gameap/pkg/proto"
)

// catchupGracePeriod is the threshold beyond which a task with a past
// executeDate is considered "late" and triggers the configured catchup
// policy. Tasks less than this grace period late simply fire on the next
// tick without policy intervention.
const catchupGracePeriod = time.Minute

func mapProtoCommandToDomain(c pb.ServerTaskCommand) (domain.ServerCommand, bool) {
	switch c {
	case pb.ServerTaskCommand_SERVER_TASK_COMMAND_START:
		return domain.Start, true
	case pb.ServerTaskCommand_SERVER_TASK_COMMAND_STOP:
		return domain.Stop, true
	case pb.ServerTaskCommand_SERVER_TASK_COMMAND_RESTART:
		return domain.Restart, true
	case pb.ServerTaskCommand_SERVER_TASK_COMMAND_UPDATE:
		return domain.Update, true
	case pb.ServerTaskCommand_SERVER_TASK_COMMAND_REINSTALL:
		return domain.Reinstall, true
	case pb.ServerTaskCommand_SERVER_TASK_COMMAND_UNSPECIFIED:
		return 0, false
	default:
		return 0, false
	}
}

func mapProtoCommandToTaskCommand(c pb.ServerTaskCommand) domain.ServerTaskCommand {
	switch c {
	case pb.ServerTaskCommand_SERVER_TASK_COMMAND_START:
		return domain.ServerTaskStart
	case pb.ServerTaskCommand_SERVER_TASK_COMMAND_STOP:
		return domain.ServerTaskStop
	case pb.ServerTaskCommand_SERVER_TASK_COMMAND_RESTART:
		return domain.ServerTaskRestart
	case pb.ServerTaskCommand_SERVER_TASK_COMMAND_UPDATE:
		return domain.ServerTaskUpdate
	case pb.ServerTaskCommand_SERVER_TASK_COMMAND_REINSTALL:
		return domain.ServerTaskReinstall
	}
	return ""
}

func mapProtoOverlapPolicy(p pb.ServerTaskOverlapPolicy) domain.ServerTaskOverlapPolicy {
	switch p {
	case pb.ServerTaskOverlapPolicy_SERVER_TASK_OVERLAP_POLICY_SKIP:
		return domain.ServerTaskOverlapSkip
	case pb.ServerTaskOverlapPolicy_SERVER_TASK_OVERLAP_POLICY_QUEUE:
		return domain.ServerTaskOverlapQueue
	}
	return domain.ServerTaskOverlapSkip
}

func mapProtoCatchupPolicy(p pb.ServerTaskCatchupPolicy) domain.ServerTaskCatchupPolicy {
	switch p {
	case pb.ServerTaskCatchupPolicy_SERVER_TASK_CATCHUP_POLICY_SKIP:
		return domain.ServerTaskCatchupSkip
	case pb.ServerTaskCatchupPolicy_SERVER_TASK_CATCHUP_POLICY_RUN_ONCE:
		return domain.ServerTaskCatchupRunOnce
	}
	return domain.ServerTaskCatchupSkip
}

func protoToTaskOptions(t *pb.ServerTask, server *domain.Server) domain.ServerTaskOptions {
	opts := domain.ServerTaskOptions{
		ID:            t.GetId(),
		ServerID:      t.GetServerId(),
		NodeID:        t.GetNodeId(),
		Version:       t.GetVersion(),
		Command:       mapProtoCommandToTaskCommand(t.GetCommand()),
		Server:        server,
		Repeat:        int(t.GetRepeatCount()),
		RepeatPeriod:  t.GetRepeatPeriod().AsDuration(),
		Counter:       int(t.GetCounter()),
		OverlapPolicy: mapProtoOverlapPolicy(t.GetOverlapPolicy()),
		CatchupPolicy: mapProtoCatchupPolicy(t.GetCatchupPolicy()),
		Name:          t.GetName(),
		Timezone:      t.GetTimezone(),
		Payload:       t.GetPayload(),
		Enabled:       t.GetEnabled(),
	}

	if ts := t.GetExecuteDate(); ts != nil {
		opts.ExecuteDate = ts.AsTime()
	}
	if ts := t.GetUpdatedAt(); ts != nil {
		opts.UpdatedAt = ts.AsTime()
	}

	return opts
}

// nextFireAfter returns the next executeDate for a task whose original
// executeDate is in the past. SKIP jumps forward in whole periods to the
// first slot >= now; RUN_ONCE returns now (caller is responsible for
// recomputing the cadence after the one catchup run).
func nextFireAfter(
	executeDate time.Time,
	period time.Duration,
	policy domain.ServerTaskCatchupPolicy,
	now time.Time,
) time.Time {
	if !executeDate.Before(now) {
		return executeDate
	}
	if period <= 0 {
		if policy == domain.ServerTaskCatchupRunOnce {
			return now
		}
		return time.Time{}
	}
	if policy == domain.ServerTaskCatchupRunOnce {
		return now
	}
	elapsed := now.Sub(executeDate)
	skips := int64(elapsed/period) + 1
	return executeDate.Add(time.Duration(skips) * period)
}

// applyCatchupOnApply mutates the task's executeDate when it arrives from
// a snapshot or delta late enough to trip the configured catchup policy.
// Caller must hold no scheduler locks; the task's own mutex is taken
// internally via SetExecuteDate.
func applyCatchupOnApply(t *domain.ServerTask, now time.Time) {
	executeDate := t.ExecuteDate()
	if !executeDate.Before(now.Add(-catchupGracePeriod)) {
		return
	}
	if !t.Enabled() {
		return
	}

	next := nextFireAfter(executeDate, t.RepeatPeriod(), t.CatchupPolicy(), now)
	if !next.IsZero() {
		t.SetExecuteDate(next)
	}
}
