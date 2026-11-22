// Kartalar, the anime card collection's Mini App. The server keeps the
// catalog, the draws and who holds what; a card not held comes without its
// name or picture, so the page shows only its rarity until it is drawn.
"use strict";

(function () {
  const $ = (id) => document.getElementById(id);
  const tg = window.Telegram && window.Telegram.WebApp;

  const texts = {
    uz: {
      arenaTab: "Arena", towerTab: "🗼 Minora", arenaShop: "🪙 Arena do'koni", arenaTeam: "⚔️ Jamoangiz", arenaSave: "Jamoani saqlash", arenaFoes: "🎯 Raqiblar", arenaHistory: "📜 Janglar tarixi", arenaRefresh: "🔄 Yangilash", topCards: "🎴 Kartalar", topArena: "⚔️ Arena",
      title: "Kartalar", collected: "to'plandi", animeTab: "Animelar", animeSearch: "Anime yoki personaj", animeBack: "← Barcha animelar", animeOwned: "✓ Sizda bor", animeMissing: "Sizda hali yo'q", animeEmpty: "Hech narsa topilmadi", animeGoal: (n) => `🎟 Hammasini yig'ing: +${n} token`, animeClaim: (n) => `🎁 ${n} token olish`, animeClaimed: (n) => `✅ ${n} token olingan`, animeGot: (n) => `🎟 +${n} token! Kolleksiya to'liq`, collection: "Kolleksiya", draw: "Olish", top: "Reyting",
      search: "Kartani qidirish", rarities: ["Oddiy", "Noyob", "Epik", "Afsonaviy", "Mifik"],
      drawNow: "Karta olish", drawing: "Ochilmoqda…", next: (t) => `Keyingi karta: ${t}`, ready: "Karta tayyor!",
     
      cancel: "Bekor qilish",
      open1: "1x", open10: "5x", open50: "50x", costShort: "💎 yetmaydi", tokenHint: (d, c) => `🎟 Token bitta karta ochadi. Har 2 soatda +${d} token (12 tagacha yig'iladi), tugasa karta ${c} 💎`, nextToken: (time) => `⏳ Keyingi token: ${time}`, pity: (l, lm, m, mm) => `🛡 Kafolat: Lega ${l}/${lm} · Mifik ${m}/${mm}`, sureTag: "KAFOLAT", given: (n) => `🎟 +${n} token berildi`, short: (n) => `💎 Yana ${n} kristal kerak`, openedN: (n) => `${n} ta karta`, freshN: (n) => `🆕 ${n} ta yangi`, againN: (n) => `🔁 ${n} ta takror`, freshTag: "YANGI", carryOn: "Davom etish", noPoints: "ochko qo'shilmaydi", 
      fresh: "🆕 Yangi karta!", again: (n) => `🔁 Sizda endi ×${n}`, plus: (n) => `+${n} ochko`,
      attack: "Hujum", health: "Jon", value: "Ochko", cards: (n) => `${n} karta`, pts: "ochko",
      none: "Hech narsa topilmadi", noTop: "Hali hech kim karta olmagan", topPct: (p) => `Top ${p}%`, youRow: "Siz",
      noTelegram: "Ilovani Telegram'da oching: botga /card yozing.", stale: "Ilova eskirdi, uni qayta oching.",
      unavailable: "Aloqa uzildi, birozdan keyin urinib ko'ring.",
      allSets: "Hammasi", pointsLabel: "ochko", loading: "Kartalar yuklanmoqda…", more: "Yuklanmoqda…",
      welcomeTitle: "Kolleksiyangiz bo'sh", welcomeText: "Har 2 soatda bitta token oling, har biri bitta anime karta ochadi. Hammasini to'plang.",
      welcomeButton: "Birinchi kartani olish", seeCollection: "Kolleksiyada ko'rish →",
      locked: (r, o) => `🔒 Bu ${r.toLowerCase()} karta hali yopiq. Uning tushish ehtimoli: ${o}.`,
      wait: (time) => `⏳ ${time}`,
      market: "Bozor", newOffer: "➕ Taklif qo'yish", filterAll: "Hammasi", filterCan: "Bera olaman", filterMine: "Mening",
      gives: "Beradi", wants: "Xohlaydi", swap: "🔄 Almashish", noCard: "Sizda yo'q", starredCard: "⭐ Sizniki yulduzli", remove: "✕ Olib tashlash", yours: "⭐ Sizniki", marketEmpty: "Hozircha taklif yo'q",
      marketHint: () => `Kartangizni qo'ying va evaziga kerakli kartani tanlang. Kimda o'sha karta bo'lsa, almashadi. Taklif 7 kun turadi.`,
      step1: "1. Qaysi kartangizni berasiz?", step2: "2. Evaziga qaysi kartani xohlaysiz?", mineSearch: "Kartalaringizdan qidiring", wantSearch: "Xohlagan karta nomini yozing",
      place: "Taklif qo'yish", placed: "✅ Taklif bozorga qo'yildi", swapped: (c) => `🔄 Almashildi! Siz ${c} oldingiz`, removed: "Taklif olib tashlandi",
      refused: { uneven: "Faqat bir daraja yuqori yoki past kartaga almashish mumkin", limited: "🔒 Limited kartani almashib bo'lmaydi", over: "Bu taklif endi yo'q", gone: "Bu taklif endi yo'q", own: "Bu o'zingizning taklifingiz", not_held: "Sizda bu karta yo'q", same: "Bir xil kartani almashib bo'lmaydi", fused: "⭐ Yulduzli kartani almashib bo'lmaydi" },
      fuse: (a, b, n) => `⭐ ★${a} → ★${b} · ${n} nusxa`, fuseNext: (a, h) => `Keyin: Hujum ${a} · Jon ${h}`, fuseFree: (n) => `Bo'sh nusxa: ${n}`,
      fuseMax: "⭐ Eng yuqori daraja: ★5", fuseFew: (n) => `Yana ${n} ta nusxa kerak`, fused: (s) => `⭐ Karta ★${s} bo'ldi!`,
      fuseRefused: { max: "⭐ Karta allaqachon ★5", few: "Nusxalar yetmaydi", not_held: "Sizda bu karta yo'q" },
      offersLimit: (n) => `Bir vaqtda ${n} tagacha taklif qo'yish mumkin`,
      shopTab: "Do'kon", shopBuyTitle: "💎 Kristall sotib olish", shopBuyHint: "Telegram Stars bilan to'lanadi, kristall darhol hisobingizga tushadi.", shopItemsTitle: "🛒 Kristallga xarid",
      wallet: (n) => `Sizda: ${n} 💎`, crystalsN: (n) => `${n} 💎`, starsN: (n) => `${n} ⭐`, buyFor: (n) => `${n} 💎`, bonus: (p) => `+${p}% foyda`,
      tokensItem: (n) => `${n} ta token`, tokensHint: (n) => `${n} ta karta ochadi, kutmasdan`, packItem: (r) => `${r} paket`, packHint: (n, r) => `${n} ta karta, kamida 1 ta ${r.toLowerCase()} kafolatlangan`,
      boxTitle: "🎁 Almaz box", bagBoxes: "🎁 Qutilar", bagMail: "📬 Pochta", bagNoBoxes: "Hozircha quti yo'q", bagNoMail: "Xat yo'q", bagOpen: "Ochish", bagClaim: "Olish", bagGift: "Sovg'a", bagClaimed: (s) => `✅ Olindi: ${s}`, bagBoxN: (n) => `${n} ta Almaz box`, bagTokensN: (n) => `${n} token`, bagCrystalsN: (n) => `${n} kristall`, boxRange: (a, b) => `${a}–${b} 💎`, boxGot: "Almaz box ochildi!", boxLucky: "🔥 Omadingiz keldi!",
      paid: "✅ To'lov qabul qilindi, kristall qo'shilmoqda…", crystalsAdded: (n) => `💎 +${n} kristall qo'shildi!`, payFailed: "To'lov amalga oshmadi", bought: (n) => `🎟 +${n} token`, packOpened: (r) => `${r} paket`,
    },
    ru: {
      arenaTab: "Арена", towerTab: "🗼 Башня", arenaShop: "🪙 Магазин арены", arenaTeam: "⚔️ Ваша команда", arenaSave: "Сохранить команду", arenaFoes: "🎯 Соперники", arenaHistory: "📜 История боёв", arenaRefresh: "🔄 Обновить", topCards: "🎴 Карты", topArena: "⚔️ Арена",
      title: "Карты", collected: "собрано", animeTab: "Аниме", animeSearch: "Аниме или персонаж", animeBack: "← Все аниме", animeOwned: "✓ Есть у вас", animeMissing: "Пока нет у вас", animeEmpty: "Ничего не найдено", animeGoal: (n) => `🎟 Соберите всех: +${n} токенов`, animeClaim: (n) => `🎁 Забрать ${n} токенов`, animeClaimed: (n) => `✅ ${n} токенов получено`, animeGot: (n) => `🎟 +${n} токенов! Коллекция собрана`, collection: "Коллекция", draw: "Получить", top: "Рейтинг",
      search: "Поиск по картам", rarities: ["Обычная", "Редкая", "Эпическая", "Легендарная", "Мифическая"],
      drawNow: "Получить карту", drawing: "Открываем…", next: (t) => `Следующая карта: ${t}`, ready: "Карта готова!",
     
      cancel: "Отмена",
      open1: "1x", open10: "5x", open50: "50x", costShort: "💎 не хватает", tokenHint: (d, c) => `🎟 Токен открывает одну карту. Каждые 2 часа +${d} (копится до 12), без токенов карта стоит ${c} 💎`, nextToken: (time) => `⏳ Следующий токен: ${time}`, pity: (l, lm, m, mm) => `🛡 Гарант: Легенда ${l}/${lm} · Миф ${m}/${mm}`, sureTag: "ГАРАНТ", given: (n) => `🎟 +${n} токен(ов)`, short: (n) => `💎 Нужно ещё ${n} кристаллов`, openedN: (n) => `${n} карт`, freshN: (n) => `🆕 Новых: ${n}`, againN: (n) => `🔁 Повторов: ${n}`, freshTag: "НОВАЯ", carryOn: "Продолжить", noPoints: "очки не добавляются", 
      fresh: "🆕 Новая карта!", again: (n) => `🔁 Теперь у вас ×${n}`, plus: (n) => `+${n} очков`,
      attack: "Атака", health: "Жизнь", value: "Очки", cards: (n) => `${n} карт`, pts: "очков",
      none: "Ничего не найдено", noTop: "Ещё никто не получил карту", topPct: (p) => `Топ ${p}%`, youRow: "Вы",
      noTelegram: "Откройте приложение в Telegram: напишите боту /card.", stale: "Приложение устарело, откройте его снова.",
      unavailable: "Нет связи, попробуйте чуть позже.",
      allSets: "Все", pointsLabel: "очки", loading: "Загружаем карты…", more: "Загружаем…",
      welcomeTitle: "Коллекция пуста", welcomeText: "Каждые 2 часа получайте токен, он открывает аниме-карту. Соберите все.",
      welcomeButton: "Получить первую карту", seeCollection: "Посмотреть в коллекции →",
      locked: (r, o) => `🔒 Эта карта (${r.toLowerCase()}) ещё закрыта. Шанс выпадения: ${o}.`,
      wait: (time) => `⏳ ${time}`,
      market: "Рынок", newOffer: "➕ Выставить", filterAll: "Все", filterCan: "Могу дать", filterMine: "Мои",
      gives: "Отдаёт", wants: "Хочет", swap: "🔄 Обменять", noCard: "Нет у вас", starredCard: "⭐ Ваша звёздная", remove: "✕ Снять", yours: "⭐ Ваше", marketEmpty: "Пока предложений нет",
      marketHint: () => `Выставьте свою карту и выберите, какую хотите взамен. У кого она есть — обменяется. Предложение висит 7 дней.`,
      step1: "1. Какую карту отдаёте?", step2: "2. Какую хотите взамен?", mineSearch: "Поиск по вашим картам", wantSearch: "Название нужной карты",
      place: "Выставить", placed: "✅ Предложение выставлено", swapped: (c) => `🔄 Обмен! Вы получили ${c}`, removed: "Предложение снято",
      refused: { uneven: "Менять можно на карту той же редкости или на одну выше/ниже", limited: "🔒 Лимитированную карту нельзя обменять", over: "Этого предложения уже нет", gone: "Этого предложения уже нет", own: "Это ваше предложение", not_held: "У вас нет этой карты", same: "Нельзя менять карту на ту же", fused: "⭐ Звёздную карту нельзя обменять" },
      fuse: (a, b, n) => `⭐ ★${a} → ★${b} · ${n} коп.`, fuseNext: (a, h) => `Станет: Атака ${a} · Жизнь ${h}`, fuseFree: (n) => `Свободных копий: ${n}`,
      fuseMax: "⭐ Высший уровень: ★5", fuseFew: (n) => `Нужно ещё копий: ${n}`, fused: (s) => `⭐ Карта стала ★${s}!`,
      fuseRefused: { max: "⭐ Карта уже ★5", few: "Не хватает копий", not_held: "У вас нет этой карты" },
      offersLimit: (n) => `Одновременно можно выставить до ${n} предложений`,
      shopTab: "Магазин", shopBuyTitle: "💎 Купить кристаллы", shopBuyHint: "Оплата Telegram Stars, кристаллы приходят сразу.", shopItemsTitle: "🛒 За кристаллы",
      wallet: (n) => `У вас: ${n} 💎`, crystalsN: (n) => `${n} 💎`, starsN: (n) => `${n} ⭐`, buyFor: (n) => `${n} 💎`, bonus: (p) => `+${p}% выгоды`,
      tokensItem: (n) => `${n} токенов`, tokensHint: (n) => `Откроют ${n} карт без ожидания`, packItem: (r) => `Пак: ${r}`, packHint: (n, r) => `${n} карт, минимум одна ${r.toLowerCase()} гарантирована`,
      boxTitle: "🎁 Алмазный бокс", bagBoxes: "🎁 Боксы", bagMail: "📬 Почта", bagNoBoxes: "Пока нет боксов", bagNoMail: "Писем нет", bagOpen: "Открыть", bagClaim: "Забрать", bagGift: "Подарок", bagClaimed: (s) => `✅ Получено: ${s}`, bagBoxN: (n) => `Алмазный бокс ×${n}`, bagTokensN: (n) => `${n} токенов`, bagCrystalsN: (n) => `${n} кристаллов`, boxRange: (a, b) => `${a}–${b} 💎`, boxGot: "Алмазный бокс открыт!", boxLucky: "🔥 Вам повезло!",
      paid: "✅ Оплата принята, кристаллы зачисляются…", crystalsAdded: (n) => `💎 +${n} кристаллов!`, payFailed: "Оплата не прошла", bought: (n) => `🎟 +${n} токенов`, packOpened: (r) => `Пак: ${r}`,
    },
    en: {
      arenaTab: "Arena", towerTab: "🗼 Tower", arenaShop: "🪙 Arena shop", arenaTeam: "⚔️ Your team", arenaSave: "Save the team", arenaFoes: "🎯 Opponents", arenaHistory: "📜 Battle history", arenaRefresh: "🔄 Refresh", topCards: "🎴 Cards", topArena: "⚔️ Arena",
      title: "Cards", collected: "collected", animeTab: "Anime", animeSearch: "Anime or character", animeBack: "← All anime", animeOwned: "✓ You have it", animeMissing: "Not yours yet", animeEmpty: "Nothing found", animeGoal: (n) => `🎟 Collect them all: +${n} tokens`, animeClaim: (n) => `🎁 Claim ${n} tokens`, animeClaimed: (n) => `✅ ${n} tokens claimed`, animeGot: (n) => `🎟 +${n} tokens! Collection complete`, collection: "Collection", draw: "Draw", top: "Ranking",
      search: "Search cards", rarities: ["Common", "Rare", "Epic", "Legendary", "Mythic"],
      drawNow: "Draw a card", drawing: "Opening…", next: (t) => `Next card in ${t}`, ready: "A card is ready!",
     
      cancel: "Cancel",
      open1: "1x", open10: "5x", open50: "50x", costShort: "💎 not enough", tokenHint: (d, c) => `🎟 A token opens one card. +${d} every 2 hours, up to 12 kept; with none, a card is ${c} 💎`, nextToken: (time) => `⏳ Next token: ${time}`, pity: (l, lm, m, mm) => `🛡 Guarantee: Legendary ${l}/${lm} · Mythic ${m}/${mm}`, sureTag: "GUARANTEED", given: (n) => `🎟 +${n} tokens`, short: (n) => `💎 ${n} more crystals needed`, openedN: (n) => `${n} cards`, freshN: (n) => `🆕 ${n} new`, againN: (n) => `🔁 ${n} again`, freshTag: "NEW", carryOn: "Continue", noPoints: "no points added", 
      fresh: "🆕 A new card!", again: (n) => `🔁 You now have ×${n}`, plus: (n) => `+${n} points`,
      attack: "Attack", health: "Life", value: "Points", cards: (n) => `${n} cards`, pts: "points",
      none: "Nothing found", noTop: "Nobody has drawn a card yet", topPct: (p) => `Top ${p}%`, youRow: "You",
      noTelegram: "Open the app in Telegram: send /card to the bot.", stale: "The app is out of date; open it again.",
      unavailable: "No connection; try again in a bit.",
      allSets: "All", pointsLabel: "points", loading: "Loading the cards…", more: "Loading…",
      welcomeTitle: "Your collection is empty", welcomeText: "Get a token every 2 hours, each opens an anime card. Collect them all.",
      welcomeButton: "Draw your first card", seeCollection: "See it in the collection →",
      locked: (r, o) => `🔒 This ${r.toLowerCase()} card is still locked. Its odds: ${o}.`,
      wait: (time) => `⏳ ${time}`,
      market: "Market", newOffer: "➕ Make an offer", filterAll: "All", filterCan: "I can give", filterMine: "Mine",
      gives: "Gives", wants: "Wants", swap: "🔄 Trade", noCard: "You lack it", starredCard: "⭐ Yours is starred", remove: "✕ Take down", yours: "⭐ Yours", marketEmpty: "No offers yet",
      marketHint: () => `Put up a card and pick the one you want for it. Whoever holds it can trade. An offer stays for 7 days.`,
      step1: "1. Which card do you give?", step2: "2. Which card do you want?", mineSearch: "Search your cards", wantSearch: "Name of the card you want",
      place: "Put it up", placed: "✅ Your offer is up", swapped: (c) => `🔄 Traded! You got ${c}`, removed: "Offer taken down",
      refused: { uneven: "A card may be traded only for one of the same rarity or one step up or down", limited: "🔒 A limited card cannot be traded", over: "This offer is gone", gone: "This offer is gone", own: "This is your own offer", not_held: "You do not have that card", same: "A card cannot be traded for itself", fused: "⭐ A starred card cannot be traded" },
      fuse: (a, b, n) => `⭐ ★${a} → ★${b} · ${n} ${n === 1 ? "copy" : "copies"}`, fuseNext: (a, h) => `Then: Attack ${a} · Life ${h}`, fuseFree: (n) => `Free copies: ${n}`,
      fuseMax: "⭐ Top level: ★5", fuseFew: (n) => `${n} more ${n === 1 ? "copy" : "copies"} needed`, fused: (s) => `⭐ The card is now ★${s}!`,
      fuseRefused: { max: "⭐ The card is already ★5", few: "Not enough copies", not_held: "You do not have that card" },
      offersLimit: (n) => `You may have up to ${n} offers at once`,
      shopTab: "Shop", shopBuyTitle: "💎 Buy crystals", shopBuyHint: "Paid in Telegram Stars; the crystals arrive at once.", shopItemsTitle: "🛒 For crystals",
      wallet: (n) => `You have: ${n} 💎`, crystalsN: (n) => `${n} 💎`, starsN: (n) => `${n} ⭐`, buyFor: (n) => `${n} 💎`, bonus: (p) => `+${p}% value`,
      tokensItem: (n) => `${n} tokens`, tokensHint: (n) => `Open ${n} cards without the wait`, packItem: (r) => `${r} pack`, packHint: (n, r) => `${n} cards, at least one ${r.toLowerCase()} guaranteed`,
      boxTitle: "🎁 Diamond box", bagBoxes: "🎁 Boxes", bagMail: "📬 Mail", bagNoBoxes: "No boxes yet", bagNoMail: "No letters", bagOpen: "Open", bagClaim: "Claim", bagGift: "Gift", bagClaimed: (s) => `✅ Claimed: ${s}`, bagBoxN: (n) => `${n} diamond box${n === 1 ? "" : "es"}`, bagTokensN: (n) => `${n} tokens`, bagCrystalsN: (n) => `${n} crystals`, boxRange: (a, b) => `${a}–${b} 💎`, boxGot: "Diamond box opened!", boxLucky: "🔥 Lucky you!",
      paid: "✅ Payment received, adding your crystals…", crystalsAdded: (n) => `💎 +${n} crystals!`, payFailed: "The payment did not go through", bought: (n) => `🎟 +${n} tokens`, packOpened: (r) => `${r} pack`,
    },
  };
  const language = ((tg && tg.initDataUnsafe && tg.initDataUnsafe.user && tg.initDataUnsafe.user.language_code) || "uz").slice(0, 2);
  const t = texts[language] || texts.uz;
  document.documentElement.lang = texts[language] ? language : "uz";
  for (const node of document.querySelectorAll("[data-t]")) node.textContent = t[node.dataset.t];
  $("search").placeholder = t.search;
  $("animeSearch").placeholder = t.animeSearch;

  // The collections, in the order the page lists them.
  const SETS = [["anime", "🎌 Anime"]];
  const COLORS = ["var(--r0)", "var(--r1)", "var(--r2)", "var(--r3)", "var(--r4)"];
  const ODDS = ["63%", "25%", "9%", "2.5%", "0.5%"];
  const spaced = (n) => String(Math.round(n)).replace(/\B(?=(\d{3})+(?!\d))/g, " ");
  const haptic = (kind) => { try { tg.HapticFeedback.impactOccurred(kind); } catch (_) { /* outside Telegram */ } };

  let cards = [], points = 0, perDay = 2, drawing = false, query = "", opened = "", chosenSet = "";
  let shards = 0, cardCost = 15, batch = 5, bigBatch = 50, tokens = 0, nextTokenAt = 0, refreshing = false;

  // ---------- server ----------

  async function api(action, extra) {
    const response = await fetch("/api/cards", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ init_data: tg.initData, action, ...extra }),
    });
    const data = await response.json().catch(() => ({ error: "unavailable" }));
    if (!response.ok || data.error) throw new Error(data.error || "unavailable");
    return data;
  }

  function notice(error) {
    const box = $("notice");
    box.textContent = error ? (error.message === "stale" ? t.stale : t.unavailable) : "";
    box.hidden = !error;
  }

  async function load() {
    try {
      applyState(await api("state"));
      $("loading").hidden = true;
      eventBlock();
      // A link may name the tab to open, as the market's old button does; otherwise a
      // newcomer starts at the draw and anyone else at their collection.
      const asked = new URLSearchParams(location.search).get("tab");
      if (!opened) showTab(["collection", "draw", "market", "shop", "arena", "top"].includes(asked) ? asked : cards.some((c) => c.count > 0) ? "collection" : "draw");
    } catch (error) {
      $("loading").hidden = true;
      if (!opened) showTab("collection");
      notice(error);
    }
  }

  // applyState shows what the server says the player holds.
  function applyState(data) {
    cards = data.cards;
    points = data.points;
    perDay = data.per_day || perDay;
    shards = data.shards || 0;
    cardCost = data.card_cost || cardCost;
    batch = data.batch || batch;
    bigBatch = data.big_batch || bigBatch;
    nextTokenAt = Date.now() + (data.next_token_in || 0);
    tokens = data.tokens || 0;
    pityLine(data.pity);
    if (data.given) toast(t.given(data.given));
    if (data.quests) questShow(data.quests);
    if (data.inbox) bagBadge(data.inbox);
    shardsPanel();
    notice(null);
    header();
    shelves();
    drawButton();
  }

  // picture loads a card's picture into img, asking again twice, a little
  // later each time, when the image host drops it: with many cards at once
  // it now and then does, and the card showed an empty frame.
  // A picture loads only when it comes near the screen, unless eager, as
  // the card just drawn is, which is waited for.
  function picture(img, src, eager) {
    img.loading = eager ? "eager" : "lazy";
    img.decoding = "async";
    // A failed picture is asked for again, the last time under a name no
    // cache has seen: a phone that once got an error for a new picture
    // (it was asked for before the picture was out) keeps that error for the
    // pictures' month-long cache, under its plain name and the first retry's.
    let tries = 0;
    img.onerror = () => {
      if (tries >= 2 || !src) return;
      tries++;
      const mark = tries === 1 ? "1" : Date.now().toString(36);
      setTimeout(() => { img.src = src + (src.includes("?") ? "&" : "?") + "r=" + mark; }, 800 * tries);
    };
    img.src = src;
  }

  // ---------- header and collection ----------

  function header() {
    $("points").textContent = spaced(points);
    const held = cards.filter((c) => c.count > 0).length;
    $("held").textContent = held;
    $("total").textContent = cards.length;
    $("progress").style.width = `${cards.length ? (held / cards.length) * 100 : 0}%`;
    $("welcome").hidden = held > 0;
    setChips();
  }

  // Each rarity's icon, drawn in its colour, in place of an emoji, which
  // every phone draws its own way.
  const TIER_ICONS = [
    '<circle cx="12" cy="12" r="7" fill="none" stroke="currentColor" stroke-width="2.6"/><circle cx="12" cy="12" r="3"/>',
    '<path d="M12 2l8 10-8 10-8-10z"/><path d="M12 2v20M4 12h16" fill="none" stroke="rgba(0,0,0,.28)" stroke-width="1.2"/>',
    '<path d="M7 3h10l4 6-9 12L3 9z"/><path d="M3 9h18M9 3l3 6 3-6M12 9v12" fill="none" stroke="rgba(0,0,0,.3)" stroke-width="1.2"/>',
    '<path d="M12 2c1 4 6 6 6 12a6 6 0 0 1-12 0c0-3 2-5 3-6 0 2 1 3 2 3 0-4-1-6 1-9z"/><path d="M12 13c.5 2 3 3 3 5.5a3 3 0 0 1-6 0c0-1.5 1-2.5 1.5-3 .2 1 .8 1.5 1.5 1.5 0-2-.5-3 0-4z" fill="rgba(255,240,200,.75)"/>',
    '<path d="M3 8l4.5 4L12 4l4.5 8L21 8l-2 10H5z"/><rect x="5" y="19.5" width="14" height="2.2" rx="1.1"/><circle cx="12" cy="13.5" r="1.6" fill="rgba(0,0,0,.3)"/>',
  ];
  function tierIcon(rarity) {
    const icon = document.createElement("span");
    icon.className = "ti";
    icon.style.setProperty("--c", COLORS[rarity]);
    icon.setAttribute("aria-hidden", "true");
    icon.innerHTML = `<svg viewBox="0 0 24 24" fill="currentColor">${TIER_ICONS[rarity]}</svg>`;
    return icon;
  }
  // withTiers is text with each {n} in it drawn as rarity n's icon.
  function withTiers(text) {
    const parts = text.split(/\{(\d)\}/);
    return parts.map((part, i) => (i % 2 ? tierIcon(Number(part)) : document.createTextNode(part)));
  }

  // A Mythic card's dress over its picture: the glint, the gold frame, the
  // crown and two sparkles; the large view adds two more and rising motes.
  const MYTHIC_DRESS = '<i class="glint"></i><i class="frame"></i><span class="crest" aria-hidden="true"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M3 8l4.5 4L12 4l4.5 8L21 8l-2 10H5z"/><rect x="5" y="19.5" width="14" height="2.2" rx="1.1"/></svg></span><em class="spark"></em><em class="spark"></em>';

  // cutOf is a Mythic card's cut-out, named for its picture too: art is
  // cached for a month, so a new picture under a new name brings its new
  // cut-out with it.
  // LARGE_ART is a picture the owners chose for a card's large view alone,
  // the small tile keeping its own; its cut-out is art/cut/<id>L.webp.
  const LARGE_ART = {};

  // largeImage is the picture of a card's large view.
  function largeImage(card) {
    return LARGE_ART[card.id] || card.image;
  }

  // PLATED are the Mythics that have a plate: their picture with the
  // character taken out, laid under the breathing cut-out so that the still
  // one behind it does not show; named for the picture like the cut-outs.
  const PLATED = new Set([1, 5, 11, 17, 27, 28, 30, 40, 80, 246, 417, 422, 4003, 34470, 40881, 40882, 45627, 72921, 73935, 88572, 90169, 90202, 124381, 126071, 126824, 127222, 127518, 127691, 128130, 129928, 137080, 176754, 249281, 265209, 298761, 341936,
    130050, 129131, 141918, 149220, 129132, 141694, 123285, 123284, 124435, 124522, 124440, 123283]);

  // underImage is the picture a Mythic's cut-out lies on: its plate when it
  // has one for that picture, else the picture itself.
  function underImage(card, large) {
    const own = large ? largeImage(card) : card.image;
    if (card.rarity !== 4 || !PLATED.has(card.id) || own !== card.image) return own;
    const name = String(card.image || "").split("/").pop();
    return `/kartalar/art/plate/${card.id}.jpg${name ? `?p=${encodeURIComponent(name)}` : ""}`;
  }

  // cutOf is the cut-out of a card's picture, of its large view's with large.
  function cutOf(card, large) {
    const big = Boolean(large && LARGE_ART[card.id]);
    const name = String(big ? LARGE_ART[card.id] : card.image || "").split("/").pop();
    return `/kartalar/art/cut/${card.id}${big ? "L" : ""}.webp${name ? `?p=${encodeURIComponent(name)}` : ""}`;
  }

  // alive is a Mythic card's character, cut from the picture, laid over it
  // and moving: it breathes, and in the large view its hair stirs.
  function alive(card, large) {
    const box = document.createElement("div");
    box.className = "alive";
    box.setAttribute("aria-hidden", "true");
    const cut = `url("${cutOf(card, large)}")`;
    for (const part of ["body", "hair"]) {
      const layer = document.createElement("i");
      layer.className = part;
      layer.style.backgroundImage = cut;
      box.append(layer);
    }
    return box;
  }

  // SPECIAL are the cards the owners dressed by hand: a gold "LIMITED" mark
  // with a number under it (a mark, not a count of copies), cherry petals
  // drifting over the picture, fireflies glowing among them and, in the large
  // view, long hair flowing in the wind while the face and body keep still.
  // fx is a card's own effect (.fx in the styles), drawn mostly behind the
  // character, between its plate and its cut-out, with a halo round it;
  // glow is its two colours, the core and the glow round it. sword is a
  // weapon cut out on its own (art/cut/<id>s.webp, taken off the plate too),
  // which swings from the hand and burns.
  const SPECIAL = {
    298761: { tag: "LIMITED", petals: true, fireflies: true, hair: true },
    27: { fx: "bolts", glow: ["#9feaff", "#2a9dff"] }, // Killua: lightning
    129131: { fx: "bolts", glow: ["#fff1a0", "#ffae00"] }, // Zenitsu: thunder
    130050: { fx: "water", glow: ["#c6f1ff", "#1e8fff"] }, // Giyu: water
    141918: { fx: "moon", glow: ["#f1dcff", "#9a4dff"] }, // Kokushibo: the moon
    149220: { fx: "sun", glow: ["#ffe6a8", "#ff5a1f"] }, // Yoriichi: the sun
    129132: { fx: "blood", glow: ["#ff8a8a", "#b0002a"] }, // Muzan: blood
    127518: { fx: "blood", glow: ["#ffc2dd", "#ff3d8b"] }, // Nezuko: her exploding blood, pink
    141694: { fx: "compass", glow: ["#a8f6ff", "#ff2e7e"] }, // Akaza: his compass
    123285: { fx: "antimagic", glow: ["#ff4a3d", "#8a0000"], sword: true }, // Asta: anti-magic, his sword swinging
    123284: { fx: "wind", glow: ["#e2ffe9", "#2ecc71"] }, // Yuno: wind
    124435: { fx: "clock", glow: ["#d8ecff", "#3d7bff"] }, // Julius: time
    124522: { fx: "fire", glow: ["#ffd36b", "#ff4d00"] }, // Mereoleona: fire
    124440: { fx: "dark", glow: ["#e4c7ff", "#6a1bb0"] }, // Yami: darkness, its dimension slash
    123283: { fx: "valkyrie", glow: ["#d8fbff", "#1fa8ff"] }, // Noelle: the sea, her Valkyrie dress
  };

  // FX_PARTS is how many pieces each effect draws behind the character and
  // in front of it, in the grid and in the large view.
  const FX_PARTS = {
    bolts: { behind: [4, 7], ahead: [1, 2] },
    water: { behind: [2, 3], ahead: [4, 6] },
    moon: { behind: [3, 5], ahead: [3, 5] },
    sun: { behind: [2, 2], ahead: [4, 6] },
    blood: { behind: [2, 3], ahead: [3, 5] },
    compass: { behind: [2, 2], ahead: [4, 6] },
    antimagic: { behind: [4, 7], ahead: [4, 6] },
    wind: { behind: [4, 6], ahead: [2, 3] },
    clock: { behind: [4, 4], ahead: [3, 5] },
    fire: { behind: [4, 6], ahead: [4, 6] },
    dark: { behind: [4, 5], ahead: [4, 6] },
    valkyrie: { behind: [5, 7], ahead: [5, 6] },
  };

  // dress adds a special card's mark, petals, fireflies or own effect; the
  // large view has more of them.
  function dress(el, card, large) {
    const special = SPECIAL[card.id] || (card.limited ? { tag: "LIMITED" } : null);
    if (!special) return;
    el.classList.add("special");
    let html = "";
    if (special.petals) html += '<div class="petals" aria-hidden="true">' + "<i></i>".repeat(large ? 14 : 6) + "</div>";
    if (special.fireflies) html += '<div class="fireflies" aria-hidden="true">' + "<i></i>".repeat(large ? 9 : 4) + "</div>";
    el.insertAdjacentHTML("beforeend", html);
    const living = el.querySelector(".alive");
    if (special.fx && living) {
      const parts = FX_PARTS[special.fx];
      const pieces = (n) => "<i></i>".repeat(n[large ? 1 : 0]);
      el.classList.add("fx-" + special.fx);
      el.style.setProperty("--fx1", special.glow[0]);
      el.style.setProperty("--fx2", special.glow[1]);
      // most of it behind the character, between the plate and the cut-out
      living.insertAdjacentHTML("beforebegin", `<div class="fx behind" aria-hidden="true">${pieces(parts.behind)}</div>`);
      el.insertAdjacentHTML("beforeend", `<div class="fx ahead" aria-hidden="true">${pieces(parts.ahead)}</div>`);
      // the halo: the cut-out once more under the character, glowing
      const aura = document.createElement("i");
      aura.className = "aura";
      aura.style.backgroundImage = living.querySelector("i").style.backgroundImage;
      living.prepend(aura);
    }
    if (special.sword && living) {
      const sword = document.createElement("i");
      sword.className = "sword";
      sword.style.backgroundImage = `url("/kartalar/art/cut/${card.id}s.webp")`;
      living.append(sword);
    }
    if (special.hair && large && living) {
      // flowing hair: the cut-out once more, its edges alone, waved by #flow
      const flow = document.createElement("i");
      flow.className = "flow";
      flow.style.backgroundImage = `url("${cutOf(card, true)}")`;
      living.classList.add("flowing");
      living.append(flow);
    }
    if (special.tag) {
      el.classList.add("marked");
      const mark = document.createElement("div");
      mark.className = "lmark";
      const tag = document.createElement("span");
      tag.className = "limited";
      tag.textContent = special.tag;
      mark.append(tag);
      // the holder's own copy number, which holder of it they are
      if (card.serial > 0) {
        const serial = document.createElement("span");
        serial.className = "serial";
        serial.textContent = serialMark(card.serial);
        mark.append(serial);
      }
      el.append(mark);
    }
  }

  function cardTile(card) {
    const tile = document.createElement("button");
    tile.type = "button";
    tile.className = "card";
    tile.style.setProperty("--c", COLORS[card.rarity]);
    if (!card.count) {
      tile.classList.add("locked");
      tile.innerHTML = `<span class="glyph">?</span>`;
      tile.setAttribute("aria-label", t.rarities[card.rarity]);
      tile.addEventListener("click", () => toast(t.locked(t.rarities[card.rarity], ODDS[card.rarity])));
      return tile;
    }
    if (card.rarity >= 3) tile.classList.add("foil");
    if (card.rarity === 4) tile.classList.add("mythic");
    const img = new Image();
    img.loading = "lazy";
    img.alt = "";
    picture(img, underImage(card));
    const name = document.createElement("span");
    name.className = "name";
    name.textContent = card.name;
    tile.append(img, name);
    if (card.rarity === 4) {
      img.after(alive(card));
      tile.insertAdjacentHTML("beforeend", MYTHIC_DRESS);
    }
    dress(tile, card, false);
    if (card.stars > 1) {
      const stars = document.createElement("span");
      stars.className = "stars";
      stars.textContent = `★${card.stars}`;
      tile.append(stars);
    }
    // The copies held: a ★1 card counts itself, a starred one only its free
    // copies; those used up for stars are gone.
    if (card.free > 0) {
      const count = document.createElement("span");
      count.className = "count";
      count.textContent = `×${copies(card)}`;
      tile.append(count);
    }
    tile.addEventListener("click", () => open(card));
    return tile;
  }

  // copies is how many copies of a card the player has: a ★1 card itself
  // and its free ones, a starred card its free ones only.
  function copies(card) {
    return (card.free || 0) + (card.stars > 1 ? 0 : 1);
  }

  function setChips() {
    const root = $("sets");
    root.textContent = "";
    const chip = (name, label, list) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = name === chosenSet ? "on" : "";
      button.setAttribute("aria-pressed", name === chosenSet);
      button.textContent = label;
      const count = document.createElement("small");
      count.textContent = `${list.filter((c) => c.count > 0).length}/${list.length}`;
      button.append(count);
      button.addEventListener("click", () => {
        chosenSet = name;
        setChips();
        shelves();
        haptic("light");
      });
      root.append(button);
    };
    const known = SETS.filter(([name]) => cards.some((c) => c.set === name));
    root.hidden = known.length < 2;
    chip("", t.allSets, cards);
    for (const [name, label] of known) chip(name, label, cards.filter((c) => c.set === name));
  }

  function shelves() {
    const root = $("shelves");
    root.textContent = "";
    const q = query.trim().toLowerCase();
    let shown = 0;
    for (let rarity = 4; rarity >= 0; rarity--) {
      const all = cards.filter((c) => c.rarity === rarity && (!chosenSet || c.set === chosenSet));
      const held = all.filter((c) => c.count > 0);
      // A search looks through the cards held; their names are all the page knows.
      // Not held, a shelf shows a row of locked cards at most, the last one
      // counting the rest, so that the cards held are not lost below them.
      const locked = all.length - held.length;
      const teaser = Math.min(locked, held.length % 3 ? 3 - (held.length % 3) : 3);
      const list = q ? held.filter((c) => (c.name + " " + c.anime).toLowerCase().includes(q)) : [...held, ...all.filter((c) => !c.count).slice(0, teaser)];
      if (!list.length) continue;
      shown += list.length;
      const shelf = document.createElement("section");
      shelf.className = "shelf";
      shelf.style.setProperty("--c", COLORS[rarity]);
      shelf.innerHTML = `<div class="shelf-head"><h2></h2><span>${held.length} <i>/ ${all.length}</i></span></div><div class="shelf-bar"><i style="width:${(held.length / all.length) * 100}%"></i></div><div class="grid"></div>`;
      shelf.querySelector("h2").replaceChildren(tierIcon(rarity), t.rarities[rarity]);
      const grid = shelf.querySelector(".grid");
      for (const card of list) grid.append(cardTile(card));
      const rest = q ? 0 : locked - teaser;
      if (rest > 0) {
        const last = grid.lastElementChild;
        last.querySelector(".glyph").textContent = `+${rest + 1}`;
        last.classList.add("more");
      }
      root.append(shelf);
    }
    if (!shown && cards.length) root.innerHTML = `<p class="empty">${t.none}</p>`;
  }

  $("search").addEventListener("input", (event) => {
    query = event.target.value;
    shelves();
  });

  // ---------- one card, large ----------

  function face(card) {
    const front = document.createElement("div");
    front.className = "face front";
    front.style.setProperty("--c", COLORS[card.rarity]);
    const img = new Image();
    img.alt = card.name;
    picture(img, underImage(card, true), true);
    const plate = document.createElement("div");
    plate.className = "plate";
    const rarity = document.createElement("span");
    rarity.className = "rarity";
    rarity.replaceChildren(tierIcon(card.rarity), t.rarities[card.rarity]);
    const name = document.createElement("b");
    name.textContent = card.name;
    const anime = document.createElement("small");
    anime.textContent = card.anime;
    plate.append(rarity, name, anime);
    if (card.stars) {
      const stars = document.createElement("span");
      stars.className = "plate-stars";
      stars.textContent = "★".repeat(card.stars) + "☆".repeat(5 - card.stars);
      plate.append(stars);
    }
    front.append(img);
    front.append(plate);
    if (card.rarity === 4) {
      front.classList.add("mythic");
      img.after(alive(card, true));
      front.insertAdjacentHTML("beforeend", MYTHIC_DRESS + '<em class="spark"></em><em class="spark"></em><div class="motes">' + "<i></i>".repeat(6) + "</div>");
    }
    dress(front, card, true);
    return front;
  }

  function statsOf(card) {
    const stats = document.createElement("div");
    stats.className = "stats";
    for (const [label, value] of [[t.attack, card.attack], [t.health, card.health], [t.value, card.value]]) {
      const cell = document.createElement("div");
      cell.innerHTML = `<small></small><b></b>`;
      cell.querySelector("small").textContent = label;
      cell.querySelector("b").textContent = spaced(value);
      stats.append(cell);
    }
    return stats;
  }

  // STAR_MULT is how much stronger a card of so many stars is, as
  // cards.StarMultiplier counts it.
  const STAR_MULT = [0, 1, 1.2, 1.45, 1.7, 2];
  const BEATS = { tank: "fighter", fighter: "mage", mage: "tank" };
  const WEAK_TO = { tank: "mage", fighter: "tank", mage: "fighter" };

  // cardBack is the large card's back: the card as the arena sees it, its
  // class, numbers, skill and anime, in the words of the arena's tab.
  function cardBack(card) {
    const f = fighterOf(card);
    const stars = Math.min(Math.max(card.stars || 1, 1), 5);
    const back = document.createElement("div");
    back.className = "face info-side";
    back.style.setProperty("--c", COLORS[card.rarity]);
    back.innerHTML = '<div class="ib-head"><span class="rarity"></span><b></b></div>' +
      '<div class="ib-stats"><div><small></small><b></b></div><div><small></small><b></b></div><div><small></small><b></b></div></div>' +
      '<small class="ib-stars"></small>' +
      '<div class="ib-row ib-class"><span class="ib-icon" aria-hidden="true"></span><div><b></b><p></p><p class="ib-tri"><span class="up"></span><span class="down"></span></p></div></div>' +
      '<div class="ib-row ib-skill"><span class="ib-icon" aria-hidden="true"></span><div><b></b><p></p></div></div>' +
      '<div class="ib-row ib-anime"><span class="ib-icon" aria-hidden="true">🔗</span><div><b></b><p></p></div></div>' +
      '<small class="ib-hint"></small>';
    back.querySelector(".ib-head .rarity").replaceChildren(tierIcon(card.rarity), t.rarities[card.rarity]);
    back.querySelector(".ib-head b").textContent = card.name || "";
    const known = card.attack > 0;
    const cells = back.querySelectorAll(".ib-stats div");
    for (const [i, label, value] of [[0, `⚔ ${t.attack}`, card.attack], [1, `❤ ${t.health}`, card.health], [2, `⚡ ${at.statPower}`, powerOf([f])]]) {
      cells[i].querySelector("small").textContent = label;
      cells[i].querySelector("b").textContent = known ? spaced(value) : "—";
    }
    back.querySelector(".ib-stars").textContent = at.starsLine(stars, STAR_MULT[stars].toFixed(2), STAR_MULT[5].toFixed(2));
    const named = (c) => `${CLASS_ICON[c]} ${at.classes[c]}`;
    const cls = back.querySelector(".ib-class");
    cls.querySelector(".ib-icon").textContent = CLASS_ICON[f.class];
    cls.querySelector("b").textContent = at.classes[f.class];
    cls.querySelector("p").textContent = at.classInfo[f.class];
    cls.querySelector(".up").textContent = "▲ " + at.beats(named(BEATS[f.class]));
    cls.querySelector(".down").textContent = "▼ " + at.weakTo(named(WEAK_TO[f.class]));
    const skill = back.querySelector(".ib-skill");
    if (f.skill) {
      skill.querySelector(".ib-icon").textContent = SKILL_ICON[f.skill];
      skill.querySelector("b").textContent = at.skillTitle(at.skills[f.skill]);
      skill.querySelector("p").textContent = at.skillInfo[f.skill];
    } else {
      skill.classList.add("none");
      skill.querySelector(".ib-icon").textContent = "✧";
      skill.querySelector("b").textContent = at.noSkillTitle;
      skill.querySelector("p").textContent = at.noSkill;
    }
    const anime = back.querySelector(".ib-anime");
    anime.querySelector("b").textContent = franchise(card.anime || "") || "—";
    anime.querySelector("p").textContent = at.animeNote;
    back.querySelector(".ib-hint").textContent = "↻ " + at.flipHint;
    return back;
  }

  // turnable is the large card, front and back; a tap on it turns it over
  // (Enter or Space from the keyboard). With less motion asked for the sides
  // only swap.
  function turnable(card) {
    const turn = document.createElement("div");
    turn.className = "turn";
    turn.setAttribute("role", "button");
    turn.tabIndex = 0;
    turn.setAttribute("aria-label", at.flipHint);
    const front = face(card), back = cardBack(card);
    turn.append(front, back);
    const show = (turned) => {
      turn.classList.toggle("turned", turned);
      front.setAttribute("aria-hidden", String(turned));
      back.setAttribute("aria-hidden", String(!turned));
      turn.setAttribute("aria-pressed", String(turned));
    };
    const flip = (event) => {
      event.stopPropagation();
      show(!turn.classList.contains("turned"));
      haptic("light");
    };
    turn.addEventListener("click", flip);
    turn.addEventListener("keydown", (event) => {
      if (event.key !== "Enter" && event.key !== " ") return;
      event.preventDefault();
      flip(event);
    });
    show(false);
    return [turn];
  }

  function open(card) {
    const sheet = $("sheetCard");
    sheet.textContent = "";
    sheet.classList.remove("giving");
    sheet.append(...turnable(card), statsOf(card));
    if (card.stars) sheet.append(fusePanel(card));
    tilt(sheet, card.rarity === 4);
    $("sheet").hidden = false;
    try { tg.BackButton.show(); } catch (_) { /* older clients */ }
    haptic("light");
  }
  // fusePanel is the large card's ⭐ button: the next star, the free copies
  // it uses up and the card's numbers after; at ★5, or with too few copies,
  // it is disabled and says why. A fusion re-renders from the state given.
  function fusePanel(card) {
    const box = document.createElement("div");
    box.className = "fuse";
    box.addEventListener("click", (event) => event.stopPropagation());
    const button = document.createElement("button");
    button.type = "button";
    button.className = "draw fuse-do";
    const hint = document.createElement("small");
    if (!card.fuse) {
      button.textContent = t.fuseMax;
      button.disabled = true;
      hint.textContent = t.fuseFree(card.free || 0);
    } else {
      button.textContent = t.fuse(card.stars, card.stars + 1, card.fuse);
      button.disabled = (card.free || 0) < card.fuse;
      hint.textContent = (button.disabled ? t.fuseFew(card.fuse - (card.free || 0)) + " · " : "") + t.fuseNext(spaced(card.next_attack), spaced(card.next_health));
    }
    button.addEventListener("click", async () => {
      button.disabled = true;
      try {
        const data = await api("fuse", { card: card.id });
        if (data.refused) {
          toast(t.fuseRefused[data.refused] || t.unavailable);
          return;
        }
        haptic("medium");
        applyState(data);
        toast(t.fused(data.fused));
        const now = cards.find((c) => c.id === card.id);
        if (now) open(now);
      } catch (error) {
        button.disabled = false;
        notice(error);
      }
    });
    box.append(button, hint);
    return box;
  }

  // A Mythic card in the large view leans toward the finger, or as the
  // phone is tilted, its glint sliding with it; any other card stays flat.
  let tilted = null;
  function lean(x, y) {
    if (!tilted) return;
    tilted.classList.add("tilting");
    tilted.style.setProperty("--tx", (x * 14).toFixed(1) + "deg");
    tilted.style.setProperty("--ty", (-y * 14).toFixed(1) + "deg");
    tilted.style.setProperty("--hx", (34 + x * 60).toFixed(0) + "%");
  }
  function tilt(sheet, on) {
    tilted = on ? sheet : null;
    sheet.classList.remove("tilting");
    for (const v of ["--tx", "--ty", "--hx"]) sheet.style.removeProperty(v);
  }
  $("sheetCard").addEventListener("pointermove", (e) => {
    const box = e.currentTarget.getBoundingClientRect();
    lean((e.clientX - box.left) / box.width * 2 - 1, (e.clientY - box.top) / box.height * 2 - 1);
  });
  window.addEventListener("deviceorientation", (e) => {
    if (e.gamma == null || e.beta == null) return;
    lean(Math.max(-1, Math.min(1, e.gamma / 30)), Math.max(-1, Math.min(1, (e.beta - 45) / 30)));
  });
  function closeSheet() {
    tilted = null;
    $("sheet").hidden = true;
    try { tg.BackButton.hide(); } catch (_) { /* older clients */ }
  }
  $("sheet").addEventListener("click", closeSheet);
  try { tg.BackButton.onClick(closeSheet); } catch (_) { /* older clients */ }

  let toastTimer = 0;
  function toast(text) {
    const box = $("toast");
    box.textContent = text;
    box.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { box.hidden = true; }, 2600);
    haptic("light");
  }

  // ---------- drawing ----------

  // coin writes a number in the wallet, the chip bumping when it changes.
  function coin(id, value) {
    const node = $(id);
    const text = String(value);
    if (node.textContent === text) return;
    if (node.textContent !== "0" || text !== "0") node.parentElement.classList.remove("bump"), void node.offsetWidth, node.parentElement.classList.add("bump");
    node.textContent = text;
  }
  $("coinTokens").addEventListener("click", () => toast(t.tokenHint(perDay, cardCost)));
  $("bagButton").addEventListener("click", openBag);
  $("coinShards").setAttribute("aria-label", t.shopTab);
  $("coinShards").addEventListener("click", () => showTab("shop"));

  // cost is what n cards take: tokens first, crystals for the rest.
  function cost(n) {
    const used = Math.min(tokens, n);
    return { tokens: used, shards: (n - used) * cardCost };
  }
  function costLabel(n) {
    const c = cost(n);
    if (c.shards > shards) return t.costShort;
    return [c.tokens ? `🎟 ${c.tokens}` : "", c.shards ? `💎 ${spaced(c.shards)}` : ""].filter(Boolean).join(" + ");
  }
  function drawButton() {
    for (const [id, n] of [["open1", 1], ["open10", batch], ["open50", bigBatch]]) {
      $(id).disabled = drawing || cost(n).shards > shards;
      $(id + "Cost").textContent = drawing ? t.drawing : costLabel(n);
    }
    coin("tokenCount", tokens);
    coin("shardTop", spaced(shards));
    $("readyDot").hidden = !cards.length || !tokens || drawing || opened === "draw";
    nextTokenLine();
  }

  // nextTokenLine counts down to the next token, and asks for the state
  // when it comes.
  function nextTokenLine() {
    if (!nextTokenAt) return;
    const left = Math.max(0, nextTokenAt - Date.now());
    const s = Math.floor(left / 1000);
    const pad = (n) => String(n).padStart(2, "0");
    $("nextToken").textContent = t.nextToken(`${Math.floor(s / 3600)}:${pad(Math.floor((s % 3600) / 60))}:${pad(s % 60)}`);
    if (left === 0 && !refreshing && !drawing) {
      refreshing = true;
      api("state").then(applyState).catch(() => {}).finally(() => { refreshing = false; });
    }
  }
  setInterval(nextTokenLine, 1000);

  // pityLine shows how near the player's sure Legendary and Mythic are.
  function pityLine(p) {
    if (!p) return;
    $("pityLine").textContent = t.pity(p.legendary, p.legendary_max, p.mythic, p.mythic_max);
  }

  // ---------- shards ----------

  function shardsPanel() {
    drawButton();
  }

  $("open1").addEventListener("click", () => reveal(1));
  $("open10").addEventListener("click", () => reveal(batch));
  $("open50").addEventListener("click", () => reveal(bigBatch));

  // reveal opens n cards, for tokens and then crystals: one turns on the
  // deck, a batch is laid out together.
  async function reveal(n) {
    if (drawing || cost(n).shards > shards) return;
    drawing = true;
    const flip = $("flip");
    flip.classList.remove("shown");
    flip.classList.add("shuffle");
    $("result").textContent = "";
    $("drawnStats").textContent = "";
    $("seeCollection").hidden = true;
    drawButton();
    haptic("medium");
    try {
      const data = await api("open", { count: n });
      tokens = data.tokens ?? tokens;
      shards = data.shards ?? shards;
      if (data.short) {
        toast(t.short(data.short));
        flip.classList.remove("shuffle");
        return;
      }
      if (data.given) toast(t.given(data.given));
      pityLine(data.pity);
      if (data.drawn.some((d) => d.sure)) toast(`🛡 ${t.sureTag}`);
      const drawn = data.drawn.map((d) => ({ card: d.card, fresh: d.new, serial: d.serial }));
      for (const { card } of drawn) keep(card);
      points = data.points;
      await celebrateMythics(drawn);
      // A batch is laid out on its own; the deck stays face down.
      if (drawn.length > 1) {
        flip.classList.remove("shuffle");
        header();
        shelves();
        openedPanel(drawn);
        return;
      }
      const best = drawn.reduce((a, b) => (b.card.rarity > a.card.rarity ? b : a));
      const front = face(best.card);
      front.id = "front";
      $("front").replaceWith(front);
      await new Promise((resolve) => {
        const img = front.querySelector("img");
        if (img.complete) resolve();
        else { img.addEventListener("load", resolve); img.addEventListener("error", resolve); setTimeout(resolve, 2500); }
      });
      flip.classList.remove("shuffle");
      flip.classList.add("shown");
      haptic(best.card.rarity >= 3 ? "heavy" : "light");
      $("result").replaceChildren(...withTiers(best.fresh ? `${t.fresh} · ${t.plus(spaced(best.card.value))}` : `${t.again(copies(best.card))} · ${t.noPoints}`));
      $("drawnStats").replaceChildren(statsOf(best.card));
      lastDrawn = best.card.id;
      $("seeCollection").hidden = false;
      header();
      shelves();
    } catch (error) {
      flip.classList.remove("shuffle");
      notice(error);
    } finally {
      drawing = false;
      drawButton();
      shardsPanel();
    }
  }

  // keep puts a card just opened into the collection, with the stars the
  // card held had: a copy more is one more free copy to fuse. A card new to
  // the player shows its ⭐ button from the next state.
  function keep(card) {
    const i = cards.findIndex((c) => c.id === card.id);
    if (i < 0) return;
    const old = cards[i];
    if (old.stars) {
      for (const key of ["stars", "fuse", "attack", "health", "next_attack", "next_health"]) card[key] = old[key];
      card.free = (old.free || 0) + card.count - (old.count || 0);
    }
    cards[i] = card;
  }

  // openedPanel lays out the batch just opened, the new ones marked.
  function openedPanel(drawn, title) {
    const fresh = drawn.filter((d) => d.fresh).length;
    const panel = document.createElement("div");
    panel.className = "opened";
    panel.innerHTML = '<div class="opened-head"><b></b><span></span></div><div class="opened-grid"></div><button type="button" class="draw opened-ok"></button>';
    panel.querySelector(".opened-head b").textContent = title || t.openedN(drawn.length);
    panel.querySelector(".opened-head span").textContent = [fresh ? t.freshN(fresh) : "", drawn.length - fresh ? t.againN(drawn.length - fresh) : ""].filter(Boolean).join(" · ");
    const grid = panel.querySelector(".opened-grid");
    // a card drawn more than once is laid out once, as the owners asked on
    // 2026-11-21: its tile shows the copies held after the batch, and +n for
    // how many the batch gave
    const merged = new Map();
    for (const d of drawn) {
      const seen = merged.get(d.card.id);
      if (!seen) { merged.set(d.card.id, { card: d.card, fresh: d.fresh, times: 1 }); continue; }
      seen.times++;
      seen.fresh = seen.fresh || d.fresh;
      if ((d.card.count || 0) > (seen.card.count || 0)) seen.card = d.card;
    }
    const tiles = [...merged.values()].sort((a, b) => b.card.rarity - a.card.rarity);
    // the tiles are dealt one after another within 2.4 s, however many
    const deal = Math.min(0.09, 2.4 / tiles.length);
    tiles.forEach(({ card, fresh, times }, i) => {
      const tile = cardTile(card);
      tile.style.animationDelay = `${i * deal}s`;
      // a batch's tile shows the copies held, the card and its free copies,
      // not its stars, as the owners asked: a starred card showed ★2 here
      // and no count
      for (const extra of tile.querySelectorAll(".stars, .count")) extra.remove();
      const held = (card.free || 0) + 1;
      if (held > 1) {
        const count = document.createElement("span");
        count.className = "count";
        count.textContent = `×${held}`;
        tile.append(count);
      }
      if (fresh) {
        const tag = document.createElement("span");
        tag.className = "fresh";
        tag.textContent = t.freshTag;
        tile.append(tag);
      }
      if (times > 1) {
        const got = document.createElement("span");
        got.className = "times";
        got.textContent = `+${times}`;
        tile.append(got);
      }
      grid.append(tile);
    });
    const done = panel.querySelector(".opened-ok");
    done.textContent = t.carryOn;
    done.addEventListener("click", closeSheet);
    showPanel(panel);
  }

  // ---------- a Mythic's celebration ----------

  // celebrate shows a Mythic just drawn on the whole screen before the
  // draw's usual reveal: light turning behind it, gold confetti (and cherry
  // petals for a card dressed with them), the card rising, and, for a limited
  // card alone, which copy of it in the whole game this is: the count of
  // others' draws (a card's whole life, from before it was Mythic) means
  // nothing on them. A tap or the button closes it. Only
  // transforms and opacity move, so that weak phones keep up; with less
  // motion asked for, the card and the words stand still.
  const celebrateTexts = {
    uz: { title: "🎉 TABRIKLAYMIZ! MIFIK KARTA!", copy: (n) => `o'yindagi ${n}-nusxa` },
    ru: { title: "🎉 ПОЗДРАВЛЯЕМ! МИФИЧЕСКАЯ КАРТА!", copy: (n) => `${n}-й экземпляр в игре` },
    en: { title: "🎉 CONGRATULATIONS! MYTHIC CARD!", copy: (n) => `copy #${n} in the game` },
  };
  const ct = celebrateTexts[language] || celebrateTexts.uz;
  const serialMark = (n) => "No." + String(n).padStart(6, "0");
  const CONFETTI = 36;

  function celebrate(card, serial) {
    return new Promise((resolve) => {
      const petals = Boolean(SPECIAL[card.id] && SPECIAL[card.id].petals);
      const box = document.createElement("div");
      box.className = "celebrate";
      box.setAttribute("role", "dialog");
      box.setAttribute("aria-label", ct.title);
      let bits = "";
      for (let i = 0; i < CONFETTI; i++) {
        const kind = petals && i % 3 === 0 ? "petal" : ["gold", "pale", "deep"][i % 3];
        const style = `left:${(Math.random() * 100).toFixed(1)}%;--dx:${(Math.random() * 80 - 40).toFixed(0)}px;--r:${(360 + Math.random() * 540).toFixed(0)}deg;animation-delay:${(Math.random() * 1.6).toFixed(2)}s;animation-duration:${(2.6 + Math.random() * 1.8).toFixed(2)}s`;
        bits += `<i class="${kind}" style="${style}"></i>`;
      }
      box.innerHTML = '<div class="cel-rays" aria-hidden="true"></div><div class="cel-glow" aria-hidden="true"></div>' +
        '<div class="cel-confetti" aria-hidden="true">' + bits + "</div>" +
        '<div class="cel-body"><p class="cel-title"></p><div class="cel-card"></div><b class="cel-name"></b>' +
        '<div class="cel-serial"><span class="cel-no"></span><small></small></div>' +
        '<button type="button" class="draw cel-ok"></button></div>';
      box.querySelector(".cel-title").textContent = ct.title;
      box.querySelector(".cel-name").textContent = card.name;
      // the number is a limited card's alone, which each player gets once
      if (serial > 0 && card.limited) {
        box.querySelector(".cel-no").textContent = serialMark(serial);
        box.querySelector(".cel-serial small").textContent = ct.copy(serial);
      } else {
        box.querySelector(".cel-serial").hidden = true;
      }
      const tile = cardTile(card);
      tile.tabIndex = -1;
      for (const extra of tile.querySelectorAll(".count, .lmark .serial, .name")) extra.remove();
      box.querySelector(".cel-card").append(tile);
      const ok = box.querySelector(".cel-ok");
      ok.textContent = t.carryOn;
      // A tap that came with the draw itself is not taken for a close.
      const shownAt = Date.now();
      let closed = false;
      box.addEventListener("click", (e) => {
        e.stopPropagation();
        if (closed || (e.target !== ok && Date.now() - shownAt < 700)) return;
        closed = true;
        box.classList.add("leaving");
        setTimeout(() => { box.remove(); resolve(); }, 220);
      });
      document.body.append(box);
      ok.focus({ preventScroll: true });
      try {
        tg.HapticFeedback.notificationOccurred("success");
        setTimeout(() => { try { tg.HapticFeedback.impactOccurred("heavy"); } catch (_) { /* outside Telegram */ } }, 180);
      } catch (_) { /* outside Telegram */ }
    });
  }

  // celebrateMythics celebrates each Mythic of a draw in turn, a card drawn
  // more than once only once, as its batch's panel lays it out once.
  async function celebrateMythics(drawn) {
    const seen = new Set();
    for (const d of drawn) {
      if (d.card.rarity !== 4 || seen.has(d.card.id)) continue;
      seen.add(d.card.id);
      await celebrate(d.card, d.serial);
    }
  }

  // The deck itself is pressed as often as the button: it draws too.
  $("deck").addEventListener("click", () => { if (!$("open1").disabled) reveal(1); });

  let lastDrawn = 0;
  $("seeCollection").addEventListener("click", () => {
    showTab("collection");
    const card = cards.find((c) => c.id === lastDrawn);
    const tile = [...document.querySelectorAll("#shelves .card")].find((el) => el.querySelector(".name") && card && el.querySelector(".name").textContent === card.name);
    if (tile) tile.scrollIntoView({ block: "center", behavior: "smooth" });
  });
  $("welcomeDraw").addEventListener("click", () => showTab("draw"));

  const odds = $("odds");
  for (let rarity = 4; rarity >= 0; rarity--) {
    const li = document.createElement("li");
    li.style.setProperty("--c", COLORS[rarity]);
    li.innerHTML = `<b></b> ${ODDS[rarity]}`;
    li.querySelector("b").replaceChildren(tierIcon(rarity), t.rarities[rarity]);
    odds.append(li);
  }

  // ---------- market ----------

  let offers = [], offerFilter = "all";

  // mini is a small card: its picture, rarity and name.
  function mini(card) {
    const box = document.createElement("div");
    box.className = "mini";
    box.style.setProperty("--c", COLORS[card.rarity]);
    box.innerHTML = '<img alt=""><span><small></small><b></b></span>';
    picture(box.querySelector("img"), card.image);
    box.querySelector("small").replaceChildren(tierIcon(card.rarity), t.rarities[card.rarity]);
    box.querySelector("b").textContent = card.name;
    return box;
  }

  async function market() {
    try {
      const data = await api("market");
      offers = data.offers || [];
      marketList();
      notice(null);
    } catch (error) {
      notice(error);
    }
  }

  // OFFER_FILTERS are the market's filters: "all" holds the player's own
  // offers too, marked, so one just placed is seen on the market; "can" is
  // others' the player can take; "mine" the player's own.
  const OFFER_FILTERS = {
    all: () => true,
    can: (o) => o.can && !o.mine,
    mine: (o) => o.mine,
  };

  function marketList() {
    $("marketHint").textContent = t.marketHint();
    // each filter shows how many offers it holds, as the owners asked
    const labels = { all: t.filterAll, can: t.filterCan, mine: t.filterMine };
    for (const chip of document.querySelectorAll("#marketFilter button")) {
      chip.setAttribute("aria-pressed", String(chip.dataset.f === offerFilter));
      const count = document.createElement("small");
      count.textContent = offers.filter(OFFER_FILTERS[chip.dataset.f]).length;
      chip.replaceChildren(labels[chip.dataset.f], count);
    }
    const list = $("offers");
    list.textContent = "";
    const shown = offers.filter(OFFER_FILTERS[offerFilter] || OFFER_FILTERS.all);
    if (!shown.length) list.innerHTML = `<li class="empty">${t.marketEmpty}</li>`;
    for (const o of shown) {
      const li = document.createElement("li");
      li.className = o.mine ? "offer mine" : "offer";
      li.innerHTML = '<div class="offer-cards"><div class="side"><em></em></div><span class="arrow" aria-hidden="true">⇄</span><div class="side"><em></em></div></div><div class="offer-foot"><small></small><button type="button"></button></div>';
      const sides = li.querySelectorAll(".side");
      sides[0].querySelector("em").textContent = t.gives;
      sides[0].append(mini(o.give));
      sides[1].querySelector("em").textContent = t.wants;
      sides[1].append(mini(o.want));
      li.querySelector(".offer-foot small").textContent = o.mine ? t.yours : o.who || "?";
      const button = li.querySelector(".offer-foot button");
      if (o.mine) {
        button.className = "ghost";
        button.textContent = t.remove;
        button.addEventListener("click", () => unoffer(o, button));
      } else {
        button.className = o.can ? "draw small" : "ghost";
        // a copy of the card wanted raised above ★1 never moves, so the
        // offer is not for this player to take
        button.textContent = o.can ? t.swap : o.starred ? t.starredCard : t.noCard;
        button.disabled = !o.can;
        button.addEventListener("click", () => takeOffer(o, button));
      }
      list.append(li);
    }
  }

  for (const chip of document.querySelectorAll("#marketFilter button")) {
    chip.addEventListener("click", () => { offerFilter = chip.dataset.f; marketList(); haptic("light"); });
  }

  async function takeOffer(o, button) {
    button.disabled = true;
    try {
      const data = await api("take", { offer: o.id });
      if (data.refused) {
        toast(t.refused[data.refused] || t.unavailable);
        market();
        return;
      }
      applyState(data);
      toast(t.swapped(data.got ? data.got.name : o.give.name));
      haptic("heavy");
      market();
    } catch (error) {
      button.disabled = false;
      notice(error);
    }
  }

  async function unoffer(o, button) {
    button.disabled = true;
    try {
      const data = await api("unoffer", { offer: o.id });
      offers = data.offers || [];
      marketList();
      toast(t.removed);
    } catch (error) {
      button.disabled = false;
      notice(error);
    }
  }

  // showPanel opens the sheet with a panel in place of a large card.
  function showPanel(panel) {
    const sheet = $("sheetCard");
    sheet.textContent = "";
    tilt(sheet, false);
    sheet.classList.add("giving");
    panel.addEventListener("click", (event) => event.stopPropagation());
    sheet.append(panel);
    $("sheet").hidden = false;
    try { tg.BackButton.show(); } catch (_) { /* older clients */ }
  }

  // offerPanel makes an offer in two steps: a card of the player's, then the
  // card they want for it, of the same rarity or one up or down, chosen by
  // its chips: the first ten of the catalog of that rarity show at once, the
  // most loved first, ten more as the list is scrolled down, a name typed
  // narrowing them. A limited card is in neither list, nor is a card of the
  // player's raised above ★1, as its stars stay with them.
  function offerPanel() {
    let give = null, want = null, timer = 0, wantRarity = 0;
    // the pages of the card wanted: asked counts the lists begun, so that a
    // page come late for an older list is dropped
    let asked = 0, offset = 0, more = false, loading = false;
    const panel = document.createElement("div");
    panel.className = "gift";
    panel.innerHTML = '<b class="step"></b><input type="search" class="mine-q" autocomplete="off"><ul class="gift-list pick-mine"></ul><b class="step"></b><div class="sets want-rarity" role="group" hidden></div><input type="search" class="want-q" autocomplete="off" disabled><ul class="gift-list pick-want"></ul><button type="button" class="draw gift-send" disabled></button>';
    const steps = panel.querySelectorAll(".step");
    steps[0].textContent = t.step1;
    steps[1].textContent = t.step2;
    const mineQ = panel.querySelector(".mine-q"), wantQ = panel.querySelector(".want-q");
    mineQ.placeholder = t.mineSearch;
    wantQ.placeholder = t.wantSearch;
    const send = panel.querySelector(".gift-send");
    send.textContent = t.place;
    const ready = () => { send.disabled = !(give && want); };
    const wanted = panel.querySelector(".pick-want");

    function pickRow(list, card, chosen, onPick, before) {
      const li = document.createElement("li");
      const pick = document.createElement("button");
      pick.type = "button";
      pick.className = "pick";
      pick.append(mini(card));
      pick.setAttribute("aria-pressed", String(!!chosen && chosen.id === card.id));
      pick.addEventListener("click", () => {
        for (const other of list.querySelectorAll("button")) other.setAttribute("aria-pressed", String(other === pick));
        onPick(card);
        haptic("light");
      });
      li.append(pick);
      list.insertBefore(li, before || null);
    }
    function mineList() {
      const list = panel.querySelector(".pick-mine");
      list.textContent = "";
      const q = mineQ.value.trim().toLowerCase();
      const held = cards.filter((c) => c.count > 0 && !c.limited && !(c.stars > 1) && (!q || c.name.toLowerCase().includes(q))).sort((a, b) => b.rarity - a.rarity).slice(0, 30);
      if (!held.length) list.innerHTML = `<li class="gift-empty">${t.none}</li>`;
      for (const card of held) pickRow(list, card, give, choose);
    }
    // rarityChips are the rarities the card given may be traded for: its
    // own, picked first, and the one below and above it there are.
    const rarityBox = panel.querySelector(".want-rarity");
    function rarityChips() {
      rarityBox.textContent = "";
      for (const r of [give.rarity - 1, give.rarity, give.rarity + 1]) {
        if (r < 0 || r >= t.rarities.length) continue;
        const chip = document.createElement("button");
        chip.type = "button";
        chip.textContent = t.rarities[r];
        chip.setAttribute("aria-pressed", String(r === wantRarity));
        chip.addEventListener("click", () => {
          if (r === wantRarity) return;
          wantRarity = r;
          want = null;
          ready();
          rarityChips();
          wantList();
          haptic("light");
        });
        rarityBox.append(chip);
      }
      rarityBox.hidden = false;
    }
    function choose(card) {
      const sameRarity = give && give.rarity === card.rarity;
      give = card;
      if (!sameRarity) wantRarity = card.rarity;
      steps[1].textContent = t.step2;
      wantQ.disabled = false;
      if (!sameRarity || (want && want.id === card.id)) want = null;
      ready();
      rarityChips();
      wantList();
    }

    // sentinel is the last row of the list: once it is seen, the next page
    // is asked for; it says the page is loading meanwhile.
    const sentinel = document.createElement("li");
    sentinel.className = "gift-empty want-more";
    sentinel.textContent = t.more;
    // near is the end of the list in sight; never so while the list is not
    // shown, as with the sheet closed
    const near = () => wanted.offsetParent !== null && sentinel.getBoundingClientRect().top <= wanted.getBoundingClientRect().bottom + 120;
    const watcher = "IntersectionObserver" in window
      ? new IntersectionObserver((seen) => { if (seen.some((e) => e.isIntersecting)) wantPage(); }, { root: wanted, rootMargin: "0px 0px 120px 0px" })
      : null;
    if (watcher) watcher.observe(sentinel);
    else wanted.addEventListener("scroll", () => { if (near()) wantPage(); });

    // wantList begins the list of the card wanted again, for the card given
    // and the name typed.
    function wantList() {
      asked++;
      offset = 0;
      more = true;
      loading = false;
      wanted.textContent = "";
      wanted.scrollTop = 0;
      wanted.append(sentinel);
      wantPage();
    }
    async function wantPage() {
      if (!give || !more || loading) return;
      const mine = asked;
      loading = true;
      sentinel.hidden = false;
      try {
        const data = await api("catalog", { rarity: wantRarity, card: give.id, query: wantQ.value.trim(), offset });
        if (mine !== asked) return;
        for (const card of data.cards) pickRow(wanted, card, want, (c) => { want = c; ready(); }, sentinel);
        offset += data.cards.length;
        more = !!data.more && data.cards.length > 0;
        if (!offset) wanted.insertAdjacentHTML("afterbegin", `<li class="gift-empty">${t.none}</li>`);
      } catch (error) {
        if (mine !== asked) return;
        more = false;
        notice(error);
      } finally {
        if (mine === asked) {
          loading = false;
          sentinel.hidden = !more;
          // the end still in sight, as a page too short to scroll leaves
          // it, asks for the next at once: the watcher sees only a change
          if (more && near()) wantPage();
        }
      }
    }
    mineQ.addEventListener("input", mineList);
    wantQ.addEventListener("input", () => { clearTimeout(timer); timer = setTimeout(wantList, 250); });
    send.addEventListener("click", async () => {
      if (!give || !want) return;
      send.disabled = true;
      try {
        const data = await api("offer", { card: give.id, want: want.id });
        if (data.refused) {
          toast(data.refused === "limit" ? t.offersLimit(data.limit) : t.refused[data.refused] || t.unavailable);
          ready();
          return;
        }
        offers = data.offers || [];
        offerFilter = "mine";
        marketList();
        closeSheet();
        toast(t.placed);
        haptic("medium");
      } catch (error) {
        ready();
        notice(error);
      }
    });
    mineList();
    showPanel(panel);
  }
  $("newOffer").addEventListener("click", offerPanel);

  // ---------- ranking ----------

  async function ranking() {
    for (const chip of document.querySelectorAll("#topKind button")) chip.setAttribute("aria-pressed", String(chip.dataset.k === topKind));
    const asked = ++topAsked, kind = topKind;
    try {
      const data = await api(kind === "arena" ? "arena" : "top");
      if (asked !== topAsked) return;
      notice(null);
      $("arenaWeek").hidden = kind !== "arena";
      if (kind === "arena") { arenaTable(data.top || [], data.me, data.player); arenaWeek(data.week); return; }
      const table = $("table");
      table.textContent = "";
      if (!data.top.length) table.innerHTML = `<p class="empty">${t.noTop}</p>`;
      data.top.forEach((row, i) => {
        const li = document.createElement("li");
        if (row.me) li.className = "me";
        li.innerHTML = `<span class="pos"></span><span class="who"><b></b><small></small></span><span class="pts"></span>`;
        li.querySelector(".pos").textContent = ["🥇", "🥈", "🥉"][i] || i + 1;
        li.querySelector(".who b").textContent = row.name || "?";
        li.querySelector(".who small").textContent = t.cards(row.cards);
        li.querySelector(".pts").textContent = spaced(row.points);
        table.append(li);
      });
      const me = data.me;
      if (me && me.place > data.top.length) table.append(placeRow(me.place, me.total, me.name, t.cards(me.cards), spaced(me.points)));
    } catch (error) {
      if (asked === topAsked) notice(error);
    }
  }

  // ---------- every anime ----------

  // An anime's seasons and sequels are one anime here, as players know them.
  const FRANCHISES = ["Attack on Titan", "My Hero Academia", "JUJUTSU KAISEN", "HAIKYU!!", "Demon Slayer", "Naruto", "JoJo's Bizarre Adventure",
    "The Seven Deadly Sins", "Re:ZERO", "Sword Art Online", "Assassination Classroom", "Danganronpa"];
  function franchise(anime) {
    if (/^(Bakemonogatari|Nisemonogatari|Monogatari)/.test(anime)) return "Monogatari";
    if (anime.startsWith("Fate/")) return "Fate";
    return FRANCHISES.find((f) => anime.startsWith(f)) || anime;
  }
  let catalogAll = null, animeQuery = "", animeClaimed = new Set(), animeReward = null;
  async function animePage() {
    if (!catalogAll) {
      try {
        const data = await api("animes");
        catalogAll = data.cards;
        animeClaimed = new Set(data.claimed || []);
        animeReward = data.reward || null;
      } catch (error) {
        notice(error);
        return;
      }
    }
    animeList();
  }
  // animeGoal is an anime's collection: the cards it needs (those only an
  // event gives, limited or not, aside, as the server counts them), how many
  // are held, and its reward's state: none (too small), open, ready (all
  // held) or claimed.
  function animeGoal(name, list) {
    const need = list.filter((c) => !c.limited && !c.event_only);
    const held = need.filter((c) => heldCount(c.id)).length;
    let state = "none";
    if (animeReward && need.length >= animeReward.least) {
      state = animeClaimed.has(name) ? "claimed" : held === need.length ? "ready" : "open";
    }
    return { need: need.length, held, state };
  }
  function heldCount(id) {
    const own = cards.find((c) => c.id === id);
    return own ? own.count : 0;
  }
  function animeList() {
    $("animeCards").hidden = true;
    const root = $("animeList");
    root.hidden = false;
    root.textContent = "";
    const q = animeQuery.trim().toLowerCase();
    const groups = new Map();
    for (const card of catalogAll) {
      const name = card.franchise || franchise(card.anime || "");
      if (!groups.has(name)) groups.set(name, []);
      groups.get(name).push(card);
    }
    const shown = [...groups].filter(([name, list]) => !q || name.toLowerCase().includes(q) || list.some((c) => c.name.toLowerCase().includes(q)))
      .sort((a, b) => b[1].length - a[1].length || a[0].localeCompare(b[0]));
    for (const [name, list] of shown) {
      list.sort((a, b) => b.rarity - a.rarity);
      const row = document.createElement("button");
      row.type = "button";
      row.className = "anime-row";
      row.style.setProperty("--c", COLORS[list[0].rarity]);
      const img = new Image();
      img.loading = "lazy";
      img.alt = "";
      picture(img, list[0].image);
      const text = document.createElement("span");
      const title = document.createElement("b");
      title.textContent = name;
      const dots = document.createElement("small");
      for (const card of list.slice(0, 12)) {
        const dot = document.createElement("i");
        dot.style.setProperty("--c", COLORS[card.rarity]);
        dots.append(dot);
      }
      text.append(title, dots);
      const goal = animeGoal(name, list);
      const held = document.createElement("span");
      held.className = "held";
      held.innerHTML = `${goal.held} <em>/ ${goal.need}</em>`;
      if (goal.state !== "none") {
        const tag = document.createElement("i");
        tag.className = "goal " + goal.state;
        tag.textContent = goal.state === "claimed" ? "✅" : goal.state === "ready" ? "🎁" : `🎟${animeReward.tokens}`;
        held.append(tag);
      }
      row.append(img, text, held);
      row.addEventListener("click", () => { animeCards(name, list); haptic("light"); });
      root.append(row);
    }
    if (!shown.length) root.innerHTML = `<p class="empty">${t.animeEmpty}</p>`;
  }
  function animeCards(name, list) {
    $("animeList").hidden = true;
    $("animeCards").hidden = false;
    $("animeTitle").textContent = name;
    const goal = animeGoal(name, list);
    $("animeHeld").textContent = `${goal.held} / ${goal.need}`;
    animeRewardBar(name, list, goal);
    const grid = $("animeGrid");
    grid.textContent = "";
    for (const card of list) grid.append(previewTile(card, heldCount(card.id)));
    window.scrollTo(0, 0);
  }
  // animeRewardBar is the anime's reward under its title: what collecting
  // it gives, the button to take it once all is held, or that it was taken.
  function animeRewardBar(name, list, goal) {
    const bar = $("animeReward");
    bar.hidden = goal.state === "none";
    bar.textContent = "";
    bar.className = "anime-reward " + goal.state;
    if (bar.hidden) return;
    if (goal.state !== "ready") {
      bar.textContent = goal.state === "claimed" ? t.animeClaimed(animeReward.tokens) : t.animeGoal(animeReward.tokens);
      return;
    }
    const button = document.createElement("button");
    button.type = "button";
    button.className = "draw";
    button.textContent = t.animeClaim(animeReward.tokens);
    button.addEventListener("click", async () => {
      button.disabled = true;
      try {
        const data = await api("anime_claim", { query: name });
        tokens = data.tokens ?? tokens;
        if (data.given || data.refused === "claimed") animeClaimed.add(name);
        if (data.given) { toast(t.animeGot(data.given)); haptic("heavy"); }
        header();
        drawButton();
        animeCards(name, list);
      } catch (error) {
        button.disabled = false;
        notice(error);
      }
    });
    bar.append(button);
  }

  // previewTile shows a card whether it is held or not: its picture and name,
  // dimmed and unmarked while the player does not have it.
  // held, when given, is what its preview says the player has, apart from
  // how the tile is drawn: the event's banner draws its card in full for
  // everyone.
  function previewTile(card, count, held = count) {
    const tile = document.createElement("button");
    tile.type = "button";
    tile.className = "card" + (count ? "" : " missing");
    tile.style.setProperty("--c", COLORS[card.rarity]);
    if (count && card.rarity >= 3) tile.classList.add("foil");
    if (card.rarity === 4) tile.classList.add("mythic");
    const img = new Image();
    img.loading = "lazy";
    img.alt = "";
    picture(img, underImage(card));
    const name = document.createElement("span");
    name.className = "name";
    name.textContent = card.name;
    tile.append(img, name);
    if (card.rarity === 4) {
      img.after(alive(card));
      tile.insertAdjacentHTML("beforeend", MYTHIC_DRESS);
    }
    dress(tile, card, false);
    if (count) tile.insertAdjacentHTML("beforeend", '<span class="owned" aria-hidden="true">✓</span>');
    tile.addEventListener("click", () => previewCard(card, held));
    return tile;
  }
  function previewCard(card, count) {
    const sheet = $("sheetCard");
    sheet.textContent = "";
    sheet.classList.remove("giving");
    const note = document.createElement("p");
    note.className = "preview-note";
    note.textContent = count ? t.animeOwned : t.animeMissing;
    sheet.append(...turnable(card), note);
    tilt(sheet, card.rarity === 4);
    $("sheet").hidden = false;
    try { tg.BackButton.show(); } catch (_) { /* older clients */ }
    haptic("light");
  }
  $("animeSearch").addEventListener("input", (event) => {
    animeQuery = event.target.value;
    if (catalogAll) animeList();
  });
  $("animeBack").addEventListener("click", () => { animeList(); window.scrollTo(0, 0); });

  // ---------- shop ----------

  const RARITY_ICON = ["⚪", "🔷", "💜", "🔥", "👑"];
  let shopData = null, buying = false;

  // shop lists the crystals sold for Stars and what crystals buy.
  async function shop() {
    eventBlock();
    $("shopWallet").textContent = t.wallet(spaced(shards));
    try {
      shopData = shopData || await api("shop");
    } catch (error) {
      notice(error);
      return;
    }
    const packs = $("crystalPacks");
    packs.replaceChildren();
    const base = shopData.packs[0] ? shopData.packs[0].crystals / shopData.packs[0].stars : 0;
    for (const pack of shopData.packs) {
      if (pack.box) { packs.append(boxButton(pack)); continue; }
      const button = document.createElement("button");
      button.type = "button";
      button.className = "pack-btn";
      const gain = base ? Math.round(((pack.crystals / pack.stars) / base - 1) * 100) : 0;
      button.innerHTML = '<b></b><span class="stars"></span><small></small>';
      button.querySelector("b").textContent = t.crystalsN(spaced(pack.crystals));
      button.querySelector(".stars").textContent = t.starsN(pack.stars);
      button.querySelector("small").textContent = gain > 0 ? t.bonus(gain) : "";
      button.disabled = !shopData.stars;
      button.addEventListener("click", () => buyCrystals(pack));
      packs.append(button);
    }
    const list = $("shopItems");
    list.replaceChildren();
    for (const item of shopData.items) {
      const row = document.createElement("li");
      row.className = "shop-item";
      const rarity = t.rarities[item.least || 0];
      row.style.setProperty("--tier", item.tokens ? "var(--r1)" : COLORS[item.least || 0]);
      row.innerHTML = '<span class="icon"></span><div><b></b><small></small></div><button type="button" class="ghost"></button>';
      row.querySelector(".icon").textContent = item.tokens ? "🎟" : RARITY_ICON[item.least || 0];
      row.querySelector("b").textContent = item.tokens ? t.tokensItem(item.tokens) : t.packItem(rarity);
      row.querySelector("small").textContent = item.tokens ? t.tokensHint(item.tokens) : t.packHint(item.cards, rarity);
      const button = row.querySelector("button");
      button.textContent = t.buyFor(spaced(item.price));
      button.disabled = item.price > shards;
      button.addEventListener("click", () => buyItem(item));
      list.append(row);
    }
  }

  // boxButton is the diamond box: a compact card that buys it on a tap,
  // with a small ⓘ in its corner that shows the chances, quietly.
  function boxButton(pack) {
    const wrap = document.createElement("div");
    wrap.className = "box-wrap";
    wrap.innerHTML = '<button type="button" class="pack-btn box-btn"><span class="box-icon">🎁</span><span class="box-text"><span class="box-title"></span><b></b></span><span class="stars"></span></button><button type="button" class="box-i" aria-label="ⓘ">ⓘ</button>';
    const buy = wrap.querySelector(".box-btn");
    buy.querySelector(".box-title").textContent = t.boxTitle.replace("🎁 ", "");
    buy.querySelector("b").textContent = t.boxRange(spaced(pack.box[0].crystals), spaced(pack.box[pack.box.length - 1].crystals));
    buy.querySelector(".stars").textContent = t.starsN(pack.stars);
    buy.disabled = !shopData.stars;
    buy.addEventListener("click", () => buyCrystals(pack));
    wrap.querySelector(".box-i").addEventListener("click", () => showPanel(boxInfoPanel(pack)));
    return wrap;
  }

  // boxInfoPanel is just the box's prizes and chances, in small print.
  function boxInfoPanel(pack) {
    const panel = document.createElement("div");
    panel.className = "gift box-info";
    panel.innerHTML = '<ul class="box-odds"></ul>';
    const list = panel.querySelector(".box-odds");
    for (const prize of pack.box) {
      const item = document.createElement("li");
      item.innerHTML = "<span></span><span></span>";
      item.children[0].textContent = t.crystalsN(spaced(prize.crystals));
      item.children[1].textContent = `${prize.percent}%`;
      list.append(item);
    }
    return panel;
  }

  // ---------- bag: boxes and mail ----------

  // bagBadge counts on the 🎒 what waits: boxes held and letters to claim.
  function bagBadge(inbox) {
    const n = (inbox.boxes || 0) + (inbox.mail || 0);
    const badge = $("bagBadge");
    badge.hidden = n === 0;
    badge.textContent = n > 9 ? "9+" : String(n);
  }

  async function openBag() {
    haptic("light");
    try {
      const data = await api("inbox");
      bagBadge({
        boxes: (data.boxes || []).reduce((n, b) => n + b.count, 0),
        mail: (data.mail || []).filter((m) => !m.claimed).length,
      });
      showPanel(bagPanel(data));
    } catch (error) {
      notice(error);
    }
  }

  function boxRange(pack) {
    return t.boxRange(spaced(pack.box[0].crystals), spaced(pack.box[pack.box.length - 1].crystals));
  }

  // bagPanel lists the boxes held, each with its count and an open button,
  // and the letters, newest first, with their gifts and a claim button.
  function bagPanel(data) {
    const panel = document.createElement("div");
    panel.className = "gift inbox";
    const boxTitle = document.createElement("h3");
    boxTitle.textContent = t.bagBoxes;
    panel.append(boxTitle);
    const boxes = (data.boxes || []).filter((b) => b.count > 0);
    if (!boxes.length) panel.append(emptyLine(t.bagNoBoxes));
    for (const b of boxes) {
      const row = inboxRow("🎁", t.boxTitle.replace("🎁 ", "") + ` ×${b.count}`, boxRange({ box: b.box }));
      const open = document.createElement("button");
      open.type = "button";
      open.textContent = t.bagOpen;
      open.addEventListener("click", async () => {
        open.disabled = true;
        try {
          const res = await api("box_open", { item: b.id });
          if (res.refused) { openBag(); return; }
          shards = res.shards ?? shards;
          header();
          shardsPanel();
          const badge = $("bagBadge");
          const left = Math.max(0, (parseInt(badge.textContent, 10) || 1) - 1);
          badge.hidden = left === 0;
          badge.textContent = String(left);
          haptic("heavy");
          showPanel(boxResultPanel({ box: [{ crystals: res.least }] }, res.won));
        } catch (error) {
          open.disabled = false;
          notice(error);
        }
      });
      row.append(open);
      panel.append(row);
    }
    const mailTitle = document.createElement("h3");
    mailTitle.textContent = t.bagMail;
    panel.append(mailTitle);
    const mail = data.mail || [];
    if (!mail.length) panel.append(emptyLine(t.bagNoMail));
    for (const m of mail) {
      const gifts = mailGifts(m);
      const row = inboxRow("📬", m.note || t.bagGift, gifts);
      if (m.claimed) {
        row.classList.add("done");
        const mark = document.createElement("span");
        mark.className = "done-mark";
        mark.textContent = "✅";
        row.append(mark);
      } else {
        const claim = document.createElement("button");
        claim.type = "button";
        claim.textContent = t.bagClaim;
        claim.addEventListener("click", async () => {
          claim.disabled = true;
          try {
            const res = await api("mail_claim", { offer: m.id });
            if (!res.refused) {
              tokens = res.tokens ?? tokens;
              shards = res.shards ?? shards;
              header();
              shardsPanel();
              drawButton();
              toast(t.bagClaimed(gifts));
              haptic("medium");
            }
            openBag();
          } catch (error) {
            claim.disabled = false;
            notice(error);
          }
        });
        row.append(claim);
      }
      panel.append(row);
    }
    return panel;
  }

  function mailGifts(m) {
    const parts = [];
    if (m.boxes) parts.push("🎁 " + t.bagBoxN(m.boxes));
    if (m.tokens) parts.push("🎟 " + t.bagTokensN(m.tokens));
    if (m.crystals) parts.push("💎 " + t.bagCrystalsN(spaced(m.crystals)));
    return parts.join(" · ");
  }

  function inboxRow(icon, title, line) {
    const row = document.createElement("div");
    row.className = "inbox-row";
    row.innerHTML = '<span class="ico"></span><span><b></b><small></small></span>';
    row.querySelector(".ico").textContent = icon;
    row.querySelector("b").textContent = title;
    row.querySelector("small").textContent = line;
    return row;
  }

  function emptyLine(text) {
    const p = document.createElement("p");
    p.className = "empty-line";
    p.textContent = text;
    return p;
  }

  // boxResultPanel shows what a box gave, big, with a word for a rare one.
  function boxResultPanel(pack, came) {
    const panel = document.createElement("div");
    panel.className = "gift box-panel box-result";
    const rare = came > pack.box[0].crystals;
    panel.innerHTML = '<div class="box-hero open">🎁</div><small class="box-hint"></small><b class="box-won"></b><small class="box-lucky"></small><button type="button" class="draw box-ok"></button>';
    panel.querySelector(".box-hint").textContent = t.boxGot;
    panel.querySelector(".box-won").textContent = `+${spaced(came)} 💎`;
    panel.querySelector(".box-lucky").textContent = rare ? t.boxLucky : "";
    const ok = panel.querySelector(".box-ok");
    ok.textContent = t.carryOn;
    ok.addEventListener("click", closeSheet);
    return panel;
  }

  // buyItem buys tokens or a pack for crystals, laying a pack out as a
  // batch is.
  async function buyItem(item) {
    if (buying) return;
    buying = true;
    haptic("medium");
    try {
      const data = await api("buy", { item: item.id });
      tokens = data.tokens ?? tokens;
      shards = data.shards ?? shards;
      if (data.short) { toast(t.short(data.short)); return; }
      if (item.tokens) {
        toast(t.bought(item.tokens));
      } else {
        const drawn = data.drawn.map((d) => ({ card: d.card, fresh: d.new, serial: d.serial }));
        for (const { card } of drawn) keep(card);
        points = data.points ?? points;
        header();
        shelves();
        await celebrateMythics(drawn);
        openedPanel(drawn, t.packOpened(t.rarities[item.least || 0]));
      }
    } catch (error) {
      notice(error);
    } finally {
      buying = false;
      drawButton();
      shardsPanel();
      shop();
    }
  }

  // buyCrystals opens Telegram's payment for a crystal pack; the crystals
  // come when the bot hears of the payment, so the state is asked for
  // until they are there.
  async function buyCrystals(pack) {
    if (buying) return;
    if (!tg.openInvoice) { toast(t.payFailed); return; }
    buying = true;
    try {
      const { link } = await api("invoice", { item: pack.id });
      tg.openInvoice(link, (status) => {
        buying = false;
        if (status === "paid") {
          toast(t.paid);
          awaitCrystals(shards, pack, 0);
        } else if (status === "failed") {
          toast(t.payFailed);
        }
      });
    } catch (error) {
      buying = false;
      notice(error);
    }
  }

  // awaitCrystals asks the state until the pack's crystals are there; a
  // box tells what it gave by how many came (at least its least prize).
  function awaitCrystals(before, pack, tries) {
    setTimeout(async () => {
      try {
        applyState(await api("state"));
        if (shards >= before + pack.crystals) {
          const came = shards - before;
          if (pack.box) showPanel(boxResultPanel(pack, came));
          else toast(t.crystalsAdded(pack.crystals));
          haptic("heavy");
          shop();
          return;
        }
      } catch (_) { /* asked again below */ }
      if (tries < 8) awaitCrystals(before, pack, tries + 1);
    }, 1500);
  }

  // ---------- events ----------

  // The events: pulls for tokens at the top of the shop, at the usual odds,
  // a share of the Mythics the event's own cards, a sure one after so many
  // pulls in a row without. The server lists those running or coming, the
  // newest last: the Firefly Wedding's limited Satoko and Demon Slayer's six
  // Mythics, which nothing else gives.
  const eventTexts = {
    uz: {
      left: (d, h, m) => (d ? `⏳ ${d} kun ${h} soat qoldi` : h ? `⏳ ${h} soat ${m} daqiqa qoldi` : `⏳ ${m} daqiqa qoldi`),
      odds: (m, icon, name, s) => `👑 Mifik ${m} · ${icon} ${name} ${s}`, pity: (name, n, max) => `${name} kafolati: ${n}/${max}`,
      open: (n, price) => `${n * price} 🎟`, short: (n) => `🎟 Yana ${n} ta token kerak`, over: "Event tugadi", starts: (h, m) => (h ? `⏳ ${h} soat ${m} daqiqadan keyin boshlanadi` : `⏳ ${m} daqiqadan keyin boshlanadi`), soon: "Event hali boshlanmadi",
      pulled: (icon, n) => `${icon} ${n} ta event karta`, got: (icon, name) => `${icon} ${name} tushdi!`, badge: (icon) => `${icon} EVENT`, go: "Ochish ›",
      firefly: { icon: "🌸", title: "🌸 Firefly Wedding", tag: "EVENT · LIMITED", name: "Satoko", owned: "✅ Satoko sizda bor — har kim faqat bir marta oladi" },
      "demon-slayer": { icon: "⚔️", title: "⚔️ Demon Slayer", tag: "EVENT · 6 MIF", name: "Demon Slayer" },
      "black-clover": { icon: "🍀", title: "🍀 Black Clover", tag: "EVENT · 6 MIF", name: "Black Clover" },
    },
    ru: {
      left: (d, h, m) => (d ? `⏳ Осталось ${d} дн. ${h} ч.` : h ? `⏳ Осталось ${h} ч. ${m} мин.` : `⏳ Осталось ${m} мин.`),
      odds: (m, icon, name, s) => `👑 Мифическая ${m} · ${icon} ${name} ${s}`, pity: (name, n, max) => `Гарант ${name}: ${n}/${max}`,
      open: (n, price) => `${n * price} 🎟`, short: (n) => `🎟 Нужно ещё ${n} токенов`, over: "Ивент закончился", starts: (h, m) => (h ? `⏳ Начнётся через ${h} ч. ${m} мин.` : `⏳ Начнётся через ${m} мин.`), soon: "Ивент ещё не начался",
      pulled: (icon, n) => `${icon} Ивент-карты: ${n}`, got: (icon, name) => `${icon} Выпал(а) ${name}!`, badge: (icon) => `${icon} ИВЕНТ`, go: "Открыть ›",
      firefly: { icon: "🌸", title: "🌸 Firefly Wedding", tag: "ИВЕНТ · LIMITED", name: "Сатоко", owned: "✅ Сатоко уже у вас — её можно получить только один раз" },
      "demon-slayer": { icon: "⚔️", title: "⚔️ Demon Slayer", tag: "ИВЕНТ · 6 МИФ", name: "Demon Slayer" },
      "black-clover": { icon: "🍀", title: "🍀 Black Clover", tag: "ИВЕНТ · 6 МИФ", name: "Black Clover" },
    },
    en: {
      left: (d, h, m) => (d ? `⏳ ${d} days ${h} hours left` : h ? `⏳ ${h} hours ${m} minutes left` : `⏳ ${m} minutes left`),
      odds: (m, icon, name, s) => `👑 Mythic ${m} · ${icon} ${name} ${s}`, pity: (name, n, max) => `${name} guarantee: ${n}/${max}`,
      open: (n, price) => `${n * price} 🎟`, short: (n) => `🎟 ${n} more tokens needed`, over: "The event is over", starts: (h, m) => (h ? `⏳ Starts in ${h} hours ${m} minutes` : `⏳ Starts in ${m} minutes`), soon: "The event has not started yet",
      pulled: (icon, n) => `${icon} ${n} event cards`, got: (icon, name) => `${icon} ${name} dropped!`, badge: (icon) => `${icon} EVENT`, go: "Open ›",
      firefly: { icon: "🌸", title: "🌸 Firefly Wedding", tag: "EVENT · LIMITED", name: "Satoko", owned: "✅ You have Satoko — each player gets her once" },
      "demon-slayer": { icon: "⚔️", title: "⚔️ Demon Slayer", tag: "EVENT · 6 MYTHIC", name: "Demon Slayer" },
      "black-clover": { icon: "🍀", title: "🍀 Black Clover", tag: "EVENT · 6 MYTHIC", name: "Black Clover" },
    },
  };
  const et = eventTexts[language] || eventTexts.uz;
  let events = [], pulling = false, bannerKey = "";

  // OWN_ODDS_ONLY are the events whose block shows their cards' odds alone,
  // not a Mythic's beside them: Demon Slayer's, as the owners asked, and
  // Black Clover's, set out as it is.
  const OWN_ODDS_ONLY = new Set(["demon-slayer", "black-clover"]);

  // eventFace is an event's own texts.
  const eventFace = (ev) => et[ev.key] || et.firefly;

  // eventsShown are the events running or coming, the newest last.
  const eventsShown = () => events.filter((ev) => ev.active || ev.soon);

  // eventFirst is the event to put forward: the newest running, else the
  // newest coming.
  function eventFirst() {
    const shown = eventsShown();
    const running = shown.filter((ev) => ev.active);
    return (running.length ? running : shown).at(-1);
  }

  // eventMark shows the event put forward's badge on the crystals' pill,
  // which opens the shop, while any runs or is coming, so that players
  // notice it.
  function eventMark() {
    const last = eventFirst();
    $("eventBadge").hidden = !last;
    if (last) $("eventBadge").textContent = et.badge(eventFace(last).icon);
    eventBanner();
  }

  // eventBanner shows the newest event at the top of the first pages, the
  // collection and the draw, while it runs or is coming; a tap opens the
  // shop at it.
  $("eventBannerGo").textContent = et.go;
  function eventBanner() {
    const ev = eventFirst();
    const on = !!ev && (opened === "collection" || opened === "draw");
    $("eventBanner").hidden = !on;
    if (!on) return;
    const face = eventFace(ev);
    bannerKey = ev.key;
    $("eventBanner").dataset.key = ev.key;
    $("eventBannerTag").textContent = face.tag;
    $("eventBannerTitle").textContent = face.title;
    const image = ev.cards[0] && ev.cards[0].image;
    if (image && $("eventBannerArt").dataset.src !== image) {
      $("eventBannerArt").dataset.src = image;
      picture($("eventBannerArt"), image, true);
    }
    $("eventBannerLeft").textContent = ev.box.querySelector(".event-ends").textContent;
  }
  $("eventBanner").addEventListener("click", () => {
    haptic("light");
    showTab("shop");
    setTimeout(() => {
      const box = $("events").querySelector(`[data-key="${bannerKey}"]`);
      if (box) box.scrollIntoView({ behavior: "smooth", block: "start" });
    }, 300);
  });

  // eventBlock asks for the events and shows each while it runs or is
  // coming; the app asks once it has loaded, for the badge, and again
  // whenever the shop opens.
  async function eventBlock() {
    let data;
    try {
      data = await api("events");
    } catch (_) {
      return;
    }
    const now = Date.now();
    events = (data.events || []).map((ev) => ({ ...ev, endsAt: now + (ev.ends_in || 0), startsAt: now + (ev.starts_in || 0) }));
    $("events").replaceChildren(...events.map(eventSection));
    eventLeft();
    eventMark();
  }

  // eventSection is an event's block in the shop, from the template.
  function eventSection(ev) {
    const face = eventFace(ev);
    const box = $("eventTemplate").content.firstElementChild.cloneNode(true);
    box.dataset.key = ev.key;
    box.classList.toggle("many", ev.cards.length > 1);
    box.hidden = !(ev.active || ev.soon);
    ev.box = box;
    eventArt(ev);
    box.querySelector(".event-tag").textContent = face.tag;
    box.querySelector(".event-title").textContent = face.title;
    const percent = (n) => `${n / 10}%`;
    // an event's cards' odds, beside a Mythic's but for the events that show
    // their own alone
    const mythic = ev.odds[4], own = percent(Math.round(mythic * ev.share));
    // an event whose cards come at any rarity shows how often a pull is one
    // of them, and how often its sure card
    const round = (n) => `${Math.round(n) / 10}%`;
    box.querySelector(".event-odds").textContent = ev.at_rarity
      ? `${face.icon} ${face.name} ${round(ev.event_odds)} · 🔥 ${face.sure} ${round(ev.sure_odds)}`
      : OWN_ODDS_ONLY.has(ev.key) ? `${face.icon} ${face.name} ${own}` : et.odds(percent(mythic), face.icon, face.name, own);
    box.querySelector(".event-one").addEventListener("click", () => eventPull(ev, 1));
    box.querySelector(".event-batch").addEventListener("click", () => eventPull(ev, ev.batch));
    box.querySelector(".event-big").addEventListener("click", () => eventPull(ev, ev.big_batch || 50));
    eventPity(ev);
    return box;
  }

  // eventArt draws an event's cards in full; of several, those the player
  // holds keep their tick.
  // An event of many cards shows its six rarest, which the server lists
  // first.
  function eventArt(ev) {
    const tiles = ev.cards.slice(0, 6).map((card) => {
      const tile = previewTile(card, 1, ev.once ? (ev.owned ? 1 : 0) : card.count);
      const mark = tile.querySelector(".owned");
      if (mark && (ev.once || !card.count)) mark.remove();
      return tile;
    });
    ev.box.querySelector(".event-art").replaceChildren(...tiles);
  }

  function eventPity(ev) {
    const face = eventFace(ev);
    ev.box.querySelector(".event-pity-text").textContent = ev.owned ? face.owned : et.pity(face.sure || face.name, ev.pity, ev.pity_max);
    ev.box.querySelector(".event-pity-bar").style.width = ev.owned ? "100%" : `${Math.min(100, (ev.pity / ev.pity_max) * 100)}%`;
    for (const [part, n] of [[".event-one", 1], [".event-batch", ev.batch], [".event-big", ev.big_batch || 50]]) {
      const button = ev.box.querySelector(part);
      button.textContent = et.open(n, ev.price);
      button.disabled = pulling || !ev.active;
    }
  }

  // eventLeft counts down to each event's start or end, and hides one that
  // has ended.
  function eventLeft() {
    for (const ev of events) {
      const ends = ev.box.querySelector(".event-ends");
      if (ev.soon) {
        const wait = ev.startsAt - Date.now();
        if (wait <= 0) { eventBlock(); return; }
        const m = Math.ceil(wait / 60000);
        ends.textContent = et.starts(Math.floor(m / 60), m % 60);
        continue;
      }
      if (!ev.active) continue;
      const left = ev.endsAt - Date.now();
      if (left <= 0) { ev.box.hidden = true; ev.active = false; eventMark(); continue; }
      const minutes = Math.floor(left / 60000);
      ends.textContent = et.left(Math.floor(minutes / 1440), Math.floor((minutes % 1440) / 60), minutes % 60);
    }
    eventBanner();
  }
  setInterval(eventLeft, 30000);

  // eventPull pulls n of an event's cards for tokens, one shown large as a
  // card opened is, more laid out as a batch is.
  async function eventPull(ev, n) {
    if (pulling || !ev.active) return;
    if (tokens < n * ev.price) { toast(et.short(n * ev.price - tokens)); return; }
    pulling = true;
    events.forEach(eventPity);
    haptic("medium");
    const face = eventFace(ev);
    try {
      const data = await api("event_open", { event: ev.key, count: n });
      if (data.refused === "soon") { toast(et.soon); return; }
      if (data.refused === "over") { toast(et.over); ev.box.hidden = true; ev.active = false; eventMark(); return; }
      if (data.refused === "funds") { tokens = data.tokens ?? tokens; toast(et.short(data.short)); return; }
      tokens = data.tokens ?? tokens;
      shards = data.shards ?? shards;
      points = data.points ?? points;
      ev.pity = data.pity;
      const won = data.drawn.filter((d) => d.event);
      for (const d of won) {
        const card = ev.cards.find((c) => c.id === d.card.id);
        if (card) card.count = d.card.count;
      }
      if (ev.once && won.length) ev.owned = true;
      if (won.length) eventArt(ev);
      if (data.given) toast(t.given(data.given));
      const drawn = data.drawn.map((d) => ({ card: d.card, fresh: d.new, serial: d.serial }));
      for (const { card } of drawn) keep(card);
      header();
      shelves();
      await celebrateMythics(drawn);
      if (drawn.length > 1) {
        openedPanel(drawn, et.pulled(face.icon, drawn.length));
      } else {
        const { card, fresh } = drawn[0];
        open(card);
        const note = document.createElement("p");
        note.className = "result";
        note.replaceChildren(...withTiers(fresh ? `${t.fresh} · ${t.plus(spaced(card.value))}` : `${t.again(copies(card))} · ${t.noPoints}`));
        $("sheetCard").prepend(note);
      }
      const prized = won.filter((d) => d.prize ?? true);
      if (prized.length) { haptic("heavy"); toast(et.got(face.icon, prized[0].card.name)); }
    } catch (error) {
      notice(error);
    } finally {
      pulling = false;
      events.forEach(eventPity);
      drawButton();
    }
  }

  // ---------- daily quests ----------

  // The daily quests: four a day, new at midnight in Tashkent, 10 tokens in
  // all. The server counts them as the actions happen; the page lists them
  // in a sheet and claims each one's tokens. The state says how many are
  // done and ready, for the entry at the top of the collection and its dot.
  const questTexts = {
    uz: {
      title: "🎯 Kunlik topshiriqlar", entry: "Kunlik topshiriqlar", claim: "Olish",
      names: { open: (n) => `${n} ta karta oching`, arena: () => "Arenada hujum qilib yuting", fuse: () => "Kartani 1 yulduzga ko'taring", event: () => "Eventda 1 ta karta oching", market: () => "Bozorda 1 marta almashing" },
      reset: (h, m) => (h ? `Yangilanishiga ${h} soat ${m} daqiqa` : `Yangilanishiga ${m} daqiqa`),
      line: (n) => (n ? `🎁 ${n} ta mukofot tayyor` : "Har kuni 4 ta topshiriq · 10 🎟"),
      allDone: "✅ Bugungi topshiriqlar bajarildi. Ertaga yangilari keladi!",
      got: (n) => `🎟 +${n} token olindi`,
      refused: { not_done: "Topshiriq hali bajarilmagan", claimed: "Bu mukofot allaqachon olingan" },
    },
    ru: {
      title: "🎯 Ежедневные задания", entry: "Ежедневные задания", claim: "Забрать",
      names: { open: (n) => `Откройте ${n} карт`, arena: () => "Победите в атаке на арене", fuse: () => "Повысьте карту на 1 звезду", event: () => "Откройте 1 карту ивента", market: () => "Совершите 1 обмен на рынке" },
      reset: (h, m) => (h ? `Обновление через ${h} ч. ${m} мин.` : `Обновление через ${m} мин.`),
      line: (n) => (n ? `🎁 Готово наград: ${n}` : "4 задания в день · 10 🎟"),
      allDone: "✅ Задания на сегодня выполнены. Завтра будут новые!",
      got: (n) => `🎟 +${n} токенов`,
      refused: { not_done: "Задание ещё не выполнено", claimed: "Награда уже получена" },
    },
    en: {
      title: "🎯 Daily quests", entry: "Daily quests", claim: "Claim",
      names: { open: (n) => `Open ${n} cards`, arena: () => "Win an arena attack", fuse: () => "Raise a card by 1 star", event: () => "Open 1 event card", market: () => "Make 1 market trade" },
      reset: (h, m) => (h ? `New quests in ${h} h ${m} min` : `New quests in ${m} min`),
      line: (n) => (n ? `🎁 ${n} ${n === 1 ? "reward" : "rewards"} ready` : "4 quests a day · 10 🎟"),
      allDone: "✅ All of today's quests are done. New ones come tomorrow!",
      got: (n) => `🎟 +${n} tokens`,
      refused: { not_done: "This quest is not done yet", claimed: "This reward was claimed already" },
    },
  };
  const qt = questTexts[language] || questTexts.uz;
  const QUEST_ICON = { open: "🎴", arena: "⚔️", fuse: "⭐", event: "🌸", market: "🔄" };
  let questSummary = null, questList = null, questResetAt = 0, claiming = false;

  // questShow keeps what the server said of the day's quests: the counts,
  // the time to new ones and, from "quests", the list itself.
  function questShow(data) {
    questSummary = { done: data.done, ready: data.ready, total: data.total };
    questResetAt = Date.now() + (data.reset_in || 0);
    if (data.quests) questList = data.quests;
    questEntry();
  }

  // questEntry shows the quests' entry on the collection: how many are
  // done, and a dot while a reward waits.
  $("questEntryTitle").textContent = qt.entry;
  function questEntry() {
    const on = !!questSummary && opened === "collection";
    $("questEntry").hidden = !on;
    if (!on) return;
    $("questEntryCount").textContent = `${questSummary.done}/${questSummary.total}`;
    $("questEntryLine").textContent = qt.line(questSummary.ready);
    $("questDot").hidden = !questSummary.ready;
    $("questEntry").classList.toggle("ready", questSummary.ready > 0);
  }
  $("questEntry").addEventListener("click", () => { haptic("light"); openQuests(); });

  async function openQuests() {
    try {
      questShow(await api("quests"));
    } catch (error) {
      notice(error);
      return;
    }
    showPanel(questPanel());
  }

  // questPanel lists the day's quests: each one's icon, name, progress and
  // reward, and its claim button once done.
  function questPanel() {
    const panel = document.createElement("div");
    panel.className = "quests";
    panel.innerHTML = `<div class="quests-head"><h2 class="shop-title"></h2><b class="quests-done"></b></div><p class="quests-reset"></p><ul class="quest-list"></ul>`;
    panel.querySelector(".shop-title").textContent = qt.title;
    panel.querySelector(".quests-done").textContent = `${questSummary.done}/${questSummary.total}`;
    const list = panel.querySelector(".quest-list");
    for (const q of questList) {
      const li = document.createElement("li");
      li.className = "quest" + (q.claimed ? " claimed" : q.done ? " done" : "");
      li.innerHTML = `<span class="quest-icon" aria-hidden="true"></span><span class="quest-body"><b></b><span class="quest-bar"><span class="progress"><i></i></span><small></small></span></span><span class="quest-side"><em></em></span>`;
      li.querySelector(".quest-icon").textContent = QUEST_ICON[q.id] || "🎯";
      li.querySelector("b").textContent = qt.names[q.id] ? qt.names[q.id](q.target) : q.id;
      li.querySelector(".progress i").style.width = `${Math.min(100, (q.progress / q.target) * 100)}%`;
      li.querySelector("small").textContent = `${q.progress}/${q.target}`;
      li.querySelector("em").textContent = `+${q.reward} 🎟`;
      const side = li.querySelector(".quest-side");
      if (q.claimed) {
        const check = document.createElement("span");
        check.className = "quest-check";
        check.textContent = "✅";
        side.append(check);
      } else if (q.done) {
        const button = document.createElement("button");
        button.type = "button";
        button.className = "draw small";
        button.textContent = qt.claim;
        button.addEventListener("click", () => claimQuest(q, button));
        side.append(button);
      }
      list.append(li);
    }
    if (questList.length && questList.every((q) => q.claimed)) {
      const all = document.createElement("p");
      all.className = "quests-all";
      all.textContent = qt.allDone;
      panel.append(all);
    }
    questTick(panel);
    return panel;
  }

  // claimQuest takes a quest's tokens; a claim refused says why, and the
  // list is asked for again.
  async function claimQuest(q, button) {
    if (claiming) return;
    claiming = true;
    button.disabled = true;
    try {
      const data = await api("quest_claim", { quest: q.id });
      if (data.refused) {
        toast(qt.refused[data.refused] || t.unavailable);
        await openQuests();
        return;
      }
      tokens = data.tokens ?? tokens;
      questShow(data);
      drawButton();
      showPanel(questPanel());
      toast(qt.got(data.claimed));
      haptic("medium");
    } catch (error) {
      notice(error);
    } finally {
      claiming = false;
      button.disabled = false;
    }
  }

  // questTick counts down to new quests in the open list, and at midnight
  // asks for the new day's.
  function questTick(panel) {
    const box = (panel || $("sheetCard")).querySelector(".quests-reset");
    if (questResetAt && Date.now() >= questResetAt) {
      questResetAt = 0;
      api("quests").then((data) => {
        questShow(data);
        if (!$("sheet").hidden && $("sheetCard").querySelector(".quests")) showPanel(questPanel());
      }).catch(() => {});
      return;
    }
    if (!box || !questResetAt) return;
    const m = Math.floor((questResetAt - Date.now()) / 60000);
    box.textContent = `⏳ ${qt.reset(Math.floor(m / 60), m % 60)}`;
  }
  setInterval(() => questTick(), 30000);

  // ---------- arena ----------

  // The arena: a saved team of five, two in front and three behind, fights
  // another player's saved team. The server plays the battle and keeps the
  // ratings; the page only replays the events it gives. Each attack takes a
  // ticket, eight at most, one back every hour, and a win gives a token while
  // the day's three last. The history lists the last ten battles.
  const arenaTexts = {
    uz: {
      classes: { tank: "Tank", fighter: "Jangchi", mage: "Sehrgar" },
      classInfo: { tank: "Joni eng ko'p: oldda turib zarbalarni o'ziga oladi", fighter: "Eng yaqin raqibni uradi, avval old qatorni", mage: "Eng tez: orqa qatordagi eng zaif raqibni uradi" },
      beats: (c) => `${c}ni yengadi`, weakTo: (c) => `${c}dan yengiladi`, statPower: "Kuch",
      starsLine: (s, m, top) => (s < 5 ? `★${s}: kuch ×${m} · ★5 da ×${top}` : `★5: kuch ×${m}`),
      skillTitle: (n) => `Skill: ${n}`, noSkillTitle: "Skill: yo'q", noSkill: "Skill Legendar va Mifik kartalarda bo'ladi",
      animeNote: "Jamoada shu animedan 2 ta: +10%, 3 ta va ko'p: +20% hujum va jon",
      flipHint: "Kartani bosib aylantiring",
      frontNote: "zarbani birinchi oladi", backNote: "sehrgarlar shu yerni uradi", addCard: "Karta qo'shing",
      rules: "Qoidalar", ruleClasses: "Sinflar", ruleTriangle: "Strelka: kim kimni 25% kuchliroq uradi", ruleSkills: "Skillar · Legendar va Mifik kartalarda",
      ruleTickets: (m) => `🎫 Har hujum 1 tiket oladi: ${m} tagacha, har soatda 1 ta qaytadi`, ruleTokens: (n) => `🎟 G'alaba 1 karta tokeni beradi: kuniga ${n} tagacha`,
      ruleRating: "🏅 G'alaba reytingni oshiradi, mag'lubiyat tushiradi. Karta yo'qolmaydi", ruleWeek: "🏆 Har dushanba haftaning top 100 tasi token oladi", toTable: "Reytingni ko'rish →",
      foeTeam: "Jamoasini ko'rish", noFoeTeam: "Jamoasi hali saqlanmagan", them: "Raqib",
      synergyNone: "🔗 Bir animedan 2 ta karta qo'ysangiz: +10%, 3 ta: +20%", synergyOn: (a, n, p) => `🔗 ${a} ×${n}: +${p}%`,
      rating: "Arena reytingi", record: (w, l) => `${w} g'alaba · ${l} mag'lubiyat`,
      tickets: (n, m) => `🎫 Arena tiketlari: ${n}/${m}`, nextTicket: (min) => (min >= 60 ? "· keyingisi 1 soatda" : `· keyingisi ${min} daqiqada`), tokens: (n, m) => `🎟 G'alaba tokenlari: ${n}/${m}`,
      histEmpty: "Hali jang bo'lmagan", histAttack: "⚔️ Hujum", histDefense: "🛡️ Himoya", histWon: "G'alaba", histLost: "Mag'lubiyat",
      front: "Old qator", back: "Orqa qator", power: (n) => `⚡ ${n}`,
      pick: "5 ta karta tanlang: 2 tasi oldinda, 3 tasi orqada. Yulduzlar kartani kuchaytiradi.",
      unsaved: "O'zgarish hali saqlanmadi", savedHint: "Jamoa saqlangan. Almashtirish uchun uyachani bosing.",
      slot: (row, n) => `${row} · ${n}-o'rin`, inTeam: "jamoada", saved: "✅ Jamoa saqlandi",
      attack: "⚔️ Hujum", noFoes: "Hozircha raqib yo'q: boshqalar ham jamoa saqlasin", teamFirst: "Hujum uchun avval jamoani saqlang", noAttacks: "Tiketlar tugadi: har soatda 1 ta qo'shiladi",
      round: (n) => `Raund ${n}`, skip: "O'tkazib yuborish", won: "🏆 G'alaba!", lost: "💀 Mag'lubiyat", change: (d) => `Reyting ${d >= 0 ? "+" : "−"}${Math.abs(d)}`, token: "🎟 +1 token",
      week: "🏆 Haftalik mukofot", weekRule: "Har dushanba 08:00 da: hafta davomida kamida 1 marta hujum qilgan top 100 ga token pochtaga keladi", weekEvent: "🎉 Event: bu hafta top 5 ga almaz box ham beriladi", weekPlace: (n, p, b) => (n ? `Siz hozir ${n}-o'rindasiz${p ? ` · +${p} 🎟` : ""}${b ? ` · +${b} 🎁` : ""}` : "Mukofot uchun bu hafta kamida 1 marta hujum qiling"), weekEnds: (d, h) => (d ? `Hafta tugashiga ${d} kun ${h} soat` : `Hafta tugashiga ${h} soat`), weekRow: (a, b) => (a === b ? `${a}-o'rin` : `${a}–${b}`),
      you: "Siz", topEmpty: "Arenada hali hech kim jang qilmagan", topRow: (w, l, p) => `${w} G · ${l} M · ⚡${p}`,
      skills: { strike: "Kuchli zarba", heal: "Davolash", shield: "Qalqon", stun: "Hayratlantirish", poison: "Zahar", provoke: "Chaqiriq" }, fell: (n) => `💀 ${n} yiqildi`,
      skillInfo: { strike: "energiya to'lganda 1.8 barobar zarba", heal: "energiya to'lganda eng zaif yarador ittifoqchiga jonining 80% igacha qaytaradi", shield: "energiya to'lganda o'ziga va eng zaif ittifoqchiga jonining 45% i qalqon", stun: "energiya to'lganda urib, raqibning keyingi yurishini o'tkazdiradi", poison: "energiya to'lganda urib zaharlaydi: 3 yurish davomida 15% jon", provoke: "energiya to'lganda 3 raund raqib tank va jangchilarini o'ziga tortadi va zarbani 30% kam oladi" },
      tower: {
        best: "Eng baland qavat", floorN: (n) => `${n}-qavat`, none: "hali yo'q",
        theme: (a, p) => `🎌 Bu hafta: ${a} +${p}%`, themeHint: "Shu anime kartalaringiz minorada kuchliroq: hujum va jon", noTheme: "Bu hafta mavzu yo'q",
        enemy: (n) => `🗼 ${n}-qavat`, reward: (c, k) => `Birinchi o'tish: +${c} 🪙` + (k ? ` · +${k} 🎟` : ""), fight: (n) => `⚔️ ${n}-qavatga jang`,
        noTeam: "Avval Arena bo'limida jamoa saqlang",
        rule: () => `G'alaba keyingi qavatni ochadi va mukofot beradi, mag'lubiyat hech narsa bermaydi. Har 5-qavat +1 🎟, har 10-qavat +3 🎟`,
        cleared: (n) => `🗼 ${n}-qavat o'tildi!`, failed: (n) => `${n}-qavat o'tilmadi`, tokens: (n) => `🎟 +${n} token`,
        refused: { no_team: "Avval Arena bo'limida jamoa saqlang", no_floor: "Bu qavat hali tayyor emas", moved: "Bu qavat allaqachon o'tildi, sahifa yangilandi" },
      },
      coins: (n) => `🪙 ${n}`, coinsWon: (n) => `+${n} 🪙`, ruleCoins: (w, l) => `🪙 Har hujum tanga beradi: g'alaba +${w}, mag'lubiyat +${l}. Tangalar Arena do'konida sarflanadi`,
      shopHint: (w, l) => `Har hujum: g'alaba +${w} 🪙, mag'lubiyat +${l} 🪙`, shopToken: "1 ta 🎟 token", shopTicket: "1 ta 🎫 arena tiketi", shopTicketHint: (n, m) => (n >= m ? `Tiketlar to'la: ${n}/${m}` : `Hozir ${n}/${m}, hujum uchun`), shopTokenHint: (left, d) => `Bugun yana ${left} ta (kuniga ${d} tagacha)`,
      shopRefused: { coins: "Tanga yetmaydi: arenada hujum qiling", daily: "Bugungi tokenlar olindi: ertaga yana", tickets_full: "Tiketlar to'la: avval hujum qiling" },
      ruleEnergy: "🔋 Energiya: har yurish +34, zarba yeganda +10. 100 ga to'lganda karta skillini ishlatadi va energiya 0 ga tushadi",
      synergy: "🔗 Bir animedan 2 ta karta: +10% hujum va jon, 3 ta va ko'p: +20%", edgeRule: "⚡ Ustunlik: 🛡️ tank ⚔️ jangchidan, ⚔️ jangchi 🔮 sehrgardan, 🔮 sehrgar 🛡️ tankdan 25% kuchliroq urar", edge: "ustun",
      stunned: (n) => `💫 ${n} hayratda: yurish o'tdi`, poisoned: (n, d) => `☠ ${n} zahardan −${d}`,
      refused: { size: "Jamoada 5 ta karta bo'lishi kerak", repeat: "Bitta kartani ikki marta qo'yib bo'lmaydi", not_held: "Jamoadagi karta endi sizda yo'q", no_tickets: "Tiket qolmadi: har soatda 1 ta qo'shiladi", no_team: "Avval jamoani saqlang: undagi karta endi sizda yo'q", opponent_gone: "Bu raqib endi yo'q, ro'yxat yangilandi", self: "O'zingizga hujum qilib bo'lmaydi" },
    },
    ru: {
      classes: { tank: "Танк", fighter: "Боец", mage: "Маг" },
      classInfo: { tank: "Больше всех здоровья: стоит впереди и принимает удары", fighter: "Бьёт ближайшего соперника, сначала передний ряд", mage: "Самый быстрый: бьёт самого слабого в заднем ряду" },
      beats: (c) => `сильнее: ${c}`, weakTo: (c) => `слабее: ${c}`, statPower: "Сила",
      starsLine: (s, m, top) => (s < 5 ? `★${s}: сила ×${m} · на ★5 ×${top}` : `★5: сила ×${m}`),
      skillTitle: (n) => `Навык: ${n}`, noSkillTitle: "Навык: нет", noSkill: "Навыки есть у Легендарных и Мифических карт",
      animeNote: "2 карты этого аниме в команде: +10%, 3 и больше: +20% атаки и здоровья",
      flipHint: "Нажмите на карту, чтобы перевернуть",
      frontNote: "принимает удары первым", backNote: "сюда бьют маги", addCard: "Добавить карту",
      rules: "Правила", ruleClasses: "Классы", ruleTriangle: "Стрелка: кто кого бьёт на 25% сильнее", ruleSkills: "Навыки · у Легендарных и Мифических",
      ruleTickets: (m) => `🎫 Атака стоит 1 билет: до ${m}, каждый час возвращается 1`, ruleTokens: (n) => `🎟 Победа даёт токен на карту: до ${n} в день`,
      ruleRating: "🏅 Победа поднимает рейтинг, поражение снижает. Карты не теряются", ruleWeek: "🏆 Каждый понедельник топ 100 недели получают токены", toTable: "Смотреть рейтинг →",
      foeTeam: "Посмотреть команду", noFoeTeam: "Команда ещё не сохранена", them: "Соперник",
      synergyNone: "🔗 2 карты одного аниме: +10%, 3: +20%", synergyOn: (a, n, p) => `🔗 ${a} ×${n}: +${p}%`,
      rating: "Рейтинг арены", record: (w, l) => `${w} побед · ${l} поражений`,
      tickets: (n, m) => `🎫 Билеты арены: ${n}/${m}`, nextTicket: (min) => (min >= 60 ? "· следующий через 1 ч" : `· следующий через ${min} мин`), tokens: (n, m) => `🎟 Токенов за победы: ${n}/${m}`,
      histEmpty: "Боёв ещё не было", histAttack: "⚔️ Атака", histDefense: "🛡️ Защита", histWon: "Победа", histLost: "Поражение",
      front: "Передний ряд", back: "Задний ряд", power: (n) => `⚡ ${n}`,
      pick: "Выберите 5 карт: 2 впереди, 3 сзади. Звёзды усиливают карту.",
      unsaved: "Изменения ещё не сохранены", savedHint: "Команда сохранена. Нажмите на ячейку, чтобы заменить карту.",
      slot: (row, n) => `${row} · место ${n}`, inTeam: "в команде", saved: "✅ Команда сохранена",
      attack: "⚔️ Атака", noFoes: "Пока нет соперников: пусть другие тоже сохранят команду", teamFirst: "Чтобы атаковать, сохраните команду", noAttacks: "Билеты закончились: каждый час добавляется 1",
      round: (n) => `Раунд ${n}`, skip: "Пропустить", won: "🏆 Победа!", lost: "💀 Поражение", change: (d) => `Рейтинг ${d >= 0 ? "+" : "−"}${Math.abs(d)}`, token: "🎟 +1 токен",
      week: "🏆 Награда недели", weekRule: "Каждый понедельник в 08:00: топ 100, атаковавшие на неделе хотя бы раз, получают токены почтой", weekEvent: "🎉 Ивент: на этой неделе топ 5 получат ещё и алмазные боксы", weekPlace: (n, p, b) => (n ? `Сейчас вы на ${n}-м месте${p ? ` · +${p} 🎟` : ""}${b ? ` · +${b} 🎁` : ""}` : "Чтобы получить награду, атакуйте хотя бы раз на этой неделе"), weekEnds: (d, h) => (d ? `До конца недели ${d} дн ${h} ч` : `До конца недели ${h} ч`), weekRow: (a, b) => (a === b ? `${a} место` : `${a}–${b}`),
      you: "Вы", topEmpty: "На арене ещё никто не сражался", topRow: (w, l, p) => `${w} В · ${l} П · ⚡${p}`,
      skills: { strike: "Сильный удар", heal: "Лечение", shield: "Щит", stun: "Оглушение", poison: "Яд", provoke: "Провокация" }, fell: (n) => `💀 ${n} пал(а)`,
      skillInfo: { strike: "при полной энергии удар ×1.8", heal: "при полной энергии возвращает самому слабому раненому союзнику до 80% здоровья", shield: "при полной энергии щит в 45% здоровья себе и самому слабому союзнику", stun: "при полной энергии бьёт, и соперник пропускает следующий ход", poison: "при полной энергии бьёт и отравляет: 15% здоровья 3 хода", provoke: "при полной энергии 3 раунда притягивает танков и бойцов соперника и получает на 30% меньше урона" },
      tower: {
        best: "Высший этаж", floorN: (n) => `Этаж ${n}`, none: "пока нет",
        theme: (a, p) => `🎌 На этой неделе: ${a} +${p}%`, themeHint: "Ваши карты этого аниме в башне сильнее: атака и здоровье", noTheme: "На этой неделе темы нет",
        enemy: (n) => `🗼 Этаж ${n}`, reward: (c, k) => `За первое прохождение: +${c} 🪙` + (k ? ` · +${k} 🎟` : ""), fight: (n) => `⚔️ Бой за этаж ${n}`,
        noTeam: "Сначала сохраните команду в разделе Арена",
        rule: () => `Победа открывает следующий этаж и даёт награду, поражение ничего не даёт. Каждый 5-й этаж +1 🎟, каждый 10-й +3 🎟`,
        cleared: (n) => `🗼 Этаж ${n} пройден!`, failed: (n) => `Этаж ${n} не пройден`, tokens: (n) => `🎟 +${n} токен.`,
        refused: { no_team: "Сначала сохраните команду в разделе Арена", no_floor: "Этот этаж ещё не готов", moved: "Этот этаж уже пройден, страница обновлена" },
      },
      coins: (n) => `🪙 ${n}`, coinsWon: (n) => `+${n} 🪙`, ruleCoins: (w, l) => `🪙 Каждая атака даёт монеты: победа +${w}, поражение +${l}. Их тратят в магазине арены`,
      shopHint: (w, l) => `За атаку: победа +${w} 🪙, поражение +${l} 🪙`, shopToken: "1 🎟 токен", shopTicket: "1 🎫 билет арены", shopTicketHint: (n, m) => (n >= m ? `Билеты полные: ${n}/${m}` : `Сейчас ${n}/${m}, для атаки`), shopTokenHint: (left, d) => `Сегодня ещё ${left} (до ${d} в день)`,
      shopRefused: { coins: "Не хватает монет: атакуйте на арене", daily: "Токены на сегодня куплены: завтра снова", tickets_full: "Билеты полные: сначала атакуйте" },
      ruleEnergy: "🔋 Энергия: +34 за каждый ход, +10 за полученный удар. На 100 карта применяет навык, и энергия падает до 0",
      synergy: "🔗 2 карты из одного аниме: +10% атаки и здоровья, 3 и больше: +20%", edgeRule: "⚡ Преимущество: 🛡️ танк бьёт ⚔️ бойца, ⚔️ боец 🔮 мага, 🔮 маг 🛡️ танка на 25% сильнее", edge: "преимущество",
      stunned: (n) => `💫 ${n} оглушён(а): ход пропущен`, poisoned: (n, d) => `☠ ${n} от яда −${d}`,
      refused: { size: "В команде должно быть 5 карт", repeat: "Одну карту нельзя поставить дважды", not_held: "Карты из команды у вас больше нет", no_tickets: "Билетов нет: каждый час добавляется 1", no_team: "Сначала сохраните команду: карты из неё у вас больше нет", opponent_gone: "Этого соперника больше нет, список обновлён", self: "Нельзя атаковать себя" },
    },
    en: {
      classes: { tank: "Tank", fighter: "Fighter", mage: "Mage" },
      classInfo: { tank: "Most health: stands in front and takes the hits", fighter: "Hits the nearest enemy, the front row first", mage: "Fastest: hits the weakest enemy in the back row" },
      beats: (c) => `strong vs ${c}`, weakTo: (c) => `weak vs ${c}`, statPower: "Power",
      starsLine: (s, m, top) => (s < 5 ? `★${s}: power ×${m} · at ★5 ×${top}` : `★5: power ×${m}`),
      skillTitle: (n) => `Skill: ${n}`, noSkillTitle: "Skill: none", noSkill: "Legendary and Mythic cards have skills",
      animeNote: "2 cards of this anime in a team: +10%, 3 or more: +20% attack and health",
      flipHint: "Tap the card to turn it over",
      frontNote: "takes the hits first", backNote: "mages hit here", addCard: "Add a card",
      rules: "Rules", ruleClasses: "Classes", ruleTriangle: "Arrow: who hits whom 25% harder", ruleSkills: "Skills · on Legendary and Mythic cards",
      ruleTickets: (m) => `🎫 Each attack takes 1 ticket: up to ${m}, one back every hour`, ruleTokens: (n) => `🎟 A win gives a card token: up to ${n} a day`,
      ruleRating: "🏅 A win raises your rating, a loss lowers it. No card is lost", ruleWeek: "🏆 Every Monday the week's top 100 get tokens", toTable: "See the ranking →",
      foeTeam: "View team", noFoeTeam: "No team saved yet", them: "Opponent",
      synergyNone: "🔗 2 cards of one anime: +10%, 3: +20%", synergyOn: (a, n, p) => `🔗 ${a} ×${n}: +${p}%`,
      rating: "Arena rating", record: (w, l) => `${w} wins · ${l} losses`,
      tickets: (n, m) => `🎫 Arena tickets: ${n}/${m}`, nextTicket: (min) => (min >= 60 ? "· next in 1 hour" : `· next in ${min} min`), tokens: (n, m) => `🎟 Win tokens: ${n}/${m}`,
      histEmpty: "No battles yet", histAttack: "⚔️ Attack", histDefense: "🛡️ Defense", histWon: "Victory", histLost: "Defeat",
      front: "Front row", back: "Back row", power: (n) => `⚡ ${n}`,
      pick: "Pick 5 cards: 2 in front, 3 behind. Stars make a card stronger.",
      unsaved: "The change is not saved yet", savedHint: "Your team is saved. Tap a slot to swap a card.",
      slot: (row, n) => `${row} · slot ${n}`, inTeam: "in the team", saved: "✅ Team saved",
      attack: "⚔️ Attack", noFoes: "No opponents yet: others need to save a team too", teamFirst: "Save a team to attack", noAttacks: "No tickets left: one comes back every hour",
      round: (n) => `Round ${n}`, skip: "Skip", won: "🏆 Victory!", lost: "💀 Defeat", change: (d) => `Rating ${d >= 0 ? "+" : "−"}${Math.abs(d)}`, token: "🎟 +1 token",
      week: "🏆 Weekly prize", weekRule: "Every Monday at 08:00 the top 100 who attacked at least once that week get tokens by mail", weekEvent: "🎉 Event: this week the top 5 get diamond boxes too", weekPlace: (n, p, b) => (n ? `You are #${n} now${p ? ` · +${p} 🎟` : ""}${b ? ` · +${b} 🎁` : ""}` : "Attack at least once this week to earn a prize"), weekEnds: (d, h) => (d ? `Week ends in ${d}d ${h}h` : `Week ends in ${h}h`), weekRow: (a, b) => (a === b ? `#${a}` : `#${a}–${b}`),
      you: "You", topEmpty: "Nobody has fought in the arena yet", topRow: (w, l, p) => `${w} W · ${l} L · ⚡${p}`,
      skills: { strike: "Power strike", heal: "Heal", shield: "Shield", stun: "Stun", poison: "Poison", provoke: "Provoke" }, fell: (n) => `💀 ${n} fell`,
      skillInfo: { strike: "when energy is full, hits ×1.8", heal: "when energy is full, gives the weakest hurt ally back up to 80% of its health", shield: "when energy is full, shields itself and its weakest ally for 45% of their health", stun: "when energy is full, hits and the target skips its next turn", poison: "when energy is full, hits and poisons: 15% health for 3 turns", provoke: "when energy is full, draws enemy tanks and fighters for 3 rounds and takes 30% less damage" },
      tower: {
        best: "Highest floor", floorN: (n) => `Floor ${n}`, none: "none yet",
        theme: (a, p) => `🎌 This week: ${a} +${p}%`, themeHint: "Your cards of this anime are stronger in the tower: attack and health", noTheme: "No theme this week",
        enemy: (n) => `🗼 Floor ${n}`, reward: (c, k) => `First clear: +${c} 🪙` + (k ? ` · +${k} 🎟` : ""), fight: (n) => `⚔️ Fight floor ${n}`,
        noTeam: "Save a team in the Arena first",
        rule: () => `A win opens the next floor and gives its reward, a loss gives nothing. Every 5th floor +1 🎟, every 10th +3 🎟`,
        cleared: (n) => `🗼 Floor ${n} cleared!`, failed: (n) => `Floor ${n} not cleared`, tokens: (n) => `🎟 +${n} tokens`,
        refused: { no_team: "Save a team in the Arena first", no_floor: "This floor is not ready yet", moved: "This floor is cleared already; the page is refreshed" },
      },
      coins: (n) => `🪙 ${n}`, coinsWon: (n) => `+${n} 🪙`, ruleCoins: (w, l) => `🪙 Each attack gives coins: a win +${w}, a loss +${l}. Spend them in the arena shop`,
      shopHint: (w, l) => `Each attack: a win +${w} 🪙, a loss +${l} 🪙`, shopToken: "1 🎟 token", shopTicket: "1 🎫 arena ticket", shopTicketHint: (n, m) => (n >= m ? `Tickets full: ${n}/${m}` : `${n}/${m} now, for an attack`), shopTokenHint: (left, d) => `${left} more today (up to ${d} a day)`,
      shopRefused: { coins: "Not enough coins: attack in the arena", daily: "Today's tokens are bought: more tomorrow", tickets_full: "Tickets are full: attack first" },
      ruleEnergy: "🔋 Energy: +34 each turn, +10 for each hit taken. At 100 the card uses its skill and drops to 0",
      synergy: "🔗 2 cards of one anime: +10% attack and health, 3 or more: +20%", edgeRule: "⚡ Edge: 🛡️ tank beats ⚔️ fighter, ⚔️ fighter beats 🔮 mage, 🔮 mage beats 🛡️ tank, for 25% more", edge: "edge",
      stunned: (n) => `💫 ${n} is stunned: turn skipped`, poisoned: (n, d) => `☠ ${n} takes −${d} from poison`,
      refused: { size: "A team needs 5 cards", repeat: "A card cannot be placed twice", not_held: "You no longer hold a card of the team", no_tickets: "No tickets left: one comes back every hour", no_team: "Save a team first: you no longer hold one of its cards", opponent_gone: "This opponent is gone; the list is refreshed", self: "You cannot attack yourself" },
    },
  };
  const at = arenaTexts[language] || arenaTexts.uz;
  const CLASS_ICON = { tank: "🛡️", fighter: "⚔️", mage: "🔮" };
  const SKILL_ICON = { strike: "💥", heal: "💚", shield: "🔰", stun: "💫", poison: "☠", provoke: "😤" };
  // SKILLS is the skills by card id, id % 6 picking one, as arena.FighterOf does.
  const SKILLS = ["strike", "heal", "shield", "stun", "poison", "provoke"];
  const EMPTY_TEAM = [0, 0, 0, 0, 0];
  let arenaData = null, arenaDraft = EMPTY_TEAM.slice(), arenaBusy = false, ticketAt = 0;

  // fighterOf is a held card as the arena draws it, its class and skill
  // the hand-picked role the server sends or worked out as arena.FighterOf does, for a team not saved yet; a saved
  // team's come from the server.
  function fighterOf(card) {
    const cls = card.class || (card.health >= 4 * card.attack ? "tank" : card.health <= 3 * card.attack ? "mage" : "fighter");
    const skill = card.rarity >= 3 ? card.skill || SKILLS[((card.id % 6) + 6) % 6] : "";
    return { id: card.id, name: card.name, image: card.image, rarity: card.rarity, stars: card.stars || 1, attack: card.attack || 0, health: card.health || 0, class: cls, skill, anime: franchise(card.anime || "") };
  }
  // withSynergy raises the team's cards of one anime as the server does:
  // a pair by 10%, three or more by 20%, rounded the same in whole numbers.
  // A fighter from the server has its synergy already and keeps it.
  function withSynergy(team) {
    const count = {};
    for (const f of team) if (f && f.anime) count[f.anime] = (count[f.anime] || 0) + 1;
    const raised = (n, p) => Math.floor((n * (100 + p) + 50) / 100);
    return team.map((f) => {
      if (!f || f.synergy !== undefined) return f;
      const n = f.anime ? count[f.anime] : 0;
      const synergy = n >= 3 ? 20 : n === 2 ? 10 : 0;
      return { ...f, synergy, attack: raised(f.attack, synergy), health: raised(f.health, synergy) };
    });
  }
  // powerOf is a team's power as arena.Team.Power counts it.
  const powerOf = (team) => team.reduce((sum, f) => sum + (f ? f.attack + Math.floor(f.health / 4) : 0), 0);

  // slotFighter is the card in a slot of the team being made, from the
  // cards held now, so that a fusion since shows at once.
  function slotFighter(id) {
    if (!id) return null;
    const card = cards.find((c) => c.id === id && c.count > 0);
    if (card) return fighterOf(card);
    return ((arenaData && arenaData.team) || []).find((f) => f.id === id) || null;
  }

  // fighterTile is a card in the arena: its picture, name, class and stars.
  function fighterTile(f) {
    const tile = document.createElement("div");
    tile.className = "fighter" + (f.rarity >= 3 ? " foil" : "");
    tile.style.setProperty("--c", COLORS[f.rarity]);
    tile.innerHTML = '<img alt=""><span class="f-chips" aria-hidden="true"><span class="f-class"></span></span><span class="f-name"></span>';
    picture(tile.querySelector("img"), f.image);
    tile.querySelector(".f-class").textContent = CLASS_ICON[f.class] || "";
    if (f.skill) tile.querySelector(".f-chips").insertAdjacentHTML("beforeend", `<span class="f-skill">${SKILL_ICON[f.skill]}</span>`);
    tile.querySelector(".f-name").textContent = f.name;
    if (f.stars > 1) tile.insertAdjacentHTML("beforeend", `<span class="f-stars">★${f.stars}</span>`);
    if (f.synergy) tile.insertAdjacentHTML("beforeend", `<span class="f-syn">🔗+${f.synergy}%</span>`);
    if (f.theme) tile.insertAdjacentHTML("beforeend", `<span class="f-theme">🎌+${f.theme}%</span>`);
    return tile;
  }

  // arenaPage loads the arena; a refresh sends the opponents shown, so that
  // others come in their place.
  async function arenaPage(refresh) {
    try {
      const shown = refresh && arenaData ? (arenaData.opponents || []).map((o) => o.id) : [];
      arenaShow(await api("arena", { shown }));
      notice(null);
    } catch (error) {
      notice(error);
    }
  }

  // arenaShow lays out the arena as the server gives it; a team null (none
  // saved, or one with a card no longer held) is to be picked again.
  function arenaShow(data) {
    arenaData = data;
    arenaDraft = data.team ? data.team.map((f) => f.id) : EMPTY_TEAM.slice();
    ticketAt = Date.now() + (data.player.next_ticket_in || 0);
    arenaGuide();
    arenaMe();
    arenaTeam();
    arenaFoes();
    arenaShopList();
    arenaHistory();
  }

  // arenaGuide is the rules, folded: the classes' triangle, the synergy,
  // the six skills, tickets, tokens and the week's prizes.
  function arenaGuide() {
    $("arenaGuideTitle").textContent = `${at.rules} ⓘ`;
    const p = arenaData.player;
    const body = $("arenaGuideBody");
    body.textContent = "";
    const add = (tag, cls, text) => { const el = document.createElement(tag); if (cls) el.className = cls; if (text) el.textContent = text; body.append(el); return el; };
    add("b", "guide-title", at.ruleClasses);
    body.append(triangle());
    add("small", "guide-caption", at.ruleTriangle);
    const classes = add("ul", "guide-list");
    for (const c of ["tank", "fighter", "mage"]) {
      const li = document.createElement("li");
      li.innerHTML = '<span aria-hidden="true"></span><p><b></b> </p>';
      li.querySelector("span").textContent = CLASS_ICON[c];
      li.querySelector("b").textContent = at.classes[c];
      li.querySelector("p").append(at.classInfo[c]);
      classes.append(li);
    }
    add("p", "guide-line", at.synergy);
    add("b", "guide-title", at.ruleSkills);
    const skills = add("ul", "guide-list");
    for (const k of SKILLS) {
      const li = document.createElement("li");
      li.innerHTML = '<span aria-hidden="true"></span><p><b></b> </p>';
      li.querySelector("span").textContent = SKILL_ICON[k];
      li.querySelector("b").textContent = at.skills[k] + ":";
      li.querySelector("p").append(at.skillInfo[k]);
      skills.append(li);
    }
    add("p", "guide-line", at.ruleEnergy);
    add("p", "guide-line", at.ruleTickets(p.tickets_max));
    add("p", "guide-line", at.ruleTokens(p.tokens));
    if (arenaData.shop) add("p", "guide-line", at.ruleCoins(arenaData.shop.coins_win, arenaData.shop.coins_loss));
    add("p", "guide-line", at.ruleRating);
    const week = add("p", "guide-line guide-week", at.ruleWeek);
    const go = document.createElement("button");
    go.type = "button";
    go.className = "ghost";
    go.textContent = at.toTable;
    go.addEventListener("click", () => { topKind = "arena"; haptic("light"); showTab("top"); });
    week.append(go);
  }

  // triangle draws the classes' edge, each arrow from the class that hits
  // harder to the one it beats.
  function triangle() {
    const ns = "http://www.w3.org/2000/svg";
    const svg = document.createElementNS(ns, "svg");
    svg.setAttribute("viewBox", "0 0 280 160");
    svg.setAttribute("class", "guide-tri");
    svg.setAttribute("role", "img");
    svg.setAttribute("aria-label", at.edgeRule);
    const spots = { tank: [140, 30], fighter: [236, 112], mage: [44, 112] };
    let html = '<defs><marker id="triArrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0 0L10 5L0 10z" fill="currentColor"/></marker></defs>';
    for (const c of ["tank", "fighter", "mage"]) {
      const [x1, y1] = spots[c], [x2, y2] = spots[BEATS[c]];
      const dx = x2 - x1, dy = y2 - y1, len = Math.hypot(dx, dy), k = 30 / len;
      html += `<line x1="${(x1 + dx * k).toFixed(1)}" y1="${(y1 + dy * k).toFixed(1)}" x2="${(x2 - dx * k).toFixed(1)}" y2="${(y2 - dy * k).toFixed(1)}" stroke="currentColor" stroke-width="2" marker-end="url(#triArrow)"/>`;
    }
    svg.innerHTML = html;
    for (const c of ["tank", "fighter", "mage"]) {
      const [x, y] = spots[c];
      const g = document.createElementNS(ns, "g");
      g.innerHTML = `<circle cx="${x}" cy="${y}" r="21" class="tri-node"/><text x="${x}" y="${y + 6}" text-anchor="middle" font-size="18"></text><text x="${x}" y="${y + 37}" text-anchor="middle" class="tri-label"></text>`;
      const texts = g.querySelectorAll("text");
      texts[0].textContent = CLASS_ICON[c];
      texts[1].textContent = at.classes[c];
      svg.append(g);
    }
    return svg;
  }

  function arenaMe() {
    const p = arenaData.player;
    const box = $("arenaMe");
    box.innerHTML = '<div class="arena-rating"><small></small><b></b><span></span></div><div class="arena-left"><b class="arena-coins"></b><span></span><span></span></div>';
    box.querySelector(".arena-rating small").textContent = at.rating;
    box.querySelector(".arena-rating b").textContent = spaced(p.rating);
    box.querySelector(".arena-rating span").textContent = at.record(p.wins, p.losses);
    const left = box.querySelectorAll(".arena-left span");
    left[0].textContent = at.tickets(p.tickets, p.tickets_max) + (p.tickets < p.tickets_max ? " " + at.nextTicket(Math.max(1, Math.ceil((ticketAt - Date.now()) / 60000))) : "");
    left[1].textContent = at.tokens(p.tokens_left, p.tokens);
    box.querySelector(".arena-coins").textContent = at.coins(spaced(p.coins || 0));
  }

  // arenaShopList is the arena's shop: a token, a few a day, an arena
  // ticket while the tickets are not full, and packs with a
  // sure Epic or Legendary, for arena coins, opened as the crystal shop's.
  function arenaShopList() {
    const shop = arenaData.shop;
    const block = $("arenaShopBlock");
    block.hidden = !shop;
    if (!shop) return;
    const coins = arenaData.player.coins || 0;
    $("arenaShopCoins").textContent = at.coins(spaced(coins));
    $("arenaShopHint").textContent = at.shopHint(shop.coins_win, shop.coins_loss);
    const list = $("arenaShopItems");
    list.replaceChildren();
    for (const item of shop.items) {
      const row = document.createElement("li");
      row.className = "shop-item";
      const rarity = t.rarities[item.least || 0];
      const p = arenaData.player;
      row.style.setProperty("--tier", item.tokens || item.tickets ? "var(--r1)" : COLORS[item.least || 0]);
      row.innerHTML = '<span class="icon"></span><div><b></b><small></small></div><button type="button" class="ghost"></button>';
      row.querySelector(".icon").textContent = item.tickets ? "🎫" : item.tokens ? "🎟" : RARITY_ICON[item.least || 0];
      row.querySelector("b").textContent = item.tickets ? at.shopTicket : item.tokens ? at.shopToken : t.packItem(rarity);
      row.querySelector("small").textContent = item.tickets ? at.shopTicketHint(p.tickets, p.tickets_max)
        : item.tokens ? at.shopTokenHint(shop.tokens_left, shop.tokens_daily) : t.packHint(item.cards, rarity);
      const button = row.querySelector("button");
      button.textContent = at.coins(spaced(item.price));
      button.disabled = buying || item.price > coins || (item.tokens && shop.tokens_left <= 0) || (item.tickets && p.tickets >= p.tickets_max);
      button.addEventListener("click", () => arenaBuy(item));
      list.append(row);
    }
  }

  // arenaBuy buys an arena shop item, laying a pack out as the crystal
  // shop does.
  async function arenaBuy(item) {
    if (buying) return;
    buying = true;
    arenaShopList();
    haptic("medium");
    try {
      const data = await api("arena_shop", { item: item.id });
      if (data.refused) { toast(at.shopRefused[data.refused] || t.unavailable); return; }
      tokens = data.tokens ?? tokens;
      shards = data.shards ?? shards;
      arenaData.player.coins = data.coins;
      if (item.tickets) {
        arenaData.player.tickets = data.tickets;
        ticketAt = Date.now() + (data.next_ticket_in || 0);
        toast(at.shopTicket.replace(/^1/, "+1"));
        arenaFoes();
      } else if (item.tokens) {
        arenaData.shop.tokens_left = Math.max(0, arenaData.shop.tokens_left - item.tokens);
        toast(t.bought(item.tokens));
      } else {
        const drawn = data.drawn.map((d) => ({ card: d.card, fresh: d.new, serial: d.serial }));
        for (const { card } of drawn) keep(card);
        points = data.points ?? points;
        header();
        shelves();
        await celebrateMythics(drawn);
        openedPanel(drawn, t.packOpened(t.rarities[item.least || 0]));
      }
    } catch (error) {
      notice(error);
    } finally {
      buying = false;
      drawButton();
      if (arenaData) { arenaMe(); arenaShopList(); }
    }
  }

  // arenaTick brings a ticket back on the page when its hour is up, as the
  // server counts it, and keeps the wait shown current.
  function arenaTick() {
    if (!arenaData) return;
    const p = arenaData.player;
    if (p.tickets >= p.tickets_max) return;
    if (Date.now() >= ticketAt) {
      p.tickets += 1;
      ticketAt += 3600000;
      arenaFoes();
    }
    arenaMe();
  }
  setInterval(arenaTick, 30000);

  // arenaHistory lists the player's last battles, attacking or attacked,
  // each with the opponent, the outcome and the rating's change.
  function arenaHistory() {
    const list = $("arenaHistory");
    list.textContent = "";
    const battles = arenaData.history || [];
    if (!battles.length) list.innerHTML = `<li class="empty">${at.histEmpty}</li>`;
    for (const b of battles) {
      const li = document.createElement("li");
      li.className = "foe hist " + (b.won ? "won" : "lost");
      li.innerHTML = '<span class="who"><b></b><small></small></span><span class="hist-change"></span>';
      li.querySelector(".who b").textContent = b.opponent || "?";
      const when = new Date(b.at).toLocaleString(language === "en" ? "en-GB" : language, { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" });
      li.querySelector(".who small").textContent = `${b.won ? "✅" : "❌"} ${b.won ? at.histWon : at.histLost} · ${b.attacked ? at.histAttack : at.histDefense} · ${when}`;
      const d = b.rating_change;
      li.querySelector(".hist-change").textContent = `${d >= 0 ? "+" : "−"}${Math.abs(d)}`;
      list.append(li);
    }
  }

  // arenaTeam draws the team being made, its live power, and the save
  // button, on only for a full team that differs from the one saved.
  function arenaTeam() {
    const saved = (arenaData.team || []).map((f) => f.id);
    const team = withSynergy(arenaDraft.map(slotFighter));
    arenaSynergy(team);
    for (const [id, from, label, note] of [["arenaFront", 0, at.front, at.frontNote], ["arenaBack", 2, at.back, at.backNote]]) {
      const row = $(id);
      row.textContent = "";
      const em = document.createElement("em");
      em.innerHTML = "<b></b> · ";
      em.querySelector("b").textContent = label;
      em.append(note);
      row.append(em);
      for (let slot = from; slot < from + (from ? 3 : 2); slot++) {
        const button = document.createElement("button");
        button.type = "button";
        button.className = "slot";
        const f = team[slot];
        const place = at.slot(label, from ? slot - 1 : slot + 1);
        if (f) {
          button.append(fighterTile(f));
          button.setAttribute("aria-label", `${place}: ${f.name}, ${at.classes[f.class]}` + (f.skill ? `, ${at.skills[f.skill]}` : "") + (f.synergy ? `, 🔗 +${f.synergy}%` : ""));
        } else {
          button.classList.add("empty-slot");
          button.innerHTML = '<span class="slot-plus" aria-hidden="true"><b>+</b><small></small></span>';
          button.querySelector("small").textContent = at.addCard;
          button.setAttribute("aria-label", `${place}: ${at.addCard}`);
        }
        button.addEventListener("click", () => { haptic("light"); arenaPick(slot); });
        row.append(button);
      }
    }
    $("arenaPower").textContent = at.power(spaced(powerOf(team)));
    const full = team.every(Boolean);
    const changed = arenaDraft.some((id, i) => id !== saved[i]);
    $("arenaSave").disabled = arenaBusy || !full || !changed;
    $("arenaSave").hidden = saved.length > 0 && !changed;
    $("arenaTeamHint").textContent = !full ? at.pick : changed ? at.unsaved : at.savedHint;
  }

  // arenaSynergy says under the team which of its animes raise each other,
  // or how a pair would.
  function arenaSynergy(team) {
    const box = $("arenaRules");
    box.textContent = "";
    const seen = {};
    for (const f of team) {
      if (!f || !f.synergy) continue;
      seen[f.anime] = seen[f.anime] || { n: 0, p: f.synergy };
      seen[f.anime].n += 1;
    }
    const names = Object.keys(seen);
    if (!names.length) { box.textContent = at.synergyNone; box.classList.remove("on"); return; }
    box.classList.add("on");
    for (const a of names) {
      const chip = document.createElement("span");
      chip.textContent = at.synergyOn(a, seen[a].n, seen[a].p);
      box.append(chip);
    }
  }

  // arenaPick chooses the card for one slot from the cards held, the
  // strongest first; one already in the team trades places with it.
  function arenaPick(slot) {
    let rarity = -1;
    const panel = document.createElement("div");
    panel.className = "gift arena-pick";
    panel.innerHTML = '<b class="step"></b><input type="search" autocomplete="off"><div class="sets" role="group"></div><ul class="gift-list"></ul>';
    panel.querySelector(".step").textContent = at.slot(slot < 2 ? at.front : at.back, slot < 2 ? slot + 1 : slot - 1);
    const input = panel.querySelector("input");
    input.placeholder = t.mineSearch;
    const chips = panel.querySelector(".sets");
    const held = cards.filter((c) => c.count > 0).map(fighterOf).sort((a, b) => powerOf([b]) - powerOf([a]));
    for (const r of [-1, 4, 3, 2, 1, 0]) {
      if (r >= 0 && !held.some((f) => f.rarity === r)) continue;
      const chip = document.createElement("button");
      chip.type = "button";
      chip.dataset.r = r;
      if (r < 0) chip.textContent = t.allSets;
      else { chip.style.setProperty("--c", COLORS[r]); chip.replaceChildren(tierIcon(r), t.rarities[r]); }
      chip.addEventListener("click", () => { rarity = r; list(); haptic("light"); });
      chips.append(chip);
    }
    function list() {
      for (const chip of chips.children) chip.setAttribute("aria-pressed", String(Number(chip.dataset.r) === rarity));
      const ul = panel.querySelector(".gift-list");
      ul.textContent = "";
      const q = input.value.trim().toLowerCase();
      const shown = held.filter((f) => (rarity < 0 || f.rarity === rarity) && (!q || f.name.toLowerCase().includes(q))).slice(0, 60);
      if (!shown.length) ul.innerHTML = `<li class="gift-empty">${t.none}</li>`;
      for (const f of shown) {
        const li = document.createElement("li");
        const pick = document.createElement("button");
        pick.type = "button";
        pick.className = "pick";
        pick.setAttribute("aria-pressed", String(arenaDraft[slot] === f.id));
        const info = document.createElement("span");
        info.className = "pick-stats";
        const inTeam = arenaDraft.includes(f.id) && arenaDraft[slot] !== f.id;
        info.textContent = `★${f.stars} · ${CLASS_ICON[f.class]} · ⚡${spaced(powerOf([f]))}` + (inTeam ? ` · ${at.inTeam}` : "");
        if (f.skill) {
          const skill = document.createElement("small");
          skill.className = "pick-skill";
          skill.textContent = `${SKILL_ICON[f.skill]} ${at.skills[f.skill]}: ${at.skillInfo[f.skill]}`;
          info.append(skill);
        }
        pick.append(mini(f), info);
        pick.addEventListener("click", () => {
          const other = arenaDraft.indexOf(f.id);
          if (other >= 0) arenaDraft[other] = arenaDraft[slot];
          arenaDraft[slot] = f.id;
          haptic("light");
          closeSheet();
          arenaTeam();
        });
        li.append(pick);
        ul.append(li);
      }
    }
    input.addEventListener("input", list);
    list();
    showPanel(panel);
  }

  $("arenaSave").addEventListener("click", async () => {
    if (arenaBusy || !arenaData || arenaDraft.some((id) => !id)) return;
    arenaBusy = true;
    arenaTeam();
    try {
      const data = await api("arena_team", { front: arenaDraft.slice(0, 2), back: arenaDraft.slice(2) });
      if (data.refused) {
        toast(at.refused[data.refused] || t.unavailable);
        return;
      }
      arenaShow(data);
      toast(at.saved);
      haptic("medium");
    } catch (error) {
      notice(error);
    } finally {
      arenaBusy = false;
      arenaTeam();
      arenaFoes();
    }
  });

  // arenaFoes lists the opponents offered, each with its attack button,
  // off without a saved team or with no ticket left.
  function arenaFoes() {
    const list = $("arenaFoes");
    list.textContent = "";
    const p = arenaData.player;
    const can = !!arenaData.team && p.tickets > 0;
    const foes = arenaData.opponents || [];
    if (!foes.length) list.innerHTML = `<li class="empty">${at.noFoes}</li>`;
    for (const o of foes) {
      const li = document.createElement("li");
      li.className = "foe";
      li.innerHTML = '<button type="button" class="who" aria-haspopup="dialog"><span class="foe-name"><b></b><span class="foe-eye" aria-hidden="true">👁</span></span><small class="foe-rating"></small><small class="foe-power"></small></button><button type="button" class="draw small"></button>';
      li.querySelector(".who b").textContent = o.name || "?";
      li.querySelector(".foe-rating").textContent = `🏅 ${spaced(o.rating)}`;
      li.querySelector(".foe-power").textContent = `⚡ ${spaced(o.power)}`;
      foeView(li, o);
      const button = li.querySelector(".draw");
      button.textContent = at.attack;
      button.setAttribute("aria-label", `${at.attack}: ${o.name || "?"}`);
      button.disabled = arenaBusy || !can;
      button.addEventListener("click", () => arenaAttack(o));
      list.append(li);
    }
    if (foes.length && !can) list.insertAdjacentHTML("beforeend", `<li class="foe-note">${arenaData.team ? at.noAttacks : at.teamFirst}</li>`);
  }

  // foeView opens an opponent's team in the sheet when its name is tapped:
  // the front row and the back row as the attack will meet them.
  function foeView(li, foe) {
    const view = li.querySelector(".who");
    view.setAttribute("aria-label", `${at.foeTeam}: ${foe.name || "?"}`);
    view.addEventListener("click", () => {
      haptic("light");
      showPanel(foePanel(foe));
    });
  }

  function foePanel(foe) {
    const panel = document.createElement("div");
    panel.className = "gift foe-sheet";
    panel.innerHTML = '<h3 class="shop-title"></h3><p class="foe-stats"><small></small><small></small></p>';
    panel.querySelector("h3").textContent = foe.name || "?";
    const stats = panel.querySelectorAll(".foe-stats small");
    stats[0].textContent = `🏅 ${spaced(foe.rating)}`;
    stats[1].textContent = `⚡ ${spaced(foe.power)}`;
    const team = foe.team || [];
    if (!team.length) panel.insertAdjacentHTML("beforeend", `<p class="foe-note">${at.noFoeTeam}</p>`);
    for (const [list, label, rear] of [[team.slice(0, 2), at.front, false], [team.slice(2), at.back, true]]) {
      if (!list.length) continue;
      const row = document.createElement("div");
      row.className = rear ? "arena-row rear" : "arena-row";
      teamRow(row, list, label);
      panel.append(row);
    }
    const attack = document.createElement("button");
    attack.type = "button";
    attack.className = "draw";
    attack.textContent = at.attack;
    attack.disabled = arenaBusy || !arenaData.team || arenaData.player.tickets <= 0;
    attack.addEventListener("click", () => {
      closeSheet();
      arenaAttack(foe);
    });
    panel.append(attack);
    return panel;
  }

  $("arenaRefresh").addEventListener("click", async (event) => {
    const button = event.currentTarget;
    if (button.disabled) return;
    button.disabled = true;
    haptic("light");
    await arenaPage(true);
    button.disabled = false;
  });

  // arenaAttack fights an opponent, replays the battle, then asks for the
  // arena again for the new rating and opponents. A token won is one more
  // card to open.
  async function arenaAttack(foe) {
    if (arenaBusy) return;
    arenaBusy = true;
    arenaFoes();
    haptic("medium");
    try {
      const data = await api("arena_attack", { opponent: foe.id });
      if (data.refused) {
        toast(at.refused[data.refused] || t.unavailable);
        if (data.refused !== "self") await arenaPage();
        return;
      }
      if (data.token) { tokens += 1; drawButton(); }
      arenaData.player.tickets = data.tickets;
      ticketAt = Date.now() + (data.next_ticket_in || 0);
      arenaData.player.tokens_left = data.tokens_left;
      if (data.coins !== undefined) arenaData.player.coins = data.coins;
      arenaMe();
      await replay(data, foe);
      await arenaPage();
    } catch (error) {
      notice(error);
    } finally {
      arenaBusy = false;
      if (arenaData) arenaFoes();
    }
  }

  // ---------- the tower ----------

  // The Tower of Souls: floors of fixed teams, fought with the arena team
  // saved, as often as the player likes; the week's anime raises its cards there.
  // The Arena tab switches between the arena and the tower by its chips.
  let arenaMode = "arena", towerData = null, towerBusy = false;
  for (const chip of document.querySelectorAll("#arenaMode button")) {
    chip.addEventListener("click", () => { arenaMode = chip.dataset.m; haptic("light"); arenaOpen(); });
  }

  // arenaOpen shows the arena or the tower, as the chips say.
  function arenaOpen() {
    for (const chip of document.querySelectorAll("#arenaMode button")) chip.setAttribute("aria-pressed", String(chip.dataset.m === arenaMode));
    $("arenaMain").hidden = arenaMode !== "arena";
    $("towerMain").hidden = arenaMode !== "tower";
    if (arenaMode === "tower") towerPage();
    else arenaPage();
  }

  async function towerPage() {
    try {
      towerShow(await api("tower"));
      notice(null);
    } catch (error) {
      notice(error);
    }
  }

  // towerRow lays fighters out in a row of the tower, two in front or three
  // behind, as the arena's team is.
  function towerRow(id, list, label) { teamRow($(id), list, label); }

  // teamRow fills a row of a team with its cards, its label first.
  function teamRow(row, list, label) {
    row.textContent = "";
    const em = document.createElement("em");
    em.innerHTML = "<b></b>";
    em.querySelector("b").textContent = label;
    row.append(em);
    for (const f of list) {
      const slot = document.createElement("div");
      slot.className = "slot";
      slot.append(fighterTile(f));
      row.append(slot);
    }
  }

  function towerShow(data) {
    towerData = data;
    const tw = at.tower;
    const me = $("towerMe");
    me.innerHTML = '<div class="arena-rating"><small></small><b></b></div><div class="arena-left"><span></span></div>';
    me.querySelector(".arena-rating small").textContent = tw.best;
    me.querySelector(".arena-rating b").textContent = data.floor ? tw.floorN(data.floor) : tw.none;
    const left = me.querySelectorAll(".arena-left span");
    left[0].textContent = data.theme && data.theme.anime ? tw.theme(data.theme.anime, data.theme.bonus) : tw.noTheme;
    $("towerTheme").textContent = data.theme && data.theme.anime ? tw.themeHint : "";
    $("towerTheme").hidden = !(data.theme && data.theme.anime);
    $("towerRule").textContent = tw.rule();
    $("towerFloorTitle").textContent = tw.enemy(data.next);
    $("towerEnemyPower").textContent = at.power(spaced(data.enemy_power || 0));
    $("towerReward").textContent = tw.reward(data.reward.coins, data.reward.tokens);
    const enemy = data.enemy || [];
    towerRow("towerFront", enemy.slice(0, 2), at.front);
    towerRow("towerBack", enemy.slice(2), at.back);
    // The tower fights with the team saved in the arena, changed there; only
    // why the fight cannot start is said here.
    const team = data.team || [];
    const hint = !team.length ? tw.noTeam : "";
    $("towerHint").textContent = hint;
    $("towerHint").hidden = !hint;
    const fight = $("towerFight");
    fight.textContent = tw.fight(data.next);
    fight.disabled = towerBusy || !team.length || !enemy.length;
  }

  $("towerFight").addEventListener("click", async () => {
    if (towerBusy || !towerData) return;
    towerBusy = true;
    towerShow(towerData);
    haptic("medium");
    try {
      const data = await api("tower_fight");
      if (data.refused) {
        toast(at.tower.refused[data.refused] || t.unavailable);
        await towerPage();
        return;
      }
      if (data.tokens !== undefined) { tokens = data.tokens; drawButton(); }
      if (data.coins !== undefined && arenaData) arenaData.player.coins = data.coins;
      await replay(data, { name: at.tower.floorN(data.fought) });
      await towerPage();
    } catch (error) {
      notice(error);
    } finally {
      towerBusy = false;
      if (towerData) towerShow(towerData);
    }
  });

  // ---------- the battle's replay ----------

  // replay shows the battle on the whole screen, the opponent above and the
  // player below, the front rows facing: one event every STEP ms, a hit
  // shaking its target and its HP bar falling, a skill flashing, a fallen
  // card greyed. Skip, or less motion asked for, shows the end at once.
  // Only transforms and opacity move, so that weak phones keep up.
  // 650 since 2026-11-21: at 350 the owners found the battle too fast to follow.
  const STEP = 650;
  // SKIP_POINTS is the card points a player needs for the battle's Skip
  // button, as the owners asked on 2026-11-21.
  const SKIP_POINTS = 200000;
  // HITS are the events that go from one side to the other.
  const HITS = ["hit", "strike", "stun", "poison"];
  // START_ENERGY is each class's energy at a battle's start, as arena.StartEnergy gives it.
  const START_ENERGY = { tank: 30, fighter: 20, mage: 40 };
  // energize fills a card's energy bar, lit when full on a card with a skill.
  function energize(unit, energy) {
    if (energy === undefined || !unit.en) return;
    unit.en.style.transform = `scaleX(${Math.max(0, Math.min(1, energy / 100))})`;
    unit.el.classList.toggle("charged", energy >= 100 && !!unit.f.skill);
  }
  const lessMotion = () => !!(window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches);

  // The battle's numbers, shown under its result as the owners asked on
  // 2026-11-21: for each card the damage it dealt (a poison's ticks counted
  // to the card that poisoned), the health it lost (a shield's share not
  // lost) and what it healed and shielded; the card that dealt the most is
  // the MVP.
  const statsTexts = {
    uz: { title: "📊 Jang statistikasi", mvp: (name, n) => `🏅 MVP: ${name} · ⚔️ ${n}`, legend: "⚔️ bergan zarba · 🩸 yo'qotgan jon · 💚 davo va qalqon" },
    ru: { title: "📊 Статистика боя", mvp: (name, n) => `🏅 MVP: ${name} · ⚔️ ${n}`, legend: "⚔️ нанесённый урон · 🩸 потерянное здоровье · 💚 лечение и щиты" },
    en: { title: "📊 Battle stats", mvp: (name, n) => `🏅 MVP: ${name} · ⚔️ ${n}`, legend: "⚔️ damage dealt · 🩸 health lost · 💚 healing and shields" },
  };
  const st = statsTexts[language] || statsTexts.uz;

  // battleStats are each side's cards with their numbers, worked out from
  // the battle's events by following every card's health.
  function battleStats(data) {
    const sides = [data.attacker || [], data.defender || []].map((list) =>
      list.map((f) => ({ f, dealt: 0, taken: 0, healed: 0, shielded: 0, hp: f.health })));
    const poisoner = new Map();
    for (const e of data.events || []) {
      const foeSide = HITS.includes(e.kind);
      const actor = sides[e.side] && sides[e.side][e.from];
      const target = sides[foeSide ? 1 - e.side : e.side] && sides[foeSide ? 1 - e.side : e.side][e.to];
      if (!target) continue;
      if (foeSide || e.kind === "poisoned") {
        // a poison's tick names the poisoned card alone: it counts to the
        // card that poisoned it last
        const hitter = foeSide ? actor : poisoner.get(target);
        if (hitter) hitter.dealt += e.amount;
        target.taken += Math.max(0, target.hp - e.hp);
        target.hp = e.hp;
        if (e.kind === "poison" && actor) poisoner.set(target, actor);
      } else if (e.kind === "heal") {
        if (actor) actor.healed += e.amount;
        target.hp = e.hp;
      } else if (e.kind === "shield") {
        if (actor) actor.shielded += e.amount;
      } else if (e.kind === "fall") {
        target.hp = 0;
      }
    }
    return sides;
  }

  // statsBlock is the battle's numbers as a table for each side, the MVP
  // first; big numbers are shortened to thousands to fit a phone.
  function statsBlock(data) {
    const sides = battleStats(data);
    const all = sides.flat();
    const mvp = all.reduce((a, b) => (!a || b.dealt > a.dealt ? b : a), null);
    const most = Math.max(1, mvp ? mvp.dealt : 0);
    const short = (n) => (n >= 100000 ? `${Math.round(n / 1000)}k` : n >= 10000 ? `${(n / 1000).toFixed(1)}k` : spaced(n));
    const box = document.createElement("div");
    box.className = "b-stats";
    const title = document.createElement("b");
    title.className = "bs-title";
    title.textContent = st.title;
    box.append(title);
    if (mvp && mvp.dealt > 0) {
      const top = document.createElement("p");
      top.className = "bs-mvp";
      top.textContent = st.mvp(mvp.f.name, short(mvp.dealt));
      box.append(top);
    }
    sides.forEach((list, side) => {
      const part = document.createElement("div");
      part.className = "bs-side " + (side ? "them" : "us");
      const head = document.createElement("div");
      head.className = "bs-head";
      for (const text of [side ? at.them : at.you, "⚔️", "🩸", "💚"]) {
        const cell = document.createElement("span");
        cell.textContent = text;
        head.append(cell);
      }
      part.append(head);
      for (const u of list) {
        const row = document.createElement("div");
        row.className = "bs-row" + (u.hp <= 0 ? " fallen" : "") + (u === mvp && mvp.dealt > 0 ? " mvp" : "");
        const who = document.createElement("span");
        who.className = "bs-name";
        who.innerHTML = '<b></b><i class="bs-bar"><i></i></i>';
        who.querySelector("b").textContent = u.f.name;
        who.querySelector(".bs-bar i").style.width = `${Math.round((u.dealt / most) * 100)}%`;
        row.append(who);
        for (const n of [u.dealt, u.taken, u.healed + u.shielded]) {
          const cell = document.createElement("span");
          cell.textContent = n ? short(n) : "—";
          row.append(cell);
        }
        part.append(row);
      }
      box.append(part);
    });
    const legend = document.createElement("p");
    legend.className = "bs-legend";
    legend.textContent = st.legend;
    box.append(legend);
    return box;
  }

  function replay(data, foe) {
    return new Promise((resolve) => {
      const box = document.createElement("div");
      box.className = "battle";
      box.setAttribute("role", "dialog");
      box.setAttribute("aria-label", at.attack);
      box.innerHTML = '<div class="b-top"><b class="b-round"></b><button type="button" class="ghost b-skip"></button></div>' +
        '<div class="b-side b-them"><small class="b-who"></small><div class="b-row rear"></div><div class="b-row fore"></div></div>' +
        '<div class="b-mid"><span class="b-vs" aria-hidden="true">VS</span><p class="b-line" aria-live="polite"></p></div>' +
        '<div class="b-side b-us"><div class="b-row fore"></div><div class="b-row rear"></div><small class="b-who"></small></div>' +
        '<div class="b-result" hidden><b class="b-title"></b><span class="b-change"></span><span class="b-token"></span><span class="b-coins"></span><button type="button" class="draw b-ok"></button></div>';
      const units = [[], []];
      for (const [side, list, sel] of [[0, data.attacker, ".b-us"], [1, data.defender, ".b-them"]]) {
        const sideBox = box.querySelector(sel);
        list.forEach((f, slot) => {
          const tile = fighterTile(f);
          tile.insertAdjacentHTML("beforeend", '<i class="hp"><i></i></i><i class="en"><i></i></i><span class="hp-n"></span>');
          sideBox.querySelector(slot < 2 ? ".fore" : ".rear").append(tile);
          const num = tile.querySelector(".hp-n");
          num.textContent = spaced(f.health);
          units[side][slot] = { el: tile, bar: tile.querySelector(".hp i"), en: tile.querySelector(".en i"), num, hp: f.health, max: f.health || 1, f, side };
          energize(units[side][slot], START_ENERGY[f.class] || 0);
        });
      }
      const whos = box.querySelectorAll(".b-who");
      whos[0].innerHTML = "<b></b> ";
      whos[0].querySelector("b").textContent = at.them;
      whos[0].append(foe.name || "?");
      whos[1].innerHTML = "<b></b>";
      whos[1].querySelector("b").textContent = at.you;
      const roundBox = box.querySelector(".b-round"), line = box.querySelector(".b-line");
      const skip = box.querySelector(".b-skip");
      skip.textContent = at.skip;
      skip.hidden = points < SKIP_POINTS;
      const events = data.events || [];
      let next = 0, timer = 0, done = false;

      const move = (el, frames, ms) => { if (el.animate) el.animate(frames, { duration: ms, easing: "ease-out" }); };
      function float(unit, text, kind) {
        const n = document.createElement("span");
        n.className = "f-float " + kind;
        n.textContent = text;
        unit.el.append(n);
        if (!n.animate) { setTimeout(() => n.remove(), 700); return; }
        n.animate([{ transform: "translate(-50%, 0)", opacity: 1 }, { transform: "translate(-50%, -28px)", opacity: 0 }], { duration: 750, easing: "ease-out" }).onfinish = () => n.remove();
      }
      function flash(unit, kind) {
        const f = document.createElement("i");
        f.className = "f-flash " + kind;
        unit.el.append(f);
        if (!f.animate) { setTimeout(() => f.remove(), 400); return; }
        f.animate([{ opacity: 0 }, { opacity: 0.85 }, { opacity: 0 }], { duration: 420 }).onfinish = () => f.remove();
      }

      // apply puts one event on the board; shown, it also moves and is told.
      function apply(e, shown) {
        const foeSide = HITS.includes(e.kind);
        const actor = units[e.side][e.from];
        const target = units[foeSide ? 1 - e.side : e.side][e.to];
        if (!target) return;
        if (actor && e.kind !== "fall") energize(actor, e.energy);
        if (e.kind !== "fall") energize(target, e.to_energy);
        target.hp = e.kind === "fall" ? 0 : e.hp;
        target.bar.style.transform = `scaleX(${Math.max(0, Math.min(1, target.hp / target.max))})`;
        target.num.textContent = spaced(Math.max(0, target.hp));
        if (e.kind === "fall") target.el.classList.add("fallen");
        // a stunned card dims until its skipped turn; a poisoned one is
        // tinted for its turns of poison
        if (e.kind === "stun") target.el.classList.add("stunned");
        if (e.kind === "stunned" || e.kind === "fall") target.el.classList.remove("stunned");
        if (e.kind === "poison") target.poison = 3;
        if (e.kind === "poisoned") target.poison = Math.max(0, (target.poison || 0) - 1);
        target.el.classList.toggle("poisoned", e.kind !== "fall" && target.poison > 0);
        if (!shown) return;
        if (e.round > 0) roundBox.textContent = at.round(e.round);
        // the line names each card in its side's colour, ours gold, theirs red
        const name = (u) => {
          const b = document.createElement("b");
          b.className = u && u.side ? "them" : "us";
          b.textContent = u ? u.f.name : "?";
          return b;
        };
        const say = (...parts) => line.replaceChildren(...parts);
        if (foeSide) {
          if (actor) move(actor.el, [{ transform: "none" }, { transform: `translateY(${e.side ? 8 : -8}px)` }, { transform: "none" }], 240);
          move(target.el, [{ transform: "none" }, { transform: "translateX(-5px)" }, { transform: "translateX(5px)" }, { transform: "none" }], 260);
          if (e.kind !== "hit" && actor) flash(actor, "strike");
          flash(target, "hurt");
          const mark = (e.kind === "stun" ? " 💫" : e.kind === "poison" ? " ☠" : "") + (e.edge ? " ⚡" : "");
          float(target, `−${spaced(e.amount)}${mark}`, e.kind === "strike" ? "crit" : e.kind === "poison" ? "poison" : "dmg");
          say(e.kind !== "hit" ? `${SKILL_ICON[e.kind]} ${at.skills[e.kind]}: ` : "", name(actor), " → ", name(target), ` −${spaced(e.amount)}` + (e.edge ? ` · ⚡ ${at.edge}` : ""));
          haptic(e.kind === "hit" ? "light" : "medium");
        } else if (e.kind === "heal") {
          if (actor) flash(actor, "heal");
          flash(target, "heal");
          float(target, `+${spaced(e.amount)}`, "heal");
          say(`${SKILL_ICON.heal} ${at.skills.heal}: `, name(actor), " → ", name(target), ` +${spaced(e.amount)}`);
        } else if (e.kind === "shield") {
          if (actor && actor !== target) flash(actor, "shield");
          flash(target, "shield");
          float(target, `🛡 ${spaced(e.amount)}`, "shield");
          say(`${SKILL_ICON.shield} ${at.skills.shield}: `, ...(actor && actor !== target ? [name(actor), " → "] : []), name(target), ` +${spaced(e.amount)}`);
        } else if (e.kind === "provoke") {
          flash(target, "provoke");
          float(target, SKILL_ICON.provoke, "provoke");
          say(`${SKILL_ICON.provoke} ${at.skills.provoke}: `, name(target));
        } else if (e.kind === "stunned") {
          float(target, SKILL_ICON.stun, "stun");
          line.textContent = at.stunned(target.f.name);
        } else if (e.kind === "poisoned") {
          float(target, `☠ −${spaced(e.amount)}`, "poison");
          line.textContent = at.poisoned(target.f.name, spaced(e.amount));
        } else if (e.kind === "fall") {
          line.textContent = at.fell(target.f.name);
        }
      }

      function step() {
        if (done) return;
        if (next >= events.length) { finish(); return; }
        const e = events[next++];
        apply(e, true);
        timer = setTimeout(step, STEP);
      }
      function finish() {
        if (done) return;
        done = true;
        clearTimeout(timer);
        while (next < events.length) apply(events[next++], false);
        const last = events[events.length - 1];
        roundBox.textContent = last && last.round > 0 ? at.round(last.round) : "";
        skip.hidden = true;
        line.textContent = "";
        const result = box.querySelector(".b-result");
        result.classList.add(data.won ? "won" : "lost");
        result.querySelector(".b-title").textContent = data.won ? at.won : at.lost;
        result.querySelector(".b-change").textContent = data.fought ? (data.won ? at.tower.cleared(data.fought) : at.tower.failed(data.fought)) : at.change(data.rating_change || 0);
        const token = result.querySelector(".b-token");
        token.textContent = data.token ? at.token : data.tokens_won ? at.tower.tokens(data.tokens_won) : "";
        token.hidden = !token.textContent;
        const coins = result.querySelector(".b-coins");
        coins.textContent = data.coins_won ? at.coinsWon(data.coins_won) : "";
        coins.hidden = !data.coins_won;
        result.querySelector(".b-ok").before(statsBlock(data));
        result.hidden = false;
        try { tg.HapticFeedback.notificationOccurred(data.won ? "success" : "error"); } catch (_) { /* outside Telegram */ }
      }
      skip.addEventListener("click", finish);
      const ok = box.querySelector(".b-ok");
      ok.textContent = t.carryOn;
      ok.addEventListener("click", () => {
        box.classList.add("leaving");
        setTimeout(() => { box.remove(); resolve(); }, 200);
      });
      document.body.append(box);
      if (lessMotion()) finish();
      else timer = setTimeout(step, 450);
    });
  }

  // ---------- the arena's table ----------

  // The Reyting tab shows the cards' table or the arena's, by its chips.
  let topKind = "cards", topAsked = 0;
  for (const chip of document.querySelectorAll("#topKind button")) {
    chip.addEventListener("click", () => { topKind = chip.dataset.k; haptic("light"); ranking(); });
  }

  // placeRow is the player's own row under a table they are not on: their
  // place up to 100, and past it the share of players at or above them, so a
  // place deep in a long table still reads well.
  function placeRow(place, total, name, small, points) {
    const li = document.createElement("li");
    li.className = "me mine";
    li.innerHTML = `<span class="pos"></span><span class="who"><b></b><small></small></span><span class="pts"></span>`;
    const pos = li.querySelector(".pos");
    if (place <= 100) pos.textContent = place;
    else { pos.textContent = t.topPct(Math.max(1, Math.ceil((place * 100) / Math.max(total, 1)))); pos.classList.add("pct"); }
    li.querySelector(".who b").textContent = name || t.youRow;
    li.querySelector(".who small").textContent = small;
    li.querySelector(".pts").textContent = points;
    return li;
  }

  function arenaTable(top, me, player) {
    const table = $("table");
    table.textContent = "";
    if (!top.length) table.innerHTML = `<p class="empty">${at.topEmpty}</p>`;
    top.forEach((row, i) => {
      const li = document.createElement("li");
      if (row.me) li.className = "me";
      li.innerHTML = `<span class="pos"></span><span class="who"><b></b><small></small></span><span class="pts"></span>`;
      li.querySelector(".pos").textContent = ["🥇", "🥈", "🥉"][i] || i + 1;
      li.querySelector(".who b").textContent = row.name || "?";
      li.querySelector(".who small").textContent = at.topRow(row.wins, row.losses, spaced(row.power));
      li.querySelector(".pts").textContent = spaced(row.rating);
      table.append(li);
    });
    if (me && player && me.place > top.length) table.append(placeRow(me.place, me.total, player.name, at.topRow(player.wins, player.losses, spaced(player.power)), spaced(player.rating)));
  }

  // The week's prizes, the player's place now and the time left; the server
  // pays them each Monday.
  function arenaWeek(week) {
    const box = $("arenaWeek");
    box.textContent = "";
    if (!week) { box.hidden = true; return; }
    const left = Math.max(0, week.ends - Date.now()), hours = Math.floor(left / 3600000);
    const head = document.createElement("b"), rule = document.createElement("small"), mine = document.createElement("p"), ends = document.createElement("small"), list = document.createElement("div");
    head.textContent = at.week;
    rule.textContent = at.weekRule;
    const event = document.createElement("p");
    event.className = "weekEvent";
    event.textContent = at.weekEvent;
    event.hidden = !week.event;
    mine.textContent = at.weekPlace(week.place, week.prize, week.boxes);
    ends.textContent = at.weekEnds(Math.floor(hours / 24), hours % 24);
    list.className = "prizes";
    for (const p of week.prizes || []) {
      const chip = document.createElement("span");
      chip.textContent = `${at.weekRow(p.from, p.to)}: ${p.tokens} 🎟${p.boxes ? ` + ${p.boxes} 🎁` : ""}`;
      list.append(chip);
    }
    box.append(head, event, rule, list, mine, ends);
  }

  // ---------- tabs ----------

  function showTab(tab) {
    const was = opened;
    opened = tab;
    for (const button of document.querySelectorAll(".nav button")) button.classList.toggle("on", button.dataset.tab === tab);
    $("coinShards").classList.toggle("on", tab === "shop");
    for (const name of ["collection", "draw", "market", "anime", "shop", "arena", "top"]) $(`tab-${name}`).hidden = name !== tab;
    if (tab === "top") ranking();
    if (tab === "arena") arenaOpen();
    if (tab === "shop") shop();
    if (tab === "anime") animePage();
    if (tab === "market") market();
    // Coming back to the collection asks for the quests again, as cards
    // opened or battles won elsewhere may have done one.
    if (tab === "collection" && was && was !== "collection" && questSummary) api("quests").then(questShow).catch(() => {});
    drawButton();
    eventBanner();
    questEntry();
    window.scrollTo(0, 0);
  }
  for (const button of document.querySelectorAll(".nav button")) {
    button.addEventListener("click", () => {
      showTab(button.dataset.tab);
      haptic("light");
    });
  }

  if (!tg || !tg.initData) {
    $("notice").textContent = t.noTelegram;
    $("notice").hidden = false;
    $("open1").disabled = $("open10").disabled = $("open50").disabled = true;
    return;
  }
  tg.ready();
  tg.expand();
  try { tg.setHeaderColor("#0b0a14"); tg.setBackgroundColor("#0b0a14"); } catch (_) { /* older clients */ }
  load();
})();
