package shellquote

import (
	"runtime"
	"strings"

	"github.com/gopherclass/go-shellquote"
)

func Split(input string) (words []string, err error) {
	// Escape backslashes on Windows
	// Without it shellquote.Split will split command without backslashes
	// C:\gameap\steamcmd\steamcmd.exe -> ["C:gameapsteamcmdsteamcmd.exe"]
	// Should be ["C:\\gameap\\steamcmd\\steamcmd.exe"]
	if runtime.GOOS == "windows" {
		input = strings.ReplaceAll(input, "\\", "\\\\")
	}

	return shellquote.Split(input)
}

func Join(words ...string) string {
	return shellquote.Join(words...)
}

// WindowsArgToString quotes a string for use as a single Windows command line
// argument. It follows the backslash/quote rules parsed by CommandLineToArgvW
// (and the MSVC runtime): a run of backslashes is doubled only when it precedes
// a double quote or the closing quote, and an embedded double quote is escaped
// as \". A value ending in a backslash therefore round-trips correctly instead
// of escaping the closing quote.
func WindowsArgToString(s string) string {
	if s == "" {
		return `""`
	}

	if !strings.ContainsAny(s, " \t\n\v\"") {
		return s
	}

	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')

	for i := 0; i < len(s); {
		backslashes := 0
		for i < len(s) && s[i] == '\\' {
			i++
			backslashes++
		}

		switch {
		case i == len(s):
			b.WriteString(strings.Repeat(`\`, backslashes*2))
		case s[i] == '"':
			b.WriteString(strings.Repeat(`\`, backslashes*2+1))
			b.WriteByte('"')
			i++
		default:
			b.WriteString(strings.Repeat(`\`, backslashes))
			b.WriteByte(s[i])
			i++
		}
	}

	b.WriteByte('"')

	return b.String()
}

// WindowsJoin quotes each argument with WindowsArgToString and joins them with
// spaces, producing the argument portion of a Windows command line that
// CommandLineToArgvW parses back into exactly these arguments.
func WindowsJoin(args ...string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = WindowsArgToString(a)
	}

	return strings.Join(quoted, " ")
}
