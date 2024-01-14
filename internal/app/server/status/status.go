package status

import (
	"context"
	"io"
	"strconv"
	"time"

	"github.com/et-nik/binngo/decode"
	"github.com/gameap/daemon/internal/app/build"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/server/response"
	"github.com/pkg/errors"
)

type operationHandlerFunc func(readWriter io.ReadWriter) error

type Status struct {
	gdTaskStatsReader domain.GDTaskStatsReader
	handlers          map[Operation]operationHandlerFunc
}

func NewStatus(gdTaskStatsReader domain.GDTaskStatsReader) *Status {
	status := &Status{
		gdTaskStatsReader: gdTaskStatsReader,
	}

	status.handlers = map[Operation]operationHandlerFunc{
		Version:       status.version,
		StatusBase:    status.statusBase,
		StatusDetails: status.statusDetails,
	}

	return status
}

func (s *Status) Handle(_ context.Context, readWriter io.ReadWriter) error {
	var operation Operation
	decoder := decode.NewDecoder(readWriter)
	err := decoder.Decode(&operation)
	if errors.Is(err, io.EOF) {
		return io.EOF
	}
	if err != nil {
		return response.WriteResponse(readWriter, response.Response{
			Code: response.StatusError,
			Info: "Failed to decode message",
		})
	}

	handler, ok := s.handlers[operation]
	if !ok {
		return response.WriteResponse(readWriter, response.Response{
			Code: response.StatusError,
			Info: "Invalid operation",
		})
	}

	return handler(readWriter)
}

func (s *Status) version(readWriter io.ReadWriter) error {
	return response.WriteResponse(readWriter, &versionResponse{
		build.Version,
		build.BuildDate,
	})
}

func (s *Status) statusBase(readWriter io.ReadWriter) error {
	stats := s.gdTaskStatsReader.Stats()

	return response.WriteResponse(readWriter, &infoBaseResponse{
		Uptime:        time.Since(domain.StartTime).Truncate(1 * time.Second).String(),
		WorkingTasks:  strconv.Itoa(stats.WorkingCount),
		WaitingTasks:  strconv.Itoa(stats.WaitingCount),
		OnlineServers: "-",
	})
}

func (s *Status) statusDetails(readWriter io.ReadWriter) error {
	return response.WriteResponse(readWriter, response.Response{
		Code: response.StatusError,
		Info: "Not implemented",
	})
}
