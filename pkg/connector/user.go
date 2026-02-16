package connector

import (
	"context"

	"github.com/PagerDuty/go-pagerduty"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

type userResourceType struct {
	resourceType *v2.ResourceType
	client       *pagerduty.Client
}

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
	l := ctxzap.Extract(ctx)
	outputAnnotations := annotations.New()

	// Extract account information
	pMap := accountInfo.Profile.AsMap()

	// Extract email - required field
	var emailStr string
	email, ok := pMap["email"]
	if ok {
		emailStr, ok = email.(string)
		if !ok {
			return nil, nil, outputAnnotations, fmt.Errorf("pagerduty-connector: email must be a string")
		}
	} else {
		// Try to get email from accountInfo.Emails if available
		if len(accountInfo.Emails) > 0 {
			emailStr = accountInfo.Emails[0].Address
		} else {
			return nil, nil, outputAnnotations, fmt.Errorf("pagerduty-connector: missing email in account info")
		}
	}

	// Extract name - required field
	name, ok := pMap["name"]
	if !ok {
		return nil, nil, outputAnnotations, fmt.Errorf("pagerduty-connector: missing name in account info")
	}
	nameStr, ok := name.(string)
	if !ok {
		return nil, nil, outputAnnotations, fmt.Errorf("pagerduty-connector: name must be a string")
	}
	if nameStr == "" {
		return nil, nil, outputAnnotations, fmt.Errorf("pagerduty-connector: name cannot be empty")
	}

	// Extract role (optional, defaults to user)
	role, ok := pMap["role"]
	if !ok {
		role = "user"
	}
	roleStr, ok := role.(string)
	if !ok {
		return nil, nil, outputAnnotations, fmt.Errorf("pagerduty-connector: role must be a string")
	}

	// Extract job_title (optional)
	var jobTitleStr string
	if jobTitle, ok := pMap["job_title"]; ok {
		jobTitleStr, _ = jobTitle.(string)
	}

	// Extract timezone (optional)
	var timezoneStr string
	if timezone, ok := pMap["timezone"]; ok {
		timezoneStr, _ = timezone.(string)
	}

	// Create user object
	user := pagerduty.User{
		Name:     nameStr,
		Email:    emailStr,
		Role:     roleStr,
		JobTitle: jobTitleStr,
		Timezone: timezoneStr,
	}

	// Create user in PagerDuty
	createdUser, err := u.client.CreateUserWithContext(ctx, user)
	if err != nil {
		l.Error("pagerduty-connector: failed to create user", zap.Error(err))
		return nil, nil, outputAnnotations, fmt.Errorf("pagerduty-connector: failed to create user: %w", err)
	}

	// Create resource from created user
	resource, err := userResource(createdUser)
	if err != nil {
		return nil, nil, outputAnnotations, fmt.Errorf("pagerduty-connector: failed to create user resource: %w", err)
	}

	car := &v2.CreateAccountResponse_SuccessResult{
		Resource: resource,
	}

	return car, nil, outputAnnotations, nil
}

// Delete implements the ResourceDeleterV2 interface - deletes a user from PagerDuty.
func (u *userResourceType) Delete(ctx context.Context, resourceId *v2.ResourceId, parentResourceID *v2.ResourceId) (annotations.Annotations, error) {
	userID := resourceId.GetResource()
	if len(userID) == 0 {
		return nil, fmt.Errorf("pagerduty-connector: missing resource ID")
	}

	l := ctxzap.Extract(ctx).With(zap.String("userID", userID))
	outputAnnotations := annotations.New()

	// Delete the user
	err := u.client.DeleteUserWithContext(ctx, userID)
	if err != nil {
		l.Error("pagerduty-connector: delete-user: failed to delete user", zap.Error(err))
		return outputAnnotations, fmt.Errorf("pagerduty-connector: failed to delete user: %w", err)
	}

	return outputAnnotations, nil
}

func userBuilder(client *pagerduty.Client) *userResourceType {
	return &userResourceType{
		resourceType: resourceTypeUser,
		client:       client,
	}
}
