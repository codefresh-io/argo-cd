package reporter

import (
	"context"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	appv1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type AppSyncRevisionsMetadata struct {
	revisions     []*appv1.RevisionMetadata
	syncRevisions []*appv1.RevisionMetadata
}

func getLatestAppHistoryId(a *appv1.Application) int64 {
	var id int64
	lastHistory := getLatestAppHistoryItem(a)

	if lastHistory != nil {
		id = lastHistory.ID
	}

	return id
}

func getLatestAppHistoryItem(a *appv1.Application) *appv1.RevisionHistory {
	if a.Status.History != nil && len(a.Status.History) > 0 {
		return &a.Status.History[len(a.Status.History)-1]
	}

	return nil
}

func getApplicationLatestRevision(a *appv1.Application) string {
	revision := a.Status.Sync.Revision
	lastHistory := getLatestAppHistoryItem(a)

	if lastHistory != nil {
		revision = lastHistory.Revision
	}

	return revision
}

func getOperationRevision(a *appv1.Application) string {
	var revision string
	if a != nil {
		// this value will be used in case if application hasnt resources , like gitsource
		revision = a.Status.Sync.Revision
		if a.Status.OperationState != nil && a.Status.OperationState.Operation.Sync != nil && a.Status.OperationState.Operation.Sync.Revision != "" {
			revision = a.Status.OperationState.Operation.Sync.Revision
		} else if a.Operation != nil && a.Operation.Sync != nil && a.Operation.Sync.Revision != "" {
			revision = a.Operation.Sync.Revision
		}
	}

	return revision
}

func (s *applicationEventReporter) getApplicationRevisionDetails(ctx context.Context, a *appv1.Application, revision string) (*appv1.RevisionMetadata, error) {
	project := a.Spec.GetProject()
	return s.applicationServiceClient.RevisionMetadata(ctx, &application.RevisionMetadataQuery{
		Name:         &a.Name,
		AppNamespace: &a.Namespace,
		Revision:     &revision,
		Project:      &project,
	})
}

func addCommitDetailsToLabels(u *unstructured.Unstructured, revisionMetadata *appv1.RevisionMetadata) *unstructured.Unstructured {
	if revisionMetadata == nil || u == nil {
		return u
	}

	if field, _, _ := unstructured.NestedFieldCopy(u.Object, "metadata", "labels"); field == nil {
		_ = unstructured.SetNestedStringMap(u.Object, map[string]string{}, "metadata", "labels")
	}

	_ = unstructured.SetNestedField(u.Object, revisionMetadata.Date.Format("2006-01-02T15:04:05.000Z"), "metadata", "labels", "app.meta.commit-date")
	_ = unstructured.SetNestedField(u.Object, revisionMetadata.Author, "metadata", "labels", "app.meta.commit-author")
	_ = unstructured.SetNestedField(u.Object, revisionMetadata.Message, "metadata", "labels", "app.meta.commit-message")

	return u
}
