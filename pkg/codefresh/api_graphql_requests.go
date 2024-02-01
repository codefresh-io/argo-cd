package codefresh

type CodefreshGraphQLRequests struct {
	client CodefreshClientInterface
}

type CodefreshGraphQLRequestsInterface interface {
	GetApplicationConfiguration(app *ApplicationIdentity) (*ApplicationConfiguration, error)
}

// GetApplicationConfiguration method to get application configuration
func (r *CodefreshGraphQLRequests) GetApplicationConfiguration(app *ApplicationIdentity) (*ApplicationConfiguration, error) {
	query := GraphQLQuery{
		Query: `
		query ($cluster: String!, $namespace: String!, $name: String!) {
		  applicationConfigurationByRuntime(cluster: $cluster, namespace: $namespace, name: $name) {
			versionSource {
			  file
			  jsonPath
			}
		  }
		}
		`,
		Variables: map[string]interface{}{
			"cluster":   app.Cluster,
			"namespace": app.Namespace,
			"name":      app.Name,
		},
	}

	responseData, err := r.client.SendGraphQLRequest(query)
	if err != nil {
		return nil, err
	}

	return responseData.(*ApplicationConfiguration), nil
}

func NewCodefreshGraphQLRequests(client CodefreshClientInterface) CodefreshGraphQLRequestsInterface {
	return &CodefreshGraphQLRequests{client}
}
