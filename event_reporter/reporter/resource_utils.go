package reporter

import (
	"encoding/json"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	appv1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
)

type ResourceTypeKey struct {
	Group string
	Kind  string
}

var allowedResourceTypes = map[ResourceTypeKey]bool{
	// Kubernetes core resources
	{Group: appsv1.GroupName, Kind: "ReplicaSet"}:  true,
	{Group: appsv1.GroupName, Kind: "Deployment"}:  true,
	{Group: appsv1.GroupName, Kind: "StatefulSet"}: true,
	{Group: corev1.GroupName, Kind: "Service"}:     true,
	{Group: corev1.GroupName, Kind: "ConfigMap"}:   true,

	// Argo CD resources
	{Group: "argoproj.io", Kind: "Application"}:    true,
	{Group: "argoproj.io", Kind: "ApplicationSet"}: true,

	// Argo Rollouts resources
	{Group: "argoproj.io", Kind: "Rollout"}:     true,
	{Group: "argoproj.io", Kind: "AnalysisRun"}: true,

	// Argo Workflows resources
	{Group: "argoproj.io", Kind: "Workflow"}:                true,
	{Group: "argoproj.io", Kind: "WorkflowTemplate"}:        true,
	{Group: "argoproj.io", Kind: "ClusterWorkflowTemplate"}: true,

	// Argo Events resources
	{Group: "argoproj.io", Kind: "Sensor"}:      true,
	{Group: "argoproj.io", Kind: "EventSource"}: true,

	// Codefresh resources
	{Group: "codefresh.io", Kind: "Product"}:             true,
	{Group: "codefresh.io", Kind: "PromotionFlow"}:       true,
	{Group: "codefresh.io", Kind: "PromotionPolicy"}:     true,
	{Group: "codefresh.io", Kind: "PromotionTemplate"}:   true,
	{Group: "codefresh.io", Kind: "RestrictedGitSource"}: true,

	// Bitnami resources
	{Group: "bitnami.com", Kind: "SealedSecret"}: true,
}

const (
	CODEFRESH_IO_ENTITY = "codefresh_io_entity"
	CODEFRESH_CM_NAME   = "codefresh-cm"
)

func isAllowedResource(rs appv1.ResourceStatus) bool {
	gvk := rs.GroupVersionKind()
	resourceKey := ResourceTypeKey{
		Group: gvk.Group,
		Kind:  gvk.Kind,
	}

	return allowedResourceTypes[resourceKey]
}

func isAllowedConfigMap(manifest string) bool {
	var u unstructured.Unstructured
	if err := json.Unmarshal([]byte(manifest), &u); err != nil {
		return false
	}

	// Check if it's the codefresh-cm
	if u.GetName() == CODEFRESH_CM_NAME {
		return true
	}

	// Check for the codefresh_io_entity label
	labels := u.GetLabels()
	_, hasCodefreshEntityLabel := labels[CODEFRESH_IO_ENTITY]
	return labels != nil && hasCodefreshEntityLabel
}
