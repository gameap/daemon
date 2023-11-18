package domain

import (
	"context"
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

	updatedAt           time.Time
	lastTaskCompletedAt time.Time

	changeset *hashset.Set

	mu *sync.RWMutex
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
		id:                  id,
		enabled:             enabled,
		installStatus:       installStatus,
		blocked:             blocked,
		name:                name,
		uuid:                uuid,
		uuidShort:           uuidShort,
		game:                game,
		gameMod:             gameMod,
		ip:                  ip,
		connectPort:         connectPort,
		queryPort:           queryPort,
		rconPort:            rconPort,
		rconPassword:        rconPassword,
		dir:                 dir,
		user:                user,
		startCommand:        startCommand,
		stopCommand:         stopCommand,
		forceStopCommand:    forceStopCommand,
		restartCommand:      restartCommand,
		processActive:       processActive,
		lastProcessCheck:    lastProcessCheck,
		vars:                vars,
		settings:            settings,
		updatedAt:           updatedAt,
		lastTaskCompletedAt: time.Unix(0, 0),
		changeset:           hashset.New(),
		mu:                  &sync.RWMutex{},
	}
}

func (s *Server) ID() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.id
}

func (s *Server) Set(
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
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.enabled = enabled
	s.installStatus = installStatus
	s.blocked = blocked
	s.name = name
	s.uuid = uuid
	s.uuidShort = uuidShort
	s.game = game
	s.gameMod = gameMod
	s.ip = ip
	s.connectPort = connectPort
	s.queryPort = queryPort
	s.rconPort = rconPort
	s.rconPassword = rconPassword
	s.dir = dir
	s.user = user
	s.startCommand = startCommand
	s.stopCommand = stopCommand
	s.forceStopCommand = forceStopCommand
	s.restartCommand = restartCommand
	s.processActive = processActive
	s.lastProcessCheck = lastProcessCheck
	s.vars = vars
	s.settings = settings
	s.updatedAt = updatedAt
}

func (s *Server) Enabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.enabled
}

func (s *Server) Blocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.blocked
}

func (s *Server) UUID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.uuid
}

func (s *Server) UUIDShort() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.uuidShort
}

func (s *Server) IP() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.ip
}

func (s *Server) ConnectPort() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.connectPort
}

func (s *Server) QueryPort() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.queryPort
}

func (s *Server) RCONPort() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.rconPort
}

func (s *Server) RCONPassword() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.rconPassword
}

func (s *Server) User() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.user
}

func (s *Server) Vars() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

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
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.game
}

func (s *Server) GameMod() GameMod {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.gameMod
}

func (s *Server) Dir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.dir
}

func (s *Server) WorkDir(cfg workDirReader) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return filepath.Clean(filepath.Join(cfg.WorkDir(), s.dir))
}

func (s *Server) StartCommand() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.startCommand
}

func (s *Server) StopCommand() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.stopCommand
}

func (s *Server) RestartCommand() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.restartCommand
}

func (s *Server) Setting(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

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

	s.updatedAt = time.Now()
}

func (s *Server) SetStatus(processActive bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.processActive = processActive
	s.lastProcessCheck = time.Now()
	s.setValueIsChanged("status")

	s.updatedAt = time.Now()
}

func (s *Server) AutoStart() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

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
	s.mu.RLock()
	defer s.mu.RUnlock()

	autostart := s.readBoolSetting(s.Setting(autostartSettingKey))
	if autostart {
		s.SetSetting(autostartCurrentSettingKey, "1")
		s.updatedAt = time.Now()
	}
}

func (s *Server) AffectStop() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.AutoStart() {
		s.SetSetting(autostartCurrentSettingKey, "0")
		s.updatedAt = time.Now()
	}
}

func (s *Server) UpdateBeforeStart() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.readBoolSetting(s.Setting(updateBeforeStartSettingKey))
}

func (s *Server) InstallationStatus() InstallationStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.installStatus
}

func (s *Server) SetInstallationStatus(status InstallationStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.installStatus = status
	s.setValueIsChanged("installationStatus")
	s.updatedAt = time.Now()
}

func (s *Server) IsActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.processActive
}

func (s *Server) LastStatusCheck() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.lastProcessCheck
}

func (s *Server) IsModified() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return !s.changeset.Empty()
}

func (s *Server) IsValueModified(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.changeset.Contains(strings.ToLower(key))
}

func (s *Server) setValueIsChanged(key string) {
	s.changeset.Add(strings.ToLower(key))
}

func (s *Server) UnmarkModifiedFlag() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.changeset.Clear()
	s.updatedAt = time.Now()
}

func (s *Server) readBoolSetting(value string) bool {
	value = strings.ToLower(value)
	return value == "1" || value == "true" || value == "yes"
}

func (s *Server) UpdatedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.updatedAt
}

func (s *Server) NoticeTaskCompleted() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastTaskCompletedAt = time.Now()
	s.updatedAt = time.Now()
}

func (s *Server) LastTaskCompletedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.lastTaskCompletedAt
}
