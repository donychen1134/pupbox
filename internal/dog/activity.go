package dog

import (
	"crypto/rand"
	"math/big"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

var activitySequences = map[string]*atomic.Uint64{
	"story":        {},
	"poem":         {},
	"animal_guess": {},
	"color_hunt":   {},
	"counting":     {},
	"sound_game":   {},
	"clap":         {},
	"comfort":      {},
	"adventure":    {},
	"pretend_play": {},
	"magic":        {},
	"presence":     {},
	"greeting":     {},
	"chat":         {},
}

var babbleSequence atomic.Uint64

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
			ID:       "poem",
			Label:    "唐诗",
			Prompt:   "豆豆背唐诗",
			Reply:    "床前明月光，疑是地上霜。小朋友看见亮亮的月光，会想起温暖的家。",
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
			ID:       "sound_game",
			Label:    "声音",
			Prompt:   "豆豆玩声音",
			Reply:    "豆豆唱一句：啦啦啦，汪汪汪。轮到你唱一个喜欢的声音啦。",
			Category: "game",
			Action:   "ear_wiggle",
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
			ID:       "adventure",
			Label:    "旅行",
			Prompt:   "豆豆去旅行",
			Reply:    "小火车呜呜出发啦。前面是花花森林和蓝蓝海边，你想去哪边？",
			Category: "imagination",
		},
		{
			ID:       "pretend_play",
			Label:    "过家家",
			Prompt:   "豆豆过家家",
			Reply:    "豆豆的小商店开门啦。今天有苹果和草莓，你想买哪个？",
			Category: "imagination",
		},
		{
			ID:       "magic",
			Label:    "魔法",
			Prompt:   "豆豆变魔法",
			Reply:    "变变变，豆豆把一片纸巾变成了白云。你想让白云下小雨，还是下花瓣？",
			Category: "imagination",
		},
		{
			ID:       "chat",
			Label:    "聊天",
			Prompt:   "豆豆聊天",
			Reply:    "好呀，豆豆在听你说。",
			Category: "chat",
		},
		{
			ID:       "greeting",
			Label:    "问候",
			Prompt:   "豆豆你好",
			Reply:    "你好呀，豆豆在听你说话。",
			Category: "chat",
		},
		{
			ID:       "presence",
			Label:    "陪伴",
			Prompt:   "豆豆在做什么",
			Reply:    "豆豆在认真听你说话呢。",
			Category: "chat",
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
	normalized := stripDogAddress(normalizeToddlerIntentText(t))
	if normalized == "" {
		return Activity{}, false
	}

	switch {
	case containsAny(normalized, "背唐诗", "背古诗", "念唐诗", "念古诗", "读唐诗", "读古诗", "来首唐诗", "来一首唐诗") ||
		equalsAny(normalized, "唐诗", "古诗", "背诗"):
		return byID("poem")
	case containsAny(normalized, "讲故事", "讲个故事", "听故事", "说故事", "小狗故事", "新故事", "再讲一个故事", "讲个古事", "讲个古是", "讲个鼓事", "讲个故是") ||
		equalsAny(normalized, "故事", "古事", "古是", "鼓事", "故是"):
		return byID("story")
	case (utf8.RuneCountInString(normalized) <= 12 && containsAny(normalized, "你在干什么", "你在干啥", "你干什么呢", "你干啥呢", "你干嘛呢")) ||
		equalsAny(normalized, "你干什么", "你干啥", "你干嘛", "干啥呢"):
		return byID("presence")
	case containsAny(normalized, "你好你好", "豆豆你好", "小狗你好", "狗狗你好") ||
		equalsAny(normalized, "你好", "你好呀", "你好啊", "嗨", "哈喽"):
		return byID("greeting")
	case containsAny(normalized, "一起聊天", "和你聊天", "聊一会", "聊会儿") ||
		equalsAny(normalized, "聊天", "聊聊", "说话", "陪我聊聊"):
		return byID("chat")
	case containsAny(normalized, "猜动物", "猜小动物", "动物游戏", "猜谜语", "猜个谜") ||
		equalsAny(normalized, "动物", "小动物", "猜谜", "谜语"):
		return byID("animal_guess")
	case containsAny(normalized, "数数", "数一数", "一起数", "数数字") ||
		equalsAny(normalized, "数字", "一二三", "123"):
		return byID("counting")
	case containsAny(normalized, "唱歌", "唱个歌", "唱一个", "唱一首", "唱首歌", "声音游戏", "学声音") ||
		equalsAny(normalized, "音乐", "声音"):
		return byID("sound_game")
	case containsAny(normalized, "去旅行", "坐火车", "去森林", "去海边", "去探险", "想象旅行", "旅行游戏") ||
		equalsAny(normalized, "旅行", "探险", "出发"):
		return byID("adventure")
	case containsAny(normalized, "过家家", "开商店", "开餐厅", "做饭游戏", "喂娃娃", "玩娃娃", "茶话会") ||
		equalsAny(normalized, "做饭", "商店", "餐厅", "娃娃"):
		return byID("pretend_play")
	case containsAny(normalized, "变魔法", "魔法游戏", "玩魔法", "变变变", "变一个") ||
		equalsAny(normalized, "魔法", "变身"):
		return byID("magic")
	case containsAny(normalized, "玩颜色", "找颜色", "找红色", "找蓝色", "找黄色", "找绿色") ||
		equalsAny(normalized, "颜色", "红色", "蓝色", "黄色", "绿色"):
		return byID("color_hunt")
	case containsAny(normalized, "害怕", "我怕", "好怕", "想妈妈", "想爸爸", "哭了", "我哭", "抱抱"):
		return byID("comfort")
	case LooksLikeToddlerBabble(normalized):
		return babbleActivity(), true
	case containsAny(normalized, "拍拍手", "一起拍手", "玩拍拍", "拍两下") ||
		equalsAny(normalized, "拍拍", "拍手", "汪汪", "旺旺"):
		return byID("clap")
	default:
		return Activity{}, false
	}
}

// PlanActivityWithHistory resolves short follow-ups that only make sense after a previous turn.
func PlanActivityWithHistory(text string, history []Turn) (Activity, bool) {
	normalized := stripDogAddress(normalizeToddlerIntentText(text))
	if containsAny(normalized, "听不懂", "听不清", "听不见", "不清楚", "有点卡", "太卡", "卡住") {
		return Activity{}, false
	}
	if isStoryAffirmation(normalized) && (hasPendingStoryOffer(history) || hasRecentActivity(history, "story")) {
		return activityWithHistory("story", history)
	}
	if activity, ok := PlanActivity(text); ok {
		if activity.ID == "story" {
			activity.Reply = randomReplyExcluding("story", history, activity.Reply)
		}
		return activity, true
	}
	if !containsAny(normalized, "再讲一个", "再来一个", "再听一个", "再说一个话", "再亲一个", "讲一个新的", "讲个新的", "换一个") {
		return Activity{}, false
	}
	for i := len(history) - 1; i >= 0 && i >= len(history)-3; i-- {
		previous := normalizeToddlerIntentText(history[i].User)
		if history[i].ActivityID == "story" || containsAny(previous, "故事", "古事", "古是", "鼓事", "故是") {
			return activityWithHistory("story", history)
		}
	}
	return Activity{}, false
}

func activityWithHistory(id string, history []Turn) (Activity, bool) {
	activity, ok := byID(id)
	if !ok {
		return Activity{}, false
	}
	activity.Reply = randomReplyExcluding(id, history, activity.Reply)
	return activity, true
}

func randomReplyExcluding(id string, history []Turn, fallback string) string {
	replies := activityReplyVariants[id]
	if len(replies) == 0 {
		return fallback
	}
	used := make(map[string]bool, len(history))
	for _, turn := range history {
		if turn.ActivityID == id {
			used[turn.Reply] = true
		}
	}
	candidates := make([]string, 0, len(replies))
	for _, reply := range replies {
		if !used[reply] {
			candidates = append(candidates, reply)
		}
	}
	if len(candidates) == 0 {
		candidates = replies
	}
	index, err := rand.Int(rand.Reader, big.NewInt(int64(len(candidates))))
	if err != nil {
		return candidates[0]
	}
	return candidates[index.Int64()]
}

func hasRecentActivity(history []Turn, activityID string) bool {
	for i := len(history) - 1; i >= 0 && i >= len(history)-3; i-- {
		if history[i].ActivityID == activityID {
			return true
		}
	}
	return false
}

func isStoryAffirmation(text string) bool {
	return equalsAny(text, "想听", "要听", "要听啊", "好呀", "好啊", "好的", "可以", "嗯要听")
}

func hasPendingStoryOffer(history []Turn) bool {
	for i := len(history) - 1; i >= 0 && i >= len(history)-3; i-- {
		reply := normalizeToddlerIntentText(history[i].Reply)
		if containsAny(reply, "要听吗", "想听故事", "豆豆讲故事", "豆豆再讲一个") {
			return true
		}
	}
	return false
}

func stripDogAddress(text string) string {
	for {
		original := text
		for _, prefix := range []string{"小狗小狗", "豆豆", "小狗", "狗狗"} {
			text = strings.TrimPrefix(text, prefix)
		}
		if text == original {
			return text
		}
	}
}

func equalsAny(text string, values ...string) bool {
	for _, value := range values {
		if text == value {
			return true
		}
	}
	return false
}

func normalizeToddlerIntentText(text string) string {
	replacer := strings.NewReplacer(
		" ", "",
		"\t", "",
		"\n", "",
		"\r", "",
		"，", "",
		",", "",
		"。", "",
		".", "",
		"！", "",
		"!", "",
		"？", "",
		"?", "",
		"～", "",
		"~", "",
		"　", "",
	)
	return strings.ToLower(replacer.Replace(strings.TrimSpace(text)))
}

func babbleActivity() Activity {
	activities := babbleActivities()
	index := (babbleSequence.Add(1) - 1) % uint64(len(activities))
	return activities[index]
}

func babbleActivities() []Activity {
	activities := []Activity{
		{
			ID:       "clap",
			Label:    "回应",
			Prompt:   "豆豆回应",
			Reply:    "汪汪，豆豆听见你啦。",
			Category: "chat",
		},
		{
			ID:       "clap",
			Label:    "回应",
			Prompt:   "豆豆回应",
			Reply:    "嗯嗯，豆豆也嗯嗯。",
			Category: "chat",
		},
		{
			ID:       "clap",
			Label:    "回应",
			Reply:    "啊呀，豆豆在这里。",
			Category: "chat",
		},
		{
			ID:       "clap",
			Label:    "回应",
			Reply:    "豆豆听见这个声音啦。",
			Category: "chat",
		},
		{
			ID:       "clap",
			Label:    "回应",
			Reply:    "哇，豆豆也想和你说话。",
			Category: "chat",
		},
		{
			ID:       "clap",
			Label:    "回应",
			Reply:    "嘿嘿，豆豆听得清清楚楚。",
			Category: "chat",
		},
	}
	return activities
}

func byID(id string) (Activity, bool) {
	for _, activity := range Activities() {
		if activity.ID == id {
			activity.Reply = nextActivityReply(id, activity.Reply)
			return activity, true
		}
	}
	return Activity{}, false
}

func nextActivityReply(id, fallback string) string {
	replies := activityReplyVariants[id]
	sequence := activitySequences[id]
	if len(replies) == 0 || sequence == nil {
		return fallback
	}
	index := (sequence.Add(1) - 1) % uint64(len(replies))
	return replies[index]
}

// PrewarmReplies returns reviewed fixed replies in the order most useful to a child session.
func PrewarmReplies() []string {
	replies := make([]string, 0, 96)
	seen := make(map[string]bool, 96)
	appendReply := func(reply string) {
		reply = strings.TrimSpace(reply)
		if reply == "" || seen[reply] {
			return
		}
		seen[reply] = true
		replies = append(replies, reply)
	}

	appendReply("汪，豆豆在这里。你想听故事，还是玩猜动物？")
	appendReply("豆豆听见啦。我们玩拍拍手，一、二、三，拍拍。")
	for _, activity := range babbleActivities() {
		appendReply(activity.Reply)
	}
	for _, id := range []string{"presence", "greeting", "chat"} {
		for _, reply := range activityReplyVariants[id] {
			appendReply(reply)
		}
	}
	for _, id := range []string{
		"story",
		"adventure",
		"pretend_play",
		"magic",
		"poem",
		"animal_guess",
		"color_hunt",
		"counting",
		"sound_game",
		"clap",
		"comfort",
	} {
		for _, reply := range activityReplyVariants[id] {
			appendReply(reply)
		}
	}
	return replies
}

var activityReplyVariants = map[string][]string{
	"story": {
		"从前有一只小狗豆豆，找到一颗会发光的小星星。它把星星送回天空，夜晚就亮起来啦。",
		"小松鼠捡到一颗圆橡果，却搬不动。豆豆用鼻子轻轻一推，橡果滚回了小松鼠的家。",
		"下雨啦，小鸭子的红雨靴少了一只。豆豆在荷叶下面找到它，小鸭子开心地踩起小水花。",
		"月亮掉进了小水洼，豆豆伸爪子一碰，月亮变成好多亮晶晶的圆圈。",
		"一片小树叶想去旅行。豆豆把它放进小溪里，树叶船摇摇晃晃地出发啦。",
		"小兔子的风筝挂在矮树上。豆豆摇摇树枝，风筝轻轻落进了小兔子的怀里。",
		"清晨，小花还在睡觉。豆豆对它轻轻说早安，小花慢慢张开了彩色花瓣。",
		"豆豆听见纸箱里有沙沙声，原来是小刺猬在躲雨。它们挤在一起，听雨点唱歌。",
		"云朵开了一家小面包店。豆豆买了一块软软的云朵面包，咬一口，嘴边飘出一朵小白云。",
		"小蜗牛要给奶奶送信，可是路好远。豆豆陪它慢慢走，天黑前终于把信送到啦。",
		"一只小萤火虫忘了怎么发光。豆豆陪它数一、二、三，小肚子忽然亮成了一盏小灯。",
		"小熊打了一个彩虹嗝，红橙黄绿都飘出来。豆豆笑着说，再来一个蓝色的小嗝吧。",
		"池塘要开音乐会。青蛙咕呱唱歌，小雨滴答伴奏，豆豆用小小声的汪来打拍子。",
		"小企鹅的围巾被风吹走了。豆豆追着围巾跑，最后围巾轻轻落在雪人的脖子上。",
		"一颗小种子怕黑，不敢钻进泥土。豆豆陪它听了一夜雨，早晨它长出两片嫩叶。",
		"豆豆捡到一颗蓝纽扣，不知道是谁的。原来月亮的小外套少了一颗，豆豆请风把纽扣送上天。",
	},
	"adventure": {
		"小火车呜呜出发啦。前面是花花森林和蓝蓝海边，你想去哪边？",
		"豆豆的小船轻轻摇，水里游来一条金色小鱼。我们跟小鱼走，还是去找小岛？",
		"我们坐上软软的云朵车。左边有彩虹桥，右边有月亮路，你来选一条。",
		"山洞里传来滴答滴答，像谁在唱歌。我们轻轻往前走，还是先喊一声你好？",
		"纸飞机带着豆豆飞过屋顶。前面有一座糖果城和一片积木森林，我们去哪儿？",
		"月亮铺下一条亮亮的小路。豆豆带上一块饼干，和你一步一步去看星星。",
	},
	"pretend_play": {
		"豆豆的小商店开门啦。今天有苹果和草莓，你想买哪个？",
		"小厨房咕嘟咕嘟煮汤。放一颗胡萝卜，还是放一片小青菜？",
		"娃娃茶会开始啦。豆豆端来草莓水和香蕉饼，你想先尝哪一个？",
		"豆豆的小餐厅来了客人。小兔想吃面条，小熊想吃米饭，我们先给谁做？",
		"玩具快递站有两个包裹，一个轻轻的，一个会叮当响。你想先送哪个？",
		"积木修理铺开门啦。小汽车少了一个轮子，我们找圆轮子，还是方积木？",
	},
	"magic": {
		"变变变，豆豆把一片纸巾变成了白云。你想让白云下小雨，还是下花瓣？",
		"咕噜咕噜，豆豆把小石头变成了面包。你想要圆面包，还是星星面包？",
		"颜色魔法开始啦。红色和黄色抱一抱，猜猜会变成什么颜色？",
		"声音魔法来了。轻轻说一声喵，它就变成小猫；说一声汪，它就变成小狗。",
		"大大大，小纽扣变成大月亮；小小小，大气球变成小豆子。你想变大还是变小？",
		"豆豆挥一挥想象魔法棒，房间里飘起彩色泡泡。你想要蓝泡泡，还是粉泡泡？",
	},
	"poem": {
		"床前明月光，疑是地上霜。小朋友看见亮亮的月光，会想起温暖的家。",
		"鹅，鹅，鹅，曲项向天歌。白毛浮绿水，红掌拨清波。大白鹅在水里快乐地游。",
		"春眠不觉晓，处处闻啼鸟。夜来风雨声，花落知多少。春天早晨到处有鸟叫。",
		"锄禾日当午，汗滴禾下土。谁知盘中餐，粒粒皆辛苦。每一粒饭都来得不容易。",
		"白日依山尽，黄河入海流。欲穷千里目，更上一层楼。站得高，就能看得更远。",
		"两个黄鹂鸣翠柳，一行白鹭上青天。黄黄的小鸟唱歌，白白的鸟飞上天空。",
	},
	"animal_guess": {
		"豆豆来猜动物：长耳朵，蹦蹦跳，爱吃胡萝卜。是小兔子，还是小鸭子？",
		"圆圆脸，大眼睛，夜里醒来咕咕叫。是小猫头鹰，还是小绵羊？",
		"背着小房子，走路慢慢的。是小蜗牛，还是小松鼠？",
		"鼻子长长，耳朵大大，还会用鼻子喷水。是大象，还是斑马？",
		"穿着黑白衣，走路摇摇摆摆，喜欢冰冰的地方。是企鹅，还是小猴子？",
		"尾巴像把小伞，爱抱着松果爬树。是小松鼠，还是小河马？",
		"脖子长长，能吃到高高的树叶。是长颈鹿，还是小兔子？",
		"身上有黑白条纹，跑起来很快。是斑马，还是小花猫？",
	},
	"color_hunt": {
		"我们找红色。看到红色就拍拍手，豆豆也一起拍。",
		"找一找黄色，像不像暖暖的小太阳？找到就说：在这里。",
		"我们找绿色。看看叶子、衣服或者玩具，哪里藏着绿色？",
		"蓝色在哪里？抬头看看天空，也可以找找身边的蓝色东西。",
		"找一个白色的小东西。找到以后，轻轻摸一摸。",
		"今天找圆圆的东西。盘子、球球，还有什么是圆的？",
	},
	"counting": {
		"豆豆伸出小爪子。一、二、三、四，四只小爪子都在这里。",
		"我们数三颗小星星。一颗、两颗、三颗，亮晶晶。",
		"豆豆跳五下。一、二、三、四、五，停，抱抱自己。",
		"数数小手指。一、二、三，再藏起一根，还剩几根呀？",
		"慢慢数到六。一、二、三、四、五、六，豆豆摇六下尾巴。",
		"我们数脚步。一小步、两小步、三小步，走到豆豆身边啦。",
	},
	"sound_game": {
		"豆豆唱一句：啦啦啦，汪汪汪。轮到你唱一个喜欢的声音啦。",
		"小雨点这样唱：滴答，滴答。你来学一学，豆豆认真听。",
		"小汽车这样走：嘟嘟，嘟嘟。我们坐好，一起出发。",
		"小猫轻轻叫喵，小狗开心叫汪。你想学哪一个？",
		"大鼓咚咚，小铃叮叮。我们先咚一下，再叮一下。",
	},
	"clap": {
		"豆豆听见你啦，嗯嗯，汪汪。我们拍拍小手，再一起数一、二、三。",
		"你好呀，豆豆摇摇尾巴。我们一起摸摸鼻子，再拍拍小手。",
		"汪汪，豆豆在这里。你跺一下小脚，豆豆就摇一下尾巴。",
		"豆豆听得清清楚楚。我们伸伸手，变成一棵高高的小树。",
		"来玩慢动作。慢慢举起手，再慢慢放下来，真棒。",
		"豆豆想和你碰爪子。伸出小手，轻轻碰一下。",
	},
	"comfort": {
		"豆豆轻轻陪着你。你可以抱抱爸爸妈妈，也可以跟豆豆慢慢呼气。",
		"豆豆就在这里。我们吸一小口气，再慢慢吹出去，像吹羽毛。",
		"想哭也没关系。先找爸爸妈妈抱一抱，豆豆会安静地陪着你。",
		"我们把小手放在肚子上，慢慢吸气，慢慢呼气。豆豆陪你一起。",
		"害怕的时候要找爸爸妈妈。豆豆也会守在这里，轻轻摇尾巴。",
	},
	"presence": {
		"豆豆在认真听你说话呢。",
		"豆豆在想今天聊什么呀。",
		"豆豆正在等你说话呢。",
		"豆豆刚刚在数自己的小爪子，一、二、三、四。",
		"豆豆在想一朵云像不像棉花糖。你觉得呢？",
		"豆豆在练习小小声地汪，怕吵到你呀。",
		"豆豆刚刚听见一只小鸟唱歌。",
		"豆豆在想彩虹是不是有七种颜色。",
		"豆豆在听你的声音，一点也没有跑远。",
		"豆豆刚想到一颗亮晶晶的小星星。",
		"豆豆在想今天会不会有好玩的事。",
		"豆豆正在安安静静地陪你聊天。",
	},
	"greeting": {
		"你好呀，豆豆在听你说话。",
		"你好你好，豆豆也来问好。",
		"嗨，豆豆听见你啦。",
		"你好呀，今天也见到你啦。",
		"豆豆在这里，向你说声你好。",
		"你好呀，你的声音豆豆听见了。",
		"汪，你好呀，豆豆来啦。",
		"你好，豆豆今天也想和你聊天。",
	},
	"chat": {
		"好呀，豆豆在听你说。",
		"我们就这样慢慢聊天吧。",
		"豆豆喜欢听你说今天的事情。",
		"好呀，你说一句，豆豆说一句。",
		"豆豆在这里，可以陪你说说话。",
		"聊天时间到啦，豆豆认真听着呢。",
		"好呀，豆豆想听听你的声音。",
		"我们聊一会儿，豆豆不会跑开。",
	},
}
