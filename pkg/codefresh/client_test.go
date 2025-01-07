package codefresh

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type MockRoundTripper struct {
	mock.Mock
}

func (r *MockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	args := r.Called(req)

	if args.Error(1) != nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*http.Response), args.Error(1)
}

func TestCodefreshClient_SendEvent(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		payload  *ApplicationPayload
		wantErr  string
		beforeFn func(t *testing.T, rt *MockRoundTripper)
	}{
		{
			name:    "should return nil when all is good",
			baseURL: "https://some.host",
			payload: &ApplicationPayload{
				Timestamp: metav1.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			beforeFn: func(t *testing.T, rt *MockRoundTripper) {
				rt.On("RoundTrip", mock.Anything).Run(func(args mock.Arguments) {
					req := args.Get(0).(*http.Request)
					assert.Equal(t, "POST", req.Method, "invalid request method")
					assert.Equal(t, "https://some.host/2.0/api/applications", req.URL.String(), "invalid request URL")
					assert.Equal(t, "application/json", req.Header.Get("Content-Type"), "missing or invalid Content-Type header")
					reader, err := gzip.NewReader(req.Body)
					require.NoError(t, err, "failed to create gzip reader")
					defer reader.Close()
					body, err := io.ReadAll(reader)
					require.NoError(t, err, "failed to read request body")
					assert.JSONEq(t, `{"timestamp": "2021-01-01T00:00:00Z", "actualManifest": ""}`, string(body), "invalid request body")
				}).Return(&http.Response{
					StatusCode: 200,
				}, nil)
			},
		},
		{
			name:    "should create correct url when baseUrl ends with '/'",
			baseURL: "https://some.host/",
			payload: &ApplicationPayload{},
			beforeFn: func(t *testing.T, rt *MockRoundTripper) {
				rt.On("RoundTrip", mock.Anything).Run(func(args mock.Arguments) {
					req := args.Get(0).(*http.Request)
					assert.Equal(t, "https://some.host/2.0/api/applications", req.URL.String(), "invalid request URL")
				}).Return(&http.Response{
					StatusCode: 200,
				}, nil)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRT := &MockRoundTripper{}
			c := &codefreshClient{
				baseURL: tt.baseURL,
				httpClient: &http.Client{
					Transport: mockRT,
				},
			}
			tt.beforeFn(t, mockRT)
			if err := c.SendApplicationEvent(context.Background(), tt.payload); err != nil || tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
			}
		})
	}
}
