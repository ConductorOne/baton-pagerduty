package connector

import (
	"context"

	"github.com/PagerDuty/go-pagerduty"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	resourceTypeTeam = &v2.ResourceType{
		Id:          "team",
		DisplayName: "Team",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_GROUP},
	}
	resourceTypeUser = &v2.ResourceType{
		Id:          "user",
		DisplayName: "User",
		Traits: []v2.ResourceType_Trait{
			v2.ResourceType_TRAIT_USER,
		},
	}
	resourceTypeRole = &v2.ResourceType{
		Id:          "role",
		DisplayName: "Role",
		Traits: []v2.ResourceType_Trait{
			v2.ResourceType_TRAIT_ROLE,
		},
	}
)

type PagerDuty struct {
	client *pagerduty.Client
}

func (pd *PagerDuty) ResourceSyncers(ctx context.Context) []connectorbuilder.ResourceSyncer {
	return []connectorbuilder.ResourceSyncer{
		teamBuilder(pd.client),
		userBuilder(pd.client),
		roleBuilder(pd.client),
	}
}

// Metadata returns metadata about the connector.
func (pd *PagerDuty) Metadata(ctx context.Context) (*v2.ConnectorMetadata, error) {
	return &v2.ConnectorMetadata{
		DisplayName: "PagerDuty",
	}, nil
}

// Validate hits the PagerDuty API to validate that the configured credentials are still valid.
func (pd *PagerDuty) Validate(ctx context.Context) (annotations.Annotations, error) {
	_, err := pd.client.ListUsersWithContext(ctx, pagerduty.ListUsersOptions{})

	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "Provided Access Token is invalid")
	}

	return nil, nil
}

// New returns the GitHub connector configured to sync against the instance URL.
func New(ctx context.Context, accessToken string) (*PagerDuty, error) {
	client := pagerduty.NewClient(accessToken)

	pd := &PagerDuty{
		client: client,
	}

	return pd, nil
}
