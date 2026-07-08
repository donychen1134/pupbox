package dog

import (
	"strings"
	"time"
)

type Activity struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Prompt   string `json:"prompt"`
	Reply    string `json:"reply,omitempty"`
	Category string `json:"category"`
	Action   string `json:"action,omitempty"`
}

func Activities() []Activity {
	return []Activity{
		{
			ID:       "story",
			Label:    "故事",
			Prompt:   "豆豆讲故事",
			Reply:    "从前有一只小狗豆豆，找到一颗会发光的小星星。它把星星送回天空，夜晚就亮起来啦。",
			Category: "listen",
			Action:   "glow_soft",
		},
		{
			ID:       "animal_guess",
			Label:    "动物",
			Prompt:   "豆豆猜动物",
			Reply:    "豆豆来猜动物：长耳朵，蹦蹦跳，爱吃胡萝卜。是小兔子。",
			Category: "game",
			Action:   "ear_wiggle",
		},
		{
			ID:       "color_hunt",
			Label:    "颜色",
			Prompt:   "豆豆玩颜色",
			Reply:    "我们找红色。看到红色就拍拍手，豆豆也一起拍。",
			Category: "game",
			Action:   "glow_red",
		},
		{
			ID:       "counting",
			Label:    "数数",
			Prompt:   "豆豆数数",
			Reply:    "豆豆伸出小爪子。一、二、三、四，四只小爪子都在这里。",
			Category: "game",
			Action:   "paw_tap",
		},
		{
			ID:       "clap",
			Label:    "拍拍",
			Prompt:   "豆豆拍拍手",
			Reply:    "豆豆听见你啦，嗯嗯，汪汪。我们拍拍小手，再一起数一、二、三。",
			Category: "movement",
			Action:   "tail_wag",
		},
		{
			ID:       "comfort",
			Label:    "抱抱",
			Prompt:   "豆豆抱抱",
			Reply:    "豆豆轻轻陪着你。你可以抱抱爸爸妈妈，也可以跟豆豆慢慢呼气。",
			Category: "comfort",
			Action:   "slow_breathe",
		},
	}
}

func PlanActivity(text string) (Activity, bool) {
	t := strings.TrimSpace(text)
	if t == "" {
		return Activity{}, false
	}

	switch {
	case containsAny(t, "唐诗", "古诗", "诗"):
		return Activity{
			ID:       "poem",
			Label:    "唐诗",
			Prompt:   "豆豆背唐诗",
			Reply:    "床前明月光，疑是地上霜。小朋友看见月光，会想起远方的家。",
			Category: "listen",
			Action:   "glow_soft",
		}, true
	case containsAny(t, "故事", "讲故事"):
		return byID("story")
	case containsAny(t, "动物", "猜谜", "猜"):
		return byID("animal_guess")
	case containsAny(t, "数数", "数字", "数"):
		return byID("counting")
	case containsAny(t, "颜色", "红色", "蓝色", "黄色", "绿色"):
		return byID("color_hunt")
	case containsAny(t, "害怕", "怕", "想妈妈", "想爸爸", "哭", "抱抱"):
		return byID("comfort")
	case LooksLikeToddlerBabble(t):
		return babbleActivity(), true
	case containsAny(t, "你好", "豆豆", "小狗", "玩", "拍拍"):
		return byID("clap")
	default:
		return Activity{}, false
	}
}

func babbleActivity() Activity {
	activities := []Activity{
		{
			ID:       "clap",
			Label:    "拍拍",
			Prompt:   "豆豆拍拍手",
			Reply:    "汪汪，豆豆听见啦。我们拍拍小手，一、二、三。",
			Category: "movement",
			Action:   "tail_wag",
		},
		{
			ID:       "clap",
			Label:    "拍拍",
			Prompt:   "豆豆拍拍手",
			Reply:    "嗯嗯，豆豆也嗯嗯。我们一起学小狗，小小声汪一下。",
			Category: "movement",
			Action:   "ear_wiggle",
		},
		{
			ID:       "clap",
			Label:    "拍拍",
			Prompt:   "豆豆在这里。我们找一个红色的小东西，好不好？",
			Category: "game",
			Action:   "glow_red",
		},
		{
			ID:       "clap",
			Label:    "拍拍",
			Prompt:   "豆豆摇摇尾巴。你也可以轻轻拍拍手。",
			Category: "movement",
			Action:   "tail_wag",
		},
	}
	return activities[int(time.Now().UnixNano()%int64(len(activities)))]
}

func byID(id string) (Activity, bool) {
	for _, activity := range Activities() {
		if activity.ID == id {
			return activity, true
		}
	}
	return Activity{}, false
}
