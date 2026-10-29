// The group dashboard. Everything it shows comes from /api/stats, which
// trusts nothing but the init data Telegram signed: the page only says which
// group and period to show, and the server checks the viewer is one of the
// bot's owners.
"use strict";

(function () {
  const tg = window.Telegram && window.Telegram.WebApp;
  const $ = (id) => document.getElementById(id);

  const texts = {
    uz: {
      eyebrow: "Statistika", groups: "Guruhlar", messages: "xabar", members: "a'zo yozdi",
      peak: "eng qizg'in soat", hours: "Soatlar bo'yicha xabarlar", top: "Eng faol a'zolar",
      guards: "Himoyalar", swearers: "Eng ko'p so'kinganlar", today: "Bugun", week: "7 kun",
      chooseTitle: "Qaysi guruh?", noGroups: "Siz bilan birga bo'lgan guruhni topmadim. Guruhda /stats yozing.",
      nobody: "Hali hech kim yozmagan.", clean: "Hech kim so'kinmagan 👏", count: (n) => `${n} xabar`,
      hourTip: (h, n) => `${h}:00 — ${n} xabar`, hourLabel: "Soatlar bo'yicha xabarlar; eng ko'pi",
      on: "yoqilgan", off: "o'chiq",
      guardNames: { swear: "So'kinish", spam: "Spam", links: "Xavfli havolalar", images: "Nojo'ya rasmlar", joins: "Qo'shilish tekshiruvi" },
      note: "Botning o'z xabarlari, u jim bo'lgan vaqtdagi va saqlanish muddatidan eski xabarlar sanalmaydi.",
      outside: "Bu sahifa Telegram ichida ochiladi. Botga /stats yozing.",
      expired: "Sahifa eskirdi. Uni qaytadan oching.", unauthorized: "Telegram tasdiqlamadi. Sahifani bot orqali qaytadan oching.",
      notOwner: "Statistikani faqat bot egalari ko'radi.",
      unavailable: "Hozir ma'lumot olib bo'lmadi.", retry: "Qayta urinish", close: "Yopish", back: "Orqaga",
    },
    ru: {
      eyebrow: "Статистика", groups: "Группы", messages: "сообщений", members: "писали",
      peak: "самый активный час", hours: "Сообщения по часам", top: "Самые активные",
      guards: "Защиты", swearers: "Больше всех ругались", today: "Сегодня", week: "7 дней",
      chooseTitle: "Какая группа?", noGroups: "Не нашла общих с вами групп. Напишите /stats в группе.",
      nobody: "Пока никто не писал.", clean: "Никто не ругался 👏", count: (n) => `${n} сообщ.`,
      hourTip: (h, n) => `${h}:00 — ${n} сообщ.`, hourLabel: "Сообщения по часам; больше всего",
      on: "включена", off: "выключена",
      guardNames: { swear: "Мат", spam: "Спам", links: "Опасные ссылки", images: "Неприличные фото", joins: "Проверка вступления" },
      note: "Не считаются сообщения самого бота, написанные пока он молчал и старше срока хранения.",
      outside: "Эта страница открывается внутри Telegram. Напишите боту /stats.",
      expired: "Страница устарела. Откройте её заново.", unauthorized: "Telegram не подтвердил вход. Откройте страницу через бота.",
      notOwner: "Статистику видят только владельцы бота.",
      unavailable: "Сейчас не удалось получить данные.", retry: "Повторить", close: "Закрыть", back: "Назад",
    },
  };

  const user = tg && tg.initDataUnsafe && tg.initDataUnsafe.user;
  const t = texts[user && user.language_code === "ru" ? "ru" : "uz"];
  document.documentElement.lang = t === texts.ru ? "ru" : "uz";

  const view = { group: "", period: "today", fromList: false };

  function start() {
    if (tg) {
      tg.ready();
      tg.expand();
      if (tg.themeParams && Object.keys(tg.themeParams).length > 0) {
        document.documentElement.dataset.tg = "1";
      }
      tg.BackButton.onClick(showList);
    }
    translate();
    if (!tg || !tg.initData) {
      showState(t.outside);
      return;
    }
    $("period").addEventListener("click", (event) => {
      const button = event.target.closest("button[data-period]");
      if (!button || button.dataset.period === view.period) return;
      view.period = button.dataset.period;
      haptic();
      load();
    });
    load();
  }

  function translate() {
    $("eyebrow").textContent = t.eyebrow;
    for (const node of document.querySelectorAll("[data-t]")) {
      node.textContent = t[node.dataset.t];
    }
    for (const button of $("period").querySelectorAll("button")) {
      button.textContent = t[button.dataset.period];
    }
    $("note").textContent = t.note;
  }

  async function load() {
    document.body.classList.add("loading");
    try {
      const response = await fetch("/api/stats", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ init_data: tg.initData, group: view.group, period: view.period }),
      });
      const data = await response.json().catch(() => ({ error: "unavailable" }));
      if (!response.ok || data.error) {
        failed(data.error || "unavailable");
      } else if (data.groups) {
        renderList(data.groups);
      } else {
        renderDashboard(data);
      }
    } catch (error) {
      failed("unavailable");
    } finally {
      document.body.classList.remove("loading");
    }
  }

  function failed(code) {
    const message = { expired: t.expired, unauthorized: t.unauthorized, not_owner: t.notOwner }[code] || t.unavailable;
    const retry = code === "unavailable" || !code;
    showState(message, retry ? { label: t.retry, run: load } : { label: t.close, run: () => tg.close() });
  }

  function showState(message, action) {
    $("groups").hidden = true;
    $("dashboard").hidden = true;
    $("period").hidden = true;
    if ($("title").textContent === "…") $("title").textContent = t.eyebrow;
    const state = $("state");
    state.replaceChildren(paragraph(message));
    if (action) {
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = action.label;
      button.addEventListener("click", action.run);
      state.append(button);
    }
    state.hidden = false;
  }

  function renderList(groups) {
    $("state").hidden = true;
    $("dashboard").hidden = true;
    $("period").hidden = true;
    $("title").textContent = t.chooseTitle;
    if (tg) tg.BackButton.hide();
    const list = $("groupList");
    list.replaceChildren();
    if (groups.length === 0) {
      showState(t.noGroups);
      return;
    }
    for (const group of groups) {
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = group.title || group.group;
      button.addEventListener("click", () => {
        view.group = group.group;
        view.fromList = true;
        haptic();
        load();
      });
      const item = document.createElement("li");
      item.append(button);
      list.append(item);
    }
    $("groups").hidden = false;
  }

  function showList() {
    view.group = "";
    view.fromList = false;
    load();
  }

  function renderDashboard(data) {
    $("state").hidden = true;
    $("groups").hidden = true;
    $("title").textContent = data.title || t.eyebrow;
    view.group = data.group;
    if (tg) view.fromList ? tg.BackButton.show() : tg.BackButton.hide();
    for (const button of $("period").querySelectorAll("button")) {
      button.setAttribute("aria-pressed", String(button.dataset.period === data.period));
    }
    $("period").hidden = false;
    $("messages").textContent = data.messages.toLocaleString();
    $("members").textContent = data.members.toLocaleString();
    renderHours(data.hours);
    renderRanks($("top"), data.top, t.nobody);
    renderRanks($("swearers"), data.swearers, t.clean);
    renderGuards(data.guards);
    $("dashboard").hidden = false;
  }

  function renderHours(hours) {
    const most = Math.max(...hours);
    const peak = most > 0 ? hours.indexOf(most) : -1;
    $("peak").textContent = peak < 0 ? "—" : `${pad(peak)}:00`;
    const chart = $("hours");
    chart.setAttribute("aria-label", peak < 0 ? t.nobody : `${t.hourLabel} ${pad(peak)}:00 (${most})`);
    chart.replaceChildren();
    $("hourTip").textContent = peak < 0 ? t.nobody : t.hourTip(pad(peak), most);
    hours.forEach((count, hour) => {
      const bar = document.createElement("button");
      bar.type = "button";
      bar.className = "hour" + (hour === peak ? " peak" : "");
      bar.title = t.hourTip(pad(hour), count);
      bar.setAttribute("aria-label", t.hourTip(pad(hour), count));
      bar.setAttribute("aria-pressed", "false");
      const fill = document.createElement("span");
      fill.style.height = most > 0 ? `${(count / most) * 100}%` : "0";
      bar.append(fill);
      const show = () => {
        for (const other of chart.children) other.setAttribute("aria-pressed", "false");
        bar.setAttribute("aria-pressed", "true");
        $("hourTip").textContent = t.hourTip(pad(hour), count);
      };
      bar.addEventListener("click", show);
      bar.addEventListener("pointerenter", show);
      chart.append(bar);
    });
  }

  function renderRanks(list, entries, empty) {
    list.replaceChildren();
    if (!entries || entries.length === 0) {
      const item = document.createElement("li");
      item.append(paragraph(empty, "empty"));
      list.append(item);
      return;
    }
    const most = entries[0].count;
    entries.forEach((entry, i) => {
      const item = document.createElement("li");
      item.className = "rank";
      const place = span("rank-place", list.id === "top" && i === 0 ? "👑" : String(i + 1));
      const name = span("rank-name", entry.name || "—");
      const count = span("rank-count", entry.count.toLocaleString());
      const bar = span("rank-bar", "");
      const fill = document.createElement("span");
      fill.style.width = `${Math.max(4, (entry.count / most) * 100)}%`;
      bar.append(fill);
      bar.setAttribute("aria-hidden", "true");
      item.append(place, name, count, bar);
      list.append(item);
    });
  }

  function renderGuards(on) {
    const list = $("guards");
    list.replaceChildren();
    for (const [key, label] of Object.entries(t.guardNames)) {
      const active = on.includes(key);
      const item = document.createElement("li");
      item.className = "guard" + (active ? " on" : "");
      item.append(span("mark", active ? "✓" : "–"), document.createTextNode(label));
      item.setAttribute("aria-label", `${label}: ${active ? t.on : t.off}`);
      list.append(item);
    }
  }

  function span(className, text) {
    const node = document.createElement("span");
    node.className = className;
    node.textContent = text;
    return node;
  }

  function paragraph(text, className) {
    const node = document.createElement("p");
    if (className) node.className = className;
    node.textContent = text;
    return node;
  }

  function pad(hour) {
    return String(hour).padStart(2, "0");
  }

  function haptic() {
    if (tg && tg.HapticFeedback) tg.HapticFeedback.selectionChanged();
  }

  start();
})();
