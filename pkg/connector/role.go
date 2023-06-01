package connector

import (
	"context"
	"fmt"

	"github.com/PagerDuty/go-pagerduty"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

const (
	userRole = "user"
)

const (
	roleOwner      = "owner"
	roleAdmin      = "admin"
	roleRestricted = "restricted_access"
)

const (
	userRoleOwner      = "user-owner"
	userRoleAdmin      = "user-admin"
	userRoleObserver   = "user-observer"
	userRoleResponder  = "user-limited_user"
	userRoleManager    = "user-manager"
	userRoleRestricted = "user-restricted_access"
)

var userAccessRoles = map[string]string{
	roleOwner:      userRoleOwner,
	roleAdmin:      userRoleAdmin,
	roleObserver:   userRoleObserver,
	roleResponder:  userRoleResponder,
	roleManager:    userRoleManager,
	roleRestricted: userRoleRestricted,
}

type roleResourceType struct {
	resourceType *v2.ResourceType
	client       *pagerduty.Client
}

func (r *roleResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return r.resourceType
}

// roleResource creates a new connector resource for a PagerDuty Role.
func roleResource(role string, roleName string, roleType string) (*v2.Resource, error) {
	displayName := titleCaser.String(fmt.Sprintf("%s-%s", roleType, roleName))
	profile := map[string]interface{}{
		"role_id":   role,
		"role_name": displayName,
	}

	resource, err := rs.NewRoleResource(
		displayName,
		resourceTypeRole,
		role,
		[]rs.RoleTraitOption{rs.WithRoleProfile(profile)},
	)
	if err != nil {
		return nil, err
	}

	return resource, nil
}

func (r *roleResourceType) List(ctx context.Context, parentID *v2.ResourceId, pt *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	rv := make([]*v2.Resource, 0, len(userAccessRoles))
	for roleName, role := range userAccessRoles {
		roleCopy := role

		urr, err := roleResource(roleCopy, roleName, userRole)
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

func (r *roleResourceType) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	// Handle pagination
	bag, page, err := parsePageToken(pToken.Token, resource.Id)
	if err != nil {
		return nil, "", nil, err
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
		return nil, "", nil, fmt.Errorf("pager-duty-connector: failed to list users: %w", err)
	}

	var rv []*v2.Grant
	for _, user := range usersResponse.Users {
		userRole := fmt.Sprintf("user-%s", user.Role)

		if resource.Id.Resource != userRole {
			continue
		}

		userCopy := user
		ur, err := userResource(ctx, &userCopy)
		if err != nil {
			return nil, "", nil, fmt.Errorf("pager-duty-connector: failed to build user resource: %w", err)
		}

		rv = append(rv, grant.NewGrant(
			resource,
			roleMember,
			ur.Id,
		))
	}

	if usersResponse.More {
		return rv, nextPage, nil, nil
	}

	return rv, "", nil, nil
}

func roleBuilder(client *pagerduty.Client) *roleResourceType {
	return &roleResourceType{
		resourceType: resourceTypeRole,
		client:       client,
	}
}
