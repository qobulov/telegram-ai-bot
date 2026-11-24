"use strict";
(() => {
  const tg = window.Telegram && window.Telegram.WebApp;
  // A Mini App has init data; a press of Play on the "kassa" game opens the
  // page in Telegram's browser with the bot's session in ?s= instead, and
  // without Telegram's back button or popups.
  const session = new URLSearchParams(location.search).get("s") || "";
  const inApp = !!(tg && tg.initData);
  const auth = inApp ? { init_data: tg.initData } : { session };
  const lang = ((tg && tg.initDataUnsafe && tg.initDataUnsafe.user && tg.initDataUnsafe.user.language_code) || "uz").slice(0, 2) === "ru" ? "ru" : "uz";
  const T = {
    uz: {
      balance: "Qoldiq", allBalance: "Jami qoldiq", myBooks: "Kassalarim", in: "Kirim", out: "Chiqim", inHint: "Pul tushdi", outHint: "Pul ishlatildi",
      recent: "So'nggi yozuvlar", date: "Sana", note: "Ma'lumot", save: "Saqlash", saving: "Saqlanmoqda…", today: "Bugun", yesterday: "Kecha", otherDay: "Boshqa sana",
      addIn: "Kirim qo'shish", addOut: "Chiqim qo'shish", amountIn: "Qancha pul tushdi", amountOut: "Qancha ishlatildi",
      totalIn: "Jami kirim", totalOut: "Jami chiqim", count: (n) => `${n} ta yozuv`, noNote: "Izohsiz",
      newBook: "Yangi kassa", bookName: "Kassa nomi", editBook: "Kassa sozlamalari", deleteBook: "Kassani o'chirish",
      delBook: (n) => `"${n}" kassasi va undagi hamma yozuvlar o'chirilsinmi?`, bookSaved: "Kassa saqlandi", bookDeleted: "Kassa o'chirildi",
      nameIdeas: ["Uy", "Do'kon", "Ish", "Sayohat", "Oila"], books: "Kassalar", back: "Orqaga",
      bookMeta: (i, o) => `+${i} · −${o}`,
      emptyAll: ["Hali yozuv yo'q", "Kirim yoki chiqim qo'shing, qoldiq o'zi hisoblanadi"],
      emptyIn: ["Kirim yo'q", "Pastdagi tugma bilan birinchi kirimni qo'shing"],
      emptyOut: ["Chiqim yo'q", "Pastdagi tugma bilan birinchi chiqimni qo'shing"],
      left: (s) => `Qoldiq: ${s}`, over: (s) => `Qoldiqdan ${s} ko'p, baribir saqlash mumkin`,
      saved: "Saqlandi", deleted: "O'chirildi", del: "Bu yozuv o'chirilsinmi?", delLabel: "O'chirish",
      months: ["yanvar", "fevral", "mart", "aprel", "may", "iyun", "iyul", "avgust", "sentabr", "oktabr", "noyabr", "dekabr"],
      openInTg: "Kassa Telegram ichida ochiladi: botda /kassa yozing.",
      err: { amount: "Summani kiriting", day: "Sanani to'g'ri tanlang", note: "Ma'lumot juda uzun", name: "Kassa nomini kiriting (40 belgigacha)", books_full: "Kassalar soni chegarasiga yetdingiz", stale: "Sahifa eskirdi, qayta oching", forged: "Kassani bot orqali oching", not_found: "Topilmadi, sahifani yangilang", net: "Internet yoki server xatosi, qayta urinib ko'ring" },
    },
    ru: {
      balance: "Остаток", allBalance: "Общий остаток", myBooks: "Мои кассы", in: "Приход", out: "Расход", inHint: "Деньги пришли", outHint: "Деньги потрачены",
      recent: "Последние записи", date: "Дата", note: "Описание", save: "Сохранить", saving: "Сохраняем…", today: "Сегодня", yesterday: "Вчера", otherDay: "Другая дата",
      addIn: "Добавить приход", addOut: "Добавить расход", amountIn: "Сколько пришло", amountOut: "Сколько потрачено",
      totalIn: "Всего приход", totalOut: "Всего расход", count: (n) => `записей: ${n}`, noNote: "Без описания",
      newBook: "Новая касса", bookName: "Название кассы", editBook: "Настройки кассы", deleteBook: "Удалить кассу",
      delBook: (n) => `Удалить кассу «${n}» и все её записи?`, bookSaved: "Касса сохранена", bookDeleted: "Касса удалена",
      nameIdeas: ["Дом", "Магазин", "Работа", "Поездка", "Семья"], books: "Кассы", back: "Назад",
      bookMeta: (i, o) => `+${i} · −${o}`,
      emptyAll: ["Записей пока нет", "Добавьте приход или расход, остаток посчитается сам"],
      emptyIn: ["Приходов нет", "Добавьте первый приход кнопкой внизу"],
      emptyOut: ["Расходов нет", "Добавьте первый расход кнопкой внизу"],
      left: (s) => `Остаток: ${s}`, over: (s) => `Больше остатка на ${s}, сохранить всё равно можно`,
      saved: "Сохранено", deleted: "Удалено", del: "Удалить эту запись?", delLabel: "Удалить",
      months: ["января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"],
      openInTg: "Касса открывается в Telegram: напишите боту /kassa.",
      err: { amount: "Введите сумму", day: "Выберите дату правильно", note: "Описание слишком длинное", name: "Введите название кассы (до 40 символов)", books_full: "Достигнут предел числа касс", stale: "Страница устарела, откройте заново", forged: "Откройте кассу через бота", not_found: "Не найдено, обновите страницу", net: "Ошибка сети или сервера, попробуйте ещё раз" },
    },
  }[lang];

  const $ = (id) => document.getElementById(id);
  const spaced = (n) => Math.abs(n).toString().replace(/\B(?=(\d{3})+(?!\d))/g, " ");
  const money = (n) => (n < 0 ? "−" : "") + spaced(n) + " so'm";
  const haptic = (kind) => { try { if (inApp) tg.HapticFeedback.notificationOccurred(kind); } catch (_) {} };
  const shiftDay = (s, days) => { const d = new Date(s + "T00:00:00Z"); d.setUTCDate(d.getUTCDate() - days); return d.toISOString().slice(0, 10); };
  const confirmBox = (text, done) => { if (inApp && tg.showConfirm) tg.showConfirm(text, done); else done(window.confirm(text)); };

  let books = null; // the list of books, with the sums of them all
  let data = null; // the open book: {book, in, out, today}
  let screen = "books"; // "books", "kassa" (one book) or "book" (its money in or out)
  let kind = null; // "in" or "out" on the "book" screen
  let sheet = null; // the open sheet: "entry", "newBook" or "editBook"

  document.documentElement.lang = lang;
  document.querySelectorAll("[data-t]").forEach((el) => { if (typeof T[el.dataset.t] === "string") el.textContent = T[el.dataset.t]; });

  const today = () => (data || books).today;
  function dayName(s) {
    if (s === today()) return T.today;
    if (s === shiftDay(today(), 1)) return T.yesterday;
    const [y, m, d] = s.split("-").map(Number);
    const name = lang === "ru" ? `${d} ${T.months[m - 1]}` : `${d}-${T.months[m - 1]}`;
    return today().slice(0, 4) === String(y) ? name : `${name} ${y}`;
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
    del.addEventListener("click", () => removeEntry(e.id));
    row.append(badge, text, sum, del);
    return row;
  }

  // entryList draws entries grouped by day, newest first, each day with its
  // sum; mixed is the book's few latest entries of both kinds, in one card.
  function entryList(entries, mixed) {
    const frag = document.createDocumentFragment();
    if (mixed) {
      const rows = el("div", "rows");
      for (const e of entries) rows.append(entryRow(e, `${e.kind === "in" ? T.in : T.out} · ${dayName(e.day)}`));
      frag.append(rows);
      return frag;
    }
    let rows = null, day = null, head = null, daySum = 0;
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

  const withKind = (list, k) => list.map((e) => ({ ...e, kind: k }));

  function setBack(visible, label) {
    if (inApp) {
      if (visible) tg.BackButton.show(); else tg.BackButton.hide();
      return;
    }
    $("backBtn").hidden = !visible;
    $("backText").textContent = label || T.back;
  }

  function render() {
    for (const id of ["books", "kassa", "book"]) $(id).hidden = screen !== id;
    const bar = $("bar");
    bar.hidden = sheet !== null || screen === "kassa";
    if (screen === "books") renderBooks();
    else if (screen === "kassa") renderKassa();
    else renderBook();
  }

  function renderBooks() {
    setBack(sheet !== null);
    const t = books.totals;
    $("allBalance").textContent = money(t.balance);
    $("allIn").textContent = money(t.in);
    $("allOut").textContent = money(t.out);
    const list = $("bookList");
    list.replaceChildren();
    for (const b of books.books) {
      const row = el("button", "book-row");
      row.type = "button";
      const badge = el("span", "badge");
      badge.append(svg("i-wallet"));
      const text = el("span", "book-text");
      text.append(el("span", "book-name", b.name), el("span", "book-meta", T.bookMeta(spaced(b.totals.in), spaced(b.totals.out))));
      const sum = el("span", "book-sum" + (b.totals.balance < 0 ? " neg" : ""), money(b.totals.balance));
      row.append(badge, text, sum, svg("i-chev", "chev"));
      row.addEventListener("click", () => openKassa(b.id));
      list.append(row);
    }
    $("addText").textContent = T.newBook;
    $("addBtn").className = "primary";
    $("bar").hidden = sheet !== null || books.books.length >= books.max;
  }

  function renderKassa() {
    setBack(true, T.books);
    const b = data.book, t = b.totals;
    $("kassaName").textContent = b.name;
    $("homeBalance").textContent = money(t.balance);
    $("homeIn").textContent = money(t.in);
    $("homeOut").textContent = money(t.out);
    const recent = withKind(data.in, "in").concat(withKind(data.out, "out"))
      .sort((x, y) => (x.day === y.day ? y.id - x.id : x.day < y.day ? 1 : -1)).slice(0, 8);
    $("recent").replaceChildren(recent.length ? entryList(recent, true) : emptyState(null));
  }

  function renderBook() {
    setBack(true, data.book.name);
    const t = data.book.totals;
    const items = withKind(data[kind], kind);
    const sumEl = $("bookSum");
    if (kind === "out") {
      $("bookLabel").textContent = `${data.book.name} · ${T.balance}`;
      sumEl.textContent = money(t.balance);
      sumEl.classList.toggle("neg", t.balance < 0);
      $("bookSub").textContent = `${T.totalOut}: ${money(t.out)}`;
    } else {
      $("bookLabel").textContent = `${data.book.name} · ${T.totalIn}`;
      sumEl.textContent = money(t.in);
      sumEl.classList.remove("neg");
      $("bookSub").textContent = T.count(items.length);
    }
    $("addText").textContent = kind === "out" ? T.addOut : T.addIn;
    $("addBtn").className = "primary " + kind;
    $("entries").replaceChildren(items.length ? entryList(items, false) : emptyState(kind));
  }

  function show(next) {
    screen = next;
    render();
    window.scrollTo(0, 0);
  }

  async function loadBooks() {
    books = await call("books");
    data = null;
  }

  async function openKassa(id) {
    try {
      data = await call("state", { book: id });
      show("kassa");
    } catch (err) { fail(err); }
  }

  async function goBack() {
    if (sheet) { closeSheet(); return; }
    if (screen === "book") { show("kassa"); return; }
    if (screen === "kassa") {
      try { await loadBooks(); } catch (err) { fail(err); return; }
      show("books");
    }
  }

  // Sheets.
  function openSheet(id, which) {
    sheet = which;
    $("scrim").hidden = false;
    $(id).hidden = false;
    $("bar").hidden = true;
    setBack(true);
  }

  function closeSheet() {
    sheet = null;
    $("scrim").hidden = true;
    $("sheet").hidden = true;
    $("bookSheet").hidden = true;
    if (document.activeElement) document.activeElement.blur();
    render();
  }

  function setDay(value) {
    $("day").value = value;
    const preset = value === today() ? "0" : value === shiftDay(today(), 1) ? "1" : null;
    document.querySelectorAll(".chip[data-day]").forEach((c) => c.setAttribute("aria-pressed", String(c.dataset.day === preset)));
    document.querySelector(".date-chip").setAttribute("aria-pressed", String(preset === null));
    $("dayText").textContent = preset === null ? dayName(value) : T.otherDay;
  }

  const amountValue = () => Number($("amount").value.replace(/\D/g, "")) || 0;

  function updateHelp() {
    const help = $("amountHelp");
    if (kind !== "out") { help.textContent = ""; help.classList.remove("warn"); return; }
    const left = data.book.totals.balance - amountValue();
    help.classList.toggle("warn", left < 0);
    help.textContent = left < 0 ? T.over(money(-left)) : T.left(money(left));
  }

  function openEntrySheet() {
    $("sheetTitle").textContent = kind === "out" ? T.addOut : T.addIn;
    $("amountLabel").textContent = kind === "out" ? T.amountOut : T.amountIn;
    $("save").className = "primary " + kind;
    $("amount").value = "";
    $("note").value = "";
    $("day").max = today();
    setDay(today());
    updateHelp();
    openSheet("sheet", "entry");
    setTimeout(() => $("amount").focus(), 60);
  }

  function openBookSheet(editing) {
    $("bookSheetTitle").textContent = editing ? T.editBook : T.newBook;
    $("bookName").value = editing ? data.book.name : "";
    $("bookDelete").hidden = !editing;
    const chips = $("nameChips");
    chips.replaceChildren();
    chips.hidden = editing;
    for (const idea of T.nameIdeas) {
      const c = el("button", "chip", idea);
      c.type = "button";
      c.addEventListener("click", () => { $("bookName").value = idea; $("bookName").focus(); });
      chips.append(c);
    }
    openSheet("bookSheet", editing ? "editBook" : "newBook");
    setTimeout(() => $("bookName").focus(), 60);
  }

  function removeEntry(id) {
    confirmBox(T.del, async (ok) => {
      if (!ok) return;
      try {
        data = await call("delete", { book: data.book.id, id });
        render();
        toast(T.deleted);
        haptic("success");
      } catch (err) { fail(err); }
    });
  }

  async function busy(button, work) {
    const label = button.querySelector("span");
    const text = label.textContent;
    button.disabled = true;
    label.textContent = T.saving;
    try { await work(); } catch (err) { fail(err); } finally {
      button.disabled = false;
      label.textContent = text;
    }
  }

  $("amount").addEventListener("input", (ev) => {
    const digits = ev.target.value.replace(/\D/g, "").replace(/^0+/, "").slice(0, 13);
    ev.target.value = digits.replace(/\B(?=(\d{3})+(?!\d))/g, " ");
    updateHelp();
  });
  $("amount").addEventListener("keydown", (ev) => { if (ev.key === "Enter") { ev.preventDefault(); $("note").focus(); } });
  document.querySelectorAll(".chip[data-day]").forEach((c) => c.addEventListener("click", () => setDay(shiftDay(today(), Number(c.dataset.day)))));
  $("day").addEventListener("change", (ev) => { if (ev.target.value) setDay(ev.target.value > today() ? today() : ev.target.value); });
  $("scrim").addEventListener("click", closeSheet);
  $("backBtn").addEventListener("click", goBack);
  $("moreBtn").addEventListener("click", () => openBookSheet(true));
  $("addBtn").addEventListener("click", () => (screen === "books" ? openBookSheet(false) : openEntrySheet()));
  document.querySelectorAll("[data-open]").forEach((b) => b.addEventListener("click", () => { kind = b.dataset.open; show("book"); }));

  $("sheet").addEventListener("submit", (ev) => {
    ev.preventDefault();
    const amount = amountValue();
    if (!amount) { toast(T.err.amount); haptic("error"); $("amount").focus(); return; }
    busy($("save"), async () => {
      data = await call("add", { book: data.book.id, kind, amount, note: $("note").value, day: $("day").value });
      closeSheet();
      toast(T.saved);
      haptic("success");
    });
  });

  $("bookSheet").addEventListener("submit", (ev) => {
    ev.preventDefault();
    const name = $("bookName").value.trim();
    if (!name) { toast(T.err.name); haptic("error"); return; }
    busy($("bookSave"), async () => {
      if (sheet === "editBook") data = await call("rename_book", { book: data.book.id, name });
      else books = await call("create_book", { name });
      closeSheet();
      toast(T.bookSaved);
      haptic("success");
    });
  });

  $("bookDelete").addEventListener("click", () => {
    confirmBox(T.delBook(data.book.name), async (ok) => {
      if (!ok) return;
      try {
        books = await call("delete_book", { book: data.book.id });
        data = null;
        sheet = null;
        $("scrim").hidden = true;
        $("bookSheet").hidden = true;
        show("books");
        toast(T.bookDeleted);
        haptic("success");
      } catch (err) { fail(err); }
    });
  });

  async function start() {
    const state = $("state");
    if (!inApp && !session) {
      $("skeleton").hidden = true;
      state.textContent = T.openInTg;
      state.hidden = false;
      return;
    }
    if (inApp) {
      tg.ready();
      tg.expand();
      tg.BackButton.onClick(goBack);
    }
    try {
      await loadBooks();
      // With one book there is nothing to choose: it opens at once.
      if (books.books.length === 1) data = await call("state", { book: books.books[0].id });
      $("skeleton").hidden = true;
      show(data ? "kassa" : "books");
    } catch (err) {
      $("skeleton").hidden = true;
      state.textContent = T.err[err.message] || T.err.net;
      state.hidden = false;
    }
  }
  start();
})();
