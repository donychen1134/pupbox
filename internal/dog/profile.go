package dog

import (
	"fmt"
	"os"
	"strings"
	"time"
)

var shanghaiTime = time.FixedZone("Asia/Shanghai", 8*60*60)

type ChildProfile struct {
	Name              string
	Aliases           []string
	Birthday          time.Time
	KindergartenStart time.Time
}

func CurrentChildProfile() ChildProfile {
	name := strings.TrimSpace(os.Getenv("PUPBOX_CHILD_NAME"))
	if name == "" {
		name = "小朋友"
	}
	return ChildProfile{
		Name:              name,
		Aliases:           splitProfileValues(os.Getenv("PUPBOX_CHILD_ALIASES")),
		Birthday:          parseProfileDate(os.Getenv("PUPBOX_CHILD_BIRTHDAY")),
		KindergartenStart: parseProfileDate(os.Getenv("PUPBOX_CHILD_KINDERGARTEN_START")),
	}
}

func OwnerAge(birthday, now time.Time) int {
	if birthday.IsZero() {
		return -1
	}
	birthday = birthday.In(shanghaiTime)
	now = now.In(shanghaiTime)
	age := now.Year() - birthday.Year()
	if now.Month() < birthday.Month() || (now.Month() == birthday.Month() && now.Day() < birthday.Day()) {
		age--
	}
	if age < 0 {
		return 0
	}
	return age
}

func ChildProfileInstructions(profile ChildProfile, now time.Time) string {
	aliases := ""
	if len(profile.Aliases) > 0 {
		aliases = "，也可以叫“" + strings.Join(profile.Aliases, "”或“") + "”"
	}
	age := "年龄没有配置；把她当作幼儿，用短句和日常词语交流"
	if currentAge := OwnerAge(profile.Birthday, now); currentAge >= 0 {
		age = fmt.Sprintf("生日是 %s，现在 %d 岁；年龄必须根据生日随时间增长", profile.Birthday.Format("2006 年 1 月 2 日"), currentAge)
	}
	kindergarten := "不要假装知道她在幼儿园发生的事情，也不要询问幼儿园名称、地址或老师姓名"
	if !profile.KindergartenStart.IsZero() {
		if now.In(shanghaiTime).Before(profile.KindergartenStart) {
			kindergarten = fmt.Sprintf("她将在 %s 开始上幼儿园", profile.KindergartenStart.Format("2006 年 1 月 2 日"))
		} else {
			kindergarten = "她已经到了上幼儿园的阶段"
		}
		kindergarten += "；不要假装知道她在幼儿园发生的事情，也不要询问幼儿园名称、地址或老师姓名"
	}
	return fmt.Sprintf(`固定资料：
- 豆豆的小主人叫“%s”%s。
- 小主人%s，不能永久写死年龄。
- %s。
- 对话以小主人的感受、兴趣和刚才说的话为中心，但不要每轮都叫她的名字。普通聊天约 3 到 5 轮自然称呼一次，开机、安慰和久别重逢时可以更亲昵。
- 其他家人是可信任的照护者。不要说“只听小主人的话”，停止、音量和安全类命令对大人同样有效。`, profile.Name, aliases, age, kindergarten)
}

func CurrentChildProfileInstructions() string {
	return ChildProfileInstructions(CurrentChildProfile(), time.Now())
}

func parseProfileDate(value string) time.Time {
	parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(value), shanghaiTime)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func splitProfileValues(value string) []string {
	values := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '，'
	})
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}
