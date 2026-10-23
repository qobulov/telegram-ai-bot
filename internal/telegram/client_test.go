package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	if allowed, _ := sent.params["allowed_updates"].([]any); len(allowed) != 4 || allowed[0] != "message" || allowed[1] != "my_chat_member" || allowed[2] != "chat_member" || allowed[3] != "callback_query" {
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
	if allowed, _ := sent.params["allowed_updates"].([]any); len(allowed) != 4 || allowed[0] != "message" || allowed[1] != "my_chat_member" || allowed[2] != "chat_member" || allowed[3] != "callback_query" {
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

func TestGetChatAdministratorsReturnsTheAdmins(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":[
		{"status":"creator","user":{"id":1,"first_name":"Ann","username":"ann"},"is_anonymous":false},
		{"status":"administrator","user":{"id":2,"first_name":"Bek"},"custom_title":"Boss","is_anonymous":true,"can_pin_messages":true}]}`)

	admins, err := client.GetChatAdministrators(context.Background(), -100123)
	if err != nil {
		t.Fatalf("GetChatAdministrators: %v", err)
	}
	if len(admins) != 2 {
		t.Fatalf("admins = %+v, want two", admins)
	}
	ann, bek := admins[0], admins[1]
	if ann.Status != "creator" || ann.User == nil || ann.User.ID != 1 || ann.User.Username != "ann" || ann.IsAnonymous || ann.CustomTitle != "" {
		t.Errorf("first admin = %+v", ann)
	}
	if bek.Status != "administrator" || bek.User == nil || bek.User.FirstName != "Bek" || !bek.IsAnonymous || bek.CustomTitle != "Boss" || !bek.CanPinMessages {
		t.Errorf("second admin = %+v", bek)
	}
	if sent := api.recorded()[0]; sent.method != "getChatAdministrators" || sent.params["chat_id"] != float64(-100123) {
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

const okTrue = `{"ok":true,"result":true}`

func TestSetMessageReactionSendsOneEmoji(t *testing.T) {
	client, api := newTestClient(t, okTrue)

	if err := client.SetMessageReaction(context.Background(), -100123, 30, "🔥"); err != nil {
		t.Fatalf("SetMessageReaction: %v", err)
	}
	sent := api.recorded()[0]
	if sent.method != "setMessageReaction" || sent.params["chat_id"] != float64(-100123) || sent.params["message_id"] != float64(30) {
		t.Errorf("call = %+v", sent)
	}
	want := []any{map[string]any{"type": "emoji", "emoji": "🔥"}}
	if got := sent.params["reaction"]; !reflect.DeepEqual(got, want) {
		t.Errorf("reaction = %v, want %v", got, want)
	}
}

func TestSetMessageReactionWithoutEmojiClearsIt(t *testing.T) {
	client, api := newTestClient(t, okTrue)

	if err := client.SetMessageReaction(context.Background(), -100123, 30, ""); err != nil {
		t.Fatalf("SetMessageReaction: %v", err)
	}
	if reaction, ok := api.recorded()[0].params["reaction"].([]any); !ok || len(reaction) != 0 {
		t.Errorf("reaction = %v, want an empty list", api.recorded()[0].params["reaction"])
	}
}

func TestRestrictChatMemberSendsExplicitFalsesAndUntil(t *testing.T) {
	client, api := newTestClient(t, okTrue)
	until := time.Date(2026, 10, 16, 16, 4, 0, 0, time.UTC)

	restriction := Restriction{ChatID: -100123, UserID: 42, Permissions: NoPermissions(), Until: until}
	if err := client.RestrictChatMember(context.Background(), restriction); err != nil {
		t.Fatalf("RestrictChatMember: %v", err)
	}
	sent := api.recorded()[0]
	if sent.method != "restrictChatMember" || sent.params["chat_id"] != float64(-100123) || sent.params["user_id"] != float64(42) {
		t.Errorf("call = %+v", sent)
	}
	if sent.params["use_independent_chat_permissions"] != true {
		t.Errorf("use_independent_chat_permissions = %v, want true", sent.params["use_independent_chat_permissions"])
	}
	permissions, _ := sent.params["permissions"].(map[string]any)
	if value, present := permissions["can_send_messages"]; !present || value != false {
		t.Errorf("can_send_messages = %v (present %v), want an explicit false", value, present)
	}
	assertAllPermissions(t, permissions, false)
	if sent.params["until_date"] != float64(until.Unix()) {
		t.Errorf("until_date = %v, want %d", sent.params["until_date"], until.Unix())
	}
}

func TestRestrictChatMemberWithoutUntilOmitsIt(t *testing.T) {
	client, api := newTestClient(t, okTrue)

	restriction := Restriction{ChatID: -100123, UserID: 42, Permissions: AllPermissions()}
	if err := client.RestrictChatMember(context.Background(), restriction); err != nil {
		t.Fatalf("RestrictChatMember: %v", err)
	}
	sent := api.recorded()[0]
	if _, present := sent.params["until_date"]; present {
		t.Errorf("until_date = %v, want it left out", sent.params["until_date"])
	}
	permissions, _ := sent.params["permissions"].(map[string]any)
	assertAllPermissions(t, permissions, true)
}

// assertAllPermissions checks that every one of the 15 permissions was sent
// with the given value: a missing key would leave Telegram's default.
func assertAllPermissions(t *testing.T, permissions map[string]any, want bool) {
	t.Helper()
	if len(permissions) != 15 {
		t.Errorf("permissions has %d keys, want all 15: %v", len(permissions), permissions)
	}
	for name, value := range permissions {
		if value != want {
			t.Errorf("%s = %v, want %v", name, value, want)
		}
	}
}

func TestUnbanAlwaysOnlyIfBanned(t *testing.T) {
	client, api := newTestClient(t, okTrue)

	if err := client.UnbanChatMember(context.Background(), -100123, 42); err != nil {
		t.Fatalf("UnbanChatMember: %v", err)
	}
	want := map[string]any{"chat_id": float64(-100123), "user_id": float64(42), "only_if_banned": true}
	if sent := api.recorded()[0]; sent.method != "unbanChatMember" || !reflect.DeepEqual(sent.params, want) {
		t.Errorf("call = %+v, want unbanChatMember %v", sent, want)
	}
}

func TestPinIsSilent(t *testing.T) {
	client, api := newTestClient(t, okTrue)

	if err := client.PinChatMessage(context.Background(), -100123, 30); err != nil {
		t.Fatalf("PinChatMessage: %v", err)
	}
	want := map[string]any{"chat_id": float64(-100123), "message_id": float64(30), "disable_notification": true}
	if sent := api.recorded()[0]; sent.method != "pinChatMessage" || !reflect.DeepEqual(sent.params, want) {
		t.Errorf("call = %+v, want pinChatMessage %v", sent, want)
	}
}

func TestChatAndMemberMethodsSendExactParams(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		method string
		call   func(*Client) error
		want   map[string]any
	}{
		{
			method: "sendChatAction",
			call:   func(c *Client) error { return c.SendChatAction(ctx, -100123, 0, "typing") },
			want:   map[string]any{"chat_id": float64(-100123), "action": "typing"},
		},
		{
			// In a forum topic, or "typing" shows in the General topic.
			method: "sendChatAction",
			call:   func(c *Client) error { return c.SendChatAction(ctx, -100123, 7, "typing") },
			want:   map[string]any{"chat_id": float64(-100123), "message_thread_id": float64(7), "action": "typing"},
		},
		{
			method: "deleteMessage",
			call:   func(c *Client) error { return c.DeleteMessage(ctx, -100123, 30) },
			want:   map[string]any{"chat_id": float64(-100123), "message_id": float64(30)},
		},
		{
			// No until_date: the ban is forever until someone unbans.
			method: "banChatMember",
			call:   func(c *Client) error { return c.BanChatMember(ctx, -100123, 42) },
			want:   map[string]any{"chat_id": float64(-100123), "user_id": float64(42)},
		},
		{
			method: "unpinChatMessage",
			call:   func(c *Client) error { return c.UnpinChatMessage(ctx, -100123, 30) },
			want:   map[string]any{"chat_id": float64(-100123), "message_id": float64(30)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			client, api := newTestClient(t, okTrue)

			if err := tt.call(client); err != nil {
				t.Fatalf("%s: %v", tt.method, err)
			}
			sent := api.recorded()[0]
			if sent.method != tt.method || !reflect.DeepEqual(sent.params, tt.want) {
				t.Errorf("call = %+v, want %s %v", sent, tt.method, tt.want)
			}
		})
	}
}

func TestDeleteMessageReturnsTelegramRefusal(t *testing.T) {
	client, _ := newTestClient(t, `{"ok":false,"error_code":400,"description":"Bad Request: message can't be deleted"}`)

	err := client.DeleteMessage(context.Background(), -100123, 30)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 400 || !strings.Contains(apiErr.Description, "can't be deleted") {
		t.Fatalf("err = %v, want Telegram's 400", err)
	}
}

func TestSendPollSendsOptionObjects(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":{"message_id":88}}`)

	poll := OutgoingPoll{ChatID: -100123, Question: "Lunch?", Options: []string{"yes", "no"}, Multiple: true}
	id, err := client.SendPoll(context.Background(), poll)
	if err != nil || id != 88 {
		t.Fatalf("id = %d, err = %v, want 88", id, err)
	}
	sent := api.recorded()[0]
	if sent.method != "sendPoll" || sent.params["chat_id"] != float64(-100123) || sent.params["question"] != "Lunch?" {
		t.Errorf("call = %+v", sent)
	}
	options, _ := sent.params["options"].([]any)
	if len(options) != 2 {
		t.Fatalf("options = %v", sent.params["options"])
	}
	if second, _ := options[1].(map[string]any); second["text"] != "no" {
		t.Errorf("options[1] = %v, want {text: no}", options[1])
	}
	if sent.params["is_anonymous"] != true || sent.params["allows_multiple_answers"] != true {
		t.Errorf("is_anonymous = %v, allows_multiple_answers = %v", sent.params["is_anonymous"], sent.params["allows_multiple_answers"])
	}
	if _, ok := sent.params["reply_parameters"]; ok {
		t.Errorf("reply_parameters = %v, want none for a poll that replies to nothing", sent.params["reply_parameters"])
	}
}

// A poll asked for in a forum topic replies to the request, which keeps it
// in that topic rather than the General one.
func TestSendPollRepliesToTheRequest(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":{"message_id":88}}`)

	poll := OutgoingPoll{ChatID: -100123, Question: "Lunch?", Options: []string{"yes", "no"}, ReplyToMessageID: 60}
	if _, err := client.SendPoll(context.Background(), poll); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"message_id": float64(60), "allow_sending_without_reply": true}
	if got := api.recorded()[0].params["reply_parameters"]; !reflect.DeepEqual(got, want) {
		t.Errorf("reply_parameters = %v, want %v", got, want)
	}
}

func TestChatMemberRightsDecoded(t *testing.T) {
	decode := func(raw string) ChatMember {
		t.Helper()
		var member ChatMember
		if err := json.Unmarshal([]byte(raw), &member); err != nil {
			t.Fatal(err)
		}
		return member
	}

	admin := decode(`{"status":"administrator","user":{"id":777,"is_bot":true},
		"can_delete_messages":true,"can_restrict_members":false,"can_pin_messages":true}`)
	if got, want := admin.Rights(), []string{"can_delete_messages", "can_pin_messages"}; !reflect.DeepEqual(got, want) {
		t.Errorf("administrator Rights() = %v, want %v", got, want)
	}
	if admin.Has(RightRestrictMembers) || !admin.Has(RightDeleteMessages) {
		t.Errorf("administrator Has: restrict %v, delete %v", admin.Has(RightRestrictMembers), admin.Has(RightDeleteMessages))
	}
	if admin.User == nil || admin.User.ID != 777 || !admin.User.IsBot {
		t.Errorf("administrator user = %+v", admin.User)
	}

	creator := decode(`{"status":"creator","user":{"id":5}}`)
	if !creator.Has(RightRestrictMembers) || len(creator.Rights()) != 7 {
		t.Errorf("creator Has(restrict) = %v, Rights() = %v; a creator holds every right", creator.Has(RightRestrictMembers), creator.Rights())
	}
	if creator.Has("can_fly") {
		t.Error("creator holds a right that does not exist")
	}

	if member := decode(`{"status":"member","user":{"id":42}}`); member.Rights() != nil {
		t.Errorf("member Rights() = %v, want nil", member.Rights())
	}
	// A restricted member's can_* fields are permissions, not admin rights.
	restricted := decode(`{"status":"restricted","user":{"id":43},"until_date":1792166400,"can_pin_messages":true}`)
	if restricted.Has(RightPinMessages) || restricted.Rights() != nil || restricted.UntilDate != 1792166400 {
		t.Errorf("restricted = %+v, Rights() = %v", restricted, restricted.Rights())
	}
}

func TestGetUpdatesParsesNewMediaKinds(t *testing.T) {
	client, _ := newTestClient(t, `{"ok":true,"result":[{"update_id":9,"message":{
		"message_id":5,"from":{"id":42},"chat":{"id":-100123,"type":"supergroup"},
		"video":{"file_id":"vid","duration":15,"thumbnail":{"file_id":"vid-thumb"}},
		"video_note":{"file_id":"round","duration":8,"length":240,"thumbnail":{"file_id":"round-thumb"}},
		"document":{"file_id":"doc","file_name":"report.pdf","mime_type":"application/pdf"},
		"audio":{"file_id":"aud","duration":200,"title":"Song","performer":"Band"},
		"poll":{"id":"p1","question":"Lunch?","options":[{"text":"plov","voter_count":3},{"text":"somsa","voter_count":1}]},
		"forward_origin":{"type":"user","date":1792166400,"sender_user":{"id":7,"first_name":"Bek"}},
		"new_chat_members":[{"id":8,"first_name":"Ali"},{"id":9,"first_name":"Vali"}],
		"left_chat_member":{"id":10,"first_name":"Sardor"}}}]}`)

	updates, err := client.GetUpdates(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	msg := updates[0].Message
	if v := msg.Video; v == nil || v.FileID != "vid" || v.Duration != 15 || v.Thumbnail == nil || v.Thumbnail.FileID != "vid-thumb" {
		t.Errorf("video = %+v", msg.Video)
	}
	if v := msg.VideoNote; v == nil || v.FileID != "round" || v.Duration != 8 || v.Thumbnail == nil || v.Thumbnail.FileID != "round-thumb" {
		t.Errorf("video_note = %+v", msg.VideoNote)
	}
	if d := msg.Document; d == nil || d.FileID != "doc" || d.FileName != "report.pdf" || d.MimeType != "application/pdf" {
		t.Errorf("document = %+v", msg.Document)
	}
	if a := msg.Audio; a == nil || a.FileID != "aud" || a.Duration != 200 || a.Title != "Song" || a.Performer != "Band" {
		t.Errorf("audio = %+v", msg.Audio)
	}
	if p := msg.Poll; p == nil || p.Question != "Lunch?" || len(p.Options) != 2 || p.Options[1].Text != "somsa" {
		t.Errorf("poll = %+v", msg.Poll)
	}
	if o := msg.ForwardOrigin; o == nil || o.Type != "user" || o.SenderUser == nil || o.SenderUser.ID != 7 || o.Name() != "Bek" {
		t.Errorf("forward_origin = %+v", msg.ForwardOrigin)
	}
	if joined := msg.NewChatMembers; len(joined) != 2 || joined[0].FirstName != "Ali" || joined[1].ID != 9 {
		t.Errorf("new_chat_members = %+v", msg.NewChatMembers)
	}
	if left := msg.LeftChatMember; left == nil || left.ID != 10 || left.FirstName != "Sardor" {
		t.Errorf("left_chat_member = %+v", msg.LeftChatMember)
	}
}

// Service messages are known by the field Telegram sets for their kind. A
// member's message the client does not decode, such as a contact, is none.
func TestGetUpdatesNotesServiceMessages(t *testing.T) {
	client, _ := newTestClient(t, `{"ok":true,"result":[
		{"update_id":1,"message":{"message_id":5,"from":{"id":42},"chat":{"id":-100123,"type":"supergroup"},
			"pinned_message":{"message_id":4,"chat":{"id":-100123,"type":"supergroup"},"date":0}}},
		{"update_id":2,"message":{"message_id":6,"from":{"id":42},"chat":{"id":-100123,"type":"supergroup"},
			"video_chat_started":{}}},
		{"update_id":3,"message":{"message_id":7,"from":{"id":42},"chat":{"id":-100123,"type":"supergroup"},
			"new_chat_members":[{"id":8,"first_name":"Ali"}]}},
		{"update_id":4,"message":{"message_id":8,"from":{"id":43},"chat":{"id":-100123,"type":"supergroup"},
			"contact":{"phone_number":"+998","first_name":"Spam"}}},
		{"update_id":5,"message":{"message_id":9,"from":{"id":43},"chat":{"id":-100123,"type":"supergroup"},
			"text":"hi","reply_to_message":{"message_id":4,"from":{"id":42},"chat":{"id":-100123,"type":"supergroup"},"delete_chat_photo":true}}}]}`)

	updates, err := client.GetUpdates(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	want := []bool{true, true, true, false, false}
	for i, u := range updates {
		if got := u.Message.IsService(); got != want[i] {
			t.Errorf("update %d: IsService = %v, want %v (Service %q)", u.UpdateID, got, want[i], u.Message.Service)
		}
	}
	if updates[0].Message.Service != "pinned_message" {
		t.Errorf("pin: Service = %q", updates[0].Message.Service)
	}
	if replied := updates[4].Message.ReplyToMessage; replied == nil || !replied.IsService() {
		t.Errorf("replied-to message = %+v, want a service message", replied)
	}
}

func TestMessageOriginName(t *testing.T) {
	tests := []struct {
		name   string
		origin *MessageOrigin
		want   string
	}{
		{"user", &MessageOrigin{Type: "user", SenderUser: &User{ID: 7, FirstName: "Bek"}}, "Bek"},
		{"hidden user", &MessageOrigin{Type: "hidden_user", SenderUserName: "Ali V."}, "Ali V."},
		{"group posting as itself", &MessageOrigin{Type: "chat", SenderChat: &Chat{Title: "Devs"}}, "Devs"},
		{"channel", &MessageOrigin{Type: "channel", Chat: &Chat{Title: "News"}}, "News"},
		{"nothing known", &MessageOrigin{Type: "user"}, ""},
		{"no origin", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.origin.Name(); got != tt.want {
				t.Errorf("Name() = %q, want %q", got, tt.want)
			}
		})
	}
}

// In a forum topic Telegram makes every message a reply to the topic's first
// message. Only a reply to some other message is a reply.
func TestImplicitTopicReply(t *testing.T) {
	tests := []struct {
		name string
		json string
		want bool
	}{
		{"plain message in a topic", `{"message_id":40,"message_thread_id":7,"is_topic_message":true,
			"reply_to_message":{"message_id":7,"forum_topic_created":{"name":"Memes"}}}`, true},
		{"reply inside a topic", `{"message_id":40,"message_thread_id":7,"is_topic_message":true,
			"reply_to_message":{"message_id":39,"text":"hi"}}`, false},
		{"reply outside topics", `{"message_id":40,"reply_to_message":{"message_id":39,"text":"hi"}}`, false},
		{"topic message that replies to nothing", `{"message_id":40,"message_thread_id":7,"is_topic_message":true}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var msg Message
			if err := json.Unmarshal([]byte(tt.json), &msg); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got := msg.ImplicitTopicReply(); got != tt.want {
				t.Errorf("ImplicitTopicReply() = %v, want %v (message %+v)", got, tt.want, msg)
			}
		})
	}
}

func TestGetStickerSetReadsThePack(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":{"name":"xaa_by_qstickbot","title":"12:43","stickers":[
		{"file_id":"f1","file_unique_id":"u1","emoji":"🫥","is_animated":false,"is_video":false},
		{"file_id":"f2","file_unique_id":"u2","emoji":"🌟","is_video":true,"thumbnail":{"file_id":"t2","width":128,"height":128}}]}}`)

	set, err := client.GetStickerSet(context.Background(), "xaa_by_qstickbot")
	if err != nil || len(set.Stickers) != 2 || set.Stickers[0].FileUniqueID != "u1" || set.Stickers[1].Thumbnail == nil || set.Stickers[1].Thumbnail.FileID != "t2" {
		t.Fatalf("set = %+v, err = %v", set, err)
	}
	if sent := api.recorded()[0]; sent.method != "getStickerSet" || sent.params["name"] != "xaa_by_qstickbot" {
		t.Errorf("call = %+v", sent)
	}
}

func TestSendStickerRepliesToTheMessage(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":{"message_id":91}}`)

	id, err := client.SendSticker(context.Background(), -100123, "f1", 40)
	if err != nil || id != 91 {
		t.Fatalf("id = %d, err = %v", id, err)
	}
	sent := api.recorded()[0]
	reply, _ := sent.params["reply_parameters"].(map[string]any)
	if sent.method != "sendSticker" || sent.params["sticker"] != "f1" || reply["message_id"] != float64(40) {
		t.Errorf("call = %+v", sent)
	}
}

func TestSendAnimationSendsTheLink(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":{"message_id":92}}`)
	id, err := client.SendAnimation(context.Background(), -100123, "https://media.giphy.com/1.mp4", 40)
	if err != nil || id != 92 {
		t.Fatalf("id = %d, err = %v", id, err)
	}
	if sent := api.recorded()[0]; sent.method != "sendAnimation" || sent.params["animation"] != "https://media.giphy.com/1.mp4" {
		t.Errorf("call = %+v", sent)
	}
}

func TestSendAudioUploadsTheTrackWithItsName(t *testing.T) {
	var got struct {
		method, title, performer, reply, filename string
		audio                                     []byte
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method = strings.TrimPrefix(r.URL.Path, "/bot"+testToken+"/")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("not a multipart upload: %v", err)
		}
		got.title, got.performer, got.reply = r.FormValue("title"), r.FormValue("performer"), r.FormValue("reply_parameters")
		if file, header, err := r.FormFile("audio"); err == nil {
			got.audio, _ = io.ReadAll(file)
			got.filename = header.Filename
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":93}}`)
	}))
	defer server.Close()
	client := NewClient(Options{Token: testToken, BaseURL: server.URL})

	id, err := client.SendAudio(context.Background(), Track{ChatID: -100123, ReplyToMessageID: 40, Data: []byte("MP3"), Title: "Yurak", Performer: "Shohruhxon"})
	if err != nil || id != 93 {
		t.Fatalf("id = %d, err = %v", id, err)
	}
	if got.method != "sendAudio" || got.title != "Yurak" || got.performer != "Shohruhxon" || string(got.audio) != "MP3" || got.filename != "Shohruhxon - Yurak.mp3" {
		t.Errorf("request = %+v", got)
	}
	if !strings.Contains(got.reply, `"message_id":40`) {
		t.Errorf("reply_parameters = %q", got.reply)
	}
}

// A quiz names its right answer, even the first (0, which JSON would drop
// were it left out when empty), and allows one answer only.
func TestSendPollSendsAQuiz(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":{"message_id":89}}`)

	poll := OutgoingPoll{ChatID: -100123, Question: "Ling?", Options: []string{"Assassin", "Tank"}, Multiple: true, Quiz: true, Correct: 0, Explanation: "Jungler"}
	if _, err := client.SendPoll(context.Background(), poll); err != nil {
		t.Fatal(err)
	}
	p := api.recorded()[0].params
	if p["type"] != "quiz" || p["correct_option_id"] != float64(0) || p["explanation"] != "Jungler" || p["allows_multiple_answers"] != false {
		t.Errorf("params = %+v", p)
	}
}

// Link buttons go under the message as an inline keyboard, and keep a
// rich-looking text plain so that they are not lost.
func TestSendMessageSendsButtons(t *testing.T) {
	client, api := newTestClient(t, okSent)
	buttons := [][]Button{{{Text: "1 - kanal", URL: "https://t.me/news"}}, {{Text: "✅ Tasdiqlash", URL: "https://t.me/bot?start=check"}}}

	if _, err := client.SendMessage(context.Background(), Outgoing{ChatID: 42, Text: "- join", Buttons: buttons}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	sent := api.recorded()[0]
	markup, _ := json.Marshal(sent.params["reply_markup"])
	want := `{"inline_keyboard":[[{"text":"1 - kanal","url":"https://t.me/news"}],[{"text":"✅ Tasdiqlash","url":"https://t.me/bot?start=check"}]]}`
	if sent.method != "sendMessage" || string(markup) != want {
		t.Errorf("call %s, reply_markup = %s\nwant %s", sent.method, markup, want)
	}
}

// A webhook set before button presses were asked for is set again, at the
// same address with the secret; one already asking for every kind, or no
// webhook at all, is left alone.
func TestEnsureAllowedUpdatesSetsAnOldWebhookAgain(t *testing.T) {
	cases := map[string]struct {
		info    string
		changed bool
	}{
		"old kinds":  {`{"ok":true,"result":{"url":"https://bot.example/api/webhook","allowed_updates":["message","my_chat_member","chat_member"]}}`, true},
		"every kind": {`{"ok":true,"result":{"url":"https://bot.example/api/webhook","allowed_updates":["message","my_chat_member","chat_member","callback_query"]}}`, false},
		"no webhook": {`{"ok":true,"result":{"url":""}}`, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			client, api := newTestClient(t, c.info)

			changed, err := client.EnsureAllowedUpdates(context.Background(), "s3cret")

			if err != nil || changed != c.changed {
				t.Fatalf("changed = %v, %v; want %v", changed, err, c.changed)
			}
			calls := api.recorded()
			if !c.changed {
				if len(calls) != 1 {
					t.Errorf("calls = %+v, want only getWebhookInfo", calls)
				}
				return
			}
			if len(calls) != 2 || calls[1].method != "setWebhook" || calls[1].params["url"] != "https://bot.example/api/webhook" || calls[1].params["secret_token"] != "s3cret" {
				t.Errorf("calls = %+v", calls)
			}
		})
	}
}
