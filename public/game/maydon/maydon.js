// Maydon's page: the group's room as the server plays it, asked for twice a
// second in a battle. The page draws the fighters and the hits the server
// counted, and sends the player's blows, skill, block and target; their own
// side is always drawn at the bottom.
(() => {
  const $ = (id) => document.getElementById(id);
  const session = new URLSearchParams(location.search).get("s") || "";
  const COLORS = ["#ffd180", "#82b1ff", "#b9f6ca", "#ff8a80", "#ea80fc", "#84ffff", "#ccff90", "#ffe57f"];
  const colorOf = (id) => COLORS[Math.abs(Number(id)) % COLORS.length];
  const TEAM = [{ name: "🔴 Qizillar", color: "#ff5a64", ring: "rgba(255,90,100,.45)" }, { name: "🔵 Ko'klar", color: "#4aa8ff", ring: "rgba(74,168,255,.45)" }];
  const CLASSES = {
    jangchi: { icon: "⚔️", name: "Jangchi", about: "ko'p jon, oldinda turadi", every: 1000, robe: "#8a5a2a", skill: "Qilich bo'roni" },
    kamonchi: { icon: "🏹", name: "Kamonchi", about: "uzoqdan, istalganini otadi", every: 900, robe: "#2e7d32", skill: "O'q yomg'iri" },
    sehrgar: { icon: "🔮", name: "Sehrgar", about: "bir nechtasini uradi", every: 1300, robe: "#6a3ab0", skill: "Olov shari" },
    tabib: { icon: "💚", name: "Tabib", about: "jamoani davolaydi", every: 1150, robe: "#e8e0d0", skill: "Shifo" },
  };
  const PERFECT = 450;
  const ERRORS = {
    group: "Maydon guruhda o'ynaladi: o'yinni guruhga yuboring (/maydon).",
    session: "O'yin havolasi eskirdi. Telegram'da «Play» ni qaytadan bosing.",
    started: "Jang boshlangan — keyingisini kuting.", full: "Xona to'la (5v5).", too_few: "Kamida 2 jangchi kerak.",
    early: "Erta!", energy: "Energiya hali to'lmagan.", block: "Himoya hali tayyor emas.", target: "Bu nishonni tanlab bo'lmaydi.",
    dead: "Siz yiqilgansiz.", phase: "Hozir emas.", unavailable: "Aloqa bo'lmadi, qayta urinib ko'ring.",
  };

  let state = null, snapAt = 0, lastHit = null, myClass = "kamonchi", busy = false, fatal = "", overSeq = null;
  const cv = $("field"), ctx = cv.getContext("2d");
  let W = 0, H = 0, dpr = 1;
  function resize() { dpr = devicePixelRatio || 1; W = innerWidth; H = innerHeight; cv.width = W * dpr; cv.height = H * dpr; }
  addEventListener("resize", resize);
  resize();
  try { myClass = localStorage.getItem("maydon-class") || myClass; } catch (error) { /* the default class */ }

  // ---------- server ----------

  async function api(action, extra) {
    const response = await fetch("/api/maydon", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ session, action, since: lastHit || 0, ...extra }),
    });
    const data = await response.json().catch(() => ({ error: "unavailable" }));
    if (data.state) apply(data.state);
    else if (!data.error) apply(data);
    if (data.error) throw new Error(data.error);
    return data;
  }

  async function send(action, extra, quiet) {
    if (busy && !quiet) return;
    if (!quiet) busy = true;
    try {
      await api(action, extra);
      say("");
    } catch (error) {
      if (error.message === "group" || error.message === "session") fatal = ERRORS[error.message];
      if (error.message !== "early") say(ERRORS[error.message] || ERRORS.unavailable);
    } finally {
      if (!quiet) busy = false;
      render();
    }
  }

  let polling = false;
  async function poll() {
    if (polling || document.hidden || fatal || !session) return;
    polling = true;
    try { await api("state", {}); } catch (error) {
      if (error.message === "group" || error.message === "session") { fatal = ERRORS[error.message]; render(); }
    } finally { polling = false; }
  }

  function say(text) { $("lobbyMessage").textContent = $("battleMessage").textContent = text || ""; }

  // ---------- what the server says ----------

  const me = () => (state && state.me && state.me.joined ? (state.fighters || []).find((f) => f.id === state.me.id) : null);

  function apply(view) {
    const before = state;
    // Animations ride on the fighters; keep them across the new snapshot.
    if (before) for (const f of view.fighters || []) { const old = before.fighters.find((o) => o.id === f.id); if (old) { f.lunge = old.lunge; f.swing = old.swing; } }
    state = view;
    snapAt = performance.now();
    if (view.me && view.me.class) myClass = view.me.class;
    if (lastHit === null || !before || before.round !== view.round) lastHit = view.last_hit;
    else for (const h of view.hits || []) if (h.id > lastHit) { play(h); lastHit = h.id; }
    if (view.phase === "over" && before && before.phase === "battle" && overSeq !== view.seq) { overSeq = view.seq; showEnd(view); }
    render();
  }

  // ---------- the lobby ----------

  function avatar(f) {
    const initial = `<div class="ava" style="background:${colorOf(f.id)}">${esc((f.name || "?").slice(0, 1).toUpperCase())}</div>`;
    if (!f.avatar) return initial;
    return `<img class="ava" alt="" src="${esc(f.avatar)}" onerror="this.outerHTML=this.dataset.f" data-f="${esc(initial)}">`;
  }
  function esc(text) { return String(text ?? "").replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c])); }
  const power = (f) => { const atk = 1 + 0.03 * (Math.max(f.sword, 1) - 1), life = 1 + 0.01 * (Math.max(f.sword, 1) - 1 + f.crit); return Math.round(100 * atk * life * (1 + (5 + 2 * f.crit) / 100)); };

  function renderLobby() {
    const fighters = state ? state.fighters || [] : [];
    const joined = state && state.me && state.me.joined;
    const lobby = state && state.phase === "lobby";
    $("lobbySub").textContent = fatal || (lobby ? `Xona ochiq · ${fighters.length}/${state.max_players}. Jamoalar jang boshlanganda kuch bo'yicha teng bo'linadi.` : "Guruh bilan jamoa jangi: 1v1 dan 5v5 gacha. Kuchingiz Dev jangi darajasidan.");
    $("classes").innerHTML = Object.entries(CLASSES).map(([k, c]) => `<button type="button" class="cls${k === myClass ? " on" : ""}" data-c="${k}"><span class="i">${c.icon}</span>${c.name}<small>${c.about}</small></button>`).join("");
    $("rosterTitle").textContent = fighters.length ? `Jangchilar (${fighters.length})` : "Hali hech kim yo'q";
    $("roster").innerHTML = fighters.map((f) => `<div class="fighter${state.me && f.id === state.me.id ? " me" : ""}">${avatar(f)}<div>${esc(f.name)}<small>${CLASSES[f.class].icon} ${CLASSES[f.class].name} · ⚔${f.sword} ⚡${f.crit}</small></div><span class="pow">⚡${power(f)}</span></div>`).join("");
    const actions = $("lobbyActions");
    if (fatal || !state) actions.innerHTML = "";
    else if (!lobby || !joined) actions.innerHTML = `<button type="button" class="btn main" data-do="join">${lobby ? "🙋 Qo'shilish" : "⚔️ Xona ochish"}</button>`;
    else actions.innerHTML = `<button type="button" class="btn" data-do="leave">Chiqish</button><button type="button" class="btn main" data-do="start" ${fighters.length >= state.min_players ? "" : "disabled"}>⚔️ Jang!</button>`;
  }

  $("classes").addEventListener("click", (e) => {
    const b = e.target.closest("[data-c]");
    if (!b) return;
    myClass = b.dataset.c;
    try { localStorage.setItem("maydon-class", myClass); } catch (error) { /* kept for this visit only */ }
    if (state && state.phase === "lobby" && state.me && state.me.joined) send("join", { class: myClass });
    else renderLobby();
  });
  $("lobbyActions").addEventListener("click", (e) => {
    const b = e.target.closest("[data-do]");
    if (!b || b.disabled) return;
    send(b.dataset.do, b.dataset.do === "join" ? { class: myClass } : {});
  });

  // ---------- the battle ----------

  const shots = [], nums = [], feed = [];
  const fighter = (id) => state && (state.fighters || []).find((f) => f.id === id);
  const bottomTeam = () => (state && state.me && state.me.joined ? state.me.team : 1);
  const padShown = () => state && state.phase === "battle" && state.me && state.me.joined;
  const top = () => 104, bottom = () => H - (padShown() ? 150 : 40);
  const scale = () => Math.min(0.85, Math.min(W, 480) / 5 / 82);
  function posOf(f) {
    const colW = Math.min(W, 480) / 5, left = (W - colW * 5) / 2, gap = Math.min(100, (bottom() - top()) / 5.5);
    const x = left + colW * (f.col + 0.5);
    const y = f.team === bottomTeam() ? bottom() - 20 - f.row * gap : top() + 100 * scale() + 14 + (1 - f.row) * gap;
    return [x, y];
  }

  function play(h) {
    const from = fighter(h.from), to = fighter(h.to);
    if (!from || !to) return;
    if (h.kind === "down") { addFeed(`💀 ${to.name} yiqildi (${from.name})`); return; }
    if (h.kind === "back") { pop(to, "✨ qaytdi!", "#ffe08a"); addFeed(`✨ ${to.name} jangga qaytdi`); return; }
    from.swing = 1;
    if (h.kind === "slash") from.lunge = 1;
    const text = h.kind === "heal" ? "+" + h.amount : (h.blocked ? "🛡 −" : h.crit ? "KRIT −" : "−") + h.amount;
    const color = h.kind === "heal" ? "#9be15d" : h.blocked ? "#8fd0ff" : h.crit ? "#ffe08a" : "#fff";
    shots.push({ from: from.id, to: to.id, kind: h.kind, k: h.kind === "slash" ? 0.7 : 0, text, color, big: h.crit || h.perfect });
    if (h.perfect && state.me && h.from === state.me.id) pop(from, "ANIQ!", "#ffe08a");
  }
  function addFeed(text) { feed.push(text); while (feed.length > 3) feed.shift(); $("feed").innerHTML = feed.map((t) => `<div>${esc(t)}</div>`).join(""); }
  function pop(f, text, color) { if (!f) return; const [x, y] = posOf(f); nums.push({ x, y: y - 120 * scale(), text, color, size: 16, life: 1 }); }

  function cdNow() { return state && state.me ? state.me.cd - (performance.now() - snapAt) : 0; }

  function renderPad() {
    const pad = $("pad");
    pad.hidden = !padShown();
    if (pad.hidden) return;
    const self = me(), c = CLASSES[state.me.class], dead = !self || !self.alive;
    const cd = cdNow(), ready = cd <= 0, perfect = ready && -cd <= PERFECT;
    const a = $("attack");
    a.style.setProperty("--p", `${(1 - Math.max(0, cd) / c.every) * 100}%`);
    a.classList.toggle("ready", ready && !dead);
    a.classList.toggle("perfect", perfect && !dead);
    a.disabled = dead;
    $("attackText").textContent = dead ? (self && self.revive_in > 0 ? `💀 ${Math.max(0, Math.ceil((self.revive_in - (performance.now() - snapAt)) / 1000))}` : "💀") : perfect ? "HOZIR!" : ready ? "URISH" : "...";
    $("skillIcon").textContent = c.icon;
    $("skillName").textContent = c.skill;
    const energy = self ? self.energy : 0;
    $("skill").disabled = dead;
    $("skill").classList.toggle("glow", energy >= 100 && !dead);
    $("skillInfo").textContent = energy >= 100 ? "TAYYOR!" : `${energy}%`;
    $("skillCool").style.setProperty("--cool", `${100 - energy}%`);
    const wait = Math.max(0, state.me.block_in - (performance.now() - snapAt));
    $("block").disabled = dead || wait > 0;
    $("block").classList.toggle("glow", !!(self && self.blocking));
    $("blockInfo").textContent = self && self.blocking ? "himoyada" : wait > 0 ? `${Math.ceil(wait / 1000)} s` : "tayyor";
    $("blockCool").style.setProperty("--cool", `${(wait / 6000) * 100}%`);
  }

  function attack() {
    if (!padShown()) return;
    const self = me();
    if (!self || !self.alive) return;
    // The ring moves at once; the server's answer sets it right.
    if (cdNow() > 0) { pop(self, "Erta!", "#ff8a8a"); state.me.cd += 150; }
    else state.me.cd = CLASSES[state.me.class].every + (performance.now() - snapAt);
    send("attack", {}, true);
    if (navigator.vibrate) navigator.vibrate(10);
  }
  $("attack").addEventListener("pointerdown", (e) => { e.preventDefault(); attack(); });
  addEventListener("keydown", (e) => { if (e.code === "Space" && padShown()) { e.preventDefault(); attack(); } });
  $("skill").addEventListener("click", () => send("skill", {}, true));
  $("block").addEventListener("click", () => send("block", {}, true));
  // A tap on a fighter makes them the target: a foe, or a friend for a
  // healer.
  cv.addEventListener("pointerdown", (e) => {
    if (!padShown()) return;
    const self = me();
    if (!self || !self.alive) return;
    let best = null, near = 44;
    for (const f of state.fighters) {
      if (!f.alive || f.id === self.id) continue;
      const [x, y] = posOf(f), d = Math.hypot(e.clientX - x, e.clientY - (y - 45 * scale()));
      if (d < near) { near = d; best = f; }
    }
    if (!best || (best.team === self.team && self.class !== "tabib")) return;
    state.me.target = best.id;
    send("aim", { target: best.id }, true);
  });

  // ---------- drawing ----------

  function figure(f, now) {
    let [x, y] = posOf(f);
    const s = scale(), c = CLASSES[f.class], dead = !f.alive, team = TEAM[f.team], mine = state.me && f.id === state.me.id;
    if (f.lunge > 0) y += (f.team === bottomTeam() ? -1 : 1) * Math.sin(f.lunge * Math.PI) * 26;
    ctx.save();
    ctx.translate(x, y);
    ctx.fillStyle = team.ring;
    ctx.beginPath(); ctx.ellipse(0, 0, 22 * s, 7 * s, 0, 0, 7); ctx.fill();
    if (mine) { ctx.strokeStyle = "#ffc83d"; ctx.lineWidth = 2; ctx.stroke(); }
    if (state.me && state.me.target === f.id && !dead) {
      ctx.save(); ctx.strokeStyle = f.team === bottomTeam() ? "#9be15d" : "#ffc83d"; ctx.lineWidth = 3; ctx.setLineDash([6, 5]); ctx.lineDashOffset = -now / 40;
      ctx.beginPath(); ctx.ellipse(0, 0, 30 * s, 11 * s, 0, 0, 7); ctx.stroke(); ctx.restore();
    }
    if (f.blocking && !dead) { ctx.save(); ctx.strokeStyle = "rgba(143,208,255,.9)"; ctx.fillStyle = "rgba(143,208,255,.15)"; ctx.lineWidth = 3; ctx.beginPath(); ctx.arc(0, -45 * s, 46 * s, 0, 7); ctx.fill(); ctx.stroke(); ctx.restore(); }
    if (dead) { ctx.globalAlpha = 0.35; ctx.rotate(f.team === bottomTeam() ? 1.2 : -1.2); }
    ctx.scale(s, s);
    const bob = dead ? 0 : Math.sin(now / 260 + f.col) * 1.5, swing = f.swing || 0;
    ctx.fillStyle = "#2b1d14"; ctx.fillRect(-10, -22, 8, 22); ctx.fillRect(3, -22, 8, 22);
    ctx.fillStyle = c.robe; ctx.beginPath(); ctx.moveTo(-16, -18 + bob); ctx.lineTo(16, -18 + bob); ctx.lineTo(12, -56 + bob); ctx.lineTo(-12, -56 + bob); ctx.fill();
    ctx.fillStyle = team.color; ctx.fillRect(-14, -40 + bob, 28, 5);
    ctx.fillStyle = "#e8b48a"; ctx.beginPath(); ctx.arc(0, -66 + bob, 11, 0, 7); ctx.fill();
    ctx.fillStyle = "#1a0c00"; ctx.beginPath(); ctx.arc(-4, -67 + bob, 1.6, 0, 7); ctx.arc(4, -67 + bob, 1.6, 0, 7); ctx.fill();
    ctx.save(); ctx.translate(13, -42 + bob); ctx.rotate(-0.5 + swing * 1.2);
    if (f.class === "jangchi") { ctx.fillStyle = "#ffc83d"; ctx.fillRect(-6, -2, 12, 3); ctx.fillStyle = "#e6edf3"; ctx.beginPath(); ctx.moveTo(-3, -2); ctx.lineTo(3, -2); ctx.lineTo(0, -36); ctx.fill(); }
    if (f.class === "kamonchi") { ctx.strokeStyle = "#a0662a"; ctx.lineWidth = 3; ctx.beginPath(); ctx.arc(0, -12, 16, -1.4, 1.4); ctx.stroke(); ctx.strokeStyle = "rgba(255,255,255,.7)"; ctx.lineWidth = 1; ctx.beginPath(); ctx.moveTo(Math.cos(-1.4) * 16, -12 + Math.sin(-1.4) * 16); ctx.lineTo(-swing * 6, -12); ctx.lineTo(Math.cos(1.4) * 16, -12 + Math.sin(1.4) * 16); ctx.stroke(); }
    if (f.class === "sehrgar") { ctx.fillStyle = "#6b4a2a"; ctx.fillRect(-2, -36, 4, 40); ctx.fillStyle = "#c792ff"; ctx.shadowColor = "#c792ff"; ctx.shadowBlur = 10; ctx.beginPath(); ctx.arc(0, -38, 6, 0, 7); ctx.fill(); ctx.shadowBlur = 0; }
    if (f.class === "tabib") { ctx.fillStyle = "#6b4a2a"; ctx.fillRect(-2, -30, 4, 34); ctx.fillStyle = "#9be15d"; ctx.fillRect(-7, -36, 14, 5); ctx.fillRect(-2.5, -41, 5, 15); }
    ctx.restore();
    if (f.class === "sehrgar") { ctx.fillStyle = "#4a2a80"; ctx.beginPath(); ctx.moveTo(-13, -72 + bob); ctx.lineTo(13, -72 + bob); ctx.lineTo(2, -96 + bob); ctx.fill(); }
    if (f.class === "jangchi") { ctx.fillStyle = "#9aa4ad"; ctx.beginPath(); ctx.arc(0, -70 + bob, 12, Math.PI, 0); ctx.fill(); }
    if (f.class === "kamonchi") { ctx.fillStyle = "#1e5a24"; ctx.beginPath(); ctx.arc(0, -70 + bob, 12, Math.PI, 0); ctx.fill(); }
    if (f.class === "tabib") { ctx.fillStyle = "#fff"; ctx.beginPath(); ctx.arc(0, -70 + bob, 12, Math.PI, 0); ctx.fill(); ctx.fillStyle = "#e53935"; ctx.fillRect(-1.5, -80 + bob, 3, 8); ctx.fillRect(-4, -77 + bob, 8, 3); }
    ctx.restore();
    const by = y - 100 * s;
    ctx.font = `${mine ? 800 : 700} 11px system-ui`; ctx.textAlign = "center"; ctx.lineWidth = 3; ctx.strokeStyle = "rgba(0,0,0,.7)";
    const label = (dead ? "💀 " : "") + (mine ? "Siz" : (f.name.length > 7 ? f.name.slice(0, 6) + "…" : f.name)) + " ⚔" + f.sword;
    ctx.strokeText(label, x, by - 8); ctx.fillStyle = mine ? "#ffc83d" : dead ? "#888" : "#fff"; ctx.fillText(label, x, by - 8);
    if (dead) return;
    const w = 40;
    ctx.fillStyle = "rgba(0,0,0,.6)"; ctx.fillRect(x - w / 2, by - 4, w, 5);
    ctx.fillStyle = team.color; ctx.fillRect(x - w / 2, by - 4, (w * f.hp) / f.max, 5);
    ctx.fillStyle = "rgba(0,0,0,.6)"; ctx.fillRect(x - w / 2, by + 2, w, 2.5);
    ctx.fillStyle = f.energy >= 100 ? "#ffc83d" : "#c9a227"; ctx.fillRect(x - w / 2, by + 2, (w * f.energy) / 100, 2.5);
  }

  function effects(dt) {
    for (const sh of shots) {
      sh.k += dt / (sh.kind === "slash" ? 200 : 380);
      const a = fighter(sh.from), b = fighter(sh.to);
      if (!a || !b) { sh.done = true; continue; }
      const [x0, y0] = posOf(a), [x1, y1] = posOf(b), s = scale(), k = Math.min(1, sh.k);
      const x = x0 + (x1 - x0) * k, y = y0 - 50 * s + (y1 - y0) * k - Math.sin(k * Math.PI) * (sh.kind === "arrow" ? 30 : 10);
      if (sh.k < 1 && sh.kind !== "slash") {
        ctx.save();
        if (sh.kind === "arrow") { ctx.strokeStyle = "#f3e2c0"; ctx.lineWidth = 2; const an = Math.atan2(y1 - y0, x1 - x0); ctx.beginPath(); ctx.moveTo(x, y); ctx.lineTo(x - Math.cos(an) * 14, y - Math.sin(an) * 14); ctx.stroke(); }
        else { const col = { orb: "#c792ff", fire: "#ff8a3d", heal: "#9be15d", leaf: "#9be15d" }[sh.kind] || "#fff"; ctx.fillStyle = col; ctx.shadowColor = col; ctx.shadowBlur = 14; ctx.beginPath(); ctx.arc(x, y, sh.kind === "fire" ? 8 : 5, 0, 7); ctx.fill(); }
        ctx.restore();
      }
      if (sh.k >= 1 && !sh.done) { sh.done = true; nums.push({ x: x1 + (Math.random() - 0.5) * 16, y: y1 - 70 * s, text: sh.text, color: sh.color, size: sh.big ? 17 : 13, life: 1 }); }
    }
    for (let i = shots.length - 1; i >= 0; i--) if (shots[i].done) shots.splice(i, 1);
    for (const n of nums) {
      n.y -= dt * 0.04; n.life -= dt / 900;
      ctx.globalAlpha = Math.max(0, n.life); ctx.font = `900 ${n.size}px system-ui`; ctx.textAlign = "center";
      ctx.lineWidth = 3; ctx.strokeStyle = "rgba(0,0,0,.7)"; ctx.strokeText(n.text, n.x, n.y); ctx.fillStyle = n.color; ctx.fillText(n.text, n.x, n.y);
    }
    ctx.globalAlpha = 1;
    for (let i = nums.length - 1; i >= 0; i--) if (nums[i].life <= 0) nums.splice(i, 1);
  }

  function arena(fighting) {
    const g = ctx.createLinearGradient(0, 0, 0, H);
    g.addColorStop(0, "#2a0f1e"); g.addColorStop(0.5, "#241a3a"); g.addColorStop(1, "#0f1a33");
    ctx.fillStyle = g; ctx.fillRect(0, 0, W, H);
    if (!fighting) return;
    ctx.fillStyle = "rgba(255,255,255,.04)";
    for (let i = 0; i < 9; i++) ctx.fillRect(0, top() + (i * (bottom() - top())) / 9, W, 1);
    ctx.strokeStyle = "rgba(255, 200, 61, .25)"; ctx.setLineDash([6, 6]); ctx.beginPath(); ctx.moveTo(0, (top() + bottom()) / 2); ctx.lineTo(W, (top() + bottom()) / 2); ctx.stroke(); ctx.setLineDash([]);
  }

  function renderBattle() {
    const fighters = state.fighters || [];
    const share = (team) => { const us = fighters.filter((f) => f.team === team); const max = us.reduce((s, f) => s + f.max, 0); return max ? us.reduce((s, f) => s + f.hp, 0) / max : 0; };
    const low = bottomTeam(), high = 1 - low;
    $("topBar").style.width = `${share(high) * 100}%`; $("topBar").style.background = TEAM[high].color;
    $("bottomBar").style.width = `${share(low) * 100}%`; $("bottomBar").style.background = TEAM[low].color;
    $("topName").textContent = TEAM[high].name; $("topName").style.color = TEAM[high].color;
    $("bottomName").textContent = TEAM[low].name + (state.me && state.me.joined ? " (siz)" : ""); $("bottomName").style.color = TEAM[low].color;
    const left = state.phase === "battle" ? Math.max(0, Math.ceil((state.left_ms - (performance.now() - snapAt)) / 1000)) : 0;
    $("clock").textContent = `${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")}`;
    $("mode").textContent = state.phase === "over" ? "🏁 Jang tugadi" : state.me && state.me.joined ? `${CLASSES[state.me.class].icon} Siz: ${CLASSES[state.me.class].name}` : "👀 Kuzatyapsiz";
  }

  const fighting = () => state && (state.phase === "battle" || (state.phase === "over" && (state.fighters || []).some((f) => f.max)));

  function render() {
    $("lobby").hidden = !!fighting();
    $("battle").hidden = !fighting();
    if (fighting()) { renderBattle(); renderPad(); } else renderLobby();
  }

  function showEnd(view) {
    const self = view.me || {};
    const won = self.joined && self.team === view.winner;
    const best = (view.fighters || []).find((f) => f.id === view.best);
    const title = view.winner === -1 ? "🤝 Durang!" : self.joined ? (won ? "🏆 G'alaba!" : "😤 Mag'lubiyat") : `${TEAM[view.winner].name} yutdi!`;
    const rows = (view.fighters || []).slice().sort((a, b) => (b.dmg + b.heal) - (a.dmg + a.heal));
    const o = $("overlay");
    o.innerHTML = `<div class="box"><h2 style="color:${view.winner >= 0 ? TEAM[view.winner].color : "#fff"}">${title}</h2>
      ${best ? `<div class="mvp">⭐ Jang qahramoni: ${esc(best.name)} ${CLASSES[best.class].icon} — ${best.dmg} zarar${best.heal ? `, ${best.heal} davolash` : ""}</div>` : ""}
      ${won ? "<p>+100 💰 Dev jangi tangasi</p>" : ""}
      <table><tr><td></td><td class="n">⚔️ zarar</td><td class="n">💚 davo</td></tr>${rows.map((f) => `<tr class="${f.team ? "b" : "r"}${self.id === f.id ? " me" : ""}"><td>${f.team ? "🔵" : "🔴"} ${CLASSES[f.class].icon} ${esc(f.name)}</td><td class="n">${f.dmg}</td><td class="n">${f.heal || ""}</td></tr>`).join("")}</table>
      <div class="actions" style="padding:0"><button type="button" class="btn" data-x="close">Yopish</button><button type="button" class="btn main" data-x="again">Yana jang</button></div></div>`;
    o.hidden = false;
    o.querySelector('[data-x="close"]').addEventListener("click", () => { o.hidden = true; });
    o.querySelector('[data-x="again"]').addEventListener("click", () => { o.hidden = true; send("join", { class: myClass }); });
  }

  let last = performance.now();
  function frame(now) {
    const dt = Math.min(60, now - last);
    last = now;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    const on = fighting();
    arena(on);
    if (on) {
      const fighters = (state.fighters || []).filter((f) => f.max);
      for (const f of fighters) { f.lunge = Math.max(0, (f.lunge || 0) - dt / 250); f.swing = Math.max(0, (f.swing || 0) - dt / 220); }
      const order = fighters.slice().sort((a, b) => posOf(a)[1] - posOf(b)[1]);
      for (const f of order) if (!f.alive) figure(f, now);
      for (const f of order) if (f.alive) figure(f, now);
      effects(dt);
      renderBattle();
      renderPad();
    }
    requestAnimationFrame(frame);
  }

  setInterval(() => { if (!state || state.phase === "battle") poll(); }, 500);
  setInterval(() => { if (state && state.phase !== "battle") poll(); }, 2000);
  document.addEventListener("visibilitychange", () => { if (!document.hidden) poll(); });
  requestAnimationFrame(frame);
  if (!session) { fatal = "O'yinni Telegram'da oching: guruhga /maydon yozing va «Play» ni bosing."; render(); }
  else { render(); poll(); }
})();
