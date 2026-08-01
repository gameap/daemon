package serversscheduler

import (
	"sync"
	"time"

	pb "github.com/gameap/gameap/pkg/proto"
)

const resyncMinInterval = 5 * time.Second

type resyncTrigger struct {
	mu       sync.Mutex
	lastSent time.Time
	sender   ServerTaskSender
	nowFn    func() time.Time
}

func newResyncTrigger(sender ServerTaskSender, nowFn func() time.Time) *resyncTrigger {
	return &resyncTrigger{sender: sender, nowFn: nowFn}
}

// Trigger emits a ServerTaskResyncRequest, rate-limited to one emission
// per resyncMinInterval. v1 always sends LastKnownSnapshotVersion=0 —
// the API ignores it and always responds with a full snapshot.
func (r *resyncTrigger) Trigger() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.nowFn()
	if !r.lastSent.IsZero() && now.Sub(r.lastSent) < resyncMinInterval {
		return
	}
	r.lastSent = now

	r.sender.Send(&pb.DaemonMessage{
		Payload: &pb.DaemonMessage_ServerTaskResyncRequest{
			ServerTaskResyncRequest: &pb.ServerTaskResyncRequest{
				LastKnownSnapshotVersion: 0,
			},
		},
	})
}
