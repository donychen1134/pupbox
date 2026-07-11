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
