package domain

import (
	"strconv"
	"strings"

	"github.com/gameap/daemon/pkg/shellquote"
	"github.com/pkg/errors"
)

// BuildCommandArgs turns a wrapper template and a server command template into a
// concrete argument vector. Both templates are tokenized first and placeholders
// are substituted into the individual tokens afterwards, so every substituted
// value (server name, vars, rcon password, ...) always stays within a single
// argument regardless of the spaces, quotes or shell metacharacters it contains.
//
// The {command} placeholder is the only one that may expand into more than one
// argument: as a standalone token in the wrapper it is replaced by the tokens of
// the server command.
func BuildCommandArgs(
	cfg workDirReader,
	server *Server,
	wrapperTemplate string,
	serverCommand string,
) ([]string, error) {
	if wrapperTemplate == "" {
		return nil, nil
	}

	wrapTokens, err := shellquote.Split(wrapperTemplate)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to split command wrapper template")
	}

	var cmdTokens []string
	if serverCommand != "" {
		cmdTokens, err = shellquote.Split(serverCommand)
		if err != nil {
			return nil, errors.WithMessage(err, "failed to split server command")
		}
	}

	replacer := newServerReplacer(cfg, server)

	args := make([]string, 0, len(wrapTokens)+len(cmdTokens))
	for _, token := range wrapTokens {
		if token == "{command}" {
			for _, cmdToken := range cmdTokens {
				args = append(args, replacer.Replace(cmdToken))
			}

			continue
		}

		args = append(args, replacer.Replace(token))
	}

	return args, nil
}

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
	return newServerReplacer(cfg, server).Replace(commandTemplate)
}

// newServerReplacer builds a single-pass replacer for all supported
// placeholders. A single pass means a substituted value is never re-scanned, so
// one variable value cannot expand another variable's placeholder, and the
// result no longer depends on Go map iteration order. Built-in placeholders are
// registered before server variables, so a variable cannot shadow a built-in.
func newServerReplacer(cfg workDirReader, server *Server) *strings.Replacer {
	vars := server.Vars()

	const builtinPairs = 32

	pairs := make([]string, 0, builtinPairs+6*len(vars))
	pairs = append(pairs,
		"{dir}", server.WorkDir(cfg),
		"{uuid}", server.UUID(),
		"{uuid_short}", server.UUIDShort(),
		"{id}", strconv.Itoa(server.ID()),
		"{host}", server.IP(),
		"{ip}", server.IP(),
		"{port}", strconv.Itoa(server.ConnectPort()),
		"{SERVER_PORT}", strconv.Itoa(server.ConnectPort()),
		"{PORT}", strconv.Itoa(server.ConnectPort()),
		"{query_port}", strconv.Itoa(server.QueryPort()),
		"{rcon_port}", strconv.Itoa(server.RCONPort()),
		"{rcon_password}", server.RCONPassword(),
		"{game}", server.Game().StartCode,
		"{user}", server.User(),
		"{node_work_path}", cfg.WorkDir(),
		"{node_tools_path}", cfg.WorkDir()+"/tools",
	)

	for k, v := range vars {
		pairs = append(pairs,
			"{"+k+"}", v,
			"{"+strings.ToLower(k)+"}", v,
			"{"+strings.ToUpper(k)+"}", v,
		)
	}

	return strings.NewReplacer(pairs...)
}
