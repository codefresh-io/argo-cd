package service

import (
	"context"
	"encoding/json"
	argoclient "github.com/argoproj/argo-cd/v2/acr_controller/application"
	appclient "github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	application "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	appclientset "github.com/argoproj/argo-cd/v2/pkg/client/clientset/versioned"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sync"
)

type ACRService interface {
	ChangeRevision(ctx context.Context, application *application.Application) error
}

type acrService struct {
	applicationClientset     appclientset.Interface
	applicationServiceClient argoclient.ApplicationClient
	lock                     sync.Mutex
}

func NewACRService(applicationClientset appclientset.Interface, applicationServiceClient argoclient.ApplicationClient) ACRService {
	return &acrService{
		applicationClientset:     applicationClientset,
		applicationServiceClient: applicationServiceClient,
	}
}

func getChangeRevision(app *application.Application) string {
	if app.Operation.Sync.ChangeRevision != "" {
		return app.Operation.Sync.ChangeRevision
	}
	if app.Status.OperationState != nil && app.Status.OperationState.Operation.Sync != nil {
		return app.Status.OperationState.Operation.Sync.ChangeRevision
	}
	return ""
}

func (c *acrService) ChangeRevision(ctx context.Context, a *application.Application) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	app, err := c.applicationClientset.ArgoprojV1alpha1().Applications(a.Namespace).Get(ctx, a.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if app.Operation == nil || app.Operation.Sync == nil {
		return nil
	}

	if getChangeRevision(app) != "" {
		log.Infof("Change revision already calculated for application %s", app.Name)
		return nil
	}

	log.Infof("Calculate revision for application %s", app.Name)

	revision, err := c.calculateRevision(ctx, app)
	if err != nil {
		return err
	}

	if revision == nil || *revision == "" {
		log.Infof("Revision for application %s is empty", app.Name)
		return nil
	}

	log.Infof("Change revision for application %s is %s", app.Name, *revision)

	app, err = c.applicationClientset.ArgoprojV1alpha1().Applications(app.Namespace).Get(ctx, app.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if app.Status.OperationState != nil && app.Status.OperationState.Operation.Sync != nil {
		log.Infof("Patch operation sync result for application %s", app.Name)
		return c.patchOperationSyncResultWithChangeRevision(ctx, app, *revision)
	}

	log.Infof("Patch operation for application %s", app.Name)
	return c.patchOperationWithChangeRevision(ctx, app, *revision)
}

func (c *acrService) calculateRevision(ctx context.Context, a *application.Application) (*string, error) {
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

func (c *acrService) patchOperationWithChangeRevision(ctx context.Context, a *application.Application, revision string) error {
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

func (c *acrService) patchOperationSyncResultWithChangeRevision(ctx context.Context, a *application.Application, revision string) error {
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

func (c *acrService) getRevisions(ctx context.Context, a *application.Application) (string, string) {
	if a.Status.History == nil || len(a.Status.History) == 0 {
		// it is first sync operation, and we dont need detect change revision in such case
		return "", ""
	}

	// in case if sync is already done, we need to use revision from sync result and previous revision from history
	if a.Status.Sync.Status == "Synced" {
		currentRevision := a.Status.OperationState.SyncResult.Revision
		return currentRevision, a.Status.History[len(a.Status.History)-2].Revision
	}

	// in case if sync is in progress, we need to use revision from operation and revision from latest history record
	currentRevision := a.Operation.Sync.Revision
	previousRevision := a.Status.History[len(a.Status.History)-1].Revision
	return currentRevision, previousRevision
}
