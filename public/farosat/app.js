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
    if (!keep && params.get("tab") === "shop") tab(true);
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
    (galaxy3d || galaxy).set(g);
  }

  $("prev").addEventListener("click", () => show(index - 1));
  $("next").addEventListener("click", () => show(index + 1));
  // A swipe changes the galaxy on the flat drawing; in 3D a drag turns it.
  let touchX = null;
  $("galaxy").addEventListener("touchstart", (e) => { touchX = e.touches[0].clientX; }, { passive: true });
  $("galaxy").addEventListener("touchend", (e) => {
    if (touchX === null || galaxy3d) return;
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
    packIndex = (i + packs.length) % packs.length;
    const p = packs[packIndex];
    const color = TIERS[packIndex % TIERS.length];
    $("shop").style.setProperty("--tier", color);
    $("packTier").textContent = p.name;
    $("packGrams").textContent = fmt(p.stars);
    $("buy").textContent = `Sotib olish · ⭐ ${fmt(p.stars)}`;
    const dots = $("packDots");
    dots.textContent = "";
    packs.forEach((_, j) => {
      const dot = document.createElement("span");
      if (j === packIndex) dot.className = "on";
      dots.appendChild(dot);
    });
    crystal3d.set(packIndex, color);
  }
  $("packPrev").addEventListener("click", () => showPack(packIndex - 1));
  $("packNext").addEventListener("click", () => showPack(packIndex + 1));
  $("buy").addEventListener("click", () => buy(data.packs[packIndex], $("buy")));

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

  // ---- 3D -----------------------------------------------------------------

  // TIERS colour the packs, the smallest first.
  const TIERS = ["#8fd3ff", "#9b8cff", "#5ef2c2", "#ffd36b"];

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

  // drag turns an object by pointer: x around its axis, y its tilt.
  function drag(canvas, onMove) {
    let last = null;
    canvas.addEventListener("pointerdown", (e) => { last = [e.clientX, e.clientY]; canvas.setPointerCapture(e.pointerId); });
    canvas.addEventListener("pointermove", (e) => {
      if (!last) return;
      onMove(e.clientX - last[0], e.clientY - last[1]);
      last = [e.clientX, e.clientY];
    });
    const stop = () => { last = null; };
    canvas.addEventListener("pointerup", stop);
    canvas.addEventListener("pointercancel", stop);
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

  const galaxy3d = (function () {
    const canvas = $("galaxy");
    const renderer = webgl(canvas);
    if (!renderer) return null;
    const scene = new THREE.Scene();
    const camera = new THREE.PerspectiveCamera(45, 1, 0.01, 10);
    camera.position.set(0, 0, 1.3);
    const tilt = new THREE.Group();
    const spin = new THREE.Group();
    tilt.add(spin);
    scene.add(tilt);
    const dot = glowTexture();
    let baseTilt = 1, userTilt = 0;
    drag(canvas, (dx, dy) => {
      spin.rotation.y += dx * 0.008;
      userTilt = Math.max(-0.7, Math.min(0.7, userTilt + dy * 0.006));
    });

    function size() {
      const w = canvas.clientWidth || 360;
      renderer.setSize(w, w, false);
    }
    function clear() {
      for (const child of [...spin.children]) {
        spin.remove(child);
        if (child.geometry) child.geometry.dispose();
        if (child.material) child.material.dispose();
      }
    }
    function cloud(points, size, open) {
      const pos = new Float32Array(points.length * 3);
      const col = new Float32Array(points.length * 3);
      points.forEach((p, i) => {
        pos.set([p[0], p[1], p[2]], i * 3);
        const a = p[7];
        if (open) {
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
        if (!reduced) spin.rotation.y += 0.0022;
        tilt.rotation.x = baseTilt + userTilt;
        renderer.render(scene, camera);
      }
      requestAnimationFrame(loop);
    }
    window.addEventListener("resize", size);
    requestAnimationFrame(loop);
    return {
      set(g) {
        clear();
        const points = points3d(g);
        spin.add(cloud(points.filter((p) => p[3] < 1.1), 0.011, g.open));
        spin.add(cloud(points.filter((p) => p[3] >= 1.1), 0.02, g.open));
        const glow = new THREE.Sprite(new THREE.SpriteMaterial({ map: dot, color: g.open ? 0xffe2b8 : 0x6a6a88, transparent: true, opacity: g.open ? 0.55 : 0.25, blending: THREE.AdditiveBlending, depthWrite: false }));
        glow.scale.set(0.45, 0.45, 1);
        spin.add(glow);
        baseTilt = galaxyTilt[g.kind] !== undefined ? galaxyTilt[g.kind] : 1;
        userTilt = 0;
        spin.rotation.y = (g.id * 1.3) % (Math.PI * 2);
        size();
      },
    };
  })();

  // crystal3d is the shop's pack on view: a faceted crystal, more faces
  // the bigger the pack, with its glow and a ring of sparks.
  const crystal3d = (function () {
    const canvas = $("crystal");
    const renderer = webgl(canvas);
    if (!renderer) return null;
    const scene = new THREE.Scene();
    const camera = new THREE.PerspectiveCamera(40, 1, 0.01, 10);
    camera.position.set(0, 0.1, 1.6);
    scene.add(new THREE.AmbientLight(0xffffff, 0.3));
    const rim = new THREE.DirectionalLight(0xffffff, 0.9);
    rim.position.set(-0.5, 1.5, -1.5);
    scene.add(rim);
    const key = new THREE.PointLight(0xffffff, 1.3);
    key.position.set(1.2, 1.4, 2);
    scene.add(key);
    const tint = new THREE.PointLight(0xffffff, 1.1);
    tint.position.set(-1.4, -0.6, 1);
    scene.add(tint);
    const holder = new THREE.Group();
    scene.add(holder);
    const dot = glowTexture();
    const shapes = [
      () => new THREE.OctahedronGeometry(0.3).scale(0.8, 1.45, 0.8),
      () => new THREE.OctahedronGeometry(0.36),
      () => new THREE.DodecahedronGeometry(0.36),
      () => new THREE.IcosahedronGeometry(0.37, 1),
    ];
    let grow = 1, tilt = 0;
    drag(canvas, (dx, dy) => {
      holder.rotation.y += dx * 0.01;
      tilt = Math.max(-0.8, Math.min(0.8, tilt + dy * 0.006));
    });
    function size() {
      const w = canvas.clientWidth || 320;
      renderer.setSize(w, w, false);
    }
    function clear() {
      for (const child of [...holder.children]) {
        holder.remove(child);
        child.traverse((o) => { if (o.geometry) o.geometry.dispose(); if (o.material) o.material.dispose(); });
      }
    }
    let gem = null, ring = null;
    function loop() {
      if (!$("shop").hidden) {
        grow += (1 - grow) * 0.12;
        holder.scale.setScalar(grow);
        if (!reduced) {
          holder.rotation.y += 0.008;
          if (ring) ring.rotation.z -= 0.004;
        }
        holder.rotation.x = 0.35 + tilt;
        if (gem) gem.position.y = reduced ? 0 : Math.sin(performance.now() / 700) * 0.025;
        renderer.render(scene, camera);
      }
      requestAnimationFrame(loop);
    }
    window.addEventListener("resize", size);
    requestAnimationFrame(loop);
    return {
      set(i, hex) {
        clear();
        const color = new THREE.Color(hex);
        tint.color = color;
        const geometry = shapes[i % shapes.length]();
        gem = new THREE.Mesh(geometry, new THREE.MeshStandardMaterial({ color, emissive: color, emissiveIntensity: 0.22, metalness: 0.55, roughness: 0.12, flatShading: true, transparent: true, opacity: 0.94 }));
        gem.add(new THREE.LineSegments(new THREE.EdgesGeometry(geometry), new THREE.LineBasicMaterial({ color: 0xffffff, transparent: true, opacity: 0.35 })));
        holder.add(gem);
        const halo = new THREE.Sprite(new THREE.SpriteMaterial({ map: dot, color, transparent: true, opacity: 0.45, blending: THREE.AdditiveBlending, depthWrite: false }));
        halo.scale.set(1.3, 1.3, 1);
        holder.add(halo);
        const r = rng(i + 3);
        const count = 120 + i * 90;
        const pos = new Float32Array(count * 3);
        for (let k = 0; k < count; k++) {
          const ang = r() * Math.PI * 2, rad = 0.5 + gauss(r) * 0.03;
          pos.set([Math.cos(ang) * rad, gauss(r) * 0.015, Math.sin(ang) * rad], k * 3);
        }
        const sparks = new THREE.BufferGeometry();
        sparks.setAttribute("position", new THREE.BufferAttribute(pos, 3));
        ring = new THREE.Points(sparks, new THREE.PointsMaterial({ size: 0.022, map: dot, color, transparent: true, depthWrite: false, blending: THREE.AdditiveBlending }));
        ring.rotation.x = Math.PI / 2 - 0.25;
        holder.add(ring);
        grow = 0.3;
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

  load(false);
})();
