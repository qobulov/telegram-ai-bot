# Card Arena (5v5) and Fusion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace card gifting and melting with Fusion (copies raise a character to ★5) and add an asynchronous 5v5 Arena with saved teams, auto-battles replayed in the cards Mini App.

**Architecture:** A pure Go battle engine (`internal/arena`, no I/O, deterministic from teams + seed) is called by the cards Mini App API (`internal/cards`), which keeps teams, ratings, limits and battles in Postgres through `internal/storage`. The Mini App (`public/kartalar`) gets a Fuse button in the card view and an Arena tab (team editor, opponents, replay, table).

**Tech Stack:** Go, pgx/Postgres migrations (`internal/storage/migrations`), vanilla JS/CSS Mini App.

**Spec:** `docs/superpowers/specs/2026-11-18-arena-and-fusion-design.md` (approved 2026-11-18 with one change: **gifting is removed entirely**; cards change hands on the market only).

## Global Constraints
- Fusion costs (copies of the same character): ★1→★2 1, ★2→★3 2, ★3→★4 3, ★4→★5 4; max ★5.
- Star multipliers on attack and health: ★1 1.00, ★2 1.20, ★3 1.45, ★4 1.70, ★5 2.00.
- Team: 5 distinct owned characters, 2 front + 3 back.
- Classes Tank, Fighter, Mage; skills only for Mythic and Legendary: Strike ×2.5 every 3rd own turn, Heal 25% of max health to the weakest ally every 4th own turn, Shield absorbing 30% of max health once.
- Battle: speed Mage > Fighter > Tank, ties by attack; at most 30 rounds, then more total health left wins (the defender wins an exact tie).
- Arena: 5 attacks a day; win +1 🎟 token, at most 3 arena tokens a day; Elo start 1000, K=32, floor 0; defending never changes tokens. Days are Tashkent days, as the cards' `CardDay`.
- No stakes, no group announcements; `/card` stays the one command.
- Limited cards (`storage.CardLimited`) may be fused and put in a team (owned, not moved).
- Collection points and the existing card table are unchanged; stars add to arena power only.
- Every user-facing text in uz/ru/en, as the app's existing text tables; Uzbek Latin first.
- Each task: gofmt clean, `go vet ./...`, full suite `TEST_DB_POSTGRES_URL="postgres://n@localhost:5432/tgbot_test_flood?sslmode=disable" go test -p 1 -count=1 ./...`, `node --check public/kartalar/app.js`; bump `?v=` of app.js/style.css in `public/kartalar/index.html` when they change; new tables in the DROP lists of `internal/storage/store_test.go`. One commit per task, long plain-English message in the repo's style.
- Prod migrations run right before the deploy that needs them (`CheckSchema` requires them first).

## Review Focus
1. A saved team holding a card the player traded away on the market: attacking and being attacked keep working; that player is not offered as an opponent and re-saving refuses. → Task 5 `TestTeamWithACardTradedAway`.
2. Two attacks at once (double tap, two devices): limits, tokens and rating count once each, never a 6th attack or 4th token. → Task 5 `TestAttacksAreCountedUnderALock`.
3. The Tashkent day turning while counters hold yesterday's date: counters reset by date. → Task 5 `TestArenaDayStartsAtTashkentMidnight`.
4. Fusing away the free copy of a card listed in an open market offer: the offer can't then be taken. → Task 3 `TestFuseThenOfferedCopyIsGone`.
5. Few players with teams (0, 1, 2): opponents never include oneself and never error. → Task 5 `TestOpponentsWhenFewHaveTeams`.

---

### Task 1: Remove card gifting

**Files:**
- Modify: `internal/cards/handler.go` (drop actions `gift` and `players`, `gift()`, `tellGift`, `giftTexts`, `Store` methods `GiveCard`, `CardPlayersNamed`), `internal/cards/handler_test.go`
- Modify: `internal/storage/cardtrade.go` (delete `GiveCard` if nothing else calls it; keep `moveCard`), `internal/storage/cardtrade_test.go`, and `CardPlayersNamed` if unused
- Modify: `public/kartalar/app.js|style.css|index.html` (gift button, panel, texts), `README.md`, `internal/config/persona.go`

**Interfaces:** Produces: actions `gift`, `players` → 400 (`errBadRequest`).

- [ ] **Step 1:** `TestGiftIsGone` in `handler_test.go`: actions `"gift"` and `"players"` → 400.
- [ ] **Step 2:** `go test ./internal/cards -run TestGiftIsGone` → FAIL.
- [ ] **Step 3:** Remove the code above; grep for `GiveCard`, `CardPlayersNamed`, gift texts, persona/README gifting lines; keep trade texts.
- [ ] **Step 4:** Full suite + node check → PASS.
- [ ] **Step 5:** Commit.

### Task 2: Remove melting

**Files:**
- Modify: `internal/cards/handler.go` (drop action `melt`, `melt()`), `internal/cards` (delete `ShardRates`, `SpareWorth` if unused), `internal/storage/cardshards.go` (delete `MeltCards`; keep `CardWallet` shards), tests
- Modify: `public/kartalar/*` (melt block and texts `melt`, `noSpare`, `melted`, `meltTitle`, `meltWarn`, `meltDo`, `ratesLabel`; `shardsHint` reworded to the shop's uses), README, persona

**Interfaces:** Produces: action `melt` → 400. `card_owned.melted` now means "copies used up", by fusion from Task 3.

- [ ] **Step 1:** `TestMeltIsGone`: action `"melt"` → 400.
- [ ] **Step 2:** FAIL. **Step 3:** Remove; held crystals stay with their shop uses. **Step 4:** PASS. **Step 5:** Commit.

### Task 3: Fusion

**Files:**
- Create: `internal/storage/migrations/0095_card_stars.sql` — `ALTER TABLE card_owned ADD COLUMN stars SMALLINT NOT NULL DEFAULT 1 CHECK (stars BETWEEN 1 AND 5);`
- Create: `internal/cards/fusion.go`, `internal/cards/fusion_test.go`, `internal/storage/cardfusion.go`, `internal/storage/cardfusion_test.go`
- Modify: `internal/storage/cards.go` (`OwnedCard.Stars`, read in `OwnedCards`), `internal/cards/handler.go` (action `fuse`; `cardView.Stars`, `cardView.Free`), `public/kartalar/*` (stars on tiles and large view; "⭐ Fuse" button with cost and new numbers), README, persona

**Interfaces:**
- Produces (cards): `var FuseCost = [5]int{0, 1, 2, 3, 4}` (index = current stars), `var StarMultiplier = [6]float64{0, 1.00, 1.20, 1.45, 1.70, 2.00}`, `func Boosted(c storage.Card, stars int) (attack, health int)` (rounded to tens like `rounded`).
- Produces (storage): `func (s *Store) FuseCard(ctx context.Context, userID, cardID int64, cost []int) (stars int, err error)`; errors `ErrCardMaxStars`, `ErrCardFewCopies`, `ErrCardNotHeld`. One transaction, row `FOR UPDATE`; free copies = `count - 1 - melted`; needs `free >= cost[stars]`; then `melted += cost`, `stars += 1`.
- Produces (handler): action `"fuse"` (`card`) → the state with the new stars, or `{"refused": "max"|"few"|"not_held"}`.

- [ ] **Step 1:** `TestFusionCosts`: costs ★1→★5 sum to 10; `Boosted` of attack 5000 / health 18000 is 10000/36000 at ★5 and 7250/26100 at ★3.
- [ ] **Step 2:** `TestFuseCard` (storage): count 4 at ★1 → ★2 (melted 1) → ★3 (melted 3) → `ErrCardFewCopies`; enough copies reach ★5 then `ErrCardMaxStars`; unheld → `ErrCardNotHeld`. `TestFuseThenOfferedCopyIsGone`: an open offer giving the card is refused on take once its free copies are fused away.
- [ ] **Step 3:** FAIL.
- [ ] **Step 4:** Implement; app shows "★2 → ★3 · 2 nusxa", disabled with the reason at max or with too few copies, re-rendering from the returned state.
- [ ] **Step 5:** Full suite + node check → PASS; commit.

### Task 4: Battle engine

**Files:** Create `internal/arena/arena.go`, `internal/arena/arena_test.go`.

**Interfaces (produces):**
- `type Class int` (`Tank`, `Fighter`, `Mage`); `type Skill int` (`NoSkill`, `Strike`, `Heal`, `Shield`).
- `type Fighter struct { CardID int64; Name string; Rarity, Stars, Attack, Health int; Class Class; Skill Skill }`
- `func FighterOf(id int64, name string, rarity, stars, attack, health int) Fighter` — attack/health already boosted by the caller; class by health/attack ratio (ratio ≥ 4.0 Tank, ≤ 3.0 Mage, else Fighter); skill for rarity ≥ 3 by `id % 3` (Strike, Heal, Shield).
- `type Team struct { Front [2]Fighter; Back [3]Fighter }`; `func (t Team) Power() int` = Σ(attack + health/4).
- `type Event struct { Round, Side, From, To int; Kind string; Amount, HP int }` — Kind "hit", "strike", "heal", "shield", "fall"; slots 0–1 front, 2–4 back.
- `type Result struct { AttackerWon bool; Rounds int; Events []Event }`; `func Battle(attacker, defender Team, seed int64) Result`.
- Rules: each round all living act by speed (Mage 3, Fighter 2, Tank 1), ties by attack, then attacker's side first, then slot; Tank/Fighter hit the first living front, else back; Mage hits the living back with least health, else front; damage = attack × factor in [0.9, 1.1] from `rand.New(rand.NewSource(seed))`; a Shield absorbs first; ≤ 30 rounds.

- [ ] **Step 1:** Tests `TestBattleIsDeterministic`, `TestStrongerTeamWins` (★5 Mythics vs ★1 Commons, under 30 rounds), `TestTimeoutGoesByHealthLeft` (all-Tank teams), `TestMageHitsTheBackRow`, `TestHealAndShieldFire`, `TestFighterOfIsStable`.
- [ ] **Step 2:** FAIL. **Step 3:** Implement. **Step 4:** PASS. **Step 5:** Commit.

### Task 5: Arena storage

**Files:** Create `internal/storage/migrations/0096_arena.sql`, `internal/storage/arena.go`, `internal/storage/arena_test.go`; modify `internal/storage/store_test.go`.

Migration: `arena_teams(user_id BIGINT PRIMARY KEY, front BIGINT[] NOT NULL, back BIGINT[] NOT NULL, power INT NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now())`; `arena_players(user_id BIGINT PRIMARY KEY, rating INT NOT NULL DEFAULT 1000, wins INT NOT NULL DEFAULT 0, losses INT NOT NULL DEFAULT 0, attacks_day DATE, attacks INT NOT NULL DEFAULT 0, tokens_day DATE, tokens_won INT NOT NULL DEFAULT 0)` with an index on `rating`; `arena_battles(id BIGSERIAL PRIMARY KEY, attacker BIGINT NOT NULL, defender BIGINT NOT NULL, seed BIGINT NOT NULL, attacker_won BOOLEAN NOT NULL, rating_change INT NOT NULL, at TIMESTAMPTZ NOT NULL DEFAULT now())`.

**Interfaces (produces):**
- `type ArenaRules struct { Attacks, Tokens, K int; Day time.Time }`
- `type ArenaPlayer struct { UserID int64; Name string; Rating, Wins, Losses, AttacksLeft, TokensLeft, Power int }`
- `type ArenaOutcome struct { RatingChange int; Token bool; AttacksLeft, TokensLeft int }`
- `SaveArenaTeam(ctx, userID int64, front [2]int64, back [3]int64, power int) error` — `ErrCardNotHeld`, `ErrArenaTeamRepeat`.
- `ArenaTeam(ctx, userID int64) (front [2]int64, back [3]int64, ok bool, err error)`
- `ArenaPlayerOf(ctx, userID int64, rules ArenaRules) (ArenaPlayer, error)`
- `ArenaOpponents(ctx, userID int64, n int) ([]ArenaPlayer, error)` — teams whose cards are all still held, nearest rating first, never oneself.
- `RecordArenaBattle(ctx, attacker, defender, seed int64, attackerWon bool, rules ArenaRules) (ArenaOutcome, error)` — one transaction, both rows locked in id order; `ErrArenaNoAttacks`; Elo for both (floor 0); +1 `card_players.tokens` on a win while `tokens_won < Tokens`; inserts the battle.
- `ArenaTop(ctx, limit int) ([]ArenaPlayer, error)`

- [ ] **Step 1:** Tests: save/read; repeats and unheld refused; Review Focus tests 1, 2, 3, 5; Elo 1000 vs 1000 → +16/−16.
- [ ] **Step 2:** FAIL. **Step 3:** Implement. **Step 4:** PASS. **Step 5:** Commit.

### Task 6: Arena API

**Files:** Create `internal/cards/arena.go`, `internal/cards/arena_test.go`; modify `internal/cards/handler.go` (request fields `Front []int64`, `Back []int64`, `Opponent int64`; `Store` additions; routes).

**Interfaces:** Consumes Tasks 3–5. Produces actions:
- `"arena"` → `{player, team (5 card views or null), opponents (≤3: id, name, rating, power), top (10)}`
- `"arena_team"` (front, back) → saves; power computed server-side from held cards and stars; refusals `"not_held"`, `"repeat"`, `"size"`.
- `"arena_attack"` (opponent) → both teams from storage, fighters boosted by stars, seed from `crypto/rand`, `arena.Battle`, `RecordArenaBattle` → `{won, events, attacker, defender (fighter views), rating_change, token, attacks_left, tokens_left}`; refusals `"no_attacks"`, `"no_team"`, `"opponent_gone"`.
- Constants `ArenaAttacks = 5`, `ArenaTokens = 3`, `ArenaK = 32`, `ArenaOpponentsShown = 3`.

- [ ] **Step 1:** Tests (fake store): team saved with positive power; attack returns events and decrements attacks; 6th refused; no team refused; opponent without a team refused.
- [ ] **Step 2:** FAIL. **Step 3:** Implement. **Step 4:** PASS. **Step 5:** Commit.

### Task 7: Arena tab in the Mini App

**Files:** Modify `public/kartalar/index.html` (section `tab-arena`, nav button ⚔️ "Arena", nav grid 6 columns), `app.js`, `style.css`, `README.md`, `internal/config/persona.go` (one sentence on arena, fusion, no gifts).

- [ ] **Step 1:** Team editor: 2 front + 3 back slots; tap a slot → pick from held cards (search, by rarity, stars shown); save → `arena_team`; power shown.
- [ ] **Step 2:** Opponents: 3 cards (name, rating, power, "⚔️ Hujum"), refresh, attacks and tokens left today.
- [ ] **Step 3:** Replay: two rows, one event every ~350 ms (hit shake, HP bar falls, skill flash, fallen greyed), "O'tkazib yuborish" skips to the end; result with rating change and token; reduced motion shows the end at once.
- [ ] **Step 4:** Arena table in the Ranking tab.
- [ ] **Step 5:** Harness check at 390 px (save, attack, replay, skip); node check; bump assets; commit.

### Deploy
Full suite green → run `bot migrate` on prod (0095, 0096) → push at once → check Vercel Ready and logs.
