package dog

import "testing"

func TestCheckSafetyDanger(t *testing.T) {
	got := CheckSafety("我想玩插座")
	if !got.Triggered || got.Category != "danger" {
		t.Fatalf("expected danger safety result, got %#v", got)
	}
}

func TestMockReplyPoem(t *testing.T) {
	got := MockReply("豆豆背唐诗")
	if !containsAny(got, "床前明月光") {
		t.Fatalf("expected poem reply, got %q", got)
	}
}

func TestPlanActivityStory(t *testing.T) {
	got, ok := PlanActivity("豆豆讲故事")
	if !ok || got.ID != "story" {
		t.Fatalf("expected story activity, got %#v ok=%v", got, ok)
	}
}

func TestPlanActivityNormalizesToddlerIntentText(t *testing.T) {
	tests := []struct {
		text string
		id   string
	}{
		{text: "讲个古是吧", id: "story"},
		{text: "我要听小狗故事", id: "story"},
		{text: "一二三", id: "counting"},
		{text: "找 红 色", id: "color_hunt"},
		{text: "旺旺", id: "clap"},
	}
	for _, tt := range tests {
		got, ok := PlanActivity(tt.text)
		if !ok || got.ID != tt.id {
			t.Fatalf("PlanActivity(%q) = %#v ok=%v, want %s", tt.text, got, ok, tt.id)
		}
	}
}

func TestLooksLikeToddlerBabble(t *testing.T) {
	if !LooksLikeToddlerBabble("嗯嗯") {
		t.Fatal("expected toddler babble")
	}
	if LooksLikeToddlerBabble("唐诗") {
		t.Fatal("did not expect meaningful phrase to be treated as babble")
	}
}

func TestBabblePlansClapActivity(t *testing.T) {
	got, ok := PlanActivity("嗯嗯")
	if !ok || got.ID != "clap" {
		t.Fatalf("expected clap activity, got %#v ok=%v", got, ok)
	}
	if got.Reply == "" {
		t.Fatalf("expected babble activity reply, got %#v", got)
	}
}

func TestBabbleActivitiesAllHaveReplies(t *testing.T) {
	for _, activity := range babbleActivities() {
		if activity.Reply == "" {
			t.Fatalf("activity %q has empty reply: %#v", activity.ID, activity)
		}
	}
}

func TestClampReply(t *testing.T) {
	got := ClampReply("一二三四五", 3)
	if got != "一二三。" {
		t.Fatalf("unexpected clamped reply %q", got)
	}
}
