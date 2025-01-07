package codefresh

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type (
	CodefreshConfig struct {
		BaseURL        string
		AuthToken      string
		TlsInsecure    bool
		CaCertPath     string
		RuntimeVersion string
	}

	CodefreshClientInterface interface {
		SendApplicationEvent(ctx context.Context, payload *ApplicationPayload) error
		SendResourceEvent(ctx context.Context, payload *ResourcePayload) error
		SendGraphQL(query GraphQLQuery) (*json.RawMessage, error)
	}

	codefreshClient struct {
		baseURL    string
		httpClient *http.Client
	}

	// GraphQLQuery structure to form a GraphQL query
	GraphQLQuery struct {
		Query     string                 `json:"query"`
		Variables map[string]interface{} `json:"variables"`
	}
)

func NewCodefreshClient(cfConfig *CodefreshConfig) CodefreshClientInterface {
	return &codefreshClient{
		baseURL:    cfConfig.BaseURL,
		httpClient: cfConfig.getHttpClient(),
	}
}

func (c *codefreshClient) SendApplicationEvent(ctx context.Context, payload *ApplicationPayload) error {
	err := c.sendEvent(ctx, "/2.0/api/applications", payload)
	if err != nil {
		return fmt.Errorf("failed to send application event: %w", err)
	}

	return nil
}

func (c *codefreshClient) SendResourceEvent(ctx context.Context, payload *ResourcePayload) error {
	err := c.sendEvent(ctx, "/2.0/api/resources", payload)
	if err != nil {
		return fmt.Errorf("failed to send resource event: %w", err)
	}

	return nil
}

// sendGraphQLRequest function to send the GraphQL request and handle the response
func (c *codefreshClient) SendGraphQL(query GraphQLQuery) (*json.RawMessage, error) {
	queryJSON, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}

	url, err := url.JoinPath(c.baseURL, "/2.0/api/graphql")
	if err != nil {
		return nil, fmt.Errorf("failed to join URL: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(queryJSON))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, errors.New(resp.Status)
	}

	defer resp.Body.Close()

	var responseStruct struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&responseStruct); err != nil {
		return nil, err
	}

	return &responseStruct.Data, nil
}

func (c *codefreshClient) sendEvent(ctx context.Context, path string, payload any) error {
	return WithRetry(&DefaultBackoff, func() error {
		url, err := url.JoinPath(c.baseURL, path)
		if err != nil {
			return fmt.Errorf("failed to join URL: %w", err)
		}

		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal event: %w", err)
		}

		// Create a buffer to hold the compressed data
		var buf bytes.Buffer

		// Create a gzip writer
		gz := gzip.NewWriter(&buf)
		defer gz.Close()

		// Write the data to the gzip writer
		if _, err := gz.Write(data); err != nil {
			return fmt.Errorf("failed to write payload to gzip writer: %w", err)
		}

		if err := gz.Close(); err != nil {
			return fmt.Errorf("failed to close gzip writer: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, "POST", url, io.NopCloser(&buf))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Encoding", "gzip")

		res, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed reporting to Codefresh, payload: %v, error: %w", payload, err)
		}
		defer res.Body.Close()

		isStatusOK := res.StatusCode >= 200 && res.StatusCode < 300
		if !isStatusOK {
			b, _ := io.ReadAll(res.Body)
			return errors.Errorf("failed reporting to Codefresh, got response: status code %d and body %s, payload: %v",
				res.StatusCode, string(b), payload)
		}

		return nil
	})
}

func (cfConfig *CodefreshConfig) getHttpClient() *http.Client {
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: cfConfig.getTlsConfig(),
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			req.Header.Set("Authorization", cfConfig.AuthToken)
			return nil
		},
	}

	return httpClient
}

func (cfConfig *CodefreshConfig) getTlsConfig() *tls.Config {
	c := &tls.Config{}

	if cfConfig.TlsInsecure {
		return &tls.Config{
			InsecureSkipVerify: true,
			ClientAuth:         0,
		}
	}

	if cfConfig.CaCertPath != "" {
		cert, err := os.ReadFile(cfConfig.CaCertPath)
		if err != nil {
			log.Fatal(err)
		}

		pool := x509.NewCertPool()
		if ok := pool.AppendCertsFromPEM(cert); !ok {
			log.Fatalf("unable to parse codefresh cert from path %s", cfConfig.CaCertPath)
		}

		c.RootCAs = pool
	}

	return c
}
