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

func TestCheckSafetyWellCoverDanger(t *testing.T) {
	got := CheckSafety("小兔子掉井盖里了")
	if !got.Triggered || got.Category != "danger" {
		t.Fatalf("expected well-cover danger result, got %#v", got)
	}
}

func TestCheckSafetyDistinguishesFireFromTrain(t *testing.T) {
	if got := CheckSafety("我们坐火车去旅行"); got.Triggered {
		t.Fatalf("train unexpectedly triggered safety: %#v", got)
	}
	if got := CheckSafety("我想玩火"); !got.Triggered || got.Category != "danger" {
		t.Fatalf("playing with fire did not trigger danger: %#v", got)
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
		{text: "豆豆唱一首童谣", id: "nursery_rhyme"},
		{text: "你还会做什么呀", id: "guide"},
		{text: "我们坐火车去旅行", id: "adventure"},
		{text: "豆豆一起过家家", id: "pretend_play"},
		{text: "玩魔法变变变", id: "magic"},
	}
	for _, tt := range tests {
		got, ok := PlanActivity(tt.text)
		if !ok || got.ID != tt.id {
			t.Fatalf("PlanActivity(%q) = %#v ok=%v, want %s", tt.text, got, ok, tt.id)
		}
	}
}

func TestCapabilityQuestionTakesPriorityOverStoryKeyword(t *testing.T) {
	activity, ok := PlanActivity("你还会干啥？你会讲故事，还会做什么吗？")
	if !ok || activity.ID != "guide" {
		t.Fatalf("activity = %#v ok=%v, want guide", activity, ok)
	}
}

func TestCallingDogByNameOffersActivityGuide(t *testing.T) {
	activity, ok := PlanActivity("豆豆")
	if !ok || activity.ID != "guide" {
		t.Fatalf("activity = %#v ok=%v, want guide", activity, ok)
	}
}

func TestNegatedFearDoesNotRouteToComfort(t *testing.T) {
	if activity, ok := PlanActivity("我不害怕"); ok {
		t.Fatalf("activity = %#v, want model routing", activity)
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

func TestActivityContinuationRepliesAreShort(t *testing.T) {
	for id, replies := range activityContinuationReplies {
		for _, reply := range replies {
			if strings.TrimSpace(reply) == "" || utf8.RuneCountInString(reply) > 90 {
				t.Fatalf("activity %q continuation is invalid: %q", id, reply)
			}
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

func TestStoryActivityWithHistoryAvoidsRecentReplies(t *testing.T) {
	stories := activityReplyVariants["story"]
	history := make([]Turn, 0, len(stories)-1)
	for _, reply := range stories[:len(stories)-1] {
		history = append(history, Turn{User: "讲故事", Reply: reply, ActivityID: "story"})
	}

	activity, ok := PlanActivityWithHistory("再讲一个故事", history)
	if !ok || activity.ID != "story" {
		t.Fatalf("activity = %#v ok=%v, want story", activity, ok)
	}
	if activity.Reply != stories[len(stories)-1] {
		t.Fatalf("reply = %q, want only unused story %q", activity.Reply, stories[len(stories)-1])
	}
}

func TestNaturalStoryRequestRoutesToStory(t *testing.T) {
	activity, ok := PlanActivity("你给我讲一个故事吧")
	if !ok || activity.ID != "story" {
		t.Fatalf("activity = %#v ok=%v, want story", activity, ok)
	}
}

func TestBareDollDoesNotOverrideConversation(t *testing.T) {
	if activity, ok := PlanActivity("娃娃"); ok {
		t.Fatalf("activity = %#v, want model routing", activity)
	}
	activity, ok := PlanActivity("我们玩娃娃吧")
	if !ok || activity.ID != "pretend_play" {
		t.Fatalf("activity = %#v ok=%v, want pretend_play", activity, ok)
	}
}

func TestStoryFollowUpUsesRecentHistory(t *testing.T) {
	history := []Turn{{User: "你给我讲个故事吧", Reply: "从前有一只小狗。", ActivityID: "story"}}
	activity, ok := PlanActivityWithHistory("再讲一个", history)
	if !ok || activity.ID != "story" {
		t.Fatalf("activity = %#v ok=%v, want story", activity, ok)
	}
}

func TestStoryAffirmationContinuesPendingOffer(t *testing.T) {
	history := []Turn{{User: "你想听故事吗", Reply: "豆豆再讲一个。要听吗？"}}
	activity, ok := PlanActivityWithHistory("要听啊", history)
	if !ok || activity.ID != "story" {
		t.Fatalf("activity = %#v ok=%v, want story", activity, ok)
	}
}

func TestStoryAffirmationContinuesRecentStory(t *testing.T) {
	history := []Turn{{User: "讲个故事", Reply: "从前有一只小狗。", ActivityID: "story"}}
	activity, ok := PlanActivityWithHistory("想听", history)
	if !ok || activity.ID != "story" {
		t.Fatalf("activity = %#v ok=%v, want story", activity, ok)
	}
}

func TestStoryMisrecognitionContinuesRecentStory(t *testing.T) {
	history := []Turn{{User: "讲故事", Reply: "从前有一只小狗。", ActivityID: "story"}}
	activity, ok := PlanActivityWithHistory("再亲一个吧", history)
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

func TestRepeatedPresenceQuestionOffersActivityGuide(t *testing.T) {
	history := []Turn{{User: "你干啥呢", Reply: "豆豆在想彩虹。", ActivityID: "presence"}}
	activity, ok := PlanActivityWithHistory("你干啥呢", history)
	if !ok || activity.ID != "guide" {
		t.Fatalf("activity = %#v ok=%v, want guide", activity, ok)
	}
}

func TestDidNotHearBirdSongUsesModel(t *testing.T) {
	for _, planner := range []func(string) (Activity, bool){
		PlanActivity,
		func(text string) (Activity, bool) { return PlanActivityWithHistory(text, nil) },
	} {
		if activity, ok := planner("我没听见小鸟唱歌"); ok {
			t.Fatalf("activity = %#v, want model routing", activity)
		}
	}
}

func TestNurseryRhymeContinuesChildSound(t *testing.T) {
	history := []Turn{{User: "唱童谣", Reply: activityReplyVariants["nursery_rhyme"][0], ActivityID: "nursery_rhyme"}}
	activity, ok := PlanActivityWithHistory("滴答滴答", history)
	if !ok || activity.ID != "nursery_rhyme" || !strings.Contains(activity.Reply, "小花喝水") {
		t.Fatalf("activity = %#v ok=%v, want nursery rhyme continuation", activity, ok)
	}
}

func TestNurseryRhymeFollowUpStartsAnotherRhyme(t *testing.T) {
	history := []Turn{{User: "唱童谣", Reply: activityReplyVariants["nursery_rhyme"][0], ActivityID: "nursery_rhyme"}}
	activity, ok := PlanActivityWithHistory("再唱一个", history)
	if !ok || activity.ID != "nursery_rhyme" || activity.Reply == "" {
		t.Fatalf("activity = %#v ok=%v, want another nursery rhyme", activity, ok)
	}
}

func TestAdventureContinuesAcrossShortChoiceTurns(t *testing.T) {
	history := []Turn{{User: "我们去旅行", Reply: activityReplyVariants["adventure"][0], ActivityID: "adventure"}}
	first, ok := PlanActivityWithHistory("海边", history)
	if !ok || first.ID != "adventure" || !strings.Contains(first.Reply, "海边到啦") {
		t.Fatalf("first continuation = %#v ok=%v", first, ok)
	}
	history = append(history, Turn{User: "海边", Reply: first.Reply, ActivityID: first.ID})
	second, ok := PlanActivityWithHistory("贝壳", history)
	if !ok || second.ID != "adventure" || !strings.Contains(second.Reply, "三只贝壳") {
		t.Fatalf("second continuation = %#v ok=%v", second, ok)
	}
}

func TestPretendAndMagicContinueShortChoices(t *testing.T) {
	tests := []struct {
		activityID string
		previous   string
		text       string
		want       string
	}{
		{activityID: "pretend_play", previous: activityReplyVariants["pretend_play"][0], text: "草莓", want: "草莓甜甜的"},
		{activityID: "magic", previous: activityReplyVariants["magic"][0], text: "下花瓣", want: "花瓣轻轻"},
	}
	for _, tt := range tests {
		history := []Turn{{User: "开始", Reply: tt.previous, ActivityID: tt.activityID}}
		activity, ok := PlanActivityWithHistory(tt.text, history)
		if !ok || activity.ID != tt.activityID || !strings.Contains(activity.Reply, tt.want) {
			t.Errorf("%s continuation = %#v ok=%v", tt.activityID, activity, ok)
		}
	}
}

func TestAnimalGuessContinuesWithNextRound(t *testing.T) {
	history := []Turn{{User: "猜动物", Reply: activityReplyVariants["animal_guess"][0], ActivityID: "animal_guess"}}
	activity, ok := PlanActivityWithHistory("小兔子", history)
	if !ok || activity.ID != "animal_guess" || !strings.Contains(activity.Reply, "猜对啦") || !strings.Contains(activity.Reply, "小猫还是小狗") {
		t.Fatalf("animal continuation = %#v ok=%v", activity, ok)
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
	if len(first) > 160 {
		first = first[:160]
	}
	seen := make(map[string]bool, len(first))
	for _, reply := range first {
		seen[reply] = true
	}
	for _, id := range []string{"presence", "greeting", "chat", "story", "adventure", "pretend_play", "magic"} {
		for _, reply := range activityReplyVariants[id] {
			if !seen[reply] {
				t.Fatalf("first 160 prewarm replies do not include %s reply %q", id, reply)
			}
		}
	}
	for id, replies := range activityContinuationReplies {
		for _, reply := range replies {
			if !seen[reply] {
				t.Fatalf("first 160 prewarm replies do not include %s continuation %q", id, reply)
			}
		}
	}
}

func TestPlaybackComplaintTakesPriorityOverPresenceActivity(t *testing.T) {
	if activity, ok := PlanActivityWithHistory("你干啥呢？我听不懂你说话，有点卡。", nil); ok {
		t.Fatalf("activity = %#v, want model to simplify the reply", activity)
	}
}

func TestLongPresenceUtteranceKeepsSpecificTopicForModel(t *testing.T) {
	text := "豆豆，你干啥呢？你怎么又不和我跳街舞了？"
	if activity, ok := PlanActivityWithHistory(text, nil); ok {
		t.Fatalf("activity = %#v, want model to continue the dance topic", activity)
	}
}

func TestSingACompleteSongRoutesToNurseryRhyme(t *testing.T) {
	activity, ok := PlanActivity("豆豆，你给橙子唱个歌吧")
	if !ok || activity.ID != "nursery_rhyme" {
		t.Fatalf("activity = %#v ok=%v, want nursery_rhyme", activity, ok)
	}
}

func TestPrewarmRepliesAreUniqueAndCoverReviewedActivities(t *testing.T) {
	replies := PrewarmReplies()
	if len(replies) > 192 {
		t.Fatalf("prewarm replies = %d, exceed default startup limit 192", len(replies))
	}
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
	for _, scene := range surpriseScenes {
		for _, reply := range scene.cards {
			if !seen[reply] {
				t.Fatalf("prewarm replies do not include %s surprise %q", scene.id, reply)
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

func TestBabbleRemembersRecentCountingRejection(t *testing.T) {
	history := []Turn{{User: "你别数一二三了吧", Reply: "好，不数啦。"}}
	activity, ok := PlanActivityWithHistory("汪汪", history)
	if !ok || activity.ID != "clap" || strings.Contains(activity.Reply, "一二三") || !strings.Contains(activity.Reply, "不数数") {
		t.Fatalf("activity = %#v ok=%v", activity, ok)
	}
}

func TestSceneSurpriseNeedsEstablishedSceneAndInvitesEasyReply(t *testing.T) {
	history := []Turn{
		{User: "我们骑小毛驴", Reply: "小毛驴出发啦。"},
		{User: "快一点", Reply: "哒哒哒，跑上小山坡。"},
		{User: "看见花了", Reply: "山坡上有好多小花。"},
	}
	activity, ok := PlanSceneSurprise("还有", history)
	if !ok || (activity.ID != "surprise_animal" && activity.ID != "surprise_travel") {
		t.Fatalf("activity = %#v ok=%v, want scene-matched surprise", activity, ok)
	}
	if !strings.Contains(activity.Reply, "还是") {
		t.Fatalf("surprise does not provide an easy choice: %q", activity.Reply)
	}
}

func TestSceneSurpriseDoesNotInterruptQuestionOrCooldown(t *testing.T) {
	history := []Turn{
		{User: "我们吃冰激凌", Reply: "草莓冰激凌甜甜的。"},
		{User: "我还要", Reply: "再加一个小球。"},
		{User: "好呀", Reply: "冰激凌做好啦。"},
	}
	if activity, ok := PlanSceneSurprise("你喜欢什么味道？", history); ok {
		t.Fatalf("question was interrupted by %#v", activity)
	}
	history = append(history, Turn{User: "还有", Reply: "一颗饼干唱起歌。", ActivityID: "surprise_food"})
	if activity, ok := PlanSceneSurprise("草莓", history); ok {
		t.Fatalf("surprise cooldown ignored: %#v", activity)
	}
}

func TestSceneSurpriseStaysOffUnrelatedShortConversation(t *testing.T) {
	history := []Turn{
		{User: "你好", Reply: "你好呀。"},
		{User: "你是谁", Reply: "我是豆豆。"},
		{User: "好吧", Reply: "豆豆陪你。"},
	}
	if activity, ok := PlanSceneSurprise("嗯", history); ok {
		t.Fatalf("unrelated chat got surprise: %#v", activity)
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

func TestSpeechOnlyReplyRemovesVisualPunctuation(t *testing.T) {
	got := SpeechOnlyReply("豆豆也“啊呸”一下~")
	if got != "豆豆也啊呸一下。" {
		t.Fatalf("unexpected spoken reply %q", got)
	}
}

func TestClarificationReplyRepeatsPreviousIdea(t *testing.T) {
	history := []Turn{{User: "你会跳什么舞", Reply: "豆豆会跳汪汪舞，恰恰恰。"}}
	got, ok := ClarificationReply("你说啥呢？我听不懂。", history)
	if !ok || !strings.Contains(got, "汪汪舞") {
		t.Fatalf("clarification = %q ok=%v", got, ok)
	}
	if utf8.RuneCountInString(got) > 28 {
		t.Fatalf("clarification is too long: %q", got)
	}
}

func TestClarificationReplyDoesNotTreatAnimalChatAsStory(t *testing.T) {
	history := []Turn{{
		User:  "小鸭子嘎嘎嘎",
		Reply: "豆豆也学小鸭子叫，嘎嘎嘎。你还能想到别的小动物叫声吗？",
	}}
	got, ok := ClarificationReply("你说啥呢？", history)
	if !ok || !strings.Contains(got, "学小鸭子叫") || strings.Contains(got, "小故事") {
		t.Fatalf("clarification = %q ok=%v", got, ok)
	}
}
