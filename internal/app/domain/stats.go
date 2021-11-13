package domain

import "time"

var StartTime time.Time

type GDTaskStats struct {
	WorkingCount int
	WaitingCount int
}

type GDTaskStatsReader interface {
	Stats() GDTaskStats
}
