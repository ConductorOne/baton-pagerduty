package connector

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/PagerDuty/go-pagerduty"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/resource"
)

type userResourceType struct {
	resourceType *v2.ResourceType
	client       *pagerduty.Client
}

func (u *userResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return u.resourceType
}

// Create a new connector resource for a PagerDuty User.
func userResource(ctx context.Context, user *pagerduty.User) (*v2.Resource, error) {
	names := strings.SplitN(user.Name, " ", 2)
	var firstName, lastName string
	switch len(names) {
	case 1:
		firstName = names[0]
	case 2:
		firstName = names[0]
		lastName = names[1]
	}

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

	usersResponse, err := u.client.ListUsersWithContext(ctx, paginationOpts)
	if err != nil {
		return nil, "", nil, fmt.Errorf("pager-duty-connector: failed to list users: %w", err)
	}

	rv := make([]*v2.Resource, 0, len(usersResponse.Users))
	for _, user := range usersResponse.Users {
		userCopy := user

		ur, err := userResource(ctx, &userCopy)
		if err != nil {
			return nil, "", nil, err
		}

		rv = append(rv, ur)
	}

	if usersResponse.More {
		nextPage := strconv.FormatUint(uint64(page+ResourcesPageSize), 10)
		pageToken, err := bag.NextToken(nextPage)
		if err != nil {
			return nil, "", nil, err
		}

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

func userBuilder(client *pagerduty.Client) *userResourceType {
	return &userResourceType{
		resourceType: resourceTypeUser,
		client:       client,
	}
}
