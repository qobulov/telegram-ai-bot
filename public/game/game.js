// Tarozi. The server deals each play's rounds and counts its score from the
// answers and their times; the score shown while playing is only a preview.
"use strict";

(function () {
  const FRUITS = ["🍎", "🍐", "🍊", "🍋", "🍇", "🍑", "🥝", "🍒"];
  // feedbackMs keeps the scale tilted after an answer; it also keeps the
  // answers further apart than the server's 300 ms floor.
  const feedbackMs = 380;
  const $ = (id) => document.getElementById(id);

  const texts = {
    uz: {
      lead: "Bozor tarozisi: qaysi palla og'irroq?",
      rule1: "Taxtada har mevaning vazni yozilgan. Narxlar har 6 tarozida o'zgaradi.",
      rule2: "Pallalardagi mevalarni qo'shing: chap og'irmi, o'ngmi yoki teng?",
      rule3: "40 soniya. Ketma-ket to'g'ri javob ko'proq ochko beradi, xato −5.",
      play: "Boshlash", score: "Ochko", streak: "Seriya", prices: "Narxlar taxtasi",
      left: "Chap", even: "Teng", right: "O'ng", over: "Vaqt tugadi",
      again: "Yana o'ynash", share: "Ulashish", table: "Reyting",
      newPrices: "Yangi narxlar!", best: "Yangi rekord! 🎉", notBest: (n) => `Bu safar ${n} ochko. Rekordingiz saqlanib qoldi.`,
      zero: "Bu safar ochko yo'q. Yana urinib ko'ring!",
      noSession: "O'yinni Telegram'da oching: botga /game yozing va «Play» ni bosing.",
      session: "O'yin havolasi eskirdi. Telegram'da «Play» ni qaytadan bosing.",
      implausible: "Bu natija qabul qilinmadi.", unavailable: "Aloqa bo'lmadi. Qayta urinib ko'ring.",
      weighing: (l, r) => `Chap pallada ${l}, o'ng pallada ${r}.`,
    },
    ru: {
      lead: "Базарные весы: какая чаша тяжелее?",
      rule1: "На доске вес каждого фрукта. Цены меняются каждые 6 взвешиваний.",
      rule2: "Сложите фрукты на чашах: тяжелее левая, правая или поровну?",
      rule3: "40 секунд. Серия верных ответов даёт больше очков, ошибка −5.",
      play: "Начать", score: "Очки", streak: "Серия", prices: "Доска цен",
      left: "Левая", even: "Поровну", right: "Правая", over: "Время вышло",
      again: "Ещё раз", share: "Поделиться", table: "Рейтинг",
      newPrices: "Новые цены!", best: "Новый рекорд! 🎉", notBest: (n) => `В этот раз ${n} очков. Рекорд остался прежним.`,
      zero: "В этот раз без очков. Попробуйте ещё!",
      noSession: "Откройте игру в Telegram: напишите боту /game и нажмите «Play».",
      session: "Ссылка на игру устарела. Нажмите «Play» в Telegram ещё раз.",
      implausible: "Этот результат не принят.", unavailable: "Нет связи. Попробуйте ещё раз.",
      weighing: (l, r) => `На левой чаше ${l}, на правой ${r}.`,
    },
  };
  const t = texts[(navigator.language || "").toLowerCase().startsWith("ru") ? "ru" : "uz"];
  document.documentElement.lang = t === texts.ru ? "ru" : "uz";

  const session = new URLSearchParams(location.search).get("s") || "";
  let play = null;

  function translate() {
    for (const node of document.querySelectorAll("[data-t]")) node.textContent = t[node.dataset.t];
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

  function show(id) {
    for (const screen of document.querySelectorAll(".screen")) screen.hidden = screen.id !== id;
  }

  async function begin() {
    $("play").disabled = $("again").disabled = true;
    try {
      const dealt = await api({ action: "start", session });
      play = { run: dealt.run, rounds: dealt.rounds, seconds: dealt.seconds, i: 0, answers: "", times: [], score: 0, streak: 0 };
      await countdown();
      startRounds();
    } catch (error) {
      show("intro");
      $("introMessage").textContent = problem(error);
    } finally {
      $("play").disabled = $("again").disabled = false;
    }
  }

  function countdown() {
    show("countdown");
    return new Promise((resolve) => {
      let n = 3;
      $("count").textContent = n;
      const tick = setInterval(() => {
        n -= 1;
        if (n === 0) {
          clearInterval(tick);
          resolve();
          return;
        }
        $("count").textContent = n;
      }, 700);
    });
  }

  function startRounds() {
    show("board");
    $("score").textContent = "0";
    $("streak").textContent = "0";
    play.started = performance.now();
    play.board = "";
    showRound();
    play.timer = requestAnimationFrame(tickTimer);
  }

  function tickTimer() {
    const left = 1 - (performance.now() - play.started) / (play.seconds * 1000);
    const bar = $("timer");
    bar.style.transform = `scaleX(${Math.max(0, left)})`;
    bar.parentElement.classList.toggle("low", left < 0.2);
    if (left <= 0) {
      finish();
      return;
    }
    play.timer = requestAnimationFrame(tickTimer);
  }

  function showRound() {
    const round = play.rounds[play.i];
    const board = round.fruits.join(",") + "|" + round.weights.join(",");
    if (board !== play.board) {
      const list = $("priceList");
      list.replaceChildren();
      round.fruits.forEach((fruit, place) => {
        const item = document.createElement("li");
        const weight = document.createElement("b");
        weight.textContent = round.weights[place];
        item.append(FRUITS[fruit], weight);
        list.append(item);
      });
      if (play.board) {
        const prices = $("prices");
        prices.classList.add("changed");
        $("weighing").textContent = t.newPrices;
        setTimeout(() => prices.classList.remove("changed"), 900);
      }
      play.board = board;
    }
    fillPan($("leftPan"), round, round.left);
    fillPan($("rightPan"), round, round.right);
    const scale = $("scale");
    scale.dataset.tilt = "none";
    scale.classList.remove("good", "bad");
    $("weighing").textContent = t.weighing(names(round, round.left), names(round, round.right));
    setAnswering(true);
  }

  function fillPan(pan, round, places) {
    pan.textContent = places.map((place) => FRUITS[round.fruits[place]]).join("");
  }

  function names(round, places) {
    return places.map((place) => FRUITS[round.fruits[place]]).join(" ");
  }

  function weigh(round, places) {
    return places.reduce((sum, place) => sum + round.weights[place], 0);
  }

  function rightAnswer(round) {
    const left = weigh(round, round.left);
    const right = weigh(round, round.right);
    return left > right ? "L" : left < right ? "R" : "E";
  }

  function setAnswering(on) {
    play.answering = on;
    for (const button of document.querySelectorAll("[data-answer]")) button.disabled = !on;
  }

  function answer(choice) {
    if (!play || !play.answering) return;
    const at = Math.round(performance.now() - play.started);
    if (at > play.seconds * 1000) return;
    setAnswering(false);
    const round = play.rounds[play.i];
    const correct = rightAnswer(round);
    play.answers += choice;
    play.times.push(at);
    if (choice === correct) {
      play.score += 10 + 2 * Math.min(play.streak, 5);
      play.streak += 1;
    } else {
      play.score = Math.max(play.score - 5, 0);
      play.streak = 0;
    }
    $("score").textContent = play.score;
    $("streak").textContent = play.streak;
    const scale = $("scale");
    scale.dataset.tilt = correct === "E" ? "none" : correct;
    scale.classList.add(choice === correct ? "good" : "bad");
    if (navigator.vibrate && choice !== correct) navigator.vibrate(40);
    setTimeout(() => {
      if (!play.answering && !play.over) {
        play.i += 1;
        if (play.i >= play.rounds.length) {
          finish();
          return;
        }
        showRound();
      }
    }, feedbackMs);
  }

  async function finish() {
    if (play.over) return;
    play.over = true;
    cancelAnimationFrame(play.timer);
    setAnswering(false);
    show("result");
    $("final").textContent = play.score;
    $("resultMessage").textContent = "";
    $("resultTable").hidden = true;
    $("again").disabled = true;
    try {
      const done = await api({ action: "finish", run: play.run, answers: play.answers, times: play.times });
      $("final").textContent = done.score;
      $("resultMessage").textContent = done.score === 0 ? t.zero : done.improved ? t.best : t.notBest(done.score);
      renderTable($("resultTable"), $("resultRows"), done.table);
    } catch (error) {
      $("resultMessage").textContent = problem(error);
    } finally {
      $("again").disabled = false;
    }
  }

  // avatarOf is a player's photo beside their name, or their initial when
  // they have none or hide it.
  function avatarOf(url, name) {
    const initial = document.createElement("span");
    initial.className = "ava";
    initial.textContent = (name || "?").trim().slice(0, 1).toUpperCase() || "?";
    if (!url) return initial;
    const img = new Image();
    img.className = "ava";
    img.alt = "";
    img.decoding = "async";
    img.onerror = () => img.replaceWith(initial);
    img.src = url;
    return img;
  }

  function renderTable(card, rows, table) {
    rows.replaceChildren();
    if (!table || table.length === 0) {
      card.hidden = true;
      return;
    }
    for (const row of table) {
      const item = document.createElement("li");
      if (row.me) item.className = "me";
      const place = document.createElement("span");
      place.className = "place";
      place.textContent = row.position;
      const name = document.createElement("span");
      name.className = "name";
      name.append(avatarOf(row.avatar, row.name), row.name);
      const score = document.createElement("span");
      score.textContent = row.score;
      item.append(place, name, score);
      rows.append(item);
    }
    card.hidden = false;
  }

  function keys(event) {
    const choice = { ArrowLeft: "L", ArrowRight: "R", ArrowDown: "E", " ": "E", "=": "E" }[event.key];
    if (choice && play && play.answering) {
      event.preventDefault();
      answer(choice);
    }
  }

  function init() {
    translate();
    if (!session) {
      $("play").hidden = true;
      $("introMessage").textContent = t.noSession;
      return;
    }
    $("play").addEventListener("click", begin);
    $("again").addEventListener("click", begin);
    $("share").addEventListener("click", () => {
      if (window.TelegramGameProxy) window.TelegramGameProxy.shareScore();
    });
    for (const button of document.querySelectorAll("[data-answer]")) {
      button.addEventListener("pointerdown", (event) => {
        event.preventDefault();
        answer(button.dataset.answer);
      });
      button.addEventListener("keydown", (event) => {
        if (event.key === "Enter") answer(button.dataset.answer);
      });
    }
    document.addEventListener("keydown", keys);
    api({ action: "table", session })
      .then((data) => renderTable($("introTable"), $("introRows"), data.table))
      .catch((error) => {
        if (error.message === "session") $("introMessage").textContent = t.session;
      });
  }

  init();
})();
