package version_config_manager

import (
	"github.com/argoproj/argo-cd/v2/pkg/codefresh"
	"github.com/argoproj/argo-cd/v2/reposerver/cache"
	log "github.com/sirupsen/logrus"
)

type VersionConfig struct {
	JsonPath     string `json:"jsonPath"`
	ResourceName string `json:"resourceName"`
}

func (v *VersionConfigManager) GetVersionConfig(app *codefresh.ApplicationIdentity) (*VersionConfig, error) {
	var appConfig *codefresh.ApplicationConfiguration

	// Get from cache
	appConfig, err := v.cache.GetCfAppConfig(app.Runtime, app.Cluster, app.Namespace, app.Name)
	if appConfig != nil {
		log.Infof("CfAppConfig cache hit: %s/%s/%s/%s", app.Runtime, app.Cluster, app.Namespace, app.Name)
		return &VersionConfig{
			JsonPath:     appConfig.VersionSource.JsonPath,
			ResourceName: appConfig.VersionSource.File,
		}, nil
	}

	if err != nil {
		log.Errorf("CfAppConfig cache get error for %s/%s/%s/%s: %v", app.Runtime, app.Cluster, app.Namespace, app.Name, err)
	}

	// Get from Codefresh API
	appConfig, err = v.client.GetApplicationConfiguration(app)
	if err != nil {
		log.Infof("Failed to get application config from API: %v", err)
		return nil, err
	}

	if appConfig != nil {
		// Set to cache
		err = v.cache.SetCfAppConfig(app.Runtime, app.Cluster, app.Namespace, app.Name, appConfig)
		if err == nil {
			log.Infof("CfAppConfig saved to cache hit: %s/%s/%s/%s", app.Runtime, app.Cluster, app.Namespace, app.Name)
		} else {
			log.Errorf("CfAppConfig cache set error for %s/%s/%s/%s: %v", app.Runtime, app.Cluster, app.Namespace, app.Name, err)
		}

		return &VersionConfig{
			JsonPath:     appConfig.VersionSource.JsonPath,
			ResourceName: appConfig.VersionSource.File,
		}, nil
	}

	// Default value
	log.Infof("Used default CfAppConfig for: %s/%s/%s/%s", app.Runtime, app.Cluster, app.Namespace, app.Name)
	return &VersionConfig{
		JsonPath:     "{.appVersion}",
		ResourceName: "Chart.yaml",
	}, nil
}

type VersionConfigManager struct {
	client codefresh.CodefreshClientInterface
	cache  *cache.Cache
}

func NewVersionConfigManager(client codefresh.CodefreshClientInterface, cache *cache.Cache) *VersionConfigManager {
	return &VersionConfigManager{
		client: client,
		cache:  cache,
	}
}
