package codefresh

import (
	"bytes"
	"context"
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

type codefreshClient struct {
	cfConfig   *CodefreshConfig
	httpClient *http.Client
}

type CodefreshClient interface {
	Send(ctx context.Context, event *events.Event) error
}

func NewCodefreshClient(cfConfig *CodefreshConfig) CodefreshClient {
	return &codefreshClient{
		cfConfig: cfConfig,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (cc *codefreshClient) Send(ctx context.Context, event *events.Event) error {
	return WithRetry(&DefaultBackoff, func() error {
		url := cc.cfConfig.BaseURL + "/2.0/api/events/"
		log.Infof("Sending event from %s", *event.Name)
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(event.Payload))
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

		log.Infof("Event from %s successfully sent", *event.Name)
		return nil
	})
}
