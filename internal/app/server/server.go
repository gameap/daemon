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
	"github.com/gameap/daemon/internal/app/server/commands"
	"github.com/gameap/daemon/internal/app/server/files"
	"github.com/gameap/daemon/internal/app/server/response"
	servercommon "github.com/gameap/daemon/internal/app/server/server_common"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type CredentialsConfig struct {
	PasswordAuthentication bool
	Login                  string
	Password               string
}

type Server struct {
	ip   string
	port int

	certFile   string
	keyFile    string
	credConfig CredentialsConfig

	listener net.Listener
	quit     chan struct{}
	wg       sync.WaitGroup

	connTimeout time.Duration
}

type componentHandler interface {
	Handle(ctx context.Context, readWriter io.ReadWriter) error
}

func NewServer(ip string, port int, certFile string, keyFile string, credConfig CredentialsConfig) (*Server, error) {
	return &Server{
		ip:          ip,
		port:        port,
		certFile:    certFile,
		keyFile:     keyFile,
		credConfig:  credConfig,
		quit:        make(chan struct{}),
		connTimeout: 5000 * time.Second,
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

	go func() {
		<-ctx.Done()
		log.Info("Server shutting down...")
		srv.Stop()
	}()

	listener, err := tls.Listen("tcp", fmt.Sprintf("%s:%d", srv.ip, srv.port), config)
	if err != nil {
		return err
	}

	srv.listener = listener
	srv.wg.Add(1)
	log.Infof("GameAP Daemon server listening at: %s:%d", srv.ip, srv.port)

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
				log.Error("accept error", err)
				return err
			}
		}

		srv.wg.Add(1)
		go func() {
			err = srv.handleConnection(ctx, conn)
			if err != nil {
				log.Warn(errors.WithMessage(err, "handle connection"))
			}
			srv.wg.Done()
		}()
	}
}

func (srv *Server) handleConnection(ctx context.Context, conn net.Conn) error {
	defer func() {
		log.Tracef("Closing connection from %s", conn.RemoteAddr())
		err := conn.Close()
		if err != nil {
			return
		}
	}()

	err := conn.SetDeadline(time.Now().Add(srv.connTimeout))
	if err != nil {
		return err
	}

	log.Infof("Connected: %s", conn.RemoteAddr())

	var msg []interface{}
	decoder := decode.NewDecoder(conn)
	err = decoder.Decode(&msg)
	if err != nil {
		log.Warnln(errors.WithMessage(err, "failed to decode message"))
		return err
	}

	authMsg, err := createAuthMessageFromSliceInterface(msg)
	if err != nil {
		log.Warnln(errors.WithMessage(err, "failed to create auth message"))

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
		handler = commands.NewCommands()
	case ModeFiles:
		handler = files.NewFiles()
	default:
		err := response.WriteResponse(conn, response.Response{
			Code: response.StatusError,
			Info: "Invalid mode",
		})
		if err != nil {
			log.Error(err)
			return err
		}
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

func (srv *Server) Stop() {
	close(srv.quit)
	err := srv.listener.Close()
	if err != nil {
		log.Error(err)
	}
	srv.wg.Wait()
}
