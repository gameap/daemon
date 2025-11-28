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

// WindowsArgToString quotes a string for use as a Windows command line argument.
// It wraps the string in double quotes if it contains spaces or special characters,
// and escapes any existing double quotes.
func WindowsArgToString(s string) string {
	if s == "" {
		return `""`
	}

	needsQuoting := strings.ContainsAny(s, " \t\"")
	if !needsQuoting {
		return s
	}

	// Escape double quotes and wrap in quotes
	escaped := strings.ReplaceAll(s, `"`, `\"`)
	return `"` + escaped + `"`
}
