package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "123456:test-secret-token"

type call struct {
	method string
	params map[string]any
}

// fakeAPI is a scripted Telegram Bot API. Each request consumes the next
// reply; the last reply repeats.
type fakeAPI struct {
	mu      sync.Mutex
	replies []string
	calls   []call
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var params map[string]any
	body, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(body, &params)
	method := strings.TrimPrefix(r.URL.Path, "/bot"+testToken+"/")
	f.calls = append(f.calls, call{method: method, params: params})

	next := f.replies[0]
	if len(f.replies) > 1 {
		f.replies = f.replies[1:]
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, next)
}

func (f *fakeAPI) recorded() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

func newTestClient(t *testing.T, replies ...string) (*Client, *fakeAPI) {
	t.Helper()
	api := &fakeAPI{replies: replies}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	client := NewClient(Options{
		Token:     testToken,
		BaseURL:   server.URL,
		BaseDelay: time.Millisecond,
	})
	return client, api
}

const okEmpty = `{"ok":true,"result":[]}`
const okSent = `{"ok":true,"result":{"message_id":1}}`

func TestGetUpdatesParsesTextMessage(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":[{"update_id":7,"message":{
		"message_id":3,"from":{"id":42,"is_bot":false},"chat":{"id":42,"type":"private"},"text":"salom"}}]}`)

	updates, err := client.GetUpdates(context.Background(), 5, 0)
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("updates = %+v", updates)
	}
	got := updates[0]
	if got.UpdateID != 7 || got.Message == nil || got.Message.Text != "salom" {
		t.Fatalf("update = %+v", got)
	}
	if got.Message.From == nil || got.Message.From.ID != 42 || got.Message.Chat.Type != "private" {
		t.Errorf("message = %+v", got.Message)
	}

	sent := api.recorded()[0]
	if sent.method != "getUpdates" || sent.params["offset"] != float64(5) {
		t.Errorf("call = %+v", sent)
	}
	if allowed, _ := sent.params["allowed_updates"].([]any); len(allowed) != 2 || allowed[0] != "message" || allowed[1] != "my_chat_member" {
		t.Errorf("allowed_updates = %v", sent.params["allowed_updates"])
	}
}

func TestAPIErrorIsDecoded(t *testing.T) {
	client, _ := newTestClient(t, `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":3}}`)

	_, err := client.GetUpdates(context.Background(), 0, 0)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.Code != 429 || apiErr.RetryAfter != 3*time.Second || apiErr.Description != "Too Many Requests" {
		t.Errorf("apiErr = %+v", apiErr)
	}
}

func TestSendMessageSendsPlainText(t *testing.T) {
	client, api := newTestClient(t, okSent)

	if _, err := client.SendMessage(context.Background(), Outgoing{ChatID: 42, Text: "hello *world*"}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	sent := api.recorded()[0]
	if sent.method != "sendMessage" || sent.params["chat_id"] != float64(42) || sent.params["text"] != "hello *world*" {
		t.Errorf("call = %+v", sent)
	}
	if _, present := sent.params["parse_mode"]; present {
		t.Error("parse_mode must not be sent")
	}
}

func TestSendMessageSplitsLongText(t *testing.T) {
	client, api := newTestClient(t, okSent)
	text := strings.Repeat("a", 4000) + "\n" + strings.Repeat("b", 999)

	if _, err := client.SendMessage(context.Background(), Outgoing{ChatID: 42, Text: text}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	calls := api.recorded()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	first, _ := calls[0].params["text"].(string)
	second, _ := calls[1].params["text"].(string)
	if !strings.HasPrefix(first, "aaa") || !strings.HasPrefix(second, "bbb") {
		t.Errorf("chunks out of order: %q..., %q...", first[:3], second[:3])
	}
}

func TestSendMessageSkipsBlankText(t *testing.T) {
	client, api := newTestClient(t, okSent)

	if _, err := client.SendMessage(context.Background(), Outgoing{ChatID: 42, Text: " \n "}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if calls := api.recorded(); len(calls) != 0 {
		t.Errorf("calls = %+v, want none", calls)
	}
}

func TestSendMessageRetriesRateLimit(t *testing.T) {
	client, api := newTestClient(t,
		`{"ok":false,"error_code":429,"description":"Too Many Requests"}`,
		okSent,
	)

	if _, err := client.SendMessage(context.Background(), Outgoing{ChatID: 42, Text: "hi"}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if calls := api.recorded(); len(calls) != 2 {
		t.Errorf("calls = %d, want 2", len(calls))
	}
}

func TestSendMessageGivesUpOnPersistentRateLimit(t *testing.T) {
	client, api := newTestClient(t, `{"ok":false,"error_code":429,"description":"Too Many Requests"}`)

	_, err := client.SendMessage(context.Background(), Outgoing{ChatID: 42, Text: "hi"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 429 {
		t.Fatalf("err = %v", err)
	}
	if calls := api.recorded(); len(calls) != 3 {
		t.Errorf("calls = %d, want 3", len(calls))
	}
}

func TestSendMessageDoesNotRetryOtherErrors(t *testing.T) {
	client, api := newTestClient(t, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`)

	if _, err := client.SendMessage(context.Background(), Outgoing{ChatID: 42, Text: "hi"}); err == nil {
		t.Fatal("expected error")
	}
	if calls := api.recorded(); len(calls) != 1 {
		t.Errorf("calls = %d, want 1", len(calls))
	}
}

func TestGetMeReturnsBotIdentity(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":{"id":777,"is_bot":true,"username":"MyAssistantBot"}}`)

	me, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	if me.ID != 777 || me.Username != "MyAssistantBot" || !me.IsBot {
		t.Errorf("me = %+v", me)
	}
	if got := api.recorded()[0].method; got != "getMe" {
		t.Errorf("method = %q", got)
	}
}

func TestGetUpdatesParsesEntitiesAndReply(t *testing.T) {
	client, _ := newTestClient(t, `{"ok":true,"result":[{"update_id":7,"message":{
		"message_id":30,"from":{"id":42,"username":"ann"},"chat":{"id":-100123,"type":"supergroup"},
		"text":"@MyBot hi","entities":[{"type":"mention","offset":0,"length":6},
			{"type":"text_mention","offset":7,"length":2,"user":{"id":777}}],
		"caption":"cap","caption_entities":[{"type":"mention","offset":0,"length":3}],
		"reply_to_message":{"message_id":29,"from":{"id":777,"is_bot":true},"chat":{"id":-100123,"type":"supergroup"},"text":"earlier answer"}}}]}`)

	updates, err := client.GetUpdates(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	msg := updates[0].Message
	if msg.MessageID != 30 || msg.Chat.ID != -100123 || msg.Chat.Type != ChatTypeSupergroup || msg.From.Username != "ann" {
		t.Errorf("message = %+v", msg)
	}
	if len(msg.Entities) != 2 || msg.Entities[0].Type != EntityMention || msg.Entities[0].Length != 6 {
		t.Fatalf("entities = %+v", msg.Entities)
	}
	if mention := msg.Entities[1]; mention.Type != EntityTextMention || mention.User == nil || mention.User.ID != 777 {
		t.Errorf("text_mention = %+v", mention)
	}
	if msg.Caption != "cap" || len(msg.CaptionEntities) != 1 {
		t.Errorf("caption = %q, %+v", msg.Caption, msg.CaptionEntities)
	}
	replied := msg.ReplyToMessage
	if replied == nil || replied.MessageID != 29 || replied.From.ID != 777 || replied.Text != "earlier answer" {
		t.Errorf("reply_to_message = %+v", replied)
	}
}

func TestSendMessageRepliesToOriginalMessage(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":{"message_id":501}}`)

	sentID, err := client.SendMessage(context.Background(), Outgoing{ChatID: -100123, Text: "answer", ReplyToMessageID: 30})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if sentID != 501 {
		t.Errorf("sent message ID = %d, want 501", sentID)
	}
	sent := api.recorded()[0]
	if sent.params["chat_id"] != float64(-100123) {
		t.Errorf("chat_id = %v", sent.params["chat_id"])
	}
	reply, _ := sent.params["reply_parameters"].(map[string]any)
	if reply["message_id"] != float64(30) || reply["allow_sending_without_reply"] != true {
		t.Errorf("reply_parameters = %v", sent.params["reply_parameters"])
	}
}

func TestSendMessageWithoutReplyTargetOmitsReplyParameters(t *testing.T) {
	client, api := newTestClient(t, okSent)

	if _, err := client.SendMessage(context.Background(), Outgoing{ChatID: 42, Text: "hi"}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if _, present := api.recorded()[0].params["reply_parameters"]; present {
		t.Error("reply_parameters sent for a message that replies to nothing")
	}
}

func TestSendMessageRepliesOnlyWithFirstChunk(t *testing.T) {
	client, api := newTestClient(t,
		`{"ok":true,"result":{"message_id":10}}`,
		`{"ok":true,"result":{"message_id":11}}`,
	)
	text := strings.Repeat("a", 4000) + "\n" + strings.Repeat("b", 999)

	sentID, err := client.SendMessage(context.Background(), Outgoing{ChatID: 1, Text: text, ReplyToMessageID: 30})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if sentID != 10 {
		t.Errorf("sent message ID = %d, want the first chunk's 10", sentID)
	}
	calls := api.recorded()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	if _, present := calls[0].params["reply_parameters"]; !present {
		t.Error("first chunk does not reply to the original message")
	}
	if _, present := calls[1].params["reply_parameters"]; present {
		t.Error("second chunk also carries reply_parameters")
	}
}

func update(id int) string {
	return `{"update_id":` + string(rune('0'+id)) + `,"message":{"message_id":1,"from":{"id":1},"chat":{"id":1,"type":"private"},"text":"m"}}`
}

func TestPollDeliversUpdatesAndAdvancesOffset(t *testing.T) {
	client, api := newTestClient(t,
		`{"ok":true,"result":[`+update(1)+`,`+update(2)+`]}`,
		`{"ok":false,"error_code":502,"description":"Bad Gateway"}`,
		`{"ok":true,"result":[`+update(3)+`]}`,
		okEmpty,
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var got []int64
	err := client.Poll(ctx, func(u Update) {
		got = append(got, u.UpdateID)
		if len(got) == 3 {
			cancel()
		}
	})
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Errorf("updates = %v", got)
	}

	calls := api.recorded()
	wantOffsets := []float64{0, 3, 3}
	for i, want := range wantOffsets {
		if calls[i].params["offset"] != want {
			t.Errorf("call %d offset = %v, want %v", i, calls[i].params["offset"], want)
		}
	}
}

func TestPollStopsOnInvalidToken(t *testing.T) {
	client, _ := newTestClient(t, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)

	err := client.Poll(context.Background(), func(Update) {})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 401 {
		t.Fatalf("err = %v", err)
	}
}

func TestErrorsNeverContainToken(t *testing.T) {
	client := NewClient(Options{Token: testToken, BaseURL: "http://127.0.0.1:1"})

	_, err := client.GetUpdates(context.Background(), 0, 0)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "test-secret") {
		t.Errorf("error leaks token: %v", err)
	}
}

func TestGetUpdatesParsesFirstName(t *testing.T) {
	client, _ := newTestClient(t, `{"ok":true,"result":[{"update_id":7,"message":{
		"message_id":3,"from":{"id":42,"first_name":"Бек","username":"bek"},"chat":{"id":-5,"type":"group"},"text":"salom"}}]}`)

	updates, err := client.GetUpdates(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if got := updates[0].Message.From.FirstName; got != "Бек" {
		t.Errorf("FirstName = %q", got)
	}
}

func TestGetChatMemberReturnsStatus(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":{"status":"administrator","user":{"id":42}}}`)

	member, err := client.GetChatMember(context.Background(), -100123, 42)
	if err != nil {
		t.Fatalf("GetChatMember: %v", err)
	}
	if member.Status != "administrator" || !member.IsAdmin() {
		t.Errorf("member = %+v", member)
	}
	sent := api.recorded()[0]
	if sent.method != "getChatMember" || sent.params["chat_id"] != float64(-100123) || sent.params["user_id"] != float64(42) {
		t.Errorf("call = %+v", sent)
	}
}

func TestChatMemberIsAdmin(t *testing.T) {
	for status, want := range map[string]bool{
		"creator": true, "administrator": true,
		"member": false, "restricted": false, "left": false, "kicked": false, "": false,
	} {
		if got := (ChatMember{Status: status}).IsAdmin(); got != want {
			t.Errorf("IsAdmin(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestSetWebhookRegistersURLWithSecret(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":true}`)

	if err := client.SetWebhook(context.Background(), "https://bot.example/api/webhook", "s3cret"); err != nil {
		t.Fatalf("SetWebhook: %v", err)
	}
	sent := api.recorded()[0]
	if sent.method != "setWebhook" || sent.params["url"] != "https://bot.example/api/webhook" || sent.params["secret_token"] != "s3cret" {
		t.Errorf("call = %+v", sent)
	}
	if allowed, _ := sent.params["allowed_updates"].([]any); len(allowed) != 2 || allowed[0] != "message" || allowed[1] != "my_chat_member" {
		t.Errorf("allowed_updates = %v", sent.params["allowed_updates"])
	}
}

func TestDeleteWebhook(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":true}`)

	if err := client.DeleteWebhook(context.Background()); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	if got := api.recorded()[0].method; got != "deleteWebhook" {
		t.Errorf("method = %q", got)
	}
}

func TestGetUpdatesParsesMedia(t *testing.T) {
	client, _ := newTestClient(t, `{"ok":true,"result":[{"update_id":8,"message":{
		"message_id":4,"from":{"id":42},"chat":{"id":42,"type":"private"},
		"photo":[{"file_id":"small","file_size":900,"width":90,"height":90},{"file_id":"big","file_size":90000,"width":800,"height":800}],
		"sticker":{"file_id":"stk","emoji":"😂","is_animated":true,"is_video":false,"thumbnail":{"file_id":"stk-thumb","file_size":5000}},
		"animation":{"file_id":"gif","mime_type":"video/mp4","thumbnail":{"file_id":"gif-thumb"}},
		"voice":{"file_id":"vc","duration":12,"mime_type":"audio/ogg","file_size":40000}}}]}`)

	updates, err := client.GetUpdates(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	msg := updates[0].Message
	if len(msg.Photo) != 2 || msg.Photo[1].FileID != "big" || msg.Photo[1].FileSize != 90000 {
		t.Errorf("photo = %+v", msg.Photo)
	}
	if msg.Sticker == nil || msg.Sticker.Emoji != "😂" || !msg.Sticker.IsAnimated || msg.Sticker.Thumbnail.FileID != "stk-thumb" {
		t.Errorf("sticker = %+v", msg.Sticker)
	}
	if msg.Animation == nil || msg.Animation.Thumbnail == nil || msg.Animation.Thumbnail.FileID != "gif-thumb" {
		t.Errorf("animation = %+v", msg.Animation)
	}
	if msg.Voice == nil || msg.Voice.FileID != "vc" || msg.Voice.Duration != 12 || msg.Voice.FileSize != 40000 {
		t.Errorf("voice = %+v", msg.Voice)
	}
}

// fileServer serves getFile and the file download, as Telegram does.
func fileServer(t *testing.T, path string, content []byte) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bot" + testToken + "/getFile":
			var params map[string]any
			_ = json.NewDecoder(r.Body).Decode(&params)
			if params["file_id"] != "file-1" {
				_, _ = io.WriteString(w, `{"ok":false,"error_code":400,"description":"Bad Request: invalid file_id"}`)
				return
			}
			_, _ = io.WriteString(w, `{"ok":true,"result":{"file_id":"file-1","file_size":`+strconv.Itoa(len(content))+`,"file_path":"`+path+`"}}`)
		case "/file/bot" + testToken + "/" + path:
			_, _ = w.Write(content)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return NewClient(Options{Token: testToken, BaseURL: server.URL})
}

func TestDownloadFile(t *testing.T) {
	client := fileServer(t, "photos/file_1.jpg", []byte("JPEGDATA"))

	data, err := client.DownloadFile(context.Background(), "file-1", 1<<20)
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if string(data) != "JPEGDATA" {
		t.Errorf("data = %q", data)
	}
}

func TestDownloadFileRefusesFilesOverTheLimit(t *testing.T) {
	client := fileServer(t, "voice/file_2.oga", make([]byte, 5000))

	_, err := client.DownloadFile(context.Background(), "file-1", 1000)
	if !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("err = %v, want ErrFileTooLarge", err)
	}
}

func TestDownloadFileErrorsDoNotLeakTheToken(t *testing.T) {
	client := fileServer(t, "photos/x.jpg", []byte("x"))

	_, err := client.DownloadFile(context.Background(), "unknown", 1000)
	if err == nil {
		t.Fatal("expected an error for an unknown file")
	}
	unreachable := NewClient(Options{Token: testToken, BaseURL: "http://127.0.0.1:1"})
	_, netErr := unreachable.DownloadFile(context.Background(), "file-1", 1000)
	for _, e := range []error{err, netErr} {
		if e == nil || strings.Contains(e.Error(), "test-secret") {
			t.Errorf("error = %v; it must exist and must not contain the token", e)
		}
	}
}

func TestSendPhotoUploadsTheImageAsAReply(t *testing.T) {
	var got struct {
		method, chatID, caption, reply string
		photo                          []byte
		filename                       string
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method = strings.TrimPrefix(r.URL.Path, "/bot"+testToken+"/")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("not a multipart upload: %v", err)
		}
		got.chatID, got.caption, got.reply = r.FormValue("chat_id"), r.FormValue("caption"), r.FormValue("reply_parameters")
		file, header, err := r.FormFile("photo")
		if err == nil {
			got.photo, _ = io.ReadAll(file)
			got.filename = header.Filename
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":777}}`)
	}))
	defer server.Close()
	client := NewClient(Options{Token: testToken, BaseURL: server.URL})

	id, err := client.SendPhoto(context.Background(), Photo{ChatID: -100, ReplyToMessageID: 30, Data: []byte("PNGDATA"), Caption: "here you go"})
	if err != nil || id != 777 {
		t.Fatalf("id = %d, err = %v", id, err)
	}
	if got.method != "sendPhoto" || got.chatID != "-100" || got.caption != "here you go" || string(got.photo) != "PNGDATA" || got.filename == "" {
		t.Errorf("request = %+v", got)
	}
	if !strings.Contains(got.reply, `"message_id":30`) {
		t.Errorf("reply_parameters = %q", got.reply)
	}
}

func TestSendPhotoCutsLongCaptions(t *testing.T) {
	var caption string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		caption = r.FormValue("caption")
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":1}}`)
	}))
	defer server.Close()
	client := NewClient(Options{Token: testToken, BaseURL: server.URL})

	if _, err := client.SendPhoto(context.Background(), Photo{ChatID: 1, Data: []byte("x"), Caption: strings.Repeat("я", 2000)}); err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(caption)); n > CaptionLimit {
		t.Errorf("caption is %d characters, over Telegram's %d", n, CaptionLimit)
	}
}

func TestGetChatMemberCountReturnsTheCount(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":37}`)

	count, err := client.GetChatMemberCount(context.Background(), -100123)
	if err != nil || count != 37 {
		t.Fatalf("count = %d, err = %v", count, err)
	}
	if sent := api.recorded()[0]; sent.method != "getChatMemberCount" || sent.params["chat_id"] != float64(-100123) {
		t.Errorf("call = %+v", sent)
	}
}

func TestGetChatReturnsTheTitle(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":{"id":-100123,"type":"supergroup","title":"Devs"}}`)

	chat, err := client.GetChat(context.Background(), -100123)
	if err != nil || chat.Title != "Devs" {
		t.Fatalf("chat = %+v, err = %v", chat, err)
	}
	if sent := api.recorded()[0]; sent.method != "getChat" || sent.params["chat_id"] != float64(-100123) {
		t.Errorf("call = %+v", sent)
	}
}

func TestMyChatMemberUpdateIsDecoded(t *testing.T) {
	var update Update
	raw := `{"update_id":7,"my_chat_member":{"chat":{"id":-100123,"type":"supergroup","title":"Devs"},"from":{"id":5},"new_chat_member":{"status":"administrator"}}}`
	if err := json.Unmarshal([]byte(raw), &update); err != nil {
		t.Fatal(err)
	}
	change := update.MyChatMember
	if change == nil || change.Chat.Title != "Devs" || change.NewChatMember.Status != "administrator" || change.From.ID != 5 {
		t.Errorf("update = %+v", update)
	}
}
