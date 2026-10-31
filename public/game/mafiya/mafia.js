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
  const MARKS = { mafia: "🤵 mafiya!", town: "✅ tinch", commissar: "🕵️‍♂️ komissar!", not: "komissar emas" };
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
  };

  let state = null, ends = 0, picked = null, busy = false, overlaySeq = null, cardFor = null, fatal = "";
  let shownNews = null, closing = 0;

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

  async function send(action, extra) {
    if (busy) return;
    busy = true;
    message("");
    try {
      await api(action, extra);
    } catch (error) {
      if (error.message === "group" || error.message === "session") fatal = ERRORS[error.message];
      message(ERRORS[error.message] || ERRORS.unavailable);
      render();
    } finally {
      busy = false;
    }
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
    if (playing && me.role && cardFor !== game && !shown(game)) { cardFor = game; showCard(me); }
    else if (before && before.seq !== view.seq && view.phase === "over") showEnd(view);
    else if (news !== shownNews && (view.phase === "day" || view.phase === "night") && (view.news || []).length) showNews(view);
    shownNews = news;
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

  function showCard(me) {
    const close = overlay(`<div><div class="card" id="card"><div class="inner">
      <div class="face back">🎭<br>Rolingiz<div style="font-size:14px;font-family:system-ui;margin-top:10px;color:#cbbfe6">ochish uchun bosing</div></div>
      <div class="face front ${esc(me.team || "town")}"><div class="big">${esc((me.role_name || "").split(" ")[0])}</div><h2>${esc((me.role_name || "").split(" ").slice(1).join(" "))}</h2>
      <div class="team">${TEAMS[me.team] || ""}</div><p>${ABOUT[me.role] || ""}</p></div>
    </div></div><div class="hint">Rolingizni hech kimga ko'rsatmang 🤫</div>
    <div class="row" style="margin-top:12px"><button class="btn main" data-close hidden>Tushundim</button></div></div>`);
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

  function showEnd(view) {
    if (overlaySeq === view.seq) return;
    overlaySeq = view.seq;
    const me = view.me || {};
    const won = me.team && me.team === view.winner;
    const title = { town: "🏛 Tinch aholi yutdi!", mafia: "🩸 Mafiya yutdi!", solo: "🦹‍♂️ Manyak yutdi!" }[view.winner] || "O'yin tugadi";
    overlay(`<div class="announce"><h2>${title}</h2>
      <p>${me.joined ? (won ? "🏆 Siz g'oliblar safidasiz! Dev jangi tangasi berildi." : "😤 Bu safar yutqazdingiz") : ""}</p>
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
    document.body.className = phase === "day" || phase === "vote" ? "day" : "";
    $("phase").textContent = !state ? "…" : { "": "Xona yo'q", lobby: `Xona · ${state.players.length}/${state.max_players}`, night: `🌙 ${state.day}-tun`, day: `☀️ ${state.day}-kun`, vote: "🗳 Ovoz", over: "🏁 Tugadi" }[phase];
    renderGrid();
    renderCenter();
    renderBar();
  }

  function renderGrid() {
    if (!state) { $("grid").innerHTML = ""; return; }
    const act = pendingAct();
    const good = act && ["heal", "check", "find"].includes(act.act);
    $("grid").innerHTML = (state.players || []).map((p) => {
      const tag = p.role_name ? esc(p.role_name) : p.mark ? MARKS[p.mark] || "" : "";
      const votes = state.votes && state.votes[p.id];
      const knives = state.me && state.me.picks && state.me.picks[p.id];
      const badge = votes ? `<span class="badge">🗳${votes}</span>` : knives ? `<span class="badge">🔪${knives}</span>` : "";
      const cls = ["p", isMe(p) ? "me" : "", pickable(p) ? "pick" : "", picked === p.id ? "chosen" : "", picked === p.id && good ? "good" : "", p.alive ? "" : "dead"].join(" ");
      return `<div class="${cls}" data-id="${p.id}">${badge}${p.alive ? "" : '<span class="mark">💀</span>'}${avatar(p)}<div class="nm">${isMe(p) ? "Siz" : esc(p.name)}</div><div class="tag">${tag}</div></div>`;
    }).join("");
  }

  function renderCenter() {
    const notes = [];
    if (fatal) notes.push(`<div class="note">${esc(fatal)}</div>`);
    else if (!state) notes.push(`<div class="note">Yuklanmoqda…</div>`);
    else {
      const me = state.me || {};
      if (state.phase === "over" && state.winner) {
        const won = me.team && me.team === state.winner;
        notes.push(`<div class="note secret"><b>${{ town: "🏛 Tinch aholi yutdi!", mafia: "🩸 Mafiya yutdi!", solo: "🦹‍♂️ Manyak yutdi!" }[state.winner]}</b>${me.joined ? (won ? " Siz g'oliblar safidasiz 🏆" : " Bu safar yutqazdingiz.") : ""}</div>`);
      }
      if (state.phase === "" || state.phase === "lobby" || state.phase === "over") {
        notes.push(`<div class="note">🎭 <b>Mafia</b> — guruh bilan o'ynaladi. Har kim yashirin rol oladi: Don va mafiya, komissar, shifokor, putana, manyak, omadli va tinch aholi. Tunda rollar shu sahifada harakat qiladi, kunduzi guruh chatida bahslashib, ovoz bilan bir kishini chiqarasiz.</div>`);
        notes.push(`<div class="note">👥 ${state.min_players}–${state.max_players} kishi. G'oliblarga Dev jangi tangasi beriladi 💰</div>`);
      }
      if (state.phase === "day") notes.push(`<div class="note">💬 Bahs <b>guruh chatida</b>: Telegram'ga qaytib, kim mafiya ekanini aniqlang. Ovoz berish vaqt tugagach shu sahifada ochiladi.</div>`);
      for (const n of state.news || []) if (state.phase !== "lobby" && state.phase !== "") notes.push(`<div class="note">${esc(n)}</div>`);
      const marks = (state.players || []).filter((p) => p.mark);
      if (marks.length) notes.push(`<div class="note secret">🔎 Tekshiruvlaringiz: ${marks.map((p) => `<b>${esc(p.name)}</b> — ${MARKS[p.mark]}`).join(", ")}</div>`);
      if (me.joined && me.alive === false && state.phase !== "over" && state.phase !== "lobby") notes.push(`<div class="note">👻 Siz o'yindan chiqdingiz. O'yinni kuzatishingiz mumkin, lekin guruhda rollarni oshkor qilmang.</div>`);
    }
    $("center").innerHTML = notes.join("");
  }

  function renderBar() {
    const b = $("bar");
    if (fatal || !state) { b.innerHTML = ""; return; }
    const me = state.me || {};
    const phase = state.phase;
    if (phase === "" || phase === "over" || (phase === "lobby" && !me.joined)) {
      const label = phase === "lobby" ? "🙋 Qo'shilish" : "🎭 Yangi o'yin ochish";
      b.innerHTML = `<div class="row"><button class="btn main" data-do="join">${label}</button></div>`;
    } else if (phase === "lobby") {
      const enough = state.players.length >= state.min_players;
      b.innerHTML = `<div class="prompt">${enough ? "Hamma tayyor bo'lsa — boshlang!" : `Yana ${state.min_players - state.players.length} kishi kerak`}<small>Guruhdagilar o'yindagi «Play» ni bosib qo'shiladi</small></div>
        <div class="row"><button class="btn" data-do="leave">Chiqish</button><button class="btn main" data-do="start" ${enough ? "" : "disabled"}>🎭 Boshlash</button></div>`;
    } else if (!me.joined) {
      b.innerHTML = `<div class="prompt">👀 O'yin ketmoqda<small>Keyingi o'yinga qo'shilishingiz mumkin</small></div>`;
    } else if (!me.alive) {
      b.innerHTML = `<div class="prompt">👻 Siz o'yindan chiqdingiz<small>O'yin oxirigacha kuzatishingiz mumkin</small></div>`;
    } else if (phase === "night") {
      const act = pendingAct();
      if (act) {
        const a = ACTS[act.act] || { prompt: act.act, hint: "", verb: "" };
        const target = picked !== null ? state.players.find((p) => p.id === picked) : null;
        b.innerHTML = `<div class="prompt">${a.prompt}<small>${target ? `Tanlandi: ${a.verb} ${esc(target.name)}` : a.hint}</small></div>
          <div class="row"><button class="btn ${a.danger ? "red" : "main"}" data-do="act" ${target ? "" : "disabled"}>Tasdiqlash</button></div>`;
      } else b.innerHTML = `<div class="prompt">😴 ${(me.pending || []).length === 0 && me.role && !["tinch", "omadli"].includes(me.role) ? "Tanlovingiz qabul qilindi" : "Siz uxlayapsiz…"}<small>Tong otishini kuting</small></div>`;
    } else if (phase === "day") {
      b.innerHTML = `<div class="prompt">☀️ Bahs guruh chatida<small>Ovoz berish birozdan keyin boshlanadi</small></div>`;
    } else if (phase === "vote") {
      const voted = me.voted ? state.players.find((p) => p.id === me.voted) : null;
      const target = picked !== null ? state.players.find((p) => p.id === picked) : null;
      b.innerHTML = `<div class="prompt">${voted ? `✅ Ovozingiz: ${esc(voted.name)}` : "🗳 Kimni shahardan chiqaramiz?"}<small>${target ? "Tanlandi: " + esc(target.name) : voted ? "Ovozni o'zgartirish uchun boshqasini tanlang" : "O'yinchini bosing"}</small></div>
        <div class="row"><button class="btn red" data-do="vote" ${target ? "" : "disabled"}>Ovoz berish</button></div>`;
    }
  }

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
