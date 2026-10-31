// Mafia's page: the game of the group the Play press came from, as the
// server lets this player see it, asked for every two seconds. The server
// keeps the roles and moves the game on; the page only shows it and sends
// the player's picks: joining, starting, the night's acts and the vote.
(() => {
  const $ = (id) => document.getElementById(id);
  const session = new URLSearchParams(location.search).get("s") || "";
  const COLORS = ["#ffd180", "#82b1ff", "#b9f6ca", "#ff8a80", "#ea80fc", "#84ffff", "#ccff90", "#ffe57f"];
  const colorOf = (id) => COLORS[Math.abs(Number(id)) % COLORS.length];

  const ABOUT = {
    don: "Mafiya boshlig'i. Qurbonni oxirgi so'z bilan siz tanlaysiz va har tuni komissarni qidirib bir kishini tekshirasiz.",
    mafiya: "Sheriklaringiz bilan har tuni bir kishini tanlaysiz. Kunduzi o'zingizni tinch aholi qilib ko'rsating.",
    komissar: "Har tuni bir kishini tekshirasiz: mafiyami yoki yo'q. Bilganingizni kunduzi ehtiyot bo'lib ayting.",
    shifokor: "Har tuni bir kishini (o'zingizni ham) hujumdan saqlaysiz. Bir kishini ketma-ket ikki kecha emas.",
    putana: "Har tuni bir kishining uyiga borasiz: u shu kechasi qobiliyatini ishlata olmaydi.",
    manyak: "Yolg'iz qotil. Har tuni xohlaganingizni o'ldirasiz. Oxirida tirik qolsangiz yutasiz.",
    omadli: "Tungi tugmangiz yo'q, lekin birinchi hujumdan omon qolasiz.",
    tinch: "Tungi qobiliyatingiz yo'q. Kunduzi guruhda bahslashib, mafiyani toping va ovoz bering.",
  };
  const TEAMS = { town: "🏛 Tinch aholi jamoasi", mafia: "🩸 Mafiya jamoasi", solo: "🎭 Yolg'iz" };
  const ACTS = {
    kill: { prompt: "Kimni o'ldiramiz?", hint: "Sheriklaringiz tanlovi 🔪 bilan ko'rinadi", verb: "🔪", danger: true },
    find: { prompt: "Komissarni qidiring: kimni tekshirasiz?", hint: "Natijani faqat siz ko'rasiz", verb: "🔍" },
    check: { prompt: "Kimni tekshirasiz?", hint: "Natijani faqat siz ko'rasiz", verb: "🔍" },
    heal: { prompt: "Kimni davolaysiz?", hint: "O'zingizni ham tanlash mumkin", verb: "💉" },
    block: { prompt: "Kimning uyiga borasiz?", hint: "U bu kecha qobiliyatini ishlata olmaydi", verb: "💋" },
    maniac: { prompt: "Bu kecha kimni o'ldirasiz?", hint: "Hech kim bilmaydi", verb: "🔪", danger: true },
  };
  const MARKS = { mafia: "🤵 mafiya!", town: "✅ mafiya emas", commissar: "🕵️‍♂️ komissar!", not: "komissar emas" };
  const WINNERS = { town: "🏛 Tinch aholi yutdi!", mafia: "🩸 Mafiya yutdi!", solo: "🦹‍♂️ Manyak yutdi!", none: "🤝 Hech kim yutmadi" };
  const ERRORS = {
    group: "Mafia guruhda o'ynaladi: o'yinni guruhga yuboring (/mafia).",
    session: "O'yin havolasi eskirdi. Telegram'da «Play» ni qaytadan bosing.",
    started: "O'yin boshlangan — keyingisini kuting.",
    full: "Xona to'la.",
    too_few: "Boshlash uchun kamida 5 kishi kerak.",
    phase: "Hozir buni qilib bo'lmaydi.",
    dead: "Siz o'yindan chiqqansiz.",
    target: "Bu o'yinchini tanlab bo'lmaydi.",
    repeating: "Bir kishini ketma-ket ikki kecha davolab bo'lmaydi.",
    no_act: "Rolingiz buni qila olmaydi.",
    unavailable: "Aloqa bo'lmadi, qayta urinib ko'ring.",
    no_words: "So'nggi so'z vaqti o'tdi.",
    words: "So'nggi so'z 1 dan 200 belgigacha bo'lsin.",
    chat_closed: "Hozir chat sizga yopiq.",
    line: "Xabar 1 dan 200 belgigacha bo'lsin.",
    too_fast: "Biroz sekinroq 🙂",
    room: "Chat almashdi (faza o'zgardi) — xabar yuborilmadi.",
  };
  // CHIPS are the quick lines of each room and phase.
  const CHIPS = {
    lobby: ["Boshlaymizmi? 🎭", "Yana kim bor? 👥", "Tayyorman ✅"],
    mafia: ["Kimni tanlaymiz? 🔪", "Men tanladim ✅", "Komissar kim bo'lishi mumkin? 🕵️‍♂️"],
    town: ["Men tinch aholiman 😇", "Komissar, gapir! 🕵️‍♂️", "Kim shubhali? 🤔"],
    over: ["Yaxshi o'yin edi 🤝", "Yana bir o'yin? 🎭"],
  };

  let state = null, ends = 0, picked = null, busy = false, overlaySeq = null, cardFor = null, fatal = "";
  let shownNews = null, closing = 0, draft = "", heardTo = "";

  // ---------- server ----------

  async function api(action, extra) {
    const response = await fetch("/api/mafia", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ session, action, ...extra }),
    });
    const data = await response.json().catch(() => ({ error: "unavailable" }));
    if (data.error) {
      if (data.state) apply(data.state);
      throw new Error(data.error);
    }
    apply(data);
    return data;
  }

  // send asks the server for one of the player's moves and reports whether
  // it was taken.
  async function send(action, extra) {
    if (busy) return false;
    busy = true;
    message("");
    try {
      await api(action, extra);
      return true;
    } catch (error) {
      if (error.message === "group" || error.message === "session") fatal = ERRORS[error.message];
      message(ERRORS[error.message] || ERRORS.unavailable);
      render();
      return false;
    } finally {
      busy = false;
    }
  }

  function buzz(pattern) {
    try { if (navigator.vibrate) navigator.vibrate(pattern); } catch (error) { /* no vibration here */ }
  }

  async function poll() {
    if (busy || document.hidden || fatal || !session) return;
    try {
      await api("state", {});
    } catch (error) {
      if (error.message === "group" || error.message === "session") { fatal = ERRORS[error.message]; render(); }
    }
  }

  function message(text) { $("message").textContent = text || ""; }

  // ---------- what the server says ----------

  function apply(view) {
    const before = state;
    state = view;
    ends = Date.now() + (view.left_ms || 0);
    if (!before || before.seq !== view.seq) picked = null;
    const me = view.me || {};
    const playing = ["night", "day", "vote"].includes(view.phase);
    const game = view.round + ":" + (view.players || []).map((p) => p.id).join(",");
    // News shows once, when it is new: the morning's as the day begins, the
    // verdict's as the night does. What a page opens on is only listed.
    const news = JSON.stringify(view.news || []);
    if (!before) shownNews = news;
    const died = before && before.me && before.me.alive && me.joined && me.alive === false;
    if (playing && me.role && cardFor !== game && !shown(game)) { cardFor = game; showCard(me); }
    else if (before && before.seq !== view.seq && view.phase === "over") showEnd(view);
    else if (died && me.can_speak) showDeath(view);
    else if (news !== shownNews && (view.phase === "day" || view.phase === "night") && (view.news || []).length) showNews(view);
    shownNews = news;
    if (died) buzz([200, 80, 200]);
    else if (before && before.phase !== view.phase && playing) buzz(me.pending && me.pending.length ? [40, 60, 40] : 25);
    render();
  }

  // shown tells whether this game's role card was opened on this device
  // before, and remembers that it now has been.
  function shown(game) {
    const key = "mafia-card:" + game;
    try {
      if (sessionStorage.getItem(key)) return true;
      sessionStorage.setItem(key, "1");
    } catch (error) {
      // Without storage the card shows again after a reload.
    }
    return false;
  }

  // ---------- overlays ----------

  function overlay(html, after) {
    clearTimeout(closing);
    const o = $("overlay");
    o.innerHTML = html;
    o.hidden = false;
    const close = () => { o.hidden = true; if (after) after(); };
    const button = o.querySelector("[data-close]");
    if (button) button.addEventListener("click", close);
    return close;
  }

  // showCard deals the player's role face down, to be turned over; open
  // shows it turned already, as the role button does.
  function showCard(me, open) {
    const close = overlay(`<div><div class="card${open ? " open" : ""}" id="card"><div class="inner">
      <div class="face back">🎭<br>Rolingiz<div style="font-size:14px;font-family:system-ui;margin-top:10px;color:#cbbfe6">ochish uchun bosing</div></div>
      <div class="face front ${esc(me.team || "town")}"><div class="big">${esc((me.role_name || "").split(" ")[0])}</div><h2>${esc((me.role_name || "").split(" ").slice(1).join(" "))}</h2>
      <div class="team">${TEAMS[me.team] || ""}</div><p>${ABOUT[me.role] || ""}</p></div>
    </div></div><div class="hint">Rolingizni hech kimga ko'rsatmang 🤫</div>
    <div class="row" style="margin-top:12px"><button class="btn main" data-close ${open ? "" : "hidden"}>Tushundim</button></div></div>`);
    const card = $("card");
    card.addEventListener("click", () => {
      card.classList.add("open");
      setTimeout(() => { const b = $("overlay").querySelector("[data-close]"); if (b) b.hidden = false; }, 600);
    });
    return close;
  }

  function showNews(view) {
    const morning = view.phase === "day";
    const close = overlay(`<div class="announce"><div class="big">${morning ? "☀️" : "⚖️"}</div><h2>${morning ? "Tong otdi" : "Hukm"}</h2>
      ${(view.news || []).map((n) => `<p>${esc(n)}</p>`).join("")}<div class="row" style="margin-top:14px"><button class="btn main" data-close>Davom etish</button></div></div>`);
    closing = setTimeout(close, 8000);
  }

  // showDeath tells a player they fell, and asks for their last word.
  function showDeath(view) {
    const voted = view.phase === "night";
    overlay(`<div class="announce"><div class="big">${voted ? "⚖️" : "💀"}</div><h2>${voted ? "Sizni shahardan chiqarishdi" : "Sizni o'ldirishdi"}</h2>
      <p>So'nggi so'zingizni yozing — bot uni guruhga yuboradi. Faqat bir marta.</p>${speakForm(view)}
      <div class="row" style="margin-top:8px"><button class="btn" data-close>Keyinroq</button></div></div>`);
  }

  function speakForm(view) {
    const most = view.max_words || 200;
    return `<div class="speak"><textarea class="words" rows="3" maxlength="${most}" placeholder="Masalan: Bek — mafiya, ishonmang!">${esc(draft)}</textarea>
      <div class="row"><span class="count">${[...draft].length}/${most}</span><button class="btn main" data-speak>🕯 Guruhga yuborish</button></div></div>`;
  }

  function showEnd(view) {
    if (overlaySeq === view.seq) return;
    overlaySeq = view.seq;
    const me = view.me || {};
    const won = me.team && me.team === view.winner;
    const title = WINNERS[view.winner] || "O'yin tugadi";
    overlay(`<div class="announce"><h2>${title}</h2>
      <p>${me.joined ? (won ? "🏆 Siz g'oliblar safidasiz! Dev jangi tangasi berildi." : view.winner === "none" ? "Durang: hamma halok bo'ldi." : "😤 Bu safar yutqazdingiz") : ""}</p>
      <table>${(view.players || []).map((p) => `<tr class="${p.alive ? "" : "dead"}${isMe(p) ? " me" : ""}"><td>${p.alive ? "" : "💀 "}${esc(p.name)}</td><td>${esc(p.role_name || "")}</td></tr>`).join("")}</table>
      <div class="row"><button class="btn main" data-close>Yopish</button></div></div>`);
  }

  // ---------- drawing ----------

  const isMe = (p) => state && state.me && state.me.joined && p.id === myId();
  function myId() {
    try { return JSON.parse(atob(session.split(".")[0].replace(/-/g, "+").replace(/_/g, "/"))).u; } catch (error) { return 0; }
  }
  function esc(text) { return String(text ?? "").replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c])); }

  function pendingAct() {
    const me = state && state.me;
    return state && state.phase === "night" && me && me.pending && me.pending.length ? me.pending[0] : null;
  }

  function pickable(p) {
    const me = state.me || {};
    const act = pendingAct();
    if (act) return act.targets.includes(p.id);
    if (state.phase === "vote") return me.alive && p.alive && !isMe(p);
    return false;
  }

  function avatar(p) {
    const initial = `<div class="ava" style="background:${colorOf(p.id)}">${esc((p.name || "?").slice(0, 1).toUpperCase())}</div>`;
    if (!p.avatar) return initial;
    return `<img class="ava" alt="" src="${esc(p.avatar)}" onerror="this.outerHTML=this.dataset.f" data-f="${esc(initial)}">`;
  }

  function render() {
    const phase = state ? state.phase : "";
    const me = (state && state.me) || {};
    const roleShown = Boolean(me.role) && ["night", "day", "vote"].includes(phase);
    $("role").hidden = !roleShown;
    $("title").hidden = roleShown;
    if (roleShown) $("role").textContent = me.role_name;
    document.body.className = phase === "day" || phase === "vote" ? "day" : "";
    $("phase").textContent = !state ? "…" : { "": "Xona yo'q", lobby: `Xona · ${state.players.length}/${state.max_players}`, night: `🌙 ${state.day}-tun`, day: `☀️ ${state.day}-kun`, vote: "🗳 Ovoz", over: "🏁 Tugadi" }[phase];
    renderGrid();
    renderCenter();
    renderChat();
    renderBar();
  }

  // renderChat shows the chat this player may read, redrawn only when a
  // line came or the room changed, kept at its end when it was there; the
  // box to write in shows while a room is open to them.
  function renderChat() {
    const feed = $("feed"), box = $("chatbox");
    const me = (state && state.me) || {};
    const lines = (state && state.chat) || [];
    const room = fatal ? "" : me.room || "";
    feed.hidden = !state || fatal || (!room && !lines.length);
    box.hidden = !room;
    box.classList.toggle("mafia", room === "mafia");
    // The notes take the room the chat leaves; without a chat, all of it.
    $("center").style.flex = feed.hidden ? "1 1 auto" : "0 1 auto";
    if (feed.hidden) return;
    const key = `${room}|${lines.length ? lines[lines.length - 1].id : 0}|${lines.length}`;
    if (key !== heardTo) {
      const atEnd = feed.scrollHeight - feed.scrollTop - feed.clientHeight < 40;
      heardTo = key;
      const title = room === "mafia" ? `<div class="room mafia">🤵 Mafiya chati — faqat sheriklaringiz ko'radi</div>` : `<div class="room">💬 O'yinchilar chati</div>`;
      const body = lines.length ? lines.map((l) => `<div class="ln${l.room === "mafia" ? " mafia" : ""}${l.from === myId() ? " mine" : ""}"><b style="color:${colorOf(l.from)}">${esc(l.name)}</b>${esc(l.text)}</div>`).join("")
        : `<div class="empty">${room === "mafia" ? "Sheriklaringiz bilan kelishib oling 🤫" : "Hali hech kim yozmadi"}</div>`;
      feed.innerHTML = title + body;
      if (atEnd || lines.length && lines[lines.length - 1].from === myId()) feed.scrollTop = feed.scrollHeight;
    }
    const chips = room === "mafia" ? CHIPS.mafia : state.phase === "lobby" ? CHIPS.lobby : state.phase === "over" ? CHIPS.over : CHIPS.town;
    const chipsKey = chips.join("|");
    if ($("chips").dataset.k !== chipsKey) {
      $("chips").dataset.k = chipsKey;
      $("chips").innerHTML = chips.map((c) => `<button type="button" data-chip="${esc(c)}">${esc(c)}</button>`).join("");
    }
  }

  // sayLine sends a line to the room the page shows; typed is true for
  // the box's own words, cleared once sent, and false for a quick line.
  async function sayLine(text, typed) {
    text = text.trim();
    if (!text) return;
    const room = state && state.me && state.me.room;
    if (await send("chat", { text, room }) && typed) $("chatline").value = "";
  }

  function renderGrid() {
    if (!state) { $("grid").innerHTML = ""; return; }
    const act = pendingAct();
    const me = state.me || {};
    const good = act && ["heal", "check", "find"].includes(act.act);
    const verb = act ? (ACTS[act.act] || {}).verb : state.phase === "vote" ? "🗳" : "";
    const picking = Boolean(act) || (state.phase === "vote" && me.alive);
    $("grid").classList.toggle("picking", picking);
    const tiles = (state.players || []).map((p) => {
      const tag = p.role_name ? esc(p.role_name) : p.mark ? MARKS[p.mark] || "" : "";
      const votes = state.votes && state.votes[p.id];
      const knives = me.picks && me.picks[p.id];
      const badge = votes ? `<span class="badge">🗳${votes}</span>` : knives ? `<span class="badge">🔪${knives}</span>` : "";
      const chosen = picked === p.id;
      const cls = ["p", isMe(p) ? "me" : "", pickable(p) ? "pick" : "", chosen ? "chosen" : "", chosen && good ? "good" : "",
        state.phase === "vote" && me.voted === p.id ? "voted" : "", p.alive ? "" : "dead"].join(" ");
      const corner = chosen && verb ? `<span class="verb">${verb}</span>` : p.alive ? "" : '<span class="mark">💀</span>';
      return `<div class="${cls}" data-id="${p.id}">${badge}${corner}${avatar(p)}<div class="nm">${isMe(p) ? "Siz" : esc(p.name)}</div><div class="tag">${tag}</div>${p.words ? '<span class="said">🕯</span>' : ""}</div>`;
    });
    if (state.phase === "lobby") {
      for (let i = state.players.length; i < state.min_players; i++) tiles.push(`<div class="p empty"><div class="ava">?</div><div class="nm">Bo'sh o'rin</div><div class="tag"></div></div>`);
    }
    $("grid").innerHTML = tiles.join("");
  }

  function renderCenter() {
    const notes = [];
    if (fatal) notes.push(`<div class="note">${esc(fatal)}</div>`);
    else if (!state) notes.push(`<div class="note">Yuklanmoqda…</div>`);
    else {
      const me = state.me || {};
      if (state.phase === "over" && state.winner) {
        const won = me.team && me.team === state.winner;
        notes.push(`<div class="note secret"><b>${WINNERS[state.winner] || "O'yin tugadi"}</b>${me.joined ? (won ? " Siz g'oliblar safidasiz 🏆" : state.winner === "none" ? "" : " Bu safar yutqazdingiz.") : ""}</div>`);
      }
      if (state.phase === "" || state.phase === "lobby" || state.phase === "over") {
        notes.push(`<div class="note">🎭 <b>Mafia</b> — guruh bilan o'ynaladi. Har kim yashirin rol oladi: Don va mafiya, komissar, shifokor, putana, manyak, omadli va tinch aholi. Tunda rollar shu sahifada harakat qiladi (mafiya o'z yashirin chatida kelishadi), kunduzi shu yerdagi chatda yoki guruhda bahslashib, ovoz bilan bir kishini chiqarasiz.</div>`);
        notes.push(`<div class="note">👥 ${state.min_players}–${state.max_players} kishi. G'oliblarga Dev jangi tangasi beriladi 💰</div>`);
      }
      if (state.phase === "night" && !pendingAct()) notes.push(`<div class="note calm"><div class="big">🌙</div><b>Shahar uxlayapti…</b><small>Rollar tanlov qilmoqda. Tong otishini kuting.</small></div>`);
      if (state.phase === "day") notes.push(`<div class="note">💬 Bahslashing: kim mafiya? Pastdagi chatda yoki guruhda yozing. Ovoz berish vaqt tugagach shu sahifada ochiladi.</div>`);
      if ((state.news || []).length && state.phase !== "lobby" && state.phase !== "") notes.push(`<div class="note news">${state.news.map((n) => `<p>${esc(n)}</p>`).join("")}</div>`);
      for (const p of (state.players || []).filter((p) => p.words)) notes.push(`<div class="note words">🕯 <b>${esc(p.name)}</b>ning so'nggi so'zi: <q>${esc(p.words)}</q></div>`);
      const marks = (state.players || []).filter((p) => p.mark);
      if (marks.length) notes.push(`<div class="note secret">🔎 Tekshiruvlaringiz: ${marks.map((p) => `<b>${esc(p.name)}</b> — ${MARKS[p.mark]}`).join(", ")}</div>`);
      if (me.joined && me.alive === false && state.phase !== "over" && state.phase !== "lobby") {
        notes.push(`<div class="note">👻 Siz o'yindan chiqdingiz. ${me.muted ? "🔇 O'yin tugaguncha guruhda yoza olmaysiz — so'nggi so'zni shu sahifadan yuboring." : "O'yinni kuzating, lekin guruhda tiriklarga maslahat bermang."}</div>`);
      }
    }
    $("center").innerHTML = notes.join("");
    $("center").hidden = notes.length === 0;
  }

  function renderBar() {
    const b = $("bar");
    if (fatal || !state) { b.innerHTML = ""; return; }
    const me = state.me || {};
    const phase = state.phase;
    if (phase === "over" && me.can_speak) {
      if (b.contains(document.activeElement) && document.activeElement.classList.contains("words")) return;
      b.innerHTML = `<div class="prompt">🕯 So'nggi so'zingiz<small>O'yin tugadi, lekin aytib qolishingiz mumkin</small></div>${speakForm(state)}`;
    } else if (phase === "" || phase === "over" || (phase === "lobby" && !me.joined)) {
      const label = phase === "lobby" ? "🙋 Qo'shilish" : "🎭 Yangi o'yin ochish";
      b.innerHTML = `<div class="row"><button class="btn main" data-do="join">${label}</button></div>`;
    } else if (phase === "lobby") {
      const enough = state.players.length >= state.min_players;
      const closes = Math.max(0, Math.ceil((ends - Date.now()) / 60000));
      b.innerHTML = `<div class="prompt">${enough ? "Hamma tayyor bo'lsa — boshlang!" : `Yana ${state.min_players - state.players.length} kishi kerak`}<small>Guruhdagilar o'yindagi «Play» ni bosib qo'shiladi · xona ${closes} daqiqada yopiladi</small></div>
        <div class="row"><button class="btn" data-do="leave">Chiqish</button><button class="btn main" data-do="start" ${enough ? "" : "disabled"}>🎭 Boshlash</button></div>`;
    } else if (!me.joined) {
      b.innerHTML = `<div class="prompt">👀 O'yin ketmoqda<small>Keyingi o'yinga qo'shilishingiz mumkin</small></div>`;
    } else if (me.can_speak) {
      // Typing must not be wiped by the next poll's redraw.
      if (b.contains(document.activeElement) && document.activeElement.classList.contains("words")) return;
      b.innerHTML = `<div class="prompt">🕯 So'nggi so'zingiz<small>Bot uni guruhga yuboradi, faqat bir marta</small></div>${speakForm(state)}`;
    } else if (!me.alive) {
      const said = (state.players || []).some((p) => isMe(p) && p.words);
      b.innerHTML = `<div class="prompt">👻 Siz o'yindan chiqdingiz<small>${said ? "So'nggi so'zingiz guruhga yuborildi" : "O'yin oxirigacha kuzatishingiz mumkin"}</small></div>`;
    } else if (phase === "night") {
      const act = pendingAct();
      if (act) {
        const a = ACTS[act.act] || { prompt: act.act, hint: "", verb: "" };
        const target = picked !== null ? state.players.find((p) => p.id === picked) : null;
        b.innerHTML = `<div class="prompt">${a.prompt}<small>${target ? `Tanlandi: ${a.verb} ${esc(target.name)}` : a.hint}</small></div>
          <div class="row"><button class="btn ${a.danger ? "red" : "main"}" data-do="act" ${target ? "" : "disabled"}>Tasdiqlash</button></div>`;
      } else b.innerHTML = `<div class="prompt">😴 ${(me.pending || []).length === 0 && me.role && !["tinch", "omadli"].includes(me.role) ? "Tanlovingiz qabul qilindi" : "Siz uxlayapsiz…"}<small>Tong otishini kuting</small></div>`;
    } else if (phase === "day") {
      b.innerHTML = `<div class="prompt">☀️ Bahs vaqti<small>Yuqoridagi chatda yozing · ovoz berish birozdan keyin</small></div>`;
    } else if (phase === "vote") {
      const voted = me.voted ? state.players.find((p) => p.id === me.voted) : null;
      const target = picked !== null ? state.players.find((p) => p.id === picked) : null;
      b.innerHTML = `<div class="prompt">${voted ? `✅ Ovozingiz: ${esc(voted.name)}` : "🗳 Kimni shahardan chiqaramiz?"}<small>${target ? "Tanlandi: " + esc(target.name) : voted ? "Ovozni o'zgartirish uchun boshqasini tanlang" : "O'yinchini bosing"}</small></div>
        <div class="row"><button class="btn red" data-do="vote" ${target ? "" : "disabled"}>Ovoz berish</button></div>`;
    }
  }

  $("role").addEventListener("click", () => { if (state && state.me && state.me.role) showCard(state.me, true); });
  $("chatbox").addEventListener("submit", (e) => { e.preventDefault(); sayLine($("chatline").value, true); });
  $("chips").addEventListener("click", (e) => { const chip = e.target.closest("[data-chip]"); if (chip) sayLine(chip.dataset.chip, false); });

  // The last word, from the bar or the overlay: what is typed is kept
  // between redraws, and counted.
  document.addEventListener("input", (e) => {
    if (!e.target.classList.contains("words")) return;
    draft = e.target.value;
    const count = e.target.closest(".speak").querySelector(".count");
    if (count) count.textContent = `${[...draft].length}/${(state && state.max_words) || 200}`;
  });
  document.addEventListener("click", async (e) => {
    const button = e.target.closest("[data-speak]");
    if (!button) return;
    const words = button.closest(".speak").querySelector(".words").value.trim();
    if (!words) { message(ERRORS.words); return; }
    if (await send("speak", { words })) {
      draft = "";
      $("overlay").hidden = true;
      message("");
      render();
    }
  });

  $("grid").addEventListener("click", (e) => {
    const el = e.target.closest(".p.pick");
    if (!el) return;
    const id = Number(el.dataset.id);
    picked = picked === id ? null : id;
    render();
  });
  $("bar").addEventListener("click", (e) => {
    const button = e.target.closest("[data-do]");
    if (!button || button.disabled) return;
    const what = button.dataset.do;
    if (what === "act") { const act = pendingAct(); if (act && picked !== null) send("act", { act: act.act, target: picked }); }
    else if (what === "vote") { if (picked !== null) send("vote", { target: picked }); }
    else send(what, {});
  });

  // The clock: the phase's time left, and a request as soon as it is up,
  // which is what moves the game on.
  let asked = false;
  setInterval(() => {
    const timed = state && ["lobby", "night", "day", "vote"].includes(state.phase);
    const left = Math.max(0, Math.ceil((ends - Date.now()) / 1000));
    $("timer").textContent = timed && state.phase !== "lobby" ? `${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")}` : "";
    $("timer").classList.toggle("low", timed && left <= 5);
    const share = timed && state.phase !== "lobby" && state.phase_ms ? Math.max(0, Math.min(1, (ends - Date.now()) / state.phase_ms)) : 0;
    $("progress").style.width = `${share * 100}%`;
    $("progress").parentElement.style.visibility = timed && state.phase !== "lobby" ? "visible" : "hidden";
    $("progress").classList.toggle("low", share > 0 && left <= 5);
    if (timed && left === 0 && !asked) { asked = true; poll().finally(() => { asked = false; }); }
  }, 250);
  setInterval(poll, 2000);
  document.addEventListener("visibilitychange", () => { if (!document.hidden) poll(); });

  // ---------- the night sky ----------
  const sky = $("stars"), sc = sky.getContext("2d");
  const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;
  const stars = Array.from({ length: 70 }, () => ({ x: Math.random(), y: Math.random() * 0.7, r: Math.random() * 1.6 + 0.3, t: Math.random() * 6 }));
  function drawSky(now) {
    const d = devicePixelRatio || 1, w = innerWidth * d, h = innerHeight * d;
    if (sky.width !== w || sky.height !== h) { sky.width = w; sky.height = h; }
    sc.clearRect(0, 0, w, h);
    for (const s of stars) { sc.globalAlpha = reduced ? 0.5 : 0.35 + 0.35 * Math.sin(now / 700 + s.t); sc.fillStyle = "#fff"; sc.beginPath(); sc.arc(s.x * w, s.y * h, s.r * d, 0, 7); sc.fill(); }
    requestAnimationFrame(drawSky);
  }
  requestAnimationFrame(drawSky);

  if (!session) { fatal = "O'yinni Telegram'da oching: guruhga /mafia yozing va «Play» ni bosing."; render(); }
  else { render(); poll(); }
})();
