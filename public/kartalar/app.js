// Kartalar, the anime card collection's Mini App. The server keeps the
// catalog, the draws and who holds what; a card not held comes without its
// name or picture, so the page shows only its rarity until it is drawn.
"use strict";

(function () {
  const $ = (id) => document.getElementById(id);
  const tg = window.Telegram && window.Telegram.WebApp;

  const texts = {
    uz: {
      title: "Kartalar", collected: "to'plandi", collection: "Kolleksiya", draw: "Olish", top: "Reyting",
      search: "Kartani qidirish", rarities: ["Oddiy", "Noyob", "Epik", "Afsonaviy", "Mifik"],
      drawNow: "Karta olish", drawing: "Ochilmoqda…", next: (t) => `Keyingi karta: ${t}`, ready: "Karta tayyor!",
      autoTitle: "🤖 Avto-ochish", autoHint: "Karta tayyor bo'lishi bilan o'zi ochilib, shaxsiy chatga ovozsiz keladi", autoOn: "🤖 Avto-ochish yoqildi", autoOff: "⏸ Avto-ochish o'chirildi",
      shardsTitle: "Kristallar", melt: (n, w) => `♻️ ${n} ta takrorni aylantirish → +${w} 💎`, noSpare: "♻️ Aylantirish uchun takror karta yo'q", fewShards: (n, h) => `💎 Kerak: ${n}, sizda: ${h}`, melted: (n, g) => `♻️ ${n} ta karta → +${g} 💎`, shardsHint: "Takror kartalar kristalga aylanadi, ochko va reyting kamaymaydi. Kristal bilan pastda karta sotib oling yoki duelda tiking.", ratesLabel: "1 ta takror:", meltTitle: "Kristalga aylantirish", meltWarn: "Bu nusxalarni keyin sovg'a qilib bo'lmaydi. Har kartaning bittasi kolleksiyada qoladi.", meltDo: (w) => `♻️ Aylantirish · +${w} 💎`, cancel: "Bekor qilish",
      fresh: "🆕 Yangi karta!", sure: "✨ Kafolatlangan karta!", pity: (l, m) => `{3} Afsonaviy — ${l} kartadan keyin aniq\n{4} Mifik — ${m} kartadan keyin aniq`, again: (n) => `🔁 Sizda endi ×${n}`, plus: (n) => `+${n} ochko`,
      attack: "Hujum", health: "Jon", value: "Ochko", cards: (n) => `${n} karta`, pts: "ochko",
      none: "Hech narsa topilmadi", noTop: "Hali hech kim karta olmagan",
      noTelegram: "Ilovani Telegram'da oching: botga /kartalar yozing.", stale: "Ilova eskirdi, uni qayta oching.",
      unavailable: "Aloqa uzildi, birozdan keyin urinib ko'ring.",
      allSets: "Hammasi", pointsLabel: "ochko", loading: "Kartalar yuklanmoqda…",
      welcomeTitle: "Kolleksiyangiz bo'sh", welcomeText: "Har soatda bitta tasodifiy anime karta oling va hammasini to'plang.",
      welcomeButton: "Birinchi kartani olish", seeCollection: "Kolleksiyada ko'rish →",
      locked: (r, o) => `🔒 Bu ${r.toLowerCase()} karta hali yopiq. Uning tushish ehtimoli: ${o}.`,
      wait: (time) => `⏳ ${time}`,
      gift: "🎁 Sovg'a qilish", giftSearch: "Ism yoki @username", giftHint: (n) => `Karta ochkolari bilan birga o'tadi. Kuniga ${n} tagacha.`,
      giftTo: (name) => `🎁 ${name}ga yuborish`, giftSent: (card, name) => `🎁 ${card} → ${name}`, giftNone: "Bunday o'yinchi topilmadi",
      giftLimit: (n) => `⏳ Kuniga ${n} ta karta berish mumkin. Ertaga yana.`, giftNoPlayer: "Bu o'yinchi hali karta ochmagan", giftGone: "Bu karta endi sizda yo'q", back: "← Orqaga",
      shop: "Do'kon", shopTitle: "Karta sotib olish", shopHint: "Yoqqan kategoriyani tanlang: undan tasodifiy karta chiqadi, daraja ehtimollari va kafolat odatdagidek.", anySet: "🎲 Istalgan", packCards: (n) => `${n} ta karta`, needMore: (n) => `yana ${n} 💎`,
      market: "Bozor", newOffer: "➕ Taklif qo'yish", filterAll: "Hammasi", filterCan: "Bera olaman", filterMine: "Mening",
      gives: "Beradi", wants: "Xohlaydi", swap: "🔄 Almashish", noCard: "Sizda yo'q", remove: "✕ Olib tashlash", marketEmpty: "Hozircha taklif yo'q",
      marketHint: (n) => `Kartangizni qo'ying va evaziga kerakli kartani tanlang. Kimda o'sha karta bo'lsa, almashadi. ${n} tagacha taklif, 7 kun turadi.`,
      step1: "1. Qaysi kartangizni berasiz?", step2: "2. Evaziga qaysi kartani xohlaysiz?", mineSearch: "Kartalaringizdan qidiring", wantSearch: "Xohlagan karta nomini yozing",
      place: "Taklif qo'yish", placed: "✅ Taklif bozorga qo'yildi", swapped: (c) => `🔄 Almashildi! Siz ${c} oldingiz`, removed: "Taklif olib tashlandi",
      refused: { over: "Bu taklif endi yo'q", gone: "Bu taklif endi yo'q", own: "Bu o'zingizning taklifingiz", not_held: "Sizda bu karta yo'q", same: "Bir xil kartani almashib bo'lmaydi", no_spare: "Faqat takror kartani sovg'a qilish mumkin" },
      offersLimit: (n) => `Bir vaqtda ${n} tagacha taklif qo'yish mumkin`, giftSpare: "Faqat takror (×2 va undan ko'p) kartani sovg'a qilish mumkin",
    },
    ru: {
      title: "Карты", collected: "собрано", collection: "Коллекция", draw: "Получить", top: "Рейтинг",
      search: "Поиск по картам", rarities: ["Обычная", "Редкая", "Эпическая", "Легендарная", "Мифическая"],
      drawNow: "Получить карту", drawing: "Открываем…", next: (t) => `Следующая карта: ${t}`, ready: "Карта готова!",
      autoTitle: "🤖 Автооткрытие", autoHint: "Карта откроется сама, как только будет готова, и придёт в личку без звука", autoOn: "🤖 Автооткрытие включено", autoOff: "⏸ Автооткрытие выключено",
      shardsTitle: "Кристаллы", melt: (n, w) => `♻️ Расплавить повторы: ${n} → +${w} 💎`, noSpare: "♻️ Повторных карт нет", fewShards: (n, h) => `💎 Нужно: ${n}, у вас: ${h}`, melted: (n, g) => `♻️ ${n} карт → +${g} 💎`, shardsHint: "Повторы становятся кристаллами, очки и рейтинг не уменьшаются. За кристаллы — карты ниже или ставка в дуэли.", ratesLabel: "1 повтор:", meltTitle: "Расплавить в кристаллы", meltWarn: "Эти копии потом нельзя будет подарить. По одной карте каждого вида останется в коллекции.", meltDo: (w) => `♻️ Расплавить · +${w} 💎`, cancel: "Отмена",
      fresh: "🆕 Новая карта!", sure: "✨ Гарантированная карта!", pity: (l, m) => `{3} Легендарная — точно через ${l} карт\n{4} Мифическая — точно через ${m} карт`, again: (n) => `🔁 Теперь у вас ×${n}`, plus: (n) => `+${n} очков`,
      attack: "Атака", health: "Жизнь", value: "Очки", cards: (n) => `${n} карт`, pts: "очков",
      none: "Ничего не найдено", noTop: "Ещё никто не получил карту",
      noTelegram: "Откройте приложение в Telegram: напишите боту /kartalar.", stale: "Приложение устарело, откройте его снова.",
      unavailable: "Нет связи, попробуйте чуть позже.",
      allSets: "Все", pointsLabel: "очки", loading: "Загружаем карты…",
      welcomeTitle: "Коллекция пуста", welcomeText: "Каждый час получайте случайную аниме-карту и соберите все.",
      welcomeButton: "Получить первую карту", seeCollection: "Посмотреть в коллекции →",
      locked: (r, o) => `🔒 Эта карта (${r.toLowerCase()}) ещё закрыта. Шанс выпадения: ${o}.`,
      wait: (time) => `⏳ ${time}`,
      gift: "🎁 Подарить", giftSearch: "Имя или @username", giftHint: (n) => `Карта переходит вместе с очками. До ${n} в день.`,
      giftTo: (name) => `🎁 Отправить: ${name}`, giftSent: (card, name) => `🎁 ${card} → ${name}`, giftNone: "Игрок не найден",
      giftLimit: (n) => `⏳ В день можно отдать ${n} карт. Завтра снова.`, giftNoPlayer: "Этот игрок ещё не открыл ни одной карты", giftGone: "Этой карты у вас уже нет", back: "← Назад",
      shop: "Магазин", shopTitle: "Купить карту", shopHint: "Выберите категорию: из неё выпадет случайная карта, шансы и гарантия как обычно.", anySet: "🎲 Любая", packCards: (n) => `${n} карт`, needMore: (n) => `ещё ${n} 💎`,
      market: "Рынок", newOffer: "➕ Выставить", filterAll: "Все", filterCan: "Могу дать", filterMine: "Мои",
      gives: "Отдаёт", wants: "Хочет", swap: "🔄 Обменять", noCard: "Нет у вас", remove: "✕ Снять", marketEmpty: "Пока предложений нет",
      marketHint: (n) => `Выставьте свою карту и выберите, какую хотите взамен. У кого она есть — обменяется. До ${n} предложений, 7 дней.`,
      step1: "1. Какую карту отдаёте?", step2: "2. Какую хотите взамен?", mineSearch: "Поиск по вашим картам", wantSearch: "Название нужной карты",
      place: "Выставить", placed: "✅ Предложение выставлено", swapped: (c) => `🔄 Обмен! Вы получили ${c}`, removed: "Предложение снято",
      refused: { over: "Этого предложения уже нет", gone: "Этого предложения уже нет", own: "Это ваше предложение", not_held: "У вас нет этой карты", same: "Нельзя менять карту на ту же", no_spare: "Дарить можно только повторную карту" },
      offersLimit: (n) => `Одновременно можно выставить до ${n} предложений`, giftSpare: "Дарить можно только повторную карту (×2 и больше)",
    },
    en: {
      title: "Cards", collected: "collected", collection: "Collection", draw: "Draw", top: "Ranking",
      search: "Search cards", rarities: ["Common", "Rare", "Epic", "Legendary", "Mythic"],
      drawNow: "Draw a card", drawing: "Opening…", next: (t) => `Next card in ${t}`, ready: "A card is ready!",
      autoTitle: "🤖 Auto opener", autoHint: "Each card opens itself when it is ready and comes to your private chat quietly", autoOn: "🤖 Auto opener on", autoOff: "⏸ Auto opener off",
      shardsTitle: "Crystals", melt: (n, w) => `♻️ Melt ${n} spares → +${w} 💎`, noSpare: "♻️ No spare cards to melt", fewShards: (n, h) => `💎 It takes ${n}, you have ${h}`, melted: (n, g) => `♻️ ${n} cards → +${g} 💎`, shardsHint: "Spares melt into crystals; your points and ranking stay. Crystals buy the cards below or a duel's stake.", ratesLabel: "1 spare:", meltTitle: "Melt into crystals", meltWarn: "These copies cannot be given afterwards. One of each card stays in your collection.", meltDo: (w) => `♻️ Melt · +${w} 💎`, cancel: "Cancel",
      fresh: "🆕 A new card!", sure: "✨ A guaranteed card!", pity: (l, m) => `{3} Legendary — sure in ${l} cards\n{4} Mythic — sure in ${m} cards`, again: (n) => `🔁 You now have ×${n}`, plus: (n) => `+${n} points`,
      attack: "Attack", health: "Life", value: "Points", cards: (n) => `${n} cards`, pts: "points",
      none: "Nothing found", noTop: "Nobody has drawn a card yet",
      noTelegram: "Open the app in Telegram: send /kartalar to the bot.", stale: "The app is out of date; open it again.",
      unavailable: "No connection; try again in a bit.",
      allSets: "All", pointsLabel: "points", loading: "Loading the cards…",
      welcomeTitle: "Your collection is empty", welcomeText: "Draw a random anime card every hour and collect them all.",
      welcomeButton: "Draw your first card", seeCollection: "See it in the collection →",
      locked: (r, o) => `🔒 This ${r.toLowerCase()} card is still locked. Its odds: ${o}.`,
      wait: (time) => `⏳ ${time}`,
      gift: "🎁 Give", giftSearch: "Name or @username", giftHint: (n) => `The card goes with its points. Up to ${n} a day.`,
      giftTo: (name) => `🎁 Send to ${name}`, giftSent: (card, name) => `🎁 ${card} → ${name}`, giftNone: "No such player",
      giftLimit: (n) => `⏳ You may give ${n} cards a day. Again tomorrow.`, giftNoPlayer: "This player has not opened a card yet", giftGone: "You no longer have this card", back: "← Back",
      shop: "Shop", shopTitle: "Buy a card", shopHint: "Pick a category: a random card of it comes out, by the usual odds and sure cards.", anySet: "🎲 Any", packCards: (n) => `${n} cards`, needMore: (n) => `${n} more 💎`,
      market: "Market", newOffer: "➕ Make an offer", filterAll: "All", filterCan: "I can give", filterMine: "Mine",
      gives: "Gives", wants: "Wants", swap: "🔄 Trade", noCard: "You lack it", remove: "✕ Take down", marketEmpty: "No offers yet",
      marketHint: (n) => `Put up a card and pick the one you want for it. Whoever holds it can trade. Up to ${n} offers, for 7 days.`,
      step1: "1. Which card do you give?", step2: "2. Which card do you want?", mineSearch: "Search your cards", wantSearch: "Name of the card you want",
      place: "Put it up", placed: "✅ Your offer is up", swapped: (c) => `🔄 Traded! You got ${c}`, removed: "Offer taken down",
      refused: { over: "This offer is gone", gone: "This offer is gone", own: "This is your own offer", not_held: "You do not have that card", same: "A card cannot be traded for itself", no_spare: "Only a spare card may be given" },
      offersLimit: (n) => `You may have up to ${n} offers at once`, giftSpare: "Only a spare card (×2 or more) may be given",
    },
  };
  const language = ((tg && tg.initDataUnsafe && tg.initDataUnsafe.user && tg.initDataUnsafe.user.language_code) || "uz").slice(0, 2);
  const t = texts[language] || texts.uz;
  document.documentElement.lang = texts[language] ? language : "uz";
  for (const node of document.querySelectorAll("[data-t]")) node.textContent = t[node.dataset.t];
  $("search").placeholder = t.search;

  // The collections, in the order the page lists them.
  const SETS = [["anime", "🎌 Anime"], ["pokemon", "⚡ Pokémon"], ["onepiece", "🏴‍☠️ One Piece"], ["dragonball", "🐉 Dragon Ball"], ["yugioh", "🃏 Yu-Gi-Oh!"], ["digimon", "🦖 Digimon"]];
  const COLORS = ["var(--r0)", "var(--r1)", "var(--r2)", "var(--r3)", "var(--r4)"];
  const ODDS = ["50%", "28%", "14%", "6.5%", "1.5%"];
  const spaced = (n) => String(Math.round(n)).replace(/\B(?=(\d{3})+(?!\d))/g, " ");
  const haptic = (kind) => { try { tg.HapticFeedback.impactOccurred(kind); } catch (_) { /* outside Telegram */ } };

  let cards = [], points = 0, nextAt = 0, drawing = false, query = "", opened = "", chosenSet = "";
  let shards = 0, spare = 0, spareWorth = 0, extraCost = 15, setCost = 20, autoOn = false, buying = false;

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
      // A newcomer starts at the draw; anyone else at their collection.
      if (!opened) showTab(cards.some((c) => c.count > 0) ? "collection" : "draw");
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
    nextAt = Date.now() + data.next_in;
    shards = data.shards || 0;
    spare = data.spare || 0;
    spareWorth = data.spare_worth || 0;
    extraCost = data.extra_cost || extraCost;
    setCost = data.set_cost || setCost;
    autoOn = !!data.auto;
    autoSwitch();
    shardsPanel();
    notice(null);
    header();
    shelves();
    drawButton();
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
    img.src = card.image;
    const name = document.createElement("span");
    name.className = "name";
    name.textContent = card.name;
    tile.append(img, name);
    if (card.rarity === 4) tile.insertAdjacentHTML("beforeend", MYTHIC_DRESS);
    if (card.count > 1) {
      const count = document.createElement("span");
      count.className = "count";
      count.textContent = `×${card.count}`;
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
    img.src = card.image;
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
    front.append(img);
    front.append(plate);
    if (card.rarity === 4) {
      front.classList.add("mythic");
      front.insertAdjacentHTML("beforeend", MYTHIC_DRESS + '<em class="spark"></em><em class="spark"></em><div class="motes">' + "<i></i>".repeat(6) + "</div>");
    }
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
    const give = document.createElement("button");
    give.type = "button";
    give.className = "ghost gift-open";
    give.textContent = t.gift;
    give.disabled = !card.spare;
    give.title = card.spare ? "" : t.giftSpare;
    give.addEventListener("click", (event) => {
      event.stopPropagation();
      if (card.spare) giftPanel(card);
    });
    const giveNote = document.createElement("p");
    giveNote.className = "gift-note";
    giveNote.textContent = card.spare ? "" : t.giftSpare;
    sheet.append(face(card), statsOf(card), give, giveNote);
    tilt(sheet, card.rarity === 4);
    $("sheet").hidden = false;
    try { tg.BackButton.show(); } catch (_) { /* older clients */ }
    haptic("light");
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

  // ---------- giving a card ----------

  // giftPanel takes the large card's place with a search for the player to
  // give it to, a list to pick from, and the button that sends it.
  function giftPanel(card) {
    const sheet = $("sheetCard");
    tilt(sheet, false);
    sheet.classList.add("giving");
    const panel = document.createElement("div");
    panel.className = "gift";
    panel.addEventListener("click", (event) => event.stopPropagation());
    panel.innerHTML = `<div class="gift-head"><img alt=""><span><small></small><b></b></span></div><input type="search" autocomplete="off" enterkeyhint="search"><ul class="gift-list"></ul><p class="gift-hint"></p><button type="button" class="draw gift-send" disabled></button><button type="button" class="link gift-back"></button>`;
    panel.querySelector(".gift-head img").src = card.image;
    panel.querySelector(".gift-head small").replaceChildren(tierIcon(card.rarity), t.rarities[card.rarity]);
    panel.querySelector(".gift-head b").textContent = card.name;
    const input = panel.querySelector("input");
    input.placeholder = t.giftSearch;
    panel.querySelector(".gift-hint").textContent = t.giftHint(5);
    const send = panel.querySelector(".gift-send");
    send.textContent = t.gift;
    panel.querySelector(".gift-back").textContent = t.back;
    panel.querySelector(".gift-back").addEventListener("click", () => open(card));
    let chosen = null, timer = 0, asked = 0;
    const list = panel.querySelector(".gift-list");
    async function search() {
      const mine = ++asked;
      try {
        const data = await api("players", { query: input.value });
        if (mine !== asked) return;
        list.textContent = "";
        if (!data.players.length) list.innerHTML = `<li class="gift-empty">${t.giftNone}</li>`;
        for (const player of data.players) {
          const li = document.createElement("li");
          const pick = document.createElement("button");
          pick.type = "button";
          pick.innerHTML = "<span><b></b><i></i></span><small></small>";
          pick.querySelector("b").textContent = player.name || "?";
          pick.querySelector("i").textContent = player.username ? "@" + player.username : "";
          pick.querySelector("small").textContent = t.cards(player.cards);
          pick.setAttribute("aria-pressed", String(chosen && chosen.id === player.id));
          pick.addEventListener("click", () => {
            chosen = player;
            for (const other of list.querySelectorAll("button")) other.setAttribute("aria-pressed", String(other === pick));
            send.textContent = t.giftTo(player.name || "?");
            send.disabled = false;
            haptic("light");
          });
          li.append(pick);
          list.append(li);
        }
      } catch (error) {
        notice(error);
      }
    }
    input.addEventListener("input", () => { clearTimeout(timer); timer = setTimeout(search, 250); });
    send.addEventListener("click", async () => {
      if (!chosen) return;
      send.disabled = true;
      try {
        const data = await api("gift", { card: card.id, to: chosen.id });
        if (data.refused) {
          toast(data.refused === "limit" ? t.giftLimit(data.limit) : data.refused === "no_player" ? t.giftNoPlayer : data.refused === "no_spare" ? t.giftSpare : t.giftGone);
          send.disabled = false;
          return;
        }
        applyState(data);
        closeSheet();
        toast(t.giftSent(card.name, chosen.name || "?"));
        haptic("medium");
      } catch (error) {
        send.disabled = false;
        notice(error);
      }
    });
    sheet.append(panel);
    search();
  }

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

  function clock(ms) {
    const s = Math.ceil(ms / 1000);
    const m = Math.floor(s / 60);
    return `${m}:${String(s % 60).padStart(2, "0")}`;
  }

  function drawButton() {
    const wait = Math.max(0, nextAt - Date.now());
    $("draw").disabled = drawing || wait > 0;
    $("drawLabel").textContent = drawing ? t.drawing : wait > 0 ? t.wait(clock(wait)) : t.drawNow;
    // The button counts down; the line below it only says a card is ready.
    $("timer").textContent = wait > 0 || !cards.length ? "" : t.ready;
    $("readyDot").hidden = !cards.length || wait > 0 || drawing || opened === "draw";
  }
  setInterval(drawButton, 1000);

  // ---------- shards ----------

  function shardsPanel() {
    $("shardCount").textContent = spaced(shards);
    $("melt").textContent = spare ? t.melt(spare, spareWorth) : t.noSpare;
    $("melt").disabled = drawing || !spare;
    packs();
  }

  // The melt is asked once more, with what goes: the spare copies, by card,
  // and the crystals each makes, as it cannot be undone.
  const RATES = [1, 3, 8, 20, 50];
  $("melt").addEventListener("click", () => {
    if (drawing || !spare) return;
    const spares = cards.filter((c) => c.spare > 0).sort((a, b) => b.rarity - a.rarity);
    const panel = document.createElement("div");
    panel.className = "gift melt";
    panel.innerHTML = '<b class="step"></b><ul class="gift-list melt-list"></ul><p class="gift-hint"></p><button type="button" class="draw gift-send"></button><button type="button" class="link gift-back"></button>';
    panel.querySelector(".step").textContent = t.meltTitle;
    const list = panel.querySelector(".melt-list");
    for (const card of spares) {
      const li = document.createElement("li");
      li.className = "melt-row";
      li.append(mini(card));
      const gain = document.createElement("small");
      gain.textContent = `×${card.spare} → +${card.spare * (RATES[card.rarity] || 0)} 💎`;
      li.append(gain);
      list.append(li);
    }
    panel.querySelector(".gift-hint").textContent = t.meltWarn;
    const go = panel.querySelector(".gift-send");
    go.textContent = t.meltDo(spareWorth);
    panel.querySelector(".gift-back").textContent = t.cancel;
    panel.querySelector(".gift-back").addEventListener("click", closeSheet);
    go.addEventListener("click", async () => {
      go.disabled = true;
      try {
        const data = await api("melt");
        closeSheet();
        haptic("medium");
        if (data.melted) toast(t.melted(data.melted, data.gained));
        applyState(await api("state"));
      } catch (error) {
        go.disabled = false;
        notice(error);
      }
    });
    showPanel(panel);
  });

  // ---------- shop ----------

  // packs are the cards on sale: one of any collection, and one of each
  // collection there are cards of, for crystals.
  function packs() {
    const root = $("packs");
    root.textContent = "";
    const known = SETS.filter(([name]) => cards.some((c) => c.set === name));
    const offer = [["", t.anySet, cards.length, extraCost], ...known.map(([name, label]) => [name, label, cards.filter((c) => c.set === name).length, setCost])];
    for (const [set, label, count, cost] of offer) {
      const pack = document.createElement("button");
      pack.type = "button";
      pack.className = "pack";
      pack.disabled = buying || shards < cost;
      pack.innerHTML = "<b></b><small></small><span></span>";
      pack.querySelector("b").textContent = label;
      pack.querySelector("small").textContent = t.packCards(count);
      pack.querySelector("span").textContent = shards >= cost ? `💎 ${cost}` : t.needMore(cost - shards);
      pack.addEventListener("click", () => buy(set));
      root.append(pack);
    }
  }

  // buy draws a card for crystals, of the collection set or of any, and
  // shows it large.
  async function buy(set) {
    if (buying) return;
    buying = true;
    packs();
    haptic("medium");
    try {
      const data = await api("extra", set ? { set } : {});
      if (data.need) {
        toast(t.fewShards(data.need, data.have));
        return;
      }
      applyState(await api("state"));
      const card = cards.find((c) => c.id === data.card.id) || data.card;
      open(card);
      toast(`${data.new ? t.fresh : t.again(data.card.count)} · ${t.plus(spaced(data.card.value))}`);
      haptic(data.card.rarity >= 3 ? "heavy" : "light");
    } catch (error) {
      notice(error);
    } finally {
      buying = false;
      packs();
    }
  }

  // ---------- auto opener ----------

  function autoSwitch() {
    $("auto").setAttribute("aria-checked", String(autoOn));
  }

  let autoBusy = false;
  $("auto").addEventListener("click", async () => {
    if (autoBusy) return;
    autoBusy = true;
    autoOn = !autoOn;
    autoSwitch();
    try {
      const data = await api("auto", { on: autoOn });
      autoOn = !!data.auto;
      toast(autoOn ? t.autoOn : t.autoOff);
    } catch (error) {
      autoOn = !autoOn;
      notice(error);
    } finally {
      autoBusy = false;
      autoSwitch();
    }
  });

  $("draw").addEventListener("click", () => {
    if (Date.now() >= nextAt) reveal("draw");
  });

  // reveal draws a card, the hourly one or one for shards, and turns it.
  async function reveal(action) {
    if (drawing) return;
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
      const data = await api(action);
      if (data.wait || data.need) {
        if (data.wait) nextAt = Date.now() + data.wait;
        if (data.need) { shards = data.have; toast(t.fewShards(data.need, data.have)); }
        flip.classList.remove("shuffle");
        return;
      }
      const card = data.card;
      const front = face(card);
      front.id = "front";
      $("front").replaceWith(front);
      await new Promise((resolve) => {
        const img = front.querySelector("img");
        if (img.complete) resolve();
        else { img.onload = img.onerror = resolve; setTimeout(resolve, 2500); }
      });
      flip.classList.remove("shuffle");
      flip.classList.add("shown");
      haptic(card.rarity >= 3 ? "heavy" : "light");
      $("result").replaceChildren(...withTiers([
        data.guaranteed ? t.sure : "",
        `${data.new ? t.fresh : t.again(card.count)} · ${t.plus(spaced(card.value))}`,
        data.legendary_in ? t.pity(data.legendary_in, data.mythic_in) : "",
      ].filter(Boolean).join("\n")));
      $("drawnStats").replaceChildren(statsOf(card));
      lastDrawn = card.id;
      $("seeCollection").hidden = false;
      points = data.points;
      if (data.next_in) nextAt = Date.now() + data.next_in;
      if (data.shards !== undefined) shards = data.shards;
      if (card.count > 1) { spare += 1; spareWorth += [1, 3, 8, 20, 50][card.rarity] || 0; }
      const i = cards.findIndex((c) => c.id === card.id);
      // A copy drawn again is one more spare, which may be given or melted.
      card.spare = card.count > 1 ? ((i >= 0 && cards[i].spare) || 0) + 1 : 0;
      if (i >= 0) cards[i] = card;
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

  // The deck itself is pressed as often as the button: it draws too.
  $("deck").addEventListener("click", () => { if (!$("draw").disabled) $("draw").click(); });

  let lastDrawn = 0;
  $("seeCollection").addEventListener("click", () => {
    showTab("collection");
    const card = cards.find((c) => c.id === lastDrawn);
    const tile = [...document.querySelectorAll("#shelves .card")].find((el) => el.querySelector(".name") && card && el.querySelector(".name").textContent === card.name);
    if (tile) tile.scrollIntoView({ block: "center", behavior: "smooth" });
  });
  $("welcomeDraw").addEventListener("click", () => showTab("draw"));

  $("rates").replaceChildren(document.createTextNode(t.ratesLabel + " "), ...withTiers(RATES.map((n, r) => `{${r}}${n}`).join("  ")));

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
    box.querySelector("img").src = card.image;
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
    const shown = offers.filter((o) => offerFilter === "all" ? !o.mine : offerFilter === "can" ? o.can && !o.mine : o.mine);
    if (!shown.length) list.innerHTML = `<li class="empty">${t.marketEmpty}</li>`;
    for (const o of shown) {
      const li = document.createElement("li");
      li.className = "offer";
      li.innerHTML = '<div class="offer-cards"><div class="side"><em></em></div><span class="arrow" aria-hidden="true">⇄</span><div class="side"><em></em></div></div><div class="offer-foot"><small></small><button type="button"></button></div>';
      const sides = li.querySelectorAll(".side");
      sides[0].querySelector("em").textContent = t.gives;
      sides[0].append(mini(o.give));
      sides[1].querySelector("em").textContent = t.wants;
      sides[1].append(mini(o.want));
      li.querySelector(".offer-foot small").textContent = o.mine ? "" : o.who || "?";
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
        toast(data.refused === "limit" ? t.giftLimit(data.limit) : t.refused[data.refused] || t.unavailable);
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
  // card they want for it, found in the whole catalog by name.
  function offerPanel() {
    let give = null, want = null, timer = 0, asked = 0;
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

    function pickRow(list, card, chosen, onPick) {
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
      list.append(li);
    }
    function mineList() {
      const list = panel.querySelector(".pick-mine");
      list.textContent = "";
      const q = mineQ.value.trim().toLowerCase();
      const held = cards.filter((c) => c.count > 0 && (!q || c.name.toLowerCase().includes(q))).sort((a, b) => b.rarity - a.rarity).slice(0, 30);
      if (!held.length) list.innerHTML = `<li class="gift-empty">${t.none}</li>`;
      for (const card of held) pickRow(list, card, give, (c) => { give = c; wantQ.disabled = false; ready(); });
    }
    async function wantList() {
      const mine = ++asked;
      const list = panel.querySelector(".pick-want");
      if (!wantQ.value.trim()) { list.textContent = ""; return; }
      try {
        const data = await api("catalog", { query: wantQ.value });
        if (mine !== asked) return;
        list.textContent = "";
        if (!data.cards.length) list.innerHTML = `<li class="gift-empty">${t.none}</li>`;
        for (const card of data.cards) pickRow(list, card, want, (c) => { want = c; ready(); });
      } catch (error) {
        notice(error);
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
    try {
      const data = await api("top");
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
      notice(null);
    } catch (error) {
      notice(error);
    }
  }

  // ---------- tabs ----------

  function showTab(tab) {
    opened = tab;
    for (const button of document.querySelectorAll(".nav button")) button.classList.toggle("on", button.dataset.tab === tab);
    for (const name of ["collection", "draw", "market", "shop", "top"]) $(`tab-${name}`).hidden = name !== tab;
    if (tab === "top") ranking();
    if (tab === "market") market();
    drawButton();
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
    $("draw").disabled = true;
    return;
  }
  tg.ready();
  tg.expand();
  try { tg.setHeaderColor("#0b0a14"); tg.setBackgroundColor("#0b0a14"); } catch (_) { /* older clients */ }
  load();
})();
