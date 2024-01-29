package version_config_manager

import (
	"github.com/argoproj/argo-cd/v2/pkg/codefresh"
	log "github.com/sirupsen/logrus"
)

type VersionConfig struct {
	JsonPath     string `json:"jsonPath"`
	ResourceName string `json:"resourceName"`
}

func (v *VersionConfigManager) GetVersionConfig(app *codefresh.ApplicationIdentity) (*VersionConfig, error) {
	appConfig, err := v.client.GetApplicationConfiguration(app)
	if err != nil {
		log.Printf("Failed to get application config from API: %v", err)
		return nil, err
	}

	if appConfig != nil {
		return &VersionConfig{
			JsonPath:     appConfig.VersionSource.JsonPath,
			ResourceName: appConfig.VersionSource.File,
		}, nil
	}

	// Default value
	return &VersionConfig{
		JsonPath:     "{.appVersion}",
		ResourceName: "Chart.yaml",
	}, nil
}

type VersionConfigManager struct {
	client codefresh.CodefreshClient
}

func NewVersionConfigManager(client *codefresh.CodefreshClient) *VersionConfigManager {
	return &VersionConfigManager{
		client: *client,
	}
}
