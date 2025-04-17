package repository

import (
	goio "io"
	"os"
	"path/filepath"

	"github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/argoproj/argo-cd/v2/pkg/version_config_manager"
	argopath "github.com/argoproj/argo-cd/v2/util/app/path"
	"github.com/argoproj/argo-cd/v2/util/git"
	"github.com/argoproj/argo-cd/v2/util/io"
	"github.com/argoproj/argo-cd/v2/util/kustomize"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type (
	CfOptions struct {
		ApplicationVersioningEnabled bool
		VersionConfig                *version_config_manager.VersionConfig
		GitClient                    git.Client
	}
)

func (s *Service) getCacheKeyWithKustomizeComponents(
	revision string,
	repo *v1alpha1.Repository,
	source *v1alpha1.ApplicationSource,
	settings operationSettings,
	gitClient git.Client,
) (string, error) {
	closer, err := s.repoLock.Lock(gitClient.Root(), revision, settings.allowConcurrent, func() (goio.Closer, error) {
		return s.checkoutRevision(gitClient, revision, s.initConstants.SubmoduleEnabled)
	})
	if err != nil {
		return "", err
	}

	defer io.Close(closer)

	appPath, err := argopath.Path(gitClient.Root(), source.Path)
	if err != nil {
		return "", err
	}

	k := kustomize.NewKustomizeApp(gitClient.Root(), appPath, repo.GetGitCreds(s.gitCredsStore), repo.Repo, source.Kustomize.Version, "", "")

	resolveRevisionFunc := func(repoURL, revision string, creds git.Creds) (string, error) {
		cloneRepo := *repo
		cloneRepo.Repo = repoURL
		_, res, err := s.newClientResolveRevision(&cloneRepo, revision)
		return res, err
	}

	return k.GetCacheKeyWithComponents(revision, source.Kustomize, resolveRevisionFunc)
}

func (s *Service) getVersionConfig(appMetadata *metav1.ObjectMeta) *version_config_manager.VersionConfig {
	if appMetadata == nil {
		log.Infof("cfAppConfig. appMetadata is nil. Unable to retrieve version configuration.")
		return nil
	}

	var versionConfig *version_config_manager.VersionConfig
	logCtx := log.WithFields(log.Fields{
		"namespace": appMetadata.Namespace,
		"name":      appMetadata.Name,
	})
	if s.initConstants.CodefreshApplicationVersioningEnabled && s.initConstants.CodefreshUseApplicationConfiguration {
		logCtx.Info("cfAppConfig. Getting version config")
		versionConfig = s.GetVersionConfig(appMetadata)
		if versionConfig != nil {
			logCtx.WithFields(log.Fields{
				"configFile": versionConfig.ResourceName,
				"jsonPath":   versionConfig.JsonPath,
			}).Infof("cfAppConfig. Version config found")
		} else {
			logCtx.Info("cfAppConfig. versionConfig is nil. Unable to retrieve version configuration.")
		}
	} else {
		logCtx.Info("cfAppConfig. Flags for application versioning (CODEFRESH_APPLICATION_VERSIONING_ENABLED and CODEFRESH_USE_APPLICATION_CONFIGURATION) disabled. Skip getting application version config.")
	}

	return versionConfig
}

func kustomizeBuild(
	k kustomize.Kustomize,
	repoRoot string,
	appPath string,
	opts *v1alpha1.ApplicationSourceKustomize,
	kustomizeOptions *v1alpha1.KustomizeOptions,
	env *v1alpha1.Env,
	buildOpts *kustomize.BuildOpts,
	namespace string,
) ([]manifest, []kustomize.Image, []string, error) {
	var targetObjs []*unstructured.Unstructured

	rawBytes, err := os.ReadFile(filepath.Join(appPath, "kustomization.yaml"))
	if err != nil {
		return nil, nil, nil, err
	}
	relPath, _ := filepath.Rel(repoRoot, appPath)
	targetObjs, images, commands, err := k.Build(opts, kustomizeOptions, env, buildOpts, namespace)
	if err != nil {
		return nil, nil, nil, err
	}

	jsonObjs, err := expandUnstructuredObjs(targetObjs)
	if err != nil {
		return nil, nil, nil, err
	}

	manifests := make([]manifest, len(jsonObjs))
	for i, obj := range jsonObjs {
		manifests[i] = manifest{
			rawManifest: rawBytes,
			obj:         obj,
			path:        relPath,
			line:        0,
		}
	}

	return manifests, images, commands, nil
}
