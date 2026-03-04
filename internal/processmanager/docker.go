//go:build linux || darwin || windows

package processmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/gameap/daemon/pkg/shellquote"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/pkg/errors"
)

const (
	defaultImage       = "debian:bookworm-slim"
	defaultStopTimeout = 30 * time.Second
	maxLogTailLines    = "500"
)

var errInstallationFailed = errors.New("installation failed")

// Docker metadata configuration keys
const (
	keyDockerImage                  = "docker_image"
	keyDockerInstallationImage      = "docker_installation_image"
	keyDockerInstallationScript     = "docker_installation_script"
	keyDockerInstallationEntrypoint = "docker_installation_entrypoint"
	keyDockerInstallationUser       = "docker_installation_user"
	keyDockerMemoryLimit            = "docker_memory_limit"
	keyDockerCPULimit               = "docker_cpu_limit"
	keyDockerNetworkMode            = "docker_network_mode"
	keyDockerContainerName          = "docker_container_name"
	keyDockerCapabilities           = "docker_capabilities"
	keyDockerPrivileged             = "docker_privileged"
	keyDockerVolumes                = "docker_volumes"
	keyDockerHealthcheck            = "docker_healthcheck"
	keyDockerDNS                    = "docker_dns"
	keyDockerWorkDir                = "docker_workdir"
)

type Docker struct {
	cfg    *config.Config
	client *client.Client
}

func NewDocker(cfg *config.Config, _, _ contracts.Executor) *Docker {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		// Client will be created lazily on first use
		return &Docker{cfg: cfg}
	}
	return &Docker{cfg: cfg, client: cli}
}

func (pm *Docker) ensureClient(ctx context.Context) error {
	if pm.client != nil {
		return nil
	}

	cli, err := client.New(client.FromEnv)
	if err != nil {
		return errors.Wrap(err, "failed to create docker client")
	}
	pm.client = cli

	// Test connection
	_, err = cli.Ping(ctx, client.PingOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to connect to docker daemon")
	}

	return nil
}

func (pm *Docker) Install(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	if err := pm.ensureClient(ctx); err != nil {
		return domain.ErrorResult, err
	}

	installImage := pm.getConfig(server, keyDockerInstallationImage)
	installScript := pm.getConfig(server, keyDockerInstallationScript)

	if installImage != "" && installScript != "" {
		return pm.runInstallation(ctx, server, installImage, installScript, out)
	}

	// Default: just pull runtime image
	return pm.pullImage(ctx, server, out)
}

func (pm *Docker) runInstallation(
	ctx context.Context,
	server *domain.Server,
	installImage, installScript string,
	out io.Writer,
) (domain.Result, error) {
	// 1. Pull installation image
	_, _ = out.Write([]byte(fmt.Sprintf("Pulling installation image %s...\n", installImage)))
	if err := pm.pullImageByName(ctx, installImage, out); err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to pull installation image")
	}

	// 2. Write script to temp file in WorkDir
	workDir := server.WorkDir(pm.cfg)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to create work directory")
	}

	scriptPath := filepath.Join(workDir, ".gameap_install.sh")
	if err := os.WriteFile(scriptPath, []byte(normalizeLineEndings(installScript)), 0600); err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to write installation script")
	}
	if err := os.Chmod(scriptPath, 0755); err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to make installation script executable")
	}
	defer func() {
		_ = os.Remove(scriptPath)
	}()

	// 3. Create temp container name
	tempName := fmt.Sprintf("gameap-install-%s", server.UUID())

	// 4. Build environment from server.Vars()
	env := make([]string, 0, len(server.Vars()))
	for k, v := range server.Vars() {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// 5. Determine user for installation container
	// Default to root for installation (most scripts need root for apt/yum/etc)
	// User can override via docker_installation_user config
	installUser := pm.getConfig(server, keyDockerInstallationUser)
	// If not specified, leave empty (runs as root)

	// 6. Container config for installation
	containerConfig := &container.Config{
		Image:      installImage,
		WorkingDir: "/mnt/server",
		Cmd: []string{
			getInstallationEntrypoint(pm.getConfig(server, keyDockerInstallationEntrypoint), installScript),
			"/mnt/server/.gameap_install.sh",
		},
		Env:  env,
		User: installUser,
	}

	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{{
			Type:   mount.TypeBind,
			Source: workDir,
			Target: "/mnt/server",
		}},
	}

	// 7. Remove existing container if any
	_, _ = pm.client.ContainerRemove(ctx, tempName, client.ContainerRemoveOptions{Force: true})

	// Debug: log installation container configuration
	configJSON, _ := json.MarshalIndent(map[string]interface{}{
		"image":      containerConfig.Image,
		"workingDir": containerConfig.WorkingDir,
		"cmd":        containerConfig.Cmd,
		"user":       containerConfig.User,
		"mounts":     hostConfig.Mounts,
	}, "", "  ")
	_, _ = out.Write([]byte(fmt.Sprintf("Installation container config:\n%s\n", configJSON)))

	// 8. Create and start container
	_, _ = out.Write([]byte("Creating installation container...\n"))
	resp, err := pm.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     containerConfig,
		HostConfig: hostConfig,
		Name:       tempName,
	})
	if err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to create installation container")
	}

	_, _ = out.Write([]byte("Starting installation container...\n"))
	if _, err := pm.client.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = pm.client.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})
		return domain.ErrorResult, errors.Wrap(err, "failed to start installation container")
	}

	// 9. Stream logs in real-time
	logsDone := make(chan struct{})
	go func() {
		defer close(logsDone)
		logs, logErr := pm.client.ContainerLogs(ctx, resp.ID, client.ContainerLogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     true,
		})
		if logErr != nil {
			return
		}
		defer logs.Close()
		_, _ = stdcopy.StdCopy(out, out, logs)
	}()

	// 10. Wait for container to finish
	waitResult := pm.client.ContainerWait(ctx, resp.ID, client.ContainerWaitOptions{
		Condition: container.WaitConditionNotRunning,
	})
	var exitCode int64

	select {
	case err := <-waitResult.Error:
		_, _ = pm.client.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})
		return domain.ErrorResult, errors.Wrap(err, "error waiting for installation container")
	case status := <-waitResult.Result:
		exitCode = status.StatusCode
	}

	// Wait for logs to finish
	<-logsDone

	// 11. Remove container
	_, _ = pm.client.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})

	if exitCode != 0 {
		return domain.ErrorResult, errors.Wrapf(errInstallationFailed, "exit code %d", exitCode)
	}

	// 12. Change ownership of installed files to server user
	uid, gid, err := pm.getUserIDs(server)
	if err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to get user IDs for chown")
	}
	_, _ = out.Write([]byte(fmt.Sprintf("Changing ownership to %s:%s...\n", uid, gid)))
	if err := chownRecursive(workDir, uid, gid); err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to change ownership of installed files")
	}

	_, _ = out.Write([]byte("Installation completed successfully\n"))
	return domain.SuccessResult, nil
}

func (pm *Docker) pullImage(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	imageName := normalizeImageName(pm.getConfig(server, keyDockerImage))
	_, _ = out.Write([]byte(fmt.Sprintf("Pulling image %s...\n", imageName)))

	if err := pm.pullImageByName(ctx, imageName, out); err != nil {
		return domain.ErrorResult, err
	}

	_, _ = out.Write([]byte("Image pulled successfully\n"))
	return domain.SuccessResult, nil
}

func (pm *Docker) pullImageByName(ctx context.Context, imageName string, out io.Writer) error {
	reader, err := pm.client.ImagePull(ctx, imageName, client.ImagePullOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to pull image")
	}
	defer reader.Close()

	_, _ = io.Copy(out, reader)
	return nil
}

func (pm *Docker) Uninstall(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	if err := pm.ensureClient(ctx); err != nil {
		return domain.ErrorResult, err
	}

	containerName := pm.containerName(server)
	_, _ = out.Write([]byte(fmt.Sprintf("Removing container %s...\n", containerName)))

	// Stop container if running
	timeout := int(defaultStopTimeout.Seconds())
	_, _ = pm.client.ContainerStop(ctx, containerName, client.ContainerStopOptions{Timeout: &timeout})

	// Remove container
	_, err := pm.client.ContainerRemove(ctx, containerName, client.ContainerRemoveOptions{Force: true})
	if err != nil && !cerrdefs.IsNotFound(err) {
		return domain.ErrorResult, errors.Wrap(err, "failed to remove container")
	}

	_, _ = out.Write([]byte("Container removed successfully\n"))
	return domain.SuccessResult, nil
}

func (pm *Docker) Start(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	if err := pm.ensureClient(ctx); err != nil {
		return domain.ErrorResult, err
	}

	containerName := pm.containerName(server)

	// Remove existing container
	_, _ = out.Write([]byte(fmt.Sprintf("Removing existing container %s if present...\n", containerName)))
	_, _ = pm.client.ContainerRemove(ctx, containerName, client.ContainerRemoveOptions{Force: true})

	// Pull image if missing
	imageName := normalizeImageName(pm.getConfig(server, keyDockerImage))
	_, err := pm.client.ImageInspect(ctx, imageName)
	if err != nil {
		_, _ = out.Write([]byte(fmt.Sprintf("Image %s not found locally, pulling...\n", imageName)))
		if pullErr := pm.pullImageByName(ctx, imageName, out); pullErr != nil {
			return domain.ErrorResult, errors.Wrap(pullErr, "failed to pull image")
		}
	}

	// Build container config
	containerConfig, hostConfig, err := pm.buildContainerConfig(server)
	if err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to build container config")
	}

	// Debug: log container configuration
	configJSON, _ := json.MarshalIndent(map[string]interface{}{
		"image":      containerConfig.Image,
		"workingDir": containerConfig.WorkingDir,
		"cmd":        containerConfig.Cmd,
		"user":       containerConfig.User,
		"env":        containerConfig.Env,
		"mounts":     hostConfig.Mounts,
	}, "", "  ")
	_, _ = out.Write([]byte(fmt.Sprintf("Container config:\n%s\n", configJSON)))

	// Create container
	_, _ = out.Write([]byte(fmt.Sprintf("Creating container %s...\n", containerName)))
	resp, err := pm.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     containerConfig,
		HostConfig: hostConfig,
		Name:       containerName,
	})
	if err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to create container")
	}

	// Start container
	_, _ = out.Write([]byte("Starting container...\n"))
	if _, err := pm.client.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to start container")
	}

	_, _ = out.Write([]byte(fmt.Sprintf("Container %s started successfully\n", containerName)))
	return domain.SuccessResult, nil
}

func (pm *Docker) Stop(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	if err := pm.ensureClient(ctx); err != nil {
		return domain.ErrorResult, err
	}

	containerName := pm.containerName(server)
	_, _ = out.Write([]byte(fmt.Sprintf("Stopping container %s...\n", containerName)))

	timeout := int(defaultStopTimeout.Seconds())
	_, err := pm.client.ContainerStop(ctx, containerName, client.ContainerStopOptions{Timeout: &timeout})
	if err != nil && !cerrdefs.IsNotFound(err) {
		return domain.ErrorResult, errors.Wrap(err, "failed to stop container")
	}

	// Remove container after stop
	_, _ = out.Write([]byte("Removing container...\n"))
	_, err = pm.client.ContainerRemove(ctx, containerName, client.ContainerRemoveOptions{})
	if err != nil && !cerrdefs.IsNotFound(err) {
		logger.Warn(ctx, errors.Wrap(err, "failed to remove container"))
	}

	_, _ = out.Write([]byte("Container stopped and removed successfully\n"))
	return domain.SuccessResult, nil
}

func (pm *Docker) Restart(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	_, err := pm.Stop(ctx, server, out)
	if err != nil {
		logger.Warn(ctx, errors.Wrap(err, "failed to stop container during restart"))
	}

	return pm.Start(ctx, server, out)
}

func (pm *Docker) Status(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	if err := pm.ensureClient(ctx); err != nil {
		return domain.ErrorResult, err
	}

	containerName := pm.containerName(server)
	inspect, err := pm.client.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			_, _ = out.Write([]byte(fmt.Sprintf("Container %s not found\n", containerName)))
			return domain.ErrorResult, nil
		}
		return domain.ErrorResult, errors.Wrap(err, "failed to inspect container")
	}

	if inspect.Container.State.Running {
		_, _ = out.Write([]byte(fmt.Sprintf("Container %s is running\n", containerName)))
		return domain.SuccessResult, nil
	}

	msg := fmt.Sprintf(
		"Container %s is not running (status: %s)\n",
		containerName, inspect.Container.State.Status,
	)
	_, _ = out.Write([]byte(msg))
	return domain.ErrorResult, nil
}

func (pm *Docker) GetOutput(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	if err := pm.ensureClient(ctx); err != nil {
		return domain.ErrorResult, err
	}

	containerName := pm.containerName(server)
	logs, err := pm.client.ContainerLogs(ctx, containerName, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       maxLogTailLines,
	})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return domain.ErrorResult, nil
		}
		return domain.ErrorResult, errors.Wrap(err, "failed to get container logs")
	}
	defer logs.Close()

	// Demultiplex stdout/stderr
	_, err = stdcopy.StdCopy(out, out, logs)
	if err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to copy logs")
	}

	return domain.SuccessResult, nil
}

func (pm *Docker) SendInput(
	ctx context.Context, input string, server *domain.Server, _ io.Writer,
) (domain.Result, error) {
	if err := pm.ensureClient(ctx); err != nil {
		return domain.ErrorResult, err
	}

	containerName := pm.containerName(server)

	// Attach to container with stdin
	resp, err := pm.client.ContainerAttach(ctx, containerName, client.ContainerAttachOptions{
		Stream: true,
		Stdin:  true,
	})
	if err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to attach to container")
	}
	defer resp.Close()

	// Write input to container stdin
	_, err = resp.Conn.Write([]byte(input + "\n"))
	if err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to write to container stdin")
	}

	return domain.SuccessResult, nil
}

func (pm *Docker) buildContainerConfig(server *domain.Server) (
	*container.Config,
	*container.HostConfig,
	error,
) {
	imageName := normalizeImageName(pm.getConfig(server, keyDockerImage))
	containerName := pm.containerName(server)

	// Parse command
	cmdSlice, err := pm.parseCommand(server)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to parse start command")
	}

	// Get user IDs
	uid, gid, err := pm.getUserIDs(server)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to get user IDs")
	}

	// Build environment variables
	env := make([]string, 0, len(server.Vars()))
	for k, v := range server.Vars() {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Container working directory
	containerWorkDir := pm.getConfig(server, keyDockerWorkDir)
	if containerWorkDir == "" {
		containerWorkDir = "/server"
	}

	// Container config
	containerConfig := &container.Config{
		Image:      imageName,
		Hostname:   containerName,
		WorkingDir: containerWorkDir,
		Env:        env,
		Cmd:        cmdSlice,
		User:       fmt.Sprintf("%s:%s", uid, gid),
		OpenStdin:  true,
		StdinOnce:  false,
	}

	// Host config
	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{{
			Type:   mount.TypeBind,
			Source: server.WorkDir(pm.cfg),
			Target: containerWorkDir,
		}},
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyDisabled},
	}

	// Port bindings
	portBindings, exposedPorts := pm.buildPortBindings(server)
	containerConfig.ExposedPorts = exposedPorts
	hostConfig.PortBindings = portBindings

	// Resource limits
	if memLimit := pm.getConfig(server, keyDockerMemoryLimit); memLimit != "" {
		if mem, parseErr := parseMemoryLimit(memLimit); parseErr == nil && mem > 0 {
			hostConfig.Resources.Memory = mem
		}
	}

	if cpuLimit := pm.getConfig(server, keyDockerCPULimit); cpuLimit != "" {
		if cpu, parseErr := parseCPULimit(cpuLimit); parseErr == nil && cpu > 0 {
			hostConfig.Resources.NanoCPUs = cpu
		}
	}

	// Capabilities
	if caps := pm.getConfig(server, keyDockerCapabilities); caps != "" {
		hostConfig.CapAdd = strings.Split(caps, ",")
	}

	// Privileged mode
	if priv := pm.getConfig(server, keyDockerPrivileged); priv == "true" {
		hostConfig.Privileged = true
	}

	// DNS
	if dns := pm.getConfig(server, keyDockerDNS); dns != "" {
		dnsServers := strings.Split(dns, ",")
		dnsAddrs := make([]netip.Addr, 0, len(dnsServers))
		for _, s := range dnsServers {
			if addr, err := netip.ParseAddr(strings.TrimSpace(s)); err == nil {
				dnsAddrs = append(dnsAddrs, addr)
			}
		}
		hostConfig.DNS = dnsAddrs
	}

	// Extra volumes
	if volumes := pm.getConfig(server, keyDockerVolumes); volumes != "" {
		extraMounts := parseExtraVolumes(volumes, server.WorkDir(pm.cfg))
		hostConfig.Mounts = append(hostConfig.Mounts, extraMounts...)
	}

	// Network mode
	if networkMode := pm.getConfig(server, keyDockerNetworkMode); networkMode == "host" {
		hostConfig.NetworkMode = "host"
	}

	return containerConfig, hostConfig, nil
}

func (pm *Docker) buildPortBindings(server *domain.Server) (network.PortMap, network.PortSet) {
	portBindings := network.PortMap{}
	exposedPorts := network.PortSet{}

	ip := server.IP()
	connectPort := server.ConnectPort()
	queryPort := server.QueryPort()
	rconPort := server.RCONPort()

	// Connect port (TCP + UDP)
	addPortBinding(portBindings, exposedPorts, ip, connectPort, "tcp")
	addPortBinding(portBindings, exposedPorts, ip, connectPort, "udp")

	// Query port (UDP) if different from connect port
	if queryPort != 0 && queryPort != connectPort {
		addPortBinding(portBindings, exposedPorts, ip, queryPort, "udp")
	}

	// RCON port (TCP) if different from connect port
	if rconPort != 0 && rconPort != connectPort {
		addPortBinding(portBindings, exposedPorts, ip, rconPort, "tcp")
	}

	return portBindings, exposedPorts
}

func addPortBinding(portBindings network.PortMap, exposedPorts network.PortSet, ip string, port int, proto string) {
	portStr := fmt.Sprintf("%d/%s", port, proto)
	netPort, err := network.ParsePort(portStr)
	if err != nil {
		return
	}
	exposedPorts[netPort] = struct{}{}

	hostIP, _ := netip.ParseAddr(ip)
	portBindings[netPort] = []network.PortBinding{{
		HostIP:   hostIP,
		HostPort: strconv.Itoa(port),
	}}
}

func (pm *Docker) parseCommand(server *domain.Server) ([]string, error) {
	cmd := domain.ReplaceShortCodes(server.StartCommand(), pm.cfg, server)
	if cmd == "" {
		return nil, ErrEmptyCommand
	}
	return shellquote.Split(cmd)
}

func (pm *Docker) containerName(server *domain.Server) string {
	if name := pm.getConfig(server, keyDockerContainerName); name != "" {
		return name
	}
	return server.UUID()
}

func (pm *Docker) getUserIDs(server *domain.Server) (string, string, error) {
	var systemUser *user.User
	var err error

	if server.User() != "" {
		systemUser, err = user.Lookup(server.User())
		if err != nil {
			return "", "", errors.Wrapf(err, "failed to lookup user %s", server.User())
		}
	} else {
		systemUser, err = user.Current()
		if err != nil {
			return "", "", errors.Wrap(err, "failed to get current user")
		}
	}

	return systemUser.Uid, systemUser.Gid, nil
}

func (pm *Docker) getConfig(server *domain.Server, key string) string {
	return getContainerConfig(pm.cfg, server, key)
}

func normalizeImageName(imageName string) string {
	if imageName == "" {
		return defaultImage
	}
	// Add :latest if no tag specified
	if !strings.Contains(imageName, ":") && !strings.Contains(imageName, "@") {
		return imageName + ":latest"
	}
	return imageName
}

// normalizeLineEndings converts Windows/Mac line endings to Unix format.
func normalizeLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n") // Windows → Unix
	s = strings.ReplaceAll(s, "\r", "\n")   // Old Mac → Unix
	return s
}

// chownRecursive changes ownership of a directory and all its contents.
func chownRecursive(path, uidStr, gidStr string) error {
	uid, err := strconv.Atoi(uidStr)
	if err != nil {
		return errors.Wrapf(err, "invalid uid: %s", uidStr)
	}
	gid, err := strconv.Atoi(gidStr)
	if err != nil {
		return errors.Wrapf(err, "invalid gid: %s", gidStr)
	}

	return filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(p, uid, gid)
	})
}

// detectShellFromShebang extracts the shell interpreter from a script's shebang line.
func detectShellFromShebang(script string) string {
	if !strings.HasPrefix(script, "#!") {
		return ""
	}
	firstLine := strings.SplitN(script, "\n", 2)[0]
	shebang := strings.TrimPrefix(firstLine, "#!")
	shebang = strings.TrimSpace(shebang)

	// Handle "#!/usr/bin/env bash" style
	if strings.HasPrefix(shebang, "/usr/bin/env ") {
		parts := strings.Fields(strings.TrimPrefix(shebang, "/usr/bin/env "))
		if len(parts) > 0 {
			return "/bin/" + parts[0]
		}
		return ""
	}

	// Return the path directly (e.g., "/bin/ash", "/bin/bash")
	if strings.HasPrefix(shebang, "/") {
		parts := strings.Fields(shebang)
		if len(parts) > 0 {
			return parts[0]
		}
	}
	return ""
}

// getInstallationEntrypoint returns the shell for running installation scripts.
// Priority: 1) config value, 2) script shebang, 3) /bin/sh fallback
func getInstallationEntrypoint(entrypointConfig, script string) string {
	if entrypointConfig != "" {
		if !strings.HasPrefix(entrypointConfig, "/") {
			return "/bin/" + entrypointConfig
		}
		return entrypointConfig
	}
	if shell := detectShellFromShebang(script); shell != "" {
		return shell
	}
	return "/bin/sh"
}

func parseMemoryLimit(s string) (int64, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, nil
	}

	multiplier := int64(1)
	switch {
	case strings.HasSuffix(s, "g"):
		multiplier = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	case strings.HasSuffix(s, "m"):
		multiplier = 1024 * 1024
		s = s[:len(s)-1]
	case strings.HasSuffix(s, "k"):
		multiplier = 1024
		s = s[:len(s)-1]
	}

	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return val * multiplier, nil
}

func parseCPULimit(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return int64(val * 1e9), nil
}

func parseExtraVolumes(volumesJSON, workDir string) []mount.Mount {
	var volumes []string
	if err := json.Unmarshal([]byte(volumesJSON), &volumes); err != nil {
		// Try parsing as comma-separated string
		volumes = strings.Split(volumesJSON, ",")
	}

	mounts := make([]mount.Mount, 0, len(volumes))
	for _, v := range volumes {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}

		parts := strings.Split(v, ":")
		if len(parts) < 2 {
			continue
		}

		source := parts[0]
		// Resolve relative paths against workDir
		if !filepath.IsAbs(source) {
			source = filepath.Join(workDir, source)
		}

		m := mount.Mount{
			Type:   mount.TypeBind,
			Source: source,
			Target: parts[1],
		}

		if len(parts) >= 3 && parts[2] == "ro" {
			m.ReadOnly = true
		}

		mounts = append(mounts, m)
	}

	return mounts
}
