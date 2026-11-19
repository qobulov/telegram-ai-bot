// Kartalar, the anime card collection's Mini App. The server keeps the
// catalog, the draws and who holds what; a card not held comes without its
// name or picture, so the page shows only its rarity until it is drawn.
"use strict";

(function () {
  const $ = (id) => document.getElementById(id);
  const tg = window.Telegram && window.Telegram.WebApp;

  const texts = {
    uz: {
      arenaTab: "Arena", arenaTeam: "⚔️ Jamoangiz", arenaSave: "Jamoani saqlash", arenaFoes: "🎯 Raqiblar", arenaHistory: "📜 Janglar tarixi", arenaRefresh: "🔄 Yangilash", topCards: "🎴 Kartalar", topArena: "⚔️ Arena",
      title: "Kartalar", collected: "to'plandi", animeTab: "Animelar", animeSearch: "Anime yoki personaj", animeBack: "← Barcha animelar", animeOwned: "✓ Sizda bor", animeMissing: "Sizda hali yo'q", animeEmpty: "Hech narsa topilmadi", animeGoal: (n) => `🎟 Hammasini yig'ing: +${n} token`, animeClaim: (n) => `🎁 ${n} token olish`, animeClaimed: (n) => `✅ ${n} token olingan`, animeGot: (n) => `🎟 +${n} token! Kolleksiya to'liq`, collection: "Kolleksiya", draw: "Olish", top: "Reyting",
      search: "Kartani qidirish", rarities: ["Oddiy", "Noyob", "Epik", "Afsonaviy", "Mifik"],
      drawNow: "Karta olish", drawing: "Ochilmoqda…", next: (t) => `Keyingi karta: ${t}`, ready: "Karta tayyor!",
     
      shardsTitle: "Kristallar", shardsHint: "Kristal bilan kutmasdan karta oching, do'konda token va paketlar oling.", cancel: "Bekor qilish",
      open1: "1x", open10: "5x", costShort: "💎 yetmaydi", tokenHint: (d, c) => `🎟 Token bitta karta ochadi. Har 2 soatda +${d} token (12 tagacha yig'iladi), tugasa karta ${c} 💎`, nextToken: (time) => `⏳ Keyingi token: ${time}`, given: (n) => `🎟 +${n} token berildi`, short: (n) => `💎 Yana ${n} kristal kerak`, openedTen: "5 ta karta", freshN: (n) => `🆕 ${n} ta yangi`, againN: (n) => `🔁 ${n} ta takror`, freshTag: "YANGI", carryOn: "Davom etish", noPoints: "ochko qo'shilmaydi", 
      fresh: "🆕 Yangi karta!", again: (n) => `🔁 Sizda endi ×${n}`, plus: (n) => `+${n} ochko`,
      attack: "Hujum", health: "Jon", value: "Ochko", cards: (n) => `${n} karta`, pts: "ochko",
      none: "Hech narsa topilmadi", noTop: "Hali hech kim karta olmagan",
      noTelegram: "Ilovani Telegram'da oching: botga /card yozing.", stale: "Ilova eskirdi, uni qayta oching.",
      unavailable: "Aloqa uzildi, birozdan keyin urinib ko'ring.",
      allSets: "Hammasi", pointsLabel: "ochko", loading: "Kartalar yuklanmoqda…", more: "Yuklanmoqda…",
      welcomeTitle: "Kolleksiyangiz bo'sh", welcomeText: "Har 2 soatda bitta token oling, har biri bitta anime karta ochadi. Hammasini to'plang.",
      welcomeButton: "Birinchi kartani olish", seeCollection: "Kolleksiyada ko'rish →",
      locked: (r, o) => `🔒 Bu ${r.toLowerCase()} karta hali yopiq. Uning tushish ehtimoli: ${o}.`,
      wait: (time) => `⏳ ${time}`,
      market: "Bozor", newOffer: "➕ Taklif qo'yish", filterAll: "Hammasi", filterCan: "Bera olaman", filterMine: "Mening",
      gives: "Beradi", wants: "Xohlaydi", swap: "🔄 Almashish", noCard: "Sizda yo'q", remove: "✕ Olib tashlash", yours: "⭐ Sizniki", marketEmpty: "Hozircha taklif yo'q",
      marketHint: (n) => `Kartangizni qo'ying va evaziga kerakli kartani tanlang. Kimda o'sha karta bo'lsa, almashadi. ${n} tagacha taklif, 7 kun turadi.`,
      step1: "1. Qaysi kartangizni berasiz?", step2: "2. Evaziga qaysi kartani xohlaysiz?", mineSearch: "Kartalaringizdan qidiring", wantSearch: "Xohlagan karta nomini yozing",
      place: "Taklif qo'yish", placed: "✅ Taklif bozorga qo'yildi", swapped: (c) => `🔄 Almashildi! Siz ${c} oldingiz`, removed: "Taklif olib tashlandi",
      refused: { uneven: "Faqat bir xil darajadagi kartaga almashish mumkin", limited: "🔒 Limited kartani almashib bo'lmaydi", over: "Bu taklif endi yo'q", gone: "Bu taklif endi yo'q", own: "Bu o'zingizning taklifingiz", not_held: "Sizda bu karta yo'q", same: "Bir xil kartani almashib bo'lmaydi", fused: "⭐ Yulduzli kartaning oxirgi nusxasini almashib bo'lmaydi" },
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
      arenaTab: "Арена", arenaTeam: "⚔️ Ваша команда", arenaSave: "Сохранить команду", arenaFoes: "🎯 Соперники", arenaHistory: "📜 История боёв", arenaRefresh: "🔄 Обновить", topCards: "🎴 Карты", topArena: "⚔️ Арена",
      title: "Карты", collected: "собрано", animeTab: "Аниме", animeSearch: "Аниме или персонаж", animeBack: "← Все аниме", animeOwned: "✓ Есть у вас", animeMissing: "Пока нет у вас", animeEmpty: "Ничего не найдено", animeGoal: (n) => `🎟 Соберите всех: +${n} токенов`, animeClaim: (n) => `🎁 Забрать ${n} токенов`, animeClaimed: (n) => `✅ ${n} токенов получено`, animeGot: (n) => `🎟 +${n} токенов! Коллекция собрана`, collection: "Коллекция", draw: "Получить", top: "Рейтинг",
      search: "Поиск по картам", rarities: ["Обычная", "Редкая", "Эпическая", "Легендарная", "Мифическая"],
      drawNow: "Получить карту", drawing: "Открываем…", next: (t) => `Следующая карта: ${t}`, ready: "Карта готова!",
     
      shardsTitle: "Кристаллы", shardsHint: "За кристаллы — карта без ожидания, а в магазине — токены и наборы.", cancel: "Отмена",
      open1: "1x", open10: "5x", costShort: "💎 не хватает", tokenHint: (d, c) => `🎟 Токен открывает одну карту. Каждые 2 часа +${d} (копится до 12), без токенов карта стоит ${c} 💎`, nextToken: (time) => `⏳ Следующий токен: ${time}`, given: (n) => `🎟 +${n} токен(ов)`, short: (n) => `💎 Нужно ещё ${n} кристаллов`, openedTen: "5 карт", freshN: (n) => `🆕 Новых: ${n}`, againN: (n) => `🔁 Повторов: ${n}`, freshTag: "НОВАЯ", carryOn: "Продолжить", noPoints: "очки не добавляются", 
      fresh: "🆕 Новая карта!", again: (n) => `🔁 Теперь у вас ×${n}`, plus: (n) => `+${n} очков`,
      attack: "Атака", health: "Жизнь", value: "Очки", cards: (n) => `${n} карт`, pts: "очков",
      none: "Ничего не найдено", noTop: "Ещё никто не получил карту",
      noTelegram: "Откройте приложение в Telegram: напишите боту /card.", stale: "Приложение устарело, откройте его снова.",
      unavailable: "Нет связи, попробуйте чуть позже.",
      allSets: "Все", pointsLabel: "очки", loading: "Загружаем карты…", more: "Загружаем…",
      welcomeTitle: "Коллекция пуста", welcomeText: "Каждые 2 часа получайте токен, он открывает аниме-карту. Соберите все.",
      welcomeButton: "Получить первую карту", seeCollection: "Посмотреть в коллекции →",
      locked: (r, o) => `🔒 Эта карта (${r.toLowerCase()}) ещё закрыта. Шанс выпадения: ${o}.`,
      wait: (time) => `⏳ ${time}`,
      market: "Рынок", newOffer: "➕ Выставить", filterAll: "Все", filterCan: "Могу дать", filterMine: "Мои",
      gives: "Отдаёт", wants: "Хочет", swap: "🔄 Обменять", noCard: "Нет у вас", remove: "✕ Снять", yours: "⭐ Ваше", marketEmpty: "Пока предложений нет",
      marketHint: (n) => `Выставьте свою карту и выберите, какую хотите взамен. У кого она есть — обменяется. До ${n} предложений, 7 дней.`,
      step1: "1. Какую карту отдаёте?", step2: "2. Какую хотите взамен?", mineSearch: "Поиск по вашим картам", wantSearch: "Название нужной карты",
      place: "Выставить", placed: "✅ Предложение выставлено", swapped: (c) => `🔄 Обмен! Вы получили ${c}`, removed: "Предложение снято",
      refused: { uneven: "Менять можно только на карту той же редкости", limited: "🔒 Лимитированную карту нельзя обменять", over: "Этого предложения уже нет", gone: "Этого предложения уже нет", own: "Это ваше предложение", not_held: "У вас нет этой карты", same: "Нельзя менять карту на ту же", fused: "⭐ Последнюю копию звёздной карты нельзя обменять" },
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
      arenaTab: "Arena", arenaTeam: "⚔️ Your team", arenaSave: "Save the team", arenaFoes: "🎯 Opponents", arenaHistory: "📜 Battle history", arenaRefresh: "🔄 Refresh", topCards: "🎴 Cards", topArena: "⚔️ Arena",
      title: "Cards", collected: "collected", animeTab: "Anime", animeSearch: "Anime or character", animeBack: "← All anime", animeOwned: "✓ You have it", animeMissing: "Not yours yet", animeEmpty: "Nothing found", animeGoal: (n) => `🎟 Collect them all: +${n} tokens`, animeClaim: (n) => `🎁 Claim ${n} tokens`, animeClaimed: (n) => `✅ ${n} tokens claimed`, animeGot: (n) => `🎟 +${n} tokens! Collection complete`, collection: "Collection", draw: "Draw", top: "Ranking",
      search: "Search cards", rarities: ["Common", "Rare", "Epic", "Legendary", "Mythic"],
      drawNow: "Draw a card", drawing: "Opening…", next: (t) => `Next card in ${t}`, ready: "A card is ready!",
     
      shardsTitle: "Crystals", shardsHint: "Crystals open a card without the wait and buy tokens and packs in the shop.", cancel: "Cancel",
      open1: "1x", open10: "5x", costShort: "💎 not enough", tokenHint: (d, c) => `🎟 A token opens one card. +${d} every 2 hours, up to 12 kept; with none, a card is ${c} 💎`, nextToken: (time) => `⏳ Next token: ${time}`, given: (n) => `🎟 +${n} tokens`, short: (n) => `💎 ${n} more crystals needed`, openedTen: "5 cards", freshN: (n) => `🆕 ${n} new`, againN: (n) => `🔁 ${n} again`, freshTag: "NEW", carryOn: "Continue", noPoints: "no points added", 
      fresh: "🆕 A new card!", again: (n) => `🔁 You now have ×${n}`, plus: (n) => `+${n} points`,
      attack: "Attack", health: "Life", value: "Points", cards: (n) => `${n} cards`, pts: "points",
      none: "Nothing found", noTop: "Nobody has drawn a card yet",
      noTelegram: "Open the app in Telegram: send /card to the bot.", stale: "The app is out of date; open it again.",
      unavailable: "No connection; try again in a bit.",
      allSets: "All", pointsLabel: "points", loading: "Loading the cards…", more: "Loading…",
      welcomeTitle: "Your collection is empty", welcomeText: "Get a token every 2 hours, each opens an anime card. Collect them all.",
      welcomeButton: "Draw your first card", seeCollection: "See it in the collection →",
      locked: (r, o) => `🔒 This ${r.toLowerCase()} card is still locked. Its odds: ${o}.`,
      wait: (time) => `⏳ ${time}`,
      market: "Market", newOffer: "➕ Make an offer", filterAll: "All", filterCan: "I can give", filterMine: "Mine",
      gives: "Gives", wants: "Wants", swap: "🔄 Trade", noCard: "You lack it", remove: "✕ Take down", yours: "⭐ Yours", marketEmpty: "No offers yet",
      marketHint: (n) => `Put up a card and pick the one you want for it. Whoever holds it can trade. Up to ${n} offers, for 7 days.`,
      step1: "1. Which card do you give?", step2: "2. Which card do you want?", mineSearch: "Search your cards", wantSearch: "Name of the card you want",
      place: "Put it up", placed: "✅ Your offer is up", swapped: (c) => `🔄 Traded! You got ${c}`, removed: "Offer taken down",
      refused: { uneven: "A card may be traded only for one of the same rarity", limited: "🔒 A limited card cannot be traded", over: "This offer is gone", gone: "This offer is gone", own: "This is your own offer", not_held: "You do not have that card", same: "A card cannot be traded for itself", fused: "⭐ The last copy of a starred card cannot be traded" },
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
  let shards = 0, cardCost = 15, batch = 5, tokens = 0, nextTokenAt = 0, refreshing = false;

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
    nextTokenAt = Date.now() + (data.next_token_in || 0);
    tokens = data.tokens || 0;
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

  // alive is a Mythic card's character, cut from the picture, laid over it
  // and moving: it breathes, and in the large view its hair stirs.
  function alive(card) {
    const box = document.createElement("div");
    box.className = "alive";
    box.setAttribute("aria-hidden", "true");
    const cut = `url("/kartalar/art/cut/${card.id}.webp")`;
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
  const SPECIAL = { 298761: { tag: "LIMITED", petals: true, fireflies: true, hair: true, plate: "/kartalar/art/298761p.jpg" } };

  // dress adds a special card's mark, petals and fireflies; the large view
  // has more of them.
  function dress(el, card, large) {
    const special = SPECIAL[card.id] || (card.limited ? { tag: "LIMITED" } : null);
    if (!special) return;
    el.classList.add("special");
    let html = "";
    if (special.petals) html += '<div class="petals" aria-hidden="true">' + "<i></i>".repeat(large ? 14 : 6) + "</div>";
    if (special.fireflies) html += '<div class="fireflies" aria-hidden="true">' + "<i></i>".repeat(large ? 9 : 4) + "</div>";
    el.insertAdjacentHTML("beforeend", html);
    const living = el.querySelector(".alive");
    if (special.hair && large && living) {
      // flowing hair: the cut-out once more, its edges alone, waved by #flow
      const flow = document.createElement("i");
      flow.className = "flow";
      flow.style.backgroundImage = `url("/kartalar/art/cut/${card.id}.webp")`;
      living.classList.add("flowing");
      living.append(flow);
      // the picture under it without her, so no still hair shows behind
      // the moving one
      const base = el.querySelector("img");
      if (special.plate && base) base.src = special.plate;
    }
    if (special.tag) {
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
    picture(img, card.image);
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
    // The copies left, the one kept and the free ones: those used up for
    // stars are gone, so a card with no free copy shows no count.
    if (card.free > 0) {
      const count = document.createElement("span");
      count.className = "count";
      count.textContent = `×${card.free + 1}`;
      tile.append(count);
    }
    tile.addEventListener("click", () => open(card));
    return tile;
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
    picture(img, card.image, true);
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
      img.after(alive(card));
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

  function open(card) {
    const sheet = $("sheetCard");
    sheet.textContent = "";
    sheet.classList.remove("giving");
    sheet.append(face(card), statsOf(card));
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
    for (const [id, n] of [["open1", 1], ["open10", batch]]) {
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

  // ---------- shards ----------

  function shardsPanel() {
    $("shardCount").textContent = spaced(shards);
    drawButton();
  }

  $("open1").addEventListener("click", () => reveal(1));
  $("open10").addEventListener("click", () => reveal(batch));

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
      $("result").replaceChildren(...withTiers(best.fresh ? `${t.fresh} · ${t.plus(spaced(best.card.value))}` : `${t.again((best.card.free || 0) + 1)} · ${t.noPoints}`));
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
    panel.querySelector(".opened-head b").textContent = title || t.openedTen;
    panel.querySelector(".opened-head span").textContent = [fresh ? t.freshN(fresh) : "", drawn.length - fresh ? t.againN(drawn.length - fresh) : ""].filter(Boolean).join(" · ");
    const grid = panel.querySelector(".opened-grid");
    [...drawn].sort((a, b) => b.card.rarity - a.card.rarity).forEach(({ card, fresh }, i) => {
      const tile = cardTile(card);
      tile.style.animationDelay = `${i * 0.09}s`;
      if (fresh) {
        const tag = document.createElement("span");
        tag.className = "fresh";
        tag.textContent = t.freshTag;
        tile.append(tag);
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
  // petals for a card dressed with them), the card rising, and which copy of
  // it in the whole game this is. A tap or the button closes it. Only
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
      if (serial > 0 && (card.limited || SPECIAL[card.id])) {
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

  // celebrateMythics celebrates each Mythic of a draw in turn.
  async function celebrateMythics(drawn) {
    for (const d of drawn) if (d.card.rarity === 4) await celebrate(d.card, d.serial);
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

  let offers = [], offerFilter = "all", openLimit = 5;

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
      openLimit = data.open_limit || openLimit;
      marketList();
      notice(null);
    } catch (error) {
      notice(error);
    }
  }

  function marketList() {
    $("marketHint").textContent = t.marketHint(openLimit);
    for (const chip of document.querySelectorAll("#marketFilter button")) chip.setAttribute("aria-pressed", String(chip.dataset.f === offerFilter));
    const list = $("offers");
    list.textContent = "";
    // "All" holds the player's own offers too, marked, so one just placed is
    // seen on the market; "can" is others' the player can take.
    const shown = offers.filter((o) => offerFilter === "all" ? true : offerFilter === "can" ? o.can && !o.mine : o.mine);
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
        button.textContent = o.can ? t.swap : t.noCard;
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
  // card they want for it, of the same rarity: the first ten of the catalog
  // show at once, the most loved first, ten more as the list is scrolled
  // down, a name typed narrowing them. A limited card is in neither list.
  function offerPanel() {
    let give = null, want = null, timer = 0;
    // the pages of the card wanted: asked counts the lists begun, so that a
    // page come late for an older list is dropped
    let asked = 0, offset = 0, more = false, loading = false;
    const panel = document.createElement("div");
    panel.className = "gift";
    panel.innerHTML = '<b class="step"></b><input type="search" class="mine-q" autocomplete="off"><ul class="gift-list pick-mine"></ul><b class="step"></b><input type="search" class="want-q" autocomplete="off" disabled><ul class="gift-list pick-want"></ul><button type="button" class="draw gift-send" disabled></button>';
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
      const held = cards.filter((c) => c.count > 0 && !c.limited && (!q || c.name.toLowerCase().includes(q))).sort((a, b) => b.rarity - a.rarity).slice(0, 30);
      if (!held.length) list.innerHTML = `<li class="gift-empty">${t.none}</li>`;
      for (const card of held) pickRow(list, card, give, choose);
    }
    function choose(card) {
      const sameRarity = give && give.rarity === card.rarity;
      give = card;
      steps[1].textContent = t.step2 + " · " + t.rarities[card.rarity];
      wantQ.disabled = false;
      if (!sameRarity || (want && want.id === card.id)) want = null;
      ready();
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
        const data = await api("catalog", { rarity: give.rarity, card: give.id, query: wantQ.value.trim(), offset });
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
      if (kind === "arena") { arenaTable(data.top || []); arenaWeek(data.week); return; }
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
  // animeGoal is an anime's collection: the cards it needs (limited ones
  // aside), how many are held, and its reward's state: none (too small),
  // open, ready (all held) or claimed.
  function animeGoal(name, list) {
    const need = list.filter((c) => !c.limited);
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
  function previewTile(card, count) {
    const tile = document.createElement("button");
    tile.type = "button";
    tile.className = "card" + (count ? "" : " missing");
    tile.style.setProperty("--c", COLORS[card.rarity]);
    if (count && card.rarity >= 3) tile.classList.add("foil");
    if (card.rarity === 4) tile.classList.add("mythic");
    const img = new Image();
    img.loading = "lazy";
    img.alt = "";
    picture(img, card.image);
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
    tile.addEventListener("click", () => previewCard(card, count));
    return tile;
  }
  function previewCard(card, count) {
    const sheet = $("sheetCard");
    sheet.textContent = "";
    sheet.classList.remove("giving");
    const note = document.createElement("p");
    note.className = "preview-note";
    note.textContent = count ? t.animeOwned : t.animeMissing;
    sheet.append(face(card), note);
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

  // ---------- event ----------

  // The Firefly Wedding event: pulls for tokens at the top of the shop,
  // at the usual odds, 0.4% of them Satoko, the 300th pull
  // without her sure to give her. The server says whether it runs.
  const eventTexts = {
    uz: {
      title: "🌸 Firefly Wedding", tag: "EVENT · LIMITED",
      left: (d, h, m) => (d ? `⏳ ${d} kun ${h} soat qoldi` : h ? `⏳ ${h} soat ${m} daqiqa qoldi` : `⏳ ${m} daqiqa qoldi`),
      odds: (m, s) => `👑 Mifik ${m} · 🌸 Satoko ${s}`, pity: (n, max) => `Satoko kafolati: ${n}/${max}`, owned: "✅ Satoko sizda bor — har kim faqat bir marta oladi",
      open: (n, price) => `${n} ta · ${n * price} 🎟`, short: (n) => `🎟 Yana ${n} ta token kerak`, over: "🌸 Event tugadi", starts: (h, m) => (h ? `⏳ ${h} soat ${m} daqiqadan keyin boshlanadi` : `⏳ ${m} daqiqadan keyin boshlanadi`), soon: "🌸 Event hali boshlanmadi",
      pulled: (n) => `🌸 ${n} ta event karta`, got: "🌸 Satoko Kirigaya tushdi!", badge: "🌸 EVENT", go: "Ochish ›",
    },
    ru: {
      title: "🌸 Firefly Wedding", tag: "ИВЕНТ · LIMITED",
      left: (d, h, m) => (d ? `⏳ Осталось ${d} дн. ${h} ч.` : h ? `⏳ Осталось ${h} ч. ${m} мин.` : `⏳ Осталось ${m} мин.`),
      odds: (m, s) => `👑 Мифическая ${m} · 🌸 Сатоко ${s}`, pity: (n, max) => `Гарант Сатоко: ${n}/${max}`, owned: "✅ Сатоко уже у вас — её можно получить только один раз",
      open: (n, price) => `${n} шт. · ${n * price} 🎟`, short: (n) => `🎟 Нужно ещё ${n} токенов`, over: "🌸 Ивент закончился", starts: (h, m) => (h ? `⏳ Начнётся через ${h} ч. ${m} мин.` : `⏳ Начнётся через ${m} мин.`), soon: "🌸 Ивент ещё не начался",
      pulled: (n) => `🌸 Ивент-карты: ${n}`, got: "🌸 Выпала Сатоко Киригая!", badge: "🌸 ИВЕНТ", go: "Открыть ›",
    },
    en: {
      title: "🌸 Firefly Wedding", tag: "EVENT · LIMITED",
      left: (d, h, m) => (d ? `⏳ ${d} days ${h} hours left` : h ? `⏳ ${h} hours ${m} minutes left` : `⏳ ${m} minutes left`),
      odds: (m, s) => `👑 Mythic ${m} · 🌸 Satoko ${s}`, pity: (n, max) => `Satoko guarantee: ${n}/${max}`, owned: "✅ You have Satoko — each player gets her once",
      open: (n, price) => `${n} · ${n * price} 🎟`, short: (n) => `🎟 ${n} more tokens needed`, over: "🌸 The event is over", starts: (h, m) => (h ? `⏳ Starts in ${h} hours ${m} minutes` : `⏳ Starts in ${m} minutes`), soon: "🌸 The event has not started yet",
      pulled: (n) => `🌸 ${n} event cards`, got: "🌸 Satoko Kirigaya dropped!", badge: "🌸 EVENT", go: "Open ›",
    },
  };
  const et = eventTexts[language] || eventTexts.uz;
  let eventData = null, eventEndsAt = 0, eventStartsAt = 0, pulling = false;

  // eventMark shows the event's badge on the crystals' pill, which opens
  // the shop, while the event runs, so that players notice it.
  $("eventBadge").textContent = et.badge;
  function eventMark() {
    $("eventBadge").hidden = !(eventData && (eventData.active || eventData.soon));
    eventBanner();
  }

  // eventBanner shows the event at the top of the first pages, the
  // collection and the draw, while it runs or is coming; a tap opens the
  // shop at the event.
  $("eventBannerTag").textContent = et.tag;
  $("eventBannerTitle").textContent = et.title;
  $("eventBannerGo").textContent = et.go;
  function eventBanner() {
    const on = !!(eventData && (eventData.active || eventData.soon)) && (opened === "collection" || opened === "draw");
    $("eventBanner").hidden = !on;
    if (!on) return;
    const image = eventData.card && eventData.card.image;
    if (image && $("eventBannerArt").dataset.src !== image) {
      $("eventBannerArt").dataset.src = image;
      picture($("eventBannerArt"), image, true);
    }
    $("eventBannerLeft").textContent = $("eventEnds").textContent;
  }
  $("eventBanner").addEventListener("click", () => {
    haptic("light");
    showTab("shop");
    setTimeout(() => $("event").scrollIntoView({ behavior: "smooth", block: "start" }), 150);
  });

  // eventBlock asks for the event and shows it while it runs; the app asks
  // once it has loaded, for the badge, and again whenever the shop opens.
  async function eventBlock() {
    try {
      eventData = await api("event");
    } catch (_) {
      $("event").hidden = true;
      return;
    }
    eventEndsAt = Date.now() + (eventData.ends_in || 0);
    eventStartsAt = Date.now() + (eventData.starts_in || 0);
    $("event").hidden = !(eventData.active || eventData.soon);
    eventMark();
    if (!eventData.active && !eventData.soon) return;
    const tile = previewTile(eventData.card, 1);
    const mark = tile.querySelector(".owned");
    if (mark) mark.remove();
    $("eventArt").replaceChildren(tile);
    $("eventTag").textContent = et.tag;
    $("eventTitle").textContent = et.title;
    const percent = (n) => `${n / 10}%`;
    const mythic = eventData.odds[4];
    $("eventOdds").textContent = et.odds(percent(mythic), percent(mythic * eventData.share));
    eventPity();
    eventLeft();
  }

  function eventPity() {
    $("eventPity").textContent = eventData.owned ? et.owned : et.pity(eventData.pity, eventData.pity_max);
    $("eventPityBar").style.width = eventData.owned ? "100%" : `${Math.min(100, (eventData.pity / eventData.pity_max) * 100)}%`;
    for (const [id, n] of [["eventOpen1", 1], ["eventOpen10", eventData.batch]]) {
      $(id).textContent = et.open(n, eventData.price);
      $(id).disabled = pulling || !eventData.active;
    }
  }

  // eventLeft counts down to the event's end, and hides it then.
  function eventLeft() {
    if (!eventData) return;
    if (eventData.soon) {
      const wait = eventStartsAt - Date.now();
      if (wait <= 0) { eventBlock(); return; }
      const m = Math.ceil(wait / 60000);
      $("eventEnds").textContent = et.starts(Math.floor(m / 60), m % 60);
      return;
    }
    if (!eventData.active) return;
    const left = eventEndsAt - Date.now();
    if (left <= 0) { $("event").hidden = true; eventData.active = false; eventMark(); return; }
    const minutes = Math.floor(left / 60000);
    $("eventEnds").textContent = et.left(Math.floor(minutes / 1440), Math.floor((minutes % 1440) / 60), minutes % 60);
    eventBanner();
  }
  setInterval(eventLeft, 30000);

  $("eventOpen1").addEventListener("click", () => eventPull(1));
  $("eventOpen10").addEventListener("click", () => eventPull(eventData ? eventData.batch : 10));

  // eventPull pulls n event cards for tokens, one shown large as a card
  // opened is, more laid out as a batch is.
  async function eventPull(n) {
    if (pulling || !eventData || !eventData.active) return;
    if (tokens < n * eventData.price) { toast(et.short(n * eventData.price - tokens)); return; }
    pulling = true;
    eventPity();
    haptic("medium");
    try {
      const data = await api("event_open", { count: n });
      if (data.refused === "soon") { toast(et.soon); return; }
      if (data.refused === "over") { toast(et.over); $("event").hidden = true; eventData.active = false; eventMark(); return; }
      if (data.refused === "funds") { tokens = data.tokens ?? tokens; toast(et.short(data.short)); return; }
      tokens = data.tokens ?? tokens;
      shards = data.shards ?? shards;
      points = data.points ?? points;
      eventData.pity = data.pity;
      if (data.drawn.some((d) => d.card && d.card.id === eventData.card.id)) eventData.owned = true;
      if (data.given) toast(t.given(data.given));
      const drawn = data.drawn.map((d) => ({ card: d.card, fresh: d.new, serial: d.serial }));
      for (const { card } of drawn) keep(card);
      header();
      shelves();
      await celebrateMythics(drawn);
      if (drawn.length > 1) {
        openedPanel(drawn, et.pulled(drawn.length));
      } else {
        const { card, fresh } = drawn[0];
        open(card);
        const note = document.createElement("p");
        note.className = "result";
        note.replaceChildren(...withTiers(fresh ? `${t.fresh} · ${t.plus(spaced(card.value))}` : `${t.again((card.free || 0) + 1)} · ${t.noPoints}`));
        $("sheetCard").prepend(note);
      }
      if (drawn.some((d) => d.card.id === eventData.card.id)) { haptic("heavy"); toast(et.got); }
    } catch (error) {
      notice(error);
    } finally {
      pulling = false;
      eventPity();
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
      rating: "Arena reytingi", record: (w, l) => `${w} g'alaba · ${l} mag'lubiyat`,
      tickets: (n, m) => `🎫 Arena tiketlari: ${n}/${m}`, nextTicket: (min) => (min >= 60 ? "· keyingisi 1 soatda" : `· keyingisi ${min} daqiqada`), tokens: (n, m) => `🎟 G'alaba tokenlari: ${n}/${m}`,
      histEmpty: "Hali jang bo'lmagan", histAttack: "⚔️ Hujum", histDefense: "🛡️ Himoya", histWon: "G'alaba", histLost: "Mag'lubiyat",
      front: "Old qator", back: "Orqa qator", power: (n) => `⚡ ${n}`,
      pick: "5 ta karta tanlang: 2 tasi oldinda, 3 tasi orqada. Yulduzlar kartani kuchaytiradi.",
      unsaved: "O'zgarish hali saqlanmadi", savedHint: "Jamoa saqlangan. Almashtirish uchun uyachani bosing.",
      slot: (row, n) => `${row} · ${n}-o'rin`, inTeam: "jamoada", saved: "✅ Jamoa saqlandi",
      attack: "⚔️ Hujum", noFoes: "Hozircha raqib yo'q: boshqalar ham jamoa saqlasin", teamFirst: "Hujum uchun avval jamoani saqlang", noAttacks: "Tiketlar tugadi: har soatda 1 ta qo'shiladi",
      round: (n) => `Raund ${n}`, skip: "O'tkazib yuborish", won: "🏆 G'alaba!", lost: "💀 Mag'lubiyat", change: (d) => `Reyting ${d >= 0 ? "+" : "−"}${Math.abs(d)}`, token: "🎟 +1 token",
      week: "🏆 Haftalik mukofot", weekRule: "Har dushanba 08:00 da: hafta davomida kamida 1 marta hujum qilgan top 100 ga token pochtaga keladi", weekPlace: (n, p) => (n ? `Siz hozir ${n}-o'rindasiz${p ? ` · +${p} 🎟` : ""}` : "Mukofot uchun bu hafta kamida 1 marta hujum qiling"), weekEnds: (d, h) => (d ? `Hafta tugashiga ${d} kun ${h} soat` : `Hafta tugashiga ${h} soat`), weekRow: (a, b) => (a === b ? `${a}-o'rin` : `${a}–${b}`),
      you: "Siz", topEmpty: "Arenada hali hech kim jang qilmagan", topRow: (w, l, p) => `${w} G · ${l} M · ⚡${p}`,
      skills: { strike: "Kuchli zarba", heal: "Davolash", shield: "Qalqon" }, fell: (n) => `💀 ${n} yiqildi`,
      refused: { size: "Jamoada 5 ta karta bo'lishi kerak", repeat: "Bitta kartani ikki marta qo'yib bo'lmaydi", not_held: "Jamoadagi karta endi sizda yo'q", no_tickets: "Tiket qolmadi: har soatda 1 ta qo'shiladi", no_team: "Avval jamoani saqlang: undagi karta endi sizda yo'q", opponent_gone: "Bu raqib endi yo'q, ro'yxat yangilandi", self: "O'zingizga hujum qilib bo'lmaydi" },
    },
    ru: {
      rating: "Рейтинг арены", record: (w, l) => `${w} побед · ${l} поражений`,
      tickets: (n, m) => `🎫 Билеты арены: ${n}/${m}`, nextTicket: (min) => (min >= 60 ? "· следующий через 1 ч" : `· следующий через ${min} мин`), tokens: (n, m) => `🎟 Токенов за победы: ${n}/${m}`,
      histEmpty: "Боёв ещё не было", histAttack: "⚔️ Атака", histDefense: "🛡️ Защита", histWon: "Победа", histLost: "Поражение",
      front: "Передний ряд", back: "Задний ряд", power: (n) => `⚡ ${n}`,
      pick: "Выберите 5 карт: 2 впереди, 3 сзади. Звёзды усиливают карту.",
      unsaved: "Изменения ещё не сохранены", savedHint: "Команда сохранена. Нажмите на ячейку, чтобы заменить карту.",
      slot: (row, n) => `${row} · место ${n}`, inTeam: "в команде", saved: "✅ Команда сохранена",
      attack: "⚔️ Атака", noFoes: "Пока нет соперников: пусть другие тоже сохранят команду", teamFirst: "Чтобы атаковать, сохраните команду", noAttacks: "Билеты закончились: каждый час добавляется 1",
      round: (n) => `Раунд ${n}`, skip: "Пропустить", won: "🏆 Победа!", lost: "💀 Поражение", change: (d) => `Рейтинг ${d >= 0 ? "+" : "−"}${Math.abs(d)}`, token: "🎟 +1 токен",
      week: "🏆 Награда недели", weekRule: "Каждый понедельник в 08:00: топ 100, атаковавшие на неделе хотя бы раз, получают токены почтой", weekPlace: (n, p) => (n ? `Сейчас вы на ${n}-м месте${p ? ` · +${p} 🎟` : ""}` : "Чтобы получить награду, атакуйте хотя бы раз на этой неделе"), weekEnds: (d, h) => (d ? `До конца недели ${d} дн ${h} ч` : `До конца недели ${h} ч`), weekRow: (a, b) => (a === b ? `${a} место` : `${a}–${b}`),
      you: "Вы", topEmpty: "На арене ещё никто не сражался", topRow: (w, l, p) => `${w} В · ${l} П · ⚡${p}`,
      skills: { strike: "Сильный удар", heal: "Лечение", shield: "Щит" }, fell: (n) => `💀 ${n} пал(а)`,
      refused: { size: "В команде должно быть 5 карт", repeat: "Одну карту нельзя поставить дважды", not_held: "Карты из команды у вас больше нет", no_tickets: "Билетов нет: каждый час добавляется 1", no_team: "Сначала сохраните команду: карты из неё у вас больше нет", opponent_gone: "Этого соперника больше нет, список обновлён", self: "Нельзя атаковать себя" },
    },
    en: {
      rating: "Arena rating", record: (w, l) => `${w} wins · ${l} losses`,
      tickets: (n, m) => `🎫 Arena tickets: ${n}/${m}`, nextTicket: (min) => (min >= 60 ? "· next in 1 hour" : `· next in ${min} min`), tokens: (n, m) => `🎟 Win tokens: ${n}/${m}`,
      histEmpty: "No battles yet", histAttack: "⚔️ Attack", histDefense: "🛡️ Defense", histWon: "Victory", histLost: "Defeat",
      front: "Front row", back: "Back row", power: (n) => `⚡ ${n}`,
      pick: "Pick 5 cards: 2 in front, 3 behind. Stars make a card stronger.",
      unsaved: "The change is not saved yet", savedHint: "Your team is saved. Tap a slot to swap a card.",
      slot: (row, n) => `${row} · slot ${n}`, inTeam: "in the team", saved: "✅ Team saved",
      attack: "⚔️ Attack", noFoes: "No opponents yet: others need to save a team too", teamFirst: "Save a team to attack", noAttacks: "No tickets left: one comes back every hour",
      round: (n) => `Round ${n}`, skip: "Skip", won: "🏆 Victory!", lost: "💀 Defeat", change: (d) => `Rating ${d >= 0 ? "+" : "−"}${Math.abs(d)}`, token: "🎟 +1 token",
      week: "🏆 Weekly prize", weekRule: "Every Monday at 08:00 the top 100 who attacked at least once that week get tokens by mail", weekPlace: (n, p) => (n ? `You are #${n} now${p ? ` · +${p} 🎟` : ""}` : "Attack at least once this week to earn a prize"), weekEnds: (d, h) => (d ? `Week ends in ${d}d ${h}h` : `Week ends in ${h}h`), weekRow: (a, b) => (a === b ? `#${a}` : `#${a}–${b}`),
      you: "You", topEmpty: "Nobody has fought in the arena yet", topRow: (w, l, p) => `${w} W · ${l} L · ⚡${p}`,
      skills: { strike: "Power strike", heal: "Heal", shield: "Shield" }, fell: (n) => `💀 ${n} fell`,
      refused: { size: "A team needs 5 cards", repeat: "A card cannot be placed twice", not_held: "You no longer hold a card of the team", no_tickets: "No tickets left: one comes back every hour", no_team: "Save a team first: you no longer hold one of its cards", opponent_gone: "This opponent is gone; the list is refreshed", self: "You cannot attack yourself" },
    },
  };
  const at = arenaTexts[language] || arenaTexts.uz;
  const CLASS_ICON = { tank: "🛡️", fighter: "⚔️", mage: "🔮" };
  const SKILL_ICON = { strike: "💥", heal: "💚", shield: "🔰" };
  const EMPTY_TEAM = [0, 0, 0, 0, 0];
  let arenaData = null, arenaDraft = EMPTY_TEAM.slice(), arenaBusy = false, ticketAt = 0;

  // fighterOf is a held card as the arena draws it, its class and skill
  // worked out as arena.FighterOf does, for a team not saved yet; a saved
  // team's come from the server.
  function fighterOf(card) {
    const cls = card.health >= 4 * card.attack ? "tank" : card.health <= 3 * card.attack ? "mage" : "fighter";
    const skill = card.rarity >= 3 ? ["strike", "heal", "shield"][card.id % 3] : "";
    return { id: card.id, name: card.name, image: card.image, rarity: card.rarity, stars: card.stars || 1, attack: card.attack || 0, health: card.health || 0, class: cls, skill };
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
    tile.innerHTML = '<img alt=""><span class="f-class"></span><span class="f-name"></span>';
    picture(tile.querySelector("img"), f.image);
    tile.querySelector(".f-class").textContent = (CLASS_ICON[f.class] || "") + (f.skill ? SKILL_ICON[f.skill] : "");
    tile.querySelector(".f-name").textContent = f.name;
    if (f.stars > 1) tile.insertAdjacentHTML("beforeend", `<span class="f-stars">★${f.stars}</span>`);
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
    arenaMe();
    arenaTeam();
    arenaFoes();
    arenaHistory();
  }

  function arenaMe() {
    const p = arenaData.player;
    const box = $("arenaMe");
    box.innerHTML = '<div class="arena-rating"><small></small><b></b><span></span></div><div class="arena-left"><span></span><span></span></div>';
    box.querySelector(".arena-rating small").textContent = at.rating;
    box.querySelector(".arena-rating b").textContent = spaced(p.rating);
    box.querySelector(".arena-rating span").textContent = at.record(p.wins, p.losses);
    const left = box.querySelectorAll(".arena-left span");
    left[0].textContent = at.tickets(p.tickets, p.tickets_max) + (p.tickets < p.tickets_max ? " " + at.nextTicket(Math.max(1, Math.ceil((ticketAt - Date.now()) / 60000))) : "");
    left[1].textContent = at.tokens(p.tokens_left, p.tokens);
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
    const team = arenaDraft.map(slotFighter);
    for (const [id, from, label] of [["arenaFront", 0, at.front], ["arenaBack", 2, at.back]]) {
      const row = $(id);
      row.textContent = "";
      const em = document.createElement("em");
      em.textContent = label;
      row.append(em);
      for (let slot = from; slot < from + (from ? 3 : 2); slot++) {
        const button = document.createElement("button");
        button.type = "button";
        button.className = "slot";
        const f = team[slot];
        if (f) button.append(fighterTile(f));
        else button.innerHTML = '<span class="slot-plus" aria-hidden="true">+</span>';
        button.setAttribute("aria-label", at.slot(label, from ? slot - 1 : slot + 1));
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
      li.innerHTML = '<span class="who"><b></b><small></small></span><button type="button" class="draw small"></button>';
      li.querySelector(".who b").textContent = o.name || "?";
      li.querySelector(".who small").textContent = `🏅 ${spaced(o.rating)} · ⚡ ${spaced(o.power)}`;
      const button = li.querySelector("button");
      button.textContent = at.attack;
      button.disabled = arenaBusy || !can;
      button.addEventListener("click", () => arenaAttack(o));
      list.append(li);
    }
    if (foes.length && !can) list.insertAdjacentHTML("beforeend", `<li class="foe-note">${arenaData.team ? at.noAttacks : at.teamFirst}</li>`);
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

  // ---------- the battle's replay ----------

  // replay shows the battle on the whole screen, the opponent above and the
  // player below, the front rows facing: one event every STEP ms, a hit
  // shaking its target and its HP bar falling, a skill flashing, a fallen
  // card greyed. Skip, or less motion asked for, shows the end at once.
  // Only transforms and opacity move, so that weak phones keep up.
  const STEP = 350;
  const lessMotion = () => !!(window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches);

  function replay(data, foe) {
    return new Promise((resolve) => {
      const box = document.createElement("div");
      box.className = "battle";
      box.setAttribute("role", "dialog");
      box.setAttribute("aria-label", at.attack);
      box.innerHTML = '<div class="b-top"><b class="b-round"></b><button type="button" class="ghost b-skip"></button></div>' +
        '<div class="b-side b-them"><small class="b-who"></small><div class="b-row rear"></div><div class="b-row fore"></div></div>' +
        '<div class="b-vs" aria-hidden="true">VS</div>' +
        '<div class="b-side b-us"><div class="b-row fore"></div><div class="b-row rear"></div><small class="b-who"></small></div>' +
        '<p class="b-line" aria-live="polite"></p>' +
        '<div class="b-result" hidden><b class="b-title"></b><span class="b-change"></span><span class="b-token"></span><button type="button" class="draw b-ok"></button></div>';
      const units = [[], []];
      for (const [side, list, sel] of [[0, data.attacker, ".b-us"], [1, data.defender, ".b-them"]]) {
        const sideBox = box.querySelector(sel);
        list.forEach((f, slot) => {
          const tile = fighterTile(f);
          tile.insertAdjacentHTML("beforeend", '<i class="hp"><i></i></i>');
          sideBox.querySelector(slot < 2 ? ".fore" : ".rear").append(tile);
          units[side][slot] = { el: tile, bar: tile.querySelector(".hp i"), hp: f.health, max: f.health || 1, f };
        });
      }
      const whos = box.querySelectorAll(".b-who");
      whos[0].textContent = foe.name || "?";
      whos[1].textContent = at.you;
      const roundBox = box.querySelector(".b-round"), line = box.querySelector(".b-line");
      const skip = box.querySelector(".b-skip");
      skip.textContent = at.skip;
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
        const foeSide = e.kind === "hit" || e.kind === "strike";
        const actor = units[e.side][e.from];
        const target = units[foeSide ? 1 - e.side : e.side][e.to];
        if (!target) return;
        target.hp = e.kind === "fall" ? 0 : e.hp;
        target.bar.style.transform = `scaleX(${Math.max(0, Math.min(1, target.hp / target.max))})`;
        if (e.kind === "fall") target.el.classList.add("fallen");
        if (!shown) return;
        if (e.round > 0) roundBox.textContent = at.round(e.round);
        const name = (u) => (u ? u.f.name : "?");
        if (foeSide) {
          if (actor) move(actor.el, [{ transform: "none" }, { transform: `translateY(${e.side ? 8 : -8}px)` }, { transform: "none" }], 240);
          move(target.el, [{ transform: "none" }, { transform: "translateX(-5px)" }, { transform: "translateX(5px)" }, { transform: "none" }], 260);
          if (e.kind === "strike" && actor) flash(actor, "strike");
          flash(target, "hurt");
          float(target, `−${spaced(e.amount)}`, e.kind === "strike" ? "crit" : "dmg");
          line.textContent = (e.kind === "strike" ? `${SKILL_ICON.strike} ${at.skills.strike}: ` : "") + `${name(actor)} → ${name(target)} −${spaced(e.amount)}`;
          haptic(e.kind === "strike" ? "medium" : "light");
        } else if (e.kind === "heal") {
          if (actor) flash(actor, "heal");
          flash(target, "heal");
          float(target, `+${spaced(e.amount)}`, "heal");
          line.textContent = `${SKILL_ICON.heal} ${at.skills.heal}: ${name(actor)} → ${name(target)} +${spaced(e.amount)}`;
        } else if (e.kind === "shield") {
          flash(target, "shield");
          float(target, `🛡 ${spaced(e.amount)}`, "shield");
          line.textContent = `${SKILL_ICON.shield} ${at.skills.shield}: ${name(target)} +${spaced(e.amount)}`;
        } else if (e.kind === "fall") {
          line.textContent = at.fell(name(target));
        }
      }

      function step() {
        if (done) return;
        if (next >= events.length) { finish(); return; }
        const e = events[next++];
        apply(e, true);
        // the shields at the start come together, quickly
        timer = setTimeout(step, e.round === 0 ? 150 : STEP);
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
        result.querySelector(".b-change").textContent = at.change(data.rating_change || 0);
        const token = result.querySelector(".b-token");
        token.textContent = data.token ? at.token : "";
        token.hidden = !data.token;
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

  function arenaTable(top) {
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
    mine.textContent = at.weekPlace(week.place, week.prize);
    ends.textContent = at.weekEnds(Math.floor(hours / 24), hours % 24);
    list.className = "prizes";
    for (const p of week.prizes || []) {
      const chip = document.createElement("span");
      chip.textContent = `${at.weekRow(p.from, p.to)}: ${p.tokens} 🎟`;
      list.append(chip);
    }
    box.append(head, rule, list, mine, ends);
  }

  // ---------- tabs ----------

  function showTab(tab) {
    const was = opened;
    opened = tab;
    for (const button of document.querySelectorAll(".nav button")) button.classList.toggle("on", button.dataset.tab === tab);
    $("coinShards").classList.toggle("on", tab === "shop");
    for (const name of ["collection", "draw", "market", "anime", "shop", "arena", "top"]) $(`tab-${name}`).hidden = name !== tab;
    if (tab === "top") ranking();
    if (tab === "arena") arenaPage();
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
    $("open1").disabled = $("open10").disabled = true;
    return;
  }
  tg.ready();
  tg.expand();
  try { tg.setHeaderColor("#0b0a14"); tg.setBackgroundColor("#0b0a14"); } catch (_) { /* older clients */ }
  load();
})();
