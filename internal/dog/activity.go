package dog

import (
	"strings"
	"sync/atomic"
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
	"presence":     {},
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
			ID:       "presence",
			Label:    "陪伴",
			Prompt:   "豆豆在做什么",
			Reply:    "豆豆正趴着听你说话呢。小尾巴还在轻轻摇。",
			Category: "chat",
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
	case containsAny(normalized, "你在干什么", "你在干啥", "你干什么呢", "你干啥呢", "你干嘛呢") ||
		equalsAny(normalized, "你干什么", "你干啥", "你干嘛", "干啥呢"):
		return byID("presence")
	case containsAny(normalized, "猜动物", "猜小动物", "动物游戏", "猜谜语", "猜个谜") ||
		equalsAny(normalized, "动物", "小动物", "猜谜", "谜语"):
		return byID("animal_guess")
	case containsAny(normalized, "数数", "数一数", "一起数", "数数字") ||
		equalsAny(normalized, "数字", "一二三", "123"):
		return byID("counting")
	case containsAny(normalized, "唱歌", "唱一个", "唱首歌", "声音游戏", "学声音") ||
		equalsAny(normalized, "音乐", "声音"):
		return byID("sound_game")
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
	if activity, ok := PlanActivity(text); ok {
		return activity, true
	}
	normalized := stripDogAddress(normalizeToddlerIntentText(text))
	if !equalsAny(normalized, "再讲一个", "再来一个", "讲一个新的", "讲个新的", "换一个") {
		return Activity{}, false
	}
	for i := len(history) - 1; i >= 0 && i >= len(history)-3; i-- {
		previous := normalizeToddlerIntentText(history[i].User)
		if containsAny(previous, "故事", "古事", "古是", "鼓事", "故是") {
			return byID("story")
		}
	}
	return Activity{}, false
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
			Reply:    "豆豆在这里。我们找一个红色的小东西，好不好？",
			Category: "game",
			Action:   "glow_red",
		},
		{
			ID:       "clap",
			Label:    "拍拍",
			Reply:    "豆豆摇摇尾巴。你也可以轻轻拍拍手。",
			Category: "movement",
			Action:   "tail_wag",
		},
		{
			ID:       "clap",
			Label:    "拍拍",
			Reply:    "啊呀，豆豆听见啦。我们摸摸小鼻子，再摸摸小耳朵。",
			Category: "movement",
			Action:   "ear_wiggle",
		},
		{
			ID:       "clap",
			Label:    "拍拍",
			Reply:    "哇，豆豆也来回应你。咚咚拍两下，再小小声说汪。",
			Category: "movement",
			Action:   "paw_tap",
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
	replies := make([]string, 0, 64)
	seen := make(map[string]bool, 64)
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
	for _, id := range []string{
		"story",
		"poem",
		"animal_guess",
		"color_hunt",
		"counting",
		"sound_game",
		"clap",
		"comfort",
		"presence",
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
		"豆豆正趴着听你说话呢。小尾巴还在轻轻摇。",
		"豆豆在看你呀，还偷偷摇了两下尾巴。",
		"豆豆在等你聊天呢。现在耳朵竖得高高的。",
		"豆豆刚刚在数自己的小爪子，一、二、三、四。",
		"豆豆在想一朵云像不像棉花糖。你觉得呢？",
		"豆豆在练习小小声地汪，怕吵到你呀。",
	},
}
