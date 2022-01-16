//go:build !windows && !plan9
// +build !windows,!plan9

package app

import "context"

func Run(args []string) {
	run(context.Background(), args)
}
