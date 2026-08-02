package server

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/donychen1134/pupbox/internal/dog"
)

const sessionHeader = "X-Pupbox-Session-ID"

const (
	contextTurnLimit = 4
	contextSceneTTL  = 90 * time.Second
)

var validSessionID = regexp.MustCompile(`^[A-Za-z0-9._-]{8,80}$`)

type sessionMemory struct {
	turns     []dog.Turn
	updatedAt time.Time
}

type SessionStore struct {
	mu          sync.Mutex
	sessions    map[string]sessionMemory
	maxSessions int
	maxTurns    int
	ttl         time.Duration
}

func NewSessionStore(maxSessions, maxTurns int, ttl time.Duration) *SessionStore {
	return &SessionStore{
		sessions:    make(map[string]sessionMemory),
		maxSessions: maxSessions,
		maxTurns:    maxTurns,
		ttl:         ttl,
	}
}

func (s *SessionStore) History(id string) []dog.Turn {
	if s == nil || !validSessionID.MatchString(id) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	memory, ok := s.sessions[id]
	if !ok {
		return nil
	}
	turns := memory.turns
	cutoff := time.Now().Add(-contextSceneTTL)
	first := len(turns)
	for first > 0 {
		turn := turns[first-1]
		if !turn.OccurredAt.IsZero() && turn.OccurredAt.Before(cutoff) {
			break
		}
		first--
	}
	turns = turns[first:]
	if len(turns) > contextTurnLimit {
		turns = turns[len(turns)-contextTurnLimit:]
	}
	return append([]dog.Turn(nil), turns...)
}

func (s *SessionStore) Append(id, user, reply string, activity *dog.Activity) {
	if s == nil || !validSessionID.MatchString(id) {
		return
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	if _, exists := s.sessions[id]; !exists && len(s.sessions) >= s.maxSessions {
		s.removeOldestLocked()
	}
	memory := s.sessions[id]
	activityID, activityState := "", ""
	if activity != nil {
		activityID = activity.ID
		activityState = activity.State
	}
	memory.turns = append(memory.turns, dog.Turn{
		User:          truncateText(user, 200),
		Reply:         truncateText(reply, 200),
		ActivityID:    truncateText(activityID, 40),
		ActivityState: truncateText(activityState, 40),
		OccurredAt:    now,
	})
	if len(memory.turns) > s.maxTurns {
		memory.turns = append([]dog.Turn(nil), memory.turns[len(memory.turns)-s.maxTurns:]...)
	}
	memory.updatedAt = now
	s.sessions[id] = memory
}

func (s *SessionStore) pruneLocked(now time.Time) {
	for id, memory := range s.sessions {
		if now.Sub(memory.updatedAt) > s.ttl {
			delete(s.sessions, id)
		}
	}
}

func (s *SessionStore) removeOldestLocked() {
	var oldestID string
	var oldestTime time.Time
	for id, memory := range s.sessions {
		if oldestID == "" || memory.updatedAt.Before(oldestTime) {
			oldestID = id
			oldestTime = memory.updatedAt
		}
	}
	delete(s.sessions, oldestID)
}

func requestSessionID(r *http.Request) string {
	id := strings.TrimSpace(r.Header.Get(sessionHeader))
	if !validSessionID.MatchString(id) {
		return ""
	}
	return id
}

func contextualInput(history []dog.Turn, current string) string {
	current = truncateText(current, 500)
	if len(history) == 0 {
		return current
	}
	var builder strings.Builder
	builder.WriteString("下面只包含豆豆和小主人最近仍有效的对话。只回答小主人现在说的话，不要复述记录。当前输入出现新的具体事物或想法时，立刻结束旧场景；不能因为上一轮在玩游戏就强行延续。\n")
	for _, turn := range history {
		if turn.ActivityID != "" {
			fmt.Fprintf(&builder, "小朋友：%s\n豆豆（正在进行%s活动）：%s\n", turn.User, turn.ActivityID, turn.Reply)
		} else {
			fmt.Fprintf(&builder, "小朋友：%s\n豆豆：%s\n", turn.User, turn.Reply)
		}
	}
	repeats := 0
	for _, turn := range history {
		if normalizeForRepeat(turn.User) == normalizeForRepeat(current) {
			repeats++
		}
	}
	if repeats > 0 {
		fmt.Fprintf(&builder, "提醒：小朋友最近已经问过这句话 %d 次。请换一个具体答案和句式，不要重复之前豆豆的回答。\n", repeats)
	}
	if utf8.RuneCountInString(normalizeForRepeat(current)) <= 3 {
		builder.WriteString("提醒：这句话很短，可能是幼儿表达或语音识别偏差。优先把它理解为对当前场景的回应，不要仅凭这几个字突然建立无关的新话题；不确定时用当前场景里的二选一轻轻确认。\n")
	}
	fmt.Fprintf(&builder, "小朋友现在说：%s", current)
	return builder.String()
}

func normalizeForRepeat(value string) string {
	return strings.NewReplacer(" ", "", "，", "", ",", "", "。", "", ".", "", "？", "", "?", "", "！", "", "!", "").Replace(strings.TrimSpace(value))
}

func truncateText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes <= 0 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	return string([]rune(value)[:maxRunes])
}
