// Which of a hero's taps are critical blows, as internal/raid/rules.go's
// Critical counts them: a hash of the hero's seed and the tap's number. The
// page shows each blow's damage at once, and the server counts the same.
"use strict";

(function (root) {
  function critical(seed, tap, chance) {
    const low = tap >>> 0, high = Math.floor(tap / 4294967296) >>> 0;
    let x = (seed ^ Math.imul(low, 0x9e3779b9) ^ high) >>> 0;
    x = (x ^ (x >>> 16)) >>> 0;
    x = Math.imul(x, 0x7feb352d) >>> 0;
    x = (x ^ (x >>> 15)) >>> 0;
    x = Math.imul(x, 0x846ca68b) >>> 0;
    x = (x ^ (x >>> 16)) >>> 0;
    return x % 100 < chance;
  }
  if (typeof module !== "undefined" && module.exports) module.exports = critical;
  else root.critical = critical;
})(this);
