package dog

import (
	"encoding/json"
	"errors"
	"strings"
)

// SemanticRoute is the single model decision used after deterministic routing.
// An activity route selects reviewed local content; a chat route carries the
// model's final spoken reply so routing never requires a second model call.
type SemanticRoute struct {
	Kind       string `json:"kind"`
	ActivityID string `json:"activity_id,omitempty"`
	Reply      string `json:"reply,omitempty"`
}

func RoutingInstructions() string {
	return Instructions() + "\n\n" + strings.TrimSpace(`

你还要判断孩子此刻是想进入一个明确玩法，还是普通聊天。只输出一个 JSON 对象，不要输出 Markdown 或解释：
{"kind":"activity","activity_id":"animal_guess","reply":""}
或
{"kind":"chat","activity_id":"","reply":"豆豆的最终口语回复"}

activity_id 只能是以下值：
- story：讲故事、听故事
- poem：背古诗或唐诗
- animal_guess：豆豆描述常见动物，由孩子猜答案
- color_hunt：猜颜色、找颜色
- counting：互动数数
- nursery_rhyme：唱原创短童谣、接唱
- sound_game：模仿声音
- adventure：想象旅行或探险
- pretend_play：过家家、开商店、做饭等角色游戏
- magic：想象魔法游戏
- guide：询问豆豆会什么、还能玩什么或想换一种游戏
- comfort：害怕、难过、想要安慰

要求：
- 按语义理解，不依赖固定关键词；口语、省略和语音识别近音词也要结合上下文判断。
- 只有明确想开始上述玩法时才返回 activity；普通话题、分享、提问和玩法中的自由聊天返回 chat。
- 返回 activity 时 reply 必须为空，具体内容由本地受控题库产生。
- 返回 chat 时 reply 就是要直接朗读的最终回复，必须遵守前面的儿童对话规则。
`)
}

func ParseSemanticRoute(raw string) (SemanticRoute, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var route SemanticRoute
	if err := json.Unmarshal([]byte(raw), &route); err != nil {
		return SemanticRoute{}, err
	}
	route.Kind = strings.ToLower(strings.TrimSpace(route.Kind))
	route.ActivityID = strings.ToLower(strings.TrimSpace(route.ActivityID))
	route.Reply = strings.TrimSpace(route.Reply)
	// Some Qwen compatible-mode models put the selected activity ID in kind
	// even when the prompt requests kind=activity. Accept that unambiguous form.
	if isRoutableActivityID(route.Kind) {
		if route.ActivityID == "" {
			route.ActivityID = route.Kind
		}
		route.Kind = "activity"
	}
	switch route.Kind {
	case "activity":
		if route.ActivityID == "" {
			return SemanticRoute{}, errors.New("semantic activity route has no activity_id")
		}
	case "chat":
		if route.Reply == "" {
			return SemanticRoute{}, errors.New("semantic chat route has no reply")
		}
	default:
		return SemanticRoute{}, errors.New("semantic route has invalid kind")
	}
	return route, nil
}

// RoutedActivity resolves only activities that are safe for semantic model
// selection. The model never supplies the spoken activity content.
func RoutedActivity(id string, history []Turn) (Activity, bool) {
	if !isRoutableActivityID(id) {
		return Activity{}, false
	}
	return activityWithHistory(id, history)
}

func isRoutableActivityID(id string) bool {
	switch id {
	case "story", "poem", "animal_guess", "color_hunt", "counting",
		"nursery_rhyme", "sound_game", "adventure", "pretend_play",
		"magic", "guide", "comfort":
		return true
	default:
		return false
	}
}
