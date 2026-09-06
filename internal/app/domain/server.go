package domain

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"uuid"

	"github.com/emirpasic/gods/sets/hashset"
	"github.com/gameap/gameap/pkg/idgen"
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
	lastProcessCheck    time.Time
	lastTaskCompletedAt time.Time
	updatedAt           time.Time
	runningSince        time.Time
	nextStartAllowedAt  time.Time
	mu                  *sync.RWMutex
	changeset           *hashset.Set
	settings            Settings
	vars                map[string]string
	restartCommand      string
	uuid                string
	forceStopCommand    string

	// localAutostartCurrent is the autostart_current value this daemon set
	// itself. See Set for why the pushed value does not overwrite it.
	localAutostartCurrent string
	uuidShort             string
	stopCommand           string
	ip                    string
	rconPassword          string
	dir                   string
	user                  string
	startCommand          string
	name                  string
	game                  Game
	gameMod               GameMod
	ramLimit              int64 // bytes
	id                    int
	connectPort           int
	queryPort             int
	installStatus         InstallationStatus
	rconPort              int
	cpuLimit              int // millicores (1000 = 1 CPU core)
	startAttempts         int
	processActive         bool
	enabled               bool
	blocked               bool

	hasLocalAutostartCurrent bool
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
	cpuLimit int,
	ramLimit int64,
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
		cpuLimit:            cpuLimit,
		ramLimit:            ramLimit,
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
	cpuLimit int,
	ramLimit int64,
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
	s.settings = s.settingsPreservingLocalAutostart(settings)
	s.updatedAt = updatedAt
	s.cpuLimit = cpuLimit
	s.ramLimit = ramLimit
}

// settingsPreservingLocalAutostart keeps the daemon's own autostart_current
// through a settings push from the panel.
//
// autostart_current records whether the server is meant to be running, and both
// sides maintain it: the panel writes it to the database when an operator starts
// or stops a server, and the daemon writes it whenever it runs a start or stop
// command. A push replaces the settings map wholesale, so without this the
// daemon's value is silently reverted to whatever the database holds — and the
// database is not told about a stop that the daemon performed on its own, such
// as one from a scheduled task. The servers loop would then start a server the
// schedule had just stopped.
//
// Keeping the local value is safe because every panel-driven change to
// autostart_current arrives with a task that makes the daemon set the same value
// itself, so the two converge. A daemon restart drops the local value and the
// panel becomes authoritative again, which is the correct fallback.
func (s *Server) settingsPreservingLocalAutostart(incoming Settings) Settings {
	if !s.hasLocalAutostartCurrent {
		return incoming
	}

	if incoming == nil {
		incoming = make(Settings, 1)
	}

	incoming[autostartCurrentSettingKey] = s.localAutostartCurrent

	return incoming
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

func (s *Server) XID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	parsed, err := uuid.Parse(s.uuid)
	if err != nil {
		var u uuid.UUID
		u[0] = byte(s.id >> 24)
		u[1] = byte(s.id >> 16)
		u[2] = byte(s.id >> 8)
		u[3] = byte(s.id)

		return idgen.UUIDToXID([16]byte(u)).String()
	}

	return idgen.UUIDToXID([16]byte(parsed)).String()
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

	vars := make(map[string]string, len(s.gameMod.Vars)+len(s.vars)+len(s.settings))
	for _, v := range s.gameMod.Vars {
		vars[v.Key] = v.DefaultValue
	}

	for k, v := range s.vars {
		vars[k] = v
	}

	for k, v := range s.settings {
		vars[k] = v
	}

	return vars
}

func (s *Server) EnvironmentVars() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	envVars := make(map[string]string)

	// 1. Start with gameMod.Vars defaults
	for _, v := range s.gameMod.Vars {
		envVars[normalizeEnvKey(v.Key)] = v.DefaultValue
	}

	// 2. Apply server vars (overwrites defaults)
	for k, v := range s.vars {
		envVars[normalizeEnvKey(k)] = v
	}

	// 3. Apply server settings (overwrites vars)
	for k, v := range s.settings {
		envVars[normalizeEnvKey(k)] = v
	}

	// 4. Add port values (always set)
	envVars["SERVER_PORT"] = strconv.Itoa(s.connectPort)
	envVars["PORT"] = strconv.Itoa(s.connectPort)
	envVars["QUERY_PORT"] = strconv.Itoa(s.queryPort)
	envVars["RCON_PORT"] = strconv.Itoa(s.rconPort)

	return envVars
}

func normalizeEnvKey(key string) string {
	var result strings.Builder
	result.Grow(len(key))

	for _, r := range key {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			result.WriteRune(unicode.ToUpper(r))
		case r == '-' || r == ' ':
			result.WriteRune('_')
		case r == '_':
			result.WriteRune('_')
		}
	}

	return strings.Trim(result.String(), "_")
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

	if s.startCommand != "" {
		return s.startCommand
	}

	return s.gameMod.DefaultStartCMD()
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

func (s *Server) AllSettings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.settings
}

func (s *Server) Setting(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.setting(key)
}

func (s *Server) setting(key string) string {
	if val, ok := s.settings[key]; ok {
		return val
	}

	return ""
}

func (s *Server) SetSetting(key string, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.setSetting(key, value)
}

func (s *Server) setSetting(key string, value string) {
	if s.settings == nil {
		// The panel may deliver a server without any settings at all, which
		// leaves the map nil; writing into it would panic.
		s.settings = make(Settings, 1)
	}

	if key == autostartCurrentSettingKey {
		s.localAutostartCurrent = value
		s.hasLocalAutostartCurrent = true
	}

	s.settings[key] = value
	s.setValueIsChanged("settings")

	s.updatedAt = time.Now()
}

func (s *Server) SetStatus(processActive bool) {
	s.SetStatusAt(time.Now(), processActive)
}

// SetStatusAt records the outcome of a liveness probe taken at the given moment.
// It also maintains runningSince, the start of the current uninterrupted run as
// the daemon observed it: the servers loop derives both the probe interval and
// the restart backoff reset from that.
func (s *Server) SetStatusAt(now time.Time, processActive bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case !processActive:
		s.runningSince = time.Time{}
	case s.runningSince.IsZero():
		s.runningSince = now
	}

	s.processActive = processActive
	s.lastProcessCheck = now
	s.setValueIsChanged("status")

	s.updatedAt = now
}

// RunningSince reports when the server was first observed running in the current
// run, or the zero time when it is not running.
func (s *Server) RunningSince() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.runningSince
}

// NoticeStartAttempt records an automatic start attempt and holds off the next
// one until now.Add(delay).
func (s *Server) NoticeStartAttempt(now time.Time, delay time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.startAttempts++
	s.nextStartAllowedAt = now.Add(delay)
}

// ResetStartAttempts clears the restart backoff. It is called when the server is
// seen running long enough to count as recovered, and whenever a start is
// requested from outside the loop: a manual start from the panel or a scheduled
// task means the operator expects the next crash to be handled from scratch.
func (s *Server) ResetStartAttempts() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.startAttempts = 0
	s.nextStartAllowedAt = time.Time{}
}

func (s *Server) StartAttempts() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.startAttempts
}

func (s *Server) NextStartAllowedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.nextStartAllowedAt
}

func (s *Server) AutoStart() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.autoStart()
}

// AutoStartSetting reports the persistent autostart preference, ignoring the
// autostart_current runtime override.
//
// Process managers that supervise restarts themselves must configure that
// supervision from this value, not from AutoStart: autostart_current is 0 for
// the whole duration of a deliberate stop, and AffectStart only sets it back to
// 1 after the process manager has already been asked to start. Reading
// AutoStart there would rewrite the supervision policy on every stop/start
// cycle.
func (s *Server) AutoStartSetting() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.readBoolSetting(s.setting(autostartSettingKey))
}

// CanAutoStart reports whether the servers loop may bring this server up on its
// own. It is AutoStart plus the conditions that make an automatic start wrong
// regardless of the setting.
func (s *Server) CanAutoStart() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.blocked {
		return false
	}

	return s.autoStart()
}

func (s *Server) autoStart() bool {
	if !s.enabled {
		return false
	}

	autostart := s.setting(autostartCurrentSettingKey)

	if autostart == "" {
		autostart = s.setting(autostartSettingKey)
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
	s.mu.Lock()
	defer s.mu.Unlock()

	autostart := s.readBoolSetting(s.setting(autostartSettingKey))
	if autostart {
		s.setSetting(autostartCurrentSettingKey, "1")
		s.updatedAt = time.Now()
	}
}

func (s *Server) AffectStop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.autoStart() {
		s.setSetting(autostartCurrentSettingKey, "0")
		s.updatedAt = time.Now()
	}
}

func (s *Server) UpdateBeforeStart() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.readBoolSetting(s.setting(updateBeforeStartSettingKey))
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

func (s *Server) CPULimit() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.cpuLimit
}

func (s *Server) RAMLimit() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.ramLimit
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
