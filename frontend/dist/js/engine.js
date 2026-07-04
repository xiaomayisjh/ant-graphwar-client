// Graphwar math engine — faithful JS port of PolishNotationFunction.java,
// Function.java and Obstacle.java.  The original game is a relay: every client
// computes its own trajectory from the shared function string + map, so this
// engine must reproduce Java's double arithmetic closely enough that hit /
// collision decisions agree across clients.  Verified point-by-point against
// the javac-compiled original in verify/.
//
// Works in both Node (CommonJS) and the browser (window.GW).
(function (root) {
  'use strict';

  // ----- Constants (GraphServer/Constants.java) -----
  var C = {
    PLANE_LENGTH: 770,
    PLANE_HEIGHT: 450,
    PLANE_GAME_LENGTH: 50,
    SOLDIER_RADIUS: 7,
    EXPLOSION_RADIUS: 12,
    STEP_SIZE: 0.01,
    FUNC_MAX_STEPS: 20000,
    FUNC_MAX_STEP_DISTANCE_SQUARED: 0.001,
    FUNC_MIN_X_STEP_DISTANCE: 0.00001,
    ANGLE_ERROR: Math.PI / 360,
    MAX_ANGLE_LOOPS: 100,
    NORMAL_FUNC: 0,
    FST_ODE: 1,
    SND_ODE: 2,
    MAX_SOLDIERS_PER_PLAYER: 4,
    TEAM1: 1,
    TEAM2: 2
  };

  // ----- FunctionToken types (FunctionToken.java) -----
  var T = {
    ADD: 1, SUBTRACT: 2, MULTIPLY: 3, DIVIDE: 4, POW: 5,
    SQRT: 6, LOG: 7, ABS: 8, SIN: 9, COS: 10, TAN: 11, LN: 12,
    VARIABLE1: 13, VARIABLE2: 14, VARIABLE3: 15, VALUE: 16,
    LEFT_BRACKET: 17, RIGHT_BRACKET: 18
  };

  function tok(type, value) { return { type: type, value: value === undefined ? 0 : value }; }

  // ======================================================================
  //  PolishNotationFunction
  // ======================================================================
  function MalformedFunction(msg) { this.name = 'MalformedFunction'; this.message = msg || 'malformed function'; }
  MalformedFunction.prototype = Object.create(Error.prototype);

  var NUMBER_RE = /^[0-9]*\.?[0-9]+$/;
  // Ordered alternation, byte-identical to the original Java Pattern. JS `|` is
  // ordered like Java's. NOTE: `y` is listed BEFORE `y'` exactly as in the
  // original — so an input `y'` matches `y` first and the trailing `'` is
  // dropped. This means the original game treats `y'` as `y` (VARIABLE3 is
  // unreachable from typed input). We MUST keep this latent behaviour for
  // online trajectory parity with original Java clients.
  var TOKEN_RE = /[0-9]*\.?[0-9]+|\(|\)|x|y|y'|\+|\*|\/|\^|sqrt|log|abs|sin|sen|cos|tan|tg|-|ln|e|pi/g;

  function isOperation(type) { return type >= 1 && type <= 12; }

  function getNumParam(type) {
    if (type === 2) return 1;          // SUBTRACT is unary negate
    if (type >= 1 && type <= 5) return 2;
    if (type >= 6 && type <= 12) return 1;
    return 0;
  }

  function createRegularNotationTokens(argStr) {
    var funcStr = argStr.toLowerCase();
    funcStr = funcStr.replace(/-/g, '+-');
    funcStr = funcStr.replace(/exp/g, 'e^');
    funcStr = funcStr.replace(/,/g, '.');

    var out = [];
    TOKEN_RE.lastIndex = 0;
    var m;
    while ((m = TOKEN_RE.exec(funcStr)) !== null) {
      var token = m[0];
      if (m[0].length === 0) { TOKEN_RE.lastIndex++; continue; }
      if (NUMBER_RE.test(token)) {
        out.push(tok(T.VALUE, parseFloat(token)));
      } else if (token === 'x') out.push(tok(T.VARIABLE1));
      else if (token === 'y') out.push(tok(T.VARIABLE2));
      else if (token === "y'") out.push(tok(T.VARIABLE3));
      else if (token === '+') out.push(tok(T.ADD));
      else if (token === '-') out.push(tok(T.SUBTRACT));
      else if (token === '*') out.push(tok(T.MULTIPLY));
      else if (token === '/') out.push(tok(T.DIVIDE));
      else if (token === 'sqrt') out.push(tok(T.SQRT));
      else if (token === 'log') out.push(tok(T.LOG));
      else if (token === 'abs') out.push(tok(T.ABS));
      else if (token === 'sin' || token === 'sen') out.push(tok(T.SIN));
      else if (token === 'cos') out.push(tok(T.COS));
      else if (token === 'tan' || token === 'tg') out.push(tok(T.TAN));
      else if (token === '^') out.push(tok(T.POW));
      else if (token === 'ln') out.push(tok(T.LN));
      else if (token === 'e') out.push(tok(T.VALUE, Math.E));
      else if (token === 'pi') out.push(tok(T.VALUE, Math.PI));
      else if (token === '(') out.push(tok(T.LEFT_BRACKET));
      else if (token === ')') out.push(tok(T.RIGHT_BRACKET));
    }
    return adjustImplicitMultiplications(out);
  }

  function isImplicit(type1, type2) {
    if (type1 === T.VALUE || type1 === T.VARIABLE1 || type1 === T.VARIABLE2 ||
        type1 === T.VARIABLE3 || type1 === T.RIGHT_BRACKET) {
      if (type2 === T.VALUE || type2 === T.VARIABLE1 || type2 === T.VARIABLE2 ||
          type2 === T.VARIABLE3 || type2 === T.LEFT_BRACKET || getNumParam(type2) === 1) {
        return true;
      }
    }
    return false;
  }

  function adjustImplicitMultiplications(tokens) {
    if (tokens.length === 0) return tokens;
    var out = [tokens[0]];
    for (var i = 1; i < tokens.length; i++) {
      if (isImplicit(out[out.length - 1].type, tokens[i].type)) {
        out.push(tok(T.MULTIPLY));
      }
      out.push(tokens[i]);
    }
    return out;
  }

  function precedes(t0, t1) { return t0 < t1; }

  // Recursive reorder to polish (prefix) order. Mirrors reorderRec exactly.
  function reorderRec(polish, funcTokens, start, end, depth) {
    if (start > end || start >= funcTokens.length) return false;
    // depth guard: refuse pathologically nested expressions (StackOverflow DoS)
    if (depth > 2000) throw new MalformedFunction();

    var next = -1;
    var nextNest = Infinity;
    var nest = 0;

    for (var i = start; i <= end; i++) {
      var ty = funcTokens[i].type;
      if (ty === T.LEFT_BRACKET) nest++;
      else if (ty === T.RIGHT_BRACKET) nest--;
      else if (nest < nextNest || (nest === nextNest && (next === -1 || precedes(ty, funcTokens[next].type)))) {
        next = i;
        nextNest = nest;
      }
    }

    if (next === -1) return false;

    switch (getNumParam(funcTokens[next].type)) {
      case 0:
        polish.push(funcTokens[next]);
        break;
      case 1:
        polish.push(funcTokens[next]);
        reorderRec(polish, funcTokens, next + 1, end, depth + 1);
        break;
      case 2:
        polish.push(funcTokens[next]);
        var leftExists = reorderRec(polish, funcTokens, start, next - 1, depth + 1);
        if (funcTokens[next].type === T.ADD && leftExists === false) {
          polish.push(tok(T.VALUE, 0));
        }
        reorderRec(polish, funcTokens, next + 1, end, depth + 1);
        break;
    }
    return true;
  }

  function getValuesNeeded(fn) {
    var valuesNeeded = 1;
    for (var i = 0; i < fn.length; i++) {
      if (isOperation(fn[i].type)) valuesNeeded += getNumParam(fn[i].type) - 1;
      else valuesNeeded--;
      if (valuesNeeded === 0 && i + 1 < fn.length) return -1;
    }
    return valuesNeeded;
  }

  // PolishNotationFunction(String) — throws MalformedFunction.
  function PolishFunction(str) {
    // Hard caps so a hostile/huge function string can't blow the parser's
    // stack or memory (matches the recursion-depth attack surface).
    if (typeof str === 'string' && str.length > 4096) throw new MalformedFunction();
    var normal = createRegularNotationTokens(str);
    if (normal.length > 4000) throw new MalformedFunction();
    var polish = [];
    reorderRec(polish, normal, 0, normal.length - 1, 0);
    this.fn = polish;
    if (getValuesNeeded(this.fn) !== 0) throw new MalformedFunction();
    this._read = 0;
  }

  PolishFunction.prototype.evaluateFunction = function (var1, var2, var3) {
    this._v1 = var1; this._v2 = var2; this._v3 = var3;
    this._read = 0;
    return this._evaluateRec();
  };

  PolishFunction.prototype._evaluateRec = function () {
    var cur = this.fn[this._read];
    this._read++;
    switch (cur.type) {
      case T.VARIABLE1: return this._v1;
      case T.VARIABLE2: return this._v2;
      case T.VARIABLE3: return this._v3;
      case T.VALUE: return cur.value;
      case T.ADD: { var a = this._evaluateRec(); var b = this._evaluateRec(); return a + b; }
      case T.SUBTRACT: return -this._evaluateRec();
      case T.MULTIPLY: { var a2 = this._evaluateRec(); var b2 = this._evaluateRec(); return a2 * b2; }
      case T.DIVIDE: { var a3 = this._evaluateRec(); var b3 = this._evaluateRec(); return a3 / b3; }
      case T.SQRT: return Math.sqrt(this._evaluateRec());
      case T.LOG: return Math.log10(this._evaluateRec());
      case T.ABS: return Math.abs(this._evaluateRec());
      case T.SIN: return Math.sin(this._evaluateRec());
      case T.COS: return Math.cos(this._evaluateRec());
      case T.TAN: return Math.tan(this._evaluateRec());
      case T.POW: { var a4 = this._evaluateRec(); var b4 = this._evaluateRec(); return Math.pow(a4, b4); }
      case T.LN: return Math.log(this._evaluateRec());
    }
    return 0;
  };

  // ======================================================================
  //  Obstacle — collision grid (Obstacle.java).
  //  Java fills antialiased ovals on a white BufferedImage and tests
  //  getRGB(x,y) != white.  We rasterize a boolean grid; the threshold is
  //  tuned in verify/ to match Java's antialiased fill at the edges.
  // ======================================================================
  function Obstacle(numCircles, circleInfo) {
    this.W = C.PLANE_LENGTH;
    this.H = C.PLANE_HEIGHT;
    this.grid = new Uint8Array(this.W * this.H); // 1 = solid
    this.circles = [];
    for (var i = 0; i < numCircles; i++) {
      var cx = circleInfo[3 * i], cy = circleInfo[3 * i + 1], r = circleInfo[3 * i + 2];
      this.circles.push({ x: cx, y: cy, r: r });
      this._fillCircle(cx, cy, r);
    }
    this.expX = 0; this.expY = 0; this.expRadius = 0;
  }

  // Match Java2D antialiased fillOval: a pixel is non-white when the disk
  // covers its centre. Using strict center-in-disk (dist^2 < r^2) reproduces
  // the original collidePoint within sub-pixel error (validated in verify/).
  Obstacle.prototype._fillCircle = function (cx, cy, r) {
    var r2 = r * r;
    var x0 = Math.max(0, Math.floor(cx - r)), x1 = Math.min(this.W - 1, Math.ceil(cx + r));
    var y0 = Math.max(0, Math.floor(cy - r)), y1 = Math.min(this.H - 1, Math.ceil(cy + r));
    for (var y = y0; y <= y1; y++) {
      for (var x = x0; x <= x1; x++) {
        var dx = x - cx, dy = y - cy;
        if (dx * dx + dy * dy < r2) this.grid[y * this.W + x] = 1;
      }
    }
  };

  Obstacle.prototype.collidePoint = function (x, y) {
    if (x < 0 || x >= C.PLANE_LENGTH) return true;
    if (y < 0 || y >= C.PLANE_HEIGHT) return true;
    return this.grid[y * this.W + x] === 1;
  };

  Obstacle.prototype.setExplosion = function (x, y, radius) { this.expX = x; this.expY = y; this.expRadius = radius; };
  Obstacle.prototype.explodePoint = function () {
    this.circles.push({ x: this.expX, y: this.expY, r: this.expRadius, blast: true });
    this._fillCircleClear(this.expX, this.expY, this.expRadius);
  };
  // Explosion in the original paints WHITE (clears terrain).
  Obstacle.prototype._fillCircleClear = function (cx, cy, r) {
    var r2 = r * r;
    var x0 = Math.max(0, Math.floor(cx - r)), x1 = Math.min(this.W - 1, Math.ceil(cx + r));
    var y0 = Math.max(0, Math.floor(cy - r)), y1 = Math.min(this.H - 1, Math.ceil(cy + r));
    for (var y = y0; y <= y1; y++)
      for (var x = x0; x <= x1; x++) {
        var dx = x - cx, dy = y - cy;
        if (dx * dx + dy * dy < r2) this.grid[y * this.W + x] = 0;
      }
  };

  root.GW = root.GW || {};
  root.GW.C = C;
  root.GW.T = T;
  root.GW.PolishFunction = PolishFunction;
  root.GW.MalformedFunction = MalformedFunction;
  root.GW.Obstacle = Obstacle;
  root.GW.getNumParam = getNumParam;
  root.GW.isOperation = isOperation;

  // Function (trajectory) is defined in the next file (engine_function.js) and
  // augments root.GW. Kept separate only for readability.
  if (typeof module !== 'undefined' && module.exports) module.exports = root.GW;
})(typeof window !== 'undefined' ? window : (typeof global !== 'undefined' ? global : this));
