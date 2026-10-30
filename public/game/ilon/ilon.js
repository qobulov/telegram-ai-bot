// Kosmik ilon. The server gives each play its seed and rules and, at the
// end, plays the turns sent again to count the score; the score shown
// while playing is the page's own count of the same rules.
"use strict";

(function () {
  const $ = (id) => document.getElementById(id);
  const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;

  const texts = {
    uz: {
      lead: "Ilon koinotda sayohatda: sayyoralarni yeb, o'sib, darajadan darajaga o'ting!",
      rule1: "Ekranni suring yoki strelkalarni bosing: ilon o'sha tomonga buriladi.",
      rule2: "Har 10 sayyorada yangi daraja: boshqa devorlar va tezroq ilon.",
      rule3: "Sayyoralarni ketma-ket tez yesangiz kombo: ochko ×2, ×3.",
      rule4: "Chetdan chiqsangiz, narigi tomondan kirasiz. Devor va o'z dumingizga urilmang!",
      starName: "Yulduz", starAbout: "+50 × daraja",
      slowName: "Muz soat", slowAbout: "Vaqt sekinlashadi",
      ghostName: "Arvoh", ghostAbout: "Devor va dumdan o'tasiz",
      play: "Boshlash", score: "Ochko", level: "Daraja", paused: "PAUZA", resume: "Davom etish",
      hint: "Suring yoki ← ↑ → ↓ · Probel: pauza",
      over: "O'yin tugadi", again: "Yana o'ynash", share: "Ulashish", table: "Reyting",
      best: "Yangi rekord! 🎉", notBest: (n) => `Bu safar ${n} ochko. Rekordingiz saqlanib qoldi.`,
      zero: "Bu safar ochko yo'q. Yana urinib ko'ring!", checking: "Natija tekshirilmoqda…",
      summary: (level, planets) => `${level}-daraja · ${planets} ta sayyora`,
      levelBanner: (n) => `${n}-DARAJA`, levelBonus: (p) => `+${p} bonus`,
      slow: "Sekinlashdi!", ghost: "Arvoh rejimi!", combo: (n) => `KOMBO ×${n}`,
      chipSlow: "❄ Sekin", chipGhost: "👻 Arvoh",
      noSession: "O'yinni Telegram'da oching: botga /ilon yozing va «Play» ni bosing.",
      session: "O'yin havolasi eskirdi. Telegram'da «Play» ni qaytadan bosing.",
      implausible: "Bu natija qabul qilinmadi.", unavailable: "Aloqa bo'lmadi. Qayta urinib ko'ring.",
    },
    ru: {
      lead: "Змейка путешествует по космосу: ешьте планеты, растите и проходите уровни!",
      rule1: "Проведите пальцем или нажмите стрелку: змейка повернёт туда.",
      rule2: "Каждые 10 планет — новый уровень: другие стены и быстрее змейка.",
      rule3: "Ешьте планеты быстро подряд — комбо: очки ×2, ×3.",
      rule4: "Ушли за край — выйдете с другой стороны. Не врезайтесь в стены и в свой хвост!",
      starName: "Звезда", starAbout: "+50 × уровень",
      slowName: "Ледяные часы", slowAbout: "Время замедляется",
      ghostName: "Призрак", ghostAbout: "Сквозь стены и хвост",
      play: "Начать", score: "Очки", level: "Уровень", paused: "ПАУЗА", resume: "Продолжить",
      hint: "Свайп или ← ↑ → ↓ · Пробел: пауза",
      over: "Игра окончена", again: "Ещё раз", share: "Поделиться", table: "Рейтинг",
      best: "Новый рекорд! 🎉", notBest: (n) => `В этот раз ${n} очков. Рекорд остался прежним.`,
      zero: "В этот раз без очков. Попробуйте ещё!", checking: "Проверяем результат…",
      summary: (level, planets) => `Уровень ${level} · планет: ${planets}`,
      levelBanner: (n) => `УРОВЕНЬ ${n}`, levelBonus: (p) => `+${p} бонус`,
      slow: "Замедление!", ghost: "Режим призрака!", combo: (n) => `КОМБО ×${n}`,
      chipSlow: "❄ Медленно", chipGhost: "👻 Призрак",
      noSession: "Откройте игру в Telegram: напишите боту /ilon и нажмите «Play».",
      session: "Ссылка на игру устарела. Нажмите «Play» в Telegram ещё раз.",
      implausible: "Этот результат не принят.", unavailable: "Нет связи. Попробуйте ещё раз.",
    },
    en: {
      lead: "A snake adrift in space: eat planets, grow, and fly from level to level!",
      rule1: "Swipe or press an arrow: the snake turns that way.",
      rule2: "Every 10 planets is a new level: other walls and a quicker snake.",
      rule3: "Eat planets quickly one after another for a combo: points ×2, ×3.",
      rule4: "Leave by an edge and you come back on the other side. Don't hit the walls or your own tail!",
      starName: "Star", starAbout: "+50 × level",
      slowName: "Ice clock", slowAbout: "Time slows down",
      ghostName: "Ghost", ghostAbout: "Through walls and tail",
      play: "Start", score: "Score", level: "Level", paused: "PAUSED", resume: "Resume",
      hint: "Swipe or ← ↑ → ↓ · Space: pause",
      over: "Game over", again: "Play again", share: "Share", table: "Leaderboard",
      best: "New record! 🎉", notBest: (n) => `${n} points this time. Your record stands.`,
      zero: "No points this time. Try again!", checking: "Checking the result…",
      summary: (level, planets) => `Level ${level} · ${planets} planets`,
      levelBanner: (n) => `LEVEL ${n}`, levelBonus: (p) => `+${p} bonus`,
      slow: "Slowed down!", ghost: "Ghost mode!", combo: (n) => `COMBO ×${n}`,
      chipSlow: "❄ Slow", chipGhost: "👻 Ghost",
      noSession: "Open the game in Telegram: send the bot /ilon and press «Play».",
      session: "The game's link has expired. Press «Play» in Telegram again.",
      implausible: "This result was not accepted.", unavailable: "No connection. Please try again.",
    },
  };
  const language = (navigator.language || "").toLowerCase().slice(0, 2);
  const t = texts[language] || texts.uz;
  document.documentElement.lang = texts[language] ? language : "uz";

  const session = new URLSearchParams(location.search).get("s") || "";
  const WALL_HUES = [195, 275, 20, 160, 320, 45];
  const BONUS_COLORS = ["#ffd166", "#8fd3ff", "#b69cff"];

  function translate() {
    for (const node of document.querySelectorAll("[data-t]")) node.textContent = t[node.dataset.t];
    $("pauseButton").setAttribute("aria-label", t.paused);
  }

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

  function buzz(ms) {
    if (navigator.vibrate) navigator.vibrate(ms);
  }

  // ---------- the sky ----------

  const sky = $("sky");
  const skyCtx = sky.getContext("2d");
  let stars = [];
  let warp = 0;
  let dpr = 1;

  function resizeSky() {
    dpr = Math.min(window.devicePixelRatio || 1, 2);
    sky.width = innerWidth * dpr;
    sky.height = innerHeight * dpr;
    const count = Math.round((innerWidth * innerHeight) / 2600);
    stars = Array.from({ length: count }, () => ({
      x: Math.random() * innerWidth,
      y: Math.random() * innerHeight,
      z: 0.2 + Math.random() * 0.8,
      tw: Math.random() * Math.PI * 2,
    }));
  }

  function drawSky(dt, now) {
    const c = skyCtx;
    c.setTransform(dpr, 0, 0, dpr, 0, 0);
    const w = innerWidth, h = innerHeight;
    const g = c.createRadialGradient(w * 0.7, h * 0.15, 0, w * 0.7, h * 0.15, Math.max(w, h) * 0.9);
    g.addColorStop(0, "#1b1446");
    g.addColorStop(0.45, "#0a0d27");
    g.addColorStop(1, "#050816");
    c.fillStyle = g;
    c.fillRect(0, 0, w, h);
    const n = c.createRadialGradient(w * 0.15, h * 0.85, 0, w * 0.15, h * 0.85, w * 0.7);
    n.addColorStop(0, "rgba(255, 79, 216, 0.16)");
    n.addColorStop(1, "rgba(255, 79, 216, 0)");
    c.fillStyle = n;
    c.fillRect(0, 0, w, h);
    const speed = reduced ? 0 : 0.012 + warp * 1.6;
    for (const s of stars) {
      s.y += s.z * speed * dt;
      if (s.y > h + 20) { s.y = -20; s.x = Math.random() * w; }
      const alpha = 0.35 + 0.45 * s.z * (0.6 + 0.4 * Math.sin(now / 700 + s.tw));
      if (warp > 0.05) {
        c.strokeStyle = `rgba(200, 220, 255, ${alpha})`;
        c.lineWidth = s.z * 1.6;
        c.beginPath();
        c.moveTo(s.x, s.y);
        c.lineTo(s.x, s.y - s.z * warp * 60);
        c.stroke();
      } else {
        c.fillStyle = `rgba(220, 230, 255, ${alpha})`;
        c.fillRect(s.x, s.y, s.z * 1.8, s.z * 1.8);
      }
    }
  }

  // ---------- the board ----------

  const board = $("board");
  const ctx = board.getContext("2d");
  let cell = 20;

  function layoutBoard() {
    if (!play) return;
    const stage = $("stage").getBoundingClientRect();
    const r = play.rules;
    cell = Math.max(10, Math.floor(Math.min(stage.width / r.cols, stage.height / r.rows)));
    board.style.width = `${cell * r.cols}px`;
    board.style.height = `${cell * r.rows}px`;
    board.width = cell * r.cols * dpr;
    board.height = cell * r.rows * dpr;
  }

  const center = (c) => [(c % play.rules.cols + 0.5) * cell, (Math.floor(c / play.rules.cols) + 0.5) * cell];

  function roundRect(c, x, y, w, h, r) {
    c.beginPath();
    c.moveTo(x + r, y);
    c.arcTo(x + w, y, x + w, y + h, r);
    c.arcTo(x + w, y + h, x, y + h, r);
    c.arcTo(x, y + h, x, y, r);
    c.arcTo(x, y, x + w, y, r);
    c.closePath();
  }

  function planetHue(c) {
    return (c * 47 + play.sim.level * 61) % 360;
  }

  function drawField(now) {
    const r = play.rules, sim = play.sim;
    const w = r.cols * cell, h = r.rows * cell;
    const g = ctx.createLinearGradient(0, 0, w, h);
    g.addColorStop(0, "rgba(14, 20, 48, 0.82)");
    g.addColorStop(1, "rgba(20, 10, 44, 0.82)");
    ctx.fillStyle = g;
    roundRect(ctx, 0, 0, w, h, 18);
    ctx.fill();
    if (sim.slowed()) {
      ctx.fillStyle = "rgba(143, 211, 255, 0.07)";
      ctx.fill();
    }
    ctx.fillStyle = "rgba(160, 175, 255, 0.14)";
    for (let y = 0; y < r.rows; y++) {
      for (let x = 0; x < r.cols; x++) ctx.fillRect((x + 0.5) * cell - 1, (y + 0.5) * cell - 1, 2, 2);
    }
    const hue = WALL_HUES[(sim.level - 1) % WALL_HUES.length];
    const glow = 0.55 + 0.25 * Math.sin(now / 500);
    ctx.save();
    ctx.shadowColor = `hsla(${hue}, 100%, 60%, ${glow})`;
    ctx.shadowBlur = cell * 0.6;
    for (let c = 0; c < sim.walls.length; c++) {
      if (!sim.walls[c]) continue;
      const x = (c % r.cols) * cell, y = Math.floor(c / r.cols) * cell;
      const wg = ctx.createLinearGradient(x, y, x + cell, y + cell);
      wg.addColorStop(0, `hsl(${hue}, 70%, 46%)`);
      wg.addColorStop(1, `hsl(${(hue + 40) % 360}, 80%, 28%)`);
      ctx.fillStyle = wg;
      roundRect(ctx, x + 1.5, y + 1.5, cell - 3, cell - 3, cell * 0.22);
      ctx.fill();
    }
    ctx.restore();
    ctx.strokeStyle = `hsla(${hue}, 100%, 85%, 0.35)`;
    ctx.lineWidth = 1;
    for (let c = 0; c < sim.walls.length; c++) {
      if (!sim.walls[c]) continue;
      const x = (c % r.cols) * cell, y = Math.floor(c / r.cols) * cell;
      roundRect(ctx, x + 3, y + 3, cell - 6, cell - 6, cell * 0.16);
      ctx.stroke();
    }
  }

  function drawPlanet(c, now) {
    if (c < 0) return;
    const [x, y] = center(c);
    const hue = planetHue(c);
    const pulse = 1 + 0.08 * Math.sin(now / 220);
    const rad = cell * 0.34 * pulse;
    ctx.save();
    ctx.shadowColor = `hsl(${hue}, 100%, 60%)`;
    ctx.shadowBlur = cell * 0.9;
    const g = ctx.createRadialGradient(x - rad * 0.4, y - rad * 0.4, rad * 0.1, x, y, rad);
    g.addColorStop(0, "#fff");
    g.addColorStop(0.25, `hsl(${hue}, 100%, 70%)`);
    g.addColorStop(1, `hsl(${(hue + 30) % 360}, 90%, 38%)`);
    ctx.fillStyle = g;
    ctx.beginPath();
    ctx.arc(x, y, rad, 0, Math.PI * 2);
    ctx.fill();
    ctx.restore();
    ctx.strokeStyle = `hsla(${(hue + 180) % 360}, 100%, 80%, 0.8)`;
    ctx.lineWidth = Math.max(1.2, cell * 0.06);
    ctx.beginPath();
    ctx.ellipse(x, y, rad * 1.55, rad * 0.45, -0.4 + Math.sin(now / 900) * 0.15, 0, Math.PI * 2);
    ctx.stroke();
  }

  function starPath(x, y, outer, inner, turn) {
    ctx.beginPath();
    for (let i = 0; i < 10; i++) {
      const a = turn + (i * Math.PI) / 5 - Math.PI / 2;
      const r = i % 2 ? inner : outer;
      ctx.lineTo(x + Math.cos(a) * r, y + Math.sin(a) * r);
    }
    ctx.closePath();
  }

  function drawBonus(now) {
    const sim = play.sim;
    if (sim.bonus < 0) return;
    const left = sim.bonusEnd - sim.tick;
    if (left < 14 && Math.floor(now / 120) % 2) return;
    const [x, y] = center(sim.bonus);
    const color = BONUS_COLORS[sim.kind];
    ctx.save();
    ctx.shadowColor = color;
    ctx.shadowBlur = cell;
    ctx.fillStyle = color;
    ctx.strokeStyle = color;
    const s = cell * 0.42 * (1 + 0.1 * Math.sin(now / 160));
    if (sim.kind === IlonSim.STAR) {
      starPath(x, y, s, s * 0.45, now / 600);
      ctx.fill();
    } else if (sim.kind === IlonSim.SLOW) {
      ctx.lineWidth = Math.max(1.5, cell * 0.08);
      ctx.beginPath();
      ctx.arc(x, y, s * 0.85, 0, Math.PI * 2);
      ctx.stroke();
      const a = now / 300;
      ctx.beginPath();
      ctx.moveTo(x, y);
      ctx.lineTo(x + Math.cos(a) * s * 0.6, y + Math.sin(a) * s * 0.6);
      ctx.moveTo(x, y);
      ctx.lineTo(x + Math.cos(a / 12) * s * 0.4, y + Math.sin(a / 12) * s * 0.4);
      ctx.stroke();
    } else {
      ctx.globalAlpha = 0.85;
      ctx.beginPath();
      ctx.arc(x, y - s * 0.15, s * 0.7, Math.PI, 0);
      const wave = Math.sin(now / 120) * s * 0.08;
      ctx.lineTo(x + s * 0.7, y + s * 0.7);
      ctx.lineTo(x + s * 0.35, y + s * 0.5 + wave);
      ctx.lineTo(x, y + s * 0.7);
      ctx.lineTo(x - s * 0.35, y + s * 0.5 - wave);
      ctx.lineTo(x - s * 0.7, y + s * 0.7);
      ctx.closePath();
      ctx.fill();
      ctx.globalAlpha = 1;
      ctx.fillStyle = "#050816";
      ctx.beginPath();
      ctx.arc(x - s * 0.25, y - s * 0.15, s * 0.12, 0, Math.PI * 2);
      ctx.arc(x + s * 0.25, y - s * 0.15, s * 0.12, 0, Math.PI * 2);
      ctx.fill();
    }
    ctx.restore();
  }

  // segments are where the snake's cells are drawn, between where they were
  // and where they are, alpha of the way through the tick.
  function segments(alpha) {
    const body = play.sim.body, before = play.before;
    const r = play.rules;
    return body.map((c, i) => {
      const [x, y] = center(c);
      const from = before[Math.min(i, before.length - 1)];
      if (from === undefined || alpha >= 1) return [x, y];
      const [fx, fy] = center(from);
      if (Math.abs(fx - x) > cell * 1.5 || Math.abs(fy - y) > cell * 1.5) {
        return [x, y]; // through an edge: it shows on the other side
      }
      return [fx + (x - fx) * alpha, fy + (y - fy) * alpha];
    });
  }

  function drawSnake(alpha, now) {
    const sim = play.sim;
    if (play.exploded) return;
    const points = segments(alpha);
    const ghost = sim.ghost();
    ctx.save();
    if (ghost) ctx.globalAlpha = 0.45 + 0.2 * Math.sin(now / 70);
    ctx.lineCap = "round";
    ctx.lineJoin = "round";
    const n = points.length;
    for (let i = n - 1; i > 0; i--) {
      const [x0, y0] = points[i], [x1, y1] = points[i - 1];
      if (Math.abs(x0 - x1) > cell * 1.5 || Math.abs(y0 - y1) > cell * 1.5) continue;
      const k = i / Math.max(n - 1, 1);
      const hue = ghost ? 265 : 185 + k * 125;
      ctx.strokeStyle = `hsl(${hue}, 100%, ${62 - k * 10}%)`;
      ctx.shadowColor = `hsl(${hue}, 100%, 60%)`;
      ctx.shadowBlur = cell * 0.5;
      ctx.lineWidth = cell * (0.7 - k * 0.32);
      ctx.beginPath();
      ctx.moveTo(x0, y0);
      ctx.lineTo(x1, y1);
      ctx.stroke();
    }
    const [hx, hy] = points[0];
    ctx.shadowBlur = cell;
    ctx.shadowColor = ghost ? "#b69cff" : "#3ef0ff";
    const hg = ctx.createRadialGradient(hx - cell * 0.15, hy - cell * 0.15, 1, hx, hy, cell * 0.46);
    hg.addColorStop(0, "#ffffff");
    hg.addColorStop(0.4, ghost ? "#cbb8ff" : "#7ff6ff");
    hg.addColorStop(1, ghost ? "#7b5cff" : "#14b8d9");
    ctx.fillStyle = hg;
    ctx.beginPath();
    ctx.arc(hx, hy, cell * 0.46, 0, Math.PI * 2);
    ctx.fill();
    ctx.shadowBlur = 0;
    const dx = [0, 1, 0, -1][sim.dir], dy = [-1, 0, 1, 0][sim.dir];
    const blink = Math.floor(now / 1700) % 6 === 0 && now % 1700 < 140;
    for (const side of [-1, 1]) {
      const ex = hx + dx * cell * 0.14 - dy * side * cell * 0.19;
      const ey = hy + dy * cell * 0.14 + dx * side * cell * 0.19;
      ctx.fillStyle = "#050816";
      ctx.beginPath();
      if (blink) ctx.fillRect(ex - cell * 0.1, ey - 1, cell * 0.2, 2);
      else ctx.arc(ex, ey, cell * 0.1, 0, Math.PI * 2);
      ctx.fill();
      if (!blink) {
        ctx.fillStyle = "#fff";
        ctx.beginPath();
        ctx.arc(ex + dx * cell * 0.03, ey + dy * cell * 0.03, cell * 0.035, 0, Math.PI * 2);
        ctx.fill();
      }
    }
    if (Math.sin(now / 180) > 0.6) {
      ctx.strokeStyle = "#ff4f8b";
      ctx.lineWidth = Math.max(1.2, cell * 0.06);
      ctx.beginPath();
      const tx = hx + dx * cell * 0.45, ty = hy + dy * cell * 0.45;
      ctx.moveTo(tx, ty);
      ctx.lineTo(tx + dx * cell * 0.22 - dy * cell * 0.08, ty + dy * cell * 0.22 + dx * cell * 0.08);
      ctx.moveTo(tx + dx * cell * 0.12, ty + dy * cell * 0.12);
      ctx.lineTo(tx + dx * cell * 0.22 + dy * cell * 0.08, ty + dy * cell * 0.22 - dx * cell * 0.08);
      ctx.stroke();
    }
    ctx.restore();
  }

  function burst(x, y, color, count, speed) {
    if (reduced) count = Math.ceil(count / 3);
    for (let i = 0; i < count; i++) {
      const a = Math.random() * Math.PI * 2, v = (0.3 + Math.random()) * speed;
      play.particles.push({ x, y, vx: Math.cos(a) * v, vy: Math.sin(a) * v, life: 1, color, size: 1.5 + Math.random() * 2.5 });
    }
  }

  function floater(x, y, text, color, big) {
    play.floaters.push({ x, y, text, color, life: 1, big });
  }

  function drawEffects(dt) {
    for (const p of play.particles) {
      p.x += p.vx * dt * 0.06;
      p.y += p.vy * dt * 0.06;
      p.vx *= 0.97;
      p.vy *= 0.97;
      p.life -= dt / 700;
    }
    play.particles = play.particles.filter((p) => p.life > 0);
    ctx.save();
    for (const p of play.particles) {
      ctx.globalAlpha = Math.max(p.life, 0);
      ctx.fillStyle = p.color;
      ctx.shadowColor = p.color;
      ctx.shadowBlur = 8;
      ctx.beginPath();
      ctx.arc(p.x, p.y, p.size * p.life + 0.5, 0, Math.PI * 2);
      ctx.fill();
    }
    ctx.restore();
    for (const f of play.floaters) {
      f.y -= dt * 0.03;
      f.life -= dt / 1100;
    }
    play.floaters = play.floaters.filter((f) => f.life > 0);
    ctx.save();
    ctx.textAlign = "center";
    for (const f of play.floaters) {
      ctx.globalAlpha = Math.min(1, f.life * 2);
      ctx.font = `700 ${f.big ? cell * 0.75 : cell * 0.55}px Unbounded, system-ui, sans-serif`;
      ctx.lineWidth = 4;
      ctx.strokeStyle = "rgba(5, 8, 22, 0.8)";
      ctx.strokeText(f.text, f.x, f.y);
      ctx.fillStyle = f.color;
      ctx.fillText(f.text, f.x, f.y);
    }
    ctx.restore();
  }

  function render(alpha, dt, now) {
    const r = play.rules;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, r.cols * cell, r.rows * cell);
    if (play.shake > 0) {
      play.shake = Math.max(0, play.shake - dt / 400);
      const m = play.shake * cell * 0.35;
      ctx.translate((Math.random() - 0.5) * m, (Math.random() - 0.5) * m);
    }
    drawField(now);
    drawPlanet(play.sim.planet, now);
    drawBonus(now);
    drawSnake(alpha, now);
    drawEffects(dt);
    if (play.flash > 0) {
      play.flash = Math.max(0, play.flash - dt / 500);
      ctx.fillStyle = `rgba(255, 90, 110, ${play.flash * 0.45})`;
      ctx.fillRect(-cell, -cell, (r.cols + 2) * cell, (r.rows + 2) * cell);
    }
  }

  // ---------- the HUD ----------

  function hud() {
    const sim = play.sim, r = play.rules;
    if ($("score").textContent !== String(sim.score)) {
      $("score").textContent = sim.score;
      $("score").classList.remove("pop");
      void $("score").offsetWidth;
      $("score").classList.add("pop");
    }
    $("level").textContent = sim.level;
    const got = sim.eaten % r.per_level;
    const dots = $("progress").children;
    if (dots.length !== r.per_level) {
      $("progress").innerHTML = "<i></i>".repeat(r.per_level);
    }
    for (let i = 0; i < r.per_level; i++) $("progress").children[i].classList.toggle("on", i < got);
    effects();
  }

  function effects() {
    const sim = play.sim, r = play.rules;
    const chips = [];
    if (sim.slowed()) chips.push(["slow", t.chipSlow, (sim.slowEnd - sim.tick) / r.slow_ticks]);
    if (sim.ghost()) chips.push(["ghost", t.chipGhost, (sim.ghostEnd - sim.tick) / r.ghost_ticks]);
    if (sim.combo > 0 && sim.tick - sim.lastMeal <= r.combo_ticks) {
      chips.push(["combo", t.combo(sim.combo + 1), 1 - (sim.tick - sim.lastMeal) / r.combo_ticks]);
    }
    const html = chips.map(([kind, label, left]) =>
      `<span class="chip ${kind}">${label}<span style="width:${Math.max(0, Math.min(1, left)) * 100}%"></span></span>`).join("");
    if ($("effects").innerHTML !== html) $("effects").innerHTML = html;
  }

  // ---------- play ----------

  let play = null;

  async function begin() {
    $("play").disabled = $("again").disabled = true;
    try {
      const dealt = await api({ action: "start", session });
      play = {
        run: dealt.run, rules: dealt.rules, sim: new IlonSim(dealt.seed, dealt.rules),
        turns: [], queue: [], acc: 0, before: [], particles: [], floaters: [],
        shake: 0, flash: 0, hold: 0, paused: false, over: false, exploded: false, ticks: 0,
      };
      play.before = play.sim.body.slice();
      show("game");
      layoutBoard();
      $("score").textContent = "0";
      hud();
      await countdown();
      play.started = true;
    } catch (error) {
      play = null;
      show("intro");
      $("introMessage").textContent = problem(error);
    } finally {
      $("play").disabled = $("again").disabled = false;
    }
  }

  function countdown() {
    const count = $("count");
    count.hidden = false;
    return new Promise((resolve) => {
      let n = 3;
      const step = () => {
        if (n === 0) {
          count.hidden = true;
          resolve();
          return;
        }
        count.textContent = n;
        count.classList.remove("tick");
        void count.offsetWidth;
        count.classList.add("tick");
        n -= 1;
        setTimeout(step, 650);
      };
      step();
    });
  }

  // tick takes the first queued turn that changes the way, and plays a
  // tick of the rules.
  function tick() {
    const sim = play.sim;
    play.before = sim.body.slice();
    while (play.queue.length) {
      const dir = play.queue.shift();
      if (sim.turn(dir)) {
        play.turns.push(sim.tick, dir);
        break;
      }
    }
    const crashed = sim.step();
    for (const event of sim.events) happened(event);
    if (crashed) {
      play.ticks = sim.tick + 1;
      play.over = true;
      setTimeout(finish, 1100);
    }
    hud();
  }

  function happened(e) {
    const sim = play.sim;
    const at = e.cell !== undefined && e.cell >= 0 ? center(e.cell) : null;
    switch (e.type) {
      case "eat": {
        const hue = planetHue(e.cell);
        burst(at[0], at[1], `hsl(${hue}, 100%, 65%)`, 18, 2.2);
        floater(at[0], at[1] - cell * 0.4, `+${e.points}`, "#ffffff", false);
        if (e.combo > 1) floater(at[0], at[1] - cell * 1.1, t.combo(e.combo), "#ffd166", true);
        buzz(12);
        break;
      }
      case "level":
        play.hold = 1700;
        play.before = sim.body.slice();
        banner(`${t.levelBanner(e.level)}<small>${t.levelBonus(e.points)}</small>`);
        buzz([20, 40, 20]);
        break;
      case "bonus":
        burst(at[0], at[1], BONUS_COLORS[e.kind], 10, 1.2);
        break;
      case "collect": {
        const color = BONUS_COLORS[e.kind];
        burst(at[0], at[1], color, 26, 2.6);
        const label = e.kind === IlonSim.STAR ? `+${e.points}` : e.kind === IlonSim.SLOW ? t.slow : t.ghost;
        floater(at[0], at[1] - cell * 0.5, label, color, true);
        buzz(25);
        break;
      }
      case "fade":
        burst(at[0], at[1], "rgba(200, 210, 255, 0.8)", 8, 0.8);
        break;
      case "crash":
        play.shake = 1;
        play.flash = 1;
        play.exploded = true;
        for (const c of sim.body) {
          const [x, y] = center(c);
          burst(x, y, "#3ef0ff", 5, 1.8);
          burst(x, y, "#ff4fd8", 3, 1.4);
        }
        buzz([60, 40, 120]);
        break;
    }
  }

  function banner(html) {
    const b = $("banner");
    b.innerHTML = html;
    b.hidden = false;
    b.style.animation = "none";
    void b.offsetWidth;
    b.style.animation = "";
    setTimeout(() => { b.hidden = true; }, 1650);
  }

  async function finish() {
    show("result");
    const sim = play.sim;
    $("final").textContent = sim.score;
    $("summary").textContent = t.summary(sim.level, sim.eaten);
    $("resultMessage").textContent = t.checking;
    $("resultTable").hidden = true;
    try {
      const result = await api({ action: "finish", run: play.run, turns: play.turns, ticks: play.ticks });
      countUp($("final"), result.score);
      $("resultMessage").textContent = result.score === 0 ? t.zero : result.improved ? t.best : t.notBest(result.score);
      fillTable("resultTable", "resultRows", result.table);
    } catch (error) {
      $("resultMessage").textContent = problem(error);
    }
  }

  function countUp(node, to) {
    if (reduced) { node.textContent = to; return; }
    const from = 0, start = performance.now(), ms = 900;
    const step = (now) => {
      const k = Math.min(1, (now - start) / ms);
      node.textContent = Math.round(from + (to - from) * (1 - Math.pow(1 - k, 3)));
      if (k < 1) requestAnimationFrame(step);
    };
    requestAnimationFrame(step);
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

  function setPaused(paused) {
    if (!play || play.over || !play.started) return;
    play.paused = paused;
    $("pause").hidden = !paused;
  }

  // ---------- input ----------

  const KEYS = { ArrowUp: 0, KeyW: 0, ArrowRight: 1, KeyD: 1, ArrowDown: 2, KeyS: 2, ArrowLeft: 3, KeyA: 3 };

  function ask(dir) {
    if (!play || play.over || play.paused) return;
    const last = play.queue.length ? play.queue[play.queue.length - 1] : play.sim.dir;
    if (dir === last || play.queue.length >= 2) return;
    play.queue.push(dir);
  }

  addEventListener("keydown", (e) => {
    if (e.code in KEYS) {
      e.preventDefault();
      ask(KEYS[e.code]);
    } else if ((e.code === "Space" || e.code === "Escape" || e.code === "KeyP") && play && !play.over) {
      e.preventDefault();
      setPaused(!play.paused);
    }
  });

  let touch = null;
  $("game").addEventListener("pointerdown", (e) => {
    if (e.target.closest("button")) return;
    touch = { x: e.clientX, y: e.clientY, id: e.pointerId };
  });
  $("game").addEventListener("pointermove", (e) => {
    if (!touch || e.pointerId !== touch.id) return;
    const dx = e.clientX - touch.x, dy = e.clientY - touch.y;
    if (Math.max(Math.abs(dx), Math.abs(dy)) < 18) return;
    ask(Math.abs(dx) > Math.abs(dy) ? (dx > 0 ? 1 : 3) : (dy > 0 ? 2 : 0));
    touch = { x: e.clientX, y: e.clientY, id: e.pointerId };
  });
  const lift = () => { touch = null; };
  $("game").addEventListener("pointerup", lift);
  $("game").addEventListener("pointercancel", lift);

  $("pauseButton").addEventListener("click", () => setPaused(true));
  $("resume").addEventListener("click", () => setPaused(false));
  document.addEventListener("visibilitychange", () => { if (document.hidden) setPaused(true); });

  $("play").addEventListener("click", begin);
  $("again").addEventListener("click", begin);
  $("share").addEventListener("click", () => {
    if (window.TelegramGameProxy) TelegramGameProxy.shareScore();
  });

  // ---------- the loop ----------

  let last = performance.now();
  function frame(now) {
    const dt = Math.min(now - last, 250);
    last = now;
    if (play && play.hold > 0) {
      play.hold -= dt;
      warp = Math.min(1, warp + dt / 300);
    } else {
      warp = Math.max(0, warp - dt / 500);
    }
    drawSky(dt, now);
    if (play && !$("game").hidden) {
      const sim = play.sim;
      // A tick is played only once its whole length has passed, so that
      // the play takes at least as long as the server counts.
      if (play.started && !play.paused && !play.over && play.hold <= 0) {
        play.acc += dt;
        let guard = 0;
        while (play.acc >= sim.tickMs() && guard++ < 4 && !play.over && play.hold <= 0) {
          play.acc -= sim.tickMs();
          tick();
        }
        if (play.hold > 0) play.acc = 0;
      }
      const alpha = play.over || play.hold > 0 ? 1 : Math.min(1, play.acc / sim.tickMs());
      render(alpha, dt, now);
      if (now % 200 < dt) effects();
    }
    requestAnimationFrame(frame);
  }

  addEventListener("resize", () => { resizeSky(); layoutBoard(); });

  async function introTable() {
    if (!session) return;
    try {
      const data = await api({ action: "table", session });
      fillTable("introTable", "introRows", data.table);
    } catch (_) {
      // The table is a nicety; the game is played without it.
    }
  }

  translate();
  resizeSky();
  if (!session) {
    $("play").disabled = true;
    $("introMessage").textContent = t.noSession;
  }
  introTable();
  requestAnimationFrame(frame);
})();
