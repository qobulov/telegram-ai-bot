package telegram

import (
	"math"
	"testing"
)

func TestEntityText(t *testing.T) {
	cases := map[string]struct {
		text   string
		entity MessageEntity
		want   string
		ok     bool
	}{
		"at the start":             {"@bot hi", MessageEntity{Offset: 0, Length: 4}, "@bot", true},
		"at the end":               {"hi @bot", MessageEntity{Offset: 3, Length: 4}, "@bot", true},
		"after emoji and cyrillic": {"😀😀 привет @bot hi", MessageEntity{Offset: 12, Length: 4}, "@bot", true},
		"offset past the end":      {"@bot", MessageEntity{Offset: 10, Length: 4}, "", false},
		"length past the end":      {"@bot", MessageEntity{Offset: 2, Length: 10}, "", false},
		"negative offset":          {"@bot", MessageEntity{Offset: -1, Length: 2}, "", false},
		"zero length":              {"@bot", MessageEntity{Offset: 0, Length: 0}, "", false},
		"empty text":               {"", MessageEntity{Offset: 0, Length: 1}, "", false},
		"offset that overflows":    {"@bot", MessageEntity{Offset: math.MaxInt, Length: 1}, "", false},
		"length that overflows":    {"@bot", MessageEntity{Offset: 1, Length: math.MaxInt}, "", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := EntityText(tc.text, tc.entity)
			if got != tc.want || ok != tc.ok {
				t.Errorf("EntityText = %q, %v; want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestRemoveEntities(t *testing.T) {
	cases := map[string]struct {
		text   string
		remove []MessageEntity
		want   string
	}{
		"leading mention":         {"@bot hi", []MessageEntity{{Offset: 0, Length: 4}}, "hi"},
		"trailing mention":        {"hi @bot", []MessageEntity{{Offset: 3, Length: 4}}, "hi"},
		"mention in the middle":   {"hey @bot what is Go", []MessageEntity{{Offset: 4, Length: 4}}, "hey what is Go"},
		"two mentions":            {"@bot hi @bot", []MessageEntity{{Offset: 0, Length: 4}, {Offset: 8, Length: 4}}, "hi"},
		"emoji before mention":    {"😀 @bot привет", []MessageEntity{{Offset: 3, Length: 4}}, "😀 привет"},
		"out of range is ignored": {"hello", []MessageEntity{{Offset: 3, Length: 40}}, "hello"},
		"nothing to remove":       {"  hello  ", nil, "hello"},
		"keeps inner line breaks": {"@bot\nline one\n\nline two", []MessageEntity{{Offset: 0, Length: 4}}, "line one\n\nline two"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := RemoveEntities(tc.text, tc.remove); got != tc.want {
				t.Errorf("RemoveEntities = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestContentPrefersTextThenCaption(t *testing.T) {
	textEntities := []MessageEntity{{Type: EntityMention, Offset: 0, Length: 4}}
	captionEntities := []MessageEntity{{Type: EntityMention, Offset: 2, Length: 4}}

	msg := &Message{Text: "@bot hi", Entities: textEntities, Caption: "c @bot", CaptionEntities: captionEntities}
	if text, entities := msg.Content(); text != "@bot hi" || entities[0].Offset != 0 {
		t.Errorf("Content = %q, %+v", text, entities)
	}

	photo := &Message{Caption: "c @bot", CaptionEntities: captionEntities}
	if text, entities := photo.Content(); text != "c @bot" || len(entities) != 1 || entities[0].Offset != 2 {
		t.Errorf("Content = %q, %+v", text, entities)
	}
}
