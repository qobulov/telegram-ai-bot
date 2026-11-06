// Kosmik ilon's rules, as internal/game/ilon.go plays them: the server
// plays a play's turns again with these to score it, so any change here
// must be made there too. The rules' numbers come from the server.
"use strict";

(function (root) {
  const DX = [0, 1, 0, -1];
  const DY = [-1, 0, 1, 0];
  const STAR = 0, SLOW = 1, GHOST = 2, KINDS = 3;

  function Sim(seed, rules) {
    this.r = rules;
    this.random = seed >>> 0;
    this.level = 1;
    this.tick = 0;
    this.eaten = 0;
    this.score = 0;
    this.combo = 0;
    this.lastMeal = -rules.combo_ticks - 1;
    this.kind = 0;
    this.bonusEnd = 0;
    this.events = [];
    this.enterLevel();
  }

  Sim.prototype.next = function () {
    let x = this.random;
    x = (x ^ (x << 13)) >>> 0;
    x = (x ^ (x >>> 17)) >>> 0;
    x = (x ^ (x << 5)) >>> 0;
    this.random = x;
    return x;
  };

  Sim.prototype.enterLevel = function () {
    const r = this.r;
    this.walls = new Uint8Array(r.cols * r.rows);
    for (const cell of r.levels[(this.level - 1) % r.levels.length]) this.walls[cell] = 1;
    this.body = r.start.slice();
    this.dir = 0;
    this.growing = 0;
    this.combo = 0;
    this.planet = -1;
    this.bonus = -1;
    this.slowEnd = 0;
    this.ghostEnd = 0;
    this.planet = this.free();
  };

  Sim.prototype.free = function () {
    const taken = Uint8Array.from(this.walls);
    for (const cell of this.body) taken[cell] = 1;
    if (this.planet >= 0) taken[this.planet] = 1;
    if (this.bonus >= 0) taken[this.bonus] = 1;
    let count = 0;
    for (let i = 0; i < taken.length; i++) if (!taken[i]) count++;
    if (count === 0) return -1;
    let pick = this.next() % count;
    for (let cell = 0; cell < taken.length; cell++) {
      if (taken[cell]) continue;
      if (pick === 0) return cell;
      pick--;
    }
    return -1;
  };

  // turn takes dir unless it goes straight back, and reports whether the
  // snake now goes another way.
  Sim.prototype.turn = function (dir) {
    if (dir === (this.dir + 2) % 4 || dir === this.dir) return false;
    this.dir = dir;
    return true;
  };

  Sim.prototype.tickMs = function () {
    const r = this.r;
    let ms = Math.max(r.first_tick - r.tick_step * (this.level - 1), r.fastest_tick);
    if (this.tick < this.slowEnd) ms = Math.floor(ms * 3 / 2);
    return ms;
  };

  Sim.prototype.ghost = function () { return this.tick < this.ghostEnd; };
  Sim.prototype.slowed = function () { return this.tick < this.slowEnd; };

  // step plays a tick and reports whether the snake crashed in it; what
  // happened is left in events, for the page to show.
  Sim.prototype.step = function () {
    const r = this.r;
    this.events = [];
    const head = this.body[0];
    const x = ((head % r.cols) + DX[this.dir] + r.cols) % r.cols;
    const y = (Math.floor(head / r.cols) + DY[this.dir] + r.rows) % r.rows;
    const cell = y * r.cols + x;
    const end = this.growing === 0 ? this.body.length - 1 : this.body.length;
    let hitsBody = false;
    for (let i = 0; i < end; i++) if (this.body[i] === cell) { hitsBody = true; break; }
    const hits = this.walls[cell] === 1 || hitsBody;
    if (hits && this.tick >= this.ghostEnd) {
      this.events.push({ type: "crash", cell });
      return true;
    }
    if (hits) this.events.push({ type: "phase", cell });
    this.body.unshift(cell);
    if (this.growing > 0) this.growing--;
    else this.body.pop();
    if (cell === this.planet) this.eat(cell);
    else if (cell === this.bonus) this.collect(cell);
    if (this.bonus >= 0 && this.tick >= this.bonusEnd) {
      this.events.push({ type: "fade", cell: this.bonus });
      this.bonus = -1;
    }
    this.tick++;
    return false;
  };

  Sim.prototype.eat = function (cell) {
    const r = this.r;
    if (this.tick - this.lastMeal <= r.combo_ticks) this.combo = Math.min(this.combo + 1, r.max_combo - 1);
    else this.combo = 0;
    this.lastMeal = this.tick;
    const points = 10 * this.level * (1 + this.combo);
    this.score += points;
    this.growing += r.growth;
    this.eaten++;
    this.events.push({ type: "eat", cell, points, combo: this.combo + 1 });
    if (this.eaten % r.per_level === 0) {
      const bonus = 100 * this.level;
      this.score += bonus;
      this.level++;
      this.enterLevel();
      this.events.push({ type: "level", level: this.level, points: bonus });
      return;
    }
    this.planet = -1;
    this.planet = this.free();
    if (this.bonus < 0 && this.next() % r.bonus_share === 0) {
      this.kind = this.next() % KINDS;
      this.bonus = this.free();
      this.bonusEnd = this.tick + r.bonus_ticks;
      if (this.bonus >= 0) this.events.push({ type: "bonus", cell: this.bonus, kind: this.kind });
    }
  };

  Sim.prototype.collect = function (cell) {
    const r = this.r;
    let points = 0;
    if (this.kind === STAR) { points = 50 * this.level; this.score += points; }
    else if (this.kind === SLOW) this.slowEnd = this.tick + 1 + r.slow_ticks;
    else if (this.kind === GHOST) this.ghostEnd = this.tick + 1 + r.ghost_ticks;
    this.events.push({ type: "collect", cell, kind: this.kind, points });
    this.bonus = -1;
  };

  // play plays turns, pairs of a tick and a direction, for ticks ticks and
  // gives the score, or null when the snake did not crash in the last one.
  Sim.play = function (seed, rules, turns, ticks) {
    const sim = new Sim(seed, rules);
    let next = 0;
    for (let tick = 0; tick < ticks; tick++) {
      if (next < turns.length && turns[next] === tick) { sim.turn(turns[next + 1]); next += 2; }
      if (sim.step() !== (tick === ticks - 1)) return null;
    }
    return sim.score;
  };

  Sim.STAR = STAR; Sim.SLOW = SLOW; Sim.GHOST = GHOST;
  if (typeof module !== "undefined" && module.exports) module.exports = Sim;
  else root.IlonSim = Sim;
})(this);
