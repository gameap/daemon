package domain

import "time"

type InstallationStatus int

const (
	ServerNotInstalled = iota
	ServerInstalled
	ServerInstallInProcess
)

const autostartSetting = "autostart"
const autostartCurrentSetting = "autostartCurrent"

type Server struct {
	id            int
	enabled       int
	installStatus InstallationStatus
	blocked       int

	name string
	uuid string
	uuidShort string

	game    Game
	gameMod GameMod

	ip          string
	connectPort int
	queryPort   int
	rconPort    int
	rconPassword string

	dir  string
	user string

	startCommand     string
	stopCommand      string
	forceStopCommand string
	restartCommand   string

	processActive    bool
	lastProcessCheck time.Time

	vars map[string]string

	settings map[string]string

	updatedAt time.Time
}

func NewServer(
	id int,
	enabled int,
	installStatus InstallationStatus,
	blocked int,
	name string,
	uuid string,
	uuidShort string,
	game Game,
	gameMod GameMod,
	ip string,
	connectPort int,
	queryPort int,
	rconPort int,
	rconPassword string,
	dir string,
	user string,
	startCommand string,
	stopCommand string,
	forceStopCommand string,
	restartCommand string,
	processActive bool,
	lastProcessCheck time.Time,
	vars map[string]string,
	settings map[string]string,
	updatedAt time.Time,
) *Server {
	return &Server{
		id,
		enabled,
		installStatus,
		blocked,
		name,
		uuid,
		uuidShort,
		game,
		gameMod,
		ip,
		connectPort,
		queryPort,
		rconPort,
		rconPassword,
		dir,
		user,
		startCommand,
		stopCommand,
		forceStopCommand,
		restartCommand,
		processActive,
		lastProcessCheck,
		vars,
		settings,
		updatedAt,
	}
}

func (s *Server) ID() int {
	return s.id
}

func (s *Server) UUID() string {
	return s.uuid
}

func (s *Server) UUIDShort() string {
	return s.uuidShort
}

func (s *Server) IP() string {
	return s.ip
}

func (s *Server) ConnectPort() int {
	return s.connectPort
}

func (s *Server) QueryPort() int {
	return s.queryPort
}

func (s *Server) RCONPort() int {
	return s.rconPort
}

func (s *Server) RCONPassword() string {
	return s.rconPassword
}

func (s *Server) User() string {
	return s.user
}

func (s *Server) Vars() map[string]string {
	return s.vars
}

func (s *Server) Game() Game {
	return s.game
}

func (s *Server) GameMod() GameMod {
	return s.gameMod
}

func (s *Server) Dir() string {
	return s.dir
}

func (s *Server) StartCommand() string {
	return s.startCommand
}

func (s *Server) StopCommand() string {
	return s.stopCommand
}

func (s *Server) RestartCommand() string {
	return s.restartCommand
}

func (s *Server) Setting(key string) string {
	if val, ok := s.settings[key]; ok {
		return val
	}

	return ""
}

func (s *Server) SetSetting(key string, value string) {
	s.settings[key] = value
}

func (s *Server) AutoStart() bool {
	autostart := s.Setting(autostartCurrentSetting)

	if autostart == "" {
		autostart = s.Setting(autostartSetting)
	}

	if autostart == "" {
		return false
	}

	return autostart == "1" || autostart == "true"
}
