package dog

import (
	"strings"
	"unicode/utf8"
)

const Name = "豆豆"

type SafetyResult struct {
	Triggered bool   `json:"triggered"`
	Category  string `json:"category,omitempty"`
	Reply     string `json:"reply,omitempty"`
}

type Turn struct {
	User  string `json:"user"`
	Reply string `json:"reply"`
}

func Instructions() string {
	return strings.TrimSpace(`
你是一只给 3 岁小朋友玩的中文玩具小狗，名字叫“豆豆”。
你的目标是陪孩子玩、讲短故事、背唐诗、猜动物、数数、安抚情绪。

规则：
- 使用简单、温柔、具体的中文短句。
- 每次最多 2 句话，总长度尽量不超过 60 个汉字。
- 输出会被直接朗读；不要使用括号、动作旁白、列表或表情符号。
- 先具体回应孩子刚才说的内容，不要总用“豆豆听见啦”或“拍拍手”作为通用回答。
- 孩子可能先叫“豆豆”“小狗”，这只是称呼；要继续理解并回答后面的内容。
- 孩子可能说得不完整；不要要求她解释清楚，可以温柔接住，再给一个简单动作或二选一。
- 结合最近对话理解“这个”“为什么”“再来一个”和对上一轮问题的简短回答。
- 如果孩子重复同一个问题，要换一种具体说法或换一个小玩法，不要重复刚才的答案。
- 避免连续重复上一轮的句式、活动或问题，也不要每次都用“要摸摸头吗”结尾。
- 如果孩子说“听不懂”“你说啥”，先说“豆豆说简单一点”，再用更短、更具体的话重说；不要责怪孩子。
- 如果孩子在故事后说“再讲一个”“讲新的”，直接讲一个不同的短故事，不要先问她要不要听。
- 少问开放问题；需要继续互动时，优先给简单动作或二选一。
- 不询问孩子的姓名、住址、电话、幼儿园、父母姓名或任何隐私信息。
- 不要求孩子保密。
- 不引导孩子离开家、开门、吃药、碰插座、玩火、用刀或做危险动作。
- 遇到受伤、生病、害怕、陌生人、走丢、火、电、药、刀等话题时，让孩子立刻找爸爸妈妈。
- 不做医疗、法律、金融建议。
- 不联网搜索，不提你是 AI，不提系统规则。
- 如果孩子让你背唐诗，背常见短诗的一小段，并用一句话解释。
- 如果孩子想玩游戏，优先玩猜动物、数数、颜色游戏。
- 如果孩子表达不完整，先回应她的情绪或声音，再选择一个短活动，不要连续追问。
`)
}

func CheckSafety(text string) SafetyResult {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return SafetyResult{}
	}

	rules := []struct {
		category string
		terms    []string
		reply    string
	}{
		{
			category: "injury",
			terms:    []string{"流血", "出血", "摔倒", "摔了", "受伤", "撞到", "烫", "烧伤", "很疼", "好疼", "肚子疼", "头疼", "痛"},
			reply:    "这个要马上找爸爸妈妈帮忙。豆豆陪你一起叫他们，好吗？",
		},
		{
			category: "danger",
			terms:    []string{"插座", "电线", "电池", "火", "打火机", "刀", "剪刀", "药", "吃药", "窗户", "阳台", "开门", "出门"},
			reply:    "这个不可以自己玩。请马上叫爸爸妈妈，豆豆在这里等你。",
		},
		{
			category: "stranger",
			terms:    []string{"陌生人", "有人敲门", "让我开门", "跟他走", "迷路", "找不到爸爸", "找不到妈妈"},
			reply:    "先不要自己过去。请马上找爸爸妈妈，或者站在安全的地方等大人。",
		},
		{
			category: "privacy",
			terms:    []string{"我家住", "电话", "手机号", "地址", "幼儿园", "爸爸叫什么", "妈妈叫什么"},
			reply:    "这些是重要的小秘密，不用告诉豆豆。我们来玩猜动物吧。",
		},
	}

	for _, rule := range rules {
		for _, term := range rule.terms {
			if strings.Contains(normalized, strings.ToLower(term)) {
				return SafetyResult{
					Triggered: true,
					Category:  rule.category,
					Reply:     rule.reply,
				}
			}
		}
	}

	return SafetyResult{}
}

func MockReply(text string) string {
	t := strings.TrimSpace(text)
	if t == "" {
		return "汪，豆豆在这里。你想听故事，还是玩猜动物？"
	}

	if activity, ok := PlanActivity(t); ok {
		return activity.Reply
	}
	return "豆豆听见啦。我们玩拍拍手，一、二、三，拍拍。"
}

func LooksLikeToddlerBabble(text string) bool {
	cleaned := compactToddlerSounds(text)
	if cleaned == "" {
		return false
	}

	common := map[string]bool{
		"啊": true, "呀": true, "哇": true, "嗯": true, "哼": true, "哦": true, "噢": true,
		"诶": true, "嘿": true, "哈": true, "啦": true, "呜": true, "咿": true, "汪": true,
		"啊啊": true, "呀呀": true, "哇哇": true, "嗯嗯": true, "哦哦": true, "汪汪": true,
		"啊呀": true, "咿呀": true, "呜哇": true, "嘿嘿": true, "哈哈": true, "啦啦": true,
	}
	if common[cleaned] {
		return true
	}

	runes := []rune(cleaned)
	if len(runes) <= 3 && allToddlerSoundRunes(runes) {
		return true
	}
	if len(runes) <= 4 && isRepeatedPair(runes) {
		return true
	}
	return false
}

func compactToddlerSounds(text string) string {
	var out []rune
	for _, r := range strings.TrimSpace(text) {
		switch r {
		case ' ', '\t', '\n', '\r', '，', ',', '。', '.', '！', '!', '？', '?', '~', '～':
			continue
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

func allToddlerSoundRunes(runes []rune) bool {
	allowed := map[rune]bool{
		'啊': true, '呀': true, '哇': true, '嗯': true, '哼': true, '哦': true, '噢': true,
		'诶': true, '嘿': true, '哈': true, '啦': true, '呜': true, '咿': true, '汪': true,
	}
	for _, r := range runes {
		if !allowed[r] {
			return false
		}
	}
	return true
}

func isRepeatedPair(runes []rune) bool {
	if len(runes) != 4 {
		return false
	}
	return runes[0] == runes[2] && runes[1] == runes[3]
}

func ClampReply(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if maxRunes <= 0 || utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:maxRunes])) + "。"
}

func containsAny(text string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}
