package server

import (
	"context"
	"strings"
	"testing"
)

func TestPartialJSONStringFieldHandlesSplitUnicodeEscape(t *testing.T) {
	value, started, complete := partialJSONStringField(`{"reply":"豆豆说\u4f6`, "reply")
	if value != "豆豆说" || !started || complete {
		t.Fatalf("partial value = %q, started=%v complete=%v", value, started, complete)
	}
	value, started, complete = partialJSONStringField(`{"reply":"豆豆说\u4f60好。"}`, "reply")
	if value != "豆豆说你好。" || !started || !complete {
		t.Fatalf("complete value = %q, started=%v complete=%v", value, started, complete)
	}
}

func TestStructuredReplyStreamReleasesCompleteSentenceEarly(t *testing.T) {
	stream := &structuredReplyStream{}
	if got := stream.Append(`{"kind":"chat","activity_id":"","reply":"云朵像`); len(got) != 0 {
		t.Fatalf("first chunk emitted %#v", got)
	}
	got := stream.Append(`棉花糖。它在天上`)
	if len(got) != 1 || got[0] != "云朵像棉花糖。" {
		t.Fatalf("complete sentence = %#v", got)
	}
	if got := stream.Append(`慢慢散步。"}`); len(got) != 1 || got[0] != "它在天上慢慢散步。" {
		t.Fatalf("second sentence = %#v", got)
	}
	if got := stream.Flush(); len(got) != 0 {
		t.Fatalf("flush = %#v", got)
	}
}

func TestServerStreamsChatSentencesBeforeStructuredResponseCompletes(t *testing.T) {
	provider := &fakeStreamingStructuredChat{}
	srv := New(Config{Chat: provider, XiaozhiStreaming: true})
	var sentences []string
	result := srv.streamReply(
		context.Background(),
		"云朵像什么",
		nil,
		func(sentence string) error {
			sentences = append(sentences, sentence)
			return nil
		},
		nil,
	)
	if result.err != nil {
		t.Fatalf("streamReply: %v", result.err)
	}
	if !provider.firstSentenceObserved || strings.Join(sentences, "") != "云朵像棉花糖。它在天上慢慢散步。" {
		t.Fatalf("sentences = %#v, observed early=%v", sentences, provider.firstSentenceObserved)
	}
	if result.source != provider.Name() || result.reply == "" {
		t.Fatalf("result = %+v", result)
	}
}

type fakeStreamingStructuredChat struct {
	firstSentenceObserved bool
}

func (*fakeStreamingStructuredChat) Available() bool   { return true }
func (*fakeStreamingStructuredChat) Name() string      { return "stream-test" }
func (*fakeStreamingStructuredChat) ChatModel() string { return "stream-test-model" }
func (*fakeStreamingStructuredChat) CreateResponse(context.Context, string, string) (string, error) {
	return "", nil
}
func (*fakeStreamingStructuredChat) CreateStructuredResponse(context.Context, string, string) (string, error) {
	return "", nil
}
func (p *fakeStreamingStructuredChat) StreamStructuredResponse(
	_ context.Context,
	_ string,
	_ string,
	onDelta func(string) error,
) (string, error) {
	parts := []string{
		`{"kind":"chat","activity_id":"","reply":"云朵像`,
		`棉花糖。它在天上`,
		`慢慢散步。"}`,
	}
	var raw strings.Builder
	for index, part := range parts {
		raw.WriteString(part)
		if err := onDelta(part); err != nil {
			return raw.String(), err
		}
		if index == 1 {
			p.firstSentenceObserved = true
		}
	}
	return raw.String(), nil
}
