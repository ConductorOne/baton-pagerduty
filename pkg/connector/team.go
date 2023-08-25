package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/PagerDuty/go-pagerduty"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

const (
	teamRoleObserver  = "team-observer"
	teamRoleResponder = "team-responder"
	teamRoleManager   = "team-manager"
)

var teamAccessRoles = map[string]string{
	roleObserver:  teamRoleObserver,
	roleResponder: teamRoleResponder,
	roleManager:   teamRoleManager,
}

type teamResourceType struct {
	resourceType *v2.ResourceType
	client       *pagerduty.Client
}

func (t *teamResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return t.resourceType
}

// teamResource creates a new connector resource for a PagerDuty Team.
func teamResource(team *pagerduty.Team) (*v2.Resource, error) {
	profile := map[string]interface{}{
		"team_id":   team.ID,
		"team_name": team.Name,
	}

	resource, err := rs.NewGroupResource(
		team.Name,
		resourceTypeTeam,
		team.ID,
		[]rs.GroupTraitOption{rs.WithGroupProfile(profile)},
	)
	if err != nil {
		return nil, err
	}

	return resource, nil
}

func (t *teamResourceType) List(ctx context.Context, parentID *v2.ResourceId, pt *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	bag, page, err := parsePageToken(pt.Token, &v2.ResourceId{ResourceType: resourceTypeTeam.Id})
	if err != nil {
		return nil, "", nil, err
	}

	paginationOpts := pagerduty.ListTeamOptions{
		Limit:  ResourcesPageSize,
		Offset: page,
	}

	pageToken, err := handleNextPage(bag, page+ResourcesPageSize)
	if err != nil {
		return nil, "", nil, err
	}

	teamsResponse, err := t.client.ListTeamsWithContext(ctx, paginationOpts)
	if err != nil {
		return nil, "", nil, fmt.Errorf("pagerduty-connector: failed to list teams: %w", err)
	}

	rv := make([]*v2.Resource, 0, len(teamsResponse.Teams))
	for _, team := range teamsResponse.Teams {
		teamCopy := team

		tr, err := teamResource(&teamCopy)
		if err != nil {
			return nil, "", nil, err
		}

		rv = append(rv, tr)
	}

	if teamsResponse.More {
		return rv, pageToken, nil, nil
	}

	return rv, "", nil, nil
}

func (t *teamResourceType) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	rv := make([]*v2.Entitlement, 0, len(teamAccessRoles))

	entitlementOptions := []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeUser),
		ent.WithDisplayName(fmt.Sprintf("%s Team %s", resource.DisplayName, titleCase(roleMember))),
		ent.WithDescription(fmt.Sprintf("Team %s role in PagerDuty", resource.DisplayName)),
	}

	rv = append(rv, ent.NewAssignmentEntitlement(resource, roleMember, entitlementOptions...))

	// Create a new entitlement for each team role
	for roleName, role := range teamAccessRoles {
		rv = append(rv, ent.NewPermissionEntitlement(
			resource,
			role,
			[]ent.EntitlementOption{
				ent.WithGrantableTo(resourceTypeUser),
				ent.WithDisplayName(fmt.Sprintf("%s Team Role %s", resource.DisplayName, titleCase(roleName))),
				ent.WithDescription(fmt.Sprintf("Team %s role %s in PagerDuty", resource.DisplayName, titleCase(roleName))),
			}...,
		))
	}

	return rv, "", nil, nil
}

func (t *teamResourceType) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	bag, page, err := parsePageToken(pToken.Token, resource.Id)
	if err != nil {
		return nil, "", nil, err
	}

	paginationOpts := pagerduty.ListTeamMembersOptions{
		Limit:  ResourcesPageSize,
		Offset: page,
	}

	pageToken, err := handleNextPage(bag, page+ResourcesPageSize)
	if err != nil {
		return nil, "", nil, err
	}

	teamMembersResponse, err := t.client.ListTeamMembers(ctx, resource.Id.Resource, paginationOpts)
	if err != nil {
		return nil, "", nil, fmt.Errorf("pagerduty-connector: failed to list team members: %w", err)
	}

	var rv []*v2.Grant
	for _, member := range teamMembersResponse.Members {
		user, err := t.client.GetUserWithContext(ctx, member.User.ID, pagerduty.GetUserOptions{})
		if err != nil {
			return nil, "", nil, fmt.Errorf("pagerduty-connector: failed to list user: %w", err)
		}

		userCopy := user
		ur, err := userResource(ctx, userCopy)
		if err != nil {
			return nil, "", nil, err
		}

		// Create a new grant for the user membership role
		rv = append(rv, grant.NewGrant(
			resource,
			roleMember,
			ur.Id,
		))

		// Create also new grant for each team role the user has
		rv = append(rv, grant.NewGrant(
			resource,
			teamAccessRoles[member.Role],
			ur.Id,
		))
	}

	if teamMembersResponse.More {
		return rv, pageToken, nil, nil
	}

	return rv, "", nil, nil
}

func (t *teamResourceType) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	if principal.Id.ResourceType != resourceTypeUser.Id {
		l.Warn(
			"pagerduty-connector: only users can be granted team membership",
			zap.String("principal_id", principal.Id.Resource),
			zap.String("principal_type", principal.Id.ResourceType),
		)

		return nil, fmt.Errorf("pagerduty-connector: only users can be granted team membership")
	}

	teamId, entitlementId := entitlement.Resource.Id.Resource, entitlement.Slug

	var roleId pagerduty.TeamUserRole

	if entitlementId != roleMember {
		// permission (role) entitlement also contains prefix `team-` which needs to be removed
		entitlementId = strings.TrimPrefix(entitlementId, "team-")
		roleId = pagerduty.TeamUserRole(entitlementId)
	}

	// grant team membership
	err := t.client.AddUserToTeamWithContext(
		ctx,
		pagerduty.AddUserToTeamOptions{
			TeamID: teamId,
			UserID: principal.Id.Resource,
			Role:   roleId,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("pagerduty-connector: failed to grant team membership or team role: %w", err)
	}

	return nil, nil
}

func (t *teamResourceType) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	entitlement := grant.Entitlement
	principal := grant.Principal

	if principal.Id.ResourceType != resourceTypeUser.Id {
		l.Warn(
			"pagerduty-connector: only users can have team membership revoked",
			zap.String("principal_id", principal.Id.Resource),
			zap.String("principal_type", principal.Id.ResourceType),
		)

		return nil, fmt.Errorf("pagerduty-connector: only users can have team membership revoked")
	}

	teamId := entitlement.Resource.Id.Resource

	// revoke team membership
	err := t.client.RemoveUserFromTeamWithContext(
		ctx,
		teamId,
		principal.Id.Resource,
	)
	if err != nil {
		return nil, fmt.Errorf("pagerduty-connector: failed to revoke team membership: %w", err)
	}

	return nil, nil
}

func teamBuilder(client *pagerduty.Client) *teamResourceType {
	return &teamResourceType{
		resourceType: resourceTypeTeam,
		client:       client,
	}
}
