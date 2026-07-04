// Graphwar client — small shared utilities: deterministic player colors and
// the game<->pixel coordinate transforms from GraphPlane.java.
(function (root) {
  'use strict';
  var GW = root.GW || (root.GW = {});

  // Modern neon-ish palette; deterministic per player id so all clients agree
  // visually-ish (the original randomizes colors per client, so exact match
  // isn't a protocol requirement — only stable within this client matters).
  var PALETTE = [
    '#46e0c0', '#5aa9ff', '#ff7eb6', '#ffd166', '#a78bfa',
    '#7CFC97', '#ff8c5a', '#4dd2ff', '#f25f8a', '#c0e85a'
  ];
  GW.colorForId = function (id) {
    var i = ((id % PALETTE.length) + PALETTE.length) % PALETTE.length;
    return PALETTE[i];
  };

  // GraphPlane.convertX/convertY: game coords -> pixel coords.
  GW.convertX = function (x) { return GW.C.PLANE_LENGTH * x / GW.C.PLANE_GAME_LENGTH + GW.C.PLANE_LENGTH / 2; };
  GW.convertY = function (y) { return -GW.C.PLANE_LENGTH * y / GW.C.PLANE_GAME_LENGTH + GW.C.PLANE_HEIGHT / 2; };

  // The function-drawing flip rule from GraphPlane.drawFunction:
  //   reversed = (funcReversed || terrainReversed) && !(funcReversed && terrainReversed)
  // i.e. XOR of the two.
  GW.funcDrawReversed = function (funcReversed, terrainReversed) {
    return (funcReversed || terrainReversed) && !(funcReversed && terrainReversed);
  };

  GW.clamp = function (v, lo, hi) { return v < lo ? lo : (v > hi ? hi : v); };
  GW.escapeHtml = function (s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  };
  GW.sanitizeText = function (s, max) {
    s = String(s == null ? '' : s)
      .replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/g, '')
      .replace(/[\r\n\t]+/g, ' ')
      .trim();
    max = max || 600;
    return Array.from(s).slice(0, max).join('');
  };
})(typeof window !== 'undefined' ? window : this);
