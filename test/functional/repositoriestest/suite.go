package repositoriestest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/di"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus/hooks/test"
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

	Cfg       *config.Config
	Container *di.Container

	apiResponses map[string]apiResponses
	apiServer    *http.Server
	wg           *sync.WaitGroup

	apiPutCalled  map[string]apiRequests
	apiPostCalled map[string]apiRequests
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
	suite.apiPostCalled = map[string]apiRequests{}

	suite.apiResponses = map[string]apiResponses{}

	suite.wg = &sync.WaitGroup{}

	suite.Cfg = &config.Config{
		APIHost: "http://localhost:14323",
		APIKey:  "0oKyfcfjZOycicaazEgW6sHw9cYUMJDVJl0pXKjMYu44eoBWBwvXUJZdv6z6OfKs",

		LogLevel: "trace",
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

	log, _ := test.NewNullLogger()
	container, err := di.NewContainer(
		suite.Cfg,
		log,
	)
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.Container = container
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
		Methods(http.MethodGet, http.MethodPost, http.MethodPut).
		HandlerFunc(suite.apiTestServerHandler)

	http.Handle("/", router)

	suite.wg.Add(1)
	go func() {
		defer suite.wg.Done()

		if err := suite.apiServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			panic(err)
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
	} else if request.Method == http.MethodPost {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			suite.T().Fatal(err)
		}

		suite.appendPostCall(request.RequestURI, body)
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

func (suite *Suite) appendPostCall(uri string, body []byte) {
	_, exist := suite.apiPostCalled[uri]

	if exist {
		suite.apiPostCalled[uri] = append(suite.apiPostCalled[uri], body)
	} else {
		suite.apiPostCalled[uri] = [][]byte{body}
	}
}

func (suite *Suite) AssertAPIPutCalled(url string, body []byte) {
	suite.T().Helper()

	suite.AssertAPICalled(http.MethodPut, url, body)
}

func (suite *Suite) AssertAPIPostCalled(url string, body []byte) {
	suite.T().Helper()

	suite.AssertAPICalled(http.MethodPost, url, body)
}

func (suite *Suite) AssertAPICalled(method string, url string, body []byte) {
	suite.T().Helper()

	var urlCalled apiRequests
	var isCalled bool

	switch method {
	case http.MethodPost:
		urlCalled, isCalled = suite.apiPostCalled[url]
	case http.MethodPut:
		urlCalled, isCalled = suite.apiPutCalled[url]
	default:
		suite.T().Fatal("Unsupported http method to assert")
	}

	if !isCalled {
		suite.T().Errorf("api call not found (%s)", url)
		return
	}

	equalFound := false
	for _, v := range urlCalled {
		if suite.JSONEq(string(v), string(body)) {
			equalFound = true
		}
	}

	if !equalFound {
		suite.T().Errorf("api call not found (%s)\n"+
			"found: \n%s",
			url, urlCalled,
		)
	}
}
