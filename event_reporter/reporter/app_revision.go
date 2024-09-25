package reporter

import (
	"context"
	"github.com/argoproj/argo-cd/v2/event_reporter/utils"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	appv1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	log "github.com/sirupsen/logrus"
)

// treats multi-sourced apps as single source and gets first revision details
func getApplicationLegacyRevisionDetails(a *appv1.Application, revisionsMetadata *utils.AppSyncRevisionsMetadata) *appv1.RevisionMetadata {
	_, sourceIdx := a.Spec.GetNonRefSource()

	if sourceIdx == -1 { // single source app
		sourceIdx = 0
	}

	if revisionsMetadata.SyncRevisions == nil || len(revisionsMetadata.SyncRevisions) == 0 {
		return nil
	}

	return revisionsMetadata.SyncRevisions[sourceIdx].Metadata
}

func (s *applicationEventReporter) getCommitRevisionsDetails(ctx context.Context, a *appv1.Application, revisions []string) ([]*utils.RevisionWithMetadata, error) {
	project := a.Spec.GetProject()
	rms := make([]*utils.RevisionWithMetadata, 0)

	for _, revision := range revisions {
		rm, err := s.applicationServiceClient.RevisionMetadata(ctx, &application.RevisionMetadataQuery{
			Name:         &a.Name,
			AppNamespace: &a.Namespace,
			Revision:     &revision,
			Project:      &project,
		})
		if err != nil {
			return nil, err
		}
		rms = append(rms, &utils.RevisionWithMetadata{
			Revision: revision,
			Metadata: rm,
		})
	}

	return rms, nil
}

func (s *applicationEventReporter) getApplicationRevisionsMetadata(ctx context.Context, logCtx *log.Entry, a *appv1.Application) (*utils.AppSyncRevisionsMetadata, error) {
	result := &utils.AppSyncRevisionsMetadata{}

	if source, _ := a.Spec.GetNonRefSource(); !source.IsHelm() && (a.Status.Sync.Revision != "" || a.Status.Sync.Revisions != nil || (a.Status.History != nil && len(a.Status.History) > 0)) {
		// can be the latest revision of repository
		operationSyncRevisionsMetadata, err := s.getCommitRevisionsDetails(ctx, a, utils.GetOperationSyncRevisions(a))

		if err != nil {
			logCtx.WithError(err).Warnf("failed to get application(%s) sync revisions metadata, resuming", a.GetName())
		}

		if err == nil && operationSyncRevisionsMetadata != nil {
			result.SyncRevisions = operationSyncRevisionsMetadata
		}
		// latest revision of repository where changes to app resource were actually made; empty if no changeRevisionі present
		operationChangeRevisionsMetadata, err := s.getCommitRevisionsDetails(ctx, a, utils.GetOperationChangeRevisions(a))

		if err != nil {
			logCtx.WithError(err).Warnf("failed to get application(%s) change revisions metadata, resuming", a.GetName())
		}

		if err == nil && operationChangeRevisionsMetadata != nil {
			result.ChangeRevisions = operationChangeRevisionsMetadata
		}
	}

	return result, nil
}
