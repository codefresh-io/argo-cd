package codefresh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/argoproj/argo-cd/v2/pkg/apiclient/events"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type CodefreshConfig struct {
	BaseURL   string
	AuthToken string
}

type CodefreshClient struct {
	cfConfig   *CodefreshConfig
	httpClient *http.Client
}

type CodefreshClientInterface interface {
	Send(ctx context.Context, appName string, event *events.Event) error
	GetApplicationConfiguration(app ApplicationIdentity) (*ApplicationConfiguration, error)
}

// VersionSource structure for the versionSource field
type VersionSource struct {
	File     string `json:"file"`
	JsonPath string `json:"jsonPath"`
}

type ApplicationIdentity struct {
	Runtime   string
	Cluster   string
	Namespace string
	Name      string
}

// ApplicationConfiguration structure for GraphQL response
type ApplicationConfiguration struct {
	VersionSource VersionSource `json:"versionSource"`
}

// GraphQLQuery structure to form a GraphQL query
type GraphQLQuery struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

func NewCodefreshClient(cfConfig *CodefreshConfig) *CodefreshClient {
	return &CodefreshClient{
		cfConfig: cfConfig,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (cc *CodefreshClient) Send(ctx context.Context, appName string, event *events.Event) error {
	return WithRetry(&DefaultBackoff, func() error {
		url := cc.cfConfig.BaseURL + "/2.0/api/events"
		log.Infof("Sending application event for %s", appName)

		wrappedPayload := map[string]json.RawMessage{
			"data": event.Payload,
		}

		newPayloadBytes, err := json.Marshal(wrappedPayload)
		if err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(newPayloadBytes))
		if err != nil {
			return err
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", cc.cfConfig.AuthToken)

		res, err := cc.httpClient.Do(req)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("failed reporting to Codefresh, event: %s", string(event.Payload)))
		}
		defer res.Body.Close()

		isStatusOK := res.StatusCode >= 200 && res.StatusCode < 300
		if !isStatusOK {
			b, _ := io.ReadAll(res.Body)
			return errors.Errorf("failed reporting to Codefresh, got response: status code %d and body %s, original request body: %s",
				res.StatusCode, string(b), string(event.Payload))
		}

		log.Infof("Application event for %s successfully sent", appName)
		return nil
	})
}

// GetApplicationConfiguration method to get application configuration
func (client *CodefreshClient) GetApplicationConfiguration(app *ApplicationIdentity) (*ApplicationConfiguration, error) {
	query := GraphQLQuery{
		Query: `
		query ($cluster: String!, $namespace: String!, $name: String!) {
		  applicationConfigurationByRuntime(cluster: $cluster, namespace: $namespace, name: $name) {
			versionSource {
			  file
			  jsonPath
			}
		  }
		}
		`,
		Variables: map[string]interface{}{
			"cluster":   app.Cluster,
			"namespace": app.Namespace,
			"name":      app.Name,
		},
	}

	responseData, err := sendGraphQLRequest(*client.cfConfig, query)
	if err != nil {
		return nil, err
	}

	return responseData.(*ApplicationConfiguration), nil
}

// sendGraphQLRequest function to send the GraphQL request and handle the response
func sendGraphQLRequest(config CodefreshConfig, query GraphQLQuery) (interface{}, error) {
	queryJSON, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", config.BaseURL+"/2.0/api/graphql", bytes.NewBuffer(queryJSON))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", config.AuthToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var responseStruct struct {
		Data interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &responseStruct); err != nil {
		return nil, err
	}

	return &responseStruct.Data, nil
}
