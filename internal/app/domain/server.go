package domain

import (
	"context"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/emirpasic/gods/sets/hashset"
)

type InstallationStatus int

const (
	ServerNotInstalled = iota
	ServerInstalled
	ServerInstallInProcess
)

type ServerCommand int

const (
	Start ServerCommand = iota + 1
	Pause
	Unpause
	Status
	Stop
	Kill
	Restart
	Update
	Install
	Reinstall
	Delete
)

const autostartSettingKey = "autostart"
const autostartCurrentSettingKey = "autostart_current"
const updateBeforeStartSettingKey = "update_before_start"

type workDirReader interface {
	WorkDir() string
}

type ServerRepository interface {
	IDs(ctx context.Context) ([]int, error)
	FindByID(ctx context.Context, id int) (*Server, error)
	Save(ctx context.Context, task *Server) error
}

// Settings are impact on server management by daemon.
type Settings map[string]string

//nolint:maligned
type Server struct {
	id            int
	enabled       bool
	installStatus InstallationStatus
	blocked       bool

	name      string
	uuid      string
	uuidShort string

	game    Game
	gameMod GameMod

	ip           string
	connectPort  int
	queryPort    int
	rconPort     int
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

	settings Settings

	updatedAt time.Time

	changeset *hashset.Set

	mu *sync.Mutex
}

func NewServer(
	id int,
	enabled bool,
	installStatus InstallationStatus,
	blocked bool,
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
	settings Settings,
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
		hashset.New(),
		&sync.Mutex{},
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
	vars := make(map[string]string)
	for _, v := range s.gameMod.Vars {
		vars[v.Key] = v.DefaultValue
	}

	for k, v := range s.vars {
		vars[k] = v
	}

	return vars
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

func (s *Server) WorkDir(cfg workDirReader) string {
	return path.Clean(filepath.Join(cfg.WorkDir(), s.dir))
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
	s.mu.Lock()
	defer s.mu.Unlock()

	s.settings[key] = value
	s.setValueIsChanged("settings")
}

func (s *Server) SetStatus(processActive bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.processActive = processActive
	s.lastProcessCheck = time.Now()
	s.setValueIsChanged("status")
}

func (s *Server) AutoStart() bool {
	autostart := s.Setting(autostartCurrentSettingKey)

	if autostart == "" {
		autostart = s.Setting(autostartSettingKey)
	}

	if autostart == "" {
		return false
	}

	return s.readBoolSetting(autostart)
}

func (s *Server) AffectInstall() {
	s.AffectStop()
}

func (s *Server) AffectStart() {
	autostart := s.readBoolSetting(s.Setting(autostartSettingKey))
	if autostart {
		s.SetSetting(autostartCurrentSettingKey, "1")
	}
}

func (s *Server) AffectStop() {
	if s.AutoStart() {
		s.SetSetting(autostartCurrentSettingKey, "0")
	}
}

func (s *Server) UpdateBeforeStart() bool {
	return s.readBoolSetting(s.Setting(updateBeforeStartSettingKey))
}

func (s *Server) InstallationStatus() InstallationStatus {
	return s.installStatus
}

func (s *Server) SetInstallationStatus(status InstallationStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.installStatus = status
	s.setValueIsChanged("installationStatus")
}

func (s *Server) IsActive() bool {
	return s.processActive
}

func (s *Server) LastStatusCheck() time.Time {
	return s.lastProcessCheck
}

func (s *Server) IsModified() bool {
	return !s.changeset.Empty()
}

func (s *Server) IsValueModified(key string) bool {
	return s.changeset.Contains(strings.ToLower(key))
}

func (s *Server) setValueIsChanged(key string) {
	s.changeset.Add(strings.ToLower(key))
}

func (s *Server) UnmarkModifiedFlag() {
	s.changeset.Clear()
}

func (s *Server) readBoolSetting(value string) bool {
	value = strings.ToLower(value)
	return value == "1" || value == "true" || value == "yes"
}
