package utils

import (
	"encoding/json"

	"github.com/argoproj/argo-cd/v2/pkg/sources_server_client"
	"github.com/argoproj/argo-cd/v2/reposerver/apiclient"
	log "github.com/sirupsen/logrus"
)

func SourcesAppVersionsToRepo(logCtx *log.Logger, applicationVersions *sources_server_client.AppVersionResult) *apiclient.ApplicationVersions {
	if applicationVersions == nil {
		return nil
	}

	applicationVersionsRepo := &apiclient.ApplicationVersions{}
	applicationVersionsData, _ := json.Marshal(applicationVersions)
	err := json.Unmarshal(applicationVersionsData, applicationVersionsRepo)
	if err != nil {
		logCtx.Errorf("can't unmarshal app version: %v", err)
		return nil
	}

	return applicationVersionsRepo
}
