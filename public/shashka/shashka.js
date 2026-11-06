// Shashka: Russian draughts against a friend, a stranger or Maria. The
// server owns the game: it says which moves are legal, plays them and keeps
// the clock. The page draws the board, sends the path the player taps and
// shows a move before the server confirms it, then takes the server's game.
// With ?mock=1 the page talks to an in-memory server instead, for trying it
// in a browser; without it the mock is never built.
"use strict";

(function () {
  const params = new URLSearchParams(location.search);
  const MOCK = params.get("mock") === "1";
  const tg = window.Telegram && window.Telegram.WebApp;
  const $ = (id) => document.getElementById(id);

  const texts = {
    uz: {
      title: "Shashka", tagline: "Rus shashkasi: do'st, raqib yoki Maria bilan",
      rating: "Reyting", wins: "G'alaba", losses: "Mag'lub", draws: "Durang",
      find: "⚔️ Raqib topish", invite: "👥 Do'stni chaqirish", bot: "🤖 Maria bilan", leaderboard: "🏆 Reyting",
      back: "‹ Orqaga", chooseLevel: "Qaysi daraja?",
      levels: { 1: "😊 Oson", 2: "🙂 O'rta", 3: "😈 Qiyin" },
      levelHints: { 1: "Endi o'rganayotganlar uchun", 2: "Maria o'ylab yuradi", 3: "Xato kechirilmaydi" },
      topTitle: "🏆 Eng yaxshi o'yinchilar", topEmpty: "Hali hech kim o'ynamagan.",
      record: (p) => `${p.wins} g'. · ${p.losses} m. · ${p.draws} d.`,
      searching: "Raqib qidirilmoqda…", cancel: "Bekor qilish",
      inviteTitle: "Do'stingizni kutyapmiz", inviteHint: "Do'stingiz havolani Telegram'da ochishi kerak. U kirishi bilan o'yin boshlanadi.",
      share: "📤 Ulashish", copy: "Nusxa olish", copied: "Havola nusxalandi", shareText: "Shashka o'ynaymizmi?",
      you: "Siz", waitingFriend: "Do'st kutilmoqda", botLabel: (level) => `🤖 Bot · ${texts.uz.levels[level] || ""}`,
      ratingLabel: (r) => `★ ${r}`,
      yourTurn: "Sizning yurishingiz", theirTurn: "Raqib o'ylayapti…", botTurn: "Maria o'ylayapti…",
      mustCapture: "Majburiy urish!", keepJumping: "Urishni davom ettiring",
      mustCaptureHint: "Urish majburiy: ura oladigan donani tanlang.", cantMove: "Bu dona yura olmaydi.",
      resign: "🏳️ Taslim bo'lish", resignConfirm: "Rostdan taslim bo'lasizmi?",
      win: "G'alaba! 🎉", lose: "Mag'lubiyat", draw: "Durang 🤝",
      reasons: {
        no_moves: { win: "Raqibda yurish qolmadi", lose: "Sizda yurish qolmadi", draw: "Yurish qolmadi" },
        resign: { win: "Raqib taslim bo'ldi", lose: "Siz taslim bo'ldingiz", draw: "Taslim" },
        timeout: { win: "Raqibning vaqti tugadi", lose: "Vaqtingiz tugadi", draw: "Vaqt tugadi" },
        draw_rule: { win: "Durang qoidasi", lose: "Durang qoidasi", draw: "Durang qoidasi bo'yicha" },
      },
      again: "🔁 Yana", home: "🏠 Bosh sahifa",
      loading: "Yuklanmoqda…", retry: "Qayta urinish",
      outside: "Shashka Telegram ichida ochiladi. Uni Maria bot orqali oching.",
      errors: {
        busy: "Biroz kuting va qayta urining.", not_found: "O'yin topilmadi.",
        taken: "Bu taklifni boshqa kishi qabul qilib bo'lgan.", illegal: "Bu yurish mumkin emas.",
        not_your_turn: "Hozir raqibning navbati.", over: "O'yin tugagan.",
        session: "Sahifa eskirdi. Uni bot orqali qaytadan oching.",
        forged: "Telegram tasdiqlamadi. Sahifani bot orqali oching.",
        unavailable: "Aloqa bo'lmadi. Qayta urinib ko'ring.", other: "Nimadir xato ketdi. Qayta urinib ko'ring.",
      },
      pieces: { w: "oq dona", W: "oq damka", b: "qora dona", B: "qora damka", ".": "bo'sh" },
    },
    ru: {
      title: "Шашки", tagline: "Русские шашки: с другом, соперником или Марией",
      rating: "Рейтинг", wins: "Победы", losses: "Пораж.", draws: "Ничьи",
      find: "⚔️ Найти соперника", invite: "👥 Позвать друга", bot: "🤖 С Марией", leaderboard: "🏆 Рейтинг",
      back: "‹ Назад", chooseLevel: "Какой уровень?",
      levels: { 1: "😊 Легко", 2: "🙂 Средне", 3: "😈 Сложно" },
      levelHints: { 1: "Для тех, кто только учится", 2: "Мария подумает над ходом", 3: "Ошибок не прощает" },
      topTitle: "🏆 Лучшие игроки", topEmpty: "Пока никто не играл.",
      record: (p) => `${p.wins} п. · ${p.losses} пор. · ${p.draws} н.`,
      searching: "Ищем соперника…", cancel: "Отмена",
      inviteTitle: "Ждём вашего друга", inviteHint: "Друг должен открыть ссылку в Telegram. Игра начнётся, как только он войдёт.",
      share: "📤 Поделиться", copy: "Скопировать", copied: "Ссылка скопирована", shareText: "Сыграем в шашки?",
      you: "Вы", waitingFriend: "Ждём друга", botLabel: (level) => `🤖 Бот · ${texts.ru.levels[level] || ""}`,
      ratingLabel: (r) => `★ ${r}`,
      yourTurn: "Ваш ход", theirTurn: "Соперник думает…", botTurn: "Мария думает…",
      mustCapture: "Бить обязательно!", keepJumping: "Продолжайте бить",
      mustCaptureHint: "Бить обязательно: выберите шашку, которая может бить.", cantMove: "Этой шашкой ходить нельзя.",
      resign: "🏳️ Сдаться", resignConfirm: "Точно сдаться?",
      win: "Победа! 🎉", lose: "Поражение", draw: "Ничья 🤝",
      reasons: {
        no_moves: { win: "У соперника нет ходов", lose: "У вас нет ходов", draw: "Нет ходов" },
        resign: { win: "Соперник сдался", lose: "Вы сдались", draw: "Сдача" },
        timeout: { win: "У соперника кончилось время", lose: "У вас кончилось время", draw: "Время вышло" },
        draw_rule: { win: "По правилу ничьей", lose: "По правилу ничьей", draw: "По правилу ничьей" },
      },
      again: "🔁 Ещё", home: "🏠 Главная",
      loading: "Загрузка…", retry: "Повторить",
      outside: "Шашки открываются внутри Telegram. Откройте их через бота Марию.",
      errors: {
        busy: "Подождите немного и попробуйте снова.", not_found: "Игра не найдена.",
        taken: "Это приглашение уже принял кто-то другой.", illegal: "Так ходить нельзя.",
        not_your_turn: "Сейчас ход соперника.", over: "Игра уже окончена.",
        session: "Страница устарела. Откройте её заново через бота.",
        forged: "Telegram не подтвердил вход. Откройте страницу через бота.",
        unavailable: "Нет связи. Попробуйте ещё раз.", other: "Что-то пошло не так. Попробуйте ещё раз.",
      },
      pieces: { w: "белая шашка", W: "белая дамка", b: "чёрная шашка", B: "чёрная дамка", ".": "пусто" },
    },
    en: {
      title: "Draughts", tagline: "Russian draughts with a friend, a rival or Maria",
      rating: "Rating", wins: "Wins", losses: "Losses", draws: "Draws",
      find: "⚔️ Find an opponent", invite: "👥 Invite a friend", bot: "🤖 Play Maria", leaderboard: "🏆 Leaderboard",
      back: "‹ Back", chooseLevel: "Which level?",
      levels: { 1: "😊 Easy", 2: "🙂 Medium", 3: "😈 Hard" },
      levelHints: { 1: "For those just learning", 2: "Maria thinks before she moves", 3: "No mistake goes unpunished" },
      topTitle: "🏆 Top players", topEmpty: "Nobody has played yet.",
      record: (p) => `${p.wins} W · ${p.losses} L · ${p.draws} D`,
      searching: "Looking for an opponent…", cancel: "Cancel",
      inviteTitle: "Waiting for your friend", inviteHint: "Your friend has to open the link in Telegram. The game starts as soon as they join.",
      share: "📤 Share", copy: "Copy link", copied: "Link copied", shareText: "Fancy a game of draughts?",
      you: "You", waitingFriend: "Waiting for a friend", botLabel: (level) => `🤖 Bot · ${texts.en.levels[level] || ""}`,
      ratingLabel: (r) => `★ ${r}`,
      yourTurn: "Your move", theirTurn: "Opponent is thinking…", botTurn: "Maria is thinking…",
      mustCapture: "You must capture!", keepJumping: "Keep capturing",
      mustCaptureHint: "Capturing is compulsory: pick a piece that can capture.", cantMove: "That piece can't move.",
      resign: "🏳️ Resign", resignConfirm: "Really resign?",
      win: "You won! 🎉", lose: "You lost", draw: "Draw 🤝",
      reasons: {
        no_moves: { win: "Your opponent has no moves", lose: "You have no moves", draw: "No moves left" },
        resign: { win: "Your opponent resigned", lose: "You resigned", draw: "Resigned" },
        timeout: { win: "Your opponent ran out of time", lose: "You ran out of time", draw: "Time ran out" },
        draw_rule: { win: "By the draw rule", lose: "By the draw rule", draw: "By the draw rule" },
      },
      again: "🔁 Again", home: "🏠 Home",
      loading: "Loading…", retry: "Try again",
      outside: "Draughts opens inside Telegram. Open it from the Maria bot.",
      errors: {
        busy: "Wait a moment and try again.", not_found: "Game not found.",
        taken: "Someone else has already accepted this invite.", illegal: "That move isn't allowed.",
        not_your_turn: "It's your opponent's move.", over: "The game is over.",
        session: "This page has expired. Open it again from the bot.",
        forged: "Telegram couldn't confirm you. Open the page from the bot.",
        unavailable: "No connection. Try again.", other: "Something went wrong. Try again.",
      },
      pieces: { w: "white man", W: "white king", b: "black man", B: "black king", ".": "empty" },
    },
  };

  const mockUser = MOCK ? { id: 1, first_name: "Siz", language_code: params.get("lang") || "uz" } : null;
  const user = (tg && tg.initDataUnsafe && tg.initDataUnsafe.user) || mockUser;
  const langCode = String((user && user.language_code) || navigator.language || "").toLowerCase();
  const lang = langCode.startsWith("ru") ? "ru" : langCode.startsWith("en") ? "en" : "uz";
  const t = texts[lang];
  document.documentElement.lang = lang;
  const initData = MOCK ? "mock" : (tg && tg.initData) || "";

  // ---------------------------------------------------------------- rules
  // Squares are the 32 dark squares, row by row from the top as White sees
  // the board. The page only needs geometry to draw a move before the server
  // confirms it; the full move generator is for the mock.
  const Rules = {
    rc(i) {
      const r = i >> 2;
      return [r, r % 2 === 0 ? 2 * (i % 4) + 1 : 2 * (i % 4)];
    },
    at(r, c) {
      if (r < 0 || r > 7 || c < 0 || c > 7 || (r + c) % 2 === 0) return -1;
      return r * 4 + (c >> 1);
    },
    color(p) {
      if (p === "w" || p === "W") return "white";
      if (p === "b" || p === "B") return "black";
      return null;
    },
    king(p) {
      return p === "W" || p === "B";
    },
    other(color) {
      return color === "white" ? "black" : "white";
    },
    promotes(color, sq) {
      return color === "white" ? sq >> 2 === 0 : sq >> 2 === 7;
    },
    between(a, b) {
      const [r1, c1] = Rules.rc(a);
      const [r2, c2] = Rules.rc(b);
      const dr = Math.sign(r2 - r1);
      const dc = Math.sign(c2 - c1);
      const out = [];
      if (Math.abs(r2 - r1) !== Math.abs(c2 - c1)) return out;
      for (let r = r1 + dr, c = c1 + dc; r !== r2; r += dr, c += dc) out.push(Rules.at(r, c));
      return out;
    },
    // captured lists the other side's pieces a path jumps over. Captured
    // pieces stay on the board until the move ends, so the board before the
    // move is the one to look at.
    captured(board, path) {
      const own = Rules.color(board[path[0]]);
      const out = [];
      for (let k = 1; k < path.length; k++) {
        for (const sq of Rules.between(path[k - 1], path[k])) {
          const side = Rules.color(board[sq]);
          if (side && side !== own && !out.includes(sq)) out.push(sq);
        }
      }
      return out;
    },
    apply(board, path) {
      const cells = board.split("");
      let piece = cells[path[0]];
      const color = Rules.color(piece);
      const captured = Rules.captured(board, path);
      if (path.slice(1).some((sq) => Rules.promotes(color, sq))) piece = piece.toUpperCase();
      cells[path[0]] = ".";
      for (const sq of captured) cells[sq] = ".";
      cells[path[path.length - 1]] = piece;
      return { board: cells.join(""), captured };
    },
  };

  // ----------------------------------------------------------------- state
  const S = {
    me: null,
    top: [],
    game: null,
    version: -1,
    optimistic: false,
    sending: false,
    sel: null,
    clock: null,
    animKey: "",
    animUntil: 0,
    searchStart: 0,
    screen: "notice",
    resultFor: 0,
    dropped: new Set(),
    pollTimer: 0,
  };

  const inflight = new Set();
  const mock = MOCK ? createMock() : null;

  // call posts one action. It never throws: a failure comes back as
  // {ok: false, error}. A second call of an action already on its way is
  // skipped, so polls and double taps never overlap.
  async function call(action, extra) {
    if (inflight.has(action)) return { ok: false, error: "in_flight", skipped: true };
    inflight.add(action);
    try {
      const body = Object.assign({ init_data: initData, action }, extra || {});
      let data;
      if (mock) {
        data = await mock.handle(body);
      } else {
        const response = await fetch("/api/shashka", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        });
        data = await response.json().catch(() => null);
      }
      if (!data || typeof data.ok !== "boolean") return { ok: false, error: "unavailable" };
      if (!data.ok && !data.error) data.error = "other";
      return data;
    } catch (error) {
      return { ok: false, error: "unavailable" };
    } finally {
      inflight.delete(action);
    }
  }

  function errorText(code) {
    if (code === "no_init_data" || code === "stale_init_data") return t.errors.session;
    return t.errors[code] || t.errors.other;
  }

  // -------------------------------------------------------- telegram glue
  function supports(version) {
    return Boolean(tg && typeof tg.isVersionAtLeast === "function" && tg.isVersionAtLeast(version));
  }

  function haptic(kind) {
    const feedback = tg && tg.HapticFeedback;
    if (!feedback || !supports("6.1")) return;
    try {
      if (kind === "select") feedback.selectionChanged();
      else if (kind === "success" || kind === "error" || kind === "warning") feedback.notificationOccurred(kind);
      else feedback.impactOccurred(kind);
    } catch (error) {
      // Haptics are a nicety; an old client without them is fine.
    }
  }

  function confirmAction(text, run) {
    if (supports("6.2")) {
      tg.showConfirm(text, (ok) => ok && run());
      return;
    }
    if (window.confirm(text)) run();
  }

  function openLink(url) {
    try {
      if (tg && tg.openTelegramLink && !MOCK) {
        tg.openTelegramLink(url);
        return;
      }
    } catch (error) {
      // Fall through to a plain window.
    }
    window.open(url, "_blank");
  }

  let toastTimer = 0;
  function toast(text) {
    const node = $("toast");
    node.textContent = text;
    node.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => (node.hidden = true), 2600);
  }

  function toastError(result) {
    if (result && !result.skipped) toast(errorText(result.error));
  }

  // --------------------------------------------------------------- screens
  function show(screen) {
    S.screen = screen;
    for (const node of document.querySelectorAll(".screen")) node.hidden = node.id !== screen;
    if (screen !== "game") $("result").hidden = true;
    updateBack();
    updateClosing();
    schedulePoll();
  }

  function backAction() {
    if (S.screen === "levels" || S.screen === "top") return () => show("lobby");
    if (S.screen === "searching") return cancelSearch;
    if (S.screen === "waiting") return cancelInvite;
    if (S.screen === "game" && S.game && S.game.status === "over") return goHome;
    return null;
  }

  function updateBack() {
    if (!tg || !supports("6.1")) return;
    if (backAction()) tg.BackButton.show();
    else tg.BackButton.hide();
  }

  function onBack() {
    const run = backAction();
    if (run) run();
  }

  function updateClosing() {
    if (!tg || !supports("6.2")) return;
    const playing = S.screen === "game" && S.game && S.game.status === "playing";
    try {
      if (playing) tg.enableClosingConfirmation();
      else tg.disableClosingConfirmation();
    } catch (error) {
      // Not every client has it.
    }
  }

  function notice(text, retry) {
    show("notice");
    $("noticeSpinner").hidden = Boolean(text);
    $("noticeText").textContent = text || t.loading;
    const button = $("noticeRetry");
    button.hidden = !retry;
    button.onclick = retry || null;
  }

  function translate() {
    document.title = t.title;
    for (const node of document.querySelectorAll("[data-t]")) node.textContent = t[node.dataset.t];
    for (const button of document.querySelectorAll("[data-level]")) {
      button.querySelector(".level-name").textContent = t.levels[button.dataset.level];
      button.querySelector(".level-hint").textContent = t.levelHints[button.dataset.level];
    }
    $("noticeRetry").textContent = t.retry;
  }

  // ----------------------------------------------------------------- lobby
  function renderLobby() {
    const me = S.me;
    if (!me) return;
    $("meName").textContent = me.name;
    $("meRating").textContent = me.rating;
    $("meWins").textContent = me.wins;
    $("meLosses").textContent = me.losses;
    $("meDraws").textContent = me.draws;
  }

  function renderTop() {
    const list = $("topList");
    list.replaceChildren();
    if (!S.top.length) {
      const item = document.createElement("li");
      item.append(element("p", "empty", t.topEmpty));
      list.append(item);
      return;
    }
    const meId = S.me && S.me.id;
    S.top.forEach((player, i) => list.append(rankRow(player, i === 0 ? "👑" : String(i + 1), player.id === meId)));
    if (S.me && !S.top.some((player) => player.id === meId)) {
      list.append(element("li", "gap", "⋯"));
      list.append(rankRow(S.me, "—", true));
    }
  }

  function rankRow(player, place, mine) {
    const item = document.createElement("li");
    if (mine) item.className = "me";
    const name = element("span", "rank-name", player.name);
    name.append(element("small", "", t.record(player)));
    item.append(element("span", "rank-place", place), name, element("span", "rank-rating", String(player.rating)));
    return item;
  }

  async function openTop() {
    renderTop();
    show("top");
    const result = await call("top");
    if (!result.ok) return;
    S.top = result.top || [];
    if (result.me) S.me = result.me;
    renderTop();
    renderLobby();
  }

  async function loadHello(first) {
    if (first) notice("");
    const hello = await call("hello");
    if (!hello.ok) {
      if (first) notice(errorText(hello.error), () => loadHello(true));
      else toastError(hello);
      return;
    }
    S.me = hello.me;
    S.top = hello.top || [];
    renderLobby();
    const active = hello.game && hello.game.status !== "over" ? hello.game : null;
    const code = first ? params.get("join") : "";
    if (code) {
      params.delete("join");
      const query = params.toString();
      history.replaceState(null, "", location.pathname + (query ? "?" + query : "") + location.hash);
      const joined = await call("join", { code });
      if (joined.ok) {
        applyGame(joined.game);
        return;
      }
      if (joined.error !== "own_game") toastError(joined);
    }
    if (active) applyGame(active);
    else if (hello.queued) beginSearching();
    else show("lobby");
  }

  function goHome() {
    if (S.game) S.dropped.add(S.game.id);
    S.game = null;
    S.sel = null;
    S.clock = null;
    show("lobby");
    loadHello(false);
  }

  // ------------------------------------------------------- starting games
  async function startBot(level) {
    haptic("select");
    const result = await call("bot", { level });
    if (result.ok) applyGame(result.game);
    else toastError(result);
    return result.ok;
  }

  async function startInvite() {
    haptic("select");
    const result = await call("invite");
    if (result.ok) applyGame(result.game);
    else toastError(result);
    return result.ok;
  }

  async function startQueue() {
    haptic("select");
    const result = await call("queue");
    if (!result.ok) {
      toastError(result);
      return false;
    }
    if (result.game) applyGame(result.game);
    else beginSearching();
    return true;
  }

  function beginSearching() {
    S.searchStart = Date.now();
    $("searchElapsed").textContent = "0:00";
    show("searching");
  }

  async function pollQueue() {
    const result = await call("queue");
    if (result.ok && result.game) applyGame(result.game);
  }

  async function cancelSearch() {
    show("lobby");
    const result = await call("unqueue");
    if (!result.ok) toastError(result);
  }

  async function cancelInvite() {
    const game = S.game;
    if (game) S.dropped.add(game.id);
    S.game = null;
    show("lobby");
    if (!game) return;
    const result = await call("cancel", { game: game.id });
    if (!result.ok && result.error !== "not_found") toastError(result);
  }

  function shareInvite() {
    const invite = S.game && S.game.invite;
    if (!invite) return;
    haptic("select");
    openLink("https://t.me/share/url?url=" + encodeURIComponent(invite.link) + "&text=" + encodeURIComponent(t.shareText));
  }

  function copyInvite() {
    const invite = S.game && S.game.invite;
    if (!invite) return;
    const done = () => {
      haptic("success");
      toast(t.copied);
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(invite.link).then(done, () => copyByHand(invite.link) && done());
    } else if (copyByHand(invite.link)) {
      done();
    }
  }

  function copyByHand(text) {
    const area = document.createElement("textarea");
    area.value = text;
    area.setAttribute("readonly", "");
    area.style.position = "fixed";
    area.style.opacity = "0";
    document.body.append(area);
    area.select();
    let ok = false;
    try {
      ok = document.execCommand("copy");
    } catch (error) {
      ok = false;
    }
    area.remove();
    return ok;
  }

  // ------------------------------------------------------------ the game
  // applyGame takes a game from the server and shows it. Older versions
  // are dropped, and while a move is on its way only its own answer counts.
  function applyGame(game, fromMove) {
    if (!game) return;
    if (S.dropped.has(game.id) && game.status !== "playing") return;
    const same = Boolean(S.game && S.game.id === game.id);
    if (same && game.version < S.version) return;
    if (same && S.sending && !fromMove) return;
    if (same && game.version === S.version && !S.optimistic) {
      setClock(game);
      return;
    }
    if (game.status === "over" && !game.black) {
      // An invite nobody took, cancelled from somewhere else.
      S.dropped.add(game.id);
      if (S.game && S.game.id === game.id) S.game = null;
      show("lobby");
      return;
    }
    const before = same ? S.game : null;
    S.game = game;
    S.version = game.version;
    S.optimistic = false;
    S.sel = null;
    setClock(game);

    if (game.status === "waiting") {
      $("inviteLink").textContent = game.invite ? game.invite.link : "";
      show("waiting");
      return;
    }
    const lastSq = game.last[game.last.length - 1];
    const opponentMoved = Boolean(before) && game.last.length > 0 &&
      Rules.color(game.board[lastSq]) === Rules.other(game.you) && before.last.join("-") !== game.last.join("-");
    if (S.screen !== "game") show("game");
    renderGame(before ? before.board : null);
    if (opponentMoved) haptic(game.captured.length ? "medium" : "soft");
    if (game.status === "over") {
      showResult(game);
    }
    updateBack();
    updateClosing();
    schedulePoll();
  }

  function setClock(game) {
    S.clock = game.status === "playing" && game.deadline_ms > 0
      ? { side: game.turn, ms: game.deadline_ms, at: performance.now(), asked: false }
      : null;
  }

  function myTurn() {
    const game = S.game;
    return Boolean(game && game.status === "playing" && game.turn === game.you && !S.optimistic);
  }

  function isCapture(board, path) {
    return Rules.captured(board, path).length > 0;
  }

  function renderGame(boardBefore) {
    const game = S.game;
    const theirs = Rules.other(game.you);
    renderBar($("oppBar"), theirs, false);
    renderBar($("myBar"), game.you, true);
    renderBoard();
    renderStatus();
    $("resignBtn").hidden = game.status !== "playing";
    animateLast(boardBefore);
    tickClocks();
  }

  function renderBar(bar, color, mine) {
    const game = S.game;
    const side = game[color];
    bar.querySelector(".chip").className = "chip pc " + (color === "white" ? "w" : "b");
    let name = side ? side.name : t.waitingFriend;
    if (mine) name = side && side.name && side.name !== t.you ? `${side.name} (${t.you})` : t.you;
    bar.querySelector(".who-name").textContent = name;
    let sub = "";
    if (side && side.bot) sub = t.botLabel(game.level);
    else if (side && side.rating != null) sub = t.ratingLabel(side.rating);
    bar.querySelector(".who-sub").textContent = sub;
    bar.classList.toggle("active", game.status === "playing" && game.turn === color);
  }

  function renderStatus() {
    const game = S.game;
    const node = $("status");
    node.className = "status";
    if (game.status !== "playing") {
      node.textContent = "";
      return;
    }
    if (game.turn !== game.you || S.optimistic) {
      node.textContent = opponentIsBot() ? t.botTurn : t.theirTurn;
      node.classList.add("wait");
      return;
    }
    if (S.sel && S.sel.prefix.length > 1) {
      node.textContent = t.keepJumping;
      node.classList.add("must");
    } else if (mustCapture()) {
      node.textContent = t.mustCapture;
      node.classList.add("must");
    } else {
      node.textContent = t.yourTurn;
    }
  }

  function opponentIsBot() {
    const side = S.game[Rules.other(S.game.you)];
    return Boolean(side && side.bot);
  }

  function mustCapture() {
    const game = S.game;
    return myTurn() && game.moves.length > 0 && isCapture(game.board, game.moves[0]);
  }

  function startsWith(path, prefix) {
    return prefix.every((sq, k) => path[k] === sq);
  }

  function renderBoard() {
    const game = S.game;
    const node = $("board");
    const flip = game.you === "black";
    const moves = myTurn() ? game.moves : [];
    const prefix = S.sel ? S.sel.prefix : [];
    const candidates = prefix.length ? moves.filter((path) => startsWith(path, prefix)) : [];
    const targets = new Set(candidates.filter((path) => path.length > prefix.length).map((path) => path[prefix.length]));
    const doomed = new Set(prefix.length > 1 ? Rules.captured(game.board, prefix) : []);
    const highlightStarts = !prefix.length && mustCapture() ? new Set(moves.map((path) => path[0])) : new Set();
    const last = new Set(game.last);

    // While the player is part-way through a multi-jump the piece is drawn
    // where it has got to.
    const cells = game.board.split("");
    const head = prefix.length > 1 ? prefix[prefix.length - 1] : -1;
    if (head >= 0) {
      const piece = cells[prefix[0]];
      cells[prefix[0]] = ".";
      cells[head] = piece;
    }

    const files = "abcdefgh";
    const fragment = document.createDocumentFragment();
    for (let row = 0; row < 8; row++) {
      for (let col = 0; col < 8; col++) {
        const r = flip ? 7 - row : row;
        const c = flip ? 7 - col : col;
        const sq = Rules.at(r, c);
        let cell;
        if (sq < 0) {
          cell = element("div", "sq light", "");
        } else {
          cell = document.createElement("button");
          cell.type = "button";
          cell.className = "sq dark";
          cell.dataset.sq = sq;
          if (last.has(sq) && !prefix.length) cell.classList.add("last");
          if (prefix.length && (sq === prefix[0] || sq === head)) cell.classList.add("sel");
          if (prefix.length > 1 && prefix.includes(sq) && sq !== head) cell.classList.add("path");
          if (targets.has(sq)) cell.classList.add("target");
          if (highlightStarts.has(sq)) cell.classList.add("can");
          const piece = cells[sq];
          cell.setAttribute("aria-label", `${files[c]}${8 - r}, ${t.pieces[piece] || ""}`);
          if (piece !== ".") cell.append(pieceNode(piece, doomed.has(sq)));
        }
        if (col === 0) cell.append(element("span", "coord rank", String(8 - r)));
        if (row === 7) cell.append(element("span", "coord file", files[c]));
        fragment.append(cell);
      }
    }
    node.replaceChildren(fragment);
  }

  function pieceNode(piece, doomed) {
    let name = "pc " + (Rules.color(piece) === "white" ? "w" : "b");
    if (Rules.king(piece)) name += " k";
    if (doomed) name += " doomed";
    return element("span", name, "");
  }

  function cellOf(sq) {
    return $("board").querySelector(`[data-sq="${sq}"]`);
  }

  // animateLast slides the piece of the last move along its path and fades
  // the pieces it took, once per move.
  function animateLast(boardBefore) {
    const game = S.game;
    const key = game.id + ":" + game.last.join("-");
    if (key === S.animKey) return;
    S.animKey = key;
    if (!boardBefore || game.last.length < 2) return;
    if (window.matchMedia && matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const path = game.last;
    const end = cellOf(path[path.length - 1]);
    const piece = end && end.querySelector(".pc");
    if (!piece || typeof piece.animate !== "function") return;
    const to = end.getBoundingClientRect();
    const frames = path.map((sq) => {
      const box = cellOf(sq).getBoundingClientRect();
      return { transform: `translate(${box.left - to.left}px, ${box.top - to.top}px)` };
    });
    frames[frames.length - 1] = { transform: "translate(0, 0)" };
    const duration = 170 * (path.length - 1) + 60;
    piece.classList.add("moving");
    piece.animate(frames, { duration, easing: "ease-in-out" }).onfinish = () => piece.classList.remove("moving");
    S.animUntil = performance.now() + duration;

    const mover = Rules.color(game.board[path[path.length - 1]]);
    for (const sq of game.captured) {
      const cell = cellOf(sq);
      if (!cell) continue;
      const was = boardBefore[sq] !== "." ? boardBefore[sq] : mover === "white" ? "b" : "w";
      const ghost = pieceNode(was, false);
      ghost.classList.add("ghost");
      ghost.style.animationDelay = `${Math.max(0, duration - 120)}ms`;
      ghost.addEventListener("animationend", () => ghost.remove());
      cell.append(ghost);
    }
  }

  function onBoardTap(event) {
    const cell = event.target.closest("[data-sq]");
    if (!cell) return;
    tapSquare(Number(cell.dataset.sq));
  }

  function tapSquare(sq) {
    const game = S.game;
    if (!game || S.sending || !myTurn()) return;
    const moves = game.moves || [];
    const prefix = S.sel ? S.sel.prefix : null;
    if (prefix) {
      const n = prefix.length;
      const next = moves.filter((path) => path.length > n && startsWith(path, prefix) && path[n] === sq);
      if (next.length) {
        const extended = prefix.concat(sq);
        const exact = next.find((path) => path.length === extended.length);
        if (exact) return play(exact);
        if (next.length === 1) return play(next[0]);
        S.sel = { prefix: extended };
        haptic("select");
        renderBoard();
        renderStatus();
        return;
      }
    }
    if (moves.some((path) => path[0] === sq)) {
      S.sel = prefix && prefix[0] === sq ? null : { prefix: [sq] };
      haptic("select");
    } else {
      if (Rules.color(game.board[sq]) === game.you) {
        toast(mustCapture() ? t.mustCaptureHint : t.cantMove);
        haptic("warning");
      }
      S.sel = null;
    }
    renderBoard();
    renderStatus();
  }

  async function play(path) {
    const game = S.game;
    const capture = isCapture(game.board, path);
    haptic(capture ? "medium" : "light");
    const next = Rules.apply(game.board, path);
    S.sending = true;
    S.sel = null;
    S.optimistic = true;
    S.clock = null;
    S.game = Object.assign({}, game, {
      board: next.board,
      last: path,
      captured: next.captured,
      moves: [],
      turn: Rules.other(game.you),
    });
    renderGame(game.board);
    const result = await call("move", { game: game.id, version: game.version, path });
    // Let the slide finish; a bot's answer then gets a beat of its own.
    let wait = S.animUntil - performance.now();
    if (result.ok && result.game && result.game.last.join("-") !== path.join("-")) wait += 250;
    if (wait > 0) await sleep(wait);
    S.sending = false;
    if (result.ok) {
      applyGame(result.game, true);
      return;
    }
    if (result.error !== "stale") toastError(result);
    if (S.game && S.game.id === game.id) {
      S.game = game;
      S.optimistic = false;
      S.animKey = game.id + ":" + game.last.join("-");
      renderGame(null);
    }
    pollState();
  }

  function resign() {
    const game = S.game;
    if (!game || game.status !== "playing") return;
    confirmAction(t.resignConfirm, async () => {
      const result = await call("resign", { game: game.id });
      if (result.ok) applyGame(result.game, true);
      else toastError(result);
    });
  }

  function showResult(game) {
    if (S.resultFor === game.id) return;
    S.resultFor = game.id;
    const result = game.result || { winner: null, reason: "draw_rule", rating_change: 0 };
    const outcome = result.winner == null ? "draw" : result.winner === game.you ? "win" : "lose";
    const title = $("resultTitle");
    title.textContent = t[outcome];
    title.className = "result-title " + outcome;
    const reasons = t.reasons[result.reason];
    $("resultReason").textContent = reasons ? reasons[outcome] : "";
    const change = $("resultChange");
    change.hidden = !game.rated || typeof result.rating_change !== "number";
    if (!change.hidden) {
      // The sign follows the outcome, whether the server sends the gain as
      // seen by the winner or by this player.
      const size = Math.abs(result.rating_change);
      const signed = outcome === "win" ? size : outcome === "lose" ? -size : result.rating_change;
      change.textContent = signed > 0 ? `+${signed}` : signed < 0 ? `−${-signed}` : "±0";
      change.className = "result-change " + (signed > 0 ? "up" : signed < 0 ? "down" : "");
    }
    $("result").hidden = false;
    haptic(outcome === "win" ? "success" : outcome === "lose" ? "error" : "warning");
  }

  async function playAgain() {
    const game = S.game;
    if (!game) return;
    S.dropped.add(game.id);
    $("result").hidden = true;
    let started;
    if (game.kind === "bot") started = await startBot(game.level || 1);
    else if (game.kind === "random") started = await startQueue();
    else started = await startInvite();
    if (!started) goHome();
  }

  // ---------------------------------------------------------- the clocks
  function tickClocks() {
    if (S.screen === "searching") {
      $("searchElapsed").textContent = minutes(Date.now() - S.searchStart);
    }
    if (S.screen !== "game" || !S.game) return;
    for (const [bar, color] of [[$("oppBar"), Rules.other(S.game.you)], [$("myBar"), S.game.you]]) {
      const clock = bar.querySelector(".clock");
      const running = S.clock && S.clock.side === color;
      clock.classList.toggle("on", Boolean(running));
      if (!running) continue;
      const left = Math.max(0, S.clock.ms - (performance.now() - S.clock.at));
      clock.textContent = minutes(left);
      clock.classList.toggle("low", left < 10000);
    }
    if (S.clock && !S.clock.asked && S.clock.ms - (performance.now() - S.clock.at) <= 0) {
      // The server decides a timeout; ask it once the clock runs out here.
      S.clock.asked = true;
      pollState();
    }
  }

  function minutes(ms) {
    const total = Math.ceil(ms / 1000);
    return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
  }

  // ------------------------------------------------------------- polling
  function pollJob() {
    if (S.screen === "searching") return { every: 2500, run: pollQueue };
    if (S.screen === "waiting" && S.game) return { every: 2000, run: pollState };
    const game = S.game;
    if (S.screen !== "game" || !game || game.status !== "playing") return null;
    if (game.turn !== game.you) return { every: 1500, run: pollState };
    // On your own move the other side can still resign or run your clock
    // out; a slow look keeps that from going unseen.
    if (!opponentIsBot()) return { every: 4000, run: pollState };
    return null;
  }

  function schedulePoll(delay) {
    clearTimeout(S.pollTimer);
    S.pollTimer = 0;
    if (document.hidden) return;
    const job = pollJob();
    if (!job) return;
    S.pollTimer = setTimeout(async () => {
      S.pollTimer = 0;
      await job.run();
      if (!S.pollTimer) schedulePoll();
    }, delay == null ? job.every : delay);
  }

  async function pollState() {
    const game = S.game;
    if (!game) return;
    const result = await call("state", { game: game.id });
    if (result.ok) {
      applyGame(result.game);
    } else if (result.error === "not_found" && S.game && S.game.id === game.id) {
      toastError(result);
      goHome();
    } else if (["no_init_data", "forged", "stale_init_data"].includes(result.error)) {
      toastError(result);
    }
  }

  // ------------------------------------------------------------ helpers
  function element(tag, className, text) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text) node.textContent = text;
    return node;
  }

  function sleep(ms) {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }

  // ---------------------------------------------------------------- start
  function start() {
    if (tg) {
      try {
        tg.ready();
        tg.expand();
        if (supports("7.7")) tg.disableVerticalSwipes();
      } catch (error) {
        // An old client: the page works without these.
      }
      if (tg.themeParams && Object.keys(tg.themeParams).length > 0) document.documentElement.dataset.tg = "1";
      if (supports("6.1")) tg.BackButton.onClick(onBack);
    }
    document.body.classList.toggle("no-tg-back", !supports("6.1"));
    translate();
    if (!initData) {
      notice(t.outside);
      return;
    }
    $("findBtn").addEventListener("click", startQueue);
    $("inviteBtn").addEventListener("click", startInvite);
    $("botBtn").addEventListener("click", () => {
      haptic("select");
      show("levels");
    });
    $("topBtn").addEventListener("click", openTop);
    for (const button of document.querySelectorAll("[data-level]")) {
      button.addEventListener("click", () => startBot(Number(button.dataset.level)));
    }
    for (const button of document.querySelectorAll("[data-back]")) button.addEventListener("click", onBack);
    $("searchCancel").addEventListener("click", cancelSearch);
    $("inviteCancel").addEventListener("click", cancelInvite);
    $("shareBtn").addEventListener("click", shareInvite);
    $("copyBtn").addEventListener("click", copyInvite);
    $("board").addEventListener("click", onBoardTap);
    $("resignBtn").addEventListener("click", resign);
    $("againBtn").addEventListener("click", playAgain);
    $("homeBtn").addEventListener("click", goHome);
    document.addEventListener("visibilitychange", () => {
      if (document.hidden) schedulePoll();
      else schedulePoll(0);
    });
    setInterval(tickClocks, 250);
    loadHello(true);
  }

  // ----------------------------------------------------------------- mock
  // createMock is an in-memory stand-in for /api/shashka with the same
  // contract, for ?mock=1 only. Its opponents play random legal moves
  // (the stronger bot levels prefer the biggest capture). ?clock=N sets the
  // seconds per move, ?board=<32 chars> the start position of a bot game,
  // ?lang=ru|en the language.
  function createMock() {
    const START = "bbbbbbbbbbbb........wwwwwwwwwwww";
    const startBoard = /^[.wWbB]{32}$/.test(params.get("board") || "") ? params.get("board") : START;
    const clockMs = (Number(params.get("clock")) || 60) * 1000;
    const me = { id: 1, name: mockUser.first_name, rating: 1200, wins: 3, losses: 2, draws: 1 };
    const others = [
      { id: 2, name: "Aziza", rating: 1342, wins: 30, losses: 11, draws: 2 },
      { id: 3, name: "Bekzod", rating: 1281, wins: 21, losses: 14, draws: 5 },
      { id: 4, name: "Дмитрий", rating: 1230, wins: 12, losses: 9, draws: 1 },
    ];
    const games = new Map();
    let nextId = 100;
    let queuedCalls = 0;
    let queued = false;

    const DIRS = [[-1, -1], [-1, 1], [1, -1], [1, 1]];

    function legal(board, color) {
      const own = (sq) => Rules.color(board[sq]) === color;
      const captures = [];
      for (let sq = 0; sq < 32; sq++) if (own(sq)) captures.push(...capturesFrom(board, sq));
      if (captures.length) return captures;
      const steps = [];
      for (let sq = 0; sq < 32; sq++) {
        if (!own(sq)) continue;
        const [r, c] = Rules.rc(sq);
        const king = Rules.king(board[sq]);
        for (const [dr, dc] of DIRS) {
          if (!king && dr !== (color === "white" ? -1 : 1)) continue;
          for (let k = 1; k < 8; k++) {
            const to = Rules.at(r + dr * k, c + dc * k);
            if (to < 0 || board[to] !== ".") break;
            steps.push([sq, to]);
            if (!king) break;
          }
        }
      }
      return steps;
    }

    function capturesFrom(board, from) {
      const cells = board.split("");
      const color = Rules.color(cells[from]);
      cells[from] = ".";
      const enemy = (sq) => Rules.color(cells[sq]) === Rules.other(color);
      const out = [];

      // jumps lists [taken, landing] pairs from a square; a king must land
      // where it can capture again if any landing allows it.
      function jumps(at, king, taken) {
        const [r, c] = Rules.rc(at);
        const found = [];
        for (const [dr, dc] of DIRS) {
          if (!king) {
            const mid = Rules.at(r + dr, c + dc);
            const land = Rules.at(r + 2 * dr, c + 2 * dc);
            if (mid >= 0 && land >= 0 && enemy(mid) && !taken.includes(mid) && cells[land] === ".") found.push([mid, land]);
            continue;
          }
          let k = 1;
          let sq = Rules.at(r + dr, c + dc);
          while (sq >= 0 && cells[sq] === ".") sq = Rules.at(r + dr * ++k, c + dc * k);
          if (sq < 0 || !enemy(sq) || taken.includes(sq)) continue;
          const lands = [];
          for (let j = k + 1; ; j++) {
            const land = Rules.at(r + dr * j, c + dc * j);
            if (land < 0 || cells[land] !== ".") break;
            lands.push(land);
          }
          const onward = lands.filter((land) => jumps(land, true, taken.concat(sq)).length > 0);
          for (const land of onward.length ? onward : lands) found.push([sq, land]);
        }
        return found;
      }

      function walk(at, king, taken, path) {
        const options = jumps(at, king, taken);
        if (!options.length) {
          if (path.length > 1) out.push(path);
          return;
        }
        for (const [sq, land] of options) {
          walk(land, king || Rules.promotes(color, land), taken.concat(sq), path.concat(land));
        }
      }

      walk(from, Rules.king(board[from]), [], [from]);
      return out;
    }

    function create(kind, extra) {
      const game = Object.assign({
        id: nextId++, kind, level: 0, status: "playing", you: "white",
        white: { name: me.name, rating: me.rating, bot: false }, black: null,
        board: START, turn: "white", last: [], captured: [], version: 1,
        rated: kind !== "bot", invite: null, result: null,
        turnAt: Date.now(), polls: 0, rules: { kingMoves: 0 },
      }, extra);
      games.set(game.id, game);
      return game;
    }

    function clockFor(game) {
      return game.kind === "bot" ? 0 : clockMs;
    }

    function view(game) {
      checkClock(game);
      const playing = game.status === "playing";
      const left = playing && clockFor(game) ? Math.max(0, clockFor(game) - (Date.now() - game.turnAt)) : 0;
      return {
        id: game.id, kind: game.kind, level: game.level, status: game.status, you: game.you,
        white: game.white, black: game.black, board: game.board, turn: game.turn,
        moves: playing && game.turn === game.you ? legal(game.board, game.you) : [],
        last: game.last, captured: game.captured, version: game.version, deadline_ms: left,
        rated: game.rated, invite: game.invite, result: game.result,
      };
    }

    function finish(game, winner, reason) {
      game.status = "over";
      const change = game.rated && winner ? 16 : 0;
      game.result = { winner, reason, rating_change: winner === game.you ? change : -change };
      if (winner === game.you) me.wins++;
      else if (winner) me.losses++;
      else me.draws++;
      me.rating += game.result.rating_change;
      game.version++;
    }

    function checkClock(game) {
      if (game.status === "playing" && clockFor(game) && Date.now() - game.turnAt > clockFor(game)) {
        finish(game, Rules.other(game.turn), "timeout");
      }
    }

    function move(game, path) {
      const before = game.board;
      const next = Rules.apply(before, path);
      const king = Rules.king(before[path[0]]);
      game.rules.kingMoves = king && !next.captured.length ? game.rules.kingMoves + 1 : 0;
      game.board = next.board;
      game.last = path;
      game.captured = next.captured;
      game.turn = Rules.other(game.turn);
      game.turnAt = Date.now();
      game.version++;
      if (!legal(game.board, game.turn).length) finish(game, Rules.other(game.turn), "no_moves");
      else if (game.rules.kingMoves >= 30) finish(game, null, "draw_rule");
    }

    function opponentMove(game, level) {
      const options = legal(game.board, game.turn);
      let pool = options;
      if (level >= 2 && isCapture(game.board, options[0])) {
        const most = Math.max(...options.map((path) => Rules.captured(game.board, path).length));
        pool = options.filter((path) => Rules.captured(game.board, path).length === most);
      }
      move(game, pool[Math.floor(Math.random() * pool.length)]);
    }

    function active() {
      for (const game of games.values()) if (game.status !== "over") return game;
      return null;
    }

    function route(body) {
      const game = games.get(Number(body.game));
      switch (body.action) {
        case "hello": {
          const current = active();
          return { ok: true, me, game: current ? view(current) : null, top: topList(), queued };
        }
        case "top":
          return { ok: true, top: topList(), me };
        case "bot": {
          const level = [1, 2, 3].includes(body.level) ? body.level : 1;
          const created = create("bot", { level, board: startBoard, black: { name: "Maria", rating: null, bot: true } });
          if (!legal(created.board, "white").length) finish(created, "black", "no_moves");
          return { ok: true, game: view(created) };
        }
        case "invite": {
          const code = Math.random().toString(36).slice(2, 8).toUpperCase();
          const created = create("friend", {
            status: "waiting",
            invite: { code, link: `https://t.me/maria_bot/shashka?startapp=join_${code}` },
          });
          return { ok: true, game: view(created) };
        }
        case "join": {
          if (body.code === "OWN") return { ok: false, error: "own_game" };
          if (body.code === "TAKEN") return { ok: false, error: "taken" };
          if (body.code === "GONE") return { ok: false, error: "not_found" };
          const joined = create("friend", {
            you: "black",
            white: { name: others[0].name, rating: others[0].rating, bot: false },
            black: { name: me.name, rating: me.rating, bot: false },
          });
          return { ok: true, game: view(joined) };
        }
        case "queue": {
          queued = true;
          queuedCalls++;
          if (queuedCalls < 3) return { ok: true, queued: true };
          queued = false;
          queuedCalls = 0;
          const rival = others[1];
          const you = Math.random() < 0.5 ? "white" : "black";
          const mine = { name: me.name, rating: me.rating, bot: false };
          const theirs = { name: rival.name, rating: rival.rating, bot: false };
          const matched = create("random", { you, white: you === "white" ? mine : theirs, black: you === "black" ? mine : theirs });
          return { ok: true, game: view(matched) };
        }
        case "unqueue":
          queued = false;
          queuedCalls = 0;
          return { ok: true };
        case "state": {
          if (!game) return { ok: false, error: "not_found" };
          if (game.status === "waiting" && ++game.polls >= 3) {
            game.status = "playing";
            game.black = { name: others[2].name, rating: others[2].rating, bot: false };
            game.turnAt = Date.now();
            game.version++;
          } else {
            checkClock(game);
            if (game.status === "playing" && game.turn !== game.you && Date.now() - game.turnAt > 1400) opponentMove(game, 1);
          }
          return { ok: true, game: view(game) };
        }
        case "move": {
          if (!game) return { ok: false, error: "not_found" };
          checkClock(game);
          if (game.status !== "playing") return { ok: false, error: "over" };
          if (game.turn !== game.you) return { ok: false, error: "not_your_turn" };
          if (body.version !== game.version) return { ok: false, error: "stale" };
          const wanted = (body.path || []).join("-");
          if (!legal(game.board, game.you).some((path) => path.join("-") === wanted)) return { ok: false, error: "illegal" };
          move(game, body.path);
          if (game.kind === "bot" && game.status === "playing") opponentMove(game, game.level);
          return { ok: true, game: view(game) };
        }
        case "resign":
          if (!game) return { ok: false, error: "not_found" };
          if (game.status !== "playing") return { ok: false, error: "over" };
          finish(game, Rules.other(game.you), "resign");
          return { ok: true, game: view(game) };
        case "cancel":
          if (!game || game.status !== "waiting") return { ok: false, error: "not_found" };
          game.status = "over";
          game.version++;
          return { ok: true };
        default:
          return { ok: false, error: "not_found" };
      }
    }

    function topList() {
      return others.concat(me).slice().sort((a, b) => b.rating - a.rating);
    }

    return {
      handle(body) {
        // A copy, as if it had crossed the network.
        return sleep(120 + Math.random() * 200).then(() => JSON.parse(JSON.stringify(route(body))));
      },
    };
  }

  start();
})();
