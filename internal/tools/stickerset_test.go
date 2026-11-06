package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"slices"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/stickers"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

const ownSet = "maria_by_mariabot"

type fakeStickerAPI struct {
	calls    []string
	uploaded []byte
	// createErr and addErr are Telegram's refusals of the set's calls.
	createErr, addErr error
	set               telegram.StickerSet
	setErr            error
}

func (f *fakeStickerAPI) UploadStickerFile(_ context.Context, userID int64, png []byte) (string, error) {
	f.calls = append(f.calls, fmt.Sprintf("upload as %d", userID))
	f.uploaded = png
	return "up1", nil
}

func (f *fakeStickerAPI) CreateNewStickerSet(_ context.Context, userID int64, name, title string, first telegram.InputSticker) error {
	f.calls = append(f.calls, fmt.Sprintf("create %s %q as %d: %s %v", name, title, userID, first.FileID, first.Emoji))
	if f.createErr == nil {
		f.set.Stickers = append(f.set.Stickers, telegram.Sticker{FileID: "new", FileUniqueID: "unew", SetName: name})
	}
	return f.createErr
}

func (f *fakeStickerAPI) AddStickerToSet(_ context.Context, userID int64, name string, sticker telegram.InputSticker) error {
	f.calls = append(f.calls, fmt.Sprintf("add to %s as %d: %s %v", name, userID, sticker.FileID, sticker.Emoji))
	if f.addErr == nil {
		f.set.Stickers = append(f.set.Stickers, telegram.Sticker{FileID: "new", FileUniqueID: "unew", SetName: name})
	}
	return f.addErr
}

func (f *fakeStickerAPI) DeleteStickerFromSet(_ context.Context, fileID string) error {
	f.calls = append(f.calls, "delete "+fileID)
	return nil
}

func (f *fakeStickerAPI) SetStickerSetTitle(_ context.Context, name, title string) error {
	f.calls = append(f.calls, fmt.Sprintf("title %s %q", name, title))
	return nil
}

func (f *fakeStickerAPI) GetStickerSet(_ context.Context, name string) (telegram.StickerSet, error) {
	f.set.Name = name
	return f.set, f.setErr
}

func (f *fakeStickerAPI) SendSticker(_ context.Context, chatID int64, fileID string, replyTo int64) (int64, error) {
	f.calls = append(f.calls, fmt.Sprintf("send %s to %d in reply to %d", fileID, chatID, replyTo))
	return 99, nil
}

type fakeStickerStore struct {
	owner     int64
	saved     []string
	refuse    bool
	limit     int
	reserved  int
	released  []int64
	forgotten []string
}

func (f *fakeStickerStore) OwnStickerSetOwner(_ context.Context, name string) (int64, error) {
	if f.owner == 0 {
		return 0, storage.ErrNoStickerSet
	}
	return f.owner, nil
}

func (f *fakeStickerStore) SaveOwnStickerSet(_ context.Context, name string, ownerID int64) error {
	f.saved = append(f.saved, fmt.Sprintf("%s %d", name, ownerID))
	if f.owner == 0 {
		f.owner = ownerID
	}
	return nil
}

func (f *fakeStickerStore) ReserveStickerUpload(_ context.Context, _, _ int64, _ time.Time, limit int) (int64, bool, error) {
	f.limit = limit
	if f.refuse {
		return 0, false, nil
	}
	f.reserved++
	return 7, true, nil
}

func (f *fakeStickerStore) ReleaseStickerUpload(_ context.Context, id int64) error {
	f.released = append(f.released, id)
	return nil
}

func (f *fakeStickerStore) ForgetSticker(_ context.Context, fileUniqueID string) error {
	f.forgotten = append(f.forgotten, fileUniqueID)
	return nil
}

type fakeLearner struct{ packs []string }

func (f *fakeLearner) Import(_ context.Context, name string) (stickers.Report, error) {
	f.packs = append(f.packs, name)
	return stickers.Report{Pack: name}, nil
}

// pictureDrawer draws a red square on white.
type pictureDrawer struct{ prompts []string }

func (d *pictureDrawer) Generate(_ context.Context, prompt string) (ai.Image, error) {
	d.prompts = append(d.prompts, prompt)
	return ai.Image{Data: testPicture(800, 800), MimeType: "image/png"}, nil
}

// testPicture is a w by h PNG: white, with a red square in the middle.
func testPicture(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{255, 255, 255, 255}
			if x > w/4 && x < 3*w/4 && y > h/4 && y < 3*h/4 {
				c = color.RGBA{220, 20, 20, 255}
			}
			img.Set(x, y, c)
		}
	}
	var out bytes.Buffer
	_ = png.Encode(&out, img)
	return out.Bytes()
}

type stickerSetFixture struct {
	api     *fakeStickerAPI
	store   *fakeStickerStore
	learner *fakeLearner
	drawer  *pictureDrawer
	tool    Tool
}

func newStickerSetFixture() *stickerSetFixture {
	f := &stickerSetFixture{api: &fakeStickerAPI{}, store: &fakeStickerStore{}, learner: &fakeLearner{}, drawer: &pictureDrawer{}}
	f.tool = StickerSetTool(StickerSetOptions{
		Telegram: f.api, Store: f.store, Learner: f.learner, Drawer: f.drawer,
		BotUsername: "mariabot", Title: "Maria",
	})
	return f
}

func (f *stickerSetFixture) run(t *testing.T, caller Caller, args string) (envelope, *Sink) {
	t.Helper()
	sink := &Sink{}
	var env envelope
	out := mustRegistry(t, f.tool).Execute(WithTurn(context.Background(), caller, sink), caller.UserID, "sticker_set", args)
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("result %s: %v", out, err)
	}
	return env, sink
}

const secondOwner = int64(3)

func stickerOwner(id int64) Caller {
	return Caller{UserID: id, ChatID: groupA, Owner: true, Human: true, UpdateID: 1, MessageID: 50}
}

// Only owners have the set: the tool is not offered to anyone else, and
// is refused to them when called.
func TestStickerSetIsOwnersOnly(t *testing.T) {
	f := newStickerSetFixture()
	member := Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: 1, Picture: testPicture(10, 10)}
	if defs := mustRegistry(t, f.tool).Definitions(WithTurn(context.Background(), member, &Sink{}), userID); len(defs) != 0 {
		t.Errorf("offered to a member: %+v", defs)
	}
	if env, _ := f.run(t, member, `{"action":"add","emoji":["😂"]}`); env.OK || env.Error.Code != codeForbidden || len(f.api.calls) != 0 {
		t.Errorf("a member added a sticker: %+v, %q", env, f.api.calls)
	}
}

// The first sticker makes the set, as the asking owner's; it is sent to
// the chat, ends the turn with the link, and is looked at to be sent later.
func TestTheFirstStickerMakesTheSet(t *testing.T) {
	f := newStickerSetFixture()
	caller := stickerOwner(userID)
	caller.Picture = testPicture(1000, 500)

	env, sink := f.run(t, caller, `{"action":"add","emoji":["😂"," 🔥 ","😂"]}`)

	if !env.OK {
		t.Fatalf("env = %+v", env)
	}
	want := []string{
		"upload as 2",
		`create maria_by_mariabot "Maria" as 2: up1 [😂 🔥]`,
		"send new to " + fmt.Sprint(groupA) + " in reply to 50",
	}
	if !slices.Equal(f.api.calls, want) {
		t.Errorf("calls = %q, want %q", f.api.calls, want)
	}
	if !slices.Equal(f.store.saved, []string{ownSet + " 2"}) || f.store.limit != stickersPerDay {
		t.Errorf("saved %q, limit %d", f.store.saved, f.store.limit)
	}
	sticker, err := png.DecodeConfig(bytes.NewReader(f.api.uploaded))
	if err != nil || sticker.Width != 512 || sticker.Height != 256 {
		t.Errorf("uploaded %+v, %v; want a 512 by 256 PNG", sticker, err)
	}
	if text, ok := sink.Finished(); !ok || !strings.Contains(text, "https://t.me/addstickers/"+ownSet) {
		t.Errorf("answer = %q, %v", text, ok)
	}
	if !slices.Equal(f.learner.packs, []string{ownSet}) {
		t.Errorf("learned %q", f.learner.packs)
	}
}

// A later sticker is added as the owner the set belongs to, whoever asks.
func TestLaterStickersAreAddedAsTheSetsOwner(t *testing.T) {
	f := newStickerSetFixture()
	f.store.owner = userID
	caller := stickerOwner(secondOwner)
	caller.Picture = testPicture(300, 600)

	if env, _ := f.run(t, caller, `{"action":"add","emoji":["🥲"]}`); !env.OK {
		t.Fatalf("env = %+v", env)
	}
	if f.api.calls[0] != "upload as 2" || f.api.calls[1] != "add to maria_by_mariabot as 2: up1 [🥲]" || len(f.store.saved) != 0 {
		t.Errorf("calls = %q, saved %q", f.api.calls, f.store.saved)
	}
}

// Asked to draw, the bot draws a sticker and cuts the white around it.
func TestADrawnStickerHasNoWhiteBox(t *testing.T) {
	f := newStickerSetFixture()
	f.store.owner = userID

	env, _ := f.run(t, stickerOwner(userID), `{"action":"add","emoji":["🐱"],"prompt":"a grumpy cat"}`)

	if !env.OK || len(f.drawer.prompts) != 1 || !strings.HasPrefix(f.drawer.prompts[0], "a grumpy cat") || !strings.Contains(f.drawer.prompts[0], "white background") {
		t.Fatalf("env = %+v, prompts %q", env, f.drawer.prompts)
	}
	img, err := png.Decode(bytes.NewReader(f.api.uploaded))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := img.At(2, 2).RGBA(); a != 0 {
		t.Errorf("the corner is not transparent: alpha %d", a)
	}
	if _, _, _, a := img.At(256, 256).RGBA(); a != 0xFFFF {
		t.Errorf("the drawing was cut away: alpha %d", a)
	}
}

// What the model or the owner gave wrong is refused before anything is
// counted or sent.
func TestStickerSetRefusesBadRequests(t *testing.T) {
	withPicture := stickerOwner(userID)
	withPicture.Picture = testPicture(64, 64)
	for name, tc := range map[string]struct {
		caller Caller
		args   string
		want   string
	}{
		"no emoji":       {withPicture, `{"action":"add"}`, "emoji: give 1 to 20"},
		"letters":        {withPicture, `{"action":"add","emoji":["lol"]}`, "not one emoji"},
		"no picture":     {stickerOwner(userID), `{"action":"add","emoji":["😂"]}`, "no picture"},
		"not a picture":  {Caller{UserID: userID, ChatID: groupA, Owner: true, Human: true, Picture: []byte("text")}, `{"action":"add","emoji":["😂"]}`, "cannot be read"},
		"no reply":       {stickerOwner(userID), `{"action":"remove"}`, "reply to the sticker"},
		"long title":     {stickerOwner(userID), `{"action":"title","title":"` + strings.Repeat("a", 65) + `"}`, "1 to 64"},
		"unknown action": {stickerOwner(userID), `{"action":"rename"}`, "action must be"},
	} {
		f := newStickerSetFixture()
		f.store.owner = userID
		env, _ := f.run(t, tc.caller, tc.args)
		if env.OK || !strings.Contains(env.Error.Message, tc.want) {
			t.Errorf("%s: %+v, want %q", name, env, tc.want)
		}
		if slices.ContainsFunc(f.api.calls, func(c string) bool { return !strings.HasPrefix(c, "upload") }) {
			t.Errorf("%s: calls %q", name, f.api.calls)
		}
		if f.store.reserved != len(f.store.released) {
			t.Errorf("%s: %d reserved, %d released", name, f.store.reserved, len(f.store.released))
		}
	}
}

// An owner who added their stickers for today is told so; a sticker
// Telegram refused is not counted, and its reason is passed on.
func TestStickerSetLimitsAndRefusals(t *testing.T) {
	caller := stickerOwner(userID)
	caller.Picture = testPicture(64, 64)

	f := newStickerSetFixture()
	f.store.refuse = true
	if env, _ := f.run(t, caller, `{"action":"add","emoji":["😂"]}`); env.OK || env.Error.Code != codeDailyLimit || len(f.api.calls) != 0 {
		t.Errorf("over the limit: %+v, %q", env, f.api.calls)
	}

	f = newStickerSetFixture()
	f.store.owner = userID
	f.api.addErr = &telegram.APIError{Code: 400, Description: "Bad Request: STICKERS_TOO_MUCH"}
	env, _ := f.run(t, caller, `{"action":"add","emoji":["😂"]}`)
	if env.OK || !strings.Contains(env.Error.Message, "STICKERS_TOO_MUCH") || !slices.Equal(f.store.released, []int64{7}) {
		t.Errorf("refused: %+v, released %v", env, f.store.released)
	}
}

// A set Telegram has but the database lost is added to, and remembered.
func TestAnOccupiedSetNameIsAddedTo(t *testing.T) {
	f := newStickerSetFixture()
	f.api.createErr = &telegram.APIError{Code: 400, Description: "Bad Request: sticker set name is already occupied"}
	caller := stickerOwner(userID)
	caller.Picture = testPicture(64, 64)

	if env, _ := f.run(t, caller, `{"action":"add","emoji":["😂"]}`); !env.OK {
		t.Fatalf("env = %+v", env)
	}
	if f.api.calls[2] != "add to maria_by_mariabot as 2: up1 [😂]" || !slices.Equal(f.store.saved, []string{ownSet + " 2"}) {
		t.Errorf("calls = %q, saved %q", f.api.calls, f.store.saved)
	}
}

// Only a sticker of the set, replied to, is taken out, and the bot forgets
// it.
func TestStickerSetRemovesTheRepliedSticker(t *testing.T) {
	f := newStickerSetFixture()
	caller := stickerOwner(userID)
	caller.Target = &Target{MessageID: 40, Sticker: &telegram.Sticker{FileID: "f9", FileUniqueID: "u9", SetName: "otherpack"}}
	if env, _ := f.run(t, caller, `{"action":"remove"}`); env.OK || !strings.Contains(env.Error.Message, "not from my set") || len(f.api.calls) != 0 {
		t.Errorf("another pack's sticker: %+v, %q", env, f.api.calls)
	}

	caller.Target.Sticker.SetName = ownSet
	if env, _ := f.run(t, caller, `{"action":"remove"}`); !env.OK {
		t.Fatalf("env = %+v", env)
	}
	if !slices.Equal(f.api.calls, []string{"delete f9"}) || !slices.Equal(f.store.forgotten, []string{"u9"}) {
		t.Errorf("calls %q, forgotten %q", f.api.calls, f.store.forgotten)
	}
}

// The set is renamed once it exists, and its link shows its size.
func TestStickerSetTitleAndLink(t *testing.T) {
	f := newStickerSetFixture()
	if env, _ := f.run(t, stickerOwner(userID), `{"action":"title","title":"Maria"}`); env.OK || !strings.Contains(env.Error.Message, "no sticker set yet") {
		t.Errorf("before the set: %+v", env)
	}
	f.store.owner = userID
	if env, _ := f.run(t, stickerOwner(secondOwner), `{"action":"title","title":" Maria stikerlari "}`); !env.OK || !slices.Equal(f.api.calls, []string{`title maria_by_mariabot "Maria stikerlari"`}) {
		t.Errorf("title: %+v, %q", env, f.api.calls)
	}

	f.api.set = telegram.StickerSet{Title: "Maria stikerlari", Stickers: make([]telegram.Sticker, 3)}
	env, _ := f.run(t, stickerOwner(userID), `{"action":"link"}`)
	var got struct {
		Link     string `json:"link"`
		Title    string `json:"title"`
		Stickers int    `json:"stickers"`
	}
	_ = json.Unmarshal(env.Result, &got)
	if !env.OK || got.Link != "https://t.me/addstickers/"+ownSet || got.Stickers != 3 || got.Title != "Maria stikerlari" {
		t.Errorf("link: %+v, %+v", env, got)
	}

	f.api.setErr = &telegram.APIError{Code: 400, Description: "Bad Request: STICKERSET_INVALID"}
	if env, _ := f.run(t, stickerOwner(userID), `{"action":"link"}`); env.OK || !strings.Contains(env.Error.Message, "no sticker set yet") {
		t.Errorf("no set on Telegram: %+v", env)
	}
}

func TestIsEmoji(t *testing.T) {
	for _, e := range []string{"😂", "❤️", "👍🏽", "🇺🇿", "👨‍👩‍👧"} {
		if !isEmoji(e) {
			t.Errorf("%q is an emoji", e)
		}
	}
	for _, e := range []string{"", "a", "lol", "😂 lol", "1", " ", "️"} {
		if isEmoji(e) {
			t.Errorf("%q is no emoji", e)
		}
	}
}
