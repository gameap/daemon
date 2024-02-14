package domain

import (
	"strconv"
	"strings"
)

func MakeFullCommand(
	cfg workDirReader,
	server *Server,
	commandTemplate string,
	serverCommand string,
) string {
	commandTemplate = strings.Replace(commandTemplate, "{command}", serverCommand, 1)

	return ReplaceShortCodes(commandTemplate, cfg, server)
}

func ReplaceShortCodes(commandTemplate string, cfg workDirReader, server *Server) string {
	command := commandTemplate

	command = strings.ReplaceAll(command, "{dir}", server.WorkDir(cfg))
	command = strings.ReplaceAll(command, "{uuid}", server.UUID())
	command = strings.ReplaceAll(command, "{uuid_short}", server.UUIDShort())
	command = strings.ReplaceAll(command, "{id}", strconv.Itoa(server.ID()))

	command = strings.ReplaceAll(command, "{host}", server.IP())
	command = strings.ReplaceAll(command, "{ip}", server.IP())
	command = strings.ReplaceAll(command, "{port}", strconv.Itoa(server.ConnectPort()))
	command = strings.ReplaceAll(command, "{query_port}", strconv.Itoa(server.QueryPort()))
	command = strings.ReplaceAll(command, "{rcon_port}", strconv.Itoa(server.RCONPort()))
	command = strings.ReplaceAll(command, "{rcon_password}", server.RCONPassword())

	command = strings.ReplaceAll(command, "{game}", server.Game().StartCode)
	command = strings.ReplaceAll(command, "{user}", server.User())

	command = strings.ReplaceAll(command, "{node_work_path}", cfg.WorkDir())
	command = strings.ReplaceAll(command, "{node_tools_path}", cfg.WorkDir()+"/tools")

	for k, v := range server.Vars() {
		command = strings.ReplaceAll(command, "{"+k+"}", v)
	}

	return command
}
