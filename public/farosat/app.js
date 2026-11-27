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
  let index = 0;

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
    if (!keep) index = Math.max(0, data.open - 1);
    $("tally").innerHTML = `🪐 <b>${data.open}</b> ta koinotingizda · 🧠 <b>${fmt(data.grams)}</b> gram · <b>${data.place}</b>-o'rin`;
    renderPacks();
    show(index);
  }

  // ---- The galaxy on view ----------------------------------------------

  function show(i) {
    const list = data.galaxies;
    index = Math.min(Math.max(i, 0), list.length - 1);
    const g = list[index];
    $("meta").textContent = `#${g.id} / ${list.length} · ${fmt(g.grams)} gram`;
    const chip = $("chip");
    chip.textContent = g.open ? "Ochilgan" : "Yetib borilmagan";
    chip.classList.toggle("locked", !g.open);
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
    }
    $("prev").disabled = index === 0;
    $("next").disabled = index === list.length - 1;
    galaxy.set(g);
  }

  $("prev").addEventListener("click", () => show(index - 1));
  $("next").addEventListener("click", () => show(index + 1));
  let touchX = null;
  $("galaxy").addEventListener("touchstart", (e) => { touchX = e.touches[0].clientX; }, { passive: true });
  $("galaxy").addEventListener("touchend", (e) => {
    if (touchX === null) return;
    const dx = e.changedTouches[0].clientX - touchX;
    touchX = null;
    if (Math.abs(dx) > 40) show(index + (dx < 0 ? 1 : -1));
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

  function renderPacks() {
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

  async function buy(pack, button) {
    button.disabled = true;
    try {
      const { link } = await api("invoice", { pack: pack.id });
      if (!tg || !tg.openInvoice) { location.href = link; return; }
      tg.openInvoice(link, (status) => {
        button.disabled = false;
        if (status === "paid") {
          if (tg.HapticFeedback) tg.HapticFeedback.notificationOccurred("success");
          // The grams come with Telegram's word to the bot: look again
          // a few times, until they are there.
          const before = data.grams;
          let tries = 0;
          const again = async () => {
            await load(true);
            if (data.grams === before && ++tries < 5) setTimeout(again, 1500);
          };
          setTimeout(again, 1200);
        }
      });
    } catch (e) {
      button.disabled = false;
      if (tg && tg.showAlert) tg.showAlert(errors[e.message] || "To'lovni ochib bo'lmadi, qayta urinib ko'ring.");
    }
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
      bulge(900, 0.055, core);
      if (barLen) for (let i = 0; i < 700; i++) push((r() - 0.5) * barLen, gauss(r) * 0.025, 0.7 + r(), gold, 0.6);
      for (let a = 0; a < arms; a++) {
        const base = (a / arms) * Math.PI * 2;
        for (let i = 0; i < 1500; i++) {
          const t = Math.pow(r(), 0.8);
          const rad = (barLen ? barLen / 2 : 0.06) + t * 0.42;
          const ang = base + t * twist + gauss(r) * 0.09;
          const jitter = gauss(r) * 0.012 * (1 + t);
          const x = Math.cos(ang) * rad + jitter, y = Math.sin(ang) * rad + jitter;
          const c = r() < 0.05 ? pink : (r() < 0.55 ? blue : white);
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

  // galaxy draws the galaxy on view, turning slowly; a locked one is dim.
  const galaxy = (function () {
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

  load(false);
})();
