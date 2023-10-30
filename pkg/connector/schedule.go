package connector

import (
	"context"
	"fmt"
	"time"

	"github.com/PagerDuty/go-pagerduty"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

const (
	scheduleMember = "member"
	scheduleOnCall = "on-call"
)

type scheduleResourceType struct {
	resourceType *v2.ResourceType
	client       *pagerduty.Client
}

func (s *scheduleResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return s.resourceType
}

// scheduleResource creates a new connector resource for a PagerDuty Schedule.
func scheduleResource(schedule *pagerduty.Schedule) (*v2.Resource, error) {
	displayName := titleCase(fmt.Sprintf("%s-%s", schedule.Type, schedule.Name))
	profile := map[string]interface{}{
		"schedule_id":   schedule,
		"schedule_name": displayName,
	}

	if schedule.Teams != nil {
		profile["schedule_teams"] = scheduleMembersToInterfaceSlice(schedule.Teams)
	}

	if schedule.Users != nil {
		profile["schedule_users"] = scheduleMembersToInterfaceSlice(schedule.Users)
	}

	resource, err := rs.NewGroupResource(
		displayName,
		resourceTypeSchedule,
		schedule,
		[]rs.GroupTraitOption{rs.WithGroupProfile(profile)},
	)
	if err != nil {
		return nil, err
	}

	return resource, nil
}

func (s *scheduleResourceType) List(ctx context.Context, parentID *v2.ResourceId, pt *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	bag, page, err := parsePageToken(pt.Token, &v2.ResourceId{ResourceType: resourceTypeSchedule.Id})
	if err != nil {
		return nil, "", nil, err
	}

	paginationOpts := pagerduty.ListSchedulesOptions{
		Limit:  ResourcesPageSize,
		Offset: page,
	}

	pageToken, err := handleNextPage(bag, page+ResourcesPageSize)
	if err != nil {
		return nil, "", nil, err
	}

	schedulesResponse, err := s.client.ListSchedulesWithContext(ctx, paginationOpts)
	if err != nil {
		return nil, "", nil, fmt.Errorf("pagerduty-connector: failed to list schedules: %w", err)
	}

	var rv []*v2.Resource
	for _, schedule := range schedulesResponse.Schedules {
		scheduleCopy := schedule

		sr, err := scheduleResource(&scheduleCopy)
		if err != nil {
			return nil, "", nil, err
		}

		rv = append(rv, sr)
	}

	if schedulesResponse.More {
		return rv, pageToken, nil, nil
	}

	return rv, "", nil, nil
}

func (s *scheduleResourceType) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var rv []*v2.Entitlement

	memberEntitlementOptions := []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeUser, resourceTypeTeam),
		ent.WithDisplayName(fmt.Sprintf("%s schedule %s", resource.DisplayName, scheduleMember)),
		ent.WithDescription(fmt.Sprintf("%s PagerDuty schedule %s", resource.DisplayName, scheduleMember)),
	}

	oncallEntitlementOptions := []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeUser),
		ent.WithDisplayName(fmt.Sprintf("%s schedule %s", resource.DisplayName, scheduleOnCall)),
		ent.WithDescription(fmt.Sprintf("%s PagerDuty schedule %s", resource.DisplayName, scheduleOnCall)),
	}

	rv = append(
		rv,
		ent.NewAssignmentEntitlement(resource, scheduleMember, memberEntitlementOptions...),
		ent.NewAssignmentEntitlement(resource, scheduleOnCall, oncallEntitlementOptions...),
	)

	return rv, "", nil, nil
}

func (s *scheduleResourceType) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	// parse resource profile to get schedule members (users or teams) and grant them the member entitlement
	groupTrait, err := rs.GetGroupTrait(resource)
	if err != nil {
		return nil, "", nil, err
	}

	users, ok := getProfileStringArray(groupTrait.Profile, "schedule_users")
	if !ok {
		return nil, "", nil, fmt.Errorf("pagerduty-connector: failed to get schedule users")
	}

	teams, ok := getProfileStringArray(groupTrait.Profile, "schedule_teams")
	if !ok {
		return nil, "", nil, fmt.Errorf("pagerduty-connector: failed to get schedule teams")
	}

	var rv []*v2.Grant
	for _, u := range users {
		rv = append(rv, grant.NewGrant(
			resource,
			scheduleMember,
			&v2.ResourceId{
				ResourceType: resourceTypeUser.Id,
				Resource:     u,
			},
		))
	}

	for _, t := range teams {
		rv = append(rv, grant.NewGrant(
			resource,
			scheduleMember,
			&v2.ResourceId{
				ResourceType: resourceTypeTeam.Id,
				Resource:     t,
			},
			grant.WithAnnotation(
				&v2.GrantExpandable{
					EntitlementIds: []string{fmt.Sprintf("team:%s:%s", t, scheduleMember)},
				},
			),
		))
	}

	// Only UTC format is supported by PagerDuty
	now := time.Now().UTC()
	hourFromNow := now.Add(time.Hour)

	// go through all users on call for this schedule resource and grant them the on-call entitlement
	usersResponse, err := s.client.ListOnCallUsersWithContext(
		ctx,
		resource.Id.Resource,
		pagerduty.ListOnCallUsersOptions{
			Since: now.Format(time.RFC3339),
			Until: hourFromNow.Format(time.RFC3339),
		},
	)
	if err != nil {
		return nil, "", nil, fmt.Errorf("pagerduty-connector: failed to list on-call users: %w", err)
	}

	for _, user := range usersResponse {
		rv = append(rv, grant.NewGrant(
			resource,
			scheduleOnCall,
			&v2.ResourceId{
				ResourceType: resourceTypeUser.Id,
				Resource:     user.ID,
			},
		))
	}

	return rv, "", nil, nil
}

func scheduleBuilder(client *pagerduty.Client) *scheduleResourceType {
	return &scheduleResourceType{
		resourceType: resourceTypeSchedule,
		client:       client,
	}
}
