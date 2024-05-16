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
	"sync"
)

type ChangeRevisionService interface {
	ChangeRevision(ctx context.Context, application *application.Application) error
}

type changeRevisionService struct {
	applicationClientset     appclientset.Interface
	applicationServiceClient argoclient.ApplicationClient
	lock                     sync.Mutex
}

func NewChangeRevisionService(applicationClientset appclientset.Interface, applicationServiceClient argoclient.ApplicationClient) ChangeRevisionService {
	return &changeRevisionService{
		applicationClientset:     applicationClientset,
		applicationServiceClient: applicationServiceClient,
	}
}

func (c *changeRevisionService) ChangeRevision(ctx context.Context, a *application.Application) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	app, err := c.applicationClientset.ArgoprojV1alpha1().Applications(a.Namespace).Get(ctx, a.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if app.Operation == nil || app.Operation.Sync == nil {
		return nil
	}

	if app.Operation.Sync.ChangeRevision != "" {
		log.Info("Change revision already calculated")
		return nil
	}

	log.Infof("Calculate revision for application %s", app.Name)

	revision, err := c.calculateRevision(ctx, app)
	if err != nil {
		return err
	}

	if revision == nil || *revision == "" {
		return nil
	}

	log.Infof("Change revision for application %s is %s", app.Name, *revision)

	app, err = c.applicationClientset.ArgoprojV1alpha1().Applications(app.Namespace).Get(ctx, app.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if app.Status.OperationState != nil && app.Status.OperationState.Operation.Sync != nil {
		log.Info("Patch operation sync result")
		return c.patchOperationSyncResultWithChangeRevision(ctx, app, *revision)
	}

	log.Info("Patch operation")
	return c.patchOperationWithChangeRevision(ctx, app, *revision)
}

func (c *changeRevisionService) calculateRevision(ctx context.Context, a *application.Application) (*string, error) {
	currentRevision, previousRevision := c.getRevisions(ctx, a)
	changeRevisionResult, err := c.applicationServiceClient.GetChangeRevision(ctx, &appclient.ChangeRevisionRequest{
		AppName:          pointer.String(a.GetName()),
		Namespace:        pointer.String(a.GetNamespace()),
		CurrentRevision:  pointer.String(currentRevision),
		PreviousRevision: pointer.String(previousRevision),
	})
	if err != nil {
		return nil, err
	}
	return changeRevisionResult.Revision, nil
}

func (c *changeRevisionService) patchOperationWithChangeRevision(ctx context.Context, a *application.Application, revision string) error {
	patch, _ := json.Marshal(map[string]interface{}{
		"operation": map[string]interface{}{
			"sync": map[string]interface{}{
				"changeRevision": revision,
			},
		},
	})
	_, err := c.applicationClientset.ArgoprojV1alpha1().Applications(a.Namespace).Patch(ctx, a.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

func (c *changeRevisionService) patchOperationSyncResultWithChangeRevision(ctx context.Context, a *application.Application, revision string) error {
	patch, _ := json.Marshal(map[string]interface{}{
		"status": map[string]interface{}{
			"operationState": map[string]interface{}{
				"operation": map[string]interface{}{
					"sync": map[string]interface{}{
						"changeRevision": revision,
					},
				},
			},
		},
	})
	_, err := c.applicationClientset.ArgoprojV1alpha1().Applications(a.Namespace).Patch(ctx, a.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

//func (c *changeRevisionService) patchStatusWithChangeRevision(ctx context.Context, a *application.Application, revision string) error {
//	patch, _ := json.Marshal(map[string]interface{}{
//		"status": map[string]interface{}{
//			"operationState": map[string]interface{}{
//				"syncResult": map[string]interface{}{
//					"changeRevision": revision,
//				},
//			},
//		},
//	})
//	_, err := c.applicationClientset.ArgoprojV1alpha1().Applications(a.Namespace).Patch(ctx, a.Name, types.MergePatchType, patch, metav1.PatchOptions{})
//	return err
//}

func (c *changeRevisionService) getRevisions(ctx context.Context, a *application.Application) (string, string) {
	// first sync operation happens
	// TODO: implement scenraio when there is no history
	if a.Status.History == nil || len(a.Status.History) == 0 {
		return "", ""
	}
	currentRevision := a.Operation.Sync.Revision
	if a.Status.Sync.Status == "Synced" {
		return currentRevision, a.Status.History[len(a.Status.History)-2].Revision
	}
	previousRevision := a.Status.History[len(a.Status.History)-1].Revision
	return currentRevision, previousRevision
}
