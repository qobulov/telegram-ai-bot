// The Farosat Mini App: a member's galaxies in a group, opened by their
// farosat, and the shop where grams are bought with Stars. Every galaxy is
// drawn here from its kind, by a seeded pattern of stars: no pictures.
(function () {
  "use strict";
  const tg = window.Telegram && window.Telegram.WebApp;
  if (tg) { tg.ready(); tg.expand(); }
  const params = new URLSearchParams(location.search);
  const group = params.get("g") || (tg && tg.initDataUnsafe && tg.initDataUnsafe.start_param) || "";
  const reduced = window.matchMedia && matchMedia("(prefers-reduced-motion: reduce)").matches;
  const $ = (id) => document.getElementById(id);
  const fmt = (n) => String(n).replace(/\B(?=(\d{3})+(?!\d))/g, " ");

  let data = null;
  let index = 0; // the galaxy on view, by its place in data.galaxies
  let view = []; // the galaxies the filter and search let through

  async function api(action, extra) {
    const res = await fetch("/api/farosat", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ init_data: (tg && tg.initData) || "", group, action, ...extra }),
    });
    const body = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(body.error || "unavailable");
    return body;
  }

  const errors = {
    no_group: "Ilovani guruhdagi «🌌 Farosat ilovasi» tugmasi orqali oching.",
    stale: "Sahifa eskirdi: ilovani yopib, qayta oching.",
    forged: "Ilovani Telegram ichida oching.",
    open: "Bu galaktika allaqachon ochilgan.",
  };

  function showError(code) {
    const state = $("state");
    state.textContent = errors[code] || "Hozir ochib bo'lmadi, birozdan keyin urinib ko'ring.";
    state.hidden = false;
    $("galaxies").hidden = true;
    $("shop").hidden = true;
  }

  async function load(keep) {
    try {
      data = await api("state");
    } catch (e) {
      showError(e.message);
      return;
    }
    $("tally").innerHTML = `🪐 <b>${data.open}</b> ta koinotingizda · 🧠 <b>${fmt(data.grams)}</b> gram · <b>${data.place}</b>-o'rin`;
    renderPacks();
    fillFilter();
    if (!keep) index = here();
    applyView(index);
    if (!keep && params.get("tab") === "shop") tab(true);
    if (!keep && params.get("pack")) showPack(Number(params.get("pack")) || 0);
    if (!keep && params.get("space") && galaxy3d) $("spaceOpen").click();
  }

  // ---- Filter, search and the galaxy on view ------------------------------

  const filters = [
    ["all", "Hammasi", () => true],
    ["open", "Ochilgan", (g) => g.open],
    ["closed", "Yopiq", (g) => !g.open],
    ["buyable", "Sotib olsa bo'ladi", (g) => !g.open && g.stars > 0],
    ["small", "Kichik", (g) => g.size === "small"],
    ["medium", "O'rta", (g) => g.size === "medium"],
    ["large", "Yirik", (g) => g.size === "large"],
  ];

  function fillFilter() {
    const select = $("filter");
    const chosen = select.value || "all";
    select.textContent = "";
    for (const [key, label, keep] of filters) {
      const option = document.createElement("option");
      option.value = key;
      option.textContent = `${label} · ${data.galaxies.filter(keep).length}`;
      select.appendChild(option);
    }
    select.value = chosen;
  }

  // here is the galaxy the member's farosat has reached last.
  function here() {
    let at = 0;
    data.galaxies.forEach((g, i) => { if (data.grams >= g.grams) at = i; });
    return at;
  }

  // applyView keeps the galaxies the filter and search let through, and
  // shows wanted when it is among them, else the first.
  function applyView(wanted) {
    const keep = (filters.find((f) => f[0] === $("filter").value) || filters[0])[2];
    const query = $("search").value.trim().toLowerCase();
    view = [];
    data.galaxies.forEach((g, i) => {
      if (keep(g) && (!query || g.name.toLowerCase().includes(query))) view.push(i);
    });
    if (!view.length) {
      empty();
      return;
    }
    show(view.includes(wanted) ? wanted : view[0]);
  }

  function empty() {
    $("card").hidden = false;
    $("meta").textContent = "";
    $("chip").textContent = "";
    $("name").textContent = "Hech narsa topilmadi";
    $("sub").textContent = "Boshqa so'z bilan qidiring yoki saralashni o'zgartiring.";
    $("fact").textContent = "";
    $("progress").hidden = true;
    $("prev").disabled = $("next").disabled = true;
  }

  $("filter").addEventListener("change", () => applyView(index));
  $("search").addEventListener("input", () => applyView(index));
  $("here").addEventListener("click", () => {
    $("filter").value = "all";
    $("search").value = "";
    applyView(here());
  });

  function show(i) {
    const list = data.galaxies;
    index = i;
    const g = list[index];
    const pos = view.indexOf(index);
    $("meta").textContent = `#${g.id} / ${list.length} · ${fmt(g.grams)} gram`;
    const chip = $("chip");
    chip.textContent = g.bought ? "Sotib olingan" : g.open ? "Ochilgan" : "Yetib borilmagan";
    chip.className = "chip" + (g.bought ? " bought" : g.open ? "" : " locked");
    $("name").textContent = g.name;
    $("sub").textContent = `${g.class} · ${g.distance}`;
    $("fact").textContent = g.fact;
    const progress = $("progress");
    progress.hidden = g.open;
    if (!g.open) {
      const prev = index > 0 ? list[index - 1].grams : 0;
      const share = Math.max(0, Math.min(1, (data.grams - prev) / Math.max(1, g.grams - prev)));
      $("barFill").style.width = `${Math.round(share * 100)}%`;
      $("need").textContent = `🔒 Yana ${fmt(g.grams - data.grams)} gram kerak`;
      const openBuy = $("openBuy");
      openBuy.hidden = !g.stars;
      openBuy.disabled = false;
      openBuy.textContent = `⭐ ${g.stars} Stars bilan hozir ochish`;
    }
    $("prev").disabled = pos <= 0;
    $("next").disabled = pos === view.length - 1;
    galaxy.set(g);
  }

  function step(by) {
    const pos = view.indexOf(index) + by;
    if (pos >= 0 && pos < view.length) show(view[pos]);
  }
  $("prev").addEventListener("click", () => step(-1));
  $("next").addEventListener("click", () => step(1));
  // A swipe changes the galaxy on the flat drawing; in 3D a finger turns it.
  let touchX = null;
  $("galaxy").addEventListener("touchstart", (e) => { touchX = e.touches[0].clientX; }, { passive: true });
  $("galaxy").addEventListener("touchend", (e) => {
    if (touchX === null || galaxy3d) return;
    const dx = e.changedTouches[0].clientX - touchX;
    touchX = null;
    if (Math.abs(dx) > 40) step(dx < 0 ? 1 : -1);
  });

  $("openBuy").addEventListener("click", () => {
    const g = data.galaxies[index];
    pay($("openBuy"), "galaxy_invoice", { galaxy: g.id }, () => data.galaxies[index].open, null);
  });

  // ---- Tabs and shop -----------------------------------------------------

  function tab(shop) {
    $("tabGalaxies").setAttribute("aria-selected", String(!shop));
    $("tabShop").setAttribute("aria-selected", String(shop));
    $("galaxies").hidden = shop;
    $("shop").hidden = !shop;
  }
  $("tabGalaxies").addEventListener("click", () => tab(false));
  $("tabShop").addEventListener("click", () => tab(true));

  // renderPacks shows the packs: one at a time as a turning crystal in 3D,
  // else as a grid.
  let packIndex = 0;
  function renderPacks() {
    if (crystal3d) {
      showPack(packIndex);
      return;
    }
    $("shopStage").hidden = true;
    $("packInfo").hidden = true;
    $("packs").hidden = false;
    renderPackGrid();
  }

  function showPack(i) {
    const packs = data.packs;
    packIndex = Math.max(0, Math.min(packs.length - 1, i));
    $("packPrev").disabled = packIndex === 0;
    $("packNext").disabled = packIndex === packs.length - 1;
    const p = packs[packIndex];
    const color = TIERS[packIndex % TIERS.length];
    $("shop").style.setProperty("--tier", color);
    $("packTier").textContent = p.name;
    $("packGrams").textContent = fmt(p.stars);
    $("crystal").setAttribute("aria-label", `${p.name}: ${fmt(p.stars)} gram, ⭐ ${fmt(p.stars)} — sotib olish`);
    const dots = $("packDots");
    dots.textContent = "";
    packs.forEach((_, j) => {
      const dot = document.createElement("span");
      if (j === packIndex) dot.className = "on";
      dots.appendChild(dot);
    });
    crystal3d.set(packIndex, color, packs.length);
  }
  $("packPrev").addEventListener("click", () => showPack(packIndex - 1));
  $("packNext").addEventListener("click", () => showPack(packIndex + 1));
  // The crystal on view is the pack's buy button, for the keyboard too.
  $("crystal").addEventListener("keydown", (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); buy(data.packs[packIndex], $("crystal")); } });

  function renderPackGrid() {
    const box = $("packs");
    box.textContent = "";
    for (const p of data.packs) {
      const card = document.createElement("div");
      card.className = "pack";
      card.innerHTML = `<span class="tier"></span><span class="grams"></span><span class="unit">gram</span><button type="button"></button>`;
      card.querySelector(".tier").textContent = p.name;
      card.querySelector(".grams").textContent = fmt(p.stars);
      const button = card.querySelector("button");
      button.textContent = `Sotib olish · ⭐ ${fmt(p.stars)}`;
      button.addEventListener("click", () => buy(p, button));
      box.appendChild(card);
    }
  }

  // toast says how a payment went, for a few seconds.
  let toastTimer = 0;
  function toast(text) {
    const el = $("toast");
    el.textContent = text;
    el.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { el.hidden = true; }, 3500);
  }

  // pay opens the invoice action makes, says how it went, and on payment
  // looks again until done() is true, as the bot hears of the payment
  // a moment after the page does.
  async function pay(button, action, extra, done, label) {
    if (button.dataset.busy) return;
    const before = button.textContent;
    button.disabled = true;
    button.dataset.busy = "1";
    button.textContent = "Ochilmoqda…";
    const restore = () => { button.disabled = false; delete button.dataset.busy; button.textContent = label || before; };
    try {
      const { link } = await api(action, extra);
      if (!tg || !tg.openInvoice) { location.href = link; return; }
      tg.openInvoice(link, (status) => {
        restore();
        if (status === "paid") {
          if (tg.HapticFeedback) tg.HapticFeedback.notificationOccurred("success");
          toast("✅ To'lov qabul qilindi");
          let tries = 0;
          const again = async () => {
            await load(true);
            if (!done() && ++tries < 6) setTimeout(again, 1500);
          };
          setTimeout(again, 1200);
        } else if (status === "cancelled") {
          toast("To'lov bekor qilindi");
        } else if (status === "failed") {
          toast("To'lov amalga oshmadi");
        }
      });
    } catch (e) {
      restore();
      toast(errors[e.message] || "To'lovni ochib bo'lmadi, qayta urinib ko'ring.");
    }
  }

  function buy(pack, button) {
    const before = data.grams;
    const label = button.id === "crystal" ? "" : `Sotib olish · ⭐ ${fmt(pack.stars)}`;
    pay(button, "invoice", { pack: pack.id }, () => data.grams !== before, label);
  }

  // ---- Drawing -----------------------------------------------------------

  // rng is a small seeded generator, so that each galaxy looks the same
  // every time it is shown.
  function rng(seed) {
    let s = (seed * 2654435761) >>> 0 || 1;
    return () => {
      s ^= s << 13; s >>>= 0; s ^= s >> 17; s ^= s << 5; s >>>= 0;
      return s / 4294967296;
    };
  }
  function gauss(r) {
    return Math.sqrt(-2 * Math.log(r() || 1e-9)) * Math.cos(2 * Math.PI * r());
  }

  // stars builds a galaxy's stars as [x, y, size, r, g, b, alpha], in a
  // unit square around 0, before its turn and tilt.
  function stars(g) {
    const r = rng(g.id * 7919 + 17);
    const out = [];
    const push = (x, y, size, c, a) => out.push([x, y, size, c[0], c[1], c[2], a]);
    const core = [255, 236, 200], blue = [170, 200, 255], white = [235, 240, 255], pink = [255, 140, 190], gold = [255, 205, 140];
    const bulge = (n, spread, color) => {
      for (let i = 0; i < n; i++) push(gauss(r) * spread, gauss(r) * spread, 0.6 + r() * 1.4, color, 0.5 + r() * 0.5);
    };
    const spiral = (arms, twist, barLen) => {
      bulge(1700, 0.055, core);
      if (barLen) for (let i = 0; i < 700; i++) push((r() - 0.5) * barLen, gauss(r) * 0.025, 0.7 + r(), gold, 0.6);
      for (let a = 0; a < arms; a++) {
        const base = (a / arms) * Math.PI * 2;
        for (let k = 0; k < 9; k++) {
          // Knots of young stars along the arm, pink as their gas glows.
          const t = 0.25 + r() * 0.7;
          const rad = (barLen ? barLen / 2 : 0.06) + t * 0.42;
          const ang = base + t * twist;
          const kx = Math.cos(ang) * rad, ky = Math.sin(ang) * rad;
          for (let j = 0; j < 26; j++) push(kx + gauss(r) * 0.008, ky + gauss(r) * 0.008, 0.8 + r() * 1.2, r() < 0.7 ? pink : white, 0.55 + r() * 0.45);
        }
        for (let i = 0; i < 2600; i++) {
          const t = Math.pow(r(), 0.8);
          const rad = (barLen ? barLen / 2 : 0.06) + t * 0.42;
          const ang = base + t * twist + gauss(r) * 0.09;
          const jitter = gauss(r) * 0.012 * (1 + t);
          const x = Math.cos(ang) * rad + jitter, y = Math.sin(ang) * rad + jitter;
          const c = r() < 0.03 ? pink : (r() < 0.55 ? blue : white);
          push(x, y, 0.5 + r() * 1.1, c, 0.35 + r() * 0.55);
        }
      }
      for (let i = 0; i < 350; i++) {
        const rad = Math.sqrt(r()) * 0.45, ang = r() * Math.PI * 2;
        push(Math.cos(ang) * rad, Math.sin(ang) * rad, 0.4 + r() * 0.6, white, 0.08 + r() * 0.14);
      }
    };
    switch (g.kind) {
      case "spiral": spiral(2 + Math.floor(r() * 2), 5.5 + r() * 2, 0); break;
      case "barred": spiral(2, 5 + r() * 1.5, 0.22 + r() * 0.1); break;
      case "elliptical": {
        const e = 0.55 + r() * 0.4;
        for (let i = 0; i < 4200; i++) {
          const s = Math.abs(gauss(r)) * 0.13;
          const ang = r() * Math.PI * 2;
          push(Math.cos(ang) * s, Math.sin(ang) * s * e, 0.5 + r() * 1.2, r() < 0.7 ? gold : core, 0.25 + r() * 0.6);
        }
        break;
      }
      case "lenticular": {
        bulge(1800, 0.09, core);
        for (let i = 0; i < 2200; i++) {
          const rad = Math.sqrt(r()) * 0.46, ang = r() * Math.PI * 2;
          const x = Math.cos(ang) * rad, y = Math.sin(ang) * rad;
          push(x, y, 0.5 + r(), gold, 0.25 + r() * 0.35);
        }
        break;
      }
      case "irregular": {
        const clumps = 4 + Math.floor(r() * 4);
        for (let c = 0; c < clumps; c++) {
          const cx = gauss(r) * 0.15, cy = gauss(r) * 0.12, spread = 0.04 + r() * 0.07;
          for (let i = 0; i < 600; i++) push(cx + gauss(r) * spread, cy + gauss(r) * spread, 0.5 + r() * 1.2, r() < 0.12 ? pink : (r() < 0.6 ? blue : white), 0.3 + r() * 0.6);
        }
        break;
      }
      case "ring": {
        bulge(1200, 0.06, gold);
        for (let i = 0; i < 3200; i++) {
          const ang = r() * Math.PI * 2, rad = 0.34 + gauss(r) * 0.025;
          push(Math.cos(ang) * rad, Math.sin(ang) * rad, 0.5 + r() * 1.2, r() < 0.1 ? pink : blue, 0.35 + r() * 0.55);
        }
        break;
      }
      case "edge": {
        for (let i = 0; i < 4200; i++) {
          const x = gauss(r) * 0.17, y = gauss(r) * 0.022 * (1 - Math.min(1, Math.abs(x) * 2));
          push(x, y, 0.5 + r() * 1.1, Math.abs(x) < 0.05 ? core : (r() < 0.6 ? blue : white), 0.3 + r() * 0.6);
        }
        bulge(500, 0.035, core);
        break;
      }
      case "merger": {
        for (const [cx, cy, turn] of [[-0.12, -0.04, 1], [0.13, 0.06, -1]]) {
          for (let i = 0; i < 1000; i++) push(cx + gauss(r) * 0.04, cy + gauss(r) * 0.04, 0.6 + r() * 1.2, core, 0.5 + r() * 0.5);
          for (let i = 0; i < 1100; i++) {
            const t = r(), ang = turn * (t * 4.5) + (turn > 0 ? 0 : Math.PI);
            const rad = 0.05 + t * 0.32;
            push(cx + Math.cos(ang) * rad + gauss(r) * 0.02, cy + Math.sin(ang) * rad * 0.6 + gauss(r) * 0.02, 0.5 + r(), r() < 0.08 ? pink : blue, 0.3 + r() * 0.5);
          }
        }
        break;
      }
      default: bulge(3000, 0.12, white);
    }
    return out;
  }

  // flatGalaxy draws the galaxy on view flat, turning slowly, where WebGL
  // is not to be had; a locked one is dim. It is made only then: a canvas
  // that gave out a 2D context gives no WebGL one.
  function flatGalaxy() {
    const canvas = $("galaxy");
    const ctx = canvas.getContext("2d");
    let current = null, points = [], tilt = 0.55, angle = 0, frame = 0;
    function size() {
      const dpr = Math.min(window.devicePixelRatio || 1, 2);
      const w = canvas.clientWidth || 360;
      canvas.width = Math.round(w * dpr);
      canvas.height = Math.round(w * dpr);
    }
    function draw() {
      const w = canvas.width, h = canvas.height, scale = w * 0.95;
      ctx.clearRect(0, 0, w, h);
      if (!current) return;
      const open = current.open;
      const glow = ctx.createRadialGradient(w / 2, h / 2, 0, w / 2, h / 2, w * 0.3);
      glow.addColorStop(0, open ? "rgba(255,230,190,0.35)" : "rgba(150,150,190,0.12)");
      glow.addColorStop(1, "rgba(0,0,0,0)");
      ctx.fillStyle = glow;
      ctx.fillRect(0, 0, w, h);
      const cos = Math.cos(angle), sin = Math.sin(angle);
      const flat = current.kind === "edge" || current.kind === "elliptical" || current.kind === "merger" || current.kind === "irregular";
      const squash = flat ? 1 : tilt;
      ctx.globalCompositeOperation = "lighter";
      for (const p of points) {
        const x = (flat ? p[0] : p[0] * cos - p[1] * sin) * scale + w / 2;
        const y = (flat ? p[1] : (p[0] * sin + p[1] * cos) * squash) * scale + h / 2;
        const shade = open ? 1 : 0.25;
        const grey = (p[3] + p[4] + p[5]) / 3;
        const c = open ? `rgba(${p[3]},${p[4]},${p[5]},${p[6]})` : `rgba(${grey},${grey},${grey + 20},${p[6] * shade})`;
        ctx.fillStyle = c;
        const s = p[2] * (w / 420);
        ctx.fillRect(x, y, s, s);
      }
      if (current.kind === "lenticular") {
        ctx.globalCompositeOperation = "source-over";
        ctx.fillStyle = open ? "rgba(20,12,8,0.75)" : "rgba(10,10,14,0.6)";
        ctx.beginPath();
        ctx.ellipse(w / 2, h / 2 + w * 0.012, w * 0.43, w * 0.022, 0, 0, Math.PI * 2);
        ctx.fill();
      }
      ctx.globalCompositeOperation = "source-over";
    }
    function loop() {
      angle += 0.0016;
      draw();
      frame = requestAnimationFrame(loop);
    }
    window.addEventListener("resize", () => { size(); draw(); });
    return {
      set(g) {
        current = g;
        points = stars(g);
        tilt = g.kind === "lenticular" ? 0.32 : 0.5 + (g.id % 5) * 0.06;
        angle = (g.id * 1.3) % (Math.PI * 2);
        size();
        cancelAnimationFrame(frame);
        if (reduced) draw(); else loop();
      },
    };
  }

  // ---- 3D -----------------------------------------------------------------

  // TIERS colour the packs, the smallest first.
  const TIERS = ["#8fd3ff", "#ff7ac8", "#9b8cff", "#5ef2c2", "#9df27a", "#ffa45c", "#ff6b7a", "#ffd36b"];

  // webgl is a renderer on canvas, or null where WebGL is not to be had,
  // and the page draws flat instead.
  function webgl(canvas) {
    if (!window.THREE) return null;
    try {
      const renderer = new THREE.WebGLRenderer({ canvas, antialias: true, alpha: true });
      renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
      return renderer;
    } catch (e) {
      return null;
    }
  }

  // glowTexture is a soft round dot, the shape of every star.
  function glowTexture() {
    const c = document.createElement("canvas");
    c.width = c.height = 64;
    const x = c.getContext("2d");
    const grad = x.createRadialGradient(32, 32, 0, 32, 32, 32);
    grad.addColorStop(0, "rgba(255,255,255,1)");
    grad.addColorStop(0.25, "rgba(255,255,255,0.8)");
    grad.addColorStop(1, "rgba(255,255,255,0)");
    x.fillStyle = grad;
    x.fillRect(0, 0, 64, 64);
    return new THREE.CanvasTexture(c);
  }

  // galaxyTilt is how far from edge-on each kind is seen, in radians.
  const galaxyTilt = { spiral: 1.0, barred: 1.05, ring: 1.2, lenticular: 0.22, edge: 0.07, elliptical: 0.5, irregular: 0.9, merger: 0.8 };

  // points3d are a galaxy's stars in space: its disc lies flat, a few stars
  // thick, its bulge round; an elliptical is a ball.
  function points3d(g) {
    const r = rng(g.id * 31 + 5);
    const flat = stars(g.kind === "edge" ? Object.assign({}, g, { kind: "spiral" }) : g);
    return flat.map((p) => {
      const [x, y] = p;
      const dist = Math.hypot(x, y);
      let z;
      switch (g.kind) {
        case "elliptical": z = gauss(r) * 0.09; break;
        case "irregular": z = gauss(r) * 0.06; break;
        case "merger": z = gauss(r) * 0.02 + (x > 0 ? 0.35 * y : -0.25 * y); break;
        default: z = dist < 0.08 ? gauss(r) * 0.045 : gauss(r) * 0.012 * (1 - Math.min(1, dist * 1.8));
      }
      return [x, z, y, p[2], p[3], p[4], p[5], p[6]];
    });
  }

  // galaxy3d draws the galaxy on view as a cloud of stars in space. A finger
  // turns it any way, with a flick it keeps turning a while; two fingers or
  // the wheel bring it near or far, a double tap sets it back. "Kosmosda
  // ochish" opens it over the whole screen among stars of its own.
  const galaxy3d = (function () {
    const canvas = $("galaxy");
    const renderer = webgl(canvas);
    if (!renderer) return null;
    const stage = $("galaxyStage");
    const scene = new THREE.Scene();
    const camera = new THREE.PerspectiveCamera(45, 1, 0.01, 20);
    const turn = new THREE.Group(); // the member's turning
    const tilt = new THREE.Group(); // how the galaxy is seen from Earth
    const spin = new THREE.Group(); // its own slow turning
    turn.add(tilt);
    tilt.add(spin);
    scene.add(turn);
    const dot = glowTexture();
    const sky = backdrop();
    scene.add(sky);
    let zoom = 1.3, open = false, velocity = null;

    // backdrop is far stars all round, seen only in open space, so that
    // turning shows the galaxy moving among them.
    function backdrop() {
      const r = rng(424242);
      const n = 1600;
      const pos = new Float32Array(n * 3);
      const col = new Float32Array(n * 3);
      for (let i = 0; i < n; i++) {
        const u = r() * 2 - 1, a = r() * Math.PI * 2, d = 5 + r() * 4;
        const k = Math.sqrt(1 - u * u);
        pos.set([d * k * Math.cos(a), d * u, d * k * Math.sin(a)], i * 3);
        const b = 0.35 + r() * 0.65, warm = r();
        col.set([b * (warm > 0.8 ? 1 : 0.85), b * 0.9, b * (warm < 0.3 ? 1 : 0.85)], i * 3);
      }
      const geometry = new THREE.BufferGeometry();
      geometry.setAttribute("position", new THREE.BufferAttribute(pos, 3));
      geometry.setAttribute("color", new THREE.BufferAttribute(col, 3));
      const points = new THREE.Points(geometry, new THREE.PointsMaterial({ size: 0.07, map: dot, vertexColors: true, transparent: true, depthWrite: false, blending: THREE.AdditiveBlending }));
      points.visible = false;
      return points;
    }

    // rotate turns the galaxy by a drag of dx, dy pixels, about the axis
    // across the drag as the camera sees it.
    function rotate(dx, dy) {
      const len = Math.hypot(dx, dy);
      if (!len) return;
      const q = new THREE.Quaternion().setFromAxisAngle(new THREE.Vector3(dy / len, dx / len, 0), len * 0.009);
      turn.quaternion.premultiply(q);
      sky.quaternion.premultiply(q);
    }
    function setZoom(z) { zoom = Math.max(0.35, Math.min(4, z)); }
    // fit is the distance the whole galaxy is seen from: further on a tall,
    // narrow screen, where less of it fits across.
    function fit() { return 1.3 / Math.min(1, camera.aspect * 1.1); }
    function reset() {
      turn.quaternion.identity();
      sky.quaternion.identity();
      velocity = null;
      zoom = fit();
    }

    const fingers = new Map();
    let pinch = null, lastTap = 0;
    canvas.addEventListener("pointerdown", (e) => {
      canvas.setPointerCapture(e.pointerId);
      fingers.set(e.pointerId, { x: e.clientX, y: e.clientY, t: performance.now() });
      velocity = null;
      if (fingers.size === 2) {
        const [a, b] = [...fingers.values()];
        pinch = { dist: Math.hypot(a.x - b.x, a.y - b.y), zoom };
      }
      const now = performance.now();
      if (fingers.size === 1 && now - lastTap < 300) reset();
      lastTap = now;
    });
    canvas.addEventListener("pointermove", (e) => {
      const f = fingers.get(e.pointerId);
      if (!f) return;
      const now = performance.now();
      const dx = e.clientX - f.x, dy = e.clientY - f.y;
      fingers.set(e.pointerId, { x: e.clientX, y: e.clientY, t: now, dx, dy, dt: now - f.t });
      if (pinch && fingers.size >= 2) {
        const [a, b] = [...fingers.values()];
        const dist = Math.hypot(a.x - b.x, a.y - b.y);
        if (dist > 10) setZoom(pinch.zoom * (pinch.dist / dist));
        return;
      }
      if (fingers.size === 1) {
        rotate(dx, dy);
        lastTap = Math.abs(dx) + Math.abs(dy) > 6 ? 0 : lastTap;
      }
    });
    const lift = (e) => {
      const f = fingers.get(e.pointerId);
      fingers.delete(e.pointerId);
      if (fingers.size < 2) pinch = null;
      // A flick keeps the galaxy turning, slowing down.
      if (f && f.dt && !pinch && fingers.size === 0 && performance.now() - f.t < 80) {
        velocity = [f.dx * (16 / Math.max(8, f.dt)), f.dy * (16 / Math.max(8, f.dt))];
      }
    };
    canvas.addEventListener("pointerup", lift);
    canvas.addEventListener("pointercancel", lift);
    canvas.addEventListener("wheel", (e) => { e.preventDefault(); setZoom(zoom * Math.exp(e.deltaY * 0.0015)); }, { passive: false });
    canvas.addEventListener("dblclick", reset);

    // Open space: the stage over the whole screen; Telegram's back button,
    // the cross or Escape close it.
    function setOpen(on) {
      open = on;
      stage.classList.toggle("space", on);
      document.body.classList.toggle("in-space", on);
      sky.visible = on;
      $("spaceOpen").hidden = on;
      $("spaceClose").hidden = !on;
      if (tg && tg.BackButton) {
        if (on) { tg.BackButton.show(); tg.BackButton.onClick(close); } else { tg.BackButton.offClick(close); tg.BackButton.hide(); }
      }
      if (on && tg && tg.expand) tg.expand();
      size();
      reset();
    }
    const close = () => setOpen(false);
    $("spaceOpen").addEventListener("click", () => setOpen(true));
    $("spaceClose").addEventListener("click", close);
    document.addEventListener("keydown", (e) => { if (e.key === "Escape" && open) close(); });

    function size() {
      if (open) {
        const w = window.innerWidth, h = window.innerHeight;
        renderer.setSize(w, h, false);
        camera.aspect = w / h;
      } else {
        const w = canvas.clientWidth || 360;
        renderer.setSize(w, w, false);
        camera.aspect = 1;
      }
      camera.updateProjectionMatrix();
    }
    function clear() {
      for (const child of [...spin.children]) {
        spin.remove(child);
        if (child.geometry) child.geometry.dispose();
        if (child.material) child.material.dispose();
      }
    }
    function cloud(points, size, lit) {
      const pos = new Float32Array(points.length * 3);
      const col = new Float32Array(points.length * 3);
      points.forEach((p, i) => {
        pos.set([p[0], p[1], p[2]], i * 3);
        const a = p[7];
        if (lit) {
          col.set([(p[4] / 255) * a, (p[5] / 255) * a, (p[6] / 255) * a], i * 3);
        } else {
          const grey = ((p[4] + p[5] + p[6]) / 765) * a * 0.32;
          col.set([grey, grey, grey * 1.15], i * 3);
        }
      });
      const geometry = new THREE.BufferGeometry();
      geometry.setAttribute("position", new THREE.BufferAttribute(pos, 3));
      geometry.setAttribute("color", new THREE.BufferAttribute(col, 3));
      const material = new THREE.PointsMaterial({ size, map: dot, vertexColors: true, transparent: true, depthWrite: false, blending: THREE.AdditiveBlending, sizeAttenuation: true });
      return new THREE.Points(geometry, material);
    }
    function loop() {
      if (!$("galaxies").hidden) {
        if (velocity) {
          rotate(velocity[0], velocity[1]);
          velocity = [velocity[0] * 0.95, velocity[1] * 0.95];
          if (Math.hypot(velocity[0], velocity[1]) < 0.05) velocity = null;
        }
        if (!reduced && !fingers.size) spin.rotation.y += 0.0022;
        camera.position.z += (zoom - camera.position.z) * 0.2;
        renderer.render(scene, camera);
      }
      requestAnimationFrame(loop);
    }
    camera.position.z = zoom;
    $("spaceOpen").hidden = false;
    window.addEventListener("resize", size);
    requestAnimationFrame(loop);
    return {
      set(g) {
        clear();
        const points = points3d(g);
        spin.add(cloud(points.filter((p) => p[3] < 1.1), 0.011, g.open));
        spin.add(cloud(points.filter((p) => p[3] >= 1.1), 0.02, g.open));
        const glow = new THREE.Sprite(new THREE.SpriteMaterial({ map: dot, color: g.open ? 0xffe2b8 : 0x6a6a88, transparent: true, opacity: g.open ? 0.55 : 0.25, blending: THREE.AdditiveBlending, depthWrite: false }));
        glow.scale.set(0.6, 0.6, 1);
        spin.add(glow);
        tilt.rotation.x = galaxyTilt[g.kind] !== undefined ? galaxyTilt[g.kind] : 1;
        turn.quaternion.identity();
        velocity = null;
        spin.rotation.y = (g.id * 1.3) % (Math.PI * 2);
        $("spaceName").textContent = g.name;
        size();
      },
    };
  })();

  const galaxy = galaxy3d || flatGalaxy();

  // crystal3d is the shop's carousel: each pack a faceted shape of its own
  // colour, more faces the bigger it is, the one on view large over a ring,
  // its neighbours small at the sides; a swipe moves it.
  const crystal3d = (function () {
    const canvas = $("crystal");
    const renderer = webgl(canvas);
    if (!renderer) return null;
    const scene = new THREE.Scene();
    const camera = new THREE.PerspectiveCamera(40, 1, 0.01, 10);
    camera.position.set(0, 0.16, 1.75);
    camera.lookAt(0, -0.04, 0);
    scene.add(new THREE.AmbientLight(0xffffff, 0.3));
    const key = new THREE.PointLight(0xffffff, 1.3);
    key.position.set(1.2, 1.4, 2);
    scene.add(key);
    const rim = new THREE.DirectionalLight(0xffffff, 0.9);
    rim.position.set(-0.5, 1.5, -1.5);
    scene.add(rim);
    const tint = new THREE.PointLight(0xffffff, 1.1);
    tint.position.set(-1.4, -0.6, 1);
    scene.add(tint);
    const dot = glowTexture();
    const shapes = [
      () => new THREE.OctahedronGeometry(0.3).scale(0.8, 1.45, 0.8),
      () => new THREE.TetrahedronGeometry(0.36),
      () => new THREE.OctahedronGeometry(0.36),
      () => new THREE.BoxGeometry(0.42, 0.42, 0.42),
      () => new THREE.DodecahedronGeometry(0.35),
      () => new THREE.IcosahedronGeometry(0.36),
      () => new THREE.TorusGeometry(0.25, 0.1, 12, 36),
      () => new THREE.IcosahedronGeometry(0.37, 1),
    ];
    function gem(i, color) {
      const geometry = shapes[i % shapes.length]();
      const mesh = new THREE.Mesh(geometry, new THREE.MeshStandardMaterial({ color, emissive: color, emissiveIntensity: 0.22, metalness: 0.55, roughness: 0.12, flatShading: true, transparent: true, opacity: 0.95 }));
      mesh.add(new THREE.LineSegments(new THREE.EdgesGeometry(geometry, 20), new THREE.LineBasicMaterial({ color: 0xffffff, transparent: true, opacity: 0.3 })));
      return mesh;
    }
    // The ring the pack on view stands over, and its glow.
    const pedestal = new THREE.Group();
    const ring = new THREE.Mesh(new THREE.TorusGeometry(0.34, 0.01, 8, 80), new THREE.MeshBasicMaterial({ color: 0xffffff, transparent: true, opacity: 0.7 }));
    ring.rotation.x = Math.PI / 2;
    pedestal.add(ring);
    const outer = new THREE.Mesh(new THREE.TorusGeometry(0.48, 0.004, 6, 90), new THREE.MeshBasicMaterial({ color: 0xffffff, transparent: true, opacity: 0.25 }));
    outer.rotation.x = Math.PI / 2;
    pedestal.add(outer);
    const pool = new THREE.Sprite(new THREE.SpriteMaterial({ map: dot, transparent: true, opacity: 0.35, blending: THREE.AdditiveBlending, depthWrite: false }));
    pool.scale.set(0.95, 0.3, 1);
    pedestal.add(pool);
    pedestal.position.y = -0.36;
    scene.add(pedestal);
    const halo = new THREE.Sprite(new THREE.SpriteMaterial({ map: dot, transparent: true, opacity: 0.4, blending: THREE.AdditiveBlending, depthWrite: false }));
    halo.scale.set(1.3, 1.3, 1);
    scene.add(halo);

    let items = [], current = 0, slide = 0, spin = 0, onSwipe = null, onTap = null;
    // A swipe moves the carousel; a small drag only turns the shape.
    let start = null;
    canvas.addEventListener("pointerdown", (e) => { start = [e.clientX, e.clientY, spin]; canvas.setPointerCapture(e.pointerId); });
    canvas.addEventListener("pointermove", (e) => { if (start) spin = start[2] + (e.clientX - start[0]) * 0.01; });
    const end = (e) => {
      if (!start) return;
      const dx = e.clientX - start[0], dy = e.clientY - start[1];
      start = null;
      if (Math.abs(dx) > 45 && onSwipe) onSwipe(dx < 0 ? 1 : -1);
      else if (Math.abs(dx) < 8 && Math.abs(dy) < 8 && onTap) onTap();
    };
    canvas.addEventListener("pointerup", end);
    canvas.addEventListener("pointercancel", () => { start = null; });

    function size() {
      const w = canvas.clientWidth || 320;
      renderer.setSize(w, w, false);
    }
    function build(count) {
      for (const item of items) {
        scene.remove(item);
        item.traverse((o) => { if (o.geometry) o.geometry.dispose(); if (o.material) o.material.dispose(); });
      }
      items = [];
      for (let i = 0; i < count; i++) {
        const item = gem(i, new THREE.Color(TIERS[i % TIERS.length]));
        scene.add(item);
        items.push(item);
      }
    }
    function loop() {
      if (!$("shop").hidden && items.length) {
        slide += (current - slide) * 0.14;
        const t = performance.now() / 1000;
        items.forEach((item, i) => {
          const off = i - slide;
          const near = Math.abs(off);
          item.visible = near < 1.6;
          item.position.set(off * 0.6, near < 0.5 ? (reduced ? 0 : Math.sin(t * 1.4) * 0.025) : -0.05, -near * 0.5);
          item.scale.setScalar(Math.max(0.32, 1 - near * 0.6));
          item.material.opacity = Math.max(0.35, 0.95 - near * 0.5);
          if (!reduced) item.rotation.y += near < 0.5 ? 0.008 : 0.003;
          item.rotation.x = 0.3;
        });
        if (!reduced) { ring.rotation.z += 0.004; outer.rotation.z -= 0.002; }
        renderer.render(scene, camera);
      }
      requestAnimationFrame(loop);
    }
    window.addEventListener("resize", size);
    requestAnimationFrame(loop);
    let spinApplied = 0;
    setInterval(() => {
      // A drag adds its turn to the shape on view.
      if (items[current] && spin !== spinApplied) {
        items[current].rotation.y += spin - spinApplied;
        spinApplied = spin;
      }
    }, 16);
    return {
      swipe(fn) { onSwipe = fn; },
      tap(fn) { onTap = fn; },
      set(i, hex, count) {
        if (items.length !== count) {
          build(count);
          slide = i;
        }
        current = i;
        const color = new THREE.Color(hex);
        tint.color = color;
        ring.material.color = color;
        outer.material.color = color;
        pool.material.color = color;
        halo.material.color = color;
        size();
      },
    };
  })();

  // sky is the starfield behind the page.
  (function sky() {
    const canvas = $("sky"), ctx = canvas.getContext("2d");
    const r = rng(42);
    function paint() {
      const dpr = Math.min(window.devicePixelRatio || 1, 2);
      canvas.width = innerWidth * dpr;
      canvas.height = innerHeight * dpr;
      ctx.fillStyle = "#05060d";
      ctx.fillRect(0, 0, canvas.width, canvas.height);
      const haze = ctx.createRadialGradient(canvas.width * 0.8, canvas.height * 0.1, 0, canvas.width * 0.8, canvas.height * 0.1, canvas.width);
      haze.addColorStop(0, "rgba(90,70,170,0.22)");
      haze.addColorStop(1, "rgba(0,0,0,0)");
      ctx.fillStyle = haze;
      ctx.fillRect(0, 0, canvas.width, canvas.height);
      for (let i = 0; i < 260; i++) {
        const a = 0.2 + r() * 0.7;
        ctx.fillStyle = `rgba(230,235,255,${a})`;
        const s = (r() < 0.9 ? 1 : 2) * dpr;
        ctx.fillRect(r() * canvas.width, r() * canvas.height, s, s);
      }
    }
    paint();
    window.addEventListener("resize", paint);
  })();

  if (crystal3d) {
    crystal3d.swipe((by) => showPack(packIndex + by));
    // A tap on the crystal on view buys its pack.
    crystal3d.tap(() => buy(data.packs[packIndex], $("crystal")));
  }
  load(false);
})();
