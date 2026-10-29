package storage

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

const (
	businessOwner    int64 = 700
	businessCustomer int64 = 4200
)

func recordConnection(t *testing.T, store *Store, c BusinessConnection) BusinessConnection {
	t.Helper()
	stored, err := store.RecordBusinessConnection(context.Background(), c)
	if err != nil {
		t.Fatalf("RecordBusinessConnection: %v", err)
	}
	return stored
}

func TestBusinessConnectionRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	stored := recordConnection(t, store, BusinessConnection{ID: "bc1", UserID: businessOwner, UserChatID: businessOwner, FirstName: "Owner", CanReply: true, Enabled: true})
	if !stored.AutoReply {
		t.Errorf("a new connection answers customers: %+v", stored)
	}
	got, err := store.BusinessConnection(ctx, "bc1")
	if err != nil || got.UserID != businessOwner || !got.CanReply || got.CanManageStories || !got.Enabled || got.Refused || got.FirstName != "Owner" {
		t.Fatalf("BusinessConnection = %+v, %v", got, err)
	}
	if _, err := store.BusinessConnection(ctx, "nope"); !errors.Is(err, ErrUnknownBusinessConnection) {
		t.Errorf("unknown connection: %v", err)
	}

	// Telegram reports the same connection again with other rights.
	recordConnection(t, store, BusinessConnection{ID: "bc1", UserID: businessOwner, UserChatID: businessOwner, CanReply: true, CanManageStories: true, Enabled: true})
	if got, _ := store.BusinessConnection(ctx, "bc1"); !got.CanManageStories {
		t.Errorf("rights were not updated: %+v", got)
	}
	// The owner is a user, for the conversations kept under their ID.
	var users int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE telegram_id = $1`, businessOwner).Scan(&users); err != nil || users != 1 {
		t.Errorf("owner as user: %d, %v", users, err)
	}
}

func TestRefusedBusinessConnectionIsNoUserAndNeverTheOwners(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	recordConnection(t, store, BusinessConnection{ID: "stranger", UserID: 99, UserChatID: 99, CanReply: true, Enabled: true, Refused: true})

	if rows := countRows(t, store, "users"); rows != 0 {
		t.Errorf("users = %d, want 0", rows)
	}
	if _, err := store.OwnerBusinessConnection(ctx, 99); !errors.Is(err, ErrUnknownBusinessConnection) {
		t.Errorf("a refused connection was found: %v", err)
	}
	if n, err := store.SetBusinessAutoReply(ctx, 99, false); err != nil || n != 0 {
		t.Errorf("SetBusinessAutoReply on a refused connection = %d, %v", n, err)
	}
}

func TestOwnerBusinessConnectionIsTheLatestEnabled(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	recordConnection(t, store, BusinessConnection{ID: "old", UserID: businessOwner, Enabled: true})
	recordConnection(t, store, BusinessConnection{ID: "new", UserID: businessOwner, Enabled: true, CanManageStories: true})
	recordConnection(t, store, BusinessConnection{ID: "off", UserID: businessOwner, Enabled: false})

	got, err := store.OwnerBusinessConnection(ctx, businessOwner)
	if err != nil || got.ID != "new" {
		t.Errorf("OwnerBusinessConnection = %+v, %v; want new", got, err)
	}
}

func TestBusinessAutoReplyIsTheOwnersChoice(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	recordConnection(t, store, BusinessConnection{ID: "bc1", UserID: businessOwner, Enabled: true})

	if n, err := store.SetBusinessAutoReply(ctx, businessOwner, false); err != nil || n != 1 {
		t.Fatalf("SetBusinessAutoReply = %d, %v", n, err)
	}
	// Telegram reporting the connection again keeps the choice, and a new
	// connection of the same owner takes it.
	if again := recordConnection(t, store, BusinessConnection{ID: "bc1", UserID: businessOwner, Enabled: true}); again.AutoReply {
		t.Errorf("recording again turned auto-reply back on: %+v", again)
	}
	if next := recordConnection(t, store, BusinessConnection{ID: "bc2", UserID: businessOwner, Enabled: true}); next.AutoReply {
		t.Errorf("a new connection forgot the owner's choice: %+v", next)
	}
	if other := recordConnection(t, store, BusinessConnection{ID: "bc3", UserID: businessOwner + 1, Enabled: true}); !other.AutoReply {
		t.Errorf("another owner's connection took the choice: %+v", other)
	}
}

func TestSeeBusinessChatNotesWhenTheOwnerWrote(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	recordConnection(t, store, BusinessConnection{ID: "bc1", UserID: businessOwner, Enabled: true})

	wrote, err := store.SeeBusinessChat(ctx, BusinessChat{ConnectionID: "bc1", ChatID: businessCustomer, FirstName: "Ali", Username: "ali"}, false)
	if err != nil || !wrote.IsZero() {
		t.Fatalf("customer's message: %v, %v", wrote, err)
	}
	wrote, err = store.SeeBusinessChat(ctx, BusinessChat{ConnectionID: "bc1", ChatID: businessCustomer}, true)
	if err != nil || time.Since(wrote) > time.Minute {
		t.Fatalf("owner's message: %v, %v", wrote, err)
	}
	if again, err := store.SeeBusinessChat(ctx, BusinessChat{ConnectionID: "bc1", ChatID: businessCustomer, FirstName: "Ali", Username: "ali"}, false); err != nil || !again.Equal(wrote) {
		t.Errorf("the owner's time was lost: %v, %v; want %v", again, err, wrote)
	}
	if _, err := store.SeeBusinessChat(ctx, BusinessChat{ConnectionID: "bc1", ChatID: businessCustomer + 1, FirstName: "Vali"}, false); err != nil {
		t.Fatal(err)
	}

	chats, err := store.BusinessChats(ctx, "bc1", 10)
	if err != nil || len(chats) != 2 {
		t.Fatalf("BusinessChats = %+v, %v", chats, err)
	}
	if chats[0].ChatID != businessCustomer+1 || !chats[0].OwnerWroteAt.IsZero() {
		t.Errorf("latest chat = %+v", chats[0])
	}
	// The owner's message, which names no customer, kept the name.
	if chats[1].FirstName != "Ali" || chats[1].Username != "ali" || chats[1].OwnerWroteAt.IsZero() {
		t.Errorf("Ali's chat = %+v", chats[1])
	}
}

func TestReserveBusinessReplyCountsEachCustomer(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	since := time.Now().Add(-time.Hour)
	quota := BusinessQuota{ConnectionID: "bc1", ChatID: businessCustomer, Since: since, Limit: 2}

	var ids []int64
	for range 2 {
		id, allowed, err := store.ReserveBusinessReply(ctx, quota)
		if err != nil || !allowed || id == 0 {
			t.Fatalf("ReserveBusinessReply = %d, %v, %v", id, allowed, err)
		}
		ids = append(ids, id)
	}
	if _, allowed, err := store.ReserveBusinessReply(ctx, quota); err != nil || allowed {
		t.Fatalf("third reply allowed = %v, %v", allowed, err)
	}
	other := quota
	other.ChatID++
	if _, allowed, _ := store.ReserveBusinessReply(ctx, other); !allowed {
		t.Error("another customer was limited by the first's answers")
	}
	if err := store.ReleaseBusinessReply(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, allowed, _ := store.ReserveBusinessReply(ctx, quota); !allowed {
		t.Error("a released answer was not given back")
	}
	quota.Since = time.Now().Add(time.Minute)
	if _, allowed, _ := store.ReserveBusinessReply(ctx, quota); !allowed {
		t.Error("yesterday's answers counted today")
	}
}

func TestBusinessChecklistKeepsTheLatest(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	recordConnection(t, store, BusinessConnection{ID: "bc1", UserID: businessOwner, Enabled: true})

	if _, err := store.LastBusinessChecklist(ctx, "bc1", businessCustomer); !errors.Is(err, ErrNoChecklist) {
		t.Errorf("no checklist yet: %v", err)
	}
	first := BusinessChecklist{ConnectionID: "bc1", ChatID: businessCustomer, MessageID: 10, Title: "Buyurtma", Tasks: []string{"To'lov"}}
	second := BusinessChecklist{ConnectionID: "bc1", ChatID: businessCustomer, MessageID: 11, Title: "Yetkazish", Tasks: []string{"Manzil", "Vaqt"}}
	for _, c := range []BusinessChecklist{first, second} {
		if err := store.SaveBusinessChecklist(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.LastBusinessChecklist(ctx, "bc1", businessCustomer)
	if err != nil || got.MessageID != 11 || got.Title != "Yetkazish" || !slices.Equal(got.Tasks, second.Tasks) {
		t.Fatalf("LastBusinessChecklist = %+v, %v", got, err)
	}
	second.Tasks = append(second.Tasks, "Chek")
	if err := store.SaveBusinessChecklist(ctx, second); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.LastBusinessChecklist(ctx, "bc1", businessCustomer); len(got.Tasks) != 3 {
		t.Errorf("edited checklist = %+v", got)
	}
}

// A customer's conversation with the business account is kept under the
// account's owner, apart from the customer's own private chat with the bot.
func TestBusinessConversationIsApartFromThePrivateOne(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	recordConnection(t, store, BusinessConnection{ID: "bc1", UserID: businessOwner, Enabled: true})
	business := Scope{ChatID: businessCustomer, UserID: businessOwner}

	if _, err := store.SaveTurn(ctx, Turn{Scope: business, UpdateID: 1, AuthorID: businessCustomer, Messages: turn("narxi?", "100 ming")}); err != nil {
		t.Fatalf("SaveTurn: %v", err)
	}
	mustSave(t, store, businessCustomer, 2, turn("salom Mari", "salom!"))

	history, err := store.History(ctx, business, 10)
	if err != nil || len(history) != 2 || history[0].Content != "narxi?" {
		t.Fatalf("business history = %+v, %v", history, err)
	}
	if private := mustHistory(t, store, businessCustomer, 10); len(private) != 2 || private[0].Content != "salom Mari" {
		t.Errorf("private history = %+v", private)
	}
	// Nor is it recalled as talk elsewhere, for the customer or the owner.
	for _, person := range []int64{businessCustomer, businessOwner} {
		got, err := store.ExchangesElsewhere(ctx, person, group, time.Now().Add(-time.Hour), 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range got {
			if e.Asked == "narxi?" {
				t.Errorf("the business chat was recalled for %d: %+v", person, got)
			}
		}
	}
}
