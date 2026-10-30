// Uchar gilam. The server gives each play its seed and rules and, at the
// end, plays the taps sent again to count the score; the score shown while
// playing is the page's own count of the same rules.
"use strict";

(function () {
  const $ = (id) => document.getElementById(id);
  const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;
  const FIELD_W = 360, FIELD_H = 640, GROUND = 600;

  const texts = {
    uz: {
      lead: "Sehrli gilamda tungi Samarqand osmoni bo'ylab uching!",
      rule1: "Ekranga bosing: gilam yuqoriga ko'tariladi. Bosmasangiz, pastga tushadi.",
      rule2: "Minora va bulut orasidagi tirqishdan o'ting: har darvoza +10 × daraja.",
      rule3: "Oltin tangalarni yig'ing. Har 10 darvozada yangi daraja: tezroq va torroq.",
      rule4: "Humo pati bir marta urilishdan saqlab qoladi.",
      play: "Uchish!", level: "Daraja", paused: "PAUZA", resume: "Davom etish", shieldChip: "🪶 Humo",
      hint: "Bosing yoki Probel · P: pauza",
      over: "Gilam qulab tushdi", again: "Yana uchish", share: "Ulashish", table: "Reyting",
      best: "Yangi rekord! 🎉", notBest: (n) => `Bu safar ${n} ochko. Rekordingiz saqlanib qoldi.`,
      zero: "Bu safar ochko yo'q. Yana urinib ko'ring!", checking: "Natija tekshirilmoqda…",
      summary: (gates, coins, level) => `${gates} darvoza · ${coins} tanga · ${level}-daraja`,
      levelBanner: (n) => `${n}-daraja`, levelBonus: (p) => `+${p} bonus`,
      feather: "Humo pati!", saved: "Humo qutqardi!",
      noSession: "O'yinni Telegram'da oching: botga /gilam yozing va «Play» ni bosing.",
      session: "O'yin havolasi eskirdi. Telegram'da «Play» ni qaytadan bosing.",
      implausible: "Bu natija qabul qilinmadi.", unavailable: "Aloqa bo'lmadi. Qayta urinib ko'ring.",
    },
    ru: {
      lead: "Летите на ковре-самолёте над ночным Самаркандом!",
      rule1: "Нажимайте на экран: ковёр поднимается. Не нажимаете — опускается.",
      rule2: "Пролетайте между минаретом и тучей: каждые ворота +10 × уровень.",
      rule3: "Собирайте золотые монеты. Каждые 10 ворот — новый уровень: быстрее и уже.",
      rule4: "Перо птицы Хумо спасает от одного удара.",
      play: "Взлёт!", level: "Уровень", paused: "ПАУЗА", resume: "Продолжить", shieldChip: "🪶 Хумо",
      hint: "Нажимайте или Пробел · P: пауза",
      over: "Ковёр упал", again: "Ещё полёт", share: "Поделиться", table: "Рейтинг",
      best: "Новый рекорд! 🎉", notBest: (n) => `В этот раз ${n} очков. Рекорд остался прежним.`,
      zero: "В этот раз без очков. Попробуйте ещё!", checking: "Проверяем результат…",
      summary: (gates, coins, level) => `Ворот: ${gates} · монет: ${coins} · уровень ${level}`,
      levelBanner: (n) => `Уровень ${n}`, levelBonus: (p) => `+${p} бонус`,
      feather: "Перо Хумо!", saved: "Хумо спасла!",
      noSession: "Откройте игру в Telegram: напишите боту /gilam и нажмите «Play».",
      session: "Ссылка на игру устарела. Нажмите «Play» в Telegram ещё раз.",
      implausible: "Этот результат не принят.", unavailable: "Нет связи. Попробуйте ещё раз.",
    },
    en: {
      lead: "Fly a magic carpet over Samarkand at night!",
      rule1: "Tap the screen: the carpet rises. Stop tapping and it sinks.",
      rule2: "Fly through the gap between minaret and storm cloud: each gate +10 × level.",
      rule3: "Pick up gold coins. Every 10 gates is a new level: quicker and narrower.",
      rule4: "A feather of the Humo bird saves you from one crash.",
      play: "Fly!", level: "Level", paused: "PAUSED", resume: "Resume", shieldChip: "🪶 Humo",
      hint: "Tap or Space · P: pause",
      over: "The carpet fell", again: "Fly again", share: "Share", table: "Leaderboard",
      best: "New record! 🎉", notBest: (n) => `${n} points this time. Your record stands.`,
      zero: "No points this time. Try again!", checking: "Checking the result…",
      summary: (gates, coins, level) => `${gates} gates · ${coins} coins · level ${level}`,
      levelBanner: (n) => `Level ${n}`, levelBonus: (p) => `+${p} bonus`,
      feather: "Humo feather!", saved: "Humo saved you!",
      noSession: "Open the game in Telegram: send the bot /gilam and press «Play».",
      session: "The game's link has expired. Press «Play» in Telegram again.",
      implausible: "This result was not accepted.", unavailable: "No connection. Please try again.",
    },
  };
  const language = (navigator.language || "").toLowerCase().slice(0, 2);
  const t = texts[language] || texts.uz;
  document.documentElement.lang = texts[language] ? language : "uz";
  const session = new URLSearchParams(location.search).get("s") || "";

  // The sky of each level, from the top down to the horizon.
  const SKIES = [
    ["#140c46", "#4b2582", "#f0875f"],
    ["#050a2c", "#161c62", "#4c4fb0"],
    ["#04131f", "#0f3e52", "#2fd4c4"],
    ["#1b0822", "#5c1440", "#ff6b4a"],
    ["#0a0618", "#2b1458", "#c86bff"],
  ];

  function translate() {
    for (const node of document.querySelectorAll("[data-t]")) node.textContent = t[node.dataset.t];
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

  // ---------- the scene ----------

  const canvas = $("scene");
  const ctx = canvas.getContext("2d");
  let W = 0, H = 0, dpr = 1, scale = 1, ox = 0, oy = 0;
  let skylines = [];
  let stars = [];
  let carpetImage = null;
  let moonImage = null;

  function resize() {
    dpr = Math.min(window.devicePixelRatio || 1, 2);
    W = innerWidth;
    H = innerHeight;
    canvas.width = W * dpr;
    canvas.height = H * dpr;
    scale = Math.min(W / FIELD_W, H / FIELD_H);
    ox = (W - FIELD_W * scale) / 2;
    oy = (H - FIELD_H * scale) / 2;
    stars = Array.from({ length: 140 }, () => ({ x: Math.random(), y: Math.random() * 0.6, z: Math.random(), tw: Math.random() * 6 }));
    skylines = [makeSkyline(0.55, 210, 1), makeSkyline(0.85, 150, 2)];
    carpetImage = makeCarpet();
    moonImage = makeMoon();
  }

  // makeMoon draws a crescent, 24 field units across, with its glow.
  function makeMoon() {
    const px = Math.ceil(scale * dpr), size = 80;
    const off = document.createElement("canvas");
    off.width = off.height = size * px;
    const c = off.getContext("2d");
    c.scale(px, px);
    c.shadowColor = "rgba(255, 236, 180, 0.9)";
    c.shadowBlur = 18;
    c.fillStyle = "#fff1c8";
    c.beginPath();
    c.arc(40, 40, 16, 0, Math.PI * 2);
    c.fill();
    c.shadowBlur = 0;
    c.globalCompositeOperation = "destination-out";
    c.beginPath();
    c.arc(47, 35, 14, 0, Math.PI * 2);
    c.fill();
    return off;
  }

  // makeSkyline draws a repeating band of domes and minarets, height tall
  // in field units, into its own canvas.
  function makeSkyline(alpha, height, seed) {
    const period = 720;
    const px = Math.ceil(scale * dpr);
    const off = document.createElement("canvas");
    off.width = period * px;
    off.height = height * px;
    const c = off.getContext("2d");
    c.scale(px, px);
    let s = seed * 9301;
    const rnd = () => ((s = (s * 9301 + 49297) % 233280) / 233280);
    c.fillStyle = seed === 1 ? `rgba(22, 14, 64, ${alpha})` : `rgba(8, 6, 26, ${alpha})`;
    let x = 0;
    while (x < period) {
      const kind = rnd();
      if (kind < 0.35) {
        const w = 40 + rnd() * 50, h = 40 + rnd() * 40;
        c.fillRect(x, height - h, w, h);
        c.beginPath();
        c.ellipse(x + w / 2, height - h, w * 0.38, w * 0.42, 0, Math.PI, 0);
        c.fill();
        c.fillRect(x + w / 2 - 1, height - h - w * 0.42 - 10, 2, 10);
        x += w + 4;
      } else if (kind < 0.6) {
        const h = 90 + rnd() * (height - 110), w = 9 + rnd() * 5;
        c.fillRect(x, height - h, w, h);
        c.fillRect(x - 3, height - h + 8, w + 6, 4);
        c.beginPath();
        c.ellipse(x + w / 2, height - h, w * 0.6, w * 0.9, 0, Math.PI, 0);
        c.fill();
        x += w + 10;
      } else if (kind < 0.85) {
        const w = 60 + rnd() * 50, h = 50 + rnd() * 30;
        c.fillRect(x, height - h, w, h);
        c.globalCompositeOperation = "destination-out";
        c.beginPath();
        const aw = w * 0.36, ax = x + w / 2;
        c.moveTo(ax - aw / 2, height);
        c.lineTo(ax - aw / 2, height - h * 0.55);
        c.quadraticCurveTo(ax - aw / 2, height - h * 0.8, ax, height - h * 0.86);
        c.quadraticCurveTo(ax + aw / 2, height - h * 0.8, ax + aw / 2, height - h * 0.55);
        c.lineTo(ax + aw / 2, height);
        c.fill();
        c.globalCompositeOperation = "source-over";
        x += w + 6;
      } else {
        const w = 30 + rnd() * 40, h = 16 + rnd() * 20;
        c.fillRect(x, height - h, w, h);
        x += w;
      }
    }
    if (seed === 2) {
      for (let i = 0; i < 40; i++) {
        c.fillStyle = `rgba(255, 200, 110, ${0.4 + rnd() * 0.5})`;
        c.fillRect(rnd() * period, height - 10 - rnd() * 50, 2, 3);
      }
    }
    return { canvas: off, period, height };
  }

  // makeCarpet draws the carpet's pattern once, 60 by 14 field units.
  function makeCarpet() {
    const px = Math.ceil(scale * dpr * 2);
    const off = document.createElement("canvas");
    off.width = 60 * px;
    off.height = 14 * px;
    const c = off.getContext("2d");
    c.scale(px, px);
    c.fillStyle = "#b3122e";
    c.fillRect(0, 0, 60, 14);
    c.fillStyle = "#ffcf5a";
    c.fillRect(0, 0, 60, 2);
    c.fillRect(0, 12, 60, 2);
    c.fillStyle = "#1c2a6b";
    c.fillRect(0, 2.5, 60, 1.2);
    c.fillRect(0, 10.3, 60, 1.2);
    for (let x = 4; x < 60; x += 9) {
      c.fillStyle = (x / 9) % 2 < 1 ? "#ffcf5a" : "#2fd4c4";
      c.beginPath();
      c.moveTo(x, 7);
      c.lineTo(x + 3, 4.3);
      c.lineTo(x + 6, 7);
      c.lineTo(x + 3, 9.7);
      c.closePath();
      c.fill();
      c.fillStyle = "#b3122e";
      c.fillRect(x + 2.3, 6.3, 1.4, 1.4);
    }
    return off;
  }

  function mix(a, b, k) {
    const pa = parseInt(a.slice(1), 16), pb = parseInt(b.slice(1), 16);
    const ch = (p, s) => (p >> s) & 255;
    const m = (s) => Math.round(ch(pa, s) + (ch(pb, s) - ch(pa, s)) * k);
    return `rgb(${m(16)}, ${m(8)}, ${m(0)})`;
  }

  const view = { dist: 0, sky: 0, skyFrom: 0, skyK: 1, idleY: 300 };

  function drawSky(now) {
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    const from = SKIES[view.skyFrom % SKIES.length], to = SKIES[view.sky % SKIES.length];
    const g = ctx.createLinearGradient(0, 0, 0, H);
    g.addColorStop(0, mix(from[0], to[0], view.skyK));
    g.addColorStop(0.55, mix(from[1], to[1], view.skyK));
    g.addColorStop(0.9, mix(from[2], to[2], view.skyK));
    ctx.fillStyle = g;
    ctx.fillRect(0, 0, W, H);
    for (const s of stars) {
      const a = 0.25 + 0.6 * s.z * (0.6 + 0.4 * Math.sin(now / 600 + s.tw));
      ctx.fillStyle = `rgba(255, 250, 230, ${a})`;
      ctx.fillRect(s.x * W, s.y * H, 1 + s.z, 1 + s.z);
    }
    const moon = 80 * scale;
    ctx.drawImage(moonImage, ox + 322 * scale - moon / 2, oy + 44 * scale - moon / 2, moon, moon);
    // The city, far and near, drifting slower than the carpet flies.
    for (const [i, layer] of skylines.entries()) {
      const speed = i === 0 ? 0.12 : 0.32;
      const top = oy + (GROUND - layer.height + (i === 0 ? -20 : 4)) * scale;
      const w = layer.period * scale;
      let x = -((view.dist * speed * scale) % w) + (ox % w) - w;
      while (x < W) {
        ctx.drawImage(layer.canvas, x, top, w, layer.height * scale);
        x += w;
      }
    }
  }

  function field() {
    ctx.setTransform(dpr * scale, 0, 0, dpr * scale, dpr * ox, dpr * oy);
  }

  function drawGround() {
    const g = ctx.createLinearGradient(0, GROUND, 0, FIELD_H);
    g.addColorStop(0, "#1c1650");
    g.addColorStop(1, "#0b0a2a");
    ctx.fillStyle = g;
    const left = -ox / scale, right = (W - ox) / scale;
    ctx.fillRect(left, GROUND, right - left, FIELD_H - GROUND + oy / scale + 2);
    ctx.fillStyle = "#ffcf5a";
    ctx.fillRect(left, GROUND, right - left, 2);
    ctx.strokeStyle = "rgba(47, 212, 196, 0.55)";
    ctx.lineWidth = 1.2;
    const step = 24, shift = -(view.dist % step);
    for (let x = left - step + shift; x < right + step; x += step) {
      ctx.beginPath();
      ctx.moveTo(x, GROUND + 10);
      ctx.lineTo(x + step / 2, GROUND + 4);
      ctx.lineTo(x + step, GROUND + 10);
      ctx.lineTo(x + step / 2, GROUND + 16);
      ctx.closePath();
      ctx.stroke();
    }
  }

  function drawMinaret(x, top) {
    const bodyX = x + 7, bodyW = 46;
    const g = ctx.createLinearGradient(bodyX, 0, bodyX + bodyW, 0);
    g.addColorStop(0, "#7a4a32");
    g.addColorStop(0.35, "#d29a6a");
    g.addColorStop(1, "#6a3b27");
    ctx.fillStyle = g;
    ctx.fillRect(bodyX, top + 12, bodyW, GROUND - top - 12);
    for (let y = top + 26; y < GROUND - 6; y += 34) {
      const band = ctx.createLinearGradient(bodyX, 0, bodyX + bodyW, 0);
      band.addColorStop(0, "#0f6f78");
      band.addColorStop(0.35, "#38e0d0");
      band.addColorStop(1, "#0b5560");
      ctx.fillStyle = band;
      ctx.fillRect(bodyX, y, bodyW, 9);
      ctx.fillStyle = "#ffcf5a";
      for (let k = bodyX + 3; k < bodyX + bodyW - 3; k += 8) {
        ctx.beginPath();
        ctx.moveTo(k, y + 4.5);
        ctx.lineTo(k + 2.5, y + 2);
        ctx.lineTo(k + 5, y + 4.5);
        ctx.lineTo(k + 2.5, y + 7);
        ctx.fill();
      }
    }
    // The balcony at the top, the gap's lower edge.
    const head = ctx.createLinearGradient(x, 0, x + 60, 0);
    head.addColorStop(0, "#0f6f78");
    head.addColorStop(0.4, "#46ecdc");
    head.addColorStop(1, "#0b5560");
    ctx.fillStyle = head;
    ctx.fillRect(x, top, 60, 12);
    ctx.fillStyle = "#ffcf5a";
    ctx.fillRect(x, top, 60, 2);
    ctx.fillStyle = "rgba(0, 0, 0, 0.25)";
    for (let k = x + 4; k < x + 58; k += 7) ctx.fillRect(k, top + 5, 3, 5);
  }

  function drawCloud(x, bottom, flash) {
    const g = ctx.createLinearGradient(0, 0, 0, bottom);
    g.addColorStop(0, "#14123a");
    g.addColorStop(1, flash ? "#8b86d6" : "#3e3a72");
    ctx.fillStyle = g;
    ctx.fillRect(x + 4, -40, 52, bottom + 30);
    for (let k = 0; k < 5; k++) {
      const cx = x + 6 + k * 12, r = 10 + ((k * 7) % 5);
      ctx.beginPath();
      ctx.arc(cx, bottom - r * 0.7, r, 0, Math.PI * 2);
      ctx.fill();
    }
    ctx.beginPath();
    ctx.arc(x - 2, bottom - 22, 13, 0, Math.PI * 2);
    ctx.arc(x + 62, bottom - 26, 14, 0, Math.PI * 2);
    ctx.fill();
    ctx.fillStyle = "rgba(200, 196, 255, 0.18)";
    for (let k = 0; k < 4; k++) {
      ctx.beginPath();
      ctx.arc(x + 12 + k * 12, bottom - 14, 5, Math.PI, 0);
      ctx.fill();
    }
    if (flash) {
      ctx.strokeStyle = "#fffbe0";
      ctx.lineWidth = 2;
      ctx.shadowColor = "#fffbe0";
      ctx.shadowBlur = 12;
      ctx.beginPath();
      ctx.moveTo(x + 30, bottom - 4);
      ctx.lineTo(x + 24, bottom + 10);
      ctx.lineTo(x + 32, bottom + 12);
      ctx.lineTo(x + 26, bottom + 26);
      ctx.stroke();
      ctx.shadowBlur = 0;
    }
  }

  function drawCoin(x, y, now) {
    const spin = Math.cos(now / 180 + x / 40);
    ctx.save();
    ctx.translate(x, y);
    ctx.scale(Math.max(0.15, Math.abs(spin)), 1);
    ctx.shadowColor = "#ffcf5a";
    ctx.shadowBlur = 14;
    ctx.fillStyle = spin > 0 ? "#ffcf5a" : "#e8a317";
    ctx.beginPath();
    ctx.arc(0, 0, 9, 0, Math.PI * 2);
    ctx.fill();
    ctx.shadowBlur = 0;
    ctx.strokeStyle = "#b8730f";
    ctx.lineWidth = 1.2;
    ctx.stroke();
    ctx.fillStyle = "#6b3a06";
    ctx.fillRect(-2.5, -2.5, 5, 5);
    ctx.restore();
  }

  function drawFeather(x, y, now) {
    ctx.save();
    ctx.translate(x, y + Math.sin(now / 300) * 3);
    ctx.rotate(-0.6 + Math.sin(now / 500) * 0.15);
    ctx.shadowColor = "#ffcf5a";
    ctx.shadowBlur = 18;
    const g = ctx.createLinearGradient(0, -14, 0, 14);
    g.addColorStop(0, "#fff3c4");
    g.addColorStop(1, "#e8a317");
    ctx.fillStyle = g;
    ctx.beginPath();
    ctx.moveTo(0, -15);
    ctx.quadraticCurveTo(9, -4, 2, 13);
    ctx.lineTo(-2, 13);
    ctx.quadraticCurveTo(-9, -4, 0, -15);
    ctx.fill();
    ctx.shadowBlur = 0;
    ctx.strokeStyle = "#9a5a08";
    ctx.lineWidth = 1;
    ctx.beginPath();
    ctx.moveTo(0, -12);
    ctx.lineTo(0, 17);
    ctx.stroke();
    ctx.restore();
  }

  // drawCarpet draws the carpet and its rider at y, tilted by tilt, its
  // edge waving.
  function drawCarpet(y, tilt, now, glow, faint) {
    ctx.save();
    ctx.translate(96, y);
    ctx.rotate(tilt);
    if (faint) ctx.globalAlpha = 0.45 + 0.25 * Math.sin(now / 60);
    if (glow) {
      ctx.save();
      ctx.shadowColor = "#ffcf5a";
      ctx.shadowBlur = 24;
      ctx.strokeStyle = "rgba(255, 207, 90, 0.75)";
      ctx.lineWidth = 2;
      ctx.beginPath();
      ctx.ellipse(0, -6, 42, 26, 0, 0, Math.PI * 2);
      ctx.stroke();
      ctx.restore();
    }
    // The rider, sitting cross-legged in a striped chopon.
    ctx.fillStyle = "#1d2c7a";
    ctx.beginPath();
    ctx.moveTo(-6, -4);
    ctx.lineTo(6, -4);
    ctx.lineTo(4, -17);
    ctx.lineTo(-4, -17);
    ctx.closePath();
    ctx.fill();
    ctx.strokeStyle = "#ffcf5a";
    ctx.lineWidth = 1;
    ctx.beginPath();
    ctx.moveTo(-2, -5);
    ctx.lineTo(-1.5, -16);
    ctx.moveTo(2, -5);
    ctx.lineTo(1.5, -16);
    ctx.stroke();
    ctx.fillStyle = "#1d2c7a";
    ctx.fillRect(-10, -6, 20, 3);
    ctx.fillStyle = "#e9b48a";
    ctx.beginPath();
    ctx.arc(0, -21, 4.2, 0, Math.PI * 2);
    ctx.fill();
    ctx.fillStyle = "#f6f1e6";
    ctx.beginPath();
    ctx.ellipse(0, -25, 5.2, 3.2, 0, Math.PI, 0);
    ctx.fill();
    ctx.fillStyle = "#2fd4c4";
    ctx.fillRect(-5, -25.2, 10, 1.4);
    const scarf = Math.sin(now / 90) * 2;
    ctx.strokeStyle = "#d6264a";
    ctx.lineWidth = 1.6;
    ctx.beginPath();
    ctx.moveTo(-3, -18);
    ctx.quadraticCurveTo(-10, -18 + scarf, -15, -15 - scarf);
    ctx.stroke();
    // The carpet, slice by slice, waving more towards its back.
    const slices = 15, sw = 60 / slices;
    for (let i = 0; i < slices; i++) {
      const back = 1 - i / (slices - 1);
      const dy = Math.sin(now / 110 - i * 0.55) * (0.6 + 2.2 * back);
      ctx.drawImage(carpetImage, (i * sw * carpetImage.width) / 60, 0, (sw * carpetImage.width) / 60 + 1, carpetImage.height,
        -30 + i * sw, -4 + dy, sw + 0.3, 14);
    }
    ctx.strokeStyle = "#ffcf5a";
    ctx.lineWidth = 1;
    for (const [end, phase] of [[-30, 0], [30, 2]]) {
      const dy = Math.sin(now / 110 - phase) * 2;
      for (let k = 0; k < 4; k++) {
        const ty = -2 + k * 3.4 + dy;
        ctx.beginPath();
        ctx.moveTo(end, ty);
        ctx.lineTo(end + (end < 0 ? -4 : 4), ty + Math.sin(now / 80 + k) * 1.2);
        ctx.stroke();
      }
    }
    ctx.restore();
  }

  function burst(x, y, color, count, speed) {
    if (!play) return;
    if (reduced) count = Math.ceil(count / 3);
    for (let i = 0; i < count; i++) {
      const a = Math.random() * Math.PI * 2, v = (0.3 + Math.random()) * speed;
      play.particles.push({ x, y, vx: Math.cos(a) * v, vy: Math.sin(a) * v, life: 1, color, size: 1.2 + Math.random() * 2 });
    }
  }

  function floater(x, y, text, color, big) {
    play.floaters.push({ x, y, text, color, life: 1, big });
  }

  function drawEffects(dt) {
    for (const p of play.particles) {
      p.x += p.vx * dt * 0.06;
      p.y += p.vy * dt * 0.06;
      p.vx *= 0.96;
      p.vy = p.vy * 0.96 + 0.01 * dt;
      p.life -= dt / 800;
    }
    play.particles = play.particles.filter((p) => p.life > 0);
    ctx.save();
    for (const p of play.particles) {
      ctx.globalAlpha = Math.max(p.life, 0);
      ctx.fillStyle = p.color;
      ctx.beginPath();
      ctx.arc(p.x, p.y, p.size * p.life + 0.4, 0, Math.PI * 2);
      ctx.fill();
    }
    ctx.restore();
    for (const f of play.floaters) {
      f.y -= dt * 0.035;
      f.life -= dt / 1000;
    }
    play.floaters = play.floaters.filter((f) => f.life > 0);
    ctx.save();
    ctx.textAlign = "center";
    for (const f of play.floaters) {
      ctx.globalAlpha = Math.min(1, f.life * 2);
      ctx.font = `700 ${f.big ? 22 : 15}px Philosopher, Georgia, serif`;
      ctx.lineWidth = 4;
      ctx.strokeStyle = "rgba(30, 10, 0, 0.75)";
      ctx.strokeText(f.text, f.x, f.y);
      ctx.fillStyle = f.color;
      ctx.fillText(f.text, f.x, f.y);
    }
    ctx.restore();
  }

  function render(alpha, dt, now) {
    drawSky(now);
    field();
    if (play && play.shake > 0) {
      play.shake = Math.max(0, play.shake - dt / 450);
      const m = play.shake * 8;
      ctx.translate((Math.random() - 0.5) * m, (Math.random() - 0.5) * m);
    }
    const sim = play && play.sim;
    if (sim) {
      const u = play.rules.unit;
      ctx.save();
      ctx.beginPath();
      ctx.rect(-ox / scale, -oy / scale, W / scale, H / scale);
      ctx.clip();
      for (const c of sim.columns) {
        const x = (c.px + (c.x - c.px) * alpha) / u;
        if (x > FIELD_W + 60 || x < -80) continue;
        if (c.flash === undefined || c.flash < now - 150) c.flash = Math.random() < 0.003 ? now : c.flash || 0;
        drawCloud(x, c.top / u, now - c.flash < 120);
        drawMinaret(x, (c.top + c.gap) / u);
        const ix = x + play.rules.column_w / u / 2, iy = c.itemY / u;
        if (c.item === GilamSim.COIN) drawCoin(ix, iy, now);
        else if (c.item === GilamSim.FEATHER) drawFeather(ix, iy, now);
      }
      ctx.restore();
    }
    drawGround();
    if (sim) {
      const u = play.rules.unit;
      let y = (sim.py + (sim.y - sim.py) * alpha) / u;
      let tilt = Math.max(-0.45, Math.min(0.55, sim.vy / u * 0.06));
      if (play.over) {
        play.fall += dt * 0.012;
        y = Math.min(GROUND - 6, y + play.fall * play.fall);
        tilt = Math.min(1.2, play.fall * 0.25);
      } else if (!reduced && Math.random() < 0.6) {
        play.particles.push({ x: 64, y: y + 4 + (Math.random() - 0.5) * 8, vx: -1.2 - Math.random(), vy: 0, life: 0.6, color: "rgba(255, 220, 140, 0.9)", size: 1.2 });
      }
      drawEffects(dt);
      drawCarpet(y, tilt, now, sim.shield, sim.ghost());
      if (play.flash > 0) {
        play.flash = Math.max(0, play.flash - dt / 500);
        ctx.fillStyle = play.flashColor.replace("A", play.flash * 0.5);
        ctx.fillRect(-ox / scale, -oy / scale, W / scale, H / scale);
      }
    } else {
      view.idleY = 300 + Math.sin(now / 700) * 14;
      drawCarpet(view.idleY, Math.sin(now / 700 + 1) * 0.06, now, false, false);
    }
  }

  // ---------- play ----------

  let play = null;

  function hud() {
    const sim = play.sim;
    const score = $("score");
    if (score.textContent !== String(sim.score)) {
      score.textContent = sim.score;
      score.classList.remove("pop");
      void score.offsetWidth;
      score.classList.add("pop");
    }
    $("level").textContent = sim.level;
    $("coins").textContent = sim.coins;
    $("shield").hidden = !sim.shield;
  }

  async function begin() {
    $("play").disabled = $("again").disabled = true;
    try {
      const dealt = await api({ action: "start", session });
      play = {
        run: dealt.run, rules: dealt.rules, sim: new GilamSim(dealt.seed, dealt.rules),
        taps: [], tapped: false, acc: 0, particles: [], floaters: [], shake: 0, flash: 0, flashColor: "",
        paused: false, over: false, started: false, fall: 0, ticks: 0,
      };
      view.skyFrom = view.sky = 0;
      view.skyK = 1;
      show("game");
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

  function tick() {
    const sim = play.sim;
    if (play.tapped) {
      play.tapped = false;
      play.taps.push(sim.tick);
      sim.tap();
    }
    const crashed = sim.step();
    view.dist += sim.pace() / play.rules.unit;
    for (const event of sim.events) happened(event);
    if (crashed) {
      play.ticks = sim.tick + 1;
      play.over = true;
      setTimeout(finish, 1300);
    }
    hud();
  }

  function happened(e) {
    const u = play.rules.unit, sim = play.sim;
    switch (e.type) {
      case "pass":
        floater(96, sim.y / u - 30, `+${e.points}`, "#fff6e5", false);
        break;
      case "coin":
        burst(e.x / u, e.y / u, "#ffcf5a", 16, 2);
        floater(e.x / u, e.y / u - 14, `+${e.points}`, "#ffcf5a", false);
        buzz(10);
        break;
      case "feather":
        burst(e.x / u, e.y / u, "#fff3c4", 22, 2.4);
        floater(e.x / u, e.y / u - 18, t.feather, "#ffcf5a", true);
        buzz(25);
        break;
      case "saved":
        play.flash = 1;
        play.flashColor = "rgba(255, 207, 90, A)";
        burst(96, sim.y / u, "#ffcf5a", 30, 3);
        floater(96, sim.y / u - 36, t.saved, "#ffcf5a", true);
        buzz([30, 30, 30]);
        break;
      case "level":
        view.skyFrom = view.sky;
        view.sky = e.level - 1;
        view.skyK = 0;
        banner(`${t.levelBanner(e.level)}<small>${t.levelBonus(e.points)}</small>`);
        buzz([20, 40, 20]);
        break;
      case "crash":
        play.shake = 1;
        play.flash = 1;
        play.flashColor = "rgba(255, 70, 90, A)";
        burst(96, sim.y / u, "#d6264a", 26, 2.6);
        burst(96, sim.y / u, "#ffcf5a", 14, 2);
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
    setTimeout(() => { b.hidden = true; }, 1600);
  }

  async function finish() {
    const sim = play.sim;
    show("result");
    $("final").textContent = sim.score;
    $("summary").textContent = t.summary(sim.passed, sim.coins, sim.level);
    $("resultMessage").textContent = t.checking;
    $("resultTable").hidden = true;
    const sent = { run: play.run, taps: play.taps, ticks: play.ticks };
    play = null;
    try {
      const result = await api({ action: "finish", ...sent });
      countUp($("final"), result.score);
      $("resultMessage").textContent = result.score === 0 ? t.zero : result.improved ? t.best : t.notBest(result.score);
      fillTable("resultTable", "resultRows", result.table);
    } catch (error) {
      $("resultMessage").textContent = problem(error);
    }
  }

  function countUp(node, to) {
    if (reduced) { node.textContent = to; return; }
    const start = performance.now(), ms = 900;
    const step = (now) => {
      const k = Math.min(1, (now - start) / ms);
      node.textContent = Math.round(to * (1 - Math.pow(1 - k, 3)));
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

  function tap() {
    if (play && play.started && !play.paused && !play.over) play.tapped = true;
  }

  $("game").addEventListener("pointerdown", (e) => {
    if (e.target.closest("button")) return;
    e.preventDefault();
    tap();
  });
  addEventListener("keydown", (e) => {
    if (e.repeat) return;
    if (["Space", "ArrowUp", "KeyW", "Enter"].includes(e.code) && !$("game").hidden) {
      e.preventDefault();
      tap();
    } else if ((e.code === "KeyP" || e.code === "Escape") && play && !play.over) {
      e.preventDefault();
      setPaused(!play.paused);
    }
  });
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
    view.skyK = Math.min(1, view.skyK + dt / 2000);
    let alpha = 1;
    if (play) {
      // A tick is played only once its whole length has passed, so that
      // the play takes at least as long as the server counts.
      const ms = play.rules.tick_ms;
      if (play.started && !play.paused && !play.over) {
        play.acc += dt;
        let guard = 0;
        while (play.acc >= ms && guard++ < 8 && !play.over) {
          play.acc -= ms;
          tick();
        }
      }
      alpha = play.over ? 1 : Math.min(1, play.acc / ms);
    } else {
      view.dist += dt * 0.05;
    }
    render(alpha, dt, now);
    requestAnimationFrame(frame);
  }

  addEventListener("resize", resize);

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
  resize();
  if (!session) {
    $("play").disabled = true;
    $("introMessage").textContent = t.noSession;
  }
  introTable();
  requestAnimationFrame(frame);
})();
