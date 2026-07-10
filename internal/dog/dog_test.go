package dog

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCheckSafetyDanger(t *testing.T) {
	got := CheckSafety("我想玩插座")
	if !got.Triggered || got.Category != "danger" {
		t.Fatalf("expected danger safety result, got %#v", got)
	}
}

func TestMockReplyPoem(t *testing.T) {
	got := MockReply("豆豆背唐诗")
	for _, reply := range activityReplyVariants["poem"] {
		if got == reply {
			return
		}
	}
	t.Fatalf("expected a reviewed poem reply, got %q", got)
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
		{text: "我们玩声音游戏", id: "sound_game"},
	}
	for _, tt := range tests {
		got, ok := PlanActivity(tt.text)
		if !ok || got.ID != tt.id {
			t.Fatalf("PlanActivity(%q) = %#v ok=%v, want %s", tt.text, got, ok, tt.id)
		}
	}
}

func TestActivityReplyVariantsAreRichAndShort(t *testing.T) {
	for id, replies := range activityReplyVariants {
		if len(replies) < 5 {
			t.Fatalf("activity %q has only %d replies", id, len(replies))
		}
		seen := make(map[string]bool, len(replies))
		for _, reply := range replies {
			if strings.TrimSpace(reply) == "" {
				t.Fatalf("activity %q has an empty reply", id)
			}
			if utf8.RuneCountInString(reply) > 90 {
				t.Fatalf("activity %q reply is too long: %q", id, reply)
			}
			if seen[reply] {
				t.Fatalf("activity %q has duplicate reply: %q", id, reply)
			}
			seen[reply] = true
		}
	}
}

func TestStoryActivityRotatesContent(t *testing.T) {
	seen := make(map[string]bool)
	for range 3 {
		activity, ok := PlanActivity("再讲一个故事")
		if !ok || activity.ID != "story" {
			t.Fatalf("unexpected activity: %#v ok=%v", activity, ok)
		}
		seen[activity.Reply] = true
	}
	if len(seen) != 3 {
		t.Fatalf("story replies did not rotate: %#v", seen)
	}
}

func TestPrewarmRepliesAreUniqueAndCoverReviewedActivities(t *testing.T) {
	replies := PrewarmReplies()
	if len(replies) < 50 {
		t.Fatalf("prewarm replies = %d, want at least 50", len(replies))
	}
	seen := make(map[string]bool, len(replies))
	for _, reply := range replies {
		if seen[reply] {
			t.Fatalf("duplicate prewarm reply: %q", reply)
		}
		seen[reply] = true
	}
	for _, id := range []string{"story", "poem", "animal_guess"} {
		for _, reply := range activityReplyVariants[id] {
			if !seen[reply] {
				t.Fatalf("prewarm replies do not include %s reply %q", id, reply)
			}
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
