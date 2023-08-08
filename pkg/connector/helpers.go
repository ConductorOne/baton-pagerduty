package connector

import (
	"fmt"
	"strconv"
	"strings"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const ResourcesPageSize = 50

var titleCaser = cases.Title(language.English)

func handleNextPage(bag *pagination.Bag, page uint) (string, error) {
	nextPage := strconv.FormatUint(uint64(page), 10)
	pageToken, err := bag.NextToken(nextPage)
	if err != nil {
		return "", err
	}

	return pageToken, nil
}

func parsePageToken(i string, resourceID *v2.ResourceId) (*pagination.Bag, uint, error) {
	b := &pagination.Bag{}
	err := b.Unmarshal(i)
	if err != nil {
		return nil, 0, err
	}

	if b.Current() == nil {
		b.Push(pagination.PageState{
			ResourceTypeID: resourceID.ResourceType,
			ResourceID:     resourceID.Resource,
		})
	}

	page, err := convertPageToken(b.PageToken())
	if err != nil {
		return nil, 0, err
	}

	return b, page, nil
}

// convertPageToken converts a string token into an int.
func convertPageToken(token string) (uint, error) {
	if token == "" {
		return 0, nil
	}

	page, err := strconv.ParseUint(token, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("failed to parse page token: %w", err)
	}

	return uint(page), nil
}

// Id of entitlement has following format <resource_type>:<resource_id>:<entitlement_id>
// extract resource_id and entitlement_id from it.
func extractResourceIds(fullId string) (string, string, error) {
	idParts := strings.Split(fullId, ":")

	if len(idParts) != 3 {
		return "", "", fmt.Errorf("invalid resource id: %s", fullId)
	}

	return idParts[1], idParts[2], nil
}
