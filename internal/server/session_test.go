package server

import (
	"strings"
	"testing"

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
