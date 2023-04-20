package connector

import (
	"context"
	"fmt"
	"strconv"

	"github.com/PagerDuty/go-pagerduty"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

const (
	teamRoleMember    = "member"
	teamRoleObserver  = "observer"
	teamRoleResponder = "responder"
	teamRoleManager   = "manager"
)

var teamAccessRoles = []string{
	teamRoleMember,
	teamRoleObserver,
	teamRoleResponder,
	teamRoleManager,
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

	teamsResponse, err := t.client.ListTeamsWithContext(ctx, paginationOpts)
	if err != nil {
		return nil, "", nil, fmt.Errorf("pager-duty-connector: failed to list teams: %w", err)
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
		nextPage := strconv.FormatUint(uint64(page+ResourcesPageSize), 10)
		pageToken, err := bag.NextToken(nextPage)
		if err != nil {
			return nil, "", nil, err
		}

		return rv, pageToken, nil, nil
	}

	return rv, "", nil, nil
}

func (t *teamResourceType) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	rv := make([]*v2.Entitlement, 0, len(teamAccessRoles))

	for _, role := range teamAccessRoles {
		var createEntitlementFunc func(*v2.Resource, string, ...ent.EntitlementOption) *v2.Entitlement

		if role == teamRoleMember {
			createEntitlementFunc = ent.NewAssignmentEntitlement
		} else {
			createEntitlementFunc = ent.NewPermissionEntitlement
		}

		entitlementOptions := []ent.EntitlementOption{
			ent.WithGrantableTo(resourceTypeUser),
			ent.WithDisplayName(fmt.Sprintf("%s Team %s", resource.DisplayName, titleCaser.String(role))),
			ent.WithDescription(fmt.Sprintf("Team %s role in PagerDuty", resource.DisplayName)),
		}

		rv = append(rv, createEntitlementFunc(resource, role, entitlementOptions...))
	}

	return rv, "", nil, nil
}

func (t *teamResourceType) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	teamTrait, err := rs.GetGroupTrait(resource)
	if err != nil {
		return nil, "", nil, err
	}

	teamId, ok := rs.GetProfileStringValue(teamTrait.Profile, "team_id")
	if !ok {
		return nil, "", nil, fmt.Errorf("error fetching team id from team profile")
	}

	bag, page, err := parsePageToken(pToken.Token, resource.Id)
	if err != nil {
		return nil, "", nil, err
	}

	paginationOpts := pagerduty.ListTeamMembersOptions{
		Limit:  ResourcesPageSize,
		Offset: page,
	}

	teamMembersResponse, err := t.client.ListTeamMembers(ctx, teamId, paginationOpts)
	if err != nil {
		return nil, "", nil, fmt.Errorf("pager-duty-connector: failed to list team members: %w", err)
	}

	rv := make([]*v2.Grant, 0, len(teamMembersResponse.Members))
	for _, member := range teamMembersResponse.Members {
		user, err := t.client.GetUserWithContext(ctx, member.User.ID, pagerduty.GetUserOptions{})
		if err != nil {
			return nil, "", nil, fmt.Errorf("pager-duty-connector: failed to list user: %w", err)
		}

		userCopy := user
		ur, err := userResource(ctx, userCopy)
		if err != nil {
			return nil, "", nil, err
		}

		// check if the user role exists among supported ones
		if !contains(teamAccessRoles, member.Role) {
			return nil, "", nil, fmt.Errorf("pager-duty-connector: unsupported user role: %s", member.Role)
		}

		// Create a new grant for the team role
		rv = append(rv, grant.NewGrant(
			resource,
			member.Role,
			ur.Id,
		))

		// Create a new grant for the user membership role
		rv = append(rv, grant.NewGrant(
			resource,
			teamRoleMember,
			ur.Id,
		))
	}

	if teamMembersResponse.More {
		nextPage := strconv.FormatUint(uint64(page+ResourcesPageSize), 10)
		pageToken, err := bag.NextToken(nextPage)
		if err != nil {
			return nil, "", nil, err
		}

		return rv, pageToken, nil, nil
	}

	return rv, "", nil, nil
}

func teamBuilder(client *pagerduty.Client) *teamResourceType {
	return &teamResourceType{
		resourceType: resourceTypeTeam,
		client:       client,
	}
}
