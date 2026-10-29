package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"slices"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

type fakeBusiness struct {
	connection storage.BusinessConnection
	missing    bool
	chats      []storage.BusinessChat
	saved      []storage.BusinessChecklist

	stories    []telegram.Story
	sent       []telegram.Checklist
	edited     map[int64]telegram.Checklist
	sendErr    error
	nextSentID int64
}

func (f *fakeBusiness) OwnerBusinessConnection(_ context.Context, userID int64) (storage.BusinessConnection, error) {
	if f.missing || f.connection.UserID != userID {
		return storage.BusinessConnection{}, storage.ErrUnknownBusinessConnection
	}
	return f.connection, nil
}

func (f *fakeBusiness) BusinessChats(_ context.Context, connectionID string, limit int) ([]storage.BusinessChat, error) {
	if connectionID != f.connection.ID {
		return nil, nil
	}
	return f.chats[:min(limit, len(f.chats))], nil
}

func (f *fakeBusiness) SaveBusinessChecklist(_ context.Context, c storage.BusinessChecklist) error {
	f.saved = append(f.saved, c)
	return nil
}

func (f *fakeBusiness) LastBusinessChecklist(_ context.Context, connectionID string, chatID int64) (storage.BusinessChecklist, error) {
	for _, c := range slices.Backward(f.saved) {
		if c.ConnectionID == connectionID && c.ChatID == chatID {
			return c, nil
		}
	}
	return storage.BusinessChecklist{}, storage.ErrNoChecklist
}

func (f *fakeBusiness) PostStory(_ context.Context, story telegram.Story) (int64, error) {
	f.stories = append(f.stories, story)
	return 5, f.sendErr
}

func (f *fakeBusiness) SendChecklist(_ context.Context, c telegram.Checklist) (int64, error) {
	if f.sendErr != nil {
		return 0, f.sendErr
	}
	f.sent = append(f.sent, c)
	f.nextSentID++
	return 100 + f.nextSentID, nil
}

func (f *fakeBusiness) EditMessageChecklist(_ context.Context, messageID int64, c telegram.Checklist) error {
	if f.edited == nil {
		f.edited = map[int64]telegram.Checklist{}
	}
	f.edited[messageID] = c
	return f.sendErr
}

func newFakeBusiness() *fakeBusiness {
	return &fakeBusiness{
		connection: storage.BusinessConnection{ID: "bc1", UserID: userID, Enabled: true, CanReply: true, CanManageStories: true},
		chats: []storage.BusinessChat{
			{ConnectionID: "bc1", ChatID: 4201, FirstName: "Ali", Username: "ali_shop", LastMessageAt: time.Now()},
			{ConnectionID: "bc1", ChatID: 4202, FirstName: "Vali", LastMessageAt: time.Now()},
			{ConnectionID: "bc1", ChatID: 4203, FirstName: "Vali", LastMessageAt: time.Now()},
		},
	}
}

func pngPicture(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, color.RGBA{R: 200, G: uint8(x), B: uint8(y), A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func businessTool(t *testing.T, tool Tool, caller Caller, sink *Sink, args string) envelope {
	t.Helper()
	registry := mustRegistry(t, tool)
	var env envelope
	_ = json.Unmarshal([]byte(registry.Execute(WithTurn(context.Background(), caller, sink), caller.UserID, tool.Name, args)), &env)
	return env
}

func storyTool(f *fakeBusiness) Tool {
	return StoryTool(StoryOptions{Accounts: f, Poster: f})
}

func TestPostStoryPostsTheOwnersPicture(t *testing.T) {
	f := newFakeBusiness()
	owner := botOwner
	owner.Picture = pngPicture(t, 64, 32)

	env := businessTool(t, storyTool(f), owner, &Sink{}, `{"caption":"Yangi **kolleksiya**","hours":12}`)
	if !env.OK || len(f.stories) != 1 {
		t.Fatalf("env = %+v, stories = %d", env, len(f.stories))
	}
	story := f.stories[0]
	if story.BusinessConnectionID != "bc1" || story.ActivePeriod != 12*3600 || story.Caption != "Yangi **kolleksiya**" {
		t.Errorf("story = %+v", story)
	}
	got, err := jpeg.Decode(bytes.NewReader(story.Photo))
	if err != nil || got.Bounds().Dx() != telegram.StoryWidth || got.Bounds().Dy() != telegram.StoryHeight {
		t.Fatalf("story picture: %v, %v", got.Bounds(), err)
	}
}

// A picture drawn in the same turn is the story, before the one replied to,
// and it stays in the sink to be sent to the owner with the answer.
func TestPostStoryPrefersTheDrawingOfTheTurn(t *testing.T) {
	f := newFakeBusiness()
	owner := botOwner
	owner.Picture = []byte("not a picture")
	sink := &Sink{}
	drawn := pngPicture(t, 20, 40)
	sink.Add(Drawing{Data: drawn, MimeType: "image/png"})

	if env := businessTool(t, storyTool(f), owner, sink, `{}`); !env.OK || len(f.stories) != 1 {
		t.Fatalf("env = %+v", env)
	}
	if f.stories[0].ActivePeriod != 24*3600 {
		t.Errorf("default period = %d", f.stories[0].ActivePeriod)
	}
	if drawings := sink.Take(); len(drawings) != 1 {
		t.Errorf("the drawing left the sink: %d", len(drawings))
	}
}

func TestPostStoryRefusals(t *testing.T) {
	cases := map[string]struct {
		change func(*fakeBusiness, *Caller)
		args   string
		want   string
	}{
		"a member":             {func(_ *fakeBusiness, c *Caller) { c.Owner = false }, `{}`, "restricted"},
		"an owner in a group":  {func(_ *fakeBusiness, c *Caller) { c.ChatID = groupA }, `{}`, businessOwnersOnly},
		"a business chat":      {func(_ *fakeBusiness, c *Caller) { c.Business = true }, `{}`, businessOwnersOnly},
		"no account":           {func(f *fakeBusiness, _ *Caller) { f.missing = true }, `{}`, "no business account"},
		"no story right":       {func(f *fakeBusiness, _ *Caller) { f.connection.CanManageStories = false }, `{}`, "managing stories"},
		"no picture":           {func(_ *fakeBusiness, c *Caller) { c.Picture = nil }, `{}`, "no picture"},
		"an unreadable one":    {func(_ *fakeBusiness, c *Caller) { c.Picture = []byte("text") }, `{}`, "cannot be used"},
		"a period not allowed": {func(*fakeBusiness, *Caller) {}, `{"hours":3}`, "hours must be"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeBusiness()
			owner := botOwner
			owner.Picture = pngPicture(t, 8, 8)
			c.change(f, &owner)
			env := businessTool(t, storyTool(f), owner, &Sink{}, c.args)
			if env.OK || !strings.Contains(env.Error.Message, c.want) || len(f.stories) != 0 {
				t.Errorf("env = %+v, stories = %d; want a refusal with %q", env, len(f.stories), c.want)
			}
		})
	}
}

func TestStoryPictureKeepsTheWholePicture(t *testing.T) {
	for _, size := range [][2]int{{400, 100}, {100, 400}, {1080, 1920}, {3000, 3000}} {
		out, err := storyPicture(pngPicture(t, size[0], size[1]))
		if err != nil {
			t.Fatalf("%v: %v", size, err)
		}
		img, err := jpeg.Decode(bytes.NewReader(out))
		if err != nil || img.Bounds() != image.Rect(0, 0, telegram.StoryWidth, telegram.StoryHeight) {
			t.Errorf("%v: %v, %v", size, img.Bounds(), err)
		}
	}
	if got := fitRect(image.Rect(0, 0, 400, 100), 1080, 1920); got != image.Rect(0, 825, 1080, 1095) {
		t.Errorf("fitRect wide = %v", got)
	}
	if got := coverRect(image.Rect(0, 0, 400, 100), 1080, 1920); got.Dy() != 100 || got.Dx() != 56 || got.Min.X != 172 {
		t.Errorf("coverRect wide = %v", got)
	}
}

func checklistTool(f *fakeBusiness) Tool {
	return ChecklistTool(ChecklistOptions{Accounts: f, Sender: f, Location: time.UTC})
}

func TestBusinessChecklistSendsAndEdits(t *testing.T) {
	f := newFakeBusiness()
	tool := checklistTool(f)

	env := businessTool(t, tool, botOwner, &Sink{}, `{"action":"send","chat":"@ALI_shop","title":"Buyurtma","tasks":["To'lov"," ","Yetkazish"]}`)
	if !env.OK || len(f.sent) != 1 {
		t.Fatalf("send: %+v", env)
	}
	sent := f.sent[0]
	if sent.BusinessConnectionID != "bc1" || sent.ChatID != 4201 || sent.Title != "Buyurtma" || !slices.Equal(sent.Tasks, []string{"To'lov", "Yetkazish"}) {
		t.Errorf("sent = %+v", sent)
	}
	if len(f.saved) != 1 || f.saved[0].MessageID != 101 {
		t.Fatalf("saved = %+v", f.saved)
	}

	env = businessTool(t, tool, botOwner, &Sink{}, `{"action":"edit","chat":"4201","tasks":["To'lov","Yetkazish","Chek"]}`)
	edited, ok := f.edited[101]
	if !env.OK || !ok || edited.Title != "Buyurtma" || len(edited.Tasks) != 3 || edited.ChatID != 4201 {
		t.Fatalf("edit: %+v, %+v", env, f.edited)
	}
	if last := f.saved[len(f.saved)-1]; last.MessageID != 101 || len(last.Tasks) != 3 {
		t.Errorf("the edit was not kept: %+v", last)
	}
}

func TestBusinessChecklistListsTheChats(t *testing.T) {
	f := newFakeBusiness()
	env := businessTool(t, checklistTool(f), botOwner, &Sink{}, `{"action":"chats"}`)
	if !env.OK || !strings.Contains(string(env.Result), `"chat_id":4201`) || !strings.Contains(string(env.Result), "ali_shop") {
		t.Errorf("chats = %+v", env)
	}
}

func TestBusinessChecklistRefusals(t *testing.T) {
	cases := map[string]struct {
		change func(*fakeBusiness)
		caller Caller
		args   string
		want   string
	}{
		"a member":        {nil, Caller{UserID: userID, ChatID: userID}, `{"action":"chats"}`, "restricted"},
		"no account":      {func(f *fakeBusiness) { f.missing = true }, botOwner, `{"action":"chats"}`, "no business account"},
		"no reply right":  {func(f *fakeBusiness) { f.connection.CanReply = false }, botOwner, `{"action":"send","chat":"Ali","title":"T","tasks":["a"]}`, "replying"},
		"no chat named":   {nil, botOwner, `{"action":"send","title":"T","tasks":["a"]}`, "whose chat"},
		"a stranger":      {nil, botOwner, `{"action":"send","chat":"Bek","title":"T","tasks":["a"]}`, "no customer chat"},
		"two Valis":       {nil, botOwner, `{"action":"send","chat":"vali","title":"T","tasks":["a"]}`, "several customers"},
		"no tasks":        {nil, botOwner, `{"action":"send","chat":"Ali","title":"T","tasks":[]}`, "1 to 30 tasks"},
		"no title":        {nil, botOwner, `{"action":"send","chat":"Ali","tasks":["a"]}`, "needs a title"},
		"a long task":     {nil, botOwner, `{"action":"send","chat":"Ali","title":"T","tasks":["` + strings.Repeat("x", 101) + `"]}`, "at most 100"},
		"nothing to edit": {nil, botOwner, `{"action":"edit","chat":"Ali","tasks":["a"]}`, "no checklist"},
		"Telegram refuses": {func(f *fakeBusiness) {
			f.sendErr = &telegram.APIError{Code: 400, Description: "Bad Request: BUSINESS_PEER_USAGE_MISSING"}
		}, botOwner, `{"action":"send","chat":"Ali","title":"T","tasks":["a"]}`, "BUSINESS_PEER_USAGE_MISSING"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeBusiness()
			if c.change != nil {
				c.change(f)
			}
			env := businessTool(t, checklistTool(f), c.caller, &Sink{}, c.args)
			if env.OK || !strings.Contains(env.Error.Message, c.want) || len(f.sent) != 0 {
				t.Errorf("env = %+v; want a refusal with %q", env, c.want)
			}
		})
	}
}

// The tools that send to the chat themselves, as the bot, or keep what they
// learn for the person's own chats are not offered to a customer.
func TestNoBusinessToolsAreNotOfferedInBusinessChats(t *testing.T) {
	registry := mustRegistry(t,
		Tool{Name: "dice_like", NoBusiness: true, Run: func(context.Context, json.RawMessage) (any, error) { return "rolled", nil }},
		Tool{Name: "anywhere", Run: func(context.Context, json.RawMessage) (any, error) { return "ok", nil }},
	)
	customer := WithTurn(context.Background(), Caller{UserID: userID, ChatID: 4201, Business: true}, &Sink{})
	names := func(ctx context.Context) []string {
		var got []string
		for _, d := range registry.Definitions(ctx, userID) {
			got = append(got, d.Function.Name)
		}
		return got
	}
	if got := names(customer); !slices.Equal(got, []string{"anywhere"}) {
		t.Errorf("offered to a customer: %v", got)
	}
	if env := decodeEnvelope(t, registry.Execute(customer, userID, "dice_like", `{}`)); env.OK {
		t.Errorf("ran for a customer: %+v", env)
	}
	private := WithTurn(context.Background(), Caller{UserID: userID, ChatID: userID}, &Sink{})
	if got := names(private); len(got) != 2 {
		t.Errorf("offered in a private chat: %v", got)
	}
}
