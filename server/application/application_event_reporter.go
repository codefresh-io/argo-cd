package application

import (
	"fmt"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/events"
	appv1reg "github.com/argoproj/argo-cd/v2/pkg/apis/application"
	appv1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/argoproj/argo-cd/v2/reposerver/apiclient"
	"github.com/argoproj/gitops-engine/pkg/health"
	"github.com/argoproj/gitops-engine/pkg/utils/kube"
	"github.com/argoproj/gitops-engine/pkg/utils/text"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"strings"
)

type applicationEventReporter struct {
}

func NewApplicationEventReporter() *applicationEventReporter {
	return &applicationEventReporter{}
}

func (s *Server) shouldSendResourceEvent(a *appv1.Application, rs appv1.ResourceStatus) bool {
	logCtx := log.WithFields(log.Fields{
		"app":      a.Name,
		"gvk":      fmt.Sprintf("%s/%s/%s", rs.Group, rs.Version, rs.Kind),
		"resource": fmt.Sprintf("%s/%s", rs.Namespace, rs.Name),
	})

	cachedRes, err := s.cache.GetLastResourceEvent(a, rs)
	if err != nil {
		logCtx.Debug("resource not in cache")
		return true
	}

	if reflect.DeepEqual(&cachedRes, &rs) {
		logCtx.Debug("resource status not changed")

		// status not changed
		return false
	}

	logCtx.Info("resource status changed")
	return true
}

func (s *Server) streamApplicationEvents(
	ctx context.Context,
	a *appv1.Application,
	es *events.EventSource,
	stream events.Eventing_StartEventSourceServer,
	ts string,
	processRootApp bool,
) error {
	var (
		logCtx         = log.WithField("application", a.Name)
		manifestGenErr bool
	)

	logCtx.Info("streaming application events")

	isChildApp := false
	if a.Labels != nil {
		isChildApp = a.Labels["app.kubernetes.io/instance"] != ""
	}

	if !isChildApp || processRootApp {
		// application events for child apps would be sent by its parent app
		// as resource event
		appEvent, err := s.getApplicationEventPayload(ctx, a, es, ts)
		if err != nil {
			return fmt.Errorf("failed to get application event: %w", err)
		}

		if appEvent == nil {
			// event did not have an OperationState - skip all events
			return nil
		}

		logWithAppStatus(a, logCtx, ts).Info("sending application event")
		if err := stream.Send(appEvent); err != nil {
			return fmt.Errorf("failed to send event for resource %s/%s: %w", a.Namespace, a.Name, err)
		}
	}

	// get the desired state manifests of the application
	desiredManifests, err := s.GetManifests(ctx, &application.ApplicationManifestQuery{
		Name:     &a.Name,
		Revision: a.Status.Sync.Revision,
	})
	if err != nil {
		if !strings.Contains(err.Error(), "Manifest generation error") {
			return fmt.Errorf("failed to get application desired state manifests: %w", err)
		}
		// if it's manifest generation error we need to still report the actual state
		// of the resources, but since we can't get the desired state, we will report
		// each resource with empty desired state
		logCtx.WithError(err).Warn("failed to get application desired state manifests, reporting actual state only")
		desiredManifests = &apiclient.ManifestResponse{Manifests: []*apiclient.Manifest{}}
		manifestGenErr = true // will ignore requiresPruning=true to not delete resources with actual state
	}

	appTree, err := s.getAppResources(ctx, a)
	if err != nil {
		return fmt.Errorf("failed to get application resources tree: %w", err)
	}

	// for each resource in the application get desired and actual state,
	// then stream the event
	for _, rs := range a.Status.Resources {
		logCtx = logCtx.WithFields(log.Fields{
			"gvk":      fmt.Sprintf("%s/%s/%s", rs.Group, rs.Version, rs.Kind),
			"resource": fmt.Sprintf("%s/%s", rs.Namespace, rs.Name),
		})

		if !s.shouldSendResourceEvent(a, rs) {
			continue
		}

		// get resource desired state
		desiredState := getResourceDesiredState(&rs, desiredManifests, logCtx)

		// get resource actual state
		actualState, err := s.GetResource(ctx, &application.ApplicationResourceRequest{
			Name:         &a.Name,
			Namespace:    rs.Namespace,
			ResourceName: rs.Name,
			Version:      rs.Version,
			Group:        rs.Group,
			Kind:         rs.Kind,
		})
		if err != nil {
			if !strings.Contains(err.Error(), "not found") {
				logCtx.WithError(err).Error("failed to get actual state")
				continue
			}

			// empty actual state
			actualState = &application.ApplicationResourceResponse{Manifest: ""}
		}

		var mr *apiclient.ManifestResponse = desiredManifests
		if isApp(rs) {
			app := &v1alpha1.Application{}
			if err := json.Unmarshal([]byte(actualState.Manifest), app); err != nil {
				logWithAppStatus(a, logCtx, ts).WithError(err).Error("failed to unmarshal child application resource")
			}
			resourceDesiredManifests, err := s.GetManifests(ctx, &application.ApplicationManifestQuery{
				Name:     &rs.Name,
				Revision: app.Status.Sync.Revision,
			})
			if err != nil {
				logWithAppStatus(a, logCtx, ts).WithError(err).Error("failed to get resource desired manifest")
			} else {
				mr = resourceDesiredManifests
			}
		}

		ev, err := getResourceEventPayload(a, &rs, es, actualState, desiredState, mr, appTree, manifestGenErr, ts)
		if err != nil {
			logCtx.WithError(err).Error("failed to get event payload")
			continue
		}

		appRes := appv1.Application{}
		if isApp(rs) && actualState.Manifest != "" && json.Unmarshal([]byte(actualState.Manifest), &appRes) == nil {
			logWithAppStatus(&appRes, logCtx, ts).Info("streaming resource event")
		} else {
			logCtx.Info("streaming resource event")
		}

		if err := stream.Send(ev); err != nil {
			logCtx.WithError(err).Error("failed to send even")
			continue
		}

		if err := s.cache.SetLastResourceEvent(a, rs, resourceEventCacheExpiration); err != nil {
			logCtx.WithError(err).Error("failed to cache resource event")
			continue
		}
	}

	return nil
}

func isApp(rs appv1.ResourceStatus) bool {
	return rs.GroupVersionKind().String() == appv1.ApplicationSchemaGroupVersionKind.String()
}

func logWithAppStatus(a *appv1.Application, logCtx *log.Entry, ts string) *log.Entry {
	return logCtx.WithFields(log.Fields{
		"status":          a.Status.Sync.Status,
		"resourceVersion": a.ResourceVersion,
		"ts":              ts,
	})
}

func getResourceEventPayload(
	a *appv1.Application,
	rs *appv1.ResourceStatus,
	es *events.EventSource,
	actualState *application.ApplicationResourceResponse,
	desiredState *apiclient.Manifest,
	manifestsResponse *apiclient.ManifestResponse,
	apptree *appv1.ApplicationTree,
	manifestGenErr bool,
	ts string,
) (*events.Event, error) {
	var (
		err          error
		syncStarted  = metav1.Now()
		syncFinished *metav1.Time
		errors       = []*events.ObjectError{}
	)

	object := []byte(actualState.Manifest)
	if len(object) == 0 {
		if len(desiredState.CompiledManifest) == 0 {
			// no actual or desired state, don't send event
			u := &unstructured.Unstructured{}
			apiVersion := rs.Version
			if rs.Group != "" {
				apiVersion = rs.Group + "/" + rs.Version
			}

			u.SetAPIVersion(apiVersion)
			u.SetKind(rs.Kind)
			u.SetName(rs.Name)
			u.SetNamespace(rs.Namespace)
			object, err = u.MarshalJSON()
			if err != nil {
				return nil, fmt.Errorf("failed to marshal unstructured object: %w", err)
			}
		} else {
			// no actual state, use desired state as event object
			manifestWithNamespace, err := addDestNamespaceToManifest([]byte(desiredState.CompiledManifest), rs)
			if err != nil {
				return nil, fmt.Errorf("failed to add destination namespace to manifest: %w", err)
			}

			object = manifestWithNamespace
		}
	} else if rs.RequiresPruning && !manifestGenErr {
		// resource should be deleted
		desiredState.CompiledManifest = ""
		actualState.Manifest = ""
	}

	if a.Status.OperationState != nil {
		syncStarted = a.Status.OperationState.StartedAt
		syncFinished = a.Status.OperationState.FinishedAt
		errors = append(errors, parseResourceSyncResultErrors(rs, a.Status.OperationState)...)
	}

	source := events.ObjectSource{
		DesiredManifest: desiredState.CompiledManifest,
		ActualManifest:  actualState.Manifest,
		GitManifest:     desiredState.RawManifest,
		RepoURL:         a.Status.Sync.ComparedTo.Source.RepoURL,
		Path:            desiredState.Path,
		Revision:        a.Status.Sync.Revision,
		CommitMessage:   manifestsResponse.CommitMessage,
		CommitAuthor:    manifestsResponse.CommitAuthor,
		CommitDate:      manifestsResponse.CommitDate,
		AppName:         a.Name,
		AppLabels:       a.Labels,
		SyncStatus:      string(rs.Status),
		SyncStartedAt:   syncStarted,
		SyncFinishedAt:  syncFinished,
		Cluster:         a.Spec.Destination.Server,
	}

	if rs.Health != nil {
		source.HealthStatus = (*string)(&rs.Health.Status)
		source.HealthMessage = &rs.Health.Message
		if rs.Health.Status != health.HealthStatusHealthy {
			errors = append(errors, parseAggregativeHealthErrors(rs, apptree)...)
		}
	}

	payload := events.EventPayload{
		Timestamp: ts,
		Object:    object,
		Source:    &source,
		Errors:    errors,
	}

	payloadBytes, err := json.Marshal(&payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload for resource %s/%s: %w", rs.Namespace, rs.Name, err)
	}

	return &events.Event{Payload: payloadBytes, Name: es.Name}, nil
}

func parseAggregativeHealthErrors(rs *appv1.ResourceStatus, apptree *appv1.ApplicationTree) []*events.ObjectError {
	errs := make([]*events.ObjectError, 0)

	n := apptree.FindNode(rs.Group, rs.Kind, rs.Namespace, rs.Name)
	if n == nil {
		return errs
	}

	childNodes := n.GetAllChildNodes(apptree)

	for _, cn := range childNodes {
		if cn.Health != nil && cn.Health.Status == health.HealthStatusDegraded {
			errs = append(errs, &events.ObjectError{
				Type:     "health",
				Level:    "error",
				Message:  cn.Health.Message,
				LastSeen: *cn.CreatedAt,
			})
		}
	}

	return errs
}

func (s *Server) getApplicationEventPayload(ctx context.Context, a *appv1.Application, es *events.EventSource, ts string) (*events.Event, error) {
	var (
		syncStarted  = metav1.Now()
		syncFinished *metav1.Time
		logCtx       = log.WithField("application", a.Name)
	)

	obj := appv1.Application{}
	a.DeepCopyInto(&obj)

	// make sure there is type meta on object
	obj.TypeMeta = metav1.TypeMeta{
		Kind:       appv1reg.ApplicationKind,
		APIVersion: appv1.SchemeGroupVersion.String(),
	}

	if a.Status.OperationState != nil {
		syncStarted = a.Status.OperationState.StartedAt
		syncFinished = a.Status.OperationState.FinishedAt
	}

	if a.Status.Sync.Revision != "" {
		revisionMetadata, err := s.RevisionMetadata(ctx, &application.RevisionMetadataQuery{
			Name:     &a.Name,
			Revision: &a.Status.Sync.Revision,
		})
		if err != nil {
			if !strings.Contains(err.Error(), "not found") {
				return nil, fmt.Errorf("failed to get revision metadata: %w", err)
			}

			logCtx.Warnf("failed to get revision metadata: %s, reporting application deletion event", err.Error())
		} else {
			if obj.ObjectMeta.Labels == nil {
				obj.ObjectMeta.Labels = map[string]string{}
			}

			obj.ObjectMeta.Labels["app.meta.commit-date"] = revisionMetadata.Date.Format("2006-01-02T15:04:05.000Z")
			obj.ObjectMeta.Labels["app.meta.commit-author"] = revisionMetadata.Author
			obj.ObjectMeta.Labels["app.meta.commit-message"] = revisionMetadata.Message
		}
	}

	object, err := json.Marshal(&obj)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal application event")
	}

	actualManifest := string(object)
	if a.DeletionTimestamp != nil {
		actualManifest = "" // mark as deleted
		logCtx.Info("reporting application deletion event")
	}

	hs := string(a.Status.Health.Status)
	source := &events.ObjectSource{
		DesiredManifest: "",
		GitManifest:     "",
		ActualManifest:  actualManifest,
		RepoURL:         a.Spec.Source.RepoURL,
		CommitMessage:   "",
		CommitAuthor:    "",
		Path:            "",
		Revision:        "",
		AppName:         "",
		AppLabels:       map[string]string{},
		SyncStatus:      string(a.Status.Sync.Status),
		SyncStartedAt:   syncStarted,
		SyncFinishedAt:  syncFinished,
		HealthStatus:    &hs,
		HealthMessage:   &a.Status.Health.Message,
		Cluster:         a.Spec.Destination.Server,
	}

	errs := []*events.ObjectError{}
	for _, cnd := range a.Status.Conditions {
		if !strings.Contains(strings.ToLower(cnd.Type), "error") {
			continue
		}

		lastSeen := metav1.Now()
		if cnd.LastTransitionTime != nil {
			lastSeen = *cnd.LastTransitionTime
		}

		errs = append(errs, &events.ObjectError{
			Type:     "sync",
			Level:    "error",
			Message:  cnd.Message,
			LastSeen: lastSeen,
		})
	}

	payload := events.EventPayload{
		Timestamp: ts,
		Object:    object,
		Source:    source,
		Errors:    errs,
	}

	payloadBytes, err := json.Marshal(&payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload for resource %s/%s: %w", a.Namespace, a.Name, err)
	}

	return &events.Event{Payload: payloadBytes, Name: es.Name}, nil
}

func getResourceDesiredState(rs *appv1.ResourceStatus, ds *apiclient.ManifestResponse, logger *log.Entry) *apiclient.Manifest {
	for _, m := range ds.Manifests {
		u, err := appv1.UnmarshalToUnstructured(m.CompiledManifest)
		if err != nil {
			logger.WithError(err).Warnf("failed to unmarshal compiled manifest")
			continue
		}

		if u == nil {
			logger.WithError(err).Warnf("no compiled manifest for: %s", m.Path)
			continue
		}

		ns := text.FirstNonEmpty(u.GetNamespace(), rs.Namespace)

		if u.GroupVersionKind().String() == rs.GroupVersionKind().String() &&
			u.GetName() == rs.Name &&
			ns == rs.Namespace {
			if rs.Kind == kube.SecretKind && rs.Version == "v1" {
				m.RawManifest = m.CompiledManifest
			}

			return m
		}
	}

	// no desired state for resource
	// it's probably deleted from git
	return &apiclient.Manifest{}
}
