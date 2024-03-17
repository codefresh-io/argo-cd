package testdata

import _ "embed"

var (
	//go:embed live_deployment_with_managed_replica.yaml
	LiveDeploymentWithManagedReplicaYaml string

	//go:embed desired_deployment.yaml
	DesiredDeploymentYaml string

	//go:embed live_validating_webhook.yaml
	LiveValidatingWebhookYaml string

	//go:embed desired_validating_webhook.yaml
	DesiredValidatingWebhookYaml string

	//go:embed live_app_with_old_label_tracking.yaml
	LiveAppWithOldLabelTrackingYaml string

	//go:embed desired_app_mixed_tracking.yaml
	DesiredAppMixedTrackingYaml string
)
