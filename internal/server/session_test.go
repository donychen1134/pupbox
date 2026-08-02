package server

import (
	"strings"
	"testing"
	"time"

	"github.com/donychen1134/pupbox/internal/dog"
)

func TestContextualInputFlagsRepeatedQuestion(t *testing.T) {
	history := []dog.Turn{
		{User: "你干啥呢？", Reply: "豆豆在等你。"},
		{User: "你干啥呢", Reply: "豆豆在玩。"},
	}
	input := contextualInput(history, "你干啥呢？")
	if !strings.Contains(input, "已经问过这句话 2 次") || !strings.Contains(input, "不要重复之前豆豆的回答") {
		t.Fatalf("repeat guidance missing from input: %q", input)
	}
}

func TestSessionStoreKeepsActivityState(t *testing.T) {
	store := NewSessionStore(2, 4, time.Minute)
	store.Append("session-1234", "猜动物", "请猜一猜", &dog.Activity{
		ID:    "animal_guess",
		State: "animal_guess:3",
	})
	history := store.History("session-1234")
	if len(history) != 1 || history[0].ActivityState != "animal_guess:3" {
		t.Fatalf("session history = %#v", history)
	}
}

func TestContextualInputIncludesActivityState(t *testing.T) {
	history := []dog.Turn{{User: "讲个故事", Reply: "从前有一只小狗。", ActivityID: "story"}}
	input := contextualInput(history, "再来一个")
	if !strings.Contains(input, "正在进行story活动") {
		t.Fatalf("activity state missing from input: %q", input)
	}
}

func TestContextualInputKeepsVeryShortSpeechInCurrentScene(t *testing.T) {
	history := []dog.Turn{{User: "我们骑小毛驴", Reply: "小毛驴跑上山坡啦。"}}
	input := contextualInput(history, "这棋")
	if !strings.Contains(input, "语音识别偏差") || !strings.Contains(input, "不要仅凭这几个字突然建立无关的新话题") {
		t.Fatalf("short speech guidance missing from input: %q", input)
	}
}

func TestSessionHistoryExpiresOldSceneAndKeepsFourTurns(t *testing.T) {
	store := NewSessionStore(2, 10, time.Hour)
	id := "session-1234"
	turns := []dog.Turn{{User: "旧旅行", Reply: "去山坡", OccurredAt: time.Now().Add(-2 * time.Minute)}}
	for i := 0; i < 5; i++ {
		turns = append(turns, dog.Turn{User: "新话题", Reply: "新回答", OccurredAt: time.Now()})
	}
	store.sessions[id] = sessionMemory{turns: turns, updatedAt: time.Now()}
	history := store.History(id)
	if len(history) != 4 {
		t.Fatalf("history length = %d, want 4", len(history))
	}
	for _, turn := range history {
		if turn.User == "旧旅行" {
			t.Fatal("expired scene remained in context")
		}
	}
}
