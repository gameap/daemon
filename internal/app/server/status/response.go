package status

import (
	"github.com/et-nik/binngo"
	"github.com/gameap/daemon/internal/app/server/response"
)

type versionResponse struct {
	Version   string
	BuildDate string
}

func (r *versionResponse) MarshalBINN() ([]byte, error) {
	resp := []interface{}{
		response.StatusOK,
		r.Version,
		r.BuildDate,
	}
	return binngo.Marshal(&resp)
}

type infoBaseResponse struct {
	Uptime        string
	WorkingTasks  string
	WaitingTasks  string
	OnlineServers string
}

func (r *infoBaseResponse) MarshalBINN() ([]byte, error) {
	resp := []interface{}{
		response.StatusOK,
		r.Uptime,
		r.WorkingTasks,
		r.WaitingTasks,
		r.OnlineServers,
	}
	return binngo.Marshal(&resp)
}
