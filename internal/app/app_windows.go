//go:build windows
// +build windows

package app

import (
	"context"
	"sync"

	"github.com/judwhite/go-svc"
	log "github.com/sirupsen/logrus"
)

type program struct {
	contextCancel context.CancelFunc
	args          []string
	wg            sync.WaitGroup
}

func Run(args []string) {
	prg := &program{
		args: args,
	}

	// Call svc.Run to start your program/service.
	if err := svc.Run(prg); err != nil {
		log.Fatal(err)
	}
}

func (p *program) Init(env svc.Environment) error {
	if env.IsWindowsService() {
		log.Println("Running as Windows service")
	} else {
		log.Println("Running as Windows standalone application")
	}
	return nil
}

func (p *program) Start() error {
	// The Start method must not block, or Windows may assume your service failed
	ctx, cancel := context.WithCancel(context.Background())
	p.contextCancel = cancel

	p.wg.Add(1)
	go func() {
		run(ctx, p.args)
		p.wg.Done()
	}()

	return nil
}

func (p *program) Stop() error {
	log.Println("Stopping...")
	p.contextCancel()
	p.wg.Wait()
	log.Println("Stopped.")
	return nil
}
