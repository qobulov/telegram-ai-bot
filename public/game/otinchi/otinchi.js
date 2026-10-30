// O'tinchi. The server gives each play its seed and rules and, at the end,
// chops the sides sent again on the same trunk to count the score; the
// score shown while playing is the page's own count of the same rules.
"use strict";

(function () {
  const $ = (id) => document.getElementById(id);
  const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;

  const texts = {
    uz: {
      lead: "Keksa chinorni chopib, shoxlardan qoching!",
      rule1: "Ekranning chap yoki o'ng tomonini bosing: o'tinchi o'sha tomondan chopadi.",
      rule2: "Shox tushib ketmasin: shox bor tomonda turmang.",
      rule3: "Vaqt kamayib boradi, har chopish uni biroz to'ldiradi. Tez choping!",
      rule4: "Oltin bo'lak 5 ochko. Har 50 bo'lakda fasl almashadi.",
      play: "Chopish!", table: "Reyting", tapHint: "👈 Bosing 👉", again: "Yana chopish", share: "Ulashish",
      branch: "Shox tegdi!", time: "Vaqt tugadi!", seasons: ["🌸 Bahor", "☀️ Yoz", "🍂 Kuz", "❄️ Qish"],
      best: "Yangi rekord! 🎉", notBest: (n) => `Bu safar ${n} ochko. Rekordingiz saqlanib qoldi.`,
      zero: "Bu safar ochko yo'q. Yana urinib ko'ring!", checking: "Natija tekshirilmoqda…",
      noSession: "O'yinni Telegram'da oching: botga /otinchi yozing va «Play» ni bosing.",
      session: "O'yin havolasi eskirdi. Telegram'da «Play» ni qaytadan bosing.",
      implausible: "Bu natija qabul qilinmadi.", unavailable: "Aloqa bo'lmadi. Qayta urinib ko'ring.",
    },
    ru: {
      lead: "Рубите старую чинару и уворачивайтесь от веток!",
      rule1: "Нажимайте на левую или правую сторону экрана: дровосек рубит с той стороны.",
      rule2: "Не стойте под веткой — она упадёт на вас.",
      rule3: "Время убывает, каждый удар немного его добавляет. Рубите быстро!",
      rule4: "Золотой чурбак — 5 очков. Каждые 50 чурбаков меняется время года.",
      play: "Рубить!", table: "Рейтинг", tapHint: "👈 Нажимайте 👉", again: "Ещё раз", share: "Поделиться",
      branch: "Задела ветка!", time: "Время вышло!", seasons: ["🌸 Весна", "☀️ Лето", "🍂 Осень", "❄️ Зима"],
      best: "Новый рекорд! 🎉", notBest: (n) => `В этот раз ${n} очков. Рекорд остался прежним.`,
      zero: "В этот раз без очков. Попробуйте ещё!", checking: "Проверяем результат…",
      noSession: "Откройте игру в Telegram: напишите боту /otinchi и нажмите «Play».",
      session: "Ссылка на игру устарела. Нажмите «Play» в Telegram ещё раз.",
      implausible: "Этот результат не принят.", unavailable: "Нет связи. Попробуйте ещё раз.",
    },
    en: {
      lead: "Chop the old plane tree and dodge its branches!",
      rule1: "Tap the left or right side of the screen: the woodcutter chops from that side.",
      rule2: "Don't stand under a branch, or it falls on you.",
      rule3: "Time runs down, and each chop adds a little. Chop fast!",
      rule4: "A golden piece is worth 5. The season changes every 50 pieces.",
      play: "Chop!", table: "Leaderboard", tapHint: "👈 Tap 👉", again: "Chop again", share: "Share",
      branch: "Hit by a branch!", time: "Time's up!", seasons: ["🌸 Spring", "☀️ Summer", "🍂 Autumn", "❄️ Winter"],
      best: "New record! 🎉", notBest: (n) => `${n} points this time. Your record stands.`,
      zero: "No points this time. Try again!", checking: "Checking the result…",
      noSession: "Open the game in Telegram: send the bot /otinchi and press «Play».",
      session: "The game's link has expired. Press «Play» in Telegram again.",
      implausible: "This result was not accepted.", unavailable: "No connection. Please try again.",
    },
  };
  const language = (navigator.language || "").toLowerCase().slice(0, 2);
  const t = texts[language] || texts.uz;
  document.documentElement.lang = texts[language] ? language : "uz";
  for (const node of document.querySelectorAll("[data-t]")) node.textContent = t[node.dataset.t];
  const session = new URLSearchParams(location.search).get("s") || "";

  // The seasons: the sky, the leaves, the ground.
  const SEASONS = [
    { sky: ["#8fd3ff", "#e6f6ff"], leaves: ["#ff9ec7", "#ffd1e3", "#f472b6"], ground: "#7cc66a", snow: false },
    { sky: ["#5ec2ff", "#c9ecff"], leaves: ["#3fa53a", "#6fd06f", "#2e7d32"], ground: "#5fae4a", snow: false },
    { sky: ["#ffb978", "#ffe6c4"], leaves: ["#f08a24", "#ffc23d", "#c2410c"], ground: "#b58a3e", snow: false },
    { sky: ["#9fb7d6", "#eef4fb"], leaves: ["#ffffff", "#e6eef7", "#cfdcea"], ground: "#f3f7fb", snow: true },
  ];

  function show(id) {
    for (const screen of document.querySelectorAll(".screen")) screen.hidden = screen.id !== id;
  }

  async function api(body) {
    const response = await fetch("/api/game", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    const data = await response.json().catch(() => ({ error: "unavailable" }));
    if (!response.ok || data.error) throw new Error(data.error || "unavailable");
    return data;
  }

  function problem(error) {
    return { session: t.session, implausible: t.implausible }[error.message] || t.unavailable;
  }

  // ---------- the scene ----------

  const canvas = $("scene");
  const ctx = canvas.getContext("2d");
  let W = 0, H = 0, dpr = 1;
  let clouds = [];

  function resize() {
    dpr = Math.min(window.devicePixelRatio || 1, 2);
    W = innerWidth;
    H = innerHeight;
    canvas.width = W * dpr;
    canvas.height = H * dpr;
    clouds = Array.from({ length: 4 }, (_, i) => ({ x: Math.random() * W, y: 40 + i * 45 + Math.random() * 30, s: 0.6 + Math.random() * 0.6 }));
  }

  const groundY = () => H * 0.84;
  const trunkW = () => Math.min(W * 0.22, 104);
  const pieceH = () => trunkW() * 0.82;

  let play = null;
  const fx = { drop: 0, swing: 0, shake: 0, logs: [], parts: [], floats: [], snow: [] };

  function season() {
    return play ? Math.floor(play.game.chops / 50) % 4 : 1;
  }

  function drawBackground(now) {
    const s = SEASONS[season()];
    const g = ctx.createLinearGradient(0, 0, 0, groundY());
    g.addColorStop(0, s.sky[0]);
    g.addColorStop(1, s.sky[1]);
    ctx.fillStyle = g;
    ctx.fillRect(-20, -20, W + 40, H + 40);
    ctx.fillStyle = "rgba(255, 244, 200, 0.95)";
    ctx.beginPath();
    ctx.arc(W * 0.82, H * 0.12, Math.min(W, H) * 0.07, 0, Math.PI * 2);
    ctx.fill();
    for (const c of clouds) {
      const x = (c.x + now * 0.008 * c.s) % (W + 160) - 80;
      ctx.fillStyle = "rgba(255, 255, 255, 0.85)";
      ctx.beginPath();
      ctx.arc(x, c.y, 18 * c.s, 0, Math.PI * 2);
      ctx.arc(x + 20 * c.s, c.y - 8 * c.s, 22 * c.s, 0, Math.PI * 2);
      ctx.arc(x + 44 * c.s, c.y, 17 * c.s, 0, Math.PI * 2);
      ctx.fill();
    }
    const gy = groundY();
    ctx.fillStyle = "rgba(90, 120, 90, 0.35)";
    ctx.beginPath();
    ctx.moveTo(0, gy - H * 0.16);
    ctx.quadraticCurveTo(W * 0.3, gy - H * 0.26, W * 0.6, gy - H * 0.17);
    ctx.quadraticCurveTo(W * 0.85, gy - H * 0.1, W, gy - H * 0.2);
    ctx.lineTo(W, gy);
    ctx.lineTo(0, gy);
    ctx.fill();
    // The mahalla's adobe wall, with a carved gate.
    const wallTop = gy - H * 0.11;
    ctx.fillStyle = "#d9b27c";
    ctx.fillRect(0, wallTop, W, gy - wallTop);
    ctx.fillStyle = "rgba(140, 95, 50, 0.25)";
    for (let y = wallTop + 10; y < gy; y += 14) ctx.fillRect(0, y, W, 2);
    ctx.fillStyle = "#c79a5f";
    ctx.fillRect(0, wallTop - 6, W, 8);
    const gx = W * 0.12, gw = Math.min(64, W * 0.15);
    ctx.fillStyle = "#7a4a22";
    ctx.beginPath();
    ctx.moveTo(gx, gy);
    ctx.lineTo(gx, wallTop + 12);
    ctx.quadraticCurveTo(gx + gw / 2, wallTop - 10, gx + gw, wallTop + 12);
    ctx.lineTo(gx + gw, gy);
    ctx.fill();
    ctx.strokeStyle = "#e0b060";
    ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.moveTo(gx + gw / 2, wallTop + 6);
    ctx.lineTo(gx + gw / 2, gy);
    ctx.stroke();
    ctx.fillStyle = s.ground;
    ctx.fillRect(-20, gy, W + 40, H - gy + 20);
    ctx.fillStyle = "rgba(0, 0, 0, 0.08)";
    ctx.fillRect(-20, gy, W + 40, 4);
  }

  function drawLeaves(x, y, r, s, now) {
    const sway = Math.sin(now / 600 + x) * 2;
    const blobs = [[0, 0, 1], [-0.55, 0.15, 0.75], [0.55, 0.12, 0.75], [-0.25, -0.45, 0.7], [0.3, -0.42, 0.7]];
    blobs.forEach(([dx, dy, k], i) => {
      ctx.fillStyle = s.leaves[i % 3];
      ctx.beginPath();
      ctx.arc(x + dx * r + sway, y + dy * r, r * k * 0.62, 0, Math.PI * 2);
      ctx.fill();
    });
    if (s.snow) {
      ctx.fillStyle = "#ffffff";
      ctx.beginPath();
      ctx.ellipse(x + sway, y - r * 0.45, r * 0.7, r * 0.22, 0, Math.PI, 0);
      ctx.fill();
    }
  }

  function drawPiece(x, y, w, h, golden, now) {
    const g = ctx.createLinearGradient(x, 0, x + w, 0);
    if (golden) {
      g.addColorStop(0, "#c98a10");
      g.addColorStop(0.4, "#ffe08a");
      g.addColorStop(1, "#b5760c");
    } else {
      g.addColorStop(0, "#6b3a14");
      g.addColorStop(0.35, "#a8642c");
      g.addColorStop(0.7, "#8a4f24");
      g.addColorStop(1, "#5a2f10");
    }
    ctx.fillStyle = g;
    ctx.fillRect(x, y, w, h + 1);
    ctx.strokeStyle = golden ? "rgba(255, 255, 255, 0.5)" : "rgba(60, 30, 10, 0.35)";
    ctx.lineWidth = 2;
    for (let k = 1; k < 4; k++) {
      ctx.beginPath();
      ctx.moveTo(x + (w * k) / 4 + Math.sin(y + k) * 3, y + 4);
      ctx.lineTo(x + (w * k) / 4 + Math.cos(y + k) * 3, y + h - 4);
      ctx.stroke();
    }
    if (golden) {
      ctx.save();
      ctx.shadowColor = "#ffd23d";
      ctx.shadowBlur = 18 + Math.sin(now / 150) * 6;
      ctx.strokeStyle = "#fff3b0";
      ctx.strokeRect(x + 3, y + 3, w - 6, h - 6);
      ctx.restore();
    }
  }

  function drawBranch(x, y, side, w, h, now) {
    const s = SEASONS[season()];
    const dir = side === OtinchiGame.LEFT ? -1 : 1;
    const from = x + (dir < 0 ? 0 : w);
    const len = w * 1.35, thick = h * 0.24;
    ctx.fillStyle = "#6b3a14";
    ctx.beginPath();
    ctx.moveTo(from, y + h * 0.42);
    ctx.lineTo(from + dir * len, y + h * 0.36 - thick * 0.3);
    ctx.lineTo(from + dir * len, y + h * 0.36 + thick * 0.3);
    ctx.lineTo(from, y + h * 0.42 + thick);
    ctx.fill();
    drawLeaves(from + dir * len, y + h * 0.36, h * 0.62, s, now);
  }

  function drawTree(now) {
    const w = trunkW(), h = pieceH(), x = W / 2 - w / 2, gy = groundY();
    if (!play) {
      // A trunk to look at before the play.
      for (let k = 0; k * h < gy + h; k++) drawPiece(x, gy - (k + 1) * h, w, h, false, now);
      drawBranch(x, gy - 4 * h, OtinchiGame.LEFT, w, h, now);
      drawBranch(x, gy - 6 * h, OtinchiGame.RIGHT, w, h, now);
      return;
    }
    const game = play.game;
    for (let k = 0; ; k++) {
      const y = gy - (k + 1) * h - fx.drop;
      if (y + h < -h) break;
      const [branch, golden] = game.tree.at(game.chops + k);
      drawPiece(x, y, w, h, golden, now);
      if (branch !== OtinchiGame.NONE) drawBranch(x, y, branch, w, h, now);
    }
    // The stump.
    ctx.fillStyle = "#5a2f10";
    ctx.beginPath();
    ctx.ellipse(W / 2, gy + 2, w * 0.62, h * 0.14, 0, 0, Math.PI * 2);
    ctx.fill();
  }

  // drawCutter draws the woodcutter in a do'ppi and a striped chopon, by
  // the trunk on side, the axe swung swing of the way.
  function drawCutter(side, swing, dead, now) {
    const w = trunkW(), h = pieceH(), gy = groundY();
    const dir = side === OtinchiGame.LEFT ? -1 : 1;
    const x = W / 2 + dir * (w / 2 + h * 0.62), s = h / 60;
    ctx.save();
    ctx.translate(x, gy);
    if (dead) ctx.rotate(dir * Math.min(1.4, (now - dead) / 250));
    ctx.scale(dir * s, s); // drawn facing left; turned to face the trunk
    ctx.fillStyle = "rgba(0, 0, 0, 0.18)";
    ctx.beginPath();
    ctx.ellipse(0, 0, 22, 5, 0, 0, Math.PI * 2);
    ctx.fill();
    ctx.fillStyle = "#3b2a1a";
    ctx.fillRect(-10, -22, 8, 22);
    ctx.fillRect(3, -22, 8, 22);
    ctx.fillStyle = "#1d3c8a";
    ctx.beginPath();
    ctx.moveTo(-16, -20);
    ctx.lineTo(16, -20);
    ctx.lineTo(12, -62);
    ctx.lineTo(-12, -62);
    ctx.fill();
    ctx.strokeStyle = "#ffc23d";
    ctx.lineWidth = 2;
    for (const k of [-7, 0, 7]) {
      ctx.beginPath();
      ctx.moveTo(k, -22);
      ctx.lineTo(k * 0.8, -60);
      ctx.stroke();
    }
    ctx.fillStyle = "#c62828";
    ctx.fillRect(-14, -42, 28, 5);
    ctx.fillStyle = "#e9b48a";
    ctx.beginPath();
    ctx.arc(0, -72, 11, 0, Math.PI * 2);
    ctx.fill();
    ctx.fillStyle = "#3b2414";
    ctx.beginPath();
    ctx.arc(0, -66, 8, 0.2, Math.PI - 0.2);
    ctx.fill();
    ctx.fillStyle = "#111";
    ctx.beginPath();
    ctx.moveTo(-12, -78);
    ctx.lineTo(12, -78);
    ctx.lineTo(10, -88);
    ctx.lineTo(-10, -88);
    ctx.fill();
    ctx.fillStyle = "#fff";
    for (const k of [-6, 0, 6]) ctx.fillRect(k - 1.5, -86, 3, 6);
    ctx.fillStyle = "#1a0c00";
    ctx.beginPath();
    ctx.arc(-5, -73, 1.8, 0, Math.PI * 2);
    ctx.fill();
    // The axe, swung towards the trunk.
    ctx.save();
    ctx.translate(-10, -48);
    ctx.rotate(-1.9 + (1 - swing) * 0.2 + swing * 1.9);
    ctx.fillStyle = "#8a5a2a";
    ctx.fillRect(-3, -46, 6, 50);
    ctx.fillStyle = "#cfd8dc";
    ctx.beginPath();
    ctx.moveTo(-3, -46);
    ctx.lineTo(-20, -52);
    ctx.quadraticCurveTo(-24, -40, -20, -30);
    ctx.lineTo(-3, -34);
    ctx.fill();
    ctx.restore();
    ctx.restore();
  }

  function drawEffects(dt) {
    for (const log of fx.logs) {
      log.x += log.vx * dt;
      log.y += log.vy * dt;
      log.vy += 0.0025 * dt;
      log.r += log.vr * dt;
      log.life -= dt / 900;
    }
    fx.logs = fx.logs.filter((l) => l.life > 0);
    for (const log of fx.logs) {
      ctx.save();
      ctx.translate(log.x, log.y);
      ctx.rotate(log.r);
      ctx.globalAlpha = Math.min(1, log.life * 2);
      drawPiece(-log.w / 2, -log.h / 2, log.w, log.h, log.golden, 0);
      ctx.restore();
    }
    for (const p of fx.parts) {
      p.x += p.vx * dt;
      p.y += p.vy * dt;
      p.vy += 0.0018 * dt;
      p.life -= dt / 600;
    }
    fx.parts = fx.parts.filter((p) => p.life > 0);
    for (const p of fx.parts) {
      ctx.globalAlpha = Math.max(0, p.life);
      ctx.fillStyle = p.color;
      ctx.fillRect(p.x, p.y, p.s, p.s * 0.6);
    }
    ctx.globalAlpha = 1;
    for (const f of fx.floats) {
      f.y -= dt * 0.06;
      f.life -= dt / 800;
    }
    fx.floats = fx.floats.filter((f) => f.life > 0);
    ctx.textAlign = "center";
    for (const f of fx.floats) {
      ctx.globalAlpha = Math.min(1, f.life * 2);
      ctx.font = `${f.size}px "Lilita One", system-ui`;
      ctx.lineWidth = 5;
      ctx.strokeStyle = "#6b3a14";
      ctx.strokeText(f.text, f.x, f.y);
      ctx.fillStyle = f.color;
      ctx.fillText(f.text, f.x, f.y);
    }
    ctx.globalAlpha = 1;
    if (SEASONS[season()].snow && !reduced) {
      if (fx.snow.length < 60) fx.snow.push({ x: Math.random() * W, y: -10, v: 0.03 + Math.random() * 0.05, r: 1 + Math.random() * 2.5 });
      for (const f of fx.snow) {
        f.y += f.v * dt;
        f.x += Math.sin(f.y / 40) * 0.3;
      }
      fx.snow = fx.snow.filter((f) => f.y < H);
      ctx.fillStyle = "#ffffff";
      for (const f of fx.snow) {
        ctx.beginPath();
        ctx.arc(f.x, f.y, f.r, 0, Math.PI * 2);
        ctx.fill();
      }
    } else {
      fx.snow = [];
    }
  }

  // ---------- play ----------

  async function begin() {
    $("play").disabled = $("again").disabled = true;
    try {
      const dealt = await api({ action: "start", session });
      play = { run: dealt.run, rules: dealt.rules, game: new OtinchiGame(dealt.seed, dealt.rules), t0: null, sides: "", times: [], over: null, dead: 0 };
      Object.assign(fx, { drop: 0, swing: 0, shake: 0, logs: [], parts: [], floats: [] });
      show("game");
      $("tapHint").hidden = false;
      $("score").textContent = "0";
      $("season").textContent = t.seasons[0];
    } catch (error) {
      play = null;
      show("intro");
      $("introMessage").textContent = problem(error);
    } finally {
      $("play").disabled = $("again").disabled = false;
    }
  }

  function chop(side) {
    if (!play || play.over) return;
    const now = performance.now();
    if (play.t0 === null) play.t0 = now;
    const at = Math.round(now - play.t0);
    const game = play.game;
    const golden = game.tree.at(game.chops)[1];
    const result = game.chop(side, at);
    if (result.result === "too_soon") return;
    if (result.result === "time") {
      end("time");
      return;
    }
    play.sides += side === OtinchiGame.LEFT ? "L" : "R";
    play.times.push(at);
    $("tapHint").hidden = true;
    fx.swing = 1;
    const w = trunkW(), h = pieceH(), gy = groundY();
    if (result.result === "branch" && result.stepped) {
      end("branch");
      return;
    }
    // The chopped piece flies off away from the woodcutter.
    const away = side === OtinchiGame.LEFT ? 1 : -1;
    fx.logs.push({ x: W / 2, y: gy - h / 2, vx: away * (0.5 + Math.random() * 0.25), vy: -0.45, r: 0, vr: away * 0.012, w, h, golden, life: 1 });
    for (let i = 0; i < (reduced ? 4 : 12); i++) {
      fx.parts.push({ x: W / 2 - away * w * 0.4, y: gy - h / 2, vx: (Math.random() - 0.3) * away * 0.4, vy: -Math.random() * 0.5, s: 4 + Math.random() * 5, color: Math.random() < 0.5 ? "#e8c58a" : "#a8642c", life: 1 });
    }
    fx.drop = h;
    if (navigator.vibrate) navigator.vibrate(golden ? 25 : 8);
    if (result.result === "branch") {
      end("branch");
      return;
    }
    if (golden) {
      fx.floats.push({ x: W / 2, y: gy - h * 1.6, text: `+${play.rules.golden}`, color: "#ffd23d", size: 34, life: 1 });
      for (let i = 0; i < 18; i++) fx.parts.push({ x: W / 2, y: gy - h / 2, vx: (Math.random() - 0.5) * 0.6, vy: -Math.random() * 0.6, s: 5, color: "#ffd23d", life: 1 });
    }
    const score = $("score");
    score.textContent = game.score;
    score.classList.remove("pop");
    void score.offsetWidth;
    score.classList.add("pop");
    if (game.chops % 50 === 0) banner(t.seasons[season()]);
    $("season").textContent = t.seasons[season()];
  }

  function banner(text) {
    const b = $("banner");
    b.textContent = text;
    b.hidden = false;
    b.style.animation = "none";
    void b.offsetWidth;
    b.style.animation = "";
    setTimeout(() => { b.hidden = true; }, 1100);
  }

  function end(why) {
    if (play.over) return;
    play.over = why;
    play.dead = performance.now();
    fx.shake = why === "branch" ? 1 : 0.4;
    if (navigator.vibrate) navigator.vibrate([60, 40, 100]);
    banner(why === "branch" ? t.branch : t.time);
    setTimeout(finish, 1200);
  }

  async function finish() {
    const done = play;
    show("result");
    $("why").textContent = done.over === "branch" ? t.branch : t.time;
    $("final").textContent = done.game.score;
    $("resultTable").hidden = true;
    if (!done.sides.length) {
      $("resultMessage").textContent = t.zero;
      return;
    }
    $("resultMessage").textContent = t.checking;
    try {
      const result = await api({ action: "finish", run: done.run, sides: done.sides, times: done.times });
      $("final").textContent = result.score;
      $("resultMessage").textContent = result.score === 0 ? t.zero : result.improved ? t.best : t.notBest(result.score);
      fillTable("resultTable", "resultRows", result.table);
    } catch (error) {
      $("resultMessage").textContent = problem(error);
    }
  }

  function fillTable(cardId, listId, rows) {
    const list = $(listId);
    list.textContent = "";
    for (const row of rows || []) {
      const item = document.createElement("li");
      if (row.me) item.className = "me";
      for (const [cls, text] of [["pos", row.position], ["name", row.name], ["pts", row.score]]) {
        const span = document.createElement("span");
        span.className = cls;
        span.textContent = text;
        item.append(span);
      }
      list.append(item);
    }
    $(cardId).hidden = !rows || rows.length === 0;
  }

  // ---------- input ----------

  // The page's screens lie over the scene, so the taps are heard on the
  // window, while a play is on.
  addEventListener("pointerdown", (e) => {
    if ($("game").hidden || (e.target instanceof Element && e.target.closest("button"))) return;
    e.preventDefault();
    chop(e.clientX < W / 2 ? OtinchiGame.LEFT : OtinchiGame.RIGHT);
  });
  addEventListener("keydown", (e) => {
    if ($("game").hidden || e.repeat) return;
    if (e.code === "ArrowLeft" || e.code === "KeyA") chop(OtinchiGame.LEFT);
    if (e.code === "ArrowRight" || e.code === "KeyD") chop(OtinchiGame.RIGHT);
  });
  $("play").addEventListener("click", begin);
  $("again").addEventListener("click", begin);
  $("share").addEventListener("click", () => {
    if (window.TelegramGameProxy) TelegramGameProxy.shareScore();
  });

  // ---------- the loop ----------

  let last = performance.now();
  function frame(now) {
    const dt = Math.min(50, now - last);
    last = now;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    if (fx.shake > 0) {
      fx.shake = Math.max(0, fx.shake - dt / 400);
      ctx.translate((Math.random() - 0.5) * fx.shake * 18, (Math.random() - 0.5) * fx.shake * 18);
    }
    fx.drop = Math.max(0, fx.drop - dt * pieceH() / 70);
    fx.swing = Math.max(0, fx.swing - dt / 120);
    drawBackground(now);
    drawTree(now);
    if (play) {
      const game = play.game;
      if (play.t0 !== null && !play.over) {
        const left = game.leftAt(Math.round(now - play.t0));
        if (left <= 0) end("time");
        const share = Math.max(0, left) / play.rules.most_ms;
        const bar = $("time");
        bar.style.width = `${share * 100}%`;
        bar.className = share > 0.5 ? "ok" : share > 0.25 ? "mid" : "";
      } else if (play.t0 === null) {
        $("time").style.width = `${(game.left / play.rules.most_ms) * 100}%`;
        $("time").className = "ok";
      }
      drawCutter(game.side, fx.swing, play.over ? play.dead : 0, now);
    } else {
      drawCutter(OtinchiGame.LEFT, (Math.sin(now / 300) + 1) / 2, 0, now);
    }
    drawEffects(dt);
    requestAnimationFrame(frame);
  }

  async function introTable() {
    if (!session) return;
    try {
      const data = await api({ action: "table", session });
      fillTable("introTable", "introRows", data.table);
    } catch (_) {
      // The table is a nicety; the game is played without it.
    }
  }

  addEventListener("resize", resize);
  resize();
  if (!session) {
    $("play").disabled = true;
    $("introMessage").textContent = t.noSession;
  }
  introTable();
  requestAnimationFrame(frame);
})();
