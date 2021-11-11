package servertest

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/et-nik/binngo"
	"github.com/et-nik/binngo/decode"
	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/server/response"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

const (
	ServerCert = "../../../../config/certs/server.crt"
	ServerKey  = "../../../../config/certs/server.key"
	ClientCert = "../../../../config/certs/client.crt"
	ClientKey  = "../../../../config/certs/client.key"
)

type Suite struct {
	suite.Suite
	Server *server.Server
	Client *tls.Conn
}

func (suite *Suite) SetupSuite() {
	var err error
	suite.Server, err = server.NewServer(
		"127.0.0.1",
		31717,
		ServerCert,
		ServerKey,
		server.CredentialsConfig{
			PasswordAuthentication: true,
			Login:                  "login",
			Password:               "password",
		},
	)
	if err != nil {
		suite.T().Fatal(err)
	}

	log.SetLevel(log.ErrorLevel)

	go func() {
		err := suite.Server.Run(context.Background())
		if err != nil {
			suite.T().Fatal(err)
		}
	}()

	time.Sleep(10 * time.Millisecond)
}

func (suite *Suite) SetupTest() {
	suite.loadClient()
}

func (suite *Suite) TearDownTest() {
	suite.closeClient()
}

func (suite *Suite) loadClient() {
	cer, err := tls.LoadX509KeyPair(ClientCert, ClientKey)
	if err != nil {
		suite.T().Fatal(err)
	}

	conf := &tls.Config{
		Certificates:       []tls.Certificate{cer},
		InsecureSkipVerify: true,
	}

	conn, err := tls.Dial("tcp", "127.0.0.1:31717", conf)
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.Client = conn
}

func (suite *Suite) closeClient() {
	if suite.Client == nil {
		return
	}

	err := suite.Client.Close()
	if err != nil {
		suite.T().Fatal(err)
	}
}

func (suite *Suite) Auth(mode server.Mode) {
	msg, err := binngo.Marshal([]interface{}{0, "login", "password", mode})
	if err != nil {
		suite.T().Fatal(err)
	}
	suite.ClientWrite(msg)

	buf := make([]byte, 256)
	suite.ClientRead(buf)
	var status response.Response

	err = decode.Unmarshal(buf, &status)

	if !suite.Assert().NoError(err) {
		suite.T().Fatal(err)
	}

	if !assert.Equal(suite.T(), response.StatusOK, status.Code) {
		suite.T().Fatal("Must be status ok")
	}
}

func (suite *Suite) ClientWrite(b []byte) {
	err := suite.Client.SetWriteDeadline(time.Now().Add(30 * time.Second))
	if err != nil {
		suite.T().Fatal(err)
	}
	_, err = suite.Client.Write(b)
	if err != nil {
		suite.T().Fatal(err)
	}
}

func (suite *Suite) ClientRead(b []byte) {
	suite.Client.ConnectionState()
	err := suite.Client.SetReadDeadline(time.Now().Add(30 * time.Second))
	if err != nil {
		suite.T().Fatal(err)
	}

	_, err = suite.Client.Read(b)
	if err != nil {
		suite.T().Fatal(err)
	}
}

func (suite *Suite) ClientWriteReadAndDecodeList(msg interface{}) []interface{} {
	bytes, err := binngo.Marshal(msg)
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.ClientWrite(bytes)

	buf := make([]byte, 256)
	suite.ClientRead(buf)

	var r []interface{}
	err = decode.Unmarshal(buf, &r)
	if err != nil {
		suite.T().Fatal(err)
	}

	return r
}
