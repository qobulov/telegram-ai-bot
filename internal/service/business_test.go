package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

const (
	shopOwner    = int64(500)
	shopCustomer = int64(4200)
	shopLink     = "bc1"
)

// fakeBusinessBook keeps business connections, chats and answers in memory.
type fakeBusinessBook struct {
	mu          sync.Mutex
	connections map[string]storage.BusinessConnection
	ownerWrote  map[int64]time.Time
	seen        []storage.BusinessChat
	// left is how many answers each customer may still get; absent is any.
	left     map[int64]int
	reserved int64
	released []int64
	autoSet  []bool
}

func newFakeBusinessBook(connections ...storage.BusinessConnection) *fakeBusinessBook {
	f := &fakeBusinessBook{connections: map[string]storage.BusinessConnection{}, ownerWrote: map[int64]time.Time{}, left: map[int64]int{}}
	for _, c := range connections {
		f.connections[c.ID] = c
	}
	return f
}

func (f *fakeBusinessBook) RecordBusinessConnection(_ context.Context, c storage.BusinessConnection) (storage.BusinessConnection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c.AutoReply = true
	if old, ok := f.connections[c.ID]; ok {
		c.AutoReply = old.AutoReply
	}
	f.connections[c.ID] = c
	return c, nil
}

func (f *fakeBusinessBook) BusinessConnection(_ context.Context, id string) (storage.BusinessConnection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.connections[id]
	if !ok {
		return storage.BusinessConnection{}, storage.ErrUnknownBusinessConnection
	}
	return c, nil
}

func (f *fakeBusinessBook) OwnerBusinessConnection(_ context.Context, userID int64) (storage.BusinessConnection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.connections {
		if c.UserID == userID && c.Enabled && !c.Refused {
			return c, nil
		}
	}
	return storage.BusinessConnection{}, storage.ErrUnknownBusinessConnection
}

func (f *fakeBusinessBook) SetBusinessAutoReply(_ context.Context, userID int64, on bool) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.autoSet = append(f.autoSet, on)
	var n int64
	for id, c := range f.connections {
		if c.UserID == userID {
			c.AutoReply = on
			f.connections[id] = c
			n++
		}
	}
	return n, nil
}

func (f *fakeBusinessBook) SeeBusinessChat(_ context.Context, chat storage.BusinessChat, byOwner bool) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, chat)
	if byOwner {
		f.ownerWrote[chat.ChatID] = time.Now()
	}
	return f.ownerWrote[chat.ChatID], nil
}

func (f *fakeBusinessBook) ReserveBusinessReply(_ context.Context, q storage.BusinessQuota) (int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if left, limited := f.left[q.ChatID]; limited {
		if left == 0 {
			return 0, false, nil
		}
		f.left[q.ChatID] = left - 1
	}
	f.reserved++
	return f.reserved, true, nil
}

func (f *fakeBusinessBook) ReleaseBusinessReply(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, id)
	return nil
}

// fakeBusinessLookup answers getBusinessConnection.
type fakeBusinessLookup struct {
	connection telegram.BusinessConnection
	asked      int
}

func (f *fakeBusinessLookup) GetBusinessConnection(_ context.Context, id string) (telegram.BusinessConnection, error) {
	f.asked++
	if f.connection.ID != id {
		return telegram.BusinessConnection{}, errors.New("unknown")
	}
	return f.connection, nil
}

func shopConnection() storage.BusinessConnection {
	return storage.BusinessConnection{ID: shopLink, UserID: shopOwner, UserChatID: shopOwner, FirstName: "Aziz", CanReply: true, Enabled: true, AutoReply: true}
}

// businessFixture is a fixture whose owner, shopOwner, connected their
// business account.
func businessFixture() (*fixture, *fakeBusinessBook) {
	f := newFixture()
	book := newFakeBusinessBook(shopConnection())
	f.opts.Business = book
	f.opts.BusinessDailyLimit = 30
	f.opts.OwnerIDs = map[int64]bool{shopOwner: true}
	return f, book
}

// customerWrites is a customer's message to the owner's business account.
func customerWrites(text string) telegram.Update {
	nextUpdateID++
	return telegram.Update{UpdateID: nextUpdateID, BusinessMessage: &telegram.Message{
		MessageID:            nextUpdateID,
		BusinessConnectionID: shopLink,
		From:                 &telegram.User{ID: shopCustomer, FirstName: "Ali", Username: "ali"},
		Chat:                 telegram.Chat{ID: shopCustomer, Type: telegram.ChatTypePrivate, FirstName: "Ali"},
		Text:                 text,
	}}
}

// accountOwnerWrites is the owner's own message in their customer's chat.
func accountOwnerWrites(text string) telegram.Update {
	u := customerWrites(text)
	u.BusinessMessage.From = &telegram.User{ID: shopOwner, FirstName: "Aziz"}
	return u
}

func TestCustomerIsAnsweredOnTheAccountsBehalf(t *testing.T) {
	f, book := businessFixture()

	sent := f.run(customerWrites("narxi qancha?"))

	if len(sent) != 1 || sent[0].chatID != shopCustomer || sent[0].business != shopLink || sent[0].replyTo != 0 {
		t.Fatalf("sent = %+v, want one answer to the customer through the connection", sent)
	}
	calls := f.replier.recorded()
	if len(calls) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	req, caller := calls[0].req, calls[0].caller
	// Kept under the owner, apart from the customer's private chat.
	if req.Scope != (storage.Scope{ChatID: shopCustomer, UserID: shopOwner}) || req.UserID != shopCustomer || req.Group {
		t.Errorf("request = %+v", req)
	}
	if !caller.Business || caller.Owner || caller.UserID != shopCustomer || caller.ChatID != shopCustomer {
		t.Errorf("caller = %+v", caller)
	}
	if !strings.Contains(req.Context, "Telegram Business account") || !strings.Contains(req.Context, "Aziz") || strings.Contains(req.Context, ownerNotice) {
		t.Errorf("context = %q", req.Context)
	}
	if len(book.seen) != 1 || book.seen[0].FirstName != "Ali" || book.seen[0].Username != "ali" {
		t.Errorf("chats seen = %+v", book.seen)
	}
	if recorded := f.recorder.calls; len(recorded) != 1 {
		t.Errorf("sent answer not recorded: %+v", recorded)
	}
}

func TestBusinessAnswersGoTheSameWayThroughTheWebhook(t *testing.T) {
	f, _ := businessFixture()
	f.opts.Workers, f.opts.QueueSize = 0, 0
	s := New(f.opts)
	s.HandleNow(customerWrites("salom"))
	s.Close()
	if sent := f.sender.messages(); len(sent) != 1 || sent[0].business != shopLink {
		t.Errorf("sent = %+v", sent)
	}
}

// The owner's own message is never answered, and leaves the chat to them
// for a while; a message a bot sent for the account, her own answers
// among them, does not.
func TestOwnersOwnMessagesAreNotAnsweredAndPauseTheBot(t *testing.T) {
	f, book := businessFixture()
	byBot := accountOwnerWrites("bot's answer")
	byBot.BusinessMessage.SenderBusinessBot = &telegram.User{ID: botID, IsBot: true}

	if sent := f.run(byBot, customerWrites("rahmat")); len(sent) != 1 {
		t.Fatalf("after a bot's message: sent = %+v", sent)
	}
	f.sender.sent = nil
	if sent := f.run(accountOwnerWrites("10 daqiqada javob beraman"), customerWrites("yaxshi")); len(sent) != 0 {
		t.Fatalf("the owner's chat was answered: %+v", sent)
	}
	// Once the owner has been quiet for a while, the bot answers again.
	book.ownerWrote[shopCustomer] = time.Now().Add(-ownerTakeover - time.Minute)
	if sent := f.run(customerWrites("hali ham kutyapman")); len(sent) != 1 {
		t.Errorf("after the owner went quiet: sent = %+v", sent)
	}
}

func TestBusinessMessagesNotAnswered(t *testing.T) {
	cases := map[string]struct {
		change func(*fakeBusinessBook, *Options)
		update func() telegram.Update
	}{
		"auto-reply off": {func(b *fakeBusinessBook, _ *Options) {
			c := b.connections[shopLink]
			c.AutoReply = false
			b.connections[shopLink] = c
		}, func() telegram.Update { return customerWrites("salom") }},
		"no right to reply": {func(b *fakeBusinessBook, _ *Options) {
			c := b.connections[shopLink]
			c.CanReply = false
			b.connections[shopLink] = c
		}, func() telegram.Update { return customerWrites("salom") }},
		"disconnected": {func(b *fakeBusinessBook, _ *Options) {
			c := b.connections[shopLink]
			c.Enabled = false
			b.connections[shopLink] = c
		}, func() telegram.Update { return customerWrites("salom") }},
		"a refused account": {func(b *fakeBusinessBook, _ *Options) {
			c := b.connections[shopLink]
			c.Refused = true
			b.connections[shopLink] = c
		}, func() telegram.Update { return customerWrites("salom") }},
		"an unknown connection": {nil, func() telegram.Update {
			u := customerWrites("salom")
			u.BusinessMessage.BusinessConnectionID = "other"
			return u
		}},
		"another owner writing": {func(_ *fakeBusinessBook, o *Options) { o.OwnerIDs[shopCustomer] = true }, func() telegram.Update { return customerWrites("salom") }},
		"another bot": {nil, func() telegram.Update {
			u := customerWrites("salom")
			u.BusinessMessage.From.IsBot = true
			return u
		}},
		"no business book": {func(_ *fakeBusinessBook, o *Options) { o.Business = nil }, func() telegram.Update { return customerWrites("salom") }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f, book := businessFixture()
			if c.change != nil {
				c.change(book, &f.opts)
			}
			if sent := f.run(c.update()); len(sent) != 0 || len(f.replier.recorded()) != 0 {
				t.Errorf("sent = %+v, model asked %d times", sent, len(f.replier.recorded()))
			}
		})
	}
}

// A connection made before the bot asked for business updates is learnt
// from Telegram, and kept, at its first message.
func TestUnknownConnectionIsAskedOfTelegram(t *testing.T) {
	f, book := businessFixture()
	delete(book.connections, shopLink)
	lookup := &fakeBusinessLookup{connection: telegram.BusinessConnection{
		ID: shopLink, User: telegram.User{ID: shopOwner, FirstName: "Aziz"}, UserChatID: shopOwner, IsEnabled: true,
		Rights: &telegram.BusinessBotRights{CanReply: true},
	}}
	f.opts.BusinessLookup = lookup

	if sent := f.run(customerWrites("salom"), customerWrites("bormisiz")); len(sent) != 2 || sent[0].business != shopLink {
		t.Fatalf("sent = %+v", sent)
	}
	if lookup.asked != 1 {
		t.Errorf("Telegram asked %d times, want once", lookup.asked)
	}
	if c := book.connections[shopLink]; c.UserID != shopOwner || !c.CanReply || c.Refused {
		t.Errorf("recorded = %+v", c)
	}
}

func TestBusinessConnectionUpdatesAreRecorded(t *testing.T) {
	f := newFixture()
	book := newFakeBusinessBook()
	f.opts.Business = book
	f.opts.OwnerIDs = map[int64]bool{shopOwner: true}
	connect := func(userID int64) telegram.Update {
		return telegram.Update{BusinessConnection: &telegram.BusinessConnection{
			ID: "bc" + string(rune('0'+userID%10)), User: telegram.User{ID: userID}, UserChatID: userID, IsEnabled: true,
			Rights: &telegram.BusinessBotRights{CanReply: true, CanManageStories: true},
		}}
	}

	sent := f.run(connect(shopOwner), connect(shopCustomer+1))

	if len(sent) != 1 || sent[0].chatID != shopOwner || !strings.Contains(sent[0].text, "Auto-reply is on") || !strings.Contains(sent[0].text, "Stories: ask me") {
		t.Fatalf("sent = %+v, want the owner told alone", sent)
	}
	var owner, stranger storage.BusinessConnection
	for _, c := range book.connections {
		if c.UserID == shopOwner {
			owner = c
		} else {
			stranger = c
		}
	}
	if owner.Refused || !owner.CanReply || !owner.CanManageStories || !stranger.Refused {
		t.Errorf("owner = %+v, stranger = %+v", owner, stranger)
	}

	// Disconnecting is recorded, and the owner told.
	gone := connect(shopOwner)
	gone.BusinessConnection.IsEnabled = false
	f.sender.sent = nil
	if sent := f.run(gone); len(sent) != 1 || sent[0].text != textBusinessGone {
		t.Errorf("on disconnect: %+v", sent)
	}
}

// Only owners known by ID count: a username can change hands, and the
// account speaks for whoever holds it.
func TestBusinessConnectionNeedsAnOwnerByID(t *testing.T) {
	f := newFixture()
	book := newFakeBusinessBook()
	f.opts.Business = book
	f.opts.OwnerUsernames = []string{"aziz"}
	f.run(telegram.Update{BusinessConnection: &telegram.BusinessConnection{ID: shopLink, User: telegram.User{ID: shopOwner, Username: "aziz"}, IsEnabled: true}})
	if c := book.connections[shopLink]; !c.Refused {
		t.Errorf("connection = %+v, want refused", c)
	}
}

func TestCustomerDailyLimitIsSilent(t *testing.T) {
	f, book := businessFixture()
	book.left[shopCustomer] = 1

	sent := f.run(customerWrites("birinchi"), customerWrites("ikkinchi"))

	if len(sent) != 1 || len(f.replier.recorded()) != 1 {
		t.Errorf("sent = %+v; want the first answered and the second left without a notice", sent)
	}
}

// A failed turn sends the customer nothing, not even the error notice a
// private chat gets, and gives the answer back.
func TestFailedBusinessTurnSendsNothing(t *testing.T) {
	f, book := businessFixture()
	f.replier.err = errors.New("provider down")

	if sent := f.run(customerWrites("salom")); len(sent) != 0 {
		t.Errorf("sent = %+v", sent)
	}
	if len(book.released) != 1 {
		t.Errorf("released = %v, want the answer given back", book.released)
	}
}

// Words and pictures reach a customer; a reaction or a sticker, which do
// not go through the account, sends nothing.
func TestOnlyWordsAndPicturesReachACustomer(t *testing.T) {
	f, _ := businessFixture()
	f.replier.draws = 1
	f.replier.answer = "Mana rasm"
	sent := f.run(customerWrites("rasm chiz"))
	if len(sent) != 1 || !sent[0].photo || sent[0].business != shopLink || sent[0].text != "Mana rasm" {
		t.Errorf("picture: %+v", sent)
	}

	f, _ = businessFixture()
	f.replier.reaction = "👍"
	if sent := f.run(customerWrites("rahmat")); len(sent) != 0 || len(f.reactor.recorded()) != 0 {
		t.Errorf("reaction: sent %+v, reactions %+v", sent, f.reactor.recorded())
	}

	f, _ = businessFixture()
	f.replier.sticker = "happy"
	if sent := f.run(customerWrites("zo'r")); len(sent) != 0 {
		t.Errorf("sticker: %+v", sent)
	}
}

// The bot's commands are for its own chats: a customer's "/help" is text
// for the model, never the bot's help.
func TestCommandsAreTextInABusinessChat(t *testing.T) {
	f, _ := businessFixture()
	sent := f.run(customerWrites("/help"))
	if len(sent) != 1 || sent[0].text != "echo: /help" {
		t.Errorf("sent = %+v", sent)
	}
}

func TestBusinessCommand(t *testing.T) {
	f, book := businessFixture()

	sent := f.run(textFrom(shopOwner, "/business"), textFrom(shopOwner, "/business off"), textFrom(shopOwner, "/business maybe"), textFrom(userID, "/business"))

	if len(sent) != 4 {
		t.Fatalf("sent = %+v", sent)
	}
	if !strings.Contains(sent[0].text, "Auto-reply is on") || !strings.Contains(sent[1].text, "Auto-reply is off") || sent[2].text != textBusinessUsage {
		t.Errorf("owner's replies = %q / %q / %q", sent[0].text, sent[1].text, sent[2].text)
	}
	if sent[3].text != textUnknown.uz {
		t.Errorf("a member's /business = %q", sent[3].text)
	}
	if len(book.autoSet) != 1 || book.autoSet[0] {
		t.Errorf("auto-reply set = %v", book.autoSet)
	}
	// Turned off, the customers are no longer answered.
	f.sender.sent = nil
	if sent := f.run(customerWrites("salom")); len(sent) != 0 {
		t.Errorf("answered with auto-reply off: %+v", sent)
	}
}

func TestBusinessCommandWithoutAnAccountSaysHowToConnect(t *testing.T) {
	f, book := businessFixture()
	delete(book.connections, shopLink)
	sent := f.run(textFrom(shopOwner, "/business"))
	if len(sent) != 1 || !strings.Contains(sent[0].text, "Telegram Business → Chatbots") || !strings.Contains(sent[0].text, "@"+botUsername) {
		t.Errorf("sent = %+v", sent)
	}
}
