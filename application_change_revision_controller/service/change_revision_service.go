package service

import (
	"context"
	"encoding/json"
	argoclient "github.com/argoproj/argo-cd/v2/application_change_revision_controller/application"
	appclient "github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	application "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	appclientset "github.com/argoproj/argo-cd/v2/pkg/client/clientset/versioned"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
)

type ChangeRevisionService interface {
	ChangeRevision(ctx context.Context, application *application.Application) error
}

type changeRevisionService struct {
	applicationClientset     appclientset.Interface
	applicationServiceClient argoclient.ApplicationClient
}

func NewChangeRevisionService(applicationClientset appclientset.Interface, applicationServiceClient argoclient.ApplicationClient) ChangeRevisionService {
	return &changeRevisionService{
		applicationClientset,
		applicationServiceClient,
	}
}

func (c *changeRevisionService) ChangeRevision(ctx context.Context, a *application.Application) error {
	log.Infof("Calculate revision for application %s", a.Name)

	revision, err := c.calculateRevision(ctx, a)
	if err != nil {
		return err
	}

	patch, _ := json.Marshal(map[string]interface{}{
		"status": map[string]interface{}{
			"operationState": map[string]interface{}{
				"syncResult": map[string]interface{}{
					"changeRevision": *revision,
				},
			},
		},
	})
	_, err = c.applicationClientset.ArgoprojV1alpha1().Applications(a.Namespace).Patch(ctx, a.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

func (c *changeRevisionService) calculateRevision(ctx context.Context, a *application.Application) (*string, error) {
	changeRevisionResult, err := c.applicationServiceClient.GetChangeRevision(ctx, &appclient.ChangeRevisionRequest{
		Name: pointer.String("test"),
	})
	if err != nil {
		return nil, err
	}
	return changeRevisionResult.Revision, nil
}

func (c *changeRevisionService) patchStatusWithChangeRevision(ctx context.Context, a *application.Application, revision string) error {
	patch, _ := json.Marshal(map[string]interface{}{
		"status": map[string]interface{}{
			"operationState": map[string]interface{}{
				"syncResult": map[string]interface{}{
					"changeRevision": revision,
				},
			},
		},
	})
	_, err := c.applicationClientset.ArgoprojV1alpha1().Applications(a.Namespace).Patch(ctx, a.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}
