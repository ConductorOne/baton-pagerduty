package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/PagerDuty/go-pagerduty"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	ctxzap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// Note: a schedule whose profile carries no schedule_users/schedule_teams
// returning an empty rv on 404 is profile truth (no member grants were
// derivable from the profile), not a swallowed error.

// memSyncer is a zapcore.WriteSyncer that records each encoded log line so a
// test can assert the level and message of what the connector actually logged.
type memSyncer struct {
	mu    sync.Mutex
	lines []string
}

func (m *memSyncer) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lines = append(m.lines, string(p))
	return len(p), nil
}

func (m *memSyncer) Sync() error { return nil }

// loggedEntries decodes the recorded JSON lines into (level, message) pairs.
func (m *memSyncer) loggedEntries(t *testing.T) []struct{ Level, Message string } {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]struct{ Level, Message string }, 0, len(m.lines))
	for _, line := range m.lines {
		var e struct {
			Level   string `json:"level"`
			Message string `json:"msg"`
		}
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("unparseable log line %q: %v", line, err)
		}
		out = append(out, struct{ Level, Message string }{e.Level, e.Message})
	}
	return out
}

func debugLoggerCtx(ctx context.Context, m *memSyncer) context.Context {
	encCfg := zap.NewProductionEncoderConfig()
	core := zapcore.NewCore(zapcore.NewJSONEncoder(encCfg), m, zapcore.DebugLevel)
	return ctxzap.ToContext(ctx, zap.New(core))
}

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

			var logs memSyncer
			ctx := debugLoggerCtx(context.Background(), &logs)
			grants, _, _, err := srt.Grants(ctx, resource, nil)

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

			// Criterion 4 (ci-review L1b): on the 404 skip path the connector
			// must log exactly ONE Debug skip line and never an Error/Warn-level
			// entry (the skip returns nil error, so an Error log would be a
			// false alarm). Pre-existing Info lines (e.g. "no teams found")
			// are tolerated; the skip line itself is asserted exactly.
			if tt.wantSkipped {
				entries := logs.loggedEntries(t)
				skips := 0
				for _, e := range entries {
					lvl := strings.ToUpper(e.Level)
					switch lvl {
					case "ERROR", "WARN", "DPANIC", "PANIC", "FATAL":
						t.Errorf("skip path logged %s-level entry: %q", e.Level, e.Message)
					case "DEBUG":
						skips++
						if !strings.Contains(e.Message, "schedule not found") {
							t.Errorf("debug log message %q does not mention schedule-not-found", e.Message)
						}
					}
				}
				if skips != 1 {
					t.Errorf("skip path logged %d DEBUG skip lines, want 1: %+v", skips, entries)
				}
			}
		})
	}
}
