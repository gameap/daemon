package components

import (
	"regexp"
	"strings"
)

const redactedPlaceholder = "***"

// secretKeys are the credential-carrying names, in the spelling they take either as the key of
// a "key=value" argument or as the body of a flag.
const secretKeys = `password|passwd|pwd|token|secret|api[-_]?key`

// secretAssignmentPattern matches the "key=value" forms that carry a credential on a command
// line. The value may be quoted, which is how a credential containing spaces reaches a command
// line, and it may be separated from the equals sign by blanks, which is the spelling sc.exe
// documents ("password= s3cret").
var secretAssignmentPattern = regexp.MustCompile(
	`(?i)\b(` + secretKeys + `)=([ \t]*)("[^"]*"|'[^']*'|\S+)`,
)

// secretAssignmentKeyPattern matches a bare "key=" argument, the form sc.exe takes once such a
// command line has been split into an argument vector and the value stands on its own.
var secretAssignmentKeyPattern = regexp.MustCompile(`(?i)^(?:` + secretKeys + `)=$`)

// secretFlagPattern matches a credential passed as the argument following a flag, such as
// "--password s3cret". It is the same rule secretFlags applies to an argument vector, for a
// command that arrives as a single string.
var secretFlagPattern = regexp.MustCompile(`(?i)(^|\s)(-p|--(?:` + secretKeys + `))(\s+)("[^"]*"|'[^']*'|\S+)`)

// secretFlags are the flags whose following argument is a credential. "-p" is included because
// it is how the mysql clients take a password; redacting a port that happens to be passed the
// same way only makes an echoed command less readable, whereas missing one leaks a credential.
var secretFlags = map[string]struct{}{
	"-p":         {},
	"--password": {},
	"--passwd":   {},
	"--pwd":      {},
	"--token":    {},
	"--secret":   {},
	"--api-key":  {},
	"--api_key":  {},
	"--apikey":   {},
}

// redactCommand hides credentials in a command line before it is echoed. The echo is streamed
// to the panel as task output, so anything it contains leaves the machine.
func redactCommand(command string) string {
	command = secretAssignmentPattern.ReplaceAllString(command, "${1}=${2}"+redactedPlaceholder)

	return secretFlagPattern.ReplaceAllString(command, "${1}${2}${3}"+redactedPlaceholder)
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

			_, isFlag := secretFlags[strings.ToLower(arg)]
			if isFlag || secretAssignmentKeyPattern.MatchString(arg) {
				redactValue = true
			}
		}
	}

	return redacted
}
