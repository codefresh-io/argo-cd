package application_change_revision_controller

import (
	"context"
	appclient "github.com/argoproj/argo-cd/v2/application_change_revision_controller/application"
	"github.com/argoproj/argo-cd/v2/application_change_revision_controller/service"
	appclientset "github.com/argoproj/argo-cd/v2/pkg/client/clientset/versioned"
	"strings"
	"time"

	appv1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	applisters "github.com/argoproj/argo-cd/v2/pkg/client/listers/application/v1alpha1"
	servercache "github.com/argoproj/argo-cd/v2/server/cache"
	"github.com/argoproj/argo-cd/v2/util/settings"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

var (
	watchAPIBufferSize = 1000
)

type ApplicationChangeRevisionController interface {
	Run(ctx context.Context)
}

type applicationChangeRevisionController struct {
	settingsMgr              *settings.SettingsManager
	appBroadcaster           Broadcaster
	cache                    *servercache.Cache
	appLister                applisters.ApplicationLister
	applicationServiceClient appclient.ApplicationClient
	changeRevisionService    service.ChangeRevisionService
	applicationClientset     appclientset.Interface
}

func NewApplicationChangeRevisionController(appInformer cache.SharedIndexInformer, cache *servercache.Cache, settingsMgr *settings.SettingsManager, applicationServiceClient appclient.ApplicationClient, appLister applisters.ApplicationLister, applicationClientset appclientset.Interface) ApplicationChangeRevisionController {
	appBroadcaster := NewBroadcaster()
	_, err := appInformer.AddEventHandler(appBroadcaster)
	if err != nil {
		log.Error(err)
	}
	return &applicationChangeRevisionController{
		appBroadcaster:           appBroadcaster,
		cache:                    cache,
		settingsMgr:              settingsMgr,
		applicationServiceClient: applicationServiceClient,
		appLister:                appLister,
		applicationClientset:     applicationClientset,
		changeRevisionService:    service.NewChangeRevisionService(applicationClientset, applicationServiceClient),
	}
}

func (c *applicationChangeRevisionController) Run(ctx context.Context) {
	var (
		logCtx log.FieldLogger = log.StandardLogger()
	)

	// sendIfPermitted is a helper to send the application to the client's streaming channel if the
	// caller has RBAC privileges permissions to view it
	sendIfPermitted := func(ctx context.Context, a appv1.Application, eventType watch.EventType, ts string) error {
		if eventType == watch.Bookmark {
			return nil // ignore this event
		}

		val, ok := a.Annotations[appv1.AnnotationKeyManifestGeneratePaths]
		if !ok || val == "" {
			return nil
		}

		if a.Operation == nil || a.Operation.Sync == nil {
			return nil
		}

		c.changeRevisionService.ChangeRevision(ctx, &a)

		return nil
	}

	//TODO: move to abstraction
	eventsChannel := make(chan *appv1.ApplicationWatchEvent, watchAPIBufferSize)
	unsubscribe := c.appBroadcaster.Subscribe(eventsChannel)
	defer unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-eventsChannel:
			logCtx.Infof("channel size is %d", len(eventsChannel))

			ts := time.Now().Format("2006-01-02T15:04:05.000Z")
			ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			err := sendIfPermitted(ctx, event.Application, event.Type, ts)
			if err != nil {
				logCtx.WithError(err).Error("failed to stream application events")
				if strings.Contains(err.Error(), "context deadline exceeded") {
					logCtx.Info("Closing event-source connection")
					cancel()
				}
			}
			cancel()
		}
	}
}
