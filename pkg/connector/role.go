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
	teamRole = "team"
	userRole = "user"
)

const (
	roleMember = "member"
	roleOwner  = "owner"
	roleAdmin  = "admin"

	roleObserver  = "observer"
	roleResponder = "responder"
	roleManager   = "manager"

	roleRestricted = "restricted_access"
)

const (
	teamRoleObserver  = "team-observer"
	teamRoleResponder = "team-responder"
	teamRoleManager   = "team-manager"

	userRoleOwner      = "user-owner"
	userRoleAdmin      = "user-admin"
	userRoleObserver   = "user-observer"
	userRoleResponder  = "user-limited_user"
	userRoleManager    = "user-user"
	userRoleRestricted = "user-restricted_access"
)

var teamAccessRoles = map[string]string{
	roleObserver:  teamRoleObserver,
	roleResponder: teamRoleResponder,
	roleManager:   teamRoleManager,
}

var userAccessRoles = map[string]string{
	roleOwner:      userRoleOwner,
	roleAdmin:      userRoleAdmin,
	roleObserver:   userRoleObserver,
	roleResponder:  userRoleResponder,
	roleManager:    userRoleManager,
	roleRestricted: userRoleRestricted,
}

type GrantsProgress struct {
	teamsMapped       bool
	teamMembersMapped bool
	usersMapped       bool

	teamMemberRoles map[string][]string
	userRoles       map[string][]string

	teamIndex int
	teamIds   []string
}

type roleResourceType struct {
	resourceType *v2.ResourceType
	client       *pagerduty.Client
	GrantsProgress
}

func (t *roleResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return t.resourceType
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

func (t *roleResourceType) List(ctx context.Context, parentID *v2.ResourceId, pt *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	rv := make([]*v2.Resource, 0, len(teamAccessRoles)+len(userAccessRoles))
	for roleName, role := range userAccessRoles {
		roleCopy := role

		urr, err := roleResource(roleCopy, roleName, userRole)
		if err != nil {
			return nil, "", nil, err
		}

		rv = append(rv, urr)
	}

	for roleName, role := range teamAccessRoles {
		roleCopy := role

		trr, err := roleResource(roleCopy, roleName, teamRole)
		if err != nil {
			return nil, "", nil, err
		}

		rv = append(rv, trr)
	}

	return rv, "", nil, nil
}

func (t *roleResourceType) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var rv []*v2.Entitlement

	entitlementOptions := []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeUser),
		ent.WithDisplayName(fmt.Sprintf("%s role", resource.DisplayName)),
		ent.WithDescription(fmt.Sprintf("%s PagerDuty role", resource.DisplayName)),
	}

	rv = append(rv, ent.NewAssignmentEntitlement(resource, roleMember, entitlementOptions...))

	return rv, "", nil, nil
}

func (t *roleResourceType) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	// Handle pagination
	bag, page, err := parsePageToken(pToken.Token, resource.Id)
	if err != nil {
		return nil, "", nil, err
	}

	// Loop through all the teams and map them
	if !t.teamsMapped {
		paginationOpts := pagerduty.ListTeamOptions{
			Limit:  ResourcesPageSize,
			Offset: page,
		}

		teamsResponse, err := t.client.ListTeamsWithContext(ctx, paginationOpts)
		if err != nil {
			return nil, "", nil, fmt.Errorf("pager-duty-connector: failed to list teams: %w", err)
		}

		t.teamIds = append(t.teamIds, mapTeamIds(teamsResponse.Teams)...)

		if teamsResponse.More {
			nextPage := strconv.FormatUint(uint64(page+ResourcesPageSize), 10)
			pageToken, err := bag.NextToken(nextPage)
			if err != nil {
				return nil, "", nil, err
			}

			return nil, pageToken, nil, nil
		}

		page = 0
		t.teamsMapped = true
	}

	// Loop through all team members and map received team members
	if t.teamsMapped && (len(t.teamIds) > 0) && !t.teamMembersMapped {
		paginationOpts := pagerduty.ListTeamMembersOptions{
			Limit:  ResourcesPageSize,
			Offset: page,
		}

		teamId := t.teamIds[t.teamIndex]
		roleMembersResponse, err := t.client.ListTeamMembers(ctx, teamId, paginationOpts)
		if err != nil {
			return nil, "", nil, fmt.Errorf("pager-duty-connector: failed to list team members: %w", err)
		}

		// map roles of team members to state map (role -> []member ids)
		for _, member := range roleMembersResponse.Members {
			memberId, memberRole := member.User.ID, member.Role
			teamMemberRole := fmt.Sprintf("team-%s", memberRole)

			t.teamMemberRoles[teamMemberRole] = append(t.teamMemberRoles[teamMemberRole], memberId)
		}

		if roleMembersResponse.More {
			nextPage := strconv.FormatUint(uint64(page+ResourcesPageSize), 10)
			pageToken, err := bag.NextToken(nextPage)
			if err != nil {
				return nil, "", nil, err
			}

			return nil, pageToken, nil, nil
		}

		t.teamIndex++

		if t.teamIndex < len(t.teamIds) {
			nextPage := strconv.FormatUint(uint64(page), 10)
			pageToken, err := bag.NextToken(nextPage)
			if err != nil {
				return nil, "", nil, err
			}

			return nil, pageToken, nil, nil
		}

		page = 0
		t.teamMembersMapped = true
	}

	// Loop through all users and map received users
	if !t.usersMapped {
		paginationOpts := pagerduty.ListUsersOptions{
			Limit:  ResourcesPageSize,
			Offset: page,
		}

		usersResponse, err := t.client.ListUsersWithContext(ctx, paginationOpts)
		if err != nil {
			return nil, "", nil, fmt.Errorf("pager-duty-connector: failed to list users: %w", err)
		}

		// map roles of users to state map (role -> []member ids)
		for _, user := range usersResponse.Users {
			userId, uRole := user.ID, user.Role
			userRole := fmt.Sprintf("user-%s", uRole)

			t.userRoles[userRole] = append(t.userRoles[userRole], userId)
		}

		if usersResponse.More {
			nextPage := strconv.FormatUint(uint64(page+ResourcesPageSize), 10)
			pageToken, err := bag.NextToken(nextPage)
			if err != nil {
				return nil, "", nil, err
			}

			return nil, pageToken, nil, nil
		}

		t.usersMapped = true
	}

	// Parse the role name (saved as role id) from the role profile
	roleTrait, err := rs.GetRoleTrait(resource)
	if err != nil {
		return nil, "", nil, err
	}

	roleName, ok := rs.GetProfileStringValue(roleTrait.Profile, "role_id")
	if !ok {
		return nil, "", nil, fmt.Errorf("error fetching role id from role profile")
	}

	var rv []*v2.Grant

	// loop through all user ids under listed role and build grants
	for _, memberId := range getUserIdsUnderRole(roleName, t.userRoles, t.teamMemberRoles) {
		// fetch user from pager duty
		user, err := t.client.GetUserWithContext(ctx, memberId, pagerduty.GetUserOptions{})
		if err != nil {
			return nil, "", nil, fmt.Errorf("pager-duty-connector: failed to get user: %w", err)
		}

		userCopy := user
		ur, err := userResource(ctx, userCopy)
		if err != nil {
			return nil, "", nil, fmt.Errorf("pager-duty-connector: failed to build user resource: %w", err)
		}

		rv = append(rv, grant.NewGrant(
			resource,
			roleMember,
			ur.Id,
		))
	}

	return rv, "", nil, nil
}

func roleBuilder(client *pagerduty.Client) *roleResourceType {
	return &roleResourceType{
		resourceType: resourceTypeRole,
		client:       client,
		GrantsProgress: GrantsProgress{
			teamsMapped:       false,
			teamMembersMapped: false,
			usersMapped:       false,

			teamIds:         make([]string, 0),
			teamMemberRoles: make(map[string][]string),
			userRoles:       make(map[string][]string),
		},
	}
}
