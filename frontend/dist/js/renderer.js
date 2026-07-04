// Graphwar client renderer — modern Canvas re-imagining of GraphPlane.java.
// Dark battlefield, neon grid + trajectory, soft terrain blobs, glowing
// soldiers. Coordinate math and the terrain/function reversal logic match the
// original so positions/hits line up with Java clients.
(function (root) {
  'use strict';
  var GW = root.GW;
  var C = GW.C;
  var GW2_ASSET = 'assets/gw2/';

  function cleanAssetName(s) {
    s = (s == null ? '' : String(s)).trim();
    return /^[A-Za-z0-9_ -]{1,80}$/.test(s) ? s : '';
  }

  function Renderer(canvas, game) {
    this.canvas = canvas;
    this.ctx = canvas.getContext('2d');
    this.game = game;
    this.W = C.PLANE_LENGTH;
    this.H = C.PLANE_HEIGHT;
    this.dpr = Math.min(window.devicePixelRatio || 1, 2);
    this._terrainCanvas = document.createElement('canvas');
    this._terrainCanvas.width = this.W;
    this._terrainCanvas.height = this.H;
    this._terrainDirty = true;
    this._imgCache = {};
    this._raf = null;
    this._resize();
    window.addEventListener('resize', this._resize.bind(this));
    var self = this;
    game.on('explosion', function () { self._terrainDirty = true; });
    game.on('game_started', function () { self._terrainDirty = true; });
  }

  Renderer.prototype._resize = function () {
    // Fit the 770x450 field into the canvas's CSS box, keeping aspect ratio.
    this.dpr = Math.min(window.devicePixelRatio || 1, 2); // re-read for DPI/zoom changes
    var box = this.canvas.getBoundingClientRect();
    var cs = window.getComputedStyle ? window.getComputedStyle(this.canvas) : null;
    var borderX = cs ? (parseFloat(cs.borderLeftWidth) || 0) + (parseFloat(cs.borderRightWidth) || 0) : 0;
    var borderY = cs ? (parseFloat(cs.borderTopWidth) || 0) + (parseFloat(cs.borderBottomWidth) || 0) : 0;
    var cssW = Math.max(1, (box.width || this.W) - borderX);
    var cssH = cssW * this.H / this.W;
    this.canvas.style.height = (cssH + borderY) + 'px';
    this.canvas.width = Math.round(cssW * this.dpr);
    this.canvas.height = Math.round(cssH * this.dpr);
    this.scale = this.canvas.width / this.W;
  };

  Renderer.prototype.start = function () {
    if (this._raf) return;
    var self = this;
    var loop = function () { self.frame(); self._raf = requestAnimationFrame(loop); };
    this._raf = requestAnimationFrame(loop);
  };
  Renderer.prototype.stop = function () {
    if (this._raf) { cancelAnimationFrame(this._raf); this._raf = null; }
  };

  // Build the terrain layer (obstacle grid) into an offscreen canvas once per
  // change. We render the boolean collision grid as soft dark blobs.
  Renderer.prototype._rebuildTerrain = function () {
    var g = this.game;
    var tc = this._terrainCanvas, tg = tc.getContext('2d');
    tg.clearRect(0, 0, this.W, this.H);
    if (!g.obstacle) { this._terrainDirty = false; return; }

    // Draw filled circles (smooth) rather than per-pixel, using the raw circle
    // list the obstacle keeps. Blast circles clear terrain.
    tg.save();
    for (var i = 0; i < g.obstacle.circles.length; i++) {
      var c = g.obstacle.circles[i];
      if (c.blast) continue; // handled by clearing below
    }
    // Solid pass
    tg.fillStyle = '#11202b';
    for (var j = 0; j < g.obstacle.circles.length; j++) {
      var cc = g.obstacle.circles[j];
      if (cc.blast) continue;
      tg.beginPath();
      tg.arc(cc.x, cc.y, cc.r, 0, Math.PI * 2);
      tg.fill();
    }
    // Edge glow pass using the actual grid so blast holes are reflected.
    // (Clearing for blast craters: re-punch holes.)
    tg.globalCompositeOperation = 'destination-out';
    for (var k = 0; k < g.obstacle.circles.length; k++) {
      var bc = g.obstacle.circles[k];
      if (!bc.blast) continue;
      tg.beginPath();
      tg.arc(bc.x, bc.y, bc.r, 0, Math.PI * 2);
      tg.fill();
    }
    tg.globalCompositeOperation = 'source-over';

    // Neon rim on intact terrain
    tg.lineWidth = 1.5;
    tg.strokeStyle = 'rgba(70,224,192,0.35)';
    for (var m = 0; m < g.obstacle.circles.length; m++) {
      var rc = g.obstacle.circles[m];
      if (rc.blast) continue;
      tg.beginPath();
      tg.arc(rc.x, rc.y, rc.r, 0, Math.PI * 2);
      tg.stroke();
    }
    // Scorched crater rims so destruction reads clearly (drawn last, over holes)
    tg.lineWidth = 2;
    for (var q = 0; q < g.obstacle.circles.length; q++) {
      var qc = g.obstacle.circles[q];
      if (!qc.blast) continue;
      tg.strokeStyle = 'rgba(255,140,70,0.55)';
      tg.beginPath();
      tg.arc(qc.x, qc.y, qc.r, 0, Math.PI * 2);
      tg.stroke();
      tg.strokeStyle = 'rgba(120,60,30,0.35)';
      tg.beginPath();
      tg.arc(qc.x, qc.y, qc.r + 2, 0, Math.PI * 2);
      tg.stroke();
    }
    tg.restore();
    this._terrainDirty = false;
  };

  Renderer.prototype.frame = function () {
    var g = this.game;
    var ctx = this.ctx;
    var reversed = g.isTerrainReversed();

    if (g.gameState === GW.GameConstants.GAME) g.updateDrawingStuff();

    ctx.save();
    ctx.setTransform(this.scale, 0, 0, this.scale, 0, 0);

    // Background gradient
    var bg = ctx.createLinearGradient(0, 0, 0, this.H);
    bg.addColorStop(0, '#0a1118');
    bg.addColorStop(1, '#0d1a16');
    ctx.fillStyle = bg;
    ctx.fillRect(0, 0, this.W, this.H);

    this._drawGrid(ctx);

    if (g.gameState === GW.GameConstants.GAME && g.obstacle) {
      if (this._terrainDirty) this._rebuildTerrain();
      // terrain (mirror if reversed)
      ctx.save();
      if (reversed) { ctx.translate(this.W, 0); ctx.scale(-1, 1); }
      ctx.drawImage(this._terrainCanvas, 0, 0);
      ctx.restore();

      this._drawAxes(ctx);
      this._drawSoldiers(ctx, reversed);
      this._drawHighlights(ctx, reversed);  // hovered soldier rings
      this._drawNames(ctx, reversed);
      this._drawCurrentMarker(ctx, reversed);
      this._drawOpponentPreview(ctx, reversed); // opponent's live aim (off-turn)
      this._drawPreview(ctx, reversed);     // live preview of typed function
      this._drawDrawPath(ctx, reversed);    // user-drawn trajectory points
      this._drawTargets(ctx, reversed);     // selected attack targets
      this._drawFunctionPoints(ctx, reversed);
      this._drawTrajectory(ctx, reversed);
      this._drawExplosion(ctx, reversed);
    } else {
      this._drawIdleHint(ctx);
    }

    ctx.restore();
  };

  Renderer.prototype._drawGrid = function (ctx) {
    var axisMode = (this.game && this.game.axisMode) || 'EveryUnit';
    if (axisMode === 'NoAxis' || axisMode === 'OnlyMain') return;
    ctx.strokeStyle = 'rgba(120,200,180,0.06)';
    ctx.lineWidth = 1;
    var step = this.W / C.PLANE_GAME_LENGTH; // ~15.4px per game unit
    var unitStep = axisMode === 'EveryFive' ? 5 : 1;
    step *= unitStep;
    ctx.beginPath();
    for (var x = this.W / 2; x < this.W; x += step) { ctx.moveTo(x, 0); ctx.lineTo(x, this.H); }
    for (var x2 = this.W / 2; x2 > 0; x2 -= step) { ctx.moveTo(x2, 0); ctx.lineTo(x2, this.H); }
    for (var y = this.H / 2; y < this.H; y += step) { ctx.moveTo(0, y); ctx.lineTo(this.W, y); }
    for (var y2 = this.H / 2; y2 > 0; y2 -= step) { ctx.moveTo(0, y2); ctx.lineTo(this.W, y2); }
    ctx.stroke();
  };

  Renderer.prototype._drawAxes = function (ctx) {
    if (this.game && this.game.axisMode === 'NoAxis') return;
    ctx.strokeStyle = 'rgba(160,230,210,0.25)';
    ctx.lineWidth = 1.2;
    ctx.beginPath();
    ctx.moveTo(0, this.H / 2); ctx.lineTo(this.W, this.H / 2);
    ctx.moveTo(this.W / 2, 0); ctx.lineTo(this.W / 2, this.H);
    ctx.stroke();
  };

  Renderer.prototype._soldierScreenX = function (s, reversed) { return reversed ? this.W - s.x : s.x; };

  Renderer.prototype._assetImage = function (kind, name) {
    name = cleanAssetName(name);
    if (!name) return null;
    var key = kind + '/' + name;
    var cached = this._imgCache[key];
    if (cached) return cached.complete && cached.naturalWidth > 0 ? cached : null;
    var img = new Image();
    img.src = GW2_ASSET + kind + '/' + encodeURIComponent(name).replace(/%2F/gi, '/') + '.svg';
    this._imgCache[key] = img;
    return null;
  };

  Renderer.prototype._drawV2Avatar = function (ctx, s, x, y, size) {
    var drawn = false;
    var skin = this._assetImage('skins', s.skinGraphics || '');
    var face = this._assetImage('faces', s.faceGraphics || '');
    var hat = this._assetImage('hats', s.hatGraphics || '');
    ctx.save();
    ctx.translate(x, y);
    if (skin) { ctx.drawImage(skin, -size / 2, -size / 2, size, size); drawn = true; }
    if (face) { ctx.drawImage(face, -size / 2, -size / 2, size, size); drawn = true; }
    if (hat) { ctx.drawImage(hat, -size / 2, -size / 2, size, size); drawn = true; }
    ctx.restore();
    return drawn;
  };

  Renderer.prototype._drawSoldiers = function (ctx, reversed) {
    var g = this.game;
    // determine the local player's team so we can mark friend vs foe
    var myTeam = null;
    for (var li = 0; li < g.players.length; li++) { if (g.players[li].local) { myTeam = g.players[li].team; break; } }
    for (var i = 0; i < g.players.length; i++) {
      var p = g.players[i];
      for (var j = 0; j < p.numSoldiers; j++) {
        var s = p.soldiers[j];
        if (!s.alive && !s.exploding) continue;
        var x = this._soldierScreenX(s, reversed), y = s.y;
        var alpha = 1;
        var dying = !s.alive && s.exploding;
        var deathT = dying ? (Date.now() - s.timeExplodingStarted) : 0;
        if (dying) alpha = GW.clamp(1 - deathT / 1000, 0, 1);
        var foe = (myTeam !== null && p.team !== myTeam);
        ctx.save();
        // hit flash burst in the first 250ms of death
        if (dying && deathT < 250) {
          var fp = deathT / 250;
          ctx.save();
          ctx.fillStyle = 'rgba(255,255,255,' + (1 - fp) + ')';
          ctx.beginPath(); ctx.arc(x, y, C.SOLDIER_RADIUS + 2 + fp * 10, 0, Math.PI * 2); ctx.fill();
          ctx.strokeStyle = 'rgba(255,120,80,' + (1 - fp) + ')';
          ctx.lineWidth = 2;
          ctx.beginPath(); ctx.arc(x, y, C.SOLDIER_RADIUS + fp * 16, 0, Math.PI * 2); ctx.stroke();
          ctx.restore();
        }
        ctx.globalAlpha = alpha;
        var avatarDrawn = false;
        if (g.protocolVersion === 2) {
          ctx.shadowColor = dying ? '#ff6040' : p.color;
          ctx.shadowBlur = 10;
          avatarDrawn = this._drawV2Avatar(ctx, s, x, y, C.SOLDIER_RADIUS * 3.4);
          ctx.shadowBlur = 0;
        }
        if (!avatarDrawn) {
          ctx.shadowColor = dying ? '#ff6040' : p.color;
          ctx.shadowBlur = 12;
          ctx.fillStyle = dying ? '#ff6040' : p.color;
          ctx.beginPath();
          ctx.arc(x, y, C.SOLDIER_RADIUS, 0, Math.PI * 2);
          ctx.fill();
          ctx.shadowBlur = 0;
          ctx.fillStyle = 'rgba(0,0,0,0.45)';
          ctx.beginPath();
          ctx.arc(x, y - 1, C.SOLDIER_RADIUS - 3, Math.PI, Math.PI * 2);
          ctx.fill();
        }
        // friend/foe ring: green dashed = ally, red solid = enemy
        if (!dying && myTeam !== null) {
          ctx.globalAlpha = alpha;
          ctx.lineWidth = 2;
          if (foe) { ctx.strokeStyle = 'rgba(255,90,90,0.9)'; ctx.setLineDash([]); }
          else { ctx.strokeStyle = 'rgba(120,255,160,0.85)'; ctx.setLineDash([3, 3]); }
          ctx.beginPath(); ctx.arc(x, y, C.SOLDIER_RADIUS + 3, 0, Math.PI * 2); ctx.stroke();
          ctx.setLineDash([]);
        }
        ctx.restore();
      }
    }
  };

  Renderer.prototype._drawNames = function (ctx, reversed) {
    var g = this.game;
    ctx.font = '12px "Segoe UI", system-ui, sans-serif';
    ctx.textBaseline = 'middle';
    for (var i = 0; i < g.players.length; i++) {
      var p = g.players[i];
      for (var j = 0; j < p.numSoldiers; j++) {
        var s = p.soldiers[j];
        if (!s.alive && !s.exploding) continue;
        var x = this._soldierScreenX(s, reversed);
        var y = s.y - 2 * C.SOLDIER_RADIUS - 8;
        if (y < 8) y = s.y + 2 * C.SOLDIER_RADIUS + 8;
        var w = ctx.measureText(p.name).width + 8;
        // Clamp the label box within the field (mirrors paintPlayerName).
        var bx = x - w / 2;
        if (bx < 0) bx = 0;
        if (bx + w > this.W) bx = this.W - w;
        var cx = bx + w / 2;
        ctx.save();
        if (!s.alive && s.exploding) ctx.globalAlpha = GW.clamp(1 - (Date.now() - s.timeExplodingStarted) / 1000, 0, 1);
        ctx.fillStyle = 'rgba(8,16,20,0.7)';
        ctx.strokeStyle = p.color;
        ctx.lineWidth = 1;
        roundRect(ctx, bx, y - 8, w, 16, 5);
        ctx.fill(); ctx.stroke();
        ctx.fillStyle = '#dff';
        ctx.textAlign = 'center';
        ctx.fillText(p.name, cx, y);
        ctx.restore();
      }
    }
  };

  Renderer.prototype._drawCurrentMarker = function (ctx, reversed) {
    var g = this.game;
    var p = g.getCurrentTurnPlayer();
    if (!p) return;
    var s = p.soldiers[p.currentTurnSoldier];
    if (!s) return;
    var x = this._soldierScreenX(s, reversed), y = s.y;
    var t = (Date.now() % 1500) / 1500;
    var r = C.SOLDIER_RADIUS + 5 + Math.sin(t * Math.PI * 2) * 2;
    ctx.save();
    ctx.strokeStyle = p.color;
    ctx.globalAlpha = 0.8;
    ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.arc(x, y, r, 0, Math.PI * 2);
    ctx.stroke();
    // turn arrow above
    ctx.fillStyle = p.color;
    ctx.beginPath();
    ctx.moveTo(x, y - r - 6);
    ctx.lineTo(x - 5, y - r - 13);
    ctx.lineTo(x + 5, y - r - 13);
    ctx.closePath();
    ctx.fill();
    ctx.restore();
  };

  // ---- hovered / selected soldier highlight rings ----
  // app.js sets these. highlight = list of {player, soldier} to ring (e.g. all
  // soldiers of a hovered player).
  Renderer.prototype.setHighlightSoldiers = function (list) { this._highlight = list || null; };
  Renderer.prototype.setSelectedSoldier = function (sel) { this._selectedSoldier = sel || null; };
  Renderer.prototype._drawHighlights = function (ctx, reversed) {
    var g = this.game;
    var t = (Date.now() % 1200) / 1200;
    if (this._highlight && this._highlight.length) {
      ctx.save();
      ctx.strokeStyle = 'rgba(255,235,140,0.85)';
      ctx.lineWidth = 2;
      ctx.setLineDash([2, 3]);
      for (var i = 0; i < this._highlight.length; i++) {
        var h = this._highlight[i];
        var pl = g.players[h.player]; if (!pl) continue;
        var s = pl.soldiers[h.soldier]; if (!s || !s.alive) continue;
        var x = this._soldierScreenX(s, reversed), y = s.y;
        ctx.beginPath(); ctx.arc(x, y, C.SOLDIER_RADIUS + 5 + t * 2, 0, Math.PI * 2); ctx.stroke();
      }
      ctx.restore();
    }
    if (this._selectedSoldier) {
      var sp = g.players[this._selectedSoldier.player];
      var ss = sp && sp.soldiers[this._selectedSoldier.soldier];
      if (ss && ss.alive) {
        var sx = this._soldierScreenX(ss, reversed), sy = ss.y;
        ctx.save();
        ctx.strokeStyle = '#ffd166';
        ctx.shadowColor = '#ffd166'; ctx.shadowBlur = 10;
        ctx.lineWidth = 2.5;
        ctx.beginPath(); ctx.arc(sx, sy, C.SOLDIER_RADIUS + 6 + t * 3, 0, Math.PI * 2); ctx.stroke();
        // eye marker above
        ctx.shadowBlur = 0; ctx.fillStyle = '#ffd166';
        ctx.font = '13px system-ui, sans-serif'; ctx.textAlign = 'center';
        ctx.fillText('👁', sx, sy - C.SOLDIER_RADIUS - 14);
        ctx.restore();
      }
    }
  };

  // ---- opponent's live preview trajectory (when it's NOT our turn) ----
  // The opponent's FUNCTION_PREVIEW string arrives in game.previewFunction. We
  // run the verified engine from THAT player's perspective (currentTurn already
  // points at them off-turn) and draw their prospective shot in their color.
  Renderer.prototype._drawOpponentPreview = function (ctx, terrainReversed) {
    var g = this.game;
    if (g.drawingFunction || g.exploding) return;
    var fnStr = g.previewFunction;
    if (!fnStr) return;
    var cur = g.getCurrentTurnPlayer();
    if (!cur || cur.local) return;          // only for opponents' turns
    // recompute only when the string changed (cache keyed by fn text)
    if (this._oppCacheStr !== fnStr) {
      this._oppCacheStr = fnStr;
      this._oppCache = null;
      try {
        var fn = new GW.GwFunction(fnStr);
        var inverted = cur.team !== GW.GameConstants.TEAM1;
        this._oppCache = fn.process(g.gameMode, g.obstacle, g.players, g.currentTurn,
          cur.soldiers[cur.currentTurnSoldier].angle || 0, inverted);
      } catch (e) { this._oppCache = null; }
    }
    var res = this._oppCache;
    if (!res || res.numSteps < 2) return;
    var drawReversed = GW.funcDrawReversed(g.isFunctionReversed(), terrainReversed);
    ctx.save();
    ctx.setLineDash([5, 5]);
    ctx.strokeStyle = cur.color;
    ctx.globalAlpha = 0.7;
    ctx.shadowColor = cur.color; ctx.shadowBlur = 6;
    ctx.lineWidth = 1.5;
    ctx.beginPath();
    var origin = cur.soldiers[cur.currentTurnSoldier];
    if (origin) { var ox = this._soldierScreenX(origin, terrainReversed); ctx.moveTo(ox, origin.y); }
    for (var i = 0; i < res.numSteps; i++) {
      var px = GW.convertX(res.valuesX[i]);
      var py = GW.convertY(res.valuesY[i]);
      if (drawReversed) px = this.W - px;
      if (i === 0 && !origin) ctx.moveTo(px, py); else ctx.lineTo(px, py);
    }
    ctx.stroke();
    ctx.restore();
    // predicted hits from the opponent's aim
    if (res.hits && res.hits.length) {
      ctx.save();
      for (var h = 0; h < res.hits.length; h++) {
        var hit = res.hits[h];
        var sol = g.players[hit.player].soldiers[hit.soldier];
        var hx = terrainReversed ? this.W - sol.x : sol.x;
        ctx.strokeStyle = 'rgba(255,160,60,0.9)';
        ctx.lineWidth = 2;
        ctx.beginPath(); ctx.arc(hx, sol.y, C.SOLDIER_RADIUS + 4, 0, Math.PI * 2); ctx.stroke();
      }
      ctx.restore();
    }
  };

  Renderer.prototype._drawTrajectory = function (ctx, terrainReversed) {
    var g = this.game;
    if (!g.drawingFunction || !g.funcResult) return;
    var res = g.funcResult;
    var n = g.getCurrentFunctionPosition();
    if (n < 1) return;
    var funcReversed = g.isFunctionReversed();
    var drawReversed = GW.funcDrawReversed(funcReversed, terrainReversed);
    var p = (g.getActiveFunctionPlayer && g.getActiveFunctionPlayer()) || g.getCurrentTurnPlayer();
    var color = p ? p.color : '#46e0c0';

    ctx.save();
    if (g.exploding) ctx.globalAlpha = GW.clamp(1 - g.getTimeExploding() / 1000, 0, 1);
    ctx.strokeStyle = color;
    ctx.shadowColor = color;
    ctx.shadowBlur = 8;
    ctx.lineWidth = 2;
    ctx.lineJoin = 'round';
    ctx.beginPath();
    var origin = null;
    if (p) {
      if (g.activeFunctionSoldierID != null) {
        for (var oi = 0; oi < p.soldiers.length; oi++) {
          if (p.soldiers[oi] && p.soldiers[oi].id === g.activeFunctionSoldierID) {
            origin = p.soldiers[oi];
            break;
          }
        }
      }
      if (!origin) origin = p.soldiers[p.currentTurnSoldier];
    }
    if (origin) {
      var sx = this._soldierScreenX(origin, terrainReversed);
      ctx.moveTo(sx, origin.y);
    }
    for (var i = 0; i < n; i++) {
      var px = GW.convertX(res.valuesX[i]);
      var py = GW.convertY(res.valuesY[i]);
      if (drawReversed) px = this.W - px;
      if (i === 0 && !origin) ctx.moveTo(px, py); else ctx.lineTo(px, py);
    }
    ctx.stroke();
    // projectile head
    var hx = GW.convertX(res.valuesX[n - 1]); var hy = GW.convertY(res.valuesY[n - 1]);
    if (drawReversed) hx = this.W - hx;
    ctx.fillStyle = '#fff';
    ctx.shadowBlur = 12;
    ctx.beginPath(); ctx.arc(hx, hy, 3, 0, Math.PI * 2); ctx.fill();
    ctx.restore();
  };

  Renderer.prototype._drawFunctionPoints = function (ctx, terrainReversed) {
    var g = this.game;
    var pts = g && g.remoteFunctionPoints;
    if (!pts || pts.startX == null || pts.startY == null) return;
    var sx = terrainReversed ? this.W - pts.startX : pts.startX;
    var sy = pts.startY;
    var ex = pts.endX == null ? null : (terrainReversed ? this.W - pts.endX : pts.endX);
    ctx.save();
    ctx.strokeStyle = 'rgba(255,255,255,0.75)';
    ctx.fillStyle = 'rgba(70,224,192,0.9)';
    ctx.lineWidth = 1.5;
    ctx.setLineDash([4, 4]);
    ctx.beginPath();
    ctx.arc(sx, sy, 5, 0, Math.PI * 2);
    ctx.stroke();
    ctx.fill();
    if (ex != null) {
      ctx.beginPath();
      ctx.moveTo(ex, Math.max(0, sy - 12));
      ctx.lineTo(ex, Math.min(this.H, sy + 12));
      ctx.stroke();
    }
    ctx.restore();
  };

  // ---- live preview of a typed function (before firing) ----
  Renderer.prototype.setPreviewFunction = function (str, angle) {
    this._previewStr = str || null;
    this._previewAngle = angle || 0;
    this._previewView = null;
    this._previewCache = null; // recompute next frame
  };
  Renderer.prototype._computePreview = function () {
    var g = this.game;
    if (!this._previewStr || g.drawingFunction || !g.obstacle) return null;
    var self = this;
    var run = function () {
      var cur = g.getCurrentTurnPlayer();
      if (!cur || !cur.local) return null;
      if (!cur) return null;
      var fn;
      try { fn = new GW.GwFunction(self._previewStr); } catch (e) { return null; }
      var inverted = cur.team !== GW.GameConstants.TEAM1;
      try {
        return fn.process(g.gameMode, g.obstacle, g.players, g.currentTurn, self._previewAngle, inverted);
      } catch (e) { return null; }
    };
    return run();
  };
  Renderer.prototype._drawPreview = function (ctx, terrainReversed) {
    var g = this.game;
    if (g.drawingFunction || !this._previewStr) return;
    if (!this._previewCache) this._previewCache = this._computePreview();
    var res = this._previewCache;
    if (!res || res.numSteps < 2) return;
    // when aiming from an explicit view, use THAT player's team for orientation
    var view = this._previewView;
    var aimer = view ? g.players[view.playerIdx] : g.getCurrentTurnPlayer();
    var funcReversed = aimer ? aimer.team === GW.GameConstants.TEAM2 : g.isFunctionReversed();
    var drawReversed = GW.funcDrawReversed(funcReversed, terrainReversed);
    ctx.save();
    ctx.setLineDash([6, 6]);
    ctx.strokeStyle = 'rgba(255,255,255,0.55)';
    ctx.lineWidth = 1.5;
    ctx.beginPath();
    var origin = aimer && aimer.soldiers[view ? view.soldierIdx : aimer.currentTurnSoldier];
    if (origin) {
      var sx = this._soldierScreenX(origin, terrainReversed);
      ctx.moveTo(sx, origin.y);
    }
    for (var i = 0; i < res.numSteps; i++) {
      var px = GW.convertX(res.valuesX[i]);
      var py = GW.convertY(res.valuesY[i]);
      if (drawReversed) px = this.W - px;
      if (i === 0 && !origin) ctx.moveTo(px, py); else ctx.lineTo(px, py);
    }
    ctx.stroke();
    ctx.restore();
    // highlight predicted hits
    if (res.hits && res.hits.length) {
      ctx.save();
      for (var h = 0; h < res.hits.length; h++) {
        var hit = res.hits[h];
        var sol = g.players[hit.player].soldiers[hit.soldier];
        var x = terrainReversed ? this.W - sol.x : sol.x;
        ctx.strokeStyle = '#ff5050';
        ctx.lineWidth = 2;
        ctx.beginPath(); ctx.arc(x, sol.y, C.SOLDIER_RADIUS + 4, 0, Math.PI * 2); ctx.stroke();
      }
      ctx.restore();
    }
  };

  // ---- user-drawn trajectory points (指定轨迹 mode) ----
  Renderer.prototype.setDrawPath = function (points) { this._drawPath = points || null; };
  Renderer.prototype._drawDrawPath = function (ctx, reversed) {
    var pts = this._drawPath;
    if (!pts || !pts.length) return;
    ctx.save();
    // points
    ctx.fillStyle = '#ffd166';
    for (var i = 0; i < pts.length; i++) {
      var px = reversed ? this.W - pts[i].x : pts[i].x;
      ctx.beginPath(); ctx.arc(px, pts[i].y, 3, 0, Math.PI * 2); ctx.fill();
    }
    // connecting polyline
    ctx.strokeStyle = 'rgba(255,209,102,0.4)';
    ctx.setLineDash([3, 4]);
    ctx.lineWidth = 1;
    ctx.beginPath();
    for (var j = 0; j < pts.length; j++) {
      var x = reversed ? this.W - pts[j].x : pts[j].x;
      if (j === 0) ctx.moveTo(x, pts[j].y); else ctx.lineTo(x, pts[j].y);
    }
    ctx.stroke();
    ctx.restore();
  };

  // ---- selected attack targets (selection mode) ----
  // targets = [{player, soldier}] or arbitrary map points {x,y,point:true}.
  Renderer.prototype.setTargets = function (targets, endPoint) {
    this._targets = targets || null;
    this._endPoint = endPoint || null;
  };
  Renderer.prototype._drawTargets = function (ctx, reversed) {
    var g = this.game;
    var ts = this._targets;
    if ((!ts || !ts.length) && !this._endPoint) return;
    ctx.save();
    ts = ts || [];
    for (var i = 0; i < ts.length; i++) {
      var x, y, r0, color, pointTarget = !!ts[i].point;
      if (pointTarget) {
        x = reversed ? this.W - ts[i].x : ts[i].x;
        y = ts[i].y;
        r0 = Math.min(ts[i].radius || 10, 10);
        color = '#ffd166';
      } else {
        var pl = g.players[ts[i].player];
        if (!pl) continue;
        var s = pl.soldiers[ts[i].soldier];
        if (!s || !s.alive) continue;
        x = this._soldierScreenX(s, reversed);
        y = s.y;
        r0 = C.SOLDIER_RADIUS + 5;
        color = '#ff5050';
      }
      var t = (Date.now() % 1000) / 1000;
      // pulsing crosshair ring
      ctx.strokeStyle = color;
      ctx.lineWidth = pointTarget ? 1.5 : 2;
      ctx.beginPath(); ctx.arc(x, y, r0 + t * (pointTarget ? 1.5 : 4), 0, Math.PI * 2); ctx.stroke();
      if (pointTarget) {
        ctx.fillStyle = color;
        ctx.beginPath(); ctx.arc(x, y, 2.5, 0, Math.PI * 2); ctx.fill();
      }
      // crosshair ticks
      var r = r0 + 4;
      if (!pointTarget) {
        ctx.beginPath();
        ctx.moveTo(x - r, y); ctx.lineTo(x - r + 5, y);
        ctx.moveTo(x + r, y); ctx.lineTo(x + r - 5, y);
        ctx.moveTo(x, y - r); ctx.lineTo(x, y - r + 5);
        ctx.moveTo(x, y + r); ctx.lineTo(x, y + r - 5);
        ctx.stroke();
      }
      // index label for multi-target
      if (ts.length > 1) {
        ctx.fillStyle = color;
        ctx.font = 'bold 11px system-ui, sans-serif';
        ctx.textAlign = 'center';
        ctx.fillText('' + (i + 1), x, y - r - 4);
      }
    }
    if (this._endPoint) {
      var ex = reversed ? this.W - this._endPoint.x : this._endPoint.x;
      var ey = this._endPoint.y;
      var pulse = (Date.now() % 1000) / 1000;
      var er = Math.min(this._endPoint.radius || 10, 10);
      ctx.strokeStyle = '#66aaff';
      ctx.fillStyle = '#66aaff';
      ctx.lineWidth = 1.5;
      ctx.beginPath(); ctx.arc(ex, ey, er + pulse * 1.5, 0, Math.PI * 2); ctx.stroke();
      ctx.beginPath(); ctx.arc(ex, ey, 2.5, 0, Math.PI * 2); ctx.fill();
      ctx.beginPath();
      ctx.moveTo(ex - er - 4, ey); ctx.lineTo(ex - er, ey);
      ctx.moveTo(ex + er, ey); ctx.lineTo(ex + er + 4, ey);
      ctx.moveTo(ex, ey - er - 4); ctx.lineTo(ex, ey - er);
      ctx.moveTo(ex, ey + er); ctx.lineTo(ex, ey + er + 4);
      ctx.stroke();
      ctx.font = 'bold 11px system-ui, sans-serif';
      ctx.textAlign = 'center';
      ctx.fillText('END', ex, ey - er - 8);
    }
    ctx.restore();
  };

  Renderer.prototype._drawExplosion = function (ctx, terrainReversed) {
    var g = this.game;
    if (!g.exploding || !g.funcResult) return;
    var t = g.getTimeExploding();
    var dur = 600;
    if (t > dur) return;
    var prog = t / dur;
    var funcReversed = g.isFunctionReversed();
    var drawReversed = GW.funcDrawReversed(funcReversed, terrainReversed);
    var x = g.funcResult.lastX, y = g.funcResult.lastY;
    if (drawReversed) x = this.W - x;
    var R = C.EXPLOSION_RADIUS;
    ctx.save();
    // fireball
    var rad = R * (0.6 + prog * 1.8);
    var grd = ctx.createRadialGradient(x, y, 0, x, y, rad);
    grd.addColorStop(0, 'rgba(255,245,200,' + (1 - prog) + ')');
    grd.addColorStop(0.5, 'rgba(255,140,60,' + (0.8 * (1 - prog)) + ')');
    grd.addColorStop(1, 'rgba(255,60,40,0)');
    ctx.fillStyle = grd;
    ctx.beginPath(); ctx.arc(x, y, rad, 0, Math.PI * 2); ctx.fill();
    // expanding shockwave ring
    var ring = R * (1 + prog * 3.2);
    ctx.strokeStyle = 'rgba(255,210,140,' + (0.6 * (1 - prog)) + ')';
    ctx.lineWidth = 2 * (1 - prog) + 0.5;
    ctx.beginPath(); ctx.arc(x, y, ring, 0, Math.PI * 2); ctx.stroke();
    // debris particles flying out (deterministic per-explosion via lastX seed)
    var n = 10;
    var seed = Math.floor(x * 13 + y * 7);
    ctx.fillStyle = 'rgba(255,180,90,' + (1 - prog) + ')';
    for (var i = 0; i < n; i++) {
      var ang = (i / n) * Math.PI * 2 + (seed % 7) * 0.3;
      var dr = ring * (0.5 + ((seed >> i) & 3) * 0.18);
      var dx = x + Math.cos(ang) * dr, dy = y + Math.sin(ang) * dr;
      ctx.beginPath(); ctx.arc(dx, dy, (1 - prog) * 2 + 0.5, 0, Math.PI * 2); ctx.fill();
    }
    ctx.restore();
  };

  Renderer.prototype._drawIdleHint = function (ctx) {
    ctx.fillStyle = 'rgba(160,220,200,0.5)';
    ctx.font = '20px "Segoe UI", system-ui, sans-serif';
    ctx.textAlign = 'center';
    ctx.fillText('Waiting for game to start…', this.W / 2, this.H / 2);
  };

  function roundRect(ctx, x, y, w, h, r) {
    ctx.beginPath();
    ctx.moveTo(x + r, y);
    ctx.arcTo(x + w, y, x + w, y + h, r);
    ctx.arcTo(x + w, y + h, x, y + h, r);
    ctx.arcTo(x, y + h, x, y, r);
    ctx.arcTo(x, y, x + w, y, r);
    ctx.closePath();
  }

  GW.Renderer = Renderer;
})(typeof window !== 'undefined' ? window : this);
