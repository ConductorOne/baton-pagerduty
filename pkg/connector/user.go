package connector

import (
	"context"
	"errors"
	"fmt"

	"github.com/PagerDuty/go-pagerduty"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/resource"
)

type userResourceType struct {
	resourceType *v2.ResourceType
	client       *pagerduty.Client
}

var _ connectorbuilder.ResourceSyncerLimited = &userResourceType{}
var _ connectorbuilder.AccountManagerLimited = &userResourceType{}
var _ connectorbuilder.ResourceDeleterV2Limited = &userResourceType{}

func (u *userResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return u.resourceType
}

// Create a new connector resource for a PagerDuty User.
func userResource(user *pagerduty.User) (*v2.Resource, error) {
	firstName, lastName := resource.SplitFullName(user.Name)
	profile := map[string]interface{}{
		"first_name": firstName,
		"last_name":  lastName,
		"login":      user.Email,
		"user_id":    user.ID,
	}

	ret, err := resource.NewUserResource(
		user.Name,
		resourceTypeUser,
		user.ID,
		[]resource.UserTraitOption{
			resource.WithEmail(user.Email, true),
			resource.WithUserProfile(profile),
			resource.WithStatus(v2.UserTrait_Status_STATUS_ENABLED),
		},
	)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func (u *userResourceType) List(ctx context.Context, parentID *v2.ResourceId, pt *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	bag, page, err := parsePageToken(pt.Token, &v2.ResourceId{ResourceType: resourceTypeUser.Id})
	if err != nil {
		return nil, "", nil, err
	}

	paginationOpts := pagerduty.ListUsersOptions{
		Limit:  ResourcesPageSize,
		Offset: page,
	}

	pageToken, err := handleNextPage(bag, page+ResourcesPageSize)
	if err != nil {
		return nil, "", nil, err
	}

	usersResponse, err := u.client.ListUsersWithContext(ctx, paginationOpts)
	if err != nil {
		return nil, "", nil, wrapPagerDutyError("failed to list users", err)
	}

	rv := make([]*v2.Resource, 0, len(usersResponse.Users))
	for _, user := range usersResponse.Users {
		ur, err := userResource(&user) // #nosec G601
		if err != nil {
			return nil, "", nil, err
		}

		rv = append(rv, ur)
	}

	if usersResponse.More {
		return rv, pageToken, nil, nil
	}

	return rv, "", nil, nil
}

func (u *userResourceType) Entitlements(_ context.Context, _ *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func (u *userResourceType) Grants(_ context.Context, _ *v2.Resource, _ *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

// CreateAccountCapabilityDetails returns the account provisioning capability details.
// PagerDuty does not require passwords for user creation.
func (u *userResourceType) CreateAccountCapabilityDetails(_ context.Context) (*v2.CredentialDetailsAccountProvisioning, annotations.Annotations, error) {
	return &v2.CredentialDetailsAccountProvisioning{
		SupportedCredentialOptions: []v2.CapabilityDetailCredentialOption{
			v2.CapabilityDetailCredentialOption_CAPABILITY_DETAIL_CREDENTIAL_OPTION_NO_PASSWORD,
		},
		PreferredCredentialOption: v2.CapabilityDetailCredentialOption_CAPABILITY_DETAIL_CREDENTIAL_OPTION_NO_PASSWORD,
	}, nil, nil
}

// CreateAccount creates a new user account in PagerDuty.
func (u *userResourceType) CreateAccount(
	ctx context.Context,
	accountInfo *v2.AccountInfo,
	credentialOptions *v2.LocalCredentialOptions,
) (
	connectorbuilder.CreateAccountResponse,
	[]*v2.PlaintextData,
	annotations.Annotations,
	error,
) {
	outputAnnotations := annotations.New()
	pMap := accountInfo.Profile.AsMap()

	// Extract email - required field
	email, ok := pMap["email"].(string)
	if !ok || email == "" {
		// Try to get email from accountInfo.Emails if available
		if len(accountInfo.Emails) > 0 {
			email = accountInfo.Emails[0].Address
		}
		if email == "" {
			return nil, nil, outputAnnotations, fmt.Errorf("pagerduty-connector: missing or invalid email")
		}
	}

	// Extract name - required field
	name, ok := pMap["name"].(string)
	if !ok || name == "" {
		return nil, nil, outputAnnotations, fmt.Errorf("pagerduty-connector: missing or invalid name")
	}

	// Extract role (optional, defaults to user)
	role, _ := pMap["role"].(string)
	if role == "" {
		role = "user"
	}

	// Extract optional fields
	jobTitle, _ := pMap["job_title"].(string)
	timezone, _ := pMap["timezone"].(string)

	// Create user in PagerDuty
	createdUser, err := u.client.CreateUserWithContext(ctx, pagerduty.User{
		Name:     name,
		Email:    email,
		Role:     role,
		JobTitle: jobTitle,
		Timezone: timezone,
	})
	if err != nil {
		return nil, nil, outputAnnotations, fmt.Errorf("pagerduty-connector: failed to create user: %w", err)
	}

	// Create resource from created user
	userRes, err := userResource(createdUser)
	if err != nil {
		return nil, nil, outputAnnotations, fmt.Errorf("pagerduty-connector: failed to create user resource: %w", err)
	}

	return &v2.CreateAccountResponse_SuccessResult{
		Resource: userRes,
	}, nil, outputAnnotations, nil
}

// Delete implements the ResourceDeleterV2 interface - deletes a user from PagerDuty.
func (u *userResourceType) Delete(ctx context.Context, resourceId *v2.ResourceId, parentResourceID *v2.ResourceId) (annotations.Annotations, error) {
	userID := resourceId.GetResource()
	err := u.client.DeleteUserWithContext(ctx, userID)
	if err != nil {
		var apiErr pagerduty.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			return nil, nil
		}
		return nil, fmt.Errorf("pagerduty-connector: failed to delete user: %w", err)
	}
	return nil, nil
}

func userBuilder(client *pagerduty.Client) *userResourceType {
	return &userResourceType{
		resourceType: resourceTypeUser,
		client:       client,
	}
}
