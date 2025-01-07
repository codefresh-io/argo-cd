package reporter

import (
	"testing"
	"time"

	"github.com/argoproj/argo-cd/v2/event_reporter/utils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-cd/v2/common"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	"github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/argoproj/argo-cd/v2/util/argo"
)

func getMockedArgoTrackingMetadata() *ArgoTrackingMetadata {
	appInstanceLabelKey := common.LabelKeyAppInstance
	trackingMethod := argo.TrackingMethodLabel

	return &ArgoTrackingMetadata{
		AppInstanceLabelKey: &appInstanceLabelKey,
		TrackingMethod:      &trackingMethod,
	}
}

func TestGetResourceEventPayload(t *testing.T) {
	t.Run("Deleting timestamp is empty", func(t *testing.T) {
		app := v1alpha1.Application{
			Spec: v1alpha1.ApplicationSpec{
				Source: &v1alpha1.ApplicationSource{
					RepoURL: "test",
				},
			},
		}
		rs := v1alpha1.ResourceStatus{}
		man := "{ \"key\" : \"manifest\" }"
		actualState := application.ApplicationResourceResponse{
			Manifest: &man,
		}
		desiredState := "{ \"key\" : \"manifest\" }"
		appTree := v1alpha1.ApplicationTree{}
		revisionMetadata := utils.AppSyncRevisionsMetadata{
			SyncRevisions: []*utils.RevisionWithMetadata{{
				Metadata: &v1alpha1.RevisionMetadata{
					Author:  "demo usert",
					Date:    metav1.Time{},
					Message: "some message",
				},
			}},
		}

		payload, err := getResourceEventPayload(
			time.Now(),
			&ReportedResource{
				rs:              &rs,
				actualState:     &actualState,
				desiredManifest: desiredState,
				manifestGenErr:  true,
				rsAsAppInfo:     nil,
			},
			&ReportedEntityParentApp{
				app:               &app,
				appTree:           &appTree,
				revisionsMetadata: &revisionMetadata,
			},
			getMockedArgoTrackingMetadata(),
		)
		require.NoError(t, err)

		assert.Equal(t, "{ \"key\" : \"manifest\" }", payload.DesiredManifest)
		assert.Equal(t, "{ \"key\" : \"manifest\" }", payload.ActualManifest)
	})

	t.Run("Deleting timestamp not empty", func(t *testing.T) {
		app := v1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				DeletionTimestamp: &metav1.Time{},
			},
			Status: v1alpha1.ApplicationStatus{},
		}
		rs := v1alpha1.ResourceStatus{}
		man := "{ \"key\" : \"manifest\" }"
		actualState := application.ApplicationResourceResponse{
			Manifest: &man,
		}
		desiredState := "{ \"key\" : \"manifest\" }"
		appTree := v1alpha1.ApplicationTree{}
		revisionMetadata := utils.AppSyncRevisionsMetadata{
			SyncRevisions: []*utils.RevisionWithMetadata{},
		}

		payload, err := getResourceEventPayload(
			time.Now(),
			&ReportedResource{
				rs:              &rs,
				actualState:     &actualState,
				desiredManifest: desiredState,
				manifestGenErr:  true,
				rsAsAppInfo:     nil,
			},
			&ReportedEntityParentApp{
				app:               &app,
				appTree:           &appTree,
				revisionsMetadata: &revisionMetadata,
			},
			getMockedArgoTrackingMetadata(),
		)
		require.NoError(t, err)

		assert.Equal(t, "", payload.DesiredManifest)
		assert.Equal(t, "", payload.ActualManifest)
	})
}

func TestGetResourceEventPayloadWithoutRevision(t *testing.T) {
	app := v1alpha1.Application{}
	rs := v1alpha1.ResourceStatus{}
	mf := "{ \"key\" : \"manifest\" }"
	actualState := application.ApplicationResourceResponse{
		Manifest: &mf,
	}
	desiredState := "{ \"key\" : \"manifest\" }"
	appTree := v1alpha1.ApplicationTree{}

	_, err := getResourceEventPayload(
		time.Now(),
		&ReportedResource{
			rs:              &rs,
			actualState:     &actualState,
			desiredManifest: desiredState,
			manifestGenErr:  true,
			rsAsAppInfo:     nil,
		},
		&ReportedEntityParentApp{
			app:     &app,
			appTree: &appTree,
		},
		getMockedArgoTrackingMetadata(),
	)
	assert.NoError(t, err)
}
