package reporter

import (
	"context"
	"github.com/argoproj/argo-cd/v2/event_reporter/application/mocks"
	"github.com/argoproj/argo-cd/v2/event_reporter/metrics"
	"github.com/argoproj/argo-cd/v2/event_reporter/utils"
	appclient "github.com/argoproj/argo-cd/v2/pkg/apiclient/application"

	"github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	servercache "github.com/argoproj/argo-cd/v2/server/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"testing"
)

func reporterWithMockedClient(t *testing.T, returnValue *v1alpha1.RevisionMetadata, returnError error) *applicationEventReporter {
	appServiceClient := mocks.NewApplicationClient(t)
	appServiceClient.On("RevisionMetadata", mock.Anything, mock.Anything, mock.Anything).Return(returnValue, returnError)

	return &applicationEventReporter{
		&servercache.Cache{},
		&MockCodefreshClient{},
		newAppLister(),
		appServiceClient,
		&metrics.MetricsServer{},
	}
}

func TestGetRevisionsDetails(t *testing.T) {

	t.Run("should return revisions for single source app", func(t *testing.T) {
		expectedRevision := "expected-revision"
		expectedResult := []*utils.RevisionWithMetadata{{
			Revision: expectedRevision,
			Metadata: &v1alpha1.RevisionMetadata{
				Author:  "Test Author",
				Message: "first commit",
			},
		}}

		reporter := reporterWithMockedClient(t, expectedResult[0].Metadata, nil)

		app := v1alpha1.Application{
			Spec: v1alpha1.ApplicationSpec{
				Source: &v1alpha1.ApplicationSource{
					RepoURL:        "https://my-site.com",
					TargetRevision: "HEAD",
					Path:           ".",
				},
			},
		}

		result, _ := reporter.getRevisionsDetails(context.Background(), &app, []string{expectedRevision})

		assert.Equal(t, expectedResult, result)
	})

	t.Run("should return revisions for multi sourced apps", func(t *testing.T) {
		expectedRevision1 := "expected-revision-1"
		expectedRevision2 := "expected-revision-2"
		expectedResult := []*utils.RevisionWithMetadata{{
			Revision: expectedRevision1,
			Metadata: &v1alpha1.RevisionMetadata{
				Author:  "Repo1 Author",
				Message: "first commit repo 1",
			},
		}, {
			Revision: expectedRevision2,
			Metadata: &v1alpha1.RevisionMetadata{
				Author:  "Repo2 Author",
				Message: "first commit repo 2",
			},
		}}

		app := v1alpha1.Application{
			Spec: v1alpha1.ApplicationSpec{
				Sources: []v1alpha1.ApplicationSource{{
					RepoURL:        "https://my-site.com/repo-1",
					TargetRevision: "branch1",
					Path:           ".",
				}, {
					RepoURL:        "https://my-site.com/repo-2",
					TargetRevision: "branch2",
					Path:           ".",
				}},
			},
		}

		project := app.Spec.GetProject()

		appServiceClient := mocks.NewApplicationClient(t)
		appServiceClient.On("RevisionMetadata", mock.Anything, &appclient.RevisionMetadataQuery{
			Name:         &app.Name,
			AppNamespace: &app.Namespace,
			Revision:     &expectedRevision1,
			Project:      &project,
		}).Return(expectedResult[0].Metadata, nil)
		appServiceClient.On("RevisionMetadata", mock.Anything, &appclient.RevisionMetadataQuery{
			Name:         &app.Name,
			AppNamespace: &app.Namespace,
			Revision:     &expectedRevision2,
			Project:      &project,
		}).Return(expectedResult[1].Metadata, nil)

		reporter := &applicationEventReporter{
			&servercache.Cache{},
			&MockCodefreshClient{},
			newAppLister(),
			appServiceClient,
			&metrics.MetricsServer{},
		}

		result, _ := reporter.getRevisionsDetails(context.Background(), &app, []string{expectedRevision1, expectedRevision2})

		assert.Equal(t, expectedResult, result)
	})

	t.Run("should return only revision because of helm single source app", func(t *testing.T) {
		expectedRevision := "expected-revision"
		expectedResult := []*utils.RevisionWithMetadata{{
			Revision: expectedRevision,
		}}

		reporter := reporterWithMockedClient(t, expectedResult[0].Metadata, nil)

		app := v1alpha1.Application{
			Spec: v1alpha1.ApplicationSpec{
				Source: &v1alpha1.ApplicationSource{
					RepoURL:        "https://my-site.com",
					TargetRevision: "HEAD",
					Path:           ".",
				},
			},
		}

		result, _ := reporter.getRevisionsDetails(context.Background(), &app, []string{expectedRevision})

		assert.Equal(t, expectedResult, result)
	})
}
