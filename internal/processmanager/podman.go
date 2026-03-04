//go:build linux || darwin

package processmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/gameap/daemon/pkg/shellquote"
	"github.com/pkg/errors"
)

const (
	podmanDefaultStopTimeout = 30 * time.Second
	podmanMaxLogTailLines    = 500
	podmanAPIVersion         = "v4.0.0"
)

var (
	errPodmanInstallationFailed = errors.New("installation failed")
	errPodmanPullImage          = errors.New("failed to pull image")
	errPodmanCreateExec         = errors.New("failed to create exec")
	errPodmanCreateContainer    = errors.New("failed to create container")
	errPodmanStartContainer     = errors.New("failed to start container")
	errPodmanStopContainer      = errors.New("failed to stop container")
	errPodmanRemoveContainer    = errors.New("failed to remove container")
	errPodmanWaitContainer      = errors.New("failed to wait for container")
	errPodmanInspectContainer   = errors.New("failed to inspect container")
	errPodmanGetLogs            = errors.New("failed to get logs")
)

// Podman metadata configuration keys (same as Docker for compatibility)
const (
	keyPodmanImage                  = "docker_image"
	keyPodmanInstallationImage      = "docker_installation_image"
	keyPodmanInstallationScript     = "docker_installation_script"
	keyPodmanInstallationEntrypoint = "docker_installation_entrypoint"
	keyPodmanInstallationUser       = "docker_installation_user"
	keyPodmanMemoryLimit            = "docker_memory_limit"
	keyPodmanCPULimit               = "docker_cpu_limit"
	keyPodmanNetworkMode            = "docker_network_mode"
	keyPodmanContainerName          = "docker_container_name"
	keyPodmanCapabilities           = "docker_capabilities"
	keyPodmanPrivileged             = "docker_privileged"
	keyPodmanVolumes                = "docker_volumes"
	keyPodmanDNS                    = "docker_dns"
	keyPodmanWorkDir                = "docker_workdir"
)

type Podman struct {
	cfg        *config.Config
	socketPath string
	httpClient *http.Client
}

func NewPodman(cfg *config.Config, _, _ contracts.Executor) *Podman {
	socketPath := getDefaultPodmanSocket(cfg)

	// Create HTTP client with Unix socket transport
	transport := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			// Strip "unix://" prefix if present
			sock := strings.TrimPrefix(socketPath, "unix://")
			return net.Dial("unix", sock)
		},
	}

	return &Podman{
		cfg:        cfg,
		socketPath: socketPath,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   5 * time.Minute,
		},
	}
}

func getDefaultPodmanSocket(cfg *config.Config) string {
	// Check process manager config first
	if cfg.ProcessManager.Config != nil {
		if socketPath, ok := cfg.ProcessManager.Config["socket_path"]; ok && socketPath != "" {
			return socketPath
		}
	}

	// Try rootless socket first
	if uid := os.Getuid(); uid != 0 {
		sockPath := fmt.Sprintf("/run/user/%d/podman/podman.sock", uid)
		if _, err := os.Stat(sockPath); err == nil {
			return "unix://" + sockPath
		}
	}

	// Fall back to root socket
	return "unix:///run/podman/podman.sock"
}

func (pm *Podman) apiURL(path string) string {
	return fmt.Sprintf("http://d/%s/libpod%s", podmanAPIVersion, path)
}

func (pm *Podman) doRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal request body")
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, pm.apiURL(path), bodyReader)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create request")
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return pm.httpClient.Do(req)
}

func (pm *Podman) Install(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	installImage := pm.getConfig(server, keyPodmanInstallationImage)
	installScript := pm.getConfig(server, keyPodmanInstallationScript)

	if installImage != "" && installScript != "" {
		return pm.runInstallation(ctx, server, installImage, installScript, out)
	}

	// Default: just pull runtime image
	return pm.pullImage(ctx, server, out)
}

func (pm *Podman) runInstallation(
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
	env := make(map[string]string, len(server.Vars()))
	for k, v := range server.Vars() {
		env[k] = v
	}

	// 5. Determine user for installation container
	// Default to root for installation (most scripts need root for apt/yum/etc)
	// User can override via docker_installation_user config
	installUser := pm.getConfig(server, keyPodmanInstallationUser)
	// If not specified, leave empty (runs as root)

	// 6. Remove existing container if any
	_ = pm.removeContainer(ctx, tempName)

	// 7. Create container spec
	entrypoint := getInstallationEntrypoint(pm.getConfig(server, keyPodmanInstallationEntrypoint), installScript)
	spec := pm.buildInstallSpec(tempName, installImage, workDir, entrypoint, installUser, env)

	// Debug: log installation container configuration
	specJSON, _ := json.MarshalIndent(spec, "", "  ")
	_, _ = out.Write([]byte(fmt.Sprintf("Installation container config:\n%s\n", specJSON)))

	// 8. Create container
	_, _ = out.Write([]byte("Creating installation container...\n"))
	containerID, err := pm.createContainer(ctx, spec)
	if err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to create installation container")
	}

	// 9. Start container
	_, _ = out.Write([]byte("Starting installation container...\n"))
	if err := pm.startContainer(ctx, containerID); err != nil {
		_ = pm.removeContainer(ctx, containerID)
		return domain.ErrorResult, errors.Wrap(err, "failed to start installation container")
	}

	// 10. Wait for container to finish
	exitCode, err := pm.waitContainer(ctx, containerID)
	if err != nil {
		_ = pm.removeContainer(ctx, containerID)
		return domain.ErrorResult, errors.Wrap(err, "error waiting for installation container")
	}

	// 11. Get logs
	logs, _ := pm.getLogs(ctx, containerID, podmanMaxLogTailLines*10)
	_, _ = out.Write([]byte(logs))

	// 12. Remove container
	_ = pm.removeContainer(ctx, containerID)

	if exitCode != 0 {
		return domain.ErrorResult, errors.Wrapf(errPodmanInstallationFailed, "exit code %d", exitCode)
	}

	// 13. Change ownership of installed files to server user
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

func (pm *Podman) buildInstallSpec(name, image, workDir, entrypoint, user string, env map[string]string) map[string]interface{} {
	return map[string]interface{}{
		"name":     name,
		"image":    image,
		"work_dir": "/mnt/server",
		"command":  []string{entrypoint, "/mnt/server/.gameap_install.sh"},
		"env":      env,
		"user":     user,
		"mounts": []map[string]interface{}{
			{
				"source":      workDir,
				"destination": "/mnt/server",
				"type":        "bind",
			},
		},
	}
}

func (pm *Podman) pullImage(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	imageName := normalizeImageName(pm.getConfig(server, keyPodmanImage))
	_, _ = out.Write([]byte(fmt.Sprintf("Pulling image %s...\n", imageName)))

	if err := pm.pullImageByName(ctx, imageName, out); err != nil {
		return domain.ErrorResult, err
	}

	_, _ = out.Write([]byte("Image pulled successfully\n"))
	return domain.SuccessResult, nil
}

func (pm *Podman) pullImageByName(ctx context.Context, imageName string, out io.Writer) error {
	path := fmt.Sprintf("/images/pull?reference=%s", imageName)
	resp, err := pm.doRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return errors.Wrap(err, "failed to pull image")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return errors.Wrapf(errPodmanPullImage, "%s", string(body))
	}

	// Stream the response to output
	_, _ = io.Copy(out, resp.Body)
	return nil
}

func (pm *Podman) Uninstall(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	containerName := pm.containerName(server)
	_, _ = out.Write([]byte(fmt.Sprintf("Removing container %s...\n", containerName)))

	// Stop container if running
	_ = pm.stopContainer(ctx, containerName)

	// Remove container
	if err := pm.removeContainer(ctx, containerName); err != nil && !isPodmanNotFoundError(err) {
		return domain.ErrorResult, errors.Wrap(err, "failed to remove container")
	}

	_, _ = out.Write([]byte("Container removed successfully\n"))
	return domain.SuccessResult, nil
}

func (pm *Podman) Start(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	containerName := pm.containerName(server)

	// Remove existing container
	_, _ = out.Write([]byte(fmt.Sprintf("Removing existing container %s if present...\n", containerName)))
	_ = pm.removeContainer(ctx, containerName)

	// Pull image if missing
	imageName := normalizeImageName(pm.getConfig(server, keyPodmanImage))
	if !pm.imageExists(ctx, imageName) {
		_, _ = out.Write([]byte(fmt.Sprintf("Image %s not found locally, pulling...\n", imageName)))
		if pullErr := pm.pullImageByName(ctx, imageName, out); pullErr != nil {
			return domain.ErrorResult, errors.Wrap(pullErr, "failed to pull image")
		}
	}

	// Build container spec
	spec, err := pm.buildContainerSpec(server)
	if err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to build container spec")
	}

	// Debug: log container configuration
	specJSON, _ := json.MarshalIndent(spec, "", "  ")
	_, _ = out.Write([]byte(fmt.Sprintf("Container config:\n%s\n", specJSON)))

	// Create container
	_, _ = out.Write([]byte(fmt.Sprintf("Creating container %s...\n", containerName)))
	containerID, err := pm.createContainer(ctx, spec)
	if err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to create container")
	}

	// Start container
	_, _ = out.Write([]byte("Starting container...\n"))
	if err := pm.startContainer(ctx, containerID); err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to start container")
	}

	_, _ = out.Write([]byte(fmt.Sprintf("Container %s started successfully\n", containerName)))
	return domain.SuccessResult, nil
}

func (pm *Podman) Stop(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	containerName := pm.containerName(server)
	_, _ = out.Write([]byte(fmt.Sprintf("Stopping container %s...\n", containerName)))

	if err := pm.stopContainer(ctx, containerName); err != nil && !isPodmanNotFoundError(err) {
		return domain.ErrorResult, errors.Wrap(err, "failed to stop container")
	}

	// Remove container after stop
	_, _ = out.Write([]byte("Removing container...\n"))
	if err := pm.removeContainer(ctx, containerName); err != nil && !isPodmanNotFoundError(err) {
		logger.Warn(ctx, errors.Wrap(err, "failed to remove container"))
	}

	_, _ = out.Write([]byte("Container stopped and removed successfully\n"))
	return domain.SuccessResult, nil
}

func (pm *Podman) Restart(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	_, err := pm.Stop(ctx, server, out)
	if err != nil {
		logger.Warn(ctx, errors.Wrap(err, "failed to stop container during restart"))
	}

	return pm.Start(ctx, server, out)
}

func (pm *Podman) Status(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	containerName := pm.containerName(server)
	running, status, err := pm.inspectContainer(ctx, containerName)
	if err != nil {
		if isPodmanNotFoundError(err) {
			_, _ = out.Write([]byte(fmt.Sprintf("Container %s not found\n", containerName)))
			return domain.ErrorResult, nil
		}
		return domain.ErrorResult, errors.Wrap(err, "failed to inspect container")
	}

	if running {
		_, _ = out.Write([]byte(fmt.Sprintf("Container %s is running\n", containerName)))
		return domain.SuccessResult, nil
	}

	_, _ = out.Write([]byte(fmt.Sprintf("Container %s is not running (status: %s)\n", containerName, status)))
	return domain.ErrorResult, nil
}

func (pm *Podman) GetOutput(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	containerName := pm.containerName(server)
	logs, err := pm.getLogs(ctx, containerName, podmanMaxLogTailLines)
	if err != nil {
		if isPodmanNotFoundError(err) {
			return domain.ErrorResult, nil
		}
		return domain.ErrorResult, errors.Wrap(err, "failed to get container logs")
	}

	_, _ = out.Write([]byte(logs))
	return domain.SuccessResult, nil
}

func (pm *Podman) SendInput(
	ctx context.Context, input string, server *domain.Server, _ io.Writer,
) (domain.Result, error) {
	containerName := pm.containerName(server)

	// Use exec to send input (simpler than attach for single commands)
	execSpec := map[string]interface{}{
		"AttachStdin":  true,
		"AttachStdout": false,
		"AttachStderr": false,
		"Tty":          false,
		"Cmd":          []string{"/bin/sh", "-c", fmt.Sprintf("echo %q", input)},
	}

	path := fmt.Sprintf("/containers/%s/exec", containerName)
	resp, err := pm.doRequest(ctx, http.MethodPost, path, execSpec)
	if err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to create exec")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return domain.ErrorResult, errors.Wrapf(errPodmanCreateExec, "%s", string(body))
	}

	var execResp struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&execResp); err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to decode exec response")
	}

	// Start exec
	startPath := fmt.Sprintf("/exec/%s/start", execResp.ID)
	startResp, err := pm.doRequest(ctx, http.MethodPost, startPath, map[string]bool{"Detach": true})
	if err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to start exec")
	}
	defer startResp.Body.Close()

	return domain.SuccessResult, nil
}

func (pm *Podman) buildContainerSpec(server *domain.Server) (map[string]interface{}, error) {
	imageName := normalizeImageName(pm.getConfig(server, keyPodmanImage))
	containerName := pm.containerName(server)

	// Parse command
	cmdSlice, err := pm.parseCommand(server)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse start command")
	}

	// Get user IDs
	uid, gid, err := pm.getUserIDs(server)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user IDs")
	}

	// Build environment variables
	env := make(map[string]string, len(server.Vars()))
	for k, v := range server.Vars() {
		env[k] = v
	}

	// Container working directory
	containerWorkDir := pm.getConfig(server, keyPodmanWorkDir)
	if containerWorkDir == "" {
		containerWorkDir = "/server"
	}

	spec := map[string]interface{}{
		"name":     containerName,
		"image":    imageName,
		"hostname": containerName,
		"work_dir": containerWorkDir,
		"env":      env,
		"command":  cmdSlice,
		"user":     fmt.Sprintf("%s:%s", uid, gid),
		"stdin":    true,
		"terminal": false,
		"mounts": []map[string]interface{}{
			{
				"source":      server.WorkDir(pm.cfg),
				"destination": containerWorkDir,
				"type":        "bind",
			},
		},
		"portmappings": pm.buildPortMappings(server),
	}

	// Resource limits
	resourceLimits := make(map[string]interface{})

	if memLimit := pm.getConfig(server, keyPodmanMemoryLimit); memLimit != "" {
		if mem, parseErr := parseMemoryLimit(memLimit); parseErr == nil && mem > 0 {
			resourceLimits["memory"] = map[string]int64{"limit": mem}
		}
	}

	if cpuLimit := pm.getConfig(server, keyPodmanCPULimit); cpuLimit != "" {
		if cpu, parseErr := parseCPULimit(cpuLimit); parseErr == nil && cpu > 0 {
			period := int64(100000)
			quota := int64((float64(cpu) / 1e9) * float64(period))
			resourceLimits["cpu"] = map[string]int64{
				"period": period,
				"quota":  quota,
			}
		}
	}

	if len(resourceLimits) > 0 {
		spec["resource_limits"] = resourceLimits
	}

	// Capabilities
	if caps := pm.getConfig(server, keyPodmanCapabilities); caps != "" {
		spec["cap_add"] = strings.Split(caps, ",")
	}

	// Privileged mode
	if priv := pm.getConfig(server, keyPodmanPrivileged); priv == "true" {
		spec["privileged"] = true
	}

	// DNS
	if dns := pm.getConfig(server, keyPodmanDNS); dns != "" {
		spec["dns_server"] = strings.Split(dns, ",")
	}

	// Extra volumes
	if volumes := pm.getConfig(server, keyPodmanVolumes); volumes != "" {
		extraMounts := pm.parseExtraVolumes(volumes, server.WorkDir(pm.cfg))
		if len(extraMounts) > 0 {
			mounts := spec["mounts"].([]map[string]interface{})
			spec["mounts"] = append(mounts, extraMounts...)
		}
	}

	// Network mode
	if networkMode := pm.getConfig(server, keyPodmanNetworkMode); networkMode == "host" {
		spec["netns"] = map[string]string{"nsmode": "host"}
	}

	return spec, nil
}

func (pm *Podman) buildPortMappings(server *domain.Server) []map[string]interface{} {
	var portMappings []map[string]interface{}

	ip := server.IP()
	connectPort := server.ConnectPort()
	queryPort := server.QueryPort()
	rconPort := server.RCONPort()

	// Connect port (TCP + UDP)
	portMappings = append(portMappings,
		map[string]interface{}{"host_ip": ip, "host_port": connectPort, "container_port": connectPort, "protocol": "tcp"},
		map[string]interface{}{"host_ip": ip, "host_port": connectPort, "container_port": connectPort, "protocol": "udp"},
	)

	// Query port (UDP) if different from connect port
	if queryPort != 0 && queryPort != connectPort {
		portMappings = append(portMappings,
			map[string]interface{}{"host_ip": ip, "host_port": queryPort, "container_port": queryPort, "protocol": "udp"},
		)
	}

	// RCON port (TCP) if different from connect port
	if rconPort != 0 && rconPort != connectPort {
		portMappings = append(portMappings,
			map[string]interface{}{"host_ip": ip, "host_port": rconPort, "container_port": rconPort, "protocol": "tcp"},
		)
	}

	return portMappings
}

func (pm *Podman) createContainer(ctx context.Context, spec map[string]interface{}) (string, error) {
	resp, err := pm.doRequest(ctx, http.MethodPost, "/containers/create", spec)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", errors.Wrapf(errPodmanCreateContainer, "%s", string(body))
	}

	var createResp struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		return "", errors.Wrap(err, "failed to decode create response")
	}

	return createResp.ID, nil
}

func (pm *Podman) startContainer(ctx context.Context, nameOrID string) error {
	path := fmt.Sprintf("/containers/%s/start", nameOrID)
	resp, err := pm.doRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return errors.Wrapf(errPodmanStartContainer, "%s", string(body))
	}

	return nil
}

func (pm *Podman) stopContainer(ctx context.Context, nameOrID string) error {
	timeout := int(podmanDefaultStopTimeout.Seconds())
	path := fmt.Sprintf("/containers/%s/stop?timeout=%d", nameOrID, timeout)
	resp, err := pm.doRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent &&
		resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return errors.Wrapf(errPodmanStopContainer, "%s", string(body))
	}

	return nil
}

func (pm *Podman) removeContainer(ctx context.Context, nameOrID string) error {
	path := fmt.Sprintf("/containers/%s?force=true", nameOrID)
	resp, err := pm.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusNoContent &&
		resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return errors.Wrapf(errPodmanRemoveContainer, "%s", string(body))
	}

	return nil
}

func (pm *Podman) waitContainer(ctx context.Context, nameOrID string) (int, error) {
	path := fmt.Sprintf("/containers/%s/wait", nameOrID)
	resp, err := pm.doRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return -1, errors.Wrapf(errPodmanWaitContainer, "%s", string(body))
	}

	var waitResp struct {
		StatusCode int `json:"StatusCode"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&waitResp); err != nil {
		return -1, errors.Wrap(err, "failed to decode wait response")
	}

	return waitResp.StatusCode, nil
}

func (pm *Podman) inspectContainer(ctx context.Context, nameOrID string) (bool, string, error) {
	path := fmt.Sprintf("/containers/%s/json", nameOrID)
	resp, err := pm.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return false, "", errors.New("container not found")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, "", errors.Wrapf(errPodmanInspectContainer, "%s", string(body))
	}

	var inspectResp struct {
		State struct {
			Running bool   `json:"Running"`
			Status  string `json:"Status"`
		} `json:"State"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&inspectResp); err != nil {
		return false, "", errors.Wrap(err, "failed to decode inspect response")
	}

	return inspectResp.State.Running, inspectResp.State.Status, nil
}

func (pm *Podman) getLogs(ctx context.Context, nameOrID string, tailLines int) (string, error) {
	path := fmt.Sprintf("/containers/%s/logs?stdout=true&stderr=true&tail=%d", nameOrID, tailLines)
	resp, err := pm.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", errors.New("container not found")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", errors.Wrapf(errPodmanGetLogs, "%s", string(body))
	}

	logs, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", errors.Wrap(err, "failed to read logs")
	}

	return string(logs), nil
}

func (pm *Podman) imageExists(ctx context.Context, imageName string) bool {
	path := fmt.Sprintf("/images/%s/exists", imageName)
	resp, err := pm.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusNoContent
}

func (pm *Podman) parseCommand(server *domain.Server) ([]string, error) {
	cmd := domain.ReplaceShortCodes(server.StartCommand(), pm.cfg, server)
	if cmd == "" {
		return nil, ErrEmptyCommand
	}
	return shellquote.Split(cmd)
}

func (pm *Podman) containerName(server *domain.Server) string {
	if name := pm.getConfig(server, keyPodmanContainerName); name != "" {
		return name
	}
	return server.UUID()
}

func (pm *Podman) getUserIDs(server *domain.Server) (string, string, error) {
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

func (pm *Podman) getConfig(server *domain.Server, key string) string {
	return getContainerConfig(pm.cfg, server, key)
}

func (pm *Podman) parseExtraVolumes(volumesJSON, workDir string) []map[string]interface{} {
	var volumes []string
	if err := json.Unmarshal([]byte(volumesJSON), &volumes); err != nil {
		// Try parsing as comma-separated string
		volumes = strings.Split(volumesJSON, ",")
	}

	mounts := make([]map[string]interface{}, 0, len(volumes))
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

		m := map[string]interface{}{
			"type":        "bind",
			"source":      source,
			"destination": parts[1],
		}

		if len(parts) >= 3 && parts[2] == "ro" {
			m["options"] = []string{"ro"}
		}

		mounts = append(mounts, m)
	}

	return mounts
}

func isPodmanNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "not found") ||
		strings.Contains(errStr, "no such container") ||
		strings.Contains(errStr, "no container with")
}
