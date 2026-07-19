package dog

import (
	"strings"
	"testing"
)

func TestParseSemanticRoute(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantKind  string
		wantID    string
		wantReply string
		wantErr   bool
	}{
		{name: "activity", raw: `{"kind":"activity","activity_id":"animal_guess","reply":""}`, wantKind: "activity", wantID: "animal_guess"},
		{name: "qwen activity kind", raw: `{"kind":"animal_guess","activity_id":"animal_guess","reply":""}`, wantKind: "activity", wantID: "animal_guess"},
		{name: "chat fence", raw: "```json\n{\"kind\":\"chat\",\"activity_id\":\"\",\"reply\":\"云朵像软软的棉花糖。\"}\n```", wantKind: "chat", wantReply: "云朵像软软的棉花糖。"},
		{name: "missing chat reply", raw: `{"kind":"chat","reply":""}`, wantErr: true},
		{name: "invalid kind", raw: `{"kind":"other","reply":"汪"}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route, err := ParseSemanticRoute(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseSemanticRoute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if route.Kind != tt.wantKind || route.ActivityID != tt.wantID || route.Reply != tt.wantReply {
				t.Fatalf("route = %+v, want kind=%q id=%q reply=%q", route, tt.wantKind, tt.wantID, tt.wantReply)
			}
		})
	}
}

func TestRoutedActivityUsesReviewedLocalContent(t *testing.T) {
	activity, ok := RoutedActivity("animal_guess", nil)
	if !ok || activity.ID != "animal_guess" {
		t.Fatalf("activity = %+v, ok = %v", activity, ok)
	}
	if !strings.Contains(activity.Reply, "你来猜") && !strings.Contains(activity.Reply, "是小") {
		t.Fatalf("animal activity does not ask the child to guess: %q", activity.Reply)
	}
	if _, ok := RoutedActivity("farewell", nil); ok {
		t.Fatal("farewell must not be selectable by semantic activity routing")
	}
}
