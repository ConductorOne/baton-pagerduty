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

const (
	abilityTeams = "teams"

	roleMember    = "member"
	roleObserver  = "observer"
	roleResponder = "responder"
	roleManager   = "manager"
)

var (
	resourceTypeTeam = &v2.ResourceType{
		Id:          "team",
		DisplayName: "Team",
		Traits: []v2.ResourceType_Trait{
			v2.ResourceType_TRAIT_GROUP,
		},
	}
	resourceTypeUser = &v2.ResourceType{
		Id:          "user",
		DisplayName: "User",
		Traits: []v2.ResourceType_Trait{
			v2.ResourceType_TRAIT_USER,
		},
		Annotations: annotationsForUserResourceType(),
	}
	resourceTypeRole = &v2.ResourceType{
		Id:          "role",
		DisplayName: "Role",
		Traits: []v2.ResourceType_Trait{
			v2.ResourceType_TRAIT_ROLE,
		},
	}
	resourceTypeSchedule = &v2.ResourceType{
		Id:          "schedule",
		DisplayName: "Schedule",
		Traits: []v2.ResourceType_Trait{
			v2.ResourceType_TRAIT_GROUP,
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
		scheduleBuilder(pd.client),
	}
}

// Metadata returns metadata about the connector.
func (pd *PagerDuty) Metadata(ctx context.Context) (*v2.ConnectorMetadata, error) {
	return &v2.ConnectorMetadata{
		DisplayName: "PagerDuty",
		Description: "Connector syncing PagerDuty users, teams, and their roles to Baton",
		AccountCreationSchema: &v2.ConnectorAccountCreationSchema{
			FieldMap: map[string]*v2.ConnectorAccountCreationSchema_Field{
				"email": {
					DisplayName: "Email",
					Required:    true,
					Description: "The email address of the user to create",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringField{
						StringField: &v2.ConnectorAccountCreationSchema_StringField{},
					},
					Placeholder: "john.doe@example.com",
					Order:       1,
				},
				"name": {
					DisplayName: "Name",
					Required:    true,
					Description: "The full name of the user",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringField{
						StringField: &v2.ConnectorAccountCreationSchema_StringField{},
					},
					Placeholder: "John Doe",
					Order:       2,
				},
				"role": {
					DisplayName: "Role",
					Required:    false,
					Description: "The role to assign to the user. Valid roles: admin, limited_user, observer, owner, " +
						"read_only_user, restricted_access, read_only_limited_user, user. " +
						"Note: read_only_user/read_only_limited_user require read_only_users ability; " +
						"observer/restricted_access require advanced permissions. Defaults to user",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringField{
						StringField: v2.ConnectorAccountCreationSchema_StringField_builder{
							DefaultValue: stringPtr("user"),
						}.Build(),
					},
					Placeholder: "user",
					Order:       3,
				},
				"job_title": {
					DisplayName: "Job Title",
					Required:    false,
					Description: "The job title of the user",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringField{
						StringField: &v2.ConnectorAccountCreationSchema_StringField{},
					},
					Placeholder: "Software Engineer",
					Order:       4,
				},
				"timezone": {
					DisplayName: "Timezone",
					Required:    false,
					Description: "The timezone of the user. Must be a valid PagerDuty timezone name " +
						"(ActiveSupport::TimeZone format, e.g., \"Eastern Time (US & Canada)\", \"UTC\")",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringField{
						StringField: &v2.ConnectorAccountCreationSchema_StringField{},
					},
					Placeholder: "Eastern Time (US & Canada)",
					Order:       5,
				},
			},
		},
	}, nil
}

// Validate hits the PagerDuty API to validate that the configured credentials are valid and compatible.
func (pd *PagerDuty) Validate(ctx context.Context) (annotations.Annotations, error) {
	// should be able to list users
	_, err := pd.client.ListUsersWithContext(ctx, pagerduty.ListUsersOptions{})
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "Provided Access Token is invalid")
	}

	// in case it's a user token, check the role for compatibility
	user, _ := pd.client.GetCurrentUserWithContext(ctx, pagerduty.GetCurrentUserOptions{})
	if user != nil && user.Role == "restricted_access" {
		return nil, status.Error(codes.PermissionDenied, "Provided Access Token must be an admin token")
	}

	return nil, nil
}

// New returns the PagerDuty connector.
func New(ctx context.Context, accessToken string, baseURL string) (*PagerDuty, error) {
	var opts []pagerduty.ClientOptions
	if baseURL != "" {
		opts = append(opts, pagerduty.WithAPIEndpoint(baseURL))
	}
	client := pagerduty.NewClient(accessToken, opts...)

	pd := &PagerDuty{
		client: client,
	}

	return pd, nil
}

// stringPtr returns a pointer to the given string.
func stringPtr(s string) *string {
	return &s
}
