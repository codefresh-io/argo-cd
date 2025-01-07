package reporter

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/argoproj/argo-cd/v2/event_reporter/utils"
	appv1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/argoproj/argo-cd/v2/pkg/codefresh"
	"github.com/argoproj/argo-cd/v2/reposerver/apiclient"

	"github.com/argoproj/gitops-engine/pkg/health"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getResourceEventPayload(
	eventProcessingStartedAt time.Time,
	rr *ReportedResource,
	parentApp *ReportedEntityParentApp,
	argoTrackingMetadata *ArgoTrackingMetadata,
) (*codefresh.ResourcePayload, error) {
	var (
		syncStarted       = metav1.Now()
		syncFinished      *metav1.Time
		revisionsMetadata *utils.AppSyncRevisionsMetadata
	)

	if rr.rsAsAppInfo != nil {
		revisionsMetadata = rr.rsAsAppInfo.revisionsMetadata
	}

	if (rr.rsAsAppInfo != nil && rr.rsAsAppInfo.app != nil && rr.rsAsAppInfo.app.DeletionTimestamp != nil) || parentApp.app.ObjectMeta.DeletionTimestamp != nil {
		// resource should be deleted in case if application in process of deletion
		rr.actualState.Manifest = nil
		rr.desiredManifest = ""
	}

	if parentApp.app.Status.OperationState != nil {
		syncStarted = parentApp.app.Status.OperationState.StartedAt
		syncFinished = parentApp.app.Status.OperationState.FinishedAt
	}

	// for primitive resources that are synced right away and don't require progression time (like configmap)
	if rr.rs.Status == appv1.SyncStatusCodeSynced && rr.rs.Health != nil && rr.rs.Health.Status == health.HealthStatusHealthy {
		syncFinished = &syncStarted
	}

	repoURL, targetRevision := getResourceSourceRepoData(rr, parentApp)
	source := &codefresh.Source{
		RepoURL:                repoURL,
		TargetRevision:         targetRevision,
		Revisions:              utils.GetApplicationLatestRevisions(parentApp.app),
		OperationSyncRevisions: utils.GetOperationRevisions(parentApp.app),
		ClusterServer:          parentApp.app.Spec.Destination.Server,
		AppName:                parentApp.app.Name,
		AppNamespace:           parentApp.app.Namespace,
		AppUID:                 parentApp.app.ObjectMeta.UID,
		AppLabels:              parentApp.app.Labels,
		SyncStatus:             rr.rs.Status,
		SyncStartedAt:          syncStarted,
		SyncFinishedAt:         syncFinished,
		HistoryId:              utils.GetLatestAppHistoryId(parentApp.app),
		AppInstanceLabelKey:    *argoTrackingMetadata.AppInstanceLabelKey,
		TrackingMethod:         *argoTrackingMetadata.TrackingMethod,
		AppMultiSourced:        parentApp.app.Spec.HasMultipleSources(),
		AppSourceIdx:           rr.appSourceIdx,
	}

	if parentApp.revisionsMetadata != nil && parentApp.revisionsMetadata.SyncRevisions != nil && rr.appSourceIdx != -1 {
		syncRevisionWithMetadata := parentApp.revisionsMetadata.GetSyncRevisionAt(int(rr.appSourceIdx))

		if syncRevisionWithMetadata != nil && syncRevisionWithMetadata.Metadata != nil {
			source.CommitMessage = syncRevisionWithMetadata.Metadata.Message
			source.CommitAuthor = syncRevisionWithMetadata.Metadata.Author
			source.CommitDate = &syncRevisionWithMetadata.Metadata.Date
		}
	}

	if parentApp.validatedDestination != nil {
		source.ClusterName = &parentApp.validatedDestination.Name
	}

	if rr.rs.Health != nil {
		source.HealthStatus = rr.rs.Health
	}

	payload := &codefresh.ResourcePayload{
		Timestamp:         metav1.NewTime(eventProcessingStartedAt),
		ActualManifest:    rr.actualState.GetManifest(),
		DesiredManifest:   rr.desiredManifest,
		Source:            source,
		AppVersions:       rr.rsAsAppInfo.applicationVersions,
		RevisionsMetadata: revisionsMetadata,
		Errors:            getResourceEventPayloadErrors(rr, parentApp),
	}

	return payload, nil
}

func getResourceSourceRepoData(rr *ReportedResource, parentApp *ReportedEntityParentApp) (repoURL string, targetRevision string) {
	repoURL = ""
	targetRevision = ""
	specCopy := parentApp.app.Spec.DeepCopy()

	specCopy.Sources = parentApp.app.Status.Sync.ComparedTo.Sources
	specCopy.Source = parentApp.app.Status.Sync.ComparedTo.Source.DeepCopy()

	if specCopy.HasMultipleSources() {
		if !rr.appSourceIdxDetected() {
			return
		}

		source := specCopy.GetSourcePtrByIndex(int(rr.appSourceIdx))
		if source != nil {
			repoURL = source.RepoURL
			targetRevision = source.TargetRevision
		}
	} else {
		repoURL = specCopy.Source.RepoURL
		targetRevision = specCopy.Source.TargetRevision
	}

	return
}

func getResourceEventPayloadErrors(rr *ReportedResource, parentApp *ReportedEntityParentApp) []*codefresh.EventError {
	var errors []*codefresh.EventError

	if parentApp.app.Status.OperationState != nil {
		errors = append(errors, parseResourceSyncResultErrors(rr.rs, parentApp.app.Status.OperationState)...)
	}

	// parent application not include errors in application originally was created with broken state, for example in destination missed namespace
	if rr.rsAsAppInfo != nil && rr.rsAsAppInfo.app != nil {
		if rr.rsAsAppInfo.app.Status.OperationState != nil {
			errors = append(errors, parseApplicationSyncResultErrors(rr.rsAsAppInfo.app.Status.OperationState)...)
		}

		if rr.rsAsAppInfo.app.Status.Conditions != nil {
			errors = append(errors, parseApplicationSyncResultErrorsFromConditions(rr.rsAsAppInfo.app.Status)...)
		}

		errors = append(errors, parseAggregativeHealthErrorsOfApplication(rr.rsAsAppInfo.app, parentApp.appTree)...)
	}

	if rr.rs.Health != nil && rr.rs.Health.Status != health.HealthStatusHealthy {
		errors = append(errors, parseAggregativeHealthErrors(rr.rs, parentApp.appTree, false)...)
	}

	return errors
}

func (s *applicationEventReporter) getApplicationEventPayload(
	ctx context.Context,
	eventProcessingStartedAt time.Time,
	app *appv1.Application,
	appTree *appv1.ApplicationTree,
	applicationVersions *apiclient.ApplicationVersions,
) (*codefresh.ApplicationPayload, error) {
	logCtx := log.WithField("application", app.Name)

	revisionsMetadata, err := s.getApplicationRevisionsMetadata(ctx, app)
	if err != nil {
		if !strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("failed to get revision metadata: %w", err)
		}

		logCtx.Warnf("failed to get revision metadata: %s, reporting application deletion event", err.Error())
	}

	var actualManifest string
	if app.DeletionTimestamp != nil {
		// mark as deleted
		actualManifest = ""
		logCtx.Info("reporting application deletion event")
	} else {
		data, err := app.Marshal()
		if err != nil {
			return nil, fmt.Errorf("failed to marshal application event: %w", err)
		}

		actualManifest = string(data)
	}

	errors := []*codefresh.EventError{}
	errors = append(errors, parseApplicationSyncResultErrorsFromConditions(app.Status)...)
	errors = append(errors, parseAggregativeHealthErrorsOfApplication(app, appTree)...)

	payload := &codefresh.ApplicationPayload{
		Timestamp:         metav1.NewTime(eventProcessingStartedAt),
		ActualManifest:    actualManifest,
		RevisionsMetadata: revisionsMetadata,
		AppVersions:       applicationVersions,
		Errors:            errors,
	}

	return payload, nil
}
