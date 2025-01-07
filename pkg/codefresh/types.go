package codefresh

import (
	"github.com/argoproj/argo-cd/v2/event_reporter/utils"
	appv1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/argoproj/argo-cd/v2/reposerver/apiclient"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type (
	// VersionSource structure for the versionSource field
	VersionSource struct {
		File     string `json:"file"`
		JsonPath string `json:"jsonPath"`
	}

	PromotionTemplate struct {
		VersionSource VersionSource `json:"versionSource"`
	}

	Source struct {
		RepoURL                string               `json:"repoURL"`
		TargetRevision         string               `json:"targetRevision"`
		Revisions              []string             `json:"revisions,omitempty"`
		OperationSyncRevisions []string             `json:"operationSyncRevisions,omitempty"`
		CommitMessage          string               `json:"commitMessage"`
		CommitAuthor           string               `json:"commitAuthor"`
		CommitDate             *metav1.Time         `json:"commitDate,omitempty"`
		ClusterServer          string               `json:"clusterServer"`
		ClusterName            *string              `json:"clusterName,omitempty"`
		AppName                string               `json:"appName"`
		AppLabels              map[string]string    `json:"appLabels"`
		AppUID                 types.UID            `json:"appUID"`
		SyncStatus             appv1.SyncStatusCode `json:"syncStatus"`
		SyncStartedAt          metav1.Time          `json:"syncStartedAt"`
		SyncFinishedAt         *metav1.Time         `json:"syncFinishedAt,omitempty"`
		HealthStatus           *appv1.HealthStatus  `json:"healthStatus,omitempty"`
		HistoryId              int64                `json:"historyId"`
		AppNamespace           string               `json:"appNamespace"`
		AppInstanceLabelKey    string               `json:"appInstanceLabelKey"`
		TrackingMethod         appv1.TrackingMethod `json:"trackingMethod"`
		AppMultiSourced        bool                 `json:"appMultiSourced"`
		AppSourceIdx           int32                `json:"appSourceIdx"`
	}

	ErrorSourceReference struct {
		Group     string `json:"group"`
		Version   string `json:"version"`
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}

	EventError struct {
		Type            string                `json:"type"`
		Level           string                `json:"level"`
		Message         string                `json:"message"`
		LastSeen        metav1.Time           `json:"lastSeen"`
		SourceReference *ErrorSourceReference `json:"sourceReference,omitempty"`
	}

	ResourcePayload struct {
		Timestamp         metav1.Time                     `json:"timestamp"`
		ActualManifest    string                          `json:"actualManifest"`
		DesiredManifest   string                          `json:"desiredManifest"`
		Source            *Source                         `json:"source"`
		AppVersions       *apiclient.ApplicationVersions  `json:"appVersions,omitempty"`
		RevisionsMetadata *utils.AppSyncRevisionsMetadata `json:"revisionsMetadata,omitempty"`
		Errors            []*EventError                   `json:"errors,omitempty"`
	}

	ApplicationPayload struct {
		Timestamp         metav1.Time                     `json:"timestamp"`
		ActualManifest    string                          `json:"actualManifest"`
		AppVersions       *apiclient.ApplicationVersions  `json:"appVersions,omitempty"`
		RevisionsMetadata *utils.AppSyncRevisionsMetadata `json:"revisionsMetadata,omitempty"`
		Errors            []*EventError                   `json:"errors,omitempty"`
	}
)
