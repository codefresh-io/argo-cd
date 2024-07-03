package reporter

import (
	appv1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/argoproj/argo-cd/v2/util/argo"
	"github.com/argoproj/gitops-engine/pkg/utils/kube"
	"strings"
)

const appInstanceNameDelimeter = "_"

type AppIdentity struct {
	name      string
	namespace string
}

// logic connected to /argo-cd/pkg/apis/application/v1alpha1/types.go - InstanceName
func instanceNameIncludesNs(instanceName string) bool {
	return strings.Contains(instanceName, appInstanceNameDelimeter)
}

// logic connected to /argo-cd/pkg/apis/application/v1alpha1/types.go - InstanceName
func parseInstanceName(appNameString string) AppIdentity {
	parts := strings.Split(appNameString, appInstanceNameDelimeter)
	namespace := parts[0]
	app := parts[1]

	return AppIdentity{
		name:      app,
		namespace: namespace,
	}
}

func getParentAppIdentity(a *appv1.Application, appInstanceLabelKey string, trackingMethod appv1.TrackingMethod) AppIdentity {
	resourceTracking := argo.NewResourceTracking()
	unApp := kube.MustToUnstructured(&a)

	instanceName := resourceTracking.GetAppName(unApp, appInstanceLabelKey, trackingMethod)

	if instanceNameIncludesNs(instanceName) {
		return parseInstanceName(instanceName)
	} else {
		return AppIdentity{
			name:      instanceName,
			namespace: "",
		}
	}
}

func isChildApp(parentApp AppIdentity) bool {
	return parentApp.name != ""
}
