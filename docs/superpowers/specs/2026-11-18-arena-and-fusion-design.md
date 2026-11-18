# Card Arena (5v5) and Fusion — design

Date: 2026-11-18. Owner's request (Uzbek): "duel uchun nimadir o'ylab tur 5v5 karta duel bo'ladi, xuddi Souls (Habby) kabi; endi melt bo'lmaydi, characterga fusion bo'ladi, max 5 star uchun o'zini o'ziga fuse qiladi."

## Why
The cards game has collecting (draws, market, gifts) but nothing to *do* with a collection since /duel was removed. Duplicates are melted into crystals, which feeds nothing a player cares about. The owner wants a reason to collect and to come back daily: a 5v5 auto-battle like Habby's *Souls*, and duplicates that make a character stronger instead of turning into crystals.

## Decided by the owner
- **Arena with saved teams** (asynchronous): each player saves a defence team; anyone attacks others' saved teams at any time. No waiting for an opponent.
- **No stakes**: the winner earns tokens and rating; the loser loses only rating. (The DB showed alt-account funnelling; stakes would invite it.)
- **No more melt**: duplicates fuse into the same character; max ★5.

## Fusion
- Every owned character has stars, ★1 when first drawn.
- Fusing uses copies of the *same* character: ★1→★2 costs 1 copy, ★2→★3 2, ★3→★4 3, ★4→★5 4 (10 copies past the first). *(my call)*
- Each star multiplies attack and health: ★1 ×1.00, ★2 ×1.20, ★3 ×1.45, ★4 ×1.70, ★5 ×2.00. *(my call)*
- The fuse is the player's choice: a "⭐ Fuse" button in the card's large view, showing the cost and the new numbers; one transaction. Copies stay giftable/tradeable until fused.
- Melt goes: its button, texts and API action are removed; crystals already held stay with their other uses (shop: tokens, packs).
- Collection points and ranking are unchanged; stars add to arena power only.

## Arena (5v5 auto-battle)
- **Team**: 5 distinct owned characters: 2 front, 3 back; saved as the defence team and used to attack. Edited in a new "Arena" tab of the Mini App (the slot the shop tab freed).
- **Class** per character, fixed, derived from id and stats: Tank (most health; enemies hit the front first), Fighter (hits the nearest), Mage (hits the back row). Mythic and Legendary get one skill each, derived by id: Strike (×2.5 hit every 3rd turn), Heal (heals the weakest ally 25% every 4th turn), Shield (absorbs 30% of max health once). *(my call)*
- **Battle**: server-side and deterministic from both teams and a seed; speed by class (Mage > Fighter > Tank, ties by attack); at most 30 turns, then more total health left wins. The server returns the event log; the Mini App replays it in ~15–20 s (strikes, HP bars, skill flashes), skippable.
- **Opponents**: 3 offered near the player's rating, with team power shown, and a refresh; players without a saved team are not offered.
- **Limits and rewards**: 5 attacks a day. Win: +1 🎟 token (at most 3 a day from the arena) and rating up; loss: rating down; defending never changes tokens. Elo-like rating, start 1000, K=32. *(my call)*
- **Ranking**: an Arena table in the Ranking tab (rating, wins).
- `/card` stays the one command; the arena lives in the Mini App, with no group announcements.

## Out of scope (v1)
Live challenges in groups, stakes, anime/element synergies, equipment, PvE campaign, weekly rewards.

## Data (migration needed)
- `card_owned`: add `stars SMALLINT NOT NULL DEFAULT 1`.
- `arena_teams (user_id PK, front BIGINT[2], back BIGINT[3], power INT, updated_at)`.
- `arena_players (user_id PK, rating INT DEFAULT 1000, wins, losses, attacks_day DATE, attacks INT, tokens_day DATE, tokens_won INT)`.
- `arena_battles (id, attacker, defender, seed, attacker_won BOOL, rating_change INT, at)` for history and replays.

## Left to the builder
Exact class thresholds, skill numbers within the ranges above, replay animation details and the matching window — kept simple and tunable in one Go file.
