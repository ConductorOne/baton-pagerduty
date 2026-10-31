package connector

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/PagerDuty/go-pagerduty"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// Note: a schedule whose profile carries no schedule_users/schedule_teams
// returning an empty rv on 404 is profile truth (no member grants were
// derivable from the profile), not a swallowed error.

func scheduleGrantTestResource(t *testing.T, profile map[string]interface{}) *v2.Resource {
	t.Helper()
	res, err := rs.NewGroupResource(
		"test-schedule",
		resourceTypeSchedule,
		"PSCHEDULE1",
		[]rs.GroupTraitOption{},
		rs.WithResourceProfile(profile),
	)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestScheduleResourceType_Grants(t *testing.T) {
	profile, err := structpb.NewStruct(map[string]interface{}{
		"schedule_users": []interface{}{"UUSER1"},
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		statusCode  int
		body        string
		wantErrCode codes.Code
		wantSkipped bool
		wantMembers int
	}{
		{
			name:        "404 schedule not found: skip on-call, keep member grants",
			statusCode:  http.StatusNotFound,
			body:        `{"error":{"code":2100,"message":"Not Found","errors":["Schedule Not Found"]}}`,
			wantSkipped: true,
			wantMembers: 1,
		},
		{
			name:        "500: error surfaces as Unavailable",
			statusCode:  http.StatusInternalServerError,
			body:        `{"error":{"code":5000,"message":"Internal Server Error","errors":["Something went wrong"]}}`,
			wantErrCode: codes.Unavailable,
		},
		{
			name:        "401: error surfaces as Unauthenticated",
			statusCode:  http.StatusUnauthorized,
			body:        `{"error":{"code":2001,"message":"Unauthorized","errors":["Invalid token"]}}`,
			wantErrCode: codes.Unauthenticated,
		},
		{
			name:        "403: error surfaces as PermissionDenied",
			statusCode:  http.StatusForbidden,
			body:        `{"error":{"code":2003,"message":"Forbidden","errors":["You are not allowed to access this resource"]}}`,
			wantErrCode: codes.PermissionDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newMockServer(t, tt.statusCode, tt.body)
			defer srv.Close()

			client := pagerduty.NewClient("fake-token", pagerduty.WithAPIEndpoint(srv.URL))
			srt := scheduleBuilder(client)

			resource := scheduleGrantTestResource(t, profile.AsMap())

			grants, _, _, err := srt.Grants(context.Background(), resource, nil)

			if tt.wantErrCode != codes.OK {
				if err == nil {
					t.Fatalf("expected error, got nil with grants: %v", grants)
				}
				st, ok := status.FromError(err)
				if !ok {
					t.Fatalf("expected gRPC status error, got: %v", err)
				}
				if st.Code() != tt.wantErrCode {
					t.Errorf("got gRPC code %v, want %v", st.Code(), tt.wantErrCode)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected nil error, got: %v", err)
			}

			members := 0
			oncall := false
			for _, g := range grants {
				entID := g.GetEntitlement().GetId()
				if strings.HasSuffix(entID, fmt.Sprintf(":%s", scheduleMember)) {
					members++
				}
				if strings.HasSuffix(entID, fmt.Sprintf(":%s", scheduleOnCall)) {
					oncall = true
				}
			}

			if members != tt.wantMembers {
				t.Errorf("got %d member grants, want %d (grants: %v)", members, tt.wantMembers, grants)
			}
			if tt.wantSkipped && oncall {
				t.Error("unexpected on-call grant on the 404 skip path")
			}
		})
	}
}