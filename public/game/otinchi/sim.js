// O'tinchi's rules, as internal/game/otinchi.go plays them: the server
// plays a play's chops again with these to score it, so any change here
// must be made there too. The rules' numbers come from the server, and all
// of it is whole numbers.
"use strict";

(function (root) {
  const NONE = 0, LEFT = 1, RIGHT = 2;

  function Tree(seed, rules) {
    this.r = rules;
    this.random = seed >>> 0;
    this.branches = [];
    this.golden = [];
  }

  Tree.prototype.next = function () {
    let x = this.random;
    x = (x ^ (x << 13)) >>> 0;
    x = (x ^ (x >>> 17)) >>> 0;
    x = (x ^ (x << 5)) >>> 0;
    this.random = x;
    return x;
  };

  // at is the piece at i, its branch and whether it is golden.
  Tree.prototype.at = function (i) {
    while (this.branches.length <= i) {
      const n = this.branches.length;
      if (n < this.r.safe) {
        this.branches.push(NONE);
        this.golden.push(false);
        continue;
      }
      const prev = this.branches[n - 1];
      const roll = this.next() % 100, shine = this.next() % 10;
      let branch = NONE;
      if (prev === NONE && roll >= 70) branch = RIGHT;
      else if (prev === NONE && roll >= 40) branch = LEFT;
      else if (prev !== NONE && roll >= 55) branch = prev;
      this.branches.push(branch);
      this.golden.push(branch === NONE && shine === 0);
    }
    return [this.branches[i], this.golden[i]];
  };

  // Game is a play: the time bar's milliseconds left, the chops and the
  // score; chop plays a chop and says what came of it.
  function Game(seed, rules) {
    this.r = rules;
    this.tree = new Tree(seed, rules);
    this.left = rules.start_ms;
    this.chops = 0;
    this.score = 0;
    this.side = LEFT;
    this.last = null;
    this.over = null;
  }

  Game.prototype.drain = function (gap) {
    const r = this.r;
    return Math.trunc((gap * (r.drain_cap + Math.min(this.chops, r.drain_cap))) / r.drain_cap);
  };

  // leftAt is the time bar's milliseconds left at, from the first chop, for
  // the page to draw between chops.
  Game.prototype.leftAt = function (at) {
    if (this.last === null) return this.left;
    return this.left - this.drain(at - this.last);
  };

  // chop chops from side at at; it gives "too_soon" for a chop quicker
  // than a hand, "time" when the time ran out first, "branch" when a
  // branch ended the play, or "ok" with golden for a golden piece.
  Game.prototype.chop = function (side, at) {
    const r = this.r;
    if (this.over) return { result: this.over };
    if (this.last !== null) {
      const gap = at - this.last;
      if (gap < r.min_gap) return { result: "too_soon" };
      this.left -= this.drain(gap);
      if (this.left <= 0) {
        this.over = "time";
        return { result: "time" };
      }
    }
    this.last = at;
    this.side = side;
    const [branch, golden] = this.tree.at(this.chops);
    if (branch === side) {
      this.over = "branch";
      return { result: "branch", stepped: true };
    }
    this.chops += 1;
    const [fallen] = this.tree.at(this.chops);
    if (fallen === side) {
      this.over = "branch";
      return { result: "branch", stepped: false, golden };
    }
    this.score += golden ? r.golden : 1;
    this.left = Math.min(r.most_ms, this.left + Math.max(r.least_bonus, r.first_bonus - this.chops));
    return { result: "ok", golden };
  };

  // play plays sides, an L or an R a chop, at times and gives the score,
  // or null for a play the server would refuse.
  Game.play = function (seed, rules, sides, times) {
    if (!sides.length || sides.length !== times.length || times[0] !== 0) return null;
    const game = new Game(seed, rules);
    for (let i = 0; i < sides.length; i++) {
      const side = { L: LEFT, R: RIGHT }[sides[i]];
      if (!side) return null;
      const { result } = game.chop(side, times[i]);
      if (result === "too_soon" || result === "time") return null;
      if (result === "branch" && i !== sides.length - 1) return null;
    }
    return game.score;
  };

  Game.LEFT = LEFT; Game.RIGHT = RIGHT; Game.NONE = NONE;
  if (typeof module !== "undefined" && module.exports) module.exports = Game;
  else root.OtinchiGame = Game;
})(this);
