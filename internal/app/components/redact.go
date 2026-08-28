package components

import (
	"regexp"
	"strings"
)

const redactedPlaceholder = "***"

// secretAssignmentPattern matches the "key=value" forms that carry a credential on a command
// line, such as the "password=" argument sc.exe takes.
var secretAssignmentPattern = regexp.MustCompile(`(?i)\b(password|passwd|pwd|token|secret|api_?key)=(\S+)`)

// secretFlags are the flags whose following argument is a credential.
var secretFlags = map[string]struct{}{
	"-p":         {},
	"--password": {},
	"--token":    {},
	"--secret":   {},
}

// redactCommand hides credentials in a command line before it is echoed. The echo is streamed
// to the panel as task output, so anything it contains leaves the machine.
func redactCommand(command string) string {
	return secretAssignmentPattern.ReplaceAllString(command, "$1="+redactedPlaceholder)
}

// redactArgs hides credentials in an argument vector before it is echoed.
func redactArgs(args []string) []string {
	redacted := make([]string, len(args))

	redactValue := false

	for i, arg := range args {
		switch {
		case redactValue:
			redacted[i] = redactedPlaceholder
			redactValue = false
		default:
			redacted[i] = redactCommand(arg)

			if _, ok := secretFlags[strings.ToLower(arg)]; ok {
				redactValue = true
			}
		}
	}

	return redacted
}
