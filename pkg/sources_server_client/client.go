package sources_server_client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"

	sourcesServerCommon "github.com/codefresh-io/octopus-argo/sources-server/common"
)

type SourcesServerConfig struct {
	BaseURL string
}

type sourceServerClient struct {
	clientConfig *SourcesServerConfig
}

type SourceServerClientInteface interface {
	GetAppVersion(app *v1alpha1.Application) *sourcesServerCommon.AppVersionResult
}

func (c *sourceServerClient) sendRequest(method, url string, payload interface{}) ([]byte, error) {
	var requestBody []byte
	var err error
	if payload != nil {
		requestBody, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("error marshalling payload: %v", err)
		}
	}

	req, err := http.NewRequest(method, fmt.Sprintf("%s%s", c.clientConfig.BaseURL, url), bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server responded with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	return body, nil
}

func (c *sourceServerClient) GetAppVersion(app *v1alpha1.Application) *sourcesServerCommon.AppVersionResult {
	appVersionResult, err := c.sendRequest("POST", "/getAppVersion", app)
	if err != nil {
		log.Errorf("error getting app version: %v", err)
		return nil
	}

	var versionStruct sourcesServerCommon.AppVersionResult
	err = json.Unmarshal(appVersionResult, &versionStruct)
	if err != nil {
		log.Errorf("error unmarshaling app version: %v", err)
		return nil
	}

	// TODO: remove this marker line
	versionStruct.AppVersion = versionStruct.AppVersion + "*"
	return &versionStruct
}

func NewSourceServerClient(clientConfig *SourcesServerConfig) SourceServerClientInteface {
	return &sourceServerClient{
		clientConfig: clientConfig,
	}
}
