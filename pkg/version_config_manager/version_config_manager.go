package version_config_manager

import (
	"errors"
)

type VersionConfig struct {
	ProductLabel string `json:"productLabel"`
	JsonPath     string `json:"jsonPath"`
	ResourceName string `json:"resourceName"`
}

type ConfigProvider interface {
	GetConfig() (*VersionConfig, error)
}

type CodereshAPIConfigProvider struct {
	CodereshAPIEndpoint string
}

type ConfigMapProvider struct {
	ConfigMapPath string
}

func (CodereshAPI *CodereshAPIConfigProvider) GetConfig() (*VersionConfig, error) {
	// Implement logic to fetch config from the CodereshAPI here.
	// For this example, we'll just return a mock config.
	return &VersionConfig{
		ProductLabel: "ProductLabelName=ProductName",
		JsonPath:     "{.appVersion}",
		ResourceName: "Chart.yaml",
	}, nil
}

func (cm *ConfigMapProvider) GetConfig() (*VersionConfig, error) {
	// Implement logic to fetch config from the config map here.
	// For this example, we'll just return a mock config.
	return &VersionConfig{
		ProductLabel: "ProductLabelName=ProductName",
		JsonPath:     "{.appVersion}",
		ResourceName: "Chart.yaml",
	}, nil
}

type VersionConfigManager struct {
	provider ConfigProvider
}

func NewVersionConfigManager(providerType string, source string) (*VersionConfigManager, error) {
	var provider ConfigProvider
	switch providerType {
	case "CodereshAPI":
		provider = &CodereshAPIConfigProvider{CodereshAPIEndpoint: source}
	case "ConfigMap":
		provider = &ConfigMapProvider{ConfigMapPath: source}
	default:
		return nil, errors.New("Invalid provider type")
	}
	return &VersionConfigManager{provider: provider}, nil
}

func (v *VersionConfigManager) ObtainConfig() (*VersionConfig, error) {
	return v.provider.GetConfig()
}
