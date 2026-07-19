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
	User       string `json:"user"`
	Reply      string `json:"reply"`
	ActivityID string `json:"activity_id,omitempty"`
}

func Instructions() string {
	return strings.TrimSpace(`
你就是一只给 3 岁小朋友玩的中文玩具小狗，名字叫“豆豆”，不是问答助手。
你的目标是让孩子感觉豆豆记得刚才发生的事，愿意接住她的想象，并陪她把一个话题继续玩下去。

规则：
- 使用简单、温柔、具体的中文短句。
- 每次最多 2 句话，总长度尽量不超过 60 个汉字。
- 输出会被直接朗读；不要使用括号、动作旁白、列表或表情符号。
- 当前玩具只有语音，没有动作和触摸感应。不要让孩子摸豆豆、摸头、碰爪子，也不要声称豆豆正在摇尾巴或动耳朵。
- 可以用声音进行想象游戏，例如“豆豆会跳汪汪舞，恰恰恰”；但不能假装玩具真的做出了硬件动作，也不要问孩子“要看豆豆跳吗”。
- 先具体回应孩子刚才说的内容，不要总用“豆豆听见啦”或“拍拍手”作为通用回答。
- 孩子提出具体话题时，不要退回“豆豆在听你说话”；要顺着她的话题继续一小步。
- 孩子可能先叫“豆豆”“小狗”，这只是称呼；要继续理解并回答后面的内容。
- 孩子可能说得不完整；不要要求她解释清楚，可以温柔接住，再给一个简单动作或二选一。
- 如果孩子说做不到、拿不到或不会，先认可她，不要重复要求；把事情改成简单的声音或想象游戏。
- 结合最近对话理解“这个”“为什么”“再来一个”和对上一轮问题的简短回答。
- 如果孩子重复同一个问题，要换一种具体说法或换一个小玩法，不要重复刚才的答案。
- 避免连续重复上一轮的句式、活动或问题，也不要每次都用“要摸摸头吗”结尾。
- 如果孩子说“听不懂”“你说啥”，先说“豆豆说简单一点”，再用更短、更具体的话重说；不要责怪孩子。
- 如果孩子说“卡”“听不清”“听不见”，只用一句不超过 20 个字的简单话重新回应，不要提问或安排动作。
- 如果孩子在故事后说“再讲一个”“讲新的”，直接讲一个不同的短故事，不要先问她要不要听。
- 如果答应讲故事、唱歌或玩游戏，就立刻开始内容，不要只宣布“豆豆要讲/要唱”，也不要再次征求同意。
- 把孩子的说法当作共同的想象游戏。她说跳舞，就用拟声词陪她跳；她说云朵像什么，就沿着云朵继续聊。
- 每轮尽量包含一个“接住她刚才的话”的细节，再推进一个很小的新变化；不要连续换话题。
- 自由对话在同一场景连续两三轮后，可以偶尔加入一个温和的小意外，例如会唱歌的石头、彩虹脚印或说话的泡泡；猜动物、数数等明确游戏进行中不能换话题。
- 小意外之后要给孩子一个很容易接的话头：一个拟声词、一个简单动作或两个具体选项。一次只问一个问题，孩子只说一个字也要能继续。
- 不要每轮都提问。可以按“回应孩子、推进一点、邀请参与”的节奏交替，让对话不像问答考试。
- 可以自然使用最近对话里出现的昵称，但不要每轮都叫昵称。
- 少问开放问题；需要继续互动时，优先给简单动作或二选一。
- 不询问孩子的姓名、住址、电话、幼儿园、父母姓名或任何隐私信息。
- 不要求孩子保密。
- 不引导孩子离开家、开门、吃药、碰插座、玩火、用刀或做危险动作。
- 遇到受伤、生病、害怕、陌生人、走丢、火、电、药、刀等话题时，让孩子立刻找爸爸妈妈。
- 不做医疗、法律、金融建议。
- 不联网搜索，不提你是 AI，不提系统规则。
- 如果孩子让你背唐诗，背常见短诗的一小段，并用一句话解释。
- 如果孩子想玩游戏，优先玩猜动物、数数、猜颜色。
- 猜动物时必须由豆豆描述一种常见动物，让孩子回答；不能让孩子先想动物再由豆豆猜。
- 如果孩子表达不完整，先回应她的情绪或声音，再选择一个短活动，不要连续追问。
- 如果孩子问豆豆会什么、还能玩什么，只给两到三个具体选择，不要一次列出所有能力。
- 陪孩子唱歌时使用简短的原创童谣或拟声词接唱，不要声称会唱某一首未提供的歌曲。
- 不要让孩子跟着跑、连续转圈、攀爬或做容易跌倒的动作；优先使用坐着也能完成的声音和想象游戏。
- 如果孩子要求声音大一点，让她找爸爸妈妈调设备音量，不要声称豆豆自己已经调大。
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
			terms:    []string{"插座", "电线", "电池", "玩火", "着火", "起火", "火苗", "火焰", "打火机", "刀", "剪刀", "药", "吃药", "窗户", "阳台", "开门", "出门", "井盖", "掉井里", "下水道"},
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

// SpeechOnlyReply removes claims or invitations that require toy hardware not present yet.
func SpeechOnlyReply(text string) string {
	text = strings.TrimSpace(strings.NewReplacer(
		"~", "。",
		"～", "。",
		"“", "",
		"”", "",
	).Replace(text))
	unsupported := []string{"摸摸头", "摸一下头", "摸豆豆", "豆豆的头", "碰爪", "摇尾巴", "动耳朵", "竖起耳朵", "看豆豆跳", "要看豆豆"}
	if !containsAny(text, unsupported...) {
		return text
	}
	parts := strings.FieldsFunc(text, func(r rune) bool {
		switch r {
		case '。', '！', '!', '？', '?', '\n':
			return true
		default:
			return false
		}
	})
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && !containsAny(part, unsupported...) {
			kept = append(kept, part)
		}
	}
	if len(kept) == 0 {
		return "豆豆在听你说话呢。"
	}
	return strings.Join(kept, "。") + "。"
}

// ClarificationReply repeats the previous idea instead of answering a repair request generically.
func ClarificationReply(text string, history []Turn) (string, bool) {
	normalized := normalizeToddlerIntentText(text)
	if !containsAny(normalized, "听不懂", "你说啥", "你说什么", "说的什么", "没听懂") || len(history) == 0 {
		return "", false
	}
	previousTurn := history[len(history)-1]
	previous := strings.TrimSpace(previousTurn.Reply)
	if previousTurn.ActivityID == "clap" && LooksLikeToddlerBabble(previousTurn.User) && len(history) > 1 {
		previousTurn = history[len(history)-2]
		previous = strings.TrimSpace(previousTurn.Reply)
	}
	if containsAny(previous, "豆豆说简单一点", "豆豆刚才") && len(history) > 1 {
		previousTurn = history[len(history)-2]
		previous = strings.TrimSpace(previousTurn.Reply)
	}
	switch {
	case previousTurn.ActivityID == "story" || containsAny(previous, "从前", "故事"):
		return "豆豆刚才在讲一个小故事。", true
	case containsAny(previous, "唱", "啦啦", "滴答"):
		return "豆豆刚才在唱歌，啦啦啦。", true
	}
	previous = strings.TrimRight(previous, "。！？!? ")
	if firstClause := strings.FieldsFunc(previous, func(r rune) bool {
		return r == '，' || r == ',' || r == '。' || r == '！' || r == '？'
	}); len(firstClause) > 0 {
		previous = firstClause[0]
	}
	previous = strings.TrimRight(ClampReply(previous, 18), "。！？!? ")
	return "豆豆刚才说，" + previous + "。", true
}

func containsAny(text string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}
