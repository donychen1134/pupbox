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

func TestInstructionsAreForDirectSpokenReplies(t *testing.T) {
	instructions := Instructions()
	for _, rule := range []string{"直接朗读", "不要使用括号", "先具体回应", "避免连续重复"} {
		if !strings.Contains(instructions, rule) {
			t.Fatalf("instructions missing %q", rule)
		}
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

func TestPlanActivityStripsDogAddressForExplicitCommands(t *testing.T) {
	got, ok := PlanActivity("小狗小狗，给我讲故事")
	if !ok || got.ID != "story" {
		t.Fatalf("expected story activity after dog address, got %#v ok=%v", got, ok)
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

func TestPlanActivityLeavesNaturalConversationForModel(t *testing.T) {
	for _, text := range []string{
		"豆豆，你今天开心吗",
		"我想玩积木",
		"我画了一辆红色汽车",
		"妈妈今天回家了",
		"我写了一首诗",
		"你好豆豆",
	} {
		if activity, ok := PlanActivity(text); ok {
			t.Errorf("PlanActivity(%q) = %#v, want model routing", text, activity)
		}
	}
}

func TestPlanActivityKeepsHighConfidenceLocalRoutes(t *testing.T) {
	tests := []struct {
		text string
		id   string
	}{
		{text: "豆豆背唐诗", id: "poem"},
		{text: "我们来猜动物", id: "animal_guess"},
		{text: "一起数数", id: "counting"},
		{text: "找蓝色", id: "color_hunt"},
		{text: "我有点害怕", id: "comfort"},
		{text: "我们拍拍手", id: "clap"},
	}
	for _, tt := range tests {
		activity, ok := PlanActivity(tt.text)
		if !ok || activity.ID != tt.id {
			t.Errorf("PlanActivity(%q) = %#v ok=%v, want %s", tt.text, activity, ok, tt.id)
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

func TestStoryFollowUpUsesRecentHistory(t *testing.T) {
	history := []Turn{{User: "你给我讲个故事吧", Reply: "从前有一只小狗。"}}
	activity, ok := PlanActivityWithHistory("再讲一个", history)
	if !ok || activity.ID != "story" {
		t.Fatalf("activity = %#v ok=%v, want story", activity, ok)
	}
}

func TestStoryFollowUpWithoutStoryContextUsesModel(t *testing.T) {
	history := []Turn{{User: "唱首歌", Reply: "啦啦啦。"}}
	if activity, ok := PlanActivityWithHistory("再来一个", history); ok {
		t.Fatalf("activity = %#v, want model routing", activity)
	}
}

func TestPresenceActivityRotatesContent(t *testing.T) {
	seen := make(map[string]bool)
	for range 3 {
		activity, ok := PlanActivity("你干啥呢")
		if !ok || activity.ID != "presence" {
			t.Fatalf("unexpected activity: %#v ok=%v", activity, ok)
		}
		seen[activity.Reply] = true
	}
	if len(seen) != 3 {
		t.Fatalf("presence replies did not rotate: %#v", seen)
	}
}

func TestGreetingActivityRotatesContent(t *testing.T) {
	seen := make(map[string]bool)
	for range 3 {
		activity, ok := PlanActivity("你好你好你好")
		if !ok || activity.ID != "greeting" {
			t.Fatalf("unexpected activity: %#v ok=%v", activity, ok)
		}
		seen[activity.Reply] = true
	}
	if len(seen) != 3 {
		t.Fatalf("greeting replies did not rotate: %#v", seen)
	}
}

func TestPrewarmPrioritizesCommonConversationReplies(t *testing.T) {
	first := PrewarmReplies()
	if len(first) > 48 {
		first = first[:48]
	}
	seen := make(map[string]bool, len(first))
	for _, reply := range first {
		seen[reply] = true
	}
	for _, id := range []string{"presence", "greeting", "chat"} {
		for _, reply := range activityReplyVariants[id] {
			if !seen[reply] {
				t.Fatalf("first 48 prewarm replies do not include %s reply %q", id, reply)
			}
		}
	}
}

func TestPlaybackComplaintTakesPriorityOverPresenceActivity(t *testing.T) {
	if activity, ok := PlanActivityWithHistory("你干啥呢？我听不懂你说话，有点卡。", nil); ok {
		t.Fatalf("activity = %#v, want model to simplify the reply", activity)
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

func TestSpeechOnlyReplyRemovesUnsupportedToyInteraction(t *testing.T) {
	got := SpeechOnlyReply("豆豆在这里呢，不跑掉。要摸摸头吗？")
	if got != "豆豆在这里呢，不跑掉。" {
		t.Fatalf("unexpected speech-only reply %q", got)
	}
	if got := SpeechOnlyReply("豆豆摇尾巴。来碰爪子吧。"); got != "豆豆在听你说话呢。" {
		t.Fatalf("unexpected all-removed fallback %q", got)
	}
}
