package sources_server_client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
)

type DependenciesMap struct {
	Lock         string `json:"helm/Chart.lock"`
	Deps         string `json:"helm/dependencies"`
	Requirements string `json:"helm/requirements.yaml"`
}

type AppVersionResult struct {
	AppVersion   string          `json:"appVersion"`
	Dependencies DependenciesMap `json:"dependencies"`
}

type SourcesServerConfig struct {
	BaseURL string
}

type sourceServerClient struct {
	clientConfig *SourcesServerConfig
}

type SourceServerClientInteface interface {
	GetAppVersion(app *v1alpha1.Application) *AppVersionResult
}

func (c *sourceServerClient) sendRequest(method, url string, payload interface{}) ([]byte, error) {
	var requestBody []byte
	var err error
	if payload != nil {
		requestBody, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("error marshalling payload: %w", err)
		}
	}

	req, err := http.NewRequest(method, fmt.Sprintf("%s%s", c.clientConfig.BaseURL, url), bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server responded with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	return body, nil
}

func (c *sourceServerClient) GetAppVersion(app *v1alpha1.Application) *AppVersionResult {
	appVersionResult, err := c.sendRequest("POST", "/getAppVersion", app)
	if err != nil {
		log.Errorf("error getting app version: %v", err)
		return nil
	}

	var versionStruct AppVersionResult
	err = json.Unmarshal(appVersionResult, &versionStruct)
	if err != nil {
		log.Errorf("error unmarshaling app version: %v", err)
		return nil
	}

	return &versionStruct
}

func NewSourceServerClient(clientConfig *SourcesServerConfig) SourceServerClientInteface {
	return &sourceServerClient{
		clientConfig: clientConfig,
	}
}
