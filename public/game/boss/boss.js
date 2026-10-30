// Dev jangi. The server keeps each chat's boss and each player's hero, and
// counts what taps do; the page shows each blow at once, with the damage
// the server will count (crit.js), and sends the taps in batches.
"use strict";

(function () {
  const $ = (id) => document.getElementById(id);
  const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;

  const texts = {
    uz: {
      you: "Siz",
      names: ["Ko'k dev", "Qizil dev", "Bir ko'zli dev", "Ajdar", "Muz devi", "Tosh dev", "Yalmog'iz kampir", "Qora dev"],
      hint: "Devni bosib uring! Guruhdagilar ham siz bilan uradi",
      chief: "BOSH DEV", storm: "Bo'ron", fire: "Olov", ready: "Tayyor!",
      stormOn: "Bo'ron zarbasi! ×5", fireOn: "Olovli qilich! 10 krit",
      quests: "Kundalik vazifalar", questsReset: "Vazifalar har kuni yarim tunda yangilanadi.",
      questNames: ["300 marta ur", "1 ta dev yiqitishda qatnash", "Mahoratni 3 marta ishlat"], claim: "Olish", claimed: "Olindi ✓",
      streak: (n) => `🔥 ${n} kun ketma-ket`, gift: (n, s) => `🎁 Kunlik sovg'a: +${n} 💰<br><small>🔥 ${s}-kun ketma-ket</small>`, got: (n) => `+${n} 💰`,
      table: "Reyting", thisBoss: "Shu dev", allTime: "Umumiy", place: (n) => `${n}-o'rin`,
      titles: ["Yangi jangchi", "Botir", "Bahodir", "Pahlavon", "Qahramon", "Alpomish"], sword: "Qilich", crit: "Krit",
      lvl: (n) => `${n}-daraja`,
      swordNext: (a, b) => `zarba ${a} → ${b}`, critNext: (a, b) => `${a}% → ${b}%`,
      max: "Eng yuqori", killed: (name) => `${name} yengildi!`, reward: (n) => `+${n} 💰 sizga`,
      next: "Keyingi dev", share: "Ulashish", others: (name) => `${name} guruh bilan yengildi!`,
      poor: "Tanga yetmaydi", upgraded: "Kuchaydi!", rage: "Dev g'azabda!",
      noSession: "O'yinni Telegram'da oching: botga /boss yozing va «Play» ni bosing.",
      session: "O'yin havolasi eskirdi. Telegram'da «Play» ni qaytadan bosing.",
      unavailable: "Aloqa uzildi, qayta urinyapmiz…",
    },
    ru: {
      you: "Вы",
      names: ["Синий дэв", "Красный дэв", "Одноглазый дэв", "Аждар", "Ледяной дэв", "Каменный дэв", "Ялмогиз-кампир", "Чёрный дэв"],
      hint: "Бейте дэва! Группа бьёт вместе с вами",
      chief: "ГЛАВНЫЙ", storm: "Буря", fire: "Огонь", ready: "Готово!",
      stormOn: "Удар бури! ×5", fireOn: "Огненный меч! 10 критов",
      quests: "Ежедневные задания", questsReset: "Задания обновляются каждую полночь.",
      questNames: ["Ударить 300 раз", "Помочь победить 1 дэва", "Применить умение 3 раза"], claim: "Забрать", claimed: "Получено ✓",
      streak: (n) => `🔥 ${n} дн. подряд`, gift: (n, s) => `🎁 Ежедневный подарок: +${n} 💰<br><small>🔥 ${s}-й день подряд</small>`, got: (n) => `+${n} 💰`,
      table: "Рейтинг", thisBoss: "Этот дэв", allTime: "Всего", place: (n) => `${n}-е место`,
      titles: ["Новобранец", "Батыр", "Богатырь", "Пахлавон", "Герой", "Алпамыш"], sword: "Меч", crit: "Крит",
      lvl: (n) => `ур. ${n}`,
      swordNext: (a, b) => `удар ${a} → ${b}`, critNext: (a, b) => `${a}% → ${b}%`,
      max: "Максимум", killed: (name) => `${name} повержен!`, reward: (n) => `+${n} 💰 вам`,
      next: "Следующий дэв", share: "Поделиться", others: (name) => `${name} повержен группой!`,
      poor: "Не хватает монет", upgraded: "Сильнее!", rage: "Дэв в ярости!",
      noSession: "Откройте игру в Telegram: напишите боту /boss и нажмите «Play».",
      session: "Ссылка на игру устарела. Нажмите «Play» в Telegram ещё раз.",
      unavailable: "Связь прервалась, пробуем снова…",
    },
    en: {
      you: "You",
      names: ["Blue giant", "Red giant", "One-eyed giant", "Ajdar the dragon", "Ice giant", "Stone giant", "The witch Yalmog'iz", "Black giant"],
      hint: "Tap the giant! Your group hits it with you",
      chief: "CHIEF", storm: "Storm", fire: "Fire", ready: "Ready!",
      stormOn: "Storm strike! ×5", fireOn: "Fiery sword! 10 crits",
      quests: "Daily quests", questsReset: "Quests renew every midnight.",
      questNames: ["Hit 300 times", "Help defeat 1 giant", "Use a skill 3 times"], claim: "Claim", claimed: "Claimed ✓",
      streak: (n) => `🔥 ${n} ${n === 1 ? "day" : "days"} in a row`, gift: (n, s) => `🎁 Daily gift: +${n} 💰<br><small>🔥 day ${s} in a row</small>`, got: (n) => `+${n} 💰`,
      table: "Ranking", thisBoss: "This giant", allTime: "All time", place: (n) => `#${n}`,
      titles: ["Recruit", "Brave", "Warrior", "Champion", "Hero", "Alpomish"], sword: "Sword", crit: "Crit",
      lvl: (n) => `level ${n}`,
      swordNext: (a, b) => `blow ${a} → ${b}`, critNext: (a, b) => `${a}% → ${b}%`,
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
  const TITLE_ICONS = ["🪖", "🛡", "⚔️", "🏅", "👑", "🌟"];
  const NAME_COLORS = ["#7fd6ff", "#ff9be0", "#b4ff8a", "#ffb36b", "#c9a6ff", "#7fffd4", "#ffd27f"];

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

  let boss = null, hero = null, top = [], topTotal = [], rank = { level: 0, total: 0 }, myTitle = 0, boardMode = "level";
  let tapsPerSecond = 12, critTimes = 3;
  // skills is when, by this page's clock, each skill is ready and lasts
  // until; fireLeft is the fiery blows left, counted here as they are
  // struck.
  const skills = { storm: { readyAt: 0, until: 0, times: 5 }, fire: { readyAt: 0 } };
  let fireLeft = 0;
  let pending = 0, pendingDamage = 0, inflight = 0, inflightDamage = 0, sending = false, polled = 0;
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
    if (data.me) {
      myDamage = data.me.damage;
      if (data.me.title > myTitle && myTitle !== null && hero) titleUp(data.me.title);
      myTitle = data.me.title;
    }
    if (data.top_total) topTotal = data.top_total;
    if (data.rank) rank = data.rank;
    if (data.hero) {
      hero = data.hero;
      // A reply to a hit sent before a skill was used may come after it:
      // the times it gives only ever move a skill's times on.
      const now = performance.now(), sk = hero.skills;
      const later = (was, ms) => Math.max(was, ms > 0 ? now + ms : 0);
      skills.storm.readyAt = later(skills.storm.readyAt, sk.storm.ready_in);
      skills.storm.until = later(skills.storm.until, sk.storm.active_for);
      skills.storm.times = sk.storm.times;
      skills.fire.readyAt = later(skills.fire.readyAt, sk.fire.ready_in);
      if (data.top && pending === 0 && inflight === 0 && !sending) fireLeft = sk.fire.left;
    }
    if (data.gift > 0 && hero) toast(t.gift(fmt(data.gift), hero.streak), 3500);
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
    if (pending === 0) return;
    sending = true;
    inflight = pending;
    inflightDamage = pendingDamage;
    pending = 0;
    pendingDamage = 0;
    try {
      const data = await api({ action: "hit", taps: inflight });
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
    let crit;
    if (fireLeft > 0) {
      fireLeft -= 1;
      crit = true;
    } else {
      crit = critical(hero.seed, index, hero.crit_chance);
    }
    const storm = performance.now() < skills.storm.until;
    const damage = hero.tap_damage * (crit ? critTimes : 1) * (storm ? skills.storm.times : 1);
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

  // ---------- skills and quests ----------

  async function useSkill(skill) {
    if (!hero || victoryOpen) return;
    const now = performance.now();
    if (now < skills[skill].readyAt) return;
    await flush();
    try {
      apply(await api({ action: "skill", skill }));
      if (skill === "fire") fireLeft = hero.skills.fire.left;
      textPop(W / 2, bossY() - bossRadius() - 20, skill === "fire" ? t.fireOn : t.stormOn, "#ffb34a", 26);
      burst(bossX(), bossY(), "#ff8a3d", 50, 8);
      fx.shake = 0.8;
      if (navigator.vibrate) navigator.vibrate([20, 30, 40]);
      hud();
    } catch (error) {
      if (error.message !== "cooldown") failed(error);
    }
  }

  function skillsHud() {
    if (!hero) return;
    const now = performance.now();
    const every = { storm: 45000, fire: 60000 };
    for (const button of document.querySelectorAll(".skill")) {
      const name = button.dataset.skill, sk = skills[name];
      const wait = Math.max(0, sk.readyAt - now);
      const active = name === "fire" ? fireLeft > 0 : now < sk.until;
      button.classList.toggle("active", active);
      button.classList.toggle("ready", !wait && !active);
      button.style.setProperty("--cool", `${Math.min(100, (wait / every[name]) * 100)}%`);
      $(`${name}Info`).textContent = active ? (name === "fire" ? `🔥 ${fireLeft}` : `${Math.ceil((sk.until - now) / 1000)}s`) : wait ? `${Math.ceil(wait / 1000)}s` : t.ready;
    }
  }

  let questsShown = "";
  function questsHud() {
    if (!hero) return;
    let can = 0;
    for (const q of hero.quests) if (q.progress >= q.goal && !q.claimed) can += 1;
    $("questsDot").hidden = can === 0;
    // Drawn again only when they changed, so that a press of a button is
    // not lost to the list being drawn under it.
    const shown = JSON.stringify([hero.quests, hero.streak]);
    if (shown === questsShown) return;
    questsShown = shown;
    const list = $("questList");
    list.textContent = "";
    hero.quests.forEach((q, i) => {
      const done = q.progress >= q.goal;
      const li = document.createElement("li");
      const name = document.createElement("span");
      name.textContent = `${t.questNames[i]} · ${q.progress}/${q.goal}`;
      const bar = document.createElement("span");
      bar.className = "bar";
      bar.innerHTML = `<i style="width:${(q.progress / q.goal) * 100}%"></i>`;
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = q.claimed ? t.claimed : `${t.claim} +${fmt(q.reward)} 💰`;
      button.disabled = q.claimed || !done;
      if (done && !q.claimed) button.className = "can";
      button.addEventListener("click", () => claim(i, button));
      li.append(name, button, bar);
      list.append(li);
    });
    $("streak").textContent = hero.streak ? t.streak(hero.streak) : "";
  }

  async function claim(i, button) {
    button.disabled = true;
    try {
      const before = hero.coins;
      apply(await api({ action: "claim", quest: i }));
      toast(t.got(fmt(hero.coins - before)), 1600);
      if (navigator.vibrate) navigator.vibrate(20);
    } catch (error) {
      failed(error);
    }
    questsHud();
  }

  let toastTimer = 0;
  function toast(html, ms) {
    const box = $("toast");
    box.innerHTML = html;
    box.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { box.hidden = true; }, ms);
  }

  for (const button of document.querySelectorAll(".skill")) button.addEventListener("click", () => useSkill(button.dataset.skill));
  $("questsToggle").addEventListener("click", () => { questsHud(); $("quests").hidden = false; });
  $("questsClose").addEventListener("click", () => { $("quests").hidden = true; });
  $("quests").addEventListener("click", (e) => { if (e.target.id === "quests") $("quests").hidden = true; });

  // ---------- what others do ----------

  // The others who fight this boss stand by you, up to three of them, and
  // swing as their blows come in.
  const swings = new Map();
  const fighters = () => top.filter((row) => !row.me).slice(0, 3);
  const fighterAt = (i) => [W * (0.43 + i * 0.22), groundY() - 66 - (i % 2) * 8];

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
        swings.set(row.id, 1);
      }, (i * 2200) / parts + Math.random() * 300);
    }
  }

  // ---------- victory ----------

  function victory(kill) {
    victoryOpen = true;
    fx.shake = 1.5;
    for (let i = 0; i < 4; i++) setTimeout(() => burst(bossX(), bossY(), ["#ffcf4a", "#ff4d5e", "#ffffff", BossSkins.skies[kill.kind][2]][i], 60, 10), i * 140);
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
    const level = data.boss.level - 1, kind = (level - 1) % BossSkins.count;
    if (myDamage > 0 && hero && data.hero) {
      const fighters = top.map((row) => ({ name: row.name, damage: row.me ? Math.max(row.damage, myDamage) : row.damage, reward: null, me: row.me }));
      victory({ level, kind, reward: Math.max(0, data.hero.coins - hero.coins), fighters });
      return;
    }
    message(t.others(t.names[kind]));
    setTimeout(() => message(""), 3000);
    newBoss();
  }

  function titleUp(title) {
    textPop(W / 2, H * 0.3, `${TITLE_ICONS[title]} ${t.titles[title]}!`, "#ffcf4a", 28);
    burst(W / 2, H * 0.3, "#ffcf4a", 40, 7);
    const chip = $("rank");
    chip.classList.remove("up");
    void chip.offsetWidth;
    chip.classList.add("up");
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
    $("chief").hidden = !boss.chief;
    questsHud();
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
      $(`${item}Lvl`).textContent = t.lvl(level);
      $(`${item}Next`).textContent = next;
      $(`${item}Cost`).textContent = hero.max[item] ? t.max : `${fmt(hero.costs[item])} 💰`;
      el.classList.toggle("max", hero.max[item]);
      el.classList.toggle("poor", !hero.max[item] && have < hero.costs[item]);
      el.classList.toggle("ready", !hero.max[item] && have >= hero.costs[item]);
    };
    card("sword", hero.sword, t.swordNext(fmt(hero.tap_damage), fmt(hero.next.tap_damage)));
    card("crit", hero.crit, t.critNext(hero.crit_chance, hero.next.crit_chance));
    const chip = $("rank");
    const place = boardMode === "total" ? rank.total : rank.level || rank.total;
    chip.hidden = false;
    chip.textContent = `${TITLE_ICONS[myTitle]} ${t.titles[myTitle]}${place ? " · 🏆 " + t.place(place) : ""}`;
    const rows = boardMode === "total" ? topTotal : top;
    const board = $("boardList");
    board.textContent = "";
    rows.forEach((row, i) => {
      const li = document.createElement("li");
      if (row.me) li.className = "me";
      const value = boardMode === "total" ? row.total : row.damage;
      for (const [cls, text] of [["pos", i + 1], ["name", `${TITLE_ICONS[row.title || 0]} ${row.name || "?"}`], ["dmg", fmt(value)]]) {
        const span = document.createElement("span");
        span.className = cls;
        span.textContent = text;
        li.append(span);
      }
      board.append(li);
    });
  }

  $("boardToggle").addEventListener("click", () => { $("board").hidden = !$("board").hidden; });
  $("rank").addEventListener("click", () => { $("board").hidden = false; });
  for (const tab of document.querySelectorAll(".board-tabs button")) {
    tab.addEventListener("click", () => {
      boardMode = tab.dataset.b;
      for (const other of document.querySelectorAll(".board-tabs button")) other.classList.toggle("on", other === tab);
      hud();
    });
  }
  for (const card of document.querySelectorAll(".card")) card.addEventListener("click", () => upgrade(card.dataset.item, card));

  // ---------- the scene ----------

  const canvas = $("scene");
  const ctx = canvas.getContext("2d");
  let W = 0, H = 0, dpr = 1;
  let nums = [], parts = [], slashes = [];
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

  const cardsTop = () => document.querySelector(".skills").getBoundingClientRect().top;
  const groundY = () => cardsTop() - 18;
  const bossRadius = () => Math.max(40, Math.min(W * 0.36, (groundY() - 150) * 0.42, 170));
  const bossX = () => W / 2;
  const bossY = () => 130 + (groundY() - 130) * 0.48;

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
    const sky = BossSkins.skies[kind];
    const g = ctx.createLinearGradient(0, 0, 0, H);
    g.addColorStop(0, sky[0]);
    g.addColorStop(0.6, sky[1]);
    g.addColorStop(1, sky[2]);
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

  // drawBoss draws the boss of kind at (x, y), s its size (skins.js draws
  // it about 100 across), red-eyed and angrier when nearly dead. A hit turns
  // it white: it is drawn on a layer of its own, which is whitened, so that
  // the sky behind it is not.
  const layer = document.createElement("canvas");
  const layerCtx = layer.getContext("2d");
  function drawBoss(kind, x, y, s, now, flash, rage) {
    if (!flash) {
      ctx.save();
      ctx.translate(x, y);
      ctx.scale(s, s);
      BossSkins.draw(ctx, kind, now, rage);
      ctx.restore();
      return;
    }
    const half = 190, size = Math.ceil(half * 2 * s * dpr);
    if (layer.width !== size) layer.width = layer.height = size;
    layerCtx.setTransform(1, 0, 0, 1, 0, 0);
    layerCtx.clearRect(0, 0, size, size);
    layerCtx.setTransform(s * dpr, 0, 0, s * dpr, half * s * dpr, half * s * dpr);
    BossSkins.draw(layerCtx, kind, now, rage);
    layerCtx.setTransform(1, 0, 0, 1, 0, 0);
    layerCtx.globalCompositeOperation = "source-atop";
    layerCtx.fillStyle = "rgba(255, 255, 255, 0.85)";
    layerCtx.fillRect(0, 0, size, size);
    layerCtx.globalCompositeOperation = "source-over";
    ctx.drawImage(layer, x - half * s, y - half * s, half * 2 * s, half * 2 * s);
  }

  function nameTag(name, x, y, color) {
    const text = name.length > 9 ? name.slice(0, 8) + "…" : name;
    ctx.font = "700 12px system-ui, sans-serif";
    ctx.textAlign = "center";
    ctx.lineWidth = 3;
    ctx.strokeStyle = "rgba(0, 0, 0, 0.6)";
    ctx.strokeText(text, x, y);
    ctx.fillStyle = color;
    ctx.fillText(text, x, y);
  }

  function drawHero(x, y, s, face, swing, robe, now) {
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

  let last = performance.now();
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
      const s = Math.min(bossRadius() / 90, (W / 2 - 4) / BossSkins.reach[kind]);
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
      fighters().forEach((row, i) => {
        const [fx2, fy] = fighterAt(i), swing = swings.get(row.id) || 0;
        swings.set(row.id, Math.max(0, swing - dt / 200));
        drawHero(fx2, fy, 0.95, fx2 < bossX() ? 1 : -1, swing, colorOf(row.id), now);
        nameTag(row.name, fx2, fy + 62, "#fff");
      });
      drawHero(W * 0.18, gy - 68, 1.15, 1, fx.swing, "#1d2c7a", now);
      nameTag(t.you, W * 0.18, gy - 68 + 72, "#ffcf4a");
    }
    if (performance.now() < skills.storm.until) {
      ctx.fillStyle = `rgba(255, 120, 40, ${0.08 + 0.05 * Math.sin(now / 80)})`;
      ctx.fillRect(-20, -20, W + 40, H + 40);
    }
    drawEffects(dt);
    skillsHud();
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
