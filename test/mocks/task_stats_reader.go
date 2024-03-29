package mocks

import (
	"github.com/gameap/daemon/internal/app/domain"
)

type TasksStatsReader struct {
	WorkingCount int
	WaitingCount int
}

func (t *TasksStatsReader) Stats() domain.GDTaskStats {
	return domain.GDTaskStats{
		WorkingCount: t.WorkingCount,
		WaitingCount: t.WaitingCount,
	}
}
