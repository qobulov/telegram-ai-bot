// Dev jangi. The server keeps each chat's boss and each player's hero, and
// counts what taps do; the page shows each blow at once, with the damage
// the server will count (crit.js), and sends the taps in batches.
"use strict";

(function () {
  const $ = (id) => document.getElementById(id);
  const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;

  const texts = {
    uz: {
      names: ["Ko'k dev", "Qizil dev", "Bir ko'zli dev", "Ajdar", "Qora dev"],
      hint: "Devni bosib uring! Guruhdagilar ham siz bilan uradi",
      table: "Zarbalar", sword: "Qilich", crit: "Krit", helper: "Barchinoy",
      lvl: (n) => `${n}-daraja`, hired: "yollanmagan",
      swordNext: (a, b) => `zarba ${a} → ${b}`, critNext: (a, b) => `${a}% → ${b}%`, helperNext: (a, b) => `${a}/s → ${b}/s`,
      max: "Eng yuqori", killed: (name) => `${name} yengildi!`, reward: (n) => `+${n} 💰 sizga`,
      next: "Keyingi dev", share: "Ulashish", others: (name) => `${name} guruh bilan yengildi!`,
      poor: "Tanga yetmaydi", upgraded: "Kuchaydi!", rage: "Dev g'azabda!",
      noSession: "O'yinni Telegram'da oching: botga /boss yozing va «Play» ni bosing.",
      session: "O'yin havolasi eskirdi. Telegram'da «Play» ni qaytadan bosing.",
      unavailable: "Aloqa uzildi, qayta urinyapmiz…",
    },
    ru: {
      names: ["Синий дэв", "Красный дэв", "Одноглазый дэв", "Аждар", "Чёрный дэв"],
      hint: "Бейте дэва! Группа бьёт вместе с вами",
      table: "Удары", sword: "Меч", crit: "Крит", helper: "Барчиной",
      lvl: (n) => `ур. ${n}`, hired: "не нанята",
      swordNext: (a, b) => `удар ${a} → ${b}`, critNext: (a, b) => `${a}% → ${b}%`, helperNext: (a, b) => `${a}/с → ${b}/с`,
      max: "Максимум", killed: (name) => `${name} повержен!`, reward: (n) => `+${n} 💰 вам`,
      next: "Следующий дэв", share: "Поделиться", others: (name) => `${name} повержен группой!`,
      poor: "Не хватает монет", upgraded: "Сильнее!", rage: "Дэв в ярости!",
      noSession: "Откройте игру в Telegram: напишите боту /boss и нажмите «Play».",
      session: "Ссылка на игру устарела. Нажмите «Play» в Telegram ещё раз.",
      unavailable: "Связь прервалась, пробуем снова…",
    },
    en: {
      names: ["Blue giant", "Red giant", "One-eyed giant", "Ajdar the dragon", "Black giant"],
      hint: "Tap the giant! Your group hits it with you",
      table: "Damage", sword: "Sword", crit: "Crit", helper: "Barchinoy",
      lvl: (n) => `level ${n}`, hired: "not hired",
      swordNext: (a, b) => `blow ${a} → ${b}`, critNext: (a, b) => `${a}% → ${b}%`, helperNext: (a, b) => `${a}/s → ${b}/s`,
      max: "Maxed", killed: (name) => `${name} defeated!`, reward: (n) => `+${n} 💰 for you`,
      next: "Next giant", share: "Share", others: (name) => `${name} defeated by the group!`,
      poor: "Not enough coins", upgraded: "Stronger!", rage: "The giant is enraged!",
      noSession: "Open the game in Telegram: send the bot /boss and press «Play».",
      session: "The game's link has expired. Press «Play» in Telegram again.",
      unavailable: "Connection lost, retrying…",
    },
  };
  const language = (navigator.language || "").toLowerCase().slice(0, 2);
  const t = texts[language] || texts.uz;
  document.documentElement.lang = texts[language] ? language : "uz";
  for (const node of document.querySelectorAll("[data-t]")) node.textContent = t[node.dataset.t];

  const session = new URLSearchParams(location.search).get("s") || "";
  const NAME_COLORS = ["#7fd6ff", "#ff9be0", "#b4ff8a", "#ffb36b", "#c9a6ff", "#7fffd4", "#ffd27f"];
  // The bosses: body colour, the sky behind them, and what sets each apart.
  const KINDS = [
    { body: "#3b6cff", dark: "#2142b8", sky: ["#1b1446", "#3d2a6e", "#e58a5f"], horns: 2 },
    { body: "#e0384d", dark: "#9c1a2c", sky: ["#2a0c1c", "#5c1a2c", "#ff9a5a"], horns: 4, beard: true },
    { body: "#8f5ad6", dark: "#5e3399", sky: ["#0d1a2c", "#20385a", "#6fc7c0"], horns: 1, cyclops: true },
    { body: "#3fae58", dark: "#21753a", sky: ["#0b1e14", "#1e4a30", "#e6c35a"], horns: 0, dragon: true },
    { body: "#2a2638", dark: "#121018", sky: ["#09060f", "#2a1236", "#c0392b"], horns: 2, crown: true },
  ];

  const fmt = (n) => n >= 1e9 ? (n / 1e9).toFixed(2) + "B" : n >= 1e6 ? (n / 1e6).toFixed(2) + "M" : n >= 1e4 ? Math.floor(n / 1e3) + "k" : n >= 1e3 ? (n / 1e3).toFixed(1) + "k" : String(Math.round(n));
  const colorOf = (id) => NAME_COLORS[Math.abs(Number(id)) % NAME_COLORS.length];

  // ---------- server ----------

  async function api(body) {
    const response = await fetch("/api/raid", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ session, ...body }),
    });
    const data = await response.json().catch(() => ({ error: "unavailable" }));
    if (!response.ok || data.error) throw new Error(data.error || "unavailable");
    return data;
  }

  let boss = null, hero = null, top = [], tapsPerSecond = 12, critTimes = 3;
  let pending = 0, pendingDamage = 0, inflight = 0, inflightDamage = 0, sending = false, lastSend = 0, polled = 0;
  const seen = new Map();
  const tapTimes = [];
  let shownLevel = 0, entrance = 1, victoryOpen = false, myDamage = 0;

  function message(text) { $("message").textContent = text || ""; }

  function apply(data) {
    if (data.boss) {
      if (shownLevel && data.boss.level > shownLevel && !data.killed) killedByOthers(data);
      boss = data.boss;
      shownLevel = boss.level;
    }
    if (data.me) myDamage = data.me.damage;
    if (data.hero) hero = data.hero;
    if (data.top) {
      for (const row of data.top) {
        const before = seen.get(row.id);
        if (!row.me && before !== undefined && row.damage > before) othersHit(row, row.damage - before);
        seen.set(row.id, row.damage);
      }
      top = data.top;
    }
    if (data.taps_per_second) tapsPerSecond = data.taps_per_second;
    if (data.crit_times) critTimes = data.crit_times;
    hud();
  }

  async function poll() {
    if (sending || victoryOpen || document.hidden) return;
    polled = performance.now();
    try {
      apply(await api({ action: "state" }));
    } catch (error) {
      failed(error);
    }
  }

  async function flush() {
    if (sending || !hero || !boss) return;
    const now = performance.now();
    const helperDue = hero.helper > 0 && now - lastSend > 2000 && !document.hidden;
    if (pending === 0 && !helperDue) return;
    sending = true;
    lastSend = now;
    inflight = pending;
    inflightDamage = pendingDamage;
    pending = 0;
    pendingDamage = 0;
    try {
      const data = await api({ action: "hit", taps: inflight });
      const arrows = data.damage - inflightDamage;
      if (hero.helper > 0 && arrows > 0) arrowsHit(arrows);
      inflight = 0;
      inflightDamage = 0;
      apply(data);
      message("");
      if (data.killed) victory(data.killed);
    } catch (error) {
      pending += inflight;
      pendingDamage += inflightDamage;
      inflight = 0;
      inflightDamage = 0;
      failed(error);
    } finally {
      sending = false;
    }
  }

  function failed(error) {
    message(error.message === "session" ? t.session : t.unavailable);
  }

  async function upgrade(item, card) {
    if (!hero || hero.max[item]) return;
    if (hero.coins + pending + inflight < hero.costs[item]) {
      card.classList.remove("shake");
      void card.offsetWidth;
      card.classList.add("shake");
      message(t.poor);
      setTimeout(() => message(""), 1500);
      return;
    }
    await flush();
    try {
      apply(await api({ action: "upgrade", item }));
      const r = card.getBoundingClientRect();
      textPop(r.left + r.width / 2, r.top - 10, t.upgraded, "#ffcf4a", 22);
      burst(r.left + r.width / 2, r.top, "#ffcf4a", 24, 5);
      if (navigator.vibrate) navigator.vibrate(20);
    } catch (error) {
      if (error.message === "coins") message(t.poor);
      else failed(error);
    }
  }

  // ---------- tapping ----------

  function tap(x, y) {
    if (!hero || !boss || victoryOpen || entrance > 0.4) return;
    const now = performance.now();
    while (tapTimes.length && now - tapTimes[0] > 1000) tapTimes.shift();
    if (tapTimes.length >= tapsPerSecond) return; // the server would not count it
    tapTimes.push(now);
    const index = hero.taps + inflight + pending;
    const crit = critical(hero.seed, index, hero.crit_chance);
    const damage = hero.tap_damage * (crit ? critTimes : 1);
    pending += 1;
    pendingDamage += damage;
    $("hint").hidden = true;
    damageNumber(x, y - 10, damage, crit);
    burst(x, y, crit ? "#ffcf4a" : "#ffffff", crit ? 22 : 10, crit ? 6 : 4);
    if (crit) slash(x, y);
    fx.flash = 90;
    fx.squash = 1;
    fx.shake = Math.max(fx.shake, crit ? 0.8 : 0.3);
    fx.swing = 1;
    if (navigator.vibrate) navigator.vibrate(crit ? 25 : 8);
    hud();
  }

  // ---------- what others and Barchinoy do ----------

  function othersHit(row, damage) {
    const parts = Math.min(4, Math.max(1, Math.round(damage / Math.max(hero ? hero.tap_damage * 3 : 50, 1))));
    for (let i = 0; i < parts; i++) {
      setTimeout(() => {
        const r = bossRadius();
        const a = Math.random() * Math.PI * 2;
        const x = bossX() + Math.cos(a) * r * 0.6, y = bossY() + Math.sin(a) * r * 0.5;
        nums.push({ x, y, vy: -1.4, vx: 0, text: `${row.name} −${fmt(damage / parts)}`, color: colorOf(row.id), size: 14, life: 1 });
        burst(x, y, colorOf(row.id), 6, 3);
        fx.flash = Math.max(fx.flash, 40);
      }, (i * 2200) / parts + Math.random() * 300);
    }
  }

  function arrowsHit(damage) {
    const from = archerAt();
    arrows.push({ x: from[0], y: from[1], tx: bossX() + (Math.random() - 0.5) * 60, ty: bossY() + (Math.random() - 0.5) * 60, k: 0, damage });
  }

  // ---------- victory ----------

  function victory(kill) {
    victoryOpen = true;
    fx.shake = 1.5;
    for (let i = 0; i < 4; i++) setTimeout(() => burst(bossX(), bossY(), ["#ffcf4a", "#ff4d5e", "#ffffff", KINDS[kill.kind].body][i], 60, 10), i * 140);
    if (navigator.vibrate) navigator.vibrate([60, 40, 60, 40, 160]);
    $("victoryTitle").textContent = t.killed(t.names[kill.kind]);
    $("victoryReward").textContent = t.reward(fmt(kill.reward));
    const list = $("victoryList");
    list.textContent = "";
    kill.fighters.forEach((f, i) => {
      const li = document.createElement("li");
      if (f.me) li.className = "me";
      const reward = f.reward === null ? "" : `+${fmt(f.reward)} 💰`;
      for (const [cls, text] of [["pos", ["🥇", "🥈", "🥉"][i] || `${i + 1}.`], ["name", f.name], ["dmg", fmt(f.damage)], ["reward", reward]]) {
        const span = document.createElement("span");
        span.className = cls;
        span.textContent = text;
        li.append(span);
      }
      list.append(li);
    });
    setTimeout(() => { $("victory").hidden = false; }, 700);
  }

  // killedByOthers shows the boss another member killed: to one who hit
  // it, as a victory with the coins it brought them and who hit it most as
  // last seen; to anyone else, as a word.
  function killedByOthers(data) {
    const level = data.boss.level - 1, kind = (level - 1) % 5;
    if (myDamage > 0 && hero && data.hero) {
      const fighters = top.map((row) => ({ name: row.name, damage: row.me ? Math.max(row.damage, myDamage) : row.damage, reward: null, me: row.me }));
      victory({ level, kind, reward: Math.max(0, data.hero.coins - hero.coins), fighters });
      return;
    }
    message(t.others(t.names[kind]));
    setTimeout(() => message(""), 3000);
    newBoss();
  }

  function newBoss() {
    entrance = 1;
    seen.clear();
  }

  $("next").addEventListener("click", () => {
    $("victory").hidden = true;
    victoryOpen = false;
    newBoss();
    poll();
  });
  $("share").addEventListener("click", () => {
    if (window.TelegramGameProxy) TelegramGameProxy.shareScore();
  });

  // ---------- HUD ----------

  function hud() {
    if (!boss || !hero) return;
    const kind = boss.kind;
    $("level").textContent = boss.level;
    $("bossName").textContent = t.names[kind];
    const hp = Math.max(0, boss.hp - inflightDamage - pendingDamage);
    const share = Math.max(0, Math.min(1, hp / boss.max_hp));
    $("hpFill").style.width = `${share * 100}%`;
    $("hpTrail").style.width = `${share * 100}%`;
    $("hpText").textContent = `${fmt(hp)} / ${fmt(boss.max_hp)}`;
    const coins = `💰 ${fmt(hero.coins + pending + inflight)}`;
    if ($("coins").textContent !== coins) {
      $("coins").textContent = coins;
      $("coins").classList.remove("pop");
      void $("coins").offsetWidth;
      $("coins").classList.add("pop");
    }
    const have = hero.coins + pending + inflight;
    const card = (item, level, next) => {
      const el = document.querySelector(`.card[data-item="${item}"]`);
      $(`${item}Lvl`).textContent = item === "helper" && hero.helper === 0 ? t.hired : t.lvl(level);
      $(`${item}Next`).textContent = next;
      $(`${item}Cost`).textContent = hero.max[item] ? t.max : `${fmt(hero.costs[item])} 💰`;
      el.classList.toggle("max", hero.max[item]);
      el.classList.toggle("poor", !hero.max[item] && have < hero.costs[item]);
      el.classList.toggle("ready", !hero.max[item] && have >= hero.costs[item]);
    };
    card("sword", hero.sword, t.swordNext(fmt(hero.tap_damage), fmt(hero.next.tap_damage)));
    card("crit", hero.crit, t.critNext(hero.crit_chance, hero.next.crit_chance));
    card("helper", hero.helper, t.helperNext(fmt(hero.helper_dps), fmt(hero.next.helper_dps)));
    const board = $("board");
    board.textContent = "";
    top.forEach((row, i) => {
      const li = document.createElement("li");
      if (row.me) li.className = "me";
      for (const [cls, text] of [["pos", i + 1], ["name", row.name || "?"], ["dmg", fmt(row.damage)]]) {
        const span = document.createElement("span");
        span.className = cls;
        span.textContent = text;
        li.append(span);
      }
      board.append(li);
    });
  }

  $("boardToggle").addEventListener("click", () => { $("board").hidden = !$("board").hidden; });
  for (const card of document.querySelectorAll(".card")) card.addEventListener("click", () => upgrade(card.dataset.item, card));

  // ---------- the scene ----------

  const canvas = $("scene");
  const ctx = canvas.getContext("2d");
  let W = 0, H = 0, dpr = 1;
  let nums = [], parts = [], arrows = [], slashes = [];
  const fx = { flash: 0, squash: 0, shake: 0, swing: 0 };
  let peaks = [];

  function resize() {
    dpr = Math.min(window.devicePixelRatio || 1, 2);
    W = innerWidth;
    H = innerHeight;
    canvas.width = W * dpr;
    canvas.height = H * dpr;
    let s = 3;
    const rnd = () => ((s = (s * 9301 + 49297) % 233280) / 233280);
    peaks = [0, 1].map((layer) => Array.from({ length: 9 }, (_, i) => ({ x: i / 8, h: 0.12 + rnd() * (layer ? 0.1 : 0.2) })));
  }

  const cardsTop = () => document.querySelector(".upgrades").getBoundingClientRect().top;
  const groundY = () => cardsTop() - 18;
  const bossRadius = () => Math.max(40, Math.min(W * 0.36, (groundY() - 150) * 0.42, 170));
  const bossX = () => W / 2;
  const bossY = () => 130 + (groundY() - 130) * 0.48;
  const archerAt = () => [W * 0.84, groundY() - 30];

  function damageNumber(x, y, value, crit) {
    nums.push({
      x: x + (Math.random() - 0.5) * 30, y, vy: -1.8 - Math.random(), vx: (Math.random() - 0.5) * 1.4,
      text: `−${fmt(value)}${crit ? "!" : ""}`, color: crit ? "#ffcf4a" : "#ffffff", size: crit ? 34 : 24, life: 1, crit,
    });
  }

  function textPop(x, y, text, color, size) {
    nums.push({ x, y, vy: -1, vx: 0, text, color, size, life: 1.2 });
  }

  function burst(x, y, color, n, speed) {
    if (reduced) n = Math.ceil(n / 3);
    for (let i = 0; i < n; i++) {
      const a = Math.random() * Math.PI * 2, v = (0.5 + Math.random()) * speed;
      parts.push({ x, y, vx: Math.cos(a) * v, vy: Math.sin(a) * v - 1, life: 1, color, s: 1.5 + Math.random() * 3 });
    }
  }

  function slash(x, y) {
    slashes.push({ x, y, a: Math.random() * Math.PI, life: 1 });
  }

  function drawSky(kind, now) {
    const k = KINDS[kind];
    const g = ctx.createLinearGradient(0, 0, 0, H);
    g.addColorStop(0, k.sky[0]);
    g.addColorStop(0.6, k.sky[1]);
    g.addColorStop(1, k.sky[2]);
    ctx.fillStyle = g;
    ctx.fillRect(-40, -40, W + 80, H + 80);
    const gy = groundY();
    peaks.forEach((layer, li) => {
      ctx.fillStyle = li ? "rgba(10, 6, 20, 0.85)" : "rgba(30, 18, 50, 0.6)";
      ctx.beginPath();
      ctx.moveTo(-20, gy);
      for (const p of layer) ctx.lineTo(p.x * (W + 40) - 20, gy - p.h * H * (li ? 0.8 : 1.2));
      ctx.lineTo(W + 20, gy);
      ctx.fill();
    });
    ctx.fillStyle = "#120a1e";
    ctx.fillRect(-40, gy, W + 80, H - gy + 40);
    ctx.fillStyle = "rgba(255, 207, 74, 0.25)";
    ctx.fillRect(-40, gy, W + 80, 2);
    for (let i = 0; i < 30; i++) {
      const x = (i * 97.3 + now * 0.004 * ((i % 3) + 1)) % (W + 20) - 10;
      const y = gy - ((i * 53.7) % (gy - 120)) - 10;
      ctx.fillStyle = `rgba(255, 220, 160, ${0.15 + 0.15 * Math.sin(now / 500 + i)})`;
      ctx.fillRect(x, y, 2, 2);
    }
  }

  // drawBoss draws the boss of kind at (x, y), s its size, white when it is
  // hit, red-eyed and steaming when nearly dead.
  function drawBoss(kind, x, y, s, now, flash, rage) {
    const k = KINDS[kind];
    const breathe = Math.sin(now / 420) * 3;
    ctx.save();
    ctx.translate(x, y);
    ctx.scale(s, s);
    ctx.fillStyle = "rgba(0, 0, 0, 0.4)";
    ctx.beginPath();
    ctx.ellipse(0, 96, 88, 14, 0, 0, Math.PI * 2);
    ctx.fill();
    const body = flash ? "#ffffff" : k.body, dark = flash ? "#ffffff" : k.dark;
    if (k.dragon) {
      const flap = Math.sin(now / 260) * 0.25;
      for (const side of [-1, 1]) {
        ctx.save();
        ctx.scale(side, 1);
        ctx.rotate(-flap);
        ctx.fillStyle = flash ? "#ffffff" : "#1d5c2e";
        ctx.beginPath();
        ctx.moveTo(40, -10);
        ctx.quadraticCurveTo(120, -110, 170, -60);
        ctx.lineTo(140, -40);
        ctx.lineTo(150, -10);
        ctx.lineTo(118, 0);
        ctx.lineTo(120, 26);
        ctx.quadraticCurveTo(80, 10, 40, 20);
        ctx.fill();
        ctx.restore();
      }
    }
    ctx.fillStyle = dark;
    ctx.beginPath();
    ctx.ellipse(-36, 72, 22, 28, 0, 0, Math.PI * 2);
    ctx.ellipse(36, 72, 22, 28, 0, 0, Math.PI * 2);
    ctx.fill();
    const g = ctx.createRadialGradient(-20, -20 + breathe, 10, 0, 10 + breathe, 90);
    g.addColorStop(0, flash ? "#ffffff" : lighten(k.body));
    g.addColorStop(1, body);
    ctx.fillStyle = g;
    ctx.beginPath();
    ctx.ellipse(0, 10 + breathe, 72, 76, 0, 0, Math.PI * 2);
    ctx.fill();
    ctx.fillStyle = body;
    ctx.beginPath();
    ctx.ellipse(-78, 22 + breathe, 19, 42, -0.3, 0, Math.PI * 2);
    ctx.ellipse(78, 22 + breathe, 19, 42, 0.3, 0, Math.PI * 2);
    ctx.fill();
    if (k.dragon) {
      ctx.fillStyle = flash ? "#ffffff" : "#e6c35a";
      for (let i = -2; i <= 2; i++) {
        ctx.beginPath();
        ctx.moveTo(i * 18 - 8, -60 + breathe + Math.abs(i) * 6);
        ctx.lineTo(i * 18, -84 + breathe + Math.abs(i) * 6);
        ctx.lineTo(i * 18 + 8, -60 + breathe + Math.abs(i) * 6);
        ctx.fill();
      }
    }
    ctx.fillStyle = flash ? "#ffffff" : "#f3e2c0";
    const horns = k.horns;
    const hornAt = [[], [0], [-1, 1], [], [-1.4, -0.6, 0.6, 1.4]][horns];
    for (const h of hornAt) {
      const hx = h * 30;
      ctx.beginPath();
      ctx.moveTo(hx - 12, -54 + breathe);
      ctx.lineTo(hx * 1.4 + (h === 0 ? 0 : Math.sign(h) * 10), -108 + breathe + Math.abs(h) * (horns > 2 ? 12 : 0));
      ctx.lineTo(hx + 12, -60 + breathe);
      ctx.fill();
    }
    if (k.crown) {
      ctx.fillStyle = flash ? "#ffffff" : "#ffcf4a";
      ctx.beginPath();
      ctx.moveTo(-34, -62 + breathe);
      for (let i = 0; i < 5; i++) {
        ctx.lineTo(-34 + i * 17, -88 + breathe);
        ctx.lineTo(-25 + i * 17, -70 + breathe);
      }
      ctx.lineTo(34, -62 + breathe);
      ctx.fill();
      ctx.fillStyle = "#d6264a";
      ctx.beginPath();
      ctx.arc(0, -72 + breathe, 4, 0, Math.PI * 2);
      ctx.fill();
    }
    const eyeY = -12 + breathe;
    const pupil = rage ? "#ff2a3a" : k.crown ? "#ffcf4a" : "#1a0c00";
    if (k.cyclops) {
      ctx.fillStyle = "#ffffff";
      ctx.beginPath();
      ctx.ellipse(0, eyeY, 26, 22, 0, 0, Math.PI * 2);
      ctx.fill();
      ctx.fillStyle = pupil;
      ctx.beginPath();
      ctx.arc(Math.sin(now / 700) * 8, eyeY + 2, 10, 0, Math.PI * 2);
      ctx.fill();
    } else {
      ctx.fillStyle = k.crown ? "#2b0a0a" : "#ffffff";
      ctx.beginPath();
      ctx.ellipse(-25, eyeY, 15, 12, 0, 0, Math.PI * 2);
      ctx.ellipse(25, eyeY, 15, 12, 0, 0, Math.PI * 2);
      ctx.fill();
      ctx.fillStyle = pupil;
      if (k.crown || rage) {
        ctx.shadowColor = pupil;
        ctx.shadowBlur = 14;
      }
      ctx.beginPath();
      ctx.arc(-23, eyeY + 2, 6, 0, Math.PI * 2);
      ctx.arc(23, eyeY + 2, 6, 0, Math.PI * 2);
      ctx.fill();
      ctx.shadowBlur = 0;
    }
    ctx.strokeStyle = "#1a0c00";
    ctx.lineWidth = 5;
    ctx.beginPath();
    ctx.moveTo(-44, -34 + breathe);
    ctx.lineTo(-10, -26 + breathe + (rage ? 6 : 0));
    ctx.moveTo(44, -34 + breathe);
    ctx.lineTo(10, -26 + breathe + (rage ? 6 : 0));
    ctx.stroke();
    if (k.dragon) {
      ctx.fillStyle = flash ? "#ffffff" : "#2f8a46";
      ctx.beginPath();
      ctx.ellipse(0, 26 + breathe, 34, 22, 0, 0, Math.PI * 2);
      ctx.fill();
      ctx.fillStyle = "#0b2a14";
      ctx.beginPath();
      ctx.arc(-10, 20 + breathe, 3, 0, Math.PI * 2);
      ctx.arc(10, 20 + breathe, 3, 0, Math.PI * 2);
      ctx.fill();
      if (Math.sin(now / 900) > 0.7) {
        const fl = ctx.createLinearGradient(0, 40, 0, 110);
        fl.addColorStop(0, "#fff3a0");
        fl.addColorStop(1, "rgba(255, 80, 20, 0)");
        ctx.fillStyle = fl;
        ctx.beginPath();
        ctx.moveTo(-14, 42 + breathe);
        ctx.quadraticCurveTo(0, 120 + Math.sin(now / 50) * 8, 14, 42 + breathe);
        ctx.fill();
      }
    } else {
      ctx.fillStyle = "#3a0a10";
      ctx.beginPath();
      ctx.ellipse(0, 26 + breathe, 32, 14 + (rage ? 8 : 0), 0, 0, Math.PI * 2);
      ctx.fill();
      ctx.fillStyle = "#ffffff";
      ctx.fillRect(-22, 14 + breathe, 9, 11);
      ctx.fillRect(13, 14 + breathe, 9, 11);
    }
    if (k.beard) {
      ctx.fillStyle = flash ? "#ffffff" : "#5a0a14";
      ctx.beginPath();
      ctx.moveTo(-34, 38 + breathe);
      ctx.quadraticCurveTo(0, 110 + breathe, 34, 38 + breathe);
      ctx.fill();
    }
    if (rage) {
      for (const side of [-1, 1]) {
        ctx.fillStyle = `rgba(255, 255, 255, ${0.25 + 0.2 * Math.sin(now / 90 + side)})`;
        ctx.beginPath();
        ctx.arc(side * (60 + Math.sin(now / 120) * 6), -70 - ((now / 8) % 30), 8, 0, Math.PI * 2);
        ctx.fill();
      }
    }
    ctx.restore();
  }

  function lighten(hex) {
    const n = parseInt(hex.slice(1), 16);
    const c = (s) => Math.min(255, ((n >> s) & 255) + 60);
    return `rgb(${c(16)}, ${c(8)}, ${c(0)})`;
  }

  function drawHero(x, y, s, face, swing, robe, now, archer) {
    ctx.save();
    ctx.translate(x, y);
    ctx.scale(s * face, s);
    const b = Math.sin(now / 250) * 2;
    ctx.fillStyle = "rgba(0, 0, 0, 0.35)";
    ctx.beginPath();
    ctx.ellipse(0, 50, 26, 6, 0, 0, Math.PI * 2);
    ctx.fill();
    ctx.fillStyle = "#2b1d14";
    ctx.fillRect(-12, 26, 9, 24);
    ctx.fillRect(3, 26, 9, 24);
    ctx.fillStyle = robe;
    ctx.beginPath();
    ctx.moveTo(-18, 30 + b);
    ctx.lineTo(18, 30 + b);
    ctx.lineTo(13, -14 + b);
    ctx.lineTo(-13, -14 + b);
    ctx.fill();
    ctx.strokeStyle = "#ffcf4a";
    ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.moveTo(-6, 28 + b);
    ctx.lineTo(-4, -12 + b);
    ctx.moveTo(6, 28 + b);
    ctx.lineTo(4, -12 + b);
    ctx.stroke();
    ctx.fillStyle = "#e8b48a";
    ctx.beginPath();
    ctx.arc(0, -24 + b, 11, 0, Math.PI * 2);
    ctx.fill();
    if (archer) {
      ctx.fillStyle = "#3a1a0a";
      ctx.beginPath();
      ctx.arc(-2, -30 + b, 12, Math.PI, 0);
      ctx.fill();
      ctx.fillRect(-14, -30 + b, 6, 22);
      ctx.fillStyle = "#d6264a";
      ctx.fillRect(-12, -36 + b, 24, 4);
      ctx.strokeStyle = "#a0662a";
      ctx.lineWidth = 3;
      ctx.beginPath();
      ctx.arc(14, -2 + b, 22, -1.3, 1.3);
      ctx.stroke();
      ctx.strokeStyle = "rgba(255, 255, 255, 0.7)";
      ctx.lineWidth = 1;
      ctx.beginPath();
      ctx.moveTo(14 + Math.cos(-1.3) * 22, -2 + b + Math.sin(-1.3) * 22);
      ctx.lineTo(14 - swing * 10, -2 + b);
      ctx.lineTo(14 + Math.cos(1.3) * 22, -2 + b + Math.sin(1.3) * 22);
      ctx.stroke();
    } else {
      ctx.fillStyle = "#f6f1e6";
      ctx.beginPath();
      ctx.ellipse(0, -32 + b, 13, 8, 0, Math.PI, 0);
      ctx.fill();
      ctx.fillStyle = "#2fd4c4";
      ctx.fillRect(-13, -33 + b, 26, 3);
      ctx.save();
      ctx.translate(12, 2 + b);
      ctx.rotate(-1.1 + swing * 2.4);
      ctx.fillStyle = "#8a5a2a";
      ctx.fillRect(-3, -4, 6, 12);
      ctx.fillStyle = "#ffcf4a";
      ctx.fillRect(-9, -6, 18, 4);
      const sg = ctx.createLinearGradient(0, -6, 0, -54);
      sg.addColorStop(0, "#d8e4f0");
      sg.addColorStop(1, "#ffffff");
      ctx.fillStyle = sg;
      ctx.beginPath();
      ctx.moveTo(-4, -6);
      ctx.lineTo(4, -6);
      ctx.quadraticCurveTo(6, -40, 0, -56);
      ctx.quadraticCurveTo(-3, -40, -4, -6);
      ctx.fill();
      if (swing > 0.2) {
        ctx.strokeStyle = "rgba(255, 255, 255, 0.6)";
        ctx.lineWidth = 3;
        ctx.beginPath();
        ctx.arc(0, 0, 52, -1.8, -1.1);
        ctx.stroke();
      }
      ctx.restore();
    }
    ctx.fillStyle = "#1a0c00";
    ctx.beginPath();
    ctx.arc(5, -25 + b, 2, 0, Math.PI * 2);
    ctx.fill();
    ctx.restore();
  }

  function drawEffects(dt) {
    for (const p of parts) {
      p.x += p.vx * dt * 0.06;
      p.y += p.vy * dt * 0.06;
      p.vy += 0.012 * dt;
      p.life -= dt / 700;
    }
    parts = parts.filter((p) => p.life > 0);
    for (const p of parts) {
      ctx.globalAlpha = p.life;
      ctx.fillStyle = p.color;
      ctx.beginPath();
      ctx.arc(p.x, p.y, p.s, 0, Math.PI * 2);
      ctx.fill();
    }
    ctx.globalAlpha = 1;
    for (const s of slashes) s.life -= dt / 260;
    slashes = slashes.filter((s) => s.life > 0);
    for (const s of slashes) {
      ctx.save();
      ctx.translate(s.x, s.y);
      ctx.rotate(s.a);
      ctx.globalAlpha = s.life;
      ctx.strokeStyle = "#fff6c8";
      ctx.lineWidth = 6 * s.life;
      ctx.shadowColor = "#ffcf4a";
      ctx.shadowBlur = 18;
      ctx.beginPath();
      ctx.moveTo(-70 * (1.2 - s.life), 0);
      ctx.lineTo(70 * (1.2 - s.life), 0);
      ctx.stroke();
      ctx.restore();
    }
    for (const a of arrows) {
      a.k += dt / 380;
      const x = a.x + (a.tx - a.x) * Math.min(1, a.k), y = a.y + (a.ty - a.y) * Math.min(1, a.k) - Math.sin(Math.min(1, a.k) * Math.PI) * 50;
      if (a.k >= 1 && !a.done) {
        a.done = true;
        if (a.silent) continue;
        nums.push({ x: a.tx, y: a.ty, vy: -1.2, vx: 0, text: `🏹 −${fmt(a.damage)}`, color: "#7fd6ff", size: 16, life: 1 });
        burst(a.tx, a.ty, "#7fd6ff", 8, 3);
      }
      if (!a.done) {
        ctx.strokeStyle = "#f3e2c0";
        ctx.lineWidth = 2.5;
        ctx.beginPath();
        ctx.moveTo(x, y);
        ctx.lineTo(x + 14, y + 4);
        ctx.stroke();
      }
    }
    arrows = arrows.filter((a) => !a.done);
    for (const n of nums) {
      n.y += n.vy * dt * 0.06;
      n.x += n.vx * dt * 0.06;
      n.vy *= 0.985;
      n.life -= dt / 950;
    }
    nums = nums.filter((n) => n.life > 0);
    ctx.textAlign = "center";
    for (const n of nums) {
      const pop = n.life > 0.85 ? 1 + (n.life - 0.85) * 3 : 1;
      ctx.globalAlpha = Math.min(1, n.life * 1.8);
      ctx.font = `${Math.round(n.size * pop)}px "Russo One", system-ui, sans-serif`;
      ctx.lineWidth = 5;
      ctx.strokeStyle = "rgba(0, 0, 0, 0.85)";
      ctx.strokeText(n.text, n.x, n.y);
      ctx.fillStyle = n.color;
      ctx.fillText(n.text, n.x, n.y);
    }
    ctx.globalAlpha = 1;
  }

  let last = performance.now(), arrowClock = 0;
  function frame(now) {
    const dt = Math.min(50, now - last);
    last = now;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    if (fx.shake > 0) {
      fx.shake = Math.max(0, fx.shake - dt / 320);
      ctx.translate((Math.random() - 0.5) * fx.shake * 14, (Math.random() - 0.5) * fx.shake * 14);
    }
    fx.flash = Math.max(0, fx.flash - dt);
    fx.squash = Math.max(0, fx.squash - dt / 150);
    fx.swing = Math.max(0, fx.swing - dt / 200);
    entrance = Math.max(0, entrance - dt / 900);
    const kind = boss ? boss.kind : 0;
    drawSky(kind, now);
    if (boss) {
      const r = bossRadius(), s = r / 90;
      const hp = Math.max(0, boss.hp - inflightDamage - pendingDamage);
      const rage = hp / boss.max_hp < 0.3;
      const x = bossX() + (rage ? Math.sin(now / 40) * 2 : 0);
      const y = bossY() - entrance * entrance * H;
      const sq = fx.squash;
      ctx.save();
      ctx.translate(x, y);
      ctx.scale(1 + sq * 0.07, 1 - sq * 0.07);
      ctx.translate(-x, -y);
      if (!victoryOpen) drawBoss(kind, x, y, s, now, fx.flash > 0, rage);
      ctx.restore();
      const gy = groundY();
      drawHero(W * 0.2, gy - 50, 1.25, 1, fx.swing, "#1d2c7a", now, false);
      if (hero && hero.helper > 0) {
        arrowClock += dt;
        if (arrowClock > 700 && !victoryOpen) {
          arrowClock = 0;
          const from = archerAt();
          arrows.push({ x: from[0], y: from[1], tx: bossX() + (Math.random() - 0.5) * 80, ty: bossY() + (Math.random() - 0.5) * 80, k: 0, done: false, silent: true });
        }
        drawHero(W * 0.84, gy - 50, 1.15, -1, (arrowClock / 700), "#7a2a6a", now, true);
      }
    }
    drawEffects(dt);
    requestAnimationFrame(frame);
  }

  // ---------- input and start ----------

  canvas.addEventListener("pointerdown", (e) => {
    if (!boss) return;
    const r = bossRadius();
    if (Math.hypot(e.clientX - bossX(), e.clientY - bossY()) < r * 1.25) tap(e.clientX, e.clientY);
  });
  addEventListener("keydown", (e) => {
    if ((e.code === "Space" || e.code === "Enter") && !e.repeat) {
      e.preventDefault();
      tap(bossX() + (Math.random() - 0.5) * 80, bossY() + (Math.random() - 0.5) * 60);
    }
  });
  addEventListener("resize", resize);
  document.addEventListener("visibilitychange", () => { if (!document.hidden) poll(); });

  resize();
  requestAnimationFrame(frame);
  if (!session) {
    message(t.noSession);
  } else {
    poll();
    setInterval(flush, 400);
    setInterval(() => { if (performance.now() - polled > 2400) poll(); }, 500);
  }
})();
