package repositoriestest

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/repositories"
	"github.com/gorilla/mux"
	"github.com/sarulabs/di"
	"github.com/stretchr/testify/suite"
)

type Suite struct {
	suite.Suite

	Cfg              *config.Config
	ServerRepository *repositories.ServerRepository

	container di.Container
	responses map[string][]byte
	apiServer *http.Server
	wg        *sync.WaitGroup
}

func (suite *Suite) API(path string, response []byte) {
	suite.responses[path] = response
}

func (suite *Suite) SetupSuite() {
	suite.wg = &sync.WaitGroup{}

	suite.Cfg = &config.Config{
		APIHost: "http://localhost:14323",
		APIKey:  "0oKyfcfjZOycicaazEgW6sHw9cYUMJDVJl0pXKjMYu44eoBWBwvXUJZdv6z6OfKs",
	}

	getTokenJson, err := json.Marshal(struct {
		Token     string `json:"token"`
		TimeStamp int64    `json:"timestamp"`
	}{
		"dYCw9ADVnS03leY9dLlckgaxiG59uKF3KMCcpmXpJUKYmlQXuAhvHtCYbL6hG3Ce",
		time.Now().Unix(),
	})
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.setupAPIServer()
	suite.responses = map[string][]byte{
		"/gdaemon_api/get_token": getTokenJson,
	}

	builder, err := app.NewBuilder(suite.Cfg)
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.container = builder.Build()
	suite.ServerRepository = suite.container.Get("serverRepository").(*repositories.ServerRepository)
}

func (suite *Suite) TearDownSuite() {
	err := suite.apiServer.Shutdown(context.Background())
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.wg.Wait()
}

func (suite *Suite) SetupTest() {
	suite.responses = map[string][]byte{}
}

func (suite *Suite) setupAPIServer() {
	suite.apiServer = &http.Server{Addr: ":14323"}

	router := mux.NewRouter()
	router.PathPrefix("/").HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		response, exist := suite.responses[request.RequestURI]
		if !exist {
			writer.WriteHeader(http.StatusNotFound)
			return
		}

		writer.WriteHeader(http.StatusOK)
		writer.Write(response)
	}).Methods("GET")

	http.Handle("/", router)

	suite.wg.Add(1)
	go func() {
		defer suite.wg.Done()

		if err := suite.apiServer.ListenAndServe(); err != http.ErrServerClosed {
			suite.T().Fatal(err)
		}
	}()
}
