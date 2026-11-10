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
      fresh: "🆕 Yangi karta!", sure: "✨ Kafolatlangan karta!", pity: (l, m) => `🎯 Kafolat: 🔥 ${l} · 👑 ${m} kartadan keyin`, again: (n) => `🔁 Sizda endi ×${n}`, plus: (n) => `+${n} ochko`,
      attack: "Hujum", health: "Sog'liq", value: "Ochko", cards: (n) => `${n} karta`, pts: "ochko",
      none: "Hech narsa topilmadi", noTop: "Hali hech kim karta olmagan",
      noTelegram: "Ilovani Telegram'da oching: botga /kartalar yozing.", stale: "Ilova eskirdi, uni qayta oching.",
      unavailable: "Aloqa uzildi, birozdan keyin urinib ko'ring.",
      allSets: "Hammasi", pointsLabel: "ochko", loading: "Kartalar yuklanmoqda…",
      welcomeTitle: "Kolleksiyangiz bo'sh", welcomeText: "Har soatda bitta tasodifiy anime karta oling va hammasini to'plang.",
      welcomeButton: "Birinchi kartani olish", seeCollection: "Kolleksiyada ko'rish →",
      locked: (r, o) => `🔒 Bu ${r.toLowerCase()} karta hali yopiq. Uning tushish ehtimoli: ${o}.`,
      wait: (time) => `⏳ ${time}`,
    },
    ru: {
      title: "Карты", collected: "собрано", collection: "Коллекция", draw: "Получить", top: "Рейтинг",
      search: "Поиск по картам", rarities: ["Обычная", "Редкая", "Эпическая", "Легендарная", "Мифическая"],
      drawNow: "Получить карту", drawing: "Открываем…", next: (t) => `Следующая карта: ${t}`, ready: "Карта готова!",
      fresh: "🆕 Новая карта!", sure: "✨ Гарантированная карта!", pity: (l, m) => `🎯 Гарантия: 🔥 через ${l} · 👑 через ${m} карт`, again: (n) => `🔁 Теперь у вас ×${n}`, plus: (n) => `+${n} очков`,
      attack: "Атака", health: "Здоровье", value: "Очки", cards: (n) => `${n} карт`, pts: "очков",
      none: "Ничего не найдено", noTop: "Ещё никто не получил карту",
      noTelegram: "Откройте приложение в Telegram: напишите боту /kartalar.", stale: "Приложение устарело, откройте его снова.",
      unavailable: "Нет связи, попробуйте чуть позже.",
      allSets: "Все", pointsLabel: "очки", loading: "Загружаем карты…",
      welcomeTitle: "Коллекция пуста", welcomeText: "Каждый час получайте случайную аниме-карту и соберите все.",
      welcomeButton: "Получить первую карту", seeCollection: "Посмотреть в коллекции →",
      locked: (r, o) => `🔒 Эта карта (${r.toLowerCase()}) ещё закрыта. Шанс выпадения: ${o}.`,
      wait: (time) => `⏳ ${time}`,
    },
    en: {
      title: "Cards", collected: "collected", collection: "Collection", draw: "Draw", top: "Ranking",
      search: "Search cards", rarities: ["Common", "Rare", "Epic", "Legendary", "Mythic"],
      drawNow: "Draw a card", drawing: "Opening…", next: (t) => `Next card in ${t}`, ready: "A card is ready!",
      fresh: "🆕 A new card!", sure: "✨ A guaranteed card!", pity: (l, m) => `🎯 Sure: 🔥 in ${l} · 👑 in ${m} cards`, again: (n) => `🔁 You now have ×${n}`, plus: (n) => `+${n} points`,
      attack: "Attack", health: "Health", value: "Points", cards: (n) => `${n} cards`, pts: "points",
      none: "Nothing found", noTop: "Nobody has drawn a card yet",
      noTelegram: "Open the app in Telegram: send /kartalar to the bot.", stale: "The app is out of date; open it again.",
      unavailable: "No connection; try again in a bit.",
      allSets: "All", pointsLabel: "points", loading: "Loading the cards…",
      welcomeTitle: "Your collection is empty", welcomeText: "Draw a random anime card every hour and collect them all.",
      welcomeButton: "Draw your first card", seeCollection: "See it in the collection →",
      locked: (r, o) => `🔒 This ${r.toLowerCase()} card is still locked. Its odds: ${o}.`,
      wait: (time) => `⏳ ${time}`,
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

  // ---------- server ----------

  async function api(action) {
    const response = await fetch("/api/cards", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ init_data: tg.initData, action }),
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
      const data = await api("state");
      cards = data.cards;
      points = data.points;
      nextAt = Date.now() + data.next_in;
      notice(null);
      $("loading").hidden = true;
      header();
      shelves();
      drawButton();
      // A newcomer starts at the draw; anyone else at their collection.
      if (!opened) showTab(cards.some((c) => c.count > 0) ? "collection" : "draw");
    } catch (error) {
      $("loading").hidden = true;
      if (!opened) showTab("collection");
      notice(error);
    }
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
    const img = new Image();
    img.loading = "lazy";
    img.alt = "";
    img.src = card.image;
    const name = document.createElement("span");
    name.className = "name";
    name.textContent = card.name;
    tile.append(img, name);
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
      shelf.querySelector("h2").textContent = t.rarities[rarity];
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
    rarity.textContent = t.rarities[card.rarity];
    const name = document.createElement("b");
    name.textContent = card.name;
    const anime = document.createElement("small");
    anime.textContent = card.anime;
    plate.append(rarity, name, anime);
    front.append(img, plate);
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
    sheet.append(face(card), statsOf(card));
    $("sheet").hidden = false;
    try { tg.BackButton.show(); } catch (_) { /* older clients */ }
    haptic("light");
  }
  function closeSheet() {
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

  function clock(ms) {
    const s = Math.ceil(ms / 1000);
    const m = Math.floor(s / 60);
    return `${m}:${String(s % 60).padStart(2, "0")}`;
  }

  function drawButton() {
    const wait = Math.max(0, nextAt - Date.now());
    $("draw").disabled = drawing || wait > 0;
    $("drawLabel").textContent = drawing ? t.drawing : wait > 0 ? t.wait(clock(wait)) : t.drawNow;
    $("timer").textContent = wait > 0 ? t.next(clock(wait)) : cards.length ? t.ready : "";
    $("readyDot").hidden = !cards.length || wait > 0 || drawing || opened === "draw";
  }
  setInterval(drawButton, 1000);

  $("draw").addEventListener("click", async () => {
    if (drawing || Date.now() < nextAt) return;
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
      const data = await api("draw");
      if (data.wait) {
        nextAt = Date.now() + data.wait;
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
      $("result").textContent = [
        data.guaranteed ? t.sure : "",
        `${data.new ? t.fresh : t.again(card.count)} · ${t.plus(spaced(card.value))}`,
        data.legendary_in ? t.pity(data.legendary_in, data.mythic_in) : "",
      ].filter(Boolean).join("\n");
      $("drawnStats").replaceChildren(statsOf(card));
      lastDrawn = card.id;
      $("seeCollection").hidden = false;
      points = data.points;
      nextAt = Date.now() + data.next_in;
      const i = cards.findIndex((c) => c.id === card.id);
      if (i >= 0) cards[i] = card;
      header();
      shelves();
    } catch (error) {
      flip.classList.remove("shuffle");
      notice(error);
    } finally {
      drawing = false;
      drawButton();
    }
  });

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

  const odds = $("odds");
  for (let rarity = 4; rarity >= 0; rarity--) {
    const li = document.createElement("li");
    li.style.setProperty("--c", COLORS[rarity]);
    li.innerHTML = `<b></b> ${ODDS[rarity]}`;
    li.querySelector("b").textContent = t.rarities[rarity];
    odds.append(li);
  }

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
    for (const name of ["collection", "draw", "top"]) $(`tab-${name}`).hidden = name !== tab;
    if (tab === "top") ranking();
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
