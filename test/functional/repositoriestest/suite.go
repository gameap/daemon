package repositoriestest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"github.com/sarulabs/di"
	"github.com/stretchr/testify/suite"
)

type apiResponse struct {
	StatusCode int
	Body       []byte
}

type apiResponses []apiResponse

type apiRequests [][]byte

type Suite struct {
	suite.Suite

	Cfg          *config.Config
	Container    di.Container

	apiResponses map[string]apiResponses
	apiServer    *http.Server
	wg           *sync.WaitGroup

	apiPutCalled map[string]apiRequests
}

func (suite *Suite) GivenAPIResponse(path string, status int, body []byte) {
	r := apiResponse{status, body}

	if _, ok := suite.apiResponses[path]; !ok {
		suite.apiResponses[path] = apiResponses{r}
	} else {
		suite.apiResponses[path] = append(suite.apiResponses[path], r)
	}
}

func (suite *Suite) SetupSuite() {
	suite.apiPutCalled = map[string]apiRequests{}
	suite.apiResponses = map[string]apiResponses{}
	suite.wg = &sync.WaitGroup{}

	suite.Cfg = &config.Config{
		APIHost: "http://localhost:14323",
		APIKey:  "0oKyfcfjZOycicaazEgW6sHw9cYUMJDVJl0pXKjMYu44eoBWBwvXUJZdv6z6OfKs",
	}

	getTokenJSON, err := json.Marshal(struct {
		Token     string `json:"token"`
		TimeStamp int64  `json:"timestamp"`
	}{
		"dYCw9ADVnS03leY9dLlckgaxiG59uKF3KMCcpmXpJUKYmlQXuAhvHtCYbL6hG3Ce",
		time.Now().Unix(),
	})
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.setupAPIServer()
	suite.GivenAPIResponse("/gdaemon_api/get_token", http.StatusOK, getTokenJSON)

	builder, err := app.NewBuilder(suite.Cfg)
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.Container = builder.Build()
}

func (suite *Suite) TearDownSuite() {
	err := suite.apiServer.Shutdown(context.Background())
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.wg.Wait()
}

func (suite *Suite) SetupTest() {
	suite.apiResponses = map[string]apiResponses{}
}

func (suite *Suite) setupAPIServer() {
	suite.apiServer = &http.Server{Addr: ":14323"}

	router := mux.NewRouter()
	router.PathPrefix("/").
		HandlerFunc(suite.apiTestServerHandler).
		Methods("GET", "PUT")

	http.Handle("/", router)

	suite.wg.Add(1)
	go func() {
		defer suite.wg.Done()

		if err := suite.apiServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			suite.T().Fatal(err)
		}
	}()
}

func (suite *Suite) apiTestServerHandler(writer http.ResponseWriter, request *http.Request) {
	responses, exist := suite.apiResponses[request.RequestURI]
	if !exist || len(responses) == 0 {
		writer.WriteHeader(http.StatusNotFound)
		return
	}

	if request.Method == http.MethodPut {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			suite.T().Fatal(err)
		}

		suite.appendPutCall(request.RequestURI, body)
	}

	response := responses[0]
	suite.apiResponses[request.RequestURI] = suite.apiResponses[request.RequestURI][1:]

	writer.WriteHeader(response.StatusCode)
	_, _ = writer.Write(response.Body)
}

func (suite *Suite) appendPutCall(uri string, body []byte) {
	_, exist := suite.apiPutCalled[uri]

	if exist {
		suite.apiPutCalled[uri] = append(suite.apiPutCalled[uri], body)
	} else {
		suite.apiPutCalled[uri] = [][]byte{body}
	}
}

func (suite *Suite) AssertAPIPutCalled(url string, body []byte) {
	suite.T().Helper()

	urlCalled, isCalled := suite.apiPutCalled[url]
	if !isCalled {
		suite.T().Error(fmt.Sprintf("api call not found (%s)", url))
		return
	}

	equalFound := false
	for _, v := range urlCalled {
		if bytes.Equal(v, body) {
			equalFound = true
		}
	}

	if !equalFound {
		suite.T().Error(fmt.Sprintf("api call not found (%s)", url))
	}
}
