"use strict";
(() => {
  const tg = window.Telegram && window.Telegram.WebApp;
  // A Mini App has init data; a press of Play on the "kassa" game opens the
  // page in Telegram's browser with the bot's session in ?s= instead, and
  // without Telegram's back button or popups.
  const session = new URLSearchParams(location.search).get("s") || "";
  const inApp = !!(tg && tg.initData);
  const auth = inApp ? { init_data: tg.initData } : { session };
  const back = {
    show() { if (inApp) back.show(); else $("backBtn").hidden = false; },
    hide() { if (inApp) back.hide(); else $("backBtn").hidden = true; },
  };
  const confirmBox = (text, done) => {
    if (inApp && tg.showConfirm) tg.showConfirm(text, done);
    else done(window.confirm(text));
  };
  const lang = ((tg && tg.initDataUnsafe && tg.initDataUnsafe.user && tg.initDataUnsafe.user.language_code) || "uz").slice(0, 2) === "ru" ? "ru" : "uz";
  const T = {
    uz: {
      balance: "Qoldiq", in: "Kirim", out: "Chiqim", inHint: "Pul tushdi", backText: "Orqaga", outHint: "Pul ishlatildi", recent: "So'nggi yozuvlar",
      date: "Sana", note: "Ma'lumot", save: "Saqlash", saving: "Saqlanmoqda…", today: "Bugun", yesterday: "Kecha", otherDay: "Boshqa sana",
      addIn: "Kirim qo'shish", addOut: "Chiqim qo'shish", amountIn: "Qancha pul tushdi", amountOut: "Qancha ishlatildi",
      totalIn: "Jami kirim", totalOut: "Jami chiqim", count: (n) => `${n} ta yozuv`, noNote: "Izohsiz",
      emptyAll: ["Hali yozuv yo'q", "Kirim yoki chiqim qo'shing, qoldiq o'zi hisoblanadi"],
      emptyIn: ["Kirim yo'q", "Pastdagi tugma bilan birinchi kirimni qo'shing"],
      emptyOut: ["Chiqim yo'q", "Pastdagi tugma bilan birinchi chiqimni qo'shing"],
      left: (s) => `Qoldiq: ${s}`, over: (s) => `Qoldiqdan ${s} ko'p, baribir saqlash mumkin`,
      saved: "Saqlandi", deleted: "O'chirildi", del: "Bu yozuv o'chirilsinmi?", delLabel: "O'chirish",
      months: ["yanvar", "fevral", "mart", "aprel", "may", "iyun", "iyul", "avgust", "sentabr", "oktabr", "noyabr", "dekabr"],
      openInTg: "Kassa Telegram ichida ochiladi: botda /kassa yozing.",
      err: { amount: "Summani kiriting", day: "Sanani to'g'ri tanlang", note: "Ma'lumot juda uzun", stale: "Sahifa eskirdi, qayta oching", forged: "Kassani bot orqali oching", not_found: "Yozuv topilmadi", net: "Internet yoki server xatosi, qayta urinib ko'ring" },
    },
    ru: {
      balance: "Остаток", in: "Приход", out: "Расход", inHint: "Деньги пришли", backText: "Назад", outHint: "Деньги потрачены", recent: "Последние записи",
      date: "Дата", note: "Описание", save: "Сохранить", saving: "Сохраняем…", today: "Сегодня", yesterday: "Вчера", otherDay: "Другая дата",
      addIn: "Добавить приход", addOut: "Добавить расход", amountIn: "Сколько пришло", amountOut: "Сколько потрачено",
      totalIn: "Всего приход", totalOut: "Всего расход", count: (n) => `записей: ${n}`, noNote: "Без описания",
      emptyAll: ["Записей пока нет", "Добавьте приход или расход, остаток посчитается сам"],
      emptyIn: ["Приходов нет", "Добавьте первый приход кнопкой внизу"],
      emptyOut: ["Расходов нет", "Добавьте первый расход кнопкой внизу"],
      left: (s) => `Остаток: ${s}`, over: (s) => `Больше остатка на ${s}, сохранить всё равно можно`,
      saved: "Сохранено", deleted: "Удалено", del: "Удалить эту запись?", delLabel: "Удалить",
      months: ["января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"],
      openInTg: "Касса открывается в Telegram: напишите боту /kassa.",
      err: { amount: "Введите сумму", day: "Выберите дату правильно", note: "Описание слишком длинное", stale: "Страница устарела, откройте заново", forged: "Откройте кассу через бота", not_found: "Запись не найдена", net: "Ошибка сети или сервера, попробуйте ещё раз" },
    },
  }[lang];

  const $ = (id) => document.getElementById(id);
  const spaced = (n) => Math.abs(n).toString().replace(/\B(?=(\d{3})+(?!\d))/g, " ");
  const money = (n) => (n < 0 ? "−" : "") + spaced(n) + " so'm";
  const haptic = (kind) => { try { if (inApp) tg.HapticFeedback.notificationOccurred(kind); } catch (_) {} };
  const shiftDay = (s, days) => { const d = new Date(s + "T00:00:00Z"); d.setUTCDate(d.getUTCDate() - days); return d.toISOString().slice(0, 10); };

  let data = null;
  let kind = null; // the open book, "in" or "out"; null on the home screen
  let sheetKind = null; // the kind the open sheet adds; null when it is closed

  document.documentElement.lang = lang;
  document.querySelectorAll("[data-t]").forEach((el) => { if (typeof T[el.dataset.t] === "string") el.textContent = T[el.dataset.t]; });

  function dayName(s) {
    if (s === data.today) return T.today;
    if (s === shiftDay(data.today, 1)) return T.yesterday;
    const [y, m, d] = s.split("-").map(Number);
    const thisYear = data.today.slice(0, 4) === String(y);
    const name = lang === "ru" ? `${d} ${T.months[m - 1]}` : `${d}-${T.months[m - 1]}`;
    return thisYear ? name : `${name} ${y}`;
  }

  function toast(text) {
    const el = $("toast");
    el.textContent = text;
    el.hidden = false;
    clearTimeout(toast.timer);
    toast.timer = setTimeout(() => { el.hidden = true; }, 2200);
  }

  async function call(action, extra) {
    let res;
    try {
      res = await fetch("/api/kassa", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ ...auth, action, ...extra }) });
    } catch (_) { throw new Error("net"); }
    const body = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(body.error || "net");
    return body;
  }

  function fail(err) {
    toast(T.err[err && err.message] || T.err.net);
    haptic("error");
  }

  function svg(id, cls) {
    const s = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    s.setAttribute("class", "ic" + (cls ? " " + cls : ""));
    s.setAttribute("aria-hidden", "true");
    const u = document.createElementNS("http://www.w3.org/2000/svg", "use");
    u.setAttribute("href", "#" + id);
    s.append(u);
    return s;
  }

  function el(tag, cls, text) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  function emptyState(k) {
    const [title, hint] = k === "in" ? T.emptyIn : k === "out" ? T.emptyOut : T.emptyAll;
    const box = el("div", "empty");
    const badge = el("span", "badge " + (k || "in"));
    badge.append(svg(k === "out" ? "i-out" : "i-in"));
    box.append(badge, el("b", "", title), el("span", "", hint));
    return box;
  }

  // entryList draws entries grouped by day, newest first, each day with
  // its sum; mixed is the home screen's list of both kinds.
  function entryList(entries, mixed) {
    const frag = document.createDocumentFragment();
    if (mixed) {
      // The home screen's few latest entries fit one card, each with its day.
      const rows = el("div", "rows");
      for (const e of entries) rows.append(entryRow(e, `${e.kind === "in" ? T.in : T.out} · ${dayName(e.day)}`));
      frag.append(rows);
      return frag;
    }
    let rows = null;
    let day = null;
    let head = null;
    let daySum = 0;
    const closeDay = () => { if (head) head.querySelector("span").textContent = money(daySum); };
    for (const e of entries) {
      if (e.day !== day) {
        closeDay();
        day = e.day;
        daySum = 0;
        const group = el("div", "group");
        head = el("div", "group-head");
        head.append(el("b", "", dayName(day)), el("span", "num", ""));
        rows = el("div", "rows");
        group.append(head, rows);
        frag.append(group);
      }
      daySum += e.amount;
      rows.append(entryRow(e, ""));
    }
    closeDay();
    return frag;
  }

  function entryRow(e, meta) {
    const row = el("div", "row");
    const badge = el("span", "badge " + e.kind);
    badge.append(svg(e.kind === "in" ? "i-in" : "i-out"));
    const text = el("span", "row-text");
    text.append(el("span", "row-note" + (e.note ? "" : " none"), e.note || T.noNote));
    if (meta) text.append(el("span", "row-meta", meta));
    const sum = el("span", "row-sum " + e.kind, (e.kind === "in" ? "+" : "−") + spaced(e.amount));
    const del = el("button", "del");
    del.type = "button";
    del.setAttribute("aria-label", T.delLabel);
    del.append(svg("i-trash"));
    del.addEventListener("click", () => remove(e.id));
    row.append(badge, text, sum, del);
    return row;
  }

  function withKind(list, k) { return list.map((e) => ({ ...e, kind: k })); }

  function render() {
    const tot = data.totals;
    $("homeBalance").textContent = money(tot.balance);
    $("homeIn").textContent = money(tot.in);
    $("homeOut").textContent = money(tot.out);
    $("home").hidden = kind !== null;
    $("book").hidden = kind === null;
    $("bar").hidden = kind === null;

    if (kind === null) {
      if (!sheetKind) back.hide();
      const recent = withKind(data.in, "in").concat(withKind(data.out, "out"))
        .sort((a, b) => (a.day === b.day ? b.id - a.id : a.day < b.day ? 1 : -1)).slice(0, 8);
      $("recent").replaceChildren(recent.length ? entryList(recent, true) : emptyState(null));
      return;
    }
    back.show();
    const items = withKind(data[kind], kind);
    const sumEl = $("bookSum");
    if (kind === "out") {
      $("bookLabel").textContent = T.balance;
      sumEl.textContent = money(tot.balance);
      sumEl.classList.toggle("neg", tot.balance < 0);
      $("bookSub").textContent = `${T.totalOut}: ${money(tot.out)}`;
    } else {
      $("bookLabel").textContent = T.totalIn;
      sumEl.textContent = money(tot.in);
      sumEl.classList.remove("neg");
      $("bookSub").textContent = T.count(items.length);
    }
    $("addText").textContent = kind === "out" ? T.addOut : T.addIn;
    $("addBtn").className = "primary " + kind;
    $("entries").replaceChildren(items.length ? entryList(items, false) : emptyState(kind));
  }

  function openBook(k) {
    kind = k;
    render();
    window.scrollTo(0, 0);
  }

  // The sheet.
  function setDay(value) {
    $("day").value = value;
    const preset = value === data.today ? "0" : value === shiftDay(data.today, 1) ? "1" : null;
    document.querySelectorAll(".chip[data-day]").forEach((c) => c.setAttribute("aria-pressed", String(c.dataset.day === preset)));
    const dateChip = document.querySelector(".date-chip");
    dateChip.setAttribute("aria-pressed", String(preset === null));
    $("dayText").textContent = preset === null ? dayName(value) : T.otherDay;
  }

  function amountValue() { return Number($("amount").value.replace(/\D/g, "")) || 0; }

  function updateHelp() {
    const help = $("amountHelp");
    if (sheetKind !== "out") { help.textContent = ""; help.classList.remove("warn"); return; }
    const left = data.totals.balance - amountValue();
    help.classList.toggle("warn", left < 0);
    help.textContent = left < 0 ? T.over(money(-left)) : T.left(money(left));
  }

  function openSheet() {
    sheetKind = kind;
    $("sheetTitle").textContent = kind === "out" ? T.addOut : T.addIn;
    $("amountLabel").textContent = kind === "out" ? T.amountOut : T.amountIn;
    $("save").className = "primary " + kind;
    $("amount").value = "";
    $("note").value = "";
    $("day").max = data.today;
    setDay(data.today);
    updateHelp();
    $("scrim").hidden = false;
    $("sheet").hidden = false;
    $("bar").hidden = true;
    back.show();
    setTimeout(() => $("amount").focus(), 60);
  }

  function closeSheet() {
    sheetKind = null;
    $("scrim").hidden = true;
    $("sheet").hidden = true;
    document.activeElement && document.activeElement.blur();
    render();
  }

  function remove(id) {
    confirmBox(T.del, async (ok) => {
      if (!ok) return;
      try {
        data = await call("delete", { id });
        render();
        toast(T.deleted);
        haptic("success");
      } catch (err) { fail(err); }
    });
  }

  $("amount").addEventListener("input", (ev) => {
    const digits = ev.target.value.replace(/\D/g, "").replace(/^0+/, "").slice(0, 13);
    ev.target.value = digits.replace(/\B(?=(\d{3})+(?!\d))/g, " ");
    updateHelp();
  });
  $("amount").addEventListener("keydown", (ev) => { if (ev.key === "Enter") { ev.preventDefault(); $("note").focus(); } });
  document.querySelectorAll(".chip[data-day]").forEach((c) => c.addEventListener("click", () => setDay(shiftDay(data.today, Number(c.dataset.day)))));
  $("day").addEventListener("change", (ev) => { if (ev.target.value) setDay(ev.target.value > data.today ? data.today : ev.target.value); });
  $("scrim").addEventListener("click", closeSheet);
  $("addBtn").addEventListener("click", openSheet);
  document.querySelectorAll("[data-open]").forEach((b) => b.addEventListener("click", () => openBook(b.dataset.open)));

  $("sheet").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const amount = amountValue();
    if (!amount) { toast(T.err.amount); haptic("error"); $("amount").focus(); return; }
    const save = $("save");
    save.disabled = true;
    save.firstElementChild.textContent = T.saving;
    try {
      data = await call("add", { kind: sheetKind, amount, note: $("note").value, day: $("day").value });
      closeSheet();
      toast(T.saved);
      haptic("success");
    } catch (err) { fail(err); } finally {
      save.disabled = false;
      save.firstElementChild.textContent = T.save;
    }
  });

  async function start() {
    const state = $("state");
    if (!inApp && !session) {
      $("skeleton").hidden = true;
      state.textContent = T.openInTg;
      state.hidden = false;
      return;
    }
    const goBack = () => {
      if (sheetKind) { closeSheet(); return; }
      kind = null;
      render();
    };
    if (inApp) {
      tg.ready();
      tg.expand();
      tg.BackButton.onClick(goBack);
    }
    $("backBtn").addEventListener("click", goBack);
    try {
      data = await call("state");
      $("skeleton").hidden = true;
      render();
    } catch (err) {
      $("skeleton").hidden = true;
      state.textContent = T.err[err.message] || T.err.net;
      state.hidden = false;
    }
  }
  start();
})();
