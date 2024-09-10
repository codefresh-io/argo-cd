package reporter

import (
	"context"
	"github.com/argoproj/argo-cd/v2/event_reporter/utils"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	appv1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	log "github.com/sirupsen/logrus"
)

func (s *applicationEventReporter) getApplicationRevisionDetails(ctx context.Context, a *appv1.Application, revision string) (*appv1.RevisionMetadata, error) {
	project := a.Spec.GetProject()
	return s.applicationServiceClient.RevisionMetadata(ctx, &application.RevisionMetadataQuery{
		Name:         &a.Name,
		AppNamespace: &a.Namespace,
		Revision:     &revision,
		Project:      &project,
	})
}

func (s *applicationEventReporter) getCommitRevisionsDetails(ctx context.Context, a *appv1.Application, revisions []string) ([]*appv1.RevisionMetadata, error) {
	project := a.Spec.GetProject()
	rms := make([]*appv1.RevisionMetadata, 0)

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
		rms = append(rms, rm)
	}

	return rms, nil
}

func (s *applicationEventReporter) getApplicationRevisionsMetadata(ctx context.Context, logCtx *log.Entry, a *appv1.Application) (*utils.AppSyncRevisionsMetadata, error) {
	result := &utils.AppSyncRevisionsMetadata{}

	// can be the latest revision of repository
	operationSyncRevisionsMetadata, err := s.getCommitRevisionsDetails(ctx, a, utils.GetOperationSyncRevisions(a))

	if err != nil {
		logCtx.WithError(err).Warnf("failed to get application(%s) revisions metadata, resuming", a.GetName())
	}

	if operationSyncRevisionsMetadata != nil {
		result.SyncRevisions = operationSyncRevisionsMetadata
	}
	// latest revision of repository where changes to app resource were actually made; empty if no changeRevisionі present
	operationChangeRevisionsMetadata, err := s.getCommitRevisionsDetails(ctx, a, utils.GetOperationChangeRevisions(a))

	if err == nil && operationChangeRevisionsMetadata != nil {
		result.ChangeRevisions = operationChangeRevisionsMetadata
	}

	return result, nil
}
