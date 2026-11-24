"use strict";
(() => {
  const tg = window.Telegram && window.Telegram.WebApp;
  const lang = ((tg && tg.initDataUnsafe && tg.initDataUnsafe.user && tg.initDataUnsafe.user.language_code) || "uz").slice(0, 2);
  const T = {
    uz: {
      kassa: "Kassa", balance: "Qoldiq", in: "Kirim", out: "Chiqim", date: "Sana", note: "Ma'lumot", save: "Saqlash",
      empty: "Hali yozuv yo'q", amountIn: "Summa", amountOut: "Qancha ishlatildi", totalIn: "Jami kirim",
      listIn: "Kirimlar", listOut: "Chiqimlar", saved: "Saqlandi", deleted: "O'chirildi", del: "Bu yozuv o'chirilsinmi?",
      loading: "Yuklanmoqda…", openInTg: "Kassa Telegram ichida ochiladi: botda /kassa yozing.",
      err: { amount: "Summani to'g'ri kiriting", day: "Sanani to'g'ri tanlang", note: "Ma'lumot juda uzun", stale: "Sahifa eskirdi, qayta oching", forged: "Kassani bot orqali oching", not_found: "Yozuv topilmadi", net: "Internet yoki server xatosi, qayta urinib ko'ring" },
    },
    ru: {
      kassa: "Касса", balance: "Остаток", in: "Приход", out: "Расход", date: "Дата", note: "Описание", save: "Сохранить",
      empty: "Записей пока нет", amountIn: "Сумма", amountOut: "Сколько потрачено", totalIn: "Всего приход",
      listIn: "Приходы", listOut: "Расходы", saved: "Сохранено", deleted: "Удалено", del: "Удалить эту запись?",
      loading: "Загрузка…", openInTg: "Касса открывается в Telegram: напишите боту /kassa.",
      err: { amount: "Введите сумму правильно", day: "Выберите дату правильно", note: "Описание слишком длинное", stale: "Страница устарела, откройте заново", forged: "Откройте кассу через бота", not_found: "Запись не найдена", net: "Ошибка сети или сервера, попробуйте ещё раз" },
    },
  };
  const t = T[lang] || T.uz;
  const $ = (id) => document.getElementById(id);
  const money = (n) => (n < 0 ? "−" : "") + Math.abs(n).toString().replace(/\B(?=(\d{3})+(?!\d))/g, " ") + " so'm";
  const dayText = (s) => { const [y, m, d] = s.split("-"); return `${d}.${m}.${y}`; };

  let data = null;
  let kind = null; // the open book, "in" or "out"; null on the home screen

  document.querySelectorAll("[data-t]").forEach((el) => { if (t[el.dataset.t]) el.textContent = t[el.dataset.t]; });
  document.documentElement.lang = lang === "ru" ? "ru" : "uz";

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
      res = await fetch("/api/kassa", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ init_data: tg.initData, action, ...extra }),
      });
    } catch (_) {
      throw new Error("net");
    }
    const body = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(body.error || "net");
    return body;
  }

  function fail(err) {
    const code = err && err.message;
    toast(t.err[code] || t.err.net);
    if (tg.HapticFeedback) tg.HapticFeedback.notificationOccurred("error");
  }

  function setMoney(el, n) {
    el.textContent = money(n);
    el.classList.toggle("neg", n < 0);
  }

  function render() {
    const tot = data.totals;
    setMoney($("homeBalance"), tot.balance);
    $("homeIn").textContent = money(tot.in);
    $("homeOut").textContent = money(tot.out);
    $("home").hidden = kind !== null;
    $("book").hidden = kind === null;
    if (kind === null) {
      tg.BackButton.hide();
      return;
    }
    tg.BackButton.show();
    // Chiqim shows what is left on top; Kirim shows all that came in.
    $("bookLabel").textContent = kind === "out" ? t.balance : t.totalIn;
    setMoney($("bookSum"), kind === "out" ? tot.balance : tot.in);
    $("amountLabel").textContent = kind === "out" ? t.amountOut : t.amountIn;
    $("listTitle").textContent = kind === "out" ? t.listOut : t.listIn;
    const items = data[kind];
    const list = $("list");
    list.replaceChildren();
    for (const e of items) {
      const li = document.createElement("li");
      const d = document.createElement("span"); d.className = "d"; d.textContent = dayText(e.day);
      const n = document.createElement("span"); n.className = "n"; n.textContent = e.note || "—";
      const a = document.createElement("span"); a.className = "a " + kind; a.textContent = (kind === "out" ? "−" : "+") + money(e.amount);
      const x = document.createElement("button"); x.type = "button"; x.className = "x"; x.textContent = "×"; x.setAttribute("aria-label", "delete");
      x.addEventListener("click", () => remove(e.id));
      li.append(d, n, a, x);
      list.append(li);
    }
    list.hidden = items.length === 0;
    $("empty").hidden = items.length !== 0;
  }

  function open(k) {
    kind = k;
    $("day").value = data.today;
    $("day").max = data.today;
    $("amount").value = "";
    $("note").value = "";
    render();
    window.scrollTo(0, 0);
  }

  function remove(id) {
    tg.showConfirm(t.del, async (ok) => {
      if (!ok) return;
      try {
        data = await call("delete", { id });
        render();
        toast(t.deleted);
      } catch (err) { fail(err); }
    });
  }

  $("amount").addEventListener("input", (ev) => {
    const digits = ev.target.value.replace(/\D/g, "").replace(/^0+/, "").slice(0, 13);
    ev.target.value = digits.replace(/\B(?=(\d{3})+(?!\d))/g, " ");
  });

  $("form").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const amount = Number($("amount").value.replace(/\D/g, ""));
    if (!amount) { toast(t.err.amount); return; }
    const save = $("save");
    save.disabled = true;
    try {
      data = await call("add", { kind, amount, note: $("note").value, day: $("day").value });
      $("amount").value = "";
      $("note").value = "";
      render();
      toast(t.saved);
      if (tg.HapticFeedback) tg.HapticFeedback.notificationOccurred("success");
    } catch (err) { fail(err); } finally { save.disabled = false; }
  });

  document.querySelectorAll("[data-open]").forEach((b) => b.addEventListener("click", () => open(b.dataset.open)));

  async function start() {
    const state = $("state");
    if (!tg || !tg.initData) {
      state.textContent = t.openInTg;
      state.hidden = false;
      return;
    }
    tg.ready();
    tg.expand();
    tg.BackButton.onClick(() => { kind = null; render(); });
    state.textContent = t.loading;
    state.hidden = false;
    try {
      data = await call("state");
      state.hidden = true;
      render();
    } catch (err) {
      state.textContent = t.err[err.message] || t.err.net;
    }
  }
  start();
})();
