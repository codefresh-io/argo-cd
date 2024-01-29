package version_config_manager

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/argoproj/argo-cd/v2/pkg/codefresh"
)

// Config structure for configuration
type Config struct {
	BaseURL   string
	Path      string
	AuthToken string
}

// VersionSource structure for the versionSource field
type VersionSource struct {
	File     string `json:"file"`
	JsonPath string `json:"jsonPath"`
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

// CodefreshClient is a client for interacting with Codefresh API
type CodefreshClient struct {
	config Config
}

// NewCodefreshClient creates a new instance of CodefreshClient
func NewCodefreshClient(config Config) *CodefreshClient {
	return &CodefreshClient{config: config}
}

// GetApplicationConfiguration method to get application configuration
func (client *CodefreshClient) GetApplicationConfiguration(app codefresh.ApplicationIdentity) (*ApplicationConfiguration, error) {
	query := GraphQLQuery{
		Query: `
		query ($runtime: String!, $cluster: String!, $namespace: String!, $name: String!) {
		  applicationConfiguration(runtime: $runtime, cluster: $cluster, namespace: $namespace, name: $name) {
			versionSource {
			  file
			  jsonPath
			}
		  }
		}
		`,
		Variables: map[string]interface{}{
			"runtime":   app.Runtime,
			"cluster":   app.Cluster,
			"namespace": app.Namespace,
			"name":      app.Name,
		},
	}

	return sendGraphQLRequest(client.config, query)
}

// sendGraphQLRequest function to send the GraphQL request and handle the response
func sendGraphQLRequest(config Config, query GraphQLQuery) (*ApplicationConfiguration, error) {
	queryJSON, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", config.BaseURL+config.Path, bytes.NewBuffer(queryJSON))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+config.AuthToken)

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
		Data struct {
			ApplicationConfiguration ApplicationConfiguration `json:"applicationConfiguration"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &responseStruct); err != nil {
		return nil, err
	}

	return &responseStruct.Data.ApplicationConfiguration, nil
}
