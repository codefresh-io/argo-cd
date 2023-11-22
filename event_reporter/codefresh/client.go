package codefresh

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/pkg/errors"
)

type config struct {
	baseURL   string
	authToken string
}

type codefreshClient struct {
	cfConfig   *config
	httpClient *http.Client
}

type CodefreshClient interface {
	Send(ctx context.Context, payload []byte) error
}

func NewCodefreshClient() CodefreshClient {
	return &codefreshClient{
		cfConfig: getCodefreshConfig(),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (cc *codefreshClient) Send(ctx context.Context, payload []byte) error {
	return WithRetry(&DefaultBackoff, func() error {
		url := cc.cfConfig.baseURL + "/2.0/api/events/"
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payload))
		if err != nil {
			return err
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", cc.cfConfig.authToken)

		res, err := cc.httpClient.Do(req)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("failed reporting to Codefresh, event: %s", string(payload)))
		}
		defer res.Body.Close()

		isStatusOK := res.StatusCode >= 200 && res.StatusCode < 300
		if !isStatusOK {
			b, _ := io.ReadAll(res.Body)
			return errors.Errorf("failed reporting to Codefresh, got response: status code %d and body %s, original request body: %s",
				res.StatusCode, string(b), string(payload))
		}

		return nil
	})
}

func getCodefreshConfig() *config {
	return &config{
		baseURL:   os.Getenv("CODEFRESH_URL"),
		authToken: os.Getenv("CODEFRESH_TOKEN"),
	}
}
