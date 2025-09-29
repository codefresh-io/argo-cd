package service

import (
	"testing"

	"github.com/sirupsen/logrus"
	test2 "github.com/sirupsen/logrus/hooks/test"

	"github.com/argoproj/argo-cd/v3/acr_controller/application/mocks"
	appclient "github.com/argoproj/argo-cd/v3/pkg/apiclient/application"
	appsv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	apps "github.com/argoproj/argo-cd/v3/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-cd/v3/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"
)

const fakeApp = `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: test-app
  namespace: default
spec:
  source:
    path: some/path
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    ksonnet:
      environment: default
  destination:
    namespace: ` + test.FakeDestNamespace + `
    server: https://cluster-api.example.com
`

const multisourceApp = `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  annotations:
    argocd.argoproj.io/manifest-generate-paths: .
  name: guestbook
  namespace: codefresh
spec:
  sources:
    - path: some/path
      repoURL: https://github.com/argoproj/argocd-example-apps.git
      targetRevision: HEAD
      ksonnet:
        environment: default
  destination:
    namespace: ` + test.FakeDestNamespace + `
    server: https://cluster-api.example.com
`

const fakeAppWithOperation = `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  annotations:
    argocd.argoproj.io/manifest-generate-paths: .
  finalizers:
  - resources-finalizer.argocd.argoproj.io
  labels:
    app.kubernetes.io/instance: guestbook
  name: guestbook
  namespace: codefresh
operation:
  initiatedBy:
    automated: true
  retry:
    limit: 5
  sync:
    prune: true
    revision: c732f4d2ef24c7eeb900e9211ff98f90bb646505
    syncOptions:
    - CreateNamespace=true
spec:
  destination:
    namespace: guestbook
    server: https://kubernetes.default.svc
  project: default
  source:
    path: apps/guestbook
    repoURL: https://github.com/pasha-codefresh/precisely-gitsource.git
    targetRevision: HEAD
`

const syncedAppWithSingleHistory = `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  annotations:
    argocd.argoproj.io/manifest-generate-paths: .
  finalizers:
  - resources-finalizer.argocd.argoproj.io
  labels:
    app.kubernetes.io/instance: guestbook
  name: guestbook
  namespace: codefresh
operation:
  initiatedBy:
    automated: true
  retry:
    limit: 5
  sync:
    prune: true
    revision: c732f4d2ef24c7eeb900e9211ff98f90bb646505
    syncOptions:
    - CreateNamespace=true
spec:
  destination:
    namespace: guestbook
    server: https://kubernetes.default.svc
  project: default
  source:
    path: apps/guestbook
    repoURL: https://github.com/pasha-codefresh/precisely-gitsource.git
    targetRevision: HEAD
status:
  history:
  - deployStartedAt: "2024-06-20T19:35:36Z"
    deployedAt: "2024-06-20T19:35:44Z"
    id: 3
    initiatedBy: {}
    revision: 792822850fd2f6db63597533e16dfa27e6757dc5
    source:
      path: apps/guestbook
      repoURL: https://github.com/pasha-codefresh/precisely-gitsource.git
      targetRevision: HEAD
  operationState:
    operation:
      sync:
        prune: true
        revision: c732f4d2ef24c7eeb900e9211ff98f90bb646506
        syncOptions:
        - CreateNamespace=true
    phase: Running
    startedAt: "2024-06-20T19:47:34Z"
    syncResult:
      revision: c732f4d2ef24c7eeb900e9211ff98f90bb646505
      source:
        path: apps/guestbook
        repoURL: https://github.com/pasha-codefresh/precisely-gitsource.git
        targetRevision: HEAD
  sync: 
    revision: 00d423763fbf56d2ea452de7b26a0ab20590f521
    status: Synced
`

const syncedAppWithHistory = `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  annotations:
    argocd.argoproj.io/manifest-generate-paths: .
  finalizers:
  - resources-finalizer.argocd.argoproj.io
  labels:
    app.kubernetes.io/instance: guestbook
  name: guestbook
  namespace: codefresh
operation:
  initiatedBy:
    automated: true
  retry:
    limit: 5
  sync:
    prune: true
    revision: c732f4d2ef24c7eeb900e9211ff98f90bb646505
    syncOptions:
    - CreateNamespace=true
spec:
  destination:
    namespace: guestbook
    server: https://kubernetes.default.svc
  project: default
  source:
    path: apps/guestbook
    repoURL: https://github.com/pasha-codefresh/precisely-gitsource.git
    targetRevision: HEAD
status:
  history:
  - deployStartedAt: "2024-06-20T19:35:36Z"
    deployedAt: "2024-06-20T19:35:44Z"
    id: 3
    initiatedBy: {}
    revision: 792822850fd2f6db63597533e16dfa27e6757dc5
    source:
      path: apps/guestbook
      repoURL: https://github.com/pasha-codefresh/precisely-gitsource.git
      targetRevision: HEAD
  - deployStartedAt: "2024-06-20T19:36:34Z"
    deployedAt: "2024-06-20T19:36:42Z"
    id: 4
    initiatedBy: {}
    revision: ee5373eb9814e247ec6944e8b8897a8ec2f8528e
    source:
      path: apps/guestbook
      repoURL: https://github.com/pasha-codefresh/precisely-gitsource.git
      targetRevision: HEAD
  operationState:
    operation:
      sync:
        prune: true
        revision: c732f4d2ef24c7eeb900e9211ff98f90bb646506
        syncOptions:
        - CreateNamespace=true
    phase: Running
    startedAt: "2024-06-20T19:47:34Z"
    syncResult:
      revision: c732f4d2ef24c7eeb900e9211ff98f90bb646505
      source:
        path: apps/guestbook
        repoURL: https://github.com/pasha-codefresh/precisely-gitsource.git
        targetRevision: HEAD
  sync: 
    revision: 00d423763fbf56d2ea452de7b26a0ab20590f521
    status: Synced
`

func newTestACRService(client *mocks.ApplicationClient) *acrService {
	fakeAppsClientset := apps.NewSimpleClientset(createTestApp(syncedAppWithHistory))
	return &acrService{
		applicationClientset:     fakeAppsClientset,
		applicationServiceClient: client,
		logger:                   logrus.New(),
	}
}

func createTestApp(testApp string, opts ...func(app *appsv1.Application)) *appsv1.Application {
	var app appsv1.Application
	err := yaml.Unmarshal([]byte(testApp), &app)
	if err != nil {
		panic(err)
	}
	for i := range opts {
		opts[i](&app)
	}
	return &app
}

func Test_getRevisions(r *testing.T) {
	r.Run("history list is empty", func(t *testing.T) {
		acrService := newTestACRService(&mocks.ApplicationClient{})
		current, previous := acrService.getRevisions(r.Context(), createTestApp(fakeApp))
		assert.Empty(t, current)
		assert.Empty(t, previous)
	})

	r.Run("history list is empty, but operation happens right now", func(t *testing.T) {
		acrService := newTestACRService(&mocks.ApplicationClient{})
		current, previous := acrService.getRevisions(r.Context(), createTestApp(fakeAppWithOperation))
		assert.Equal(t, "c732f4d2ef24c7eeb900e9211ff98f90bb646505", current)
		assert.Empty(t, previous)
	})

	r.Run("history list contains only one element, also sync result is here", func(t *testing.T) {
		acrService := newTestACRService(&mocks.ApplicationClient{})
		current, previous := acrService.getRevisions(r.Context(), createTestApp(syncedAppWithSingleHistory))
		assert.Equal(t, "c732f4d2ef24c7eeb900e9211ff98f90bb646505", current)
		assert.Empty(t, previous)
	})

	r.Run("application is synced", func(t *testing.T) {
		acrService := newTestACRService(&mocks.ApplicationClient{})
		app := createTestApp(syncedAppWithHistory)
		current, previous := acrService.getRevisions(r.Context(), app)
		assert.Equal(t, app.Status.OperationState.SyncResult.Revision, current)
		assert.Equal(t, app.Status.History[len(app.Status.History)-2].Revision, previous)
	})

	r.Run("application sync is in progress", func(t *testing.T) {
		acrService := newTestACRService(&mocks.ApplicationClient{})
		app := createTestApp(syncedAppWithHistory)
		app.Status.Sync.Status = "Syncing"
		current, previous := acrService.getRevisions(r.Context(), app)
		assert.Equal(t, app.Operation.Sync.Revision, current)
		assert.Equal(t, app.Status.History[len(app.Status.History)-1].Revision, previous)
	})
}

func Test_ChangeRevision(r *testing.T) {
	newChangeRevision := "new-revision"
	gitRevision := "c732f4d2ef24c7eeb900e9211ff98f90bb646505"
	useAnnotations := true
	appSrc := syncedAppWithHistory

	testNewChangeRevision := func(t *testing.T) (*acrService, *test2.Hook, *appsv1.Application) {
		t.Helper()
		client := &mocks.ApplicationClient{}
		client.On("GetChangeRevision", mock.Anything, mock.Anything).Return(&appclient.ChangeRevisionResponse{
			Revision: ptr.To(newChangeRevision),
		}, nil)
		logger, logHook := test2.NewNullLogger()
		acrService := newTestACRService(client)
		acrService.logger = logger
		app := createTestApp(appSrc)
		orgManifestPath := app.GetAnnotation(appsv1.AnnotationKeyManifestGeneratePaths)
		err := acrService.ChangeRevision(r.Context(), app, useAnnotations)
		require.NoError(t, err)
		assert.Equal(t, orgManifestPath, app.GetAnnotation(appsv1.AnnotationKeyManifestGeneratePaths))
		return acrService, logHook, app
	}

	testLastLogEntry := func(t *testing.T, hook *test2.Hook, expectedMsg string) {
		t.Helper()
		lastLogEntry := hook.LastEntry()
		if lastLogEntry == nil {
			t.Fatal("No log entry")
		}
		require.Equal(t, expectedMsg, lastLogEntry.Message)
	}

	r.Run("New change revision with annotations", func(t *testing.T) {
		acrService, _, app := testNewChangeRevision(t)
		app, err := acrService.applicationClientset.ArgoprojV1alpha1().Applications(app.Namespace).Get(r.Context(), app.Name, metav1.GetOptions{})
		require.NoError(t, err)
		assert.Len(t, app.GetAnnotations(), 5)
		assert.Equal(t, newChangeRevision, app.Status.OperationState.Operation.Sync.ChangeRevision)
		assert.Equal(t, newChangeRevision, app.GetAnnotation(CHANGE_REVISION_ANN))
		assert.Equal(t, "[\""+"new-revision"+"\"]", app.GetAnnotation(CHANGE_REVISIONS_ANN))
		assert.Equal(t, gitRevision, app.GetAnnotation(GIT_REVISION_ANN))
		assert.Equal(t, "[\""+gitRevision+"\"]", app.GetAnnotation(GIT_REVISIONS_ANN))
	})

	r.Run("Change revision already exists with annotations", func(t *testing.T) {
		acrService, logHook, app := testNewChangeRevision(t)
		app, err := acrService.applicationClientset.ArgoprojV1alpha1().Applications(app.Namespace).Get(r.Context(), app.Name, metav1.GetOptions{})
		require.NoError(t, err)
		err = acrService.ChangeRevision(r.Context(), app, useAnnotations)
		require.NoError(t, err)
		testLastLogEntry(t, logHook, "Change revision already calculated for application guestbook")
	})

	useAnnotations = false

	r.Run("New change revision without annotations", func(t *testing.T) {
		acrService, _, app := testNewChangeRevision(t)
		app, err := acrService.applicationClientset.ArgoprojV1alpha1().Applications(app.Namespace).Get(r.Context(), app.Name, metav1.GetOptions{})
		require.NoError(t, err)
		assert.Len(t, app.GetAnnotations(), 1)
		assert.Equal(t, newChangeRevision, app.Status.OperationState.Operation.Sync.ChangeRevision)
	})

	appSrc = syncedAppWithSingleHistory
	newChangeRevision = ""
	r.Run("No previous revision", func(t *testing.T) {
		acrService, logHook, app := testNewChangeRevision(t)
		app, err := acrService.applicationClientset.ArgoprojV1alpha1().Applications(app.Namespace).Get(r.Context(), app.Name, metav1.GetOptions{})
		require.NoError(t, err)
		assert.Len(t, app.GetAnnotations(), 1)
		testLastLogEntry(t, logHook, "No patch needed")
	})

	appSrc = multisourceApp

	r.Run("No current revision", func(t *testing.T) {
		acrService, logHook, app := testNewChangeRevision(t)
		app, err := acrService.applicationClientset.ArgoprojV1alpha1().Applications(app.Namespace).Get(r.Context(), app.Name, metav1.GetOptions{})
		require.NoError(t, err)
		assert.Len(t, app.GetAnnotations(), 1)
		testLastLogEntry(t, logHook, "Got empty current revision for application guestbook, is it an unsupported multisource or helm repo based application?")
	})
}
