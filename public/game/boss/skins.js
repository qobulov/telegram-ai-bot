// Dev jangi's bosses, each its own: draw(c, kind, now, rage) draws the
// boss of kind with its feet at (0, 95) and its body about 100 across, red
// eyed and angrier when rage is set. The kinds are internal/raid's, taken
// in turn by level.
"use strict";

(function (root) {
  const TAU = Math.PI * 2;

  function ellipse(c, x, y, rx, ry, color, rot) {
    c.fillStyle = color;
    c.beginPath();
    c.ellipse(x, y, rx, ry, rot || 0, 0, TAU);
    c.fill();
  }

  function poly(c, color, points) {
    c.fillStyle = color;
    c.beginPath();
    points.forEach(([x, y], i) => (i ? c.lineTo(x, y) : c.moveTo(x, y)));
    c.closePath();
    c.fill();
  }

  function shadow(c, w) {
    ellipse(c, 0, 96, w, 13, "rgba(0, 0, 0, 0.35)");
  }

  function eyes(c, y, gap, r, white, pupil, now, glow) {
    for (const s of [-1, 1]) {
      ellipse(c, s * gap, y, r * 1.25, r, white);
      if (glow) {
        c.save();
        c.shadowColor = pupil;
        c.shadowBlur = 14;
      }
      ellipse(c, s * gap + Math.sin(now / 900) * 2, y + 1, r * 0.5, r * 0.5, pupil);
      if (glow) c.restore();
    }
  }

  function brows(c, y, gap, angry) {
    c.strokeStyle = "#1a0c00";
    c.lineWidth = 5;
    c.lineCap = "round";
    c.beginPath();
    c.moveTo(-gap - 16, y - (angry ? 8 : 4));
    c.lineTo(-gap + 12, y + (angry ? 4 : 0));
    c.moveTo(gap + 16, y - (angry ? 8 : 4));
    c.lineTo(gap - 12, y + (angry ? 4 : 0));
    c.stroke();
  }

  function horn(c, x, y, tipX, tipY, w, color) {
    poly(c, color, [[x - w, y], [tipX, tipY], [x + w, y + 4]]);
  }

  // Ko'k dev: a round blue ogre with tusks and a club.
  function blueOgre(c, now, rage) {
    const b = Math.sin(now / 420) * 3;
    shadow(c, 86);
    ellipse(c, -34, 72, 22, 28, "#2142b8");
    ellipse(c, 34, 72, 22, 28, "#2142b8");
    const g = c.createRadialGradient(-20, -20 + b, 10, 0, 10 + b, 90);
    g.addColorStop(0, "#7ba0ff");
    g.addColorStop(1, "#3b6cff");
    c.fillStyle = g;
    c.beginPath();
    c.ellipse(0, 10 + b, 74, 76, 0, 0, TAU);
    c.fill();
    ellipse(c, 0, 34 + b, 40, 30, "#8fb0ff");
    ellipse(c, -80, 22 + b, 19, 42, "#3b6cff", -0.3);
    // The club, in the right hand.
    c.save();
    c.translate(84, 30 + b);
    c.rotate(-0.5 + Math.sin(now / 700) * 0.08);
    poly(c, "#7a4a22", [[-6, 10], [6, 10], [14, -70], [-14, -70]]);
    for (const [x, y] of [[-10, -60], [10, -48], [-8, -36], [9, -24]]) poly(c, "#d8c8a8", [[x - 4, y], [x + (x > 0 ? 9 : -9), y - 4], [x - 4, y - 8]]);
    c.restore();
    ellipse(c, 80, 22 + b, 18, 22, "#3b6cff");
    horn(c, -32, -52 + b, -54, -104 + b, 12, "#f3e2c0");
    horn(c, 32, -52 + b, 54, -104 + b, 12, "#f3e2c0");
    eyes(c, -14 + b, 25, 12, "#fff", rage ? "#ff2a3a" : "#1a0c00", now, rage);
    brows(c, -34 + b, 25, rage);
    ellipse(c, 0, 26 + b, 32, 14 + (rage ? 6 : 0), "#3a0a10");
    poly(c, "#fff", [[-22, 34 + b], [-14, 34 + b], [-18, 14 + b]]);
    poly(c, "#fff", [[22, 34 + b], [14, 34 + b], [18, 14 + b]]);
  }

  // Qizil dev: a muscular fire demon, his hair burning.
  function fireDemon(c, now, rage) {
    const b = Math.sin(now / 380) * 2;
    shadow(c, 90);
    poly(c, "#5a0a14", [[-40, 50], [40, 50], [34, 96], [8, 96], [0, 66], [-8, 96], [-34, 96]]);
    const g = c.createLinearGradient(0, -60, 0, 60);
    g.addColorStop(0, "#ff5a5a");
    g.addColorStop(1, "#b3122e");
    c.fillStyle = g;
    c.beginPath();
    c.moveTo(-86, -30 + b);
    c.quadraticCurveTo(0, -70 + b, 86, -30 + b);
    c.lineTo(46, 54);
    c.lineTo(-46, 54);
    c.closePath();
    c.fill();
    c.strokeStyle = "rgba(90, 0, 10, 0.5)";
    c.lineWidth = 3;
    c.beginPath();
    c.moveTo(0, -30 + b);
    c.lineTo(0, 40);
    c.moveTo(-30, 0);
    c.quadraticCurveTo(0, 12, 30, 0);
    c.stroke();
    for (const s of [-1, 1]) {
      ellipse(c, s * 92, 0 + b, 22, 44, "#d6264a", s * 0.25);
      ellipse(c, s * 100, 44 + b, 20, 18, "#b3122e");
    }
    // The burning hair.
    for (let i = -3; i <= 3; i++) {
      const h = 40 + Math.sin(now / 90 + i * 1.7) * 10 + (3 - Math.abs(i)) * 8;
      const fg = c.createLinearGradient(0, -60 - h, 0, -50);
      fg.addColorStop(0, "rgba(255, 240, 120, 0.9)");
      fg.addColorStop(1, "rgba(255, 80, 20, 0.95)");
      c.fillStyle = fg;
      c.beginPath();
      c.moveTo(i * 14 - 12, -48 + b);
      c.quadraticCurveTo(i * 14 - 4, -60 - h * 0.6 + b, i * 14 + Math.sin(now / 120 + i) * 6, -60 - h + b);
      c.quadraticCurveTo(i * 14 + 6, -60 - h * 0.6 + b, i * 14 + 12, -48 + b);
      c.fill();
    }
    ellipse(c, 0, -26 + b, 42, 38, "#e0384d");
    for (const s of [-1, 1]) {
      horn(c, s * 26, -54 + b, s * 60, -80 + b, 8, "#2a1418");
      horn(c, s * 34, -42 + b, s * 76, -50 + b, 7, "#2a1418");
    }
    eyes(c, -32 + b, 16, 8, "#ffe08a", rage ? "#ff0000" : "#ff8a00", now, true);
    brows(c, -44 + b, 16, true);
    poly(c, "#1a0508", [[-30, -8 + b], [30, -8 + b], [18, 30 + b], [0, 40 + b], [-18, 30 + b]]);
    ellipse(c, 0, -10 + b, 16, 7 + (rage ? 4 : 0), "#3a0a10");
    poly(c, "#fff", [[-10, -14 + b], [-5, -14 + b], [-7.5, -4 + b]]);
    poly(c, "#fff", [[10, -14 + b], [5, -14 + b], [7.5, -4 + b]]);
  }

  // Bir ko'zli dev: a tall cyclops with one great eye and a boulder.
  function cyclops(c, now, rage) {
    const b = Math.sin(now / 460) * 3;
    shadow(c, 74);
    ellipse(c, -24, 76, 16, 24, "#2f8a7a");
    ellipse(c, 24, 76, 16, 24, "#2f8a7a");
    const g = c.createRadialGradient(-16, -30 + b, 10, 0, 0 + b, 100);
    g.addColorStop(0, "#7ee0c8");
    g.addColorStop(1, "#2fa58c");
    c.fillStyle = g;
    c.beginPath();
    c.moveTo(0, -96 + b);
    c.bezierCurveTo(56, -96 + b, 70, 0 + b, 64, 52);
    c.quadraticCurveTo(0, 74, -64, 52);
    c.bezierCurveTo(-70, 0 + b, -56, -96 + b, 0, -96 + b);
    c.fill();
    poly(c, "#7a4a22", [[-60, 40], [60, 40], [50, 70], [20, 62], [0, 78], [-20, 62], [-50, 70]]);
    horn(c, 0, -92 + b, 4, -130 + b, 10, "#e8dcc0");
    // The one eye.
    ellipse(c, 0, -46 + b, 30, 26, "#ffffff");
    c.strokeStyle = "rgba(220, 40, 40, 0.5)";
    c.lineWidth = 1.5;
    for (const a of [0.4, 1.4, 2.5, 3.6, 4.6, 5.6]) {
      c.beginPath();
      c.moveTo(Math.cos(a) * 28, -46 + b + Math.sin(a) * 24);
      c.lineTo(Math.cos(a) * 18, -46 + b + Math.sin(a) * 15);
      c.stroke();
    }
    const look = Math.sin(now / 800) * 9;
    ellipse(c, look, -44 + b, 13, 13, rage ? "#ff2a3a" : "#2e7d32");
    ellipse(c, look, -44 + b, 6, 6, "#0a0a0a");
    ellipse(c, look - 4, -48 + b, 3, 3, "#ffffff");
    c.strokeStyle = "#1a2a20";
    c.lineWidth = 6;
    c.beginPath();
    c.moveTo(-32, -78 + b + (rage ? 6 : 0));
    c.quadraticCurveTo(0, -84 + b, 32, -78 + b + (rage ? 6 : 0));
    c.stroke();
    ellipse(c, 0, -6 + b, 26, 10 + (rage ? 5 : 0), "#1a3a2a");
    for (const x of [-14, 0, 14]) poly(c, "#f6f0d8", [[x - 4, -10 + b], [x + 4, -10 + b], [x, -2 + b]]);
    // The boulder, held up in the left hand.
    ellipse(c, -66, 14 + b, 16, 36, "#2fa58c", 0.4);
    c.save();
    c.translate(-92, -26 + b);
    c.rotate(now / 3000);
    poly(c, "#8a8a8a", [[-26, -6], [-12, -24], [14, -24], [28, -4], [20, 20], [-10, 24], [-26, 10]]);
    poly(c, "#a8a8a8", [[-14, -18], [8, -20], [16, -6], [-6, -2]]);
    c.restore();
  }

  // Ajdar: a dragon, its long neck up, wings spread, breathing fire.
  function dragon(c, now, rage) {
    const flap = Math.sin(now / 260) * 0.22;
    const b = Math.sin(now / 420) * 2;
    shadow(c, 100);
    for (const s of [-1, 1]) {
      c.save();
      c.translate(s * 30, -6);
      c.scale(s, 1);
      c.rotate(-flap);
      poly(c, "#1d5c2e", [[0, 0], [70, -96], [130, -70], [112, -40], [124, -20], [96, -6], [104, 18], [60, 14]]);
      c.strokeStyle = "#0f3a1c";
      c.lineWidth = 3;
      c.beginPath();
      c.moveTo(0, 0);
      c.lineTo(70, -96);
      c.moveTo(20, 0);
      c.lineTo(112, -40);
      c.moveTo(30, 6);
      c.lineTo(96, -6);
      c.stroke();
      c.restore();
    }
    // The tail, curled on the ground.
    c.strokeStyle = "#2f8a46";
    c.lineWidth = 18;
    c.lineCap = "round";
    c.beginPath();
    c.moveTo(-40, 70);
    c.quadraticCurveTo(-120, 96, -130, 50 + Math.sin(now / 500) * 8);
    c.stroke();
    poly(c, "#e6c35a", [[-140, 40 + Math.sin(now / 500) * 8], [-126, 54], [-122, 30]]);
    ellipse(c, -30, 82, 16, 14, "#21753a");
    ellipse(c, 30, 82, 16, 14, "#21753a");
    ellipse(c, 0, 40, 66, 50, "#3fae58");
    for (let i = 0; i < 5; i++) ellipse(c, 0, 14 + i * 12, 34 - i * 3, 5, "#e6c35a");
    // The neck and the head.
    c.strokeStyle = "#3fae58";
    c.lineWidth = 34;
    c.beginPath();
    c.moveTo(10, 10);
    c.quadraticCurveTo(10, -50 + b, 34, -66 + b);
    c.stroke();
    for (let i = 0; i < 4; i++) poly(c, "#e6c35a", [[-6 + i * 6, -10 - i * 16 + b], [-18 + i * 6, -16 - i * 16 + b], [-8 + i * 6, -22 - i * 16 + b]]);
    c.save();
    c.translate(40, -72 + b);
    ellipse(c, 0, 0, 30, 22, "#3fae58");
    poly(c, "#3fae58", [[14, -10], [62, -2], [62, 12], [14, 16]]);
    const jaw = rage ? 0.35 : 0.15 + Math.max(0, Math.sin(now / 900)) * 0.25;
    c.save();
    c.translate(14, 10);
    c.rotate(jaw);
    poly(c, "#2f8a46", [[0, 0], [46, 2], [44, 12], [0, 10]]);
    c.restore();
    for (const x of [24, 36, 48]) poly(c, "#fff", [[x, 12], [x + 4, 12], [x + 2, 18]]);
    horn(c, -10, -16, -34, -40, 6, "#e6c35a");
    horn(c, 4, -18, -12, -46, 6, "#e6c35a");
    ellipse(c, 6, -6, 7, 6, "#fff3a0");
    ellipse(c, 7, -6, 3, 5, rage ? "#ff2a3a" : "#0b2a14");
    ellipse(c, 56, 2, 2.5, 2, "#0b2a14");
    if (Math.sin(now / 900) > 0.5 || rage) {
      const fl = c.createLinearGradient(60, 0, 130, 40);
      fl.addColorStop(0, "#fff3a0");
      fl.addColorStop(0.5, "rgba(255, 140, 30, 0.9)");
      fl.addColorStop(1, "rgba(255, 60, 20, 0)");
      c.fillStyle = fl;
      c.beginPath();
      c.moveTo(60, 8);
      c.quadraticCurveTo(110, 4 + Math.sin(now / 40) * 6, 140, 40);
      c.quadraticCurveTo(100, 40, 60, 16);
      c.fill();
    }
    c.restore();
  }

  // Muz devi: an ice giant with crystals and a frozen beard.
  function iceGiant(c, now, rage) {
    const b = Math.sin(now / 520) * 2;
    shadow(c, 88);
    poly(c, "#7fb2d6", [[-44, 50], [-14, 50], [-18, 96], [-46, 96]]);
    poly(c, "#7fb2d6", [[44, 50], [14, 50], [18, 96], [46, 96]]);
    const g = c.createLinearGradient(-70, -60, 70, 60);
    g.addColorStop(0, "#e8f6ff");
    g.addColorStop(1, "#8fc4e8");
    c.fillStyle = g;
    c.beginPath();
    c.moveTo(-70, -40 + b);
    c.lineTo(70, -40 + b);
    c.lineTo(58, 56);
    c.lineTo(-58, 56);
    c.closePath();
    c.fill();
    for (const s of [-1, 1]) {
      poly(c, "#b8dcf2", [[s * 70, -40 + b], [s * 104, -20 + b], [s * 96, 40 + b], [s * 70, 40 + b]]);
      // The crystals on the shoulders.
      for (let i = 0; i < 3; i++) poly(c, "rgba(200, 240, 255, 0.95)", [[s * (56 + i * 14), -40 + b], [s * (62 + i * 14), -82 + b + i * 10], [s * (70 + i * 14), -40 + b]]);
    }
    poly(c, "#d6eefb", [[-36, -40 + b], [-30, -86 + b], [30, -86 + b], [36, -40 + b]]);
    for (let i = -2; i <= 2; i++) poly(c, "rgba(220, 245, 255, 0.95)", [[i * 14 - 6, -84 + b], [i * 14, -110 + b + Math.abs(i) * 8], [i * 14 + 6, -84 + b]]);
    eyes(c, -64 + b, 14, 7, "#0b3a5a", rage ? "#ff4a4a" : "#7ff0ff", now, true);
    // The frozen beard.
    poly(c, "#ffffff", [[-30, -50 + b], [30, -50 + b], [22, -6 + b], [10, -20 + b], [0, 6 + b], [-10, -20 + b], [-22, -6 + b]]);
    ellipse(c, 0, -46 + b, 12, 4 + (rage ? 3 : 0), "#0b3a5a");
    for (let i = 0; i < 6; i++) {
      const a = now / 1400 + i;
      const x = Math.cos(a) * 110, y = -20 + Math.sin(a * 1.3) * 60;
      c.strokeStyle = "rgba(220, 245, 255, 0.85)";
      c.lineWidth = 1.5;
      c.beginPath();
      for (let k = 0; k < 3; k++) {
        const r = (k * Math.PI) / 3;
        c.moveTo(x - Math.cos(r) * 5, y - Math.sin(r) * 5);
        c.lineTo(x + Math.cos(r) * 5, y + Math.sin(r) * 5);
      }
      c.stroke();
    }
  }

  // Tosh dev: a golem of stones, its runes glowing, moss on its shoulders.
  function golem(c, now, rage) {
    const b = Math.sin(now / 600) * 2;
    const glow = rage ? "#ff3a1a" : `rgba(255, ${Math.round(150 + Math.sin(now / 300) * 60)}, 40, 1)`;
    shadow(c, 92);
    const rock = (x, y, w, h, tone) => {
      c.fillStyle = tone;
      c.beginPath();
      c.roundRect(x - w / 2, y - h / 2, w, h, 12);
      c.fill();
      c.fillStyle = "rgba(255, 255, 255, 0.12)";
      c.beginPath();
      c.roundRect(x - w / 2 + 4, y - h / 2 + 4, w * 0.5, h * 0.3, 8);
      c.fill();
    };
    rock(-30, 78, 36, 36, "#6e6a64");
    rock(30, 78, 36, 36, "#6e6a64");
    rock(0, 16 + b, 110, 96, "#85807a");
    rock(-82, 0 + b, 44, 54, "#7a756f");
    rock(82, 0 + b, 44, 54, "#7a756f");
    rock(-90, 52 + b, 40, 40, "#6e6a64");
    rock(90, 52 + b, 40, 40, "#6e6a64");
    rock(0, -58 + b, 56, 46, "#908b84");
    c.fillStyle = "#4f8a3a";
    for (const [x, y] of [[-82, -24], [-70, -26], [82, -24], [94, -22], [-10, -80]]) {
      c.beginPath();
      c.arc(x, y + b, 9, Math.PI, 0);
      c.fill();
    }
    c.strokeStyle = "rgba(40, 36, 30, 0.6)";
    c.lineWidth = 3;
    c.beginPath();
    c.moveTo(-30, -20 + b);
    c.lineTo(-10, 10 + b);
    c.lineTo(-24, 40 + b);
    c.moveTo(30, -10 + b);
    c.lineTo(14, 30 + b);
    c.stroke();
    c.save();
    c.shadowColor = glow;
    c.shadowBlur = 16;
    c.strokeStyle = glow;
    c.lineWidth = 3;
    c.beginPath();
    c.moveTo(-14, 6 + b);
    c.lineTo(0, -10 + b);
    c.lineTo(14, 6 + b);
    c.lineTo(0, 22 + b);
    c.closePath();
    c.moveTo(0, 22 + b);
    c.lineTo(0, 40 + b);
    c.stroke();
    c.fillStyle = glow;
    c.fillRect(-18, -64 + b, 12, 5);
    c.fillRect(6, -64 + b, 12, 5);
    c.restore();
    c.fillStyle = "#2a2622";
    c.fillRect(-14, -46 + b, 28, 5);
  }

  // Yalmog'iz kampir: a witch floating on her broom, her orb glowing.
  function witch(c, now, rage) {
    const b = Math.sin(now / 500) * 8 - 10;
    ellipse(c, 0, 96, 50, 9, "rgba(0, 0, 0, 0.25)");
    c.save();
    c.translate(0, b);
    // The broom.
    c.strokeStyle = "#7a4a22";
    c.lineWidth = 7;
    c.beginPath();
    c.moveTo(-110, 64);
    c.lineTo(100, 40);
    c.stroke();
    poly(c, "#c9a24a", [[100, 30], [140, 18], [146, 46], [140, 70], [100, 50]]);
    // The cloak.
    const g = c.createLinearGradient(0, -100, 0, 70);
    g.addColorStop(0, "#5a2a8a");
    g.addColorStop(1, "#2a1046");
    c.fillStyle = g;
    c.beginPath();
    c.moveTo(0, -112);
    c.quadraticCurveTo(30, -60, 70, 64);
    for (let i = 0; i < 6; i++) c.lineTo(70 - (i + 0.5) * 23, 64 + (i % 2 ? 0 : 14));
    c.lineTo(-70, 64);
    c.quadraticCurveTo(-30, -60, 0, -112);
    c.fill();
    // The face in the hood.
    ellipse(c, 0, -46, 30, 34, "#1a0a2a");
    ellipse(c, 0, -42, 22, 26, "#a8c88a");
    c.fillStyle = "#cfcfcf";
    for (const s of [-1, 1]) {
      c.beginPath();
      c.moveTo(s * 14, -66);
      c.quadraticCurveTo(s * 40, -40, s * 30, -10);
      c.quadraticCurveTo(s * 24, -40, s * 10, -60);
      c.fill();
    }
    poly(c, "#8fb070", [[-4, -46], [4, -46], [16, -24], [6, -26]]);
    ellipse(c, 12, -30, 3, 3, "#6a7a40");
    eyes(c, -50, 9, 5, "#fff6a0", rage ? "#ff2a3a" : "#3aff6a", now, true);
    c.strokeStyle = "#3a2a1a";
    c.lineWidth = 2;
    c.beginPath();
    c.moveTo(-10, -22);
    c.quadraticCurveTo(0, -18 + (rage ? -4 : 2), 10, -22);
    c.stroke();
    // The bony hand and the orb.
    c.strokeStyle = "#a8c88a";
    c.lineWidth = 5;
    c.beginPath();
    c.moveTo(-40, -6);
    c.lineTo(-74, -30);
    c.stroke();
    c.save();
    c.shadowColor = "#3aff6a";
    c.shadowBlur = 26;
    const orb = c.createRadialGradient(-84, -46, 2, -84, -42, 18);
    orb.addColorStop(0, "#e0ffe8");
    orb.addColorStop(1, "rgba(58, 255, 106, 0.6)");
    c.fillStyle = orb;
    c.beginPath();
    c.arc(-84, -42, 16 + Math.sin(now / 200) * 2, 0, TAU);
    c.fill();
    c.restore();
    c.restore();
  }

  // Qora dev: a dark armoured king, magma in the cracks, a great sword.
  function darkKing(c, now, rage) {
    const b = Math.sin(now / 480) * 2;
    const magma = rage ? "#ff2a1a" : `rgba(255, ${Math.round(90 + Math.sin(now / 250) * 40)}, 20, 1)`;
    shadow(c, 92);
    // The cape.
    c.fillStyle = "#7a0a1a";
    c.beginPath();
    c.moveTo(-60, -40 + b);
    c.quadraticCurveTo(-100, 40, -84, 96);
    c.lineTo(84, 96);
    c.quadraticCurveTo(100, 40, 60, -40 + b);
    c.fill();
    poly(c, "#1a1820", [[-40, 50], [-12, 50], [-14, 96], [-42, 96]]);
    poly(c, "#1a1820", [[40, 50], [12, 50], [14, 96], [42, 96]]);
    const g = c.createLinearGradient(-60, 0, 60, 0);
    g.addColorStop(0, "#1c1a24");
    g.addColorStop(0.5, "#3a3646");
    g.addColorStop(1, "#1c1a24");
    c.fillStyle = g;
    c.beginPath();
    c.moveTo(-64, -44 + b);
    c.lineTo(64, -44 + b);
    c.lineTo(52, 58);
    c.lineTo(-52, 58);
    c.closePath();
    c.fill();
    for (const s of [-1, 1]) {
      poly(c, "#2a2834", [[s * 50, -50 + b], [s * 100, -36 + b], [s * 92, 0 + b], [s * 56, -8 + b]]);
      for (let i = 0; i < 3; i++) poly(c, "#55505f", [[s * (64 + i * 12), -50 + b], [s * (70 + i * 12), -70 + b], [s * (76 + i * 12), -48 + b]]);
    }
    c.save();
    c.shadowColor = magma;
    c.shadowBlur = 14;
    c.strokeStyle = magma;
    c.lineWidth = 3;
    c.beginPath();
    c.moveTo(-30, -30 + b);
    c.lineTo(-14, 0 + b);
    c.lineTo(-26, 30 + b);
    c.moveTo(24, -24 + b);
    c.lineTo(10, 10 + b);
    c.lineTo(22, 44 + b);
    c.stroke();
    c.restore();
    // The great sword.
    c.save();
    c.translate(70, 20 + b);
    c.rotate(0.3);
    poly(c, "#c0c4cc", [[-6, -110], [6, -110], [8, 10], [0, 22], [-8, 10]]);
    poly(c, "#ffc23d", [[-22, 10], [22, 10], [22, 18], [-22, 18]]);
    c.fillStyle = "#3a2a1a";
    c.fillRect(-4, 18, 8, 26);
    c.restore();
    // The helmet and the crown.
    ellipse(c, 0, -70 + b, 34, 32, "#2a2834");
    for (const s of [-1, 1]) horn(c, s * 26, -86 + b, s * 50, -120 + b, 8, "#1a1820");
    poly(c, "#ffc23d", [[-28, -94 + b], [-28, -112 + b], [-16, -102 + b], [-6, -118 + b], [6, -102 + b], [16, -118 + b], [28, -102 + b], [28, -94 + b]]);
    ellipse(c, 0, -104 + b, 4, 4, "#d6264a");
    c.fillStyle = "#0a0810";
    c.fillRect(-26, -74 + b, 52, 12);
    c.save();
    c.shadowColor = "#ff2a3a";
    c.shadowBlur = 16;
    c.fillStyle = rage ? "#ff2a3a" : "#ff6a3a";
    c.fillRect(-20, -71 + b, 12, 6);
    c.fillRect(8, -71 + b, 12, 6);
    c.restore();
  }

  const skins = [blueOgre, fireDemon, cyclops, dragon, iceGiant, golem, witch, darkKing];

  // Each boss's sky: its top, middle and horizon.
  const skies = [
    ["#1b1446", "#3d2a6e", "#e58a5f"],
    ["#2a0c1c", "#5c1a2c", "#ff9a5a"],
    ["#0d1a2c", "#20385a", "#6fc7c0"],
    ["#0b1e14", "#1e4a30", "#e6c35a"],
    ["#0a1a2e", "#2a4a6e", "#bfe6ff"],
    ["#1a1612", "#3a3026", "#d9a05a"],
    ["#0e0818", "#2a1440", "#7ad68a"],
    ["#09060f", "#2a1236", "#c0392b"],
  ];

  // How far to the side each boss reaches, wings, tails and brooms too, so
  // that the page can make the wide ones smaller to keep them on the screen.
  const reach = [104, 122, 122, 174, 116, 112, 147, 110];

  function draw(c, kind, now, rage) {
    skins[kind % skins.length](c, now, rage);
  }

  const api = { draw, skies, reach, count: skins.length };
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  else root.BossSkins = api;
})(this);
