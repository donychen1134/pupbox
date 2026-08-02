package server

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/donychen1134/pupbox/internal/dog"
)

type streamedReplyResult struct {
	reply    string
	safety   dog.SafetyResult
	activity *dog.Activity
	source   string
	err      error
}

func (s *Server) streamReply(
	ctx context.Context,
	text string,
	history []dog.Turn,
	onSentence func(string) error,
	onFirstToken func(),
) streamedReplyResult {
	if reply, safety, activity, source, handled := s.deterministicReply(text, history); handled {
		err := emitSpeechSentence(reply, onSentence)
		return streamedReplyResult{reply: reply, safety: safety, activity: activity, source: source, err: err}
	}

	provider, ok := s.chat.(StreamingStructuredChatProvider)
	if !s.xiaozhi.streaming || !s.useChat || !ok {
		reply, safety, activity, source, err := s.reply(ctx, text, history)
		if err == nil {
			err = emitSpeechSentence(reply, onSentence)
		}
		return streamedReplyResult{reply: reply, safety: safety, activity: activity, source: source, err: err}
	}

	stream := &structuredReplyStream{}
	var first sync.Once
	raw, streamErr := provider.StreamStructuredResponse(
		ctx,
		dog.RoutingInstructions(),
		contextualInput(history, text),
		func(delta string) error {
			if delta == "" {
				return nil
			}
			if onFirstToken != nil {
				first.Do(onFirstToken)
			}
			for _, sentence := range stream.Append(delta) {
				if err := emitSpeechSentence(sentence, onSentence); err != nil {
					return err
				}
			}
			return nil
		},
	)

	route, parseErr := dog.ParseSemanticRoute(raw)
	if parseErr != nil {
		if streamErr == nil {
			streamErr = fmt.Errorf("parse streaming semantic route %q: %w", truncateText(raw, 300), parseErr)
		}
		if stream.delivered == "" {
			fallback := dog.SpeechOnlyReply(dog.MockReply(text))
			emitErr := emitSpeechSentence(fallback, onSentence)
			return streamedReplyResult{
				reply: fallback, source: "mock_fallback", err: errors.Join(streamErr, emitErr),
			}
		}
		return streamedReplyResult{reply: stream.delivered, source: s.chat.Name(), err: streamErr}
	}

	if route.Kind == "activity" {
		activity, found := dog.RoutedActivity(route.ActivityID, history)
		if !found {
			fallback := dog.SpeechOnlyReply(dog.MockReply(text))
			emitErr := emitSpeechSentence(fallback, onSentence)
			return streamedReplyResult{
				reply:  fallback,
				source: "mock_fallback",
				err:    errors.Join(streamErr, emitErr, fmt.Errorf("unsupported routed activity %q", route.ActivityID)),
			}
		}
		activity.Reply = dog.SpeechOnlyReply(activity.Reply)
		emitErr := emitSpeechSentence(activity.Reply, onSentence)
		return streamedReplyResult{
			reply: activity.Reply, activity: &activity, source: "activity:" + activity.ID,
			err: errors.Join(streamErr, emitErr),
		}
	}

	reply := dog.SpeechOnlyReply(dog.ClampReply(route.Reply, 100))
	if route.Kind == "uncertain" {
		emitErr := emitSpeechSentence(reply, onSentence)
		return streamedReplyResult{
			reply: reply, source: "input:uncertain", err: errors.Join(streamErr, emitErr),
		}
	}
	for _, sentence := range stream.Flush() {
		if err := emitSpeechSentence(sentence, onSentence); err != nil {
			return streamedReplyResult{reply: reply, source: s.chat.Name(), err: errors.Join(streamErr, err)}
		}
	}
	return streamedReplyResult{reply: reply, source: s.chat.Name(), err: streamErr}
}

func emitSpeechSentence(sentence string, onSentence func(string) error) error {
	sentence = dog.SpeechOnlyReply(strings.TrimSpace(sentence))
	if sentence == "" || onSentence == nil {
		return nil
	}
	return onSentence(sentence)
}

type structuredReplyStream struct {
	raw       strings.Builder
	seenReply string
	pending   string
	delivered string
}

func (s *structuredReplyStream) Append(delta string) []string {
	s.raw.WriteString(delta)
	raw := s.raw.String()
	kind, _, kindComplete := partialJSONStringField(raw, "kind")
	if !kindComplete || strings.ToLower(strings.TrimSpace(kind)) != "chat" {
		return nil
	}
	reply, started, _ := partialJSONStringField(raw, "reply")
	if !started || !strings.HasPrefix(reply, s.seenReply) {
		return nil
	}
	s.pending += strings.TrimPrefix(reply, s.seenReply)
	s.seenReply = reply
	return s.takeCompleteSentences()
}

func (s *structuredReplyStream) takeCompleteSentences() []string {
	var sentences []string
	start := 0
	for index, r := range s.pending {
		if !strings.ContainsRune("。！？!?", r) {
			continue
		}
		end := index + utf8.RuneLen(r)
		sentence := strings.TrimSpace(s.pending[start:end])
		if sentence != "" {
			sentences = append(sentences, sentence)
			s.delivered += sentence
		}
		start = end
	}
	if start > 0 {
		s.pending = s.pending[start:]
	}
	return sentences
}

func (s *structuredReplyStream) Flush() []string {
	remaining := strings.TrimSpace(s.pending)
	s.pending = ""
	if remaining == "" {
		return nil
	}
	s.delivered += remaining
	return []string{remaining}
}

// partialJSONStringField decodes the complete portion of a JSON string field.
// It accepts an unfinished final string so structured chat can be spoken while
// the model is still producing the rest of the JSON object.
func partialJSONStringField(raw, field string) (value string, started, complete bool) {
	marker := strconv.Quote(field)
	index := strings.Index(raw, marker)
	if index < 0 {
		return "", false, false
	}
	index += len(marker)
	for index < len(raw) && (raw[index] == ' ' || raw[index] == '\t' || raw[index] == '\r' || raw[index] == '\n') {
		index++
	}
	if index >= len(raw) || raw[index] != ':' {
		return "", false, false
	}
	index++
	for index < len(raw) && (raw[index] == ' ' || raw[index] == '\t' || raw[index] == '\r' || raw[index] == '\n') {
		index++
	}
	if index >= len(raw) || raw[index] != '"' {
		return "", false, false
	}
	started = true
	index++

	var decoded strings.Builder
	for index < len(raw) {
		if raw[index] == '"' {
			return decoded.String(), true, true
		}
		if raw[index] != '\\' {
			r, size := utf8.DecodeRuneInString(raw[index:])
			if r == utf8.RuneError && size == 1 {
				break
			}
			decoded.WriteRune(r)
			index += size
			continue
		}
		if index+1 >= len(raw) {
			break
		}
		escape := raw[index+1]
		switch escape {
		case '"', '\\', '/':
			decoded.WriteByte(escape)
			index += 2
		case 'b':
			decoded.WriteByte('\b')
			index += 2
		case 'f':
			decoded.WriteByte('\f')
			index += 2
		case 'n':
			decoded.WriteByte('\n')
			index += 2
		case 'r':
			decoded.WriteByte('\r')
			index += 2
		case 't':
			decoded.WriteByte('\t')
			index += 2
		case 'u':
			r, consumed, ok := decodeJSONUnicodeEscape(raw[index:])
			if !ok {
				return decoded.String(), true, false
			}
			decoded.WriteRune(r)
			index += consumed
		default:
			return decoded.String(), true, false
		}
	}
	return decoded.String(), true, false
}

func decodeJSONUnicodeEscape(value string) (rune, int, bool) {
	if len(value) < 6 || value[0] != '\\' || value[1] != 'u' {
		return 0, 0, false
	}
	bytes, err := hex.DecodeString(value[2:6])
	if err != nil {
		return 0, 0, false
	}
	first := rune(bytes[0])<<8 | rune(bytes[1])
	if first < 0xD800 || first > 0xDBFF {
		return first, 6, true
	}
	if len(value) < 12 || value[6] != '\\' || value[7] != 'u' {
		return 0, 0, false
	}
	bytes, err = hex.DecodeString(value[8:12])
	if err != nil {
		return 0, 0, false
	}
	second := rune(bytes[0])<<8 | rune(bytes[1])
	r := utf16.DecodeRune(first, second)
	if r == utf8.RuneError {
		return 0, 0, false
	}
	return r, 12, true
}
