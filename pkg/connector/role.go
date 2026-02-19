package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/PagerDuty/go-pagerduty"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TODO: Add support for advanced permissions (Team roles, object roles)
const (
	userRoleType      = "user"
	limitedUserRoleId = "limited_user"
)

// Note: Not all of these roles are available to all PagerDuty plans.
// For example, the "stakeholder" role is not available to Free plans.
// See https://support.pagerduty.com/main/docs/user-roles for more information.
var roleIDsToNames = map[string]string{
	"admin":                  "Administrator",
	limitedUserRoleId:        "Limited User",
	"observer":               "Observer",
	"owner":                  "Owner",
	"read_only_limited_user": "Read-Only Limited User",
	"read_only_user":         "Read-Only User",
	"restricted_access":      "Restricted Access",
	"stakeholder":            "Stakeholder",
	"team_responder":         "Team Responder",
	"user":                   "User",
}

type roleResourceType struct {
	resourceType *v2.ResourceType
	client       *pagerduty.Client
}

var _ connectorbuilder.ResourceProvisioner = &roleResourceType{}

func (r *roleResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return r.resourceType
}

// roleResource creates a new connector resource for a PagerDuty Role.
func roleResource(roleId string, roleName string, roleType string) (*v2.Resource, error) {
	roleResourceId := fmt.Sprintf("%s-%s", roleType, roleId)
	var displayName string
	// Omit the role type from the display name if it's a user role.
	if roleType == userRoleType {
		displayName = titleCase(roleName)
	} else {
		displayName = titleCase(fmt.Sprintf("%s role: %s", roleType, roleName))
	}
	profile := map[string]any{
		"role_id":   roleResourceId,
		"role_name": displayName,
	}

	resource, err := rs.NewRoleResource(
		displayName,
		resourceTypeRole,
		roleResourceId,
		[]rs.RoleTraitOption{rs.WithRoleProfile(profile)},
	)
	if err != nil {
		return nil, err
	}

	return resource, nil
}

func (r *roleResourceType) List(ctx context.Context, parentID *v2.ResourceId, pt *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	rv := make([]*v2.Resource, 0, len(roleIDsToNames))
	for roleId, roleName := range roleIDsToNames {
		urr, err := roleResource(roleId, roleName, userRoleType)
		if err != nil {
			return nil, "", nil, err
		}

		rv = append(rv, urr)
	}

	return rv, "", nil, nil
}

func (r *roleResourceType) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var rv []*v2.Entitlement

	entitlementOptions := []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeUser),
		ent.WithDisplayName(fmt.Sprintf("%s role", resource.DisplayName)),
		ent.WithDescription(fmt.Sprintf("%s PagerDuty role", resource.DisplayName)),
	}

	rv = append(rv, ent.NewAssignmentEntitlement(resource, roleMember, entitlementOptions...))

	return rv, "", nil, nil
}

func parseResourceRoleId(resourceId string) (string, string, error) {
	parts := strings.SplitN(resourceId, "-", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("pagerduty-connector: invalid resource id: %s", resourceId)
	}
	return parts[0], parts[1], nil
}

func (r *roleResourceType) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	bag, page, err := parsePageToken(pToken.Token, resource.Id)
	if err != nil {
		return nil, "", nil, err
	}

	roleType, roleId, err := parseResourceRoleId(resource.Id.Resource)
	if err != nil {
		return nil, "", nil, err
	}
	if roleType != userRoleType {
		return nil, "", nil, nil
	}

	paginationOpts := pagerduty.ListUsersOptions{
		Limit:  ResourcesPageSize,
		Offset: page,
	}

	nextPage, err := handleNextPage(bag, page+ResourcesPageSize)
	if err != nil {
		return nil, "", nil, err
	}

	usersResponse, err := r.client.ListUsersWithContext(ctx, paginationOpts)
	if err != nil {
		return nil, "", nil, wrapPagerDutyError("failed to list users", err)
	}

	var rv []*v2.Grant
	for _, user := range usersResponse.Users {
		if roleId != user.Role {
			continue
		}

		uID, err := rs.NewResourceID(resourceTypeUser, user.ID)
		if err != nil {
			return nil, "", nil, fmt.Errorf("pagerduty-connector: failed to create user resource id: %w", err)
		}

		grantOpts := []grant.GrantOption{}
		if roleId == limitedUserRoleId {
			// PagerDuty users must have a role, and limited_user is the lowest level of permissions, so it cannot be revoked.
			grantOpts = append(grantOpts, grant.WithAnnotation(&v2.GrantImmutable{}))
		}
		rv = append(rv, grant.NewGrant(
			resource,
			roleMember,
			uID,
			grantOpts...,
		))
	}

	if usersResponse.More {
		return rv, nextPage, nil, nil
	}

	return rv, "", nil, nil
}

func (r *roleResourceType) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	if principal.Id.ResourceType != resourceTypeUser.Id {
		l.Warn(
			"pagerduty-connector: only users can be granted roles",
			zap.String("principal_id", principal.Id.Resource),
			zap.String("principal_type", principal.Id.ResourceType),
		)

		return nil, fmt.Errorf("pagerduty-connector: only users can be granted roles")
	}

	user, err := r.client.GetUserWithContext(
		ctx,
		principal.Id.Resource,
		pagerduty.GetUserOptions{},
	)
	if err != nil {
		return nil, wrapPagerDutyError("failed to get user", err)
	}

	roleType, roleId, err := parseResourceRoleId(entitlement.GetResource().GetId().GetResource())
	if err != nil {
		return nil, err
	}
	if roleType != userRoleType {
		return nil, fmt.Errorf("pagerduty-connector: only user roles can be granted")
	}
	if user.Role == roleId {
		return annotations.New(&v2.GrantAlreadyExists{}), nil
	}

	user.Role = roleId

	// grant role membership
	_, err = r.client.UpdateUserWithContext(
		ctx,
		*user,
	)
	if err != nil {
		return nil, wrapPagerDutyError(fmt.Sprintf("failed to grant role %s", roleId), err)
	}

	return nil, nil
}

func (r *roleResourceType) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	principal := grant.Principal
	entitlement := grant.Entitlement

	if principal.Id.ResourceType != resourceTypeUser.Id {
		l.Warn(
			"pagerduty-connector: only users can have roles revoked",
			zap.String("principal_id", principal.Id.Resource),
			zap.String("principal_type", principal.Id.ResourceType),
		)

		return nil, fmt.Errorf("pagerduty-connector: only users can have roles revoked")
	}

	roleType, roleId, err := parseResourceRoleId(entitlement.GetResource().GetId().GetResource())
	if err != nil {
		return nil, err
	}
	if roleType != userRoleType {
		return nil, fmt.Errorf("pagerduty-connector: only user roles can be revoked")
	}
	if roleId == limitedUserRoleId {
		return nil, fmt.Errorf("pagerduty-connector: limited_user role cannot be revoked")
	}

	user, err := r.client.GetUserWithContext(
		ctx,
		principal.Id.Resource,
		pagerduty.GetUserOptions{},
	)
	if err != nil {
		return nil, wrapPagerDutyError("failed to get user", err)
	}

	if user.Role == limitedUserRoleId {
		// Users must have a role, and limited_user is the lowest level of permissions, so it cannot be revoked.
		return annotations.New(&v2.GrantAlreadyRevoked{}), nil
	}

	// Return a grpc status of NotFound if the user doesn't have the role.
	// We don't return GrantAlreadyRevoked because the user's current role might have more permissions than the role being revoked.
	// All we know is that we were asked to revoke a role that the user doesn't have.
	if user.Role != roleId {
		return nil, status.Errorf(codes.NotFound, "pagerduty-connector: user %s does not have role %s", user.ID, roleId)
	}

	// Since PagerDuty users must have at least one role, we reset it to limited_user.
	user.Role = limitedUserRoleId

	// revoke role
	_, err = r.client.UpdateUserWithContext(
		ctx,
		*user,
	)
	if err != nil {
		return nil, wrapPagerDutyError(fmt.Sprintf("failed to revoke role %s", entitlement.Resource.Id.Resource), err)
	}

	return nil, nil
}

func roleBuilder(client *pagerduty.Client) *roleResourceType {
	return &roleResourceType{
		resourceType: resourceTypeRole,
		client:       client,
	}
}
