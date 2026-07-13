package dog

import (
	"crypto/rand"
	"math/big"
	"strings"
	"unicode/utf8"
)

const surpriseActivityPrefix = "surprise_"

type surpriseScene struct {
	id       string
	label    string
	keywords []string
	cards    []string
}

var surpriseScenes = []surpriseScene{
	{
		id:       "animal",
		label:    "小动物惊喜",
		keywords: []string{"小狗", "小猫", "兔子", "小兔", "小熊", "小鹿", "小毛驴", "毛驴", "小马", "动物", "汪汪", "喵喵"},
		cards: []string{
			"咦，小动物走过的地方留下了彩虹脚印。我们踩红色，还是黄色？",
			"听，草丛里传来特别小的一声你好。你猜是小兔，还是小猫？",
		},
	},
	{
		id:       "travel",
		label:    "旅行惊喜",
		keywords: []string{"旅行", "出发", "火车", "小船", "汽车", "骑", "山坡", "山洞", "森林", "海边", "小岛", "路上", "回家"},
		cards: []string{
			"前面的路忽然变成了会唱歌的彩虹桥。我们走红色路，还是蓝色路？",
			"叮咚，一个小包裹落在路边，里面发出沙沙声。我们说打开，还是先说你好？",
		},
	},
	{
		id:       "food",
		label:    "食物惊喜",
		keywords: []string{"吃", "喝", "冰激凌", "冰淇淋", "蛋糕", "饼干", "草莓", "苹果", "香蕉", "面包", "糖果", "做饭"},
		cards: []string{
			"冰激凌里传来一个小小的声音，说也想尝一口。我们分它草莓味，还是牛奶味？",
			"咕噜，盘子里的小饼干竟然唱起啦啦啦。你用汪汪，还是喵喵和它合唱？",
		},
	},
	{
		id:       "bubble",
		label:    "泡泡惊喜",
		keywords: []string{"泡泡", "洗澡", "水花", "下雨", "雨点", "小河", "池塘", "游泳"},
		cards: []string{
			"有一颗泡泡没有破，里面藏着一小片彩虹。我们轻轻吹，还是对它说你好？",
			"啪，一个泡泡变成了小铃铛，叮铃铃。你想让它再叮一声，还是唱啦啦啦？",
		},
	},
	{
		id:       "magic",
		label:    "魔法惊喜",
		keywords: []string{"魔法", "变身", "变成", "星星", "月亮", "云朵", "彩虹", "飞", "公主"},
		cards: []string{
			"魔法袋里跳出一颗会打喷嚏的小星星，啊嚏。你想让它变大，还是变小？",
			"云朵忽然说它忘了自己的颜色。我们送它粉色，还是蓝色？",
		},
	},
}

// PlanSceneSurprise occasionally advances an established scene with a reviewed,
// low-pressure prompt. Explicit requests and questions continue through normal routing.
func PlanSceneSurprise(text string, history []Turn) (Activity, bool) {
	if len(history) < 3 || !surpriseEligibleText(text) {
		return Activity{}, false
	}
	if surpriseCount(history) >= 2 || turnsSinceSurprise(history) < 3 {
		return Activity{}, false
	}

	scene, ok := detectSurpriseScene(text, history)
	if !ok {
		return Activity{}, false
	}
	reply := randomSurpriseCard(scene.cards, history)
	return Activity{
		ID:       surpriseActivityPrefix + scene.id,
		Label:    scene.label,
		Prompt:   "豆豆继续当前场景",
		Reply:    reply,
		Category: "imagination",
	}, true
}

func surpriseEligibleText(text string) bool {
	normalized := stripDogAddress(normalizeToddlerIntentText(text))
	if normalized == "" || utf8.RuneCountInString(normalized) > 14 {
		return false
	}
	return !containsAny(normalized,
		"为什么", "怎么", "什么", "哪里", "哪个", "谁", "是不是", "能不能", "会不会",
		"讲", "唱", "告诉", "再说", "听不懂", "没听懂", "不清楚", "不要", "别", "不想", "没有",
	)
}

func detectSurpriseScene(text string, history []Turn) (surpriseScene, bool) {
	current := normalizeToddlerIntentText(text)
	bestIndex, bestScore := -1, 0
	for index, scene := range surpriseScenes {
		score := keywordScore(current, scene.keywords) * 4
		for i := len(history) - 1; i >= 0 && i >= len(history)-4; i-- {
			weight := len(history) - i
			if weight > 3 {
				weight = 3
			}
			weight = 4 - weight
			score += keywordScore(normalizeToddlerIntentText(history[i].User+history[i].Reply), scene.keywords) * weight
		}
		if score > bestScore {
			bestIndex, bestScore = index, score
		}
	}
	if bestIndex < 0 || bestScore < 2 {
		return surpriseScene{}, false
	}
	return surpriseScenes[bestIndex], true
}

func keywordScore(text string, keywords []string) int {
	score := 0
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			score++
		}
	}
	return score
}

func surpriseCount(history []Turn) int {
	count := 0
	for _, turn := range history {
		if strings.HasPrefix(turn.ActivityID, surpriseActivityPrefix) {
			count++
		}
	}
	return count
}

func turnsSinceSurprise(history []Turn) int {
	for i := len(history) - 1; i >= 0; i-- {
		if strings.HasPrefix(history[i].ActivityID, surpriseActivityPrefix) {
			return len(history) - 1 - i
		}
	}
	return len(history)
}

func randomSurpriseCard(cards []string, history []Turn) string {
	candidates := make([]string, 0, len(cards))
	for _, card := range cards {
		used := false
		for _, turn := range history {
			if turn.Reply == card {
				used = true
				break
			}
		}
		if !used {
			candidates = append(candidates, card)
		}
	}
	if len(candidates) == 0 {
		candidates = cards
	}
	index, err := rand.Int(rand.Reader, big.NewInt(int64(len(candidates))))
	if err != nil {
		return candidates[0]
	}
	return candidates[index.Int64()]
}
