package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/et-nik/binngo/decode"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/server/commands"
	"github.com/gameap/daemon/internal/app/server/files"
	"github.com/gameap/daemon/internal/app/server/response"
	servercommon "github.com/gameap/daemon/internal/app/server/server_common"
	"github.com/gameap/daemon/internal/app/server/status"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var errInvalidMode = errors.New("invalid server mode")

type CredentialsConfig struct {
	Login                  string
	Password               string
	PasswordAuthentication bool
}

type Server struct {
	listener        net.Listener
	executor        contracts.Executor
	taskStatsReader domain.GDTaskStatsReader

	quit chan struct{}

	ip          string
	certFile    string
	keyFile     string
	credConfig  CredentialsConfig
	wg          sync.WaitGroup
	port        int
	connTimeout time.Duration
}

type componentHandler interface {
	Handle(ctx context.Context, readWriter io.ReadWriter) error
}

func NewServer(
	ip string,
	port int,
	certFile string,
	keyFile string,
	credConfig CredentialsConfig,
	executor contracts.Executor,
	taskStatsReader domain.GDTaskStatsReader,
) (*Server, error) {
	return &Server{
		ip:              ip,
		port:            port,
		certFile:        certFile,
		keyFile:         keyFile,
		credConfig:      credConfig,
		quit:            make(chan struct{}),
		connTimeout:     5 * time.Second,
		executor:        executor,
		taskStatsReader: taskStatsReader,
	}, nil
}

func (srv *Server) Run(ctx context.Context) error {
	cer, err := tls.LoadX509KeyPair(srv.certFile, srv.keyFile)
	if err != nil {
		return err
	}

	config := &tls.Config{
		Certificates:             []tls.Certificate{cer},
		MinVersion:               tls.VersionTLS12,
		CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
	}

	listener, err := tls.Listen("tcp", fmt.Sprintf("%s:%d", srv.ip, srv.port), config)
	if err != nil {
		return err
	}

	srv.listener = listener
	srv.wg.Add(1)
	logger.Infof(ctx, "GameAP Daemon server listening at: %s:%d", srv.ip, srv.port)

	go func() {
		<-ctx.Done()
		logger.Info(ctx, "Server shutting down...")
		srv.Stop(ctx)
	}()

	return srv.serve(ctx)
}

func (srv *Server) serve(ctx context.Context) error {
	defer srv.wg.Done()

	for {
		conn, err := srv.listener.Accept()
		if err != nil {
			select {
			case <-srv.quit:
				return nil
			default:
				logger.Info(ctx, "Accept error")
				return err
			}
		}

		srv.wg.Add(1)
		go func() {
			err = srv.handleConnection(ctx, conn)
			if err != nil && !errors.Is(err, io.EOF) {
				logger.WithError(ctx, err).Warn("Handle connection")
			}

			logger.Tracef(ctx, "Closing connection from %s", conn.RemoteAddr())
			err = conn.Close()
			if err != nil {
				logger.WithError(ctx, err).Warn("Failed to close connection")
			}

			srv.wg.Done()
		}()
	}
}

func (srv *Server) handleConnection(ctx context.Context, conn net.Conn) error {
	err := conn.SetDeadline(time.Now().Add(srv.connTimeout))
	if err != nil {
		return err
	}

	ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithFields(log.Fields{
		"client": conn.RemoteAddr(),
	}))

	var msg []interface{}
	decoder := decode.NewDecoder(conn)
	err = decoder.Decode(&msg)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		logger.WithError(ctx, err).Warn("failed to decode message")
		return errors.WithMessage(err, "failed to decode message")
	}

	authMsg, err := createAuthMessageFromSliceInterface(msg)
	if err != nil {
		logger.WithError(ctx, err).Warn("failed to create auth message")

		return response.WriteResponse(conn, response.Response{
			Code: response.StatusError,
			Info: "Invalid message",
		})
	}

	if !srv.auth(authMsg.Login, authMsg.Password) {
		return response.WriteResponse(conn, response.Response{
			Code: response.StatusError,
			Info: "Auth failed",
		})
	}

	err = response.WriteResponse(conn, response.Response{
		Code: response.StatusOK,
		Info: "Auth success",
	})
	if err != nil {
		return errors.WithMessage(err, "failed to write auth response")
	}

	err = servercommon.ReadEndBytes(ctx, conn)
	if err != nil {
		return err
	}

	return srv.serveComponent(ctx, conn, authMsg.Mode)
}

func (srv *Server) auth(login string, password string) bool {
	if srv.credConfig.PasswordAuthentication {
		if srv.credConfig.Login != login || srv.credConfig.Password != password {
			return false
		}
	}

	return true
}

func (srv *Server) serveComponent(ctx context.Context, conn net.Conn, m Mode) error {
	var handler componentHandler
	switch m {
	case ModeCommands:
		handler = commands.NewCommands(srv.executor)
	case ModeFiles:
		handler = files.NewFiles()
	case ModeStatus:
		handler = status.NewStatus(srv.taskStatsReader)
	default:
		err := response.WriteResponse(conn, response.Response{
			Code: response.StatusError,
			Info: "Invalid mode",
		})
		if err != nil {
			logger.WithError(ctx, err).Warn("Failed to write response")
			return err
		}

		return errInvalidMode
	}

	for {
		select {
		case <-srv.quit:
			return nil
		default:
			err := conn.SetDeadline(time.Now().Add(srv.connTimeout))
			if err != nil {
				return err
			}

			err = handler.Handle(ctx, conn)
			if err != nil {
				return err
			}

			err = servercommon.ReadEndBytes(ctx, conn)
			if err != nil {
				return err
			}
		}
	}
}

func (srv *Server) Stop(ctx context.Context) {
	close(srv.quit)
	err := srv.listener.Close()
	if err != nil {
		logger.WithError(ctx, err).Error("Failed to stop server")
	}
	srv.wg.Wait()
}
