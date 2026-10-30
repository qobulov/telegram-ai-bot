// Uchar gilam's rules, as internal/game/gilam.go plays them: the server
// plays a play's taps again with these to score it, so any change here must
// be made there too. The rules' numbers come from the server; lengths are
// in sixteenths of the field's units, and all of it is whole numbers.
"use strict";

(function (root) {
  const NONE = 0, COIN = 1, FEATHER = 2;

  function Sim(seed, rules) {
    this.r = rules;
    this.random = seed >>> 0;
    this.y = rules.start_y;
    this.vy = rules.flap;
    this.py = this.y;
    this.tick = 0;
    this.level = 1;
    this.passed = 0;
    this.coins = 0;
    this.score = 0;
    this.shield = false;
    this.ghostEnd = 0;
    this.columns = [];
    this.events = [];
  }

  Sim.prototype.next = function () {
    let x = this.random;
    x = (x ^ (x << 13)) >>> 0;
    x = (x ^ (x >>> 17)) >>> 0;
    x = (x ^ (x << 5)) >>> 0;
    this.random = x;
    return x;
  };

  Sim.prototype.tap = function () { this.vy = this.r.flap; };
  Sim.prototype.ghost = function () { return this.tick < this.ghostEnd; };

  Sim.prototype.pace = function () {
    const r = this.r;
    return Math.min(r.first_pace + r.pace_step * (this.level - 1), r.top_pace);
  };

  Sim.prototype.spawn = function () {
    const r = this.r;
    for (;;) {
      let x = r.width + 40 * r.unit;
      const n = this.columns.length;
      if (n > 0) {
        if (this.columns[n - 1].x + r.spacing > x) return;
        x = this.columns[n - 1].x + r.spacing;
      }
      const gap = Math.max(r.first_gap - r.gap_step * (this.level - 1), r.least_gap);
      const room = Math.trunc((r.ground - 2 * r.margin - gap) / r.unit);
      const top = r.margin + (this.next() % (room + 1)) * r.unit;
      const c = { x, px: x, top, gap, passed: false, item: NONE, itemY: top + Math.trunc(gap / 2) };
      const roll = this.next() % 8;
      if (roll === 0) c.item = FEATHER;
      else if (roll < 5) c.item = COIN;
      this.columns.push(c);
    }
  };

  // step plays a tick and reports whether the carpet crashed in it; what
  // happened is left in events, and where things were in py and px, for
  // the page to show.
  Sim.prototype.step = function () {
    const r = this.r;
    this.events = [];
    this.py = this.y;
    this.vy = Math.min(this.vy + r.gravity, r.max_fall);
    this.y += this.vy;
    if (this.y < r.carpet_h) { this.y = r.carpet_h; this.vy = 0; }
    const pace = this.pace();
    const kept = [];
    for (const c of this.columns) {
      c.px = c.x;
      c.x -= pace;
      if (c.x + r.column_w > 0) kept.push(c);
    }
    this.columns = kept;
    this.spawn();
    let crashed = false;
    const mid = Math.trunc((r.carpet_x0 + r.carpet_x1) / 2);
    for (const c of this.columns) {
      if (!c.passed && c.x + r.column_w < r.carpet_x0) {
        c.passed = true;
        this.passed++;
        const points = 10 * this.level;
        this.score += points;
        this.events.push({ type: "pass", points });
        if (this.passed % r.per_level === 0) {
          const bonus = 50 * this.level;
          this.score += bonus;
          this.level++;
          this.events.push({ type: "level", level: this.level, points: bonus });
        }
      }
      const center = c.x + Math.trunc(r.column_w / 2);
      if (c.item !== NONE && Math.abs(center - mid) < r.item_w && Math.abs(c.itemY - this.y) < r.item_h) {
        if (c.item === COIN) {
          this.coins++;
          const points = 5 * this.level;
          this.score += points;
          this.events.push({ type: "coin", x: center, y: c.itemY, points });
        } else {
          this.shield = true;
          this.events.push({ type: "feather", x: center, y: c.itemY });
        }
        c.item = NONE;
      }
      const inside = r.carpet_x1 > c.x && r.carpet_x0 < c.x + r.column_w;
      if (inside && (this.y - r.carpet_h < c.top || this.y + r.carpet_h > c.top + c.gap)) crashed = true;
    }
    const grounded = this.y + r.carpet_h >= r.ground;
    if (grounded) this.y = r.ground - r.carpet_h;
    if ((crashed || grounded) && this.tick >= this.ghostEnd) {
      if (!this.shield) {
        this.events.push({ type: "crash", grounded });
        return true;
      }
      this.shield = false;
      this.ghostEnd = this.tick + 1 + r.ghost;
      this.vy = r.flap;
      this.events.push({ type: "saved" });
    } else if (grounded) {
      this.vy = r.flap;
    }
    this.tick++;
    return false;
  };

  // play plays taps, the ticks the carpet was lifted in, for ticks ticks
  // and gives the score, or null when it did not crash in the last one.
  Sim.play = function (seed, rules, taps, ticks) {
    const sim = new Sim(seed, rules);
    let next = 0;
    for (let tick = 0; tick < ticks; tick++) {
      if (next < taps.length && taps[next] === tick) { sim.tap(); next++; }
      if (sim.step() !== (tick === ticks - 1)) return null;
    }
    return sim.score;
  };

  Sim.COIN = COIN; Sim.FEATHER = FEATHER;
  if (typeof module !== "undefined" && module.exports) module.exports = Sim;
  else root.GilamSim = Sim;
})(this);
