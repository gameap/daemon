package grpc

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

type ConnectionManager struct {
	cfg        *config.Config
	conn       *grpc.ClientConn
	client     *GatewayClient
	onConnect  []func(conn *grpc.ClientConn)
	mu         sync.RWMutex
	reconnects int
}

func NewConnectionManager(cfg *config.Config, client *GatewayClient) *ConnectionManager {
	return &ConnectionManager{
		cfg:    cfg,
		client: client,
	}
}

func (cm *ConnectionManager) OnConnect(fn func(conn *grpc.ClientConn)) {
	cm.onConnect = append(cm.onConnect, fn)
}

func (cm *ConnectionManager) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return cm.Close()
		default:
			if err := cm.connectAndRun(ctx); err != nil {
				log.WithError(err).Error("gRPC connection failed")
			}

			if ctx.Err() != nil {
				return cm.Close()
			}

			var delay time.Duration
			if d, ok := cm.client.PendingShutdownDelay(); ok {
				delay = d
				cm.reconnects = 0
				log.WithField("delay", delay).Info("Server requested reconnect delay")
			} else {
				delay = cm.calculateBackoff()
			}

			log.WithField("delay", delay).Info("Waiting before reconnecting to panel...")

			select {
			case <-ctx.Done():
				return cm.Close()
			case <-time.After(delay):
				log.Info("Reconnecting to panel...")
				continue
			}
		}
	}
}

func (cm *ConnectionManager) connectAndRun(ctx context.Context) error {
	var dialOpt grpc.DialOption

	if cm.cfg.IsInsecure() || cm.cfg.GRPC.Insecure {
		log.Warn("gRPC connection is running without TLS. It is recommended to enable TLS for security")
		dialOpt = grpc.WithTransportCredentials(insecure.NewCredentials())
	} else {
		creds, err := NewTLSCredentials(cm.cfg)
		if err != nil {
			return errors.Wrap(err, "failed to create TLS credentials")
		}
		dialOpt = grpc.WithTransportCredentials(creds)
	}

	conn, err := grpc.NewClient(
		cm.cfg.GRPCAddress(),
		dialOpt,
	)
	if err != nil {
		cm.reconnects++
		return errors.Wrap(err, "failed to connect to panel")
	}

	cm.mu.Lock()
	cm.conn = conn
	cm.mu.Unlock()

	log.Infof("Connected to panel via gRPC at %s", cm.cfg.GRPCAddress())

	for _, fn := range cm.onConnect {
		fn(conn)
	}

	err = cm.client.Run(ctx, conn)
	if err != nil {
		cm.reconnects++
		return err
	}

	cm.reconnects = 0

	return nil
}

func (cm *ConnectionManager) calculateBackoff() time.Duration {
	baseDelay := cm.cfg.GRPC.InitialReconnectDelay
	maxDelay := cm.cfg.GRPC.MaxReconnectDelay

	backoff := float64(baseDelay) * math.Pow(2, float64(cm.reconnects))
	if backoff > float64(maxDelay) {
		backoff = float64(maxDelay)
	}

	jitter := backoff * 0.2 * (0.5 - rand.Float64()) //nolint:gosec
	return time.Duration(backoff + jitter)
}

func (cm *ConnectionManager) Connection() *grpc.ClientConn {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.conn
}

func (cm *ConnectionManager) IsConnected() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.conn == nil {
		return false
	}

	return cm.conn.GetState() == connectivity.Ready
}

func (cm *ConnectionManager) Close() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.conn != nil {
		return cm.conn.Close()
	}
	return nil
}
