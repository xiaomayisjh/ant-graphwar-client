// Graphwar trajectory engine — faithful JS port of Function.java.
// Augments window.GW / module.exports from engine.js.
(function (root) {
  'use strict';
  var GW = root.GW;
  if (!GW) throw new Error('engine.js must load before engine_function.js');
  var C = GW.C;

  // players: [{ team, soldiers:[{x,y,alive}], currentTurnSoldier }]
  // Returns a result object with valuesX/valuesY (game coords), numSteps,
  // fireAngle, lastX/lastY (pixel), and hits [{player,soldier,position}].
  function GwFunction(str) {
    this.polish = new GW.PolishFunction(str); // throws MalformedFunction
    this.strFunc = str;
  }

  GwFunction.prototype._newResult = function (numPlayers) {
    return {
      valuesX: new Float64Array(C.FUNC_MAX_STEPS),
      valuesY: new Float64Array(C.FUNC_MAX_STEPS),
      valuesDY: null,
      numSteps: 0,
      fireAngle: 0,
      lastX: 0, lastY: 0,
      hits: []
    };
  };

  function alreadyHit(hits, p, s) {
    for (var i = 0; i < hits.length; i++) if (hits[i].player === p && hits[i].soldier === s) return true;
    return false;
  }

  // getStartAngle (NORMAL_FUNC)
  GwFunction.prototype._getStartAngle = function (x, radius) {
    var pf = this.polish;
    var angle = 0;
    var startAngleTangent = (pf.evaluateFunction(x + C.STEP_SIZE, 0, 0) - pf.evaluateFunction(x, 0, 0)) / C.STEP_SIZE;
    angle = Math.atan(startAngleTangent);
    var error = 10000;
    for (var i = 0; error > C.ANGLE_ERROR && i < C.MAX_ANGLE_LOOPS; i++) {
      var finalX = x + radius * Math.cos(angle);
      startAngleTangent = (pf.evaluateFunction(finalX + C.STEP_SIZE, 0, 0) - pf.evaluateFunction(finalX, 0, 0)) / C.STEP_SIZE;
      var newAngle = Math.atan(startAngleTangent);
      error = Math.abs(newAngle - angle);
      angle = newAngle;
    }
    return angle;
  };

  GwFunction.prototype._getRK4StartAngle = function (x, y, radius) {
    var pf = this.polish;
    var angle = 0;
    var error = 10000;
    for (var i = 0; error > C.ANGLE_ERROR && i < C.MAX_ANGLE_LOOPS; i++) {
      var finalX = x + radius * Math.cos(angle);
      var finalY = y + radius * Math.sin(angle);
      var h = C.STEP_SIZE;
      var k1 = pf.evaluateFunction(finalX, finalY, 0);
      var k2 = pf.evaluateFunction(finalX + 0.5 * h, finalY + 0.5 * h * k1, 0);
      var k3 = pf.evaluateFunction(finalX + 0.5 * h, finalY + 0.5 * h * k2, 0);
      var k4 = pf.evaluateFunction(finalX + h, finalY + h * k3, 0);
      var nextY = finalY + (h / 6) * (k1 + 2 * k2 + 2 * k3 + k4);
      var nextX = finalX + h;
      var tangent = (nextY - finalY) / (nextX - finalX);
      var newAngle = Math.atan(tangent);
      error = Math.abs(newAngle - angle);
      angle = newAngle;
    }
    return angle;
  };

  // Shared hit/collision check per step. Returns true to end function.
  function checkStep(res, obstacle, players, numPlayers, currentTurn, inverted, gx, gy, i) {
    var x = C.PLANE_LENGTH * gx / C.PLANE_GAME_LENGTH + C.PLANE_LENGTH / 2;
    var y = -C.PLANE_LENGTH * gy / C.PLANE_GAME_LENGTH + C.PLANE_HEIGHT / 2;
    if (inverted) x = C.PLANE_LENGTH - x;

    for (var j = 0; j < numPlayers; j++) {
      var pl = players[j];
      var ns = pl.soldiers.length;
      for (var k = 0; k < ns; k++) {
        if (j === currentTurn && k === pl.currentTurnSoldier) continue;
        var sol = pl.soldiers[k];
        if (sol.alive) {
          var distX = sol.x - x, distY = sol.y - y;
          var distSquared = distX * distX + distY * distY;
          if (distSquared < C.SOLDIER_RADIUS * C.SOLDIER_RADIUS) {
            if (!alreadyHit(res.hits, j, k)) res.hits.push({ player: j, soldier: k, position: i });
          }
        }
      }
    }

    if (obstacle.collidePoint(Math.trunc(x), Math.trunc(y))) { res.numSteps = i; return true; }
    if (Number.isNaN(y) || !Number.isFinite(y)) { res.numSteps = i; return true; }
    return false;
  }

  function finishLast(res, inverted) {
    var n = res.numSteps;
    res.lastX = C.PLANE_LENGTH * res.valuesX[n - 1] / C.PLANE_GAME_LENGTH + C.PLANE_LENGTH / 2;
    res.lastY = -C.PLANE_LENGTH * res.valuesY[n - 1] / C.PLANE_GAME_LENGTH + C.PLANE_HEIGHT / 2;
  }

  // processFunctionRange (NORMAL_FUNC)
  GwFunction.prototype.processFunctionRange = function (obstacle, players, numPlayers, currentTurn, inverted) {
    var res = this._newResult(numPlayers);
    var pf = this.polish;
    var vx = res.valuesX, vy = res.valuesY;
    var cur = players[currentTurn].soldiers[players[currentTurn].currentTurnSoldier];

    vx[0] = cur.x; vy[0] = cur.y;
    if (inverted) vx[0] = C.PLANE_LENGTH - vx[0];
    vx[0] = (C.PLANE_GAME_LENGTH * (vx[0] - C.PLANE_LENGTH / 2)) / C.PLANE_LENGTH;
    vy[0] = (C.PLANE_GAME_LENGTH * (-vy[0] + C.PLANE_HEIGHT / 2)) / C.PLANE_LENGTH;

    var gameRadius = (C.PLANE_GAME_LENGTH * C.SOLDIER_RADIUS) / C.PLANE_LENGTH;
    res.fireAngle = this._getStartAngle(vx[0], gameRadius);
    if (!Number.isNaN(res.fireAngle) && Number.isFinite(res.fireAngle)) {
      vx[0] = vx[0] + gameRadius * Math.cos(res.fireAngle);
      vy[0] = vy[0] + gameRadius * Math.sin(res.fireAngle);
    }

    var offSet = -pf.evaluateFunction(vx[0], 0, 0) + vy[0];
    var stepSize = C.STEP_SIZE;
    res.numSteps = C.FUNC_MAX_STEPS;

    for (var i = 1; i < C.FUNC_MAX_STEPS; i++) {
      var tempStepSize = stepSize;
      vx[i] = vx[i - 1] + tempStepSize;
      vy[i] = pf.evaluateFunction(vx[i], 0, 0) + offSet;

      var endFunc = false;
      while (Math.pow(vx[i] - vx[i - 1], 2) + Math.pow(vy[i] - vy[i - 1], 2) > C.FUNC_MAX_STEP_DISTANCE_SQUARED) {
        if (vx[i] - vx[i - 1] > C.FUNC_MIN_X_STEP_DISTANCE) {
          tempStepSize = tempStepSize / 2;
          vx[i] = vx[i - 1] + tempStepSize;
          vy[i] = pf.evaluateFunction(vx[i], 0, 0) + offSet;
        } else { endFunc = true; break; }
      }
      if (endFunc) { res.numSteps = i; break; }

      if (checkStep(res, obstacle, players, numPlayers, currentTurn, inverted, vx[i], vy[i], i)) break;
    }
    finishLast(res, inverted);
    return res;
  };

  // processRK4Range (FST_ODE)
  GwFunction.prototype.processRK4Range = function (obstacle, players, numPlayers, currentTurn, inverted) {
    var res = this._newResult(numPlayers);
    var pf = this.polish;
    var vx = res.valuesX, vy = res.valuesY;
    var stepSize = C.STEP_SIZE;
    var cur = players[currentTurn].soldiers[players[currentTurn].currentTurnSoldier];

    vx[0] = cur.x; vy[0] = cur.y;
    if (inverted) vx[0] = C.PLANE_LENGTH - vx[0];
    vx[0] = (C.PLANE_GAME_LENGTH * (vx[0] - C.PLANE_LENGTH / 2)) / C.PLANE_LENGTH;
    vy[0] = (C.PLANE_GAME_LENGTH * (-vy[0] + C.PLANE_HEIGHT / 2)) / C.PLANE_LENGTH;

    var gameRadius = (C.PLANE_GAME_LENGTH * C.SOLDIER_RADIUS) / C.PLANE_LENGTH;
    res.fireAngle = this._getRK4StartAngle(vx[0], vy[0], gameRadius);
    vx[0] = vx[0] + gameRadius * Math.cos(res.fireAngle);
    vy[0] = vy[0] + gameRadius * Math.sin(res.fireAngle);

    res.numSteps = C.FUNC_MAX_STEPS;
    for (var i = 1; i < C.FUNC_MAX_STEPS; i++) {
      var h = stepSize;
      var k1 = pf.evaluateFunction(vx[i - 1], vy[i - 1], 0);
      var k2 = pf.evaluateFunction(vx[i - 1] + 0.5 * h, vy[i - 1] + 0.5 * h * k1, 0);
      var k3 = pf.evaluateFunction(vx[i - 1] + 0.5 * h, vy[i - 1] + 0.5 * h * k2, 0);
      var k4 = pf.evaluateFunction(vx[i - 1] + h, vy[i - 1] + h * k3, 0);
      vy[i] = vy[i - 1] + (h / 6) * (k1 + 2 * k2 + 2 * k3 + k4);
      vx[i] = vx[i - 1] + h;

      var endFunc = false;
      while (Math.pow(vx[i] - vx[i - 1], 2) + Math.pow(vy[i] - vy[i - 1], 2) > C.FUNC_MAX_STEP_DISTANCE_SQUARED &&
             vx[i] - vx[i - 1] > C.FUNC_MIN_X_STEP_DISTANCE) {
        if (vx[i] - vx[i - 1] > C.FUNC_MIN_X_STEP_DISTANCE) {
          h = h / 2;
          k1 = pf.evaluateFunction(vx[i - 1], vy[i - 1], 0);
          k2 = pf.evaluateFunction(vx[i - 1] + 0.5 * h, vy[i - 1] + 0.5 * h * k1, 0);
          k3 = pf.evaluateFunction(vx[i - 1] + 0.5 * h, vy[i - 1] + 0.5 * h * k2, 0);
          k4 = pf.evaluateFunction(vx[i - 1] + h, vy[i - 1] + h * k3, 0);
          vy[i] = vy[i - 1] + (h / 6) * (k1 + 2 * k2 + 2 * k3 + k4);
          vx[i] = vx[i - 1] + h;
        } else { endFunc = true; break; }
      }
      if (endFunc) { res.numSteps = i; break; }

      if (checkStep(res, obstacle, players, numPlayers, currentTurn, inverted, vx[i], vy[i], i)) break;
    }
    finishLast(res, inverted);
    return res;
  };

  // processRK42Range (SND_ODE)
  GwFunction.prototype.processRK42Range = function (obstacle, players, numPlayers, currentTurn, angle, inverted) {
    var res = this._newResult(numPlayers);
    res.valuesDY = new Float64Array(C.FUNC_MAX_STEPS);
    var pf = this.polish;
    var vx = res.valuesX, vy = res.valuesY, vdy = res.valuesDY;
    var stepSize = C.STEP_SIZE;
    var cur = players[currentTurn].soldiers[players[currentTurn].currentTurnSoldier];

    vx[0] = cur.x;
    if (inverted) vx[0] = C.PLANE_LENGTH - vx[0];
    vx[0] = vx[0] + C.SOLDIER_RADIUS * Math.cos(angle);
    vy[0] = cur.y;
    vy[0] = vy[0] - C.SOLDIER_RADIUS * Math.sin(angle);

    vx[0] = (C.PLANE_GAME_LENGTH * (vx[0] - C.PLANE_LENGTH / 2)) / C.PLANE_LENGTH;
    vy[0] = (C.PLANE_GAME_LENGTH * (-vy[0] + C.PLANE_HEIGHT / 2)) / C.PLANE_LENGTH;
    vdy[0] = Math.tan(angle);
    res.fireAngle = angle;

    res.numSteps = C.FUNC_MAX_STEPS;
    for (var i = 1; i < C.FUNC_MAX_STEPS; i++) {
      var h = stepSize;
      var x1, y1, y2, k11, k12, k21, k22, k31, k32, k41, k42;

      x1 = vx[i - 1]; y1 = vy[i - 1]; y2 = vdy[i - 1];
      k11 = y2; k12 = pf.evaluateFunction(x1, y1, y2);
      x1 = vx[i - 1] + h / 2; y1 = vy[i - 1] + (h / 2) * k11; y2 = vdy[i - 1] + (h / 2) * k12;
      k21 = y2; k22 = pf.evaluateFunction(x1, y1, y2);
      y1 = vy[i - 1] + (h / 2) * k21; y2 = vdy[i - 1] + (h / 2) * k22;
      k31 = y2; k32 = pf.evaluateFunction(x1, y1, y2);
      x1 = vx[i - 1] + h; y1 = vy[i - 1] + h * k31; y2 = vdy[i - 1] + h * k32;
      k41 = y2; k42 = pf.evaluateFunction(x1, y1, y2);

      vx[i] = vx[i - 1] + h;
      vy[i] = vy[i - 1] + (h / 6) * (k11 + 2 * k21 + 2 * k31 + k41);
      vdy[i] = vdy[i - 1] + (h / 6) * (k12 + 2 * k22 + 2 * k32 + k42);

      var endFunc = false;
      while (Math.pow(vx[i] - vx[i - 1], 2) + Math.pow(vy[i] - vy[i - 1], 2) > C.FUNC_MAX_STEP_DISTANCE_SQUARED &&
             vx[i] - vx[i - 1] > C.FUNC_MIN_X_STEP_DISTANCE) {
        if (vx[i] - vx[i - 1] > C.FUNC_MIN_X_STEP_DISTANCE) {
          h = h / 2;
          x1 = vx[i - 1]; y1 = vy[i - 1]; y2 = vdy[i - 1];
          k11 = y2; k12 = pf.evaluateFunction(x1, y1, y2);
          x1 = vx[i - 1] + h / 2; y1 = vy[i - 1] + (h / 2) * k11; y2 = vdy[i - 1] + (h / 2) * k12;
          k21 = y2; k22 = pf.evaluateFunction(x1, y1, y2);
          y1 = vy[i - 1] + (h / 2) * k21; y2 = vdy[i - 1] + (h / 2) * k22;
          k31 = y2; k32 = pf.evaluateFunction(x1, y1, y2);
          x1 = vx[i - 1] + h; y1 = vy[i - 1] + h * k31; y2 = vdy[i - 1] + h * k32;
          k41 = y2; k42 = pf.evaluateFunction(x1, y1, y2);
          vx[i] = vx[i - 1] + h;
          vy[i] = vy[i - 1] + (h / 6) * (k11 + 2 * k21 + 2 * k31 + k41);
          vdy[i] = vdy[i - 1] + (h / 6) * (k12 + 2 * k22 + 2 * k32 + k42);
        } else { endFunc = true; break; }
      }
      if (endFunc) { res.numSteps = i; break; }

      if (checkStep(res, obstacle, players, numPlayers, currentTurn, inverted, vx[i], vy[i], i)) break;
    }
    finishLast(res, inverted);
    return res;
  };

  // Convenience: dispatch by mode, build players array from raw soldier data.
  GwFunction.prototype.process = function (mode, obstacle, players, currentTurn, angle, inverted) {
    switch (mode) {
      case C.NORMAL_FUNC: return this.processFunctionRange(obstacle, players, players.length, currentTurn, inverted);
      case C.FST_ODE: return this.processRK4Range(obstacle, players, players.length, currentTurn, inverted);
      case C.SND_ODE: return this.processRK42Range(obstacle, players, players.length, currentTurn, angle, inverted);
    }
  };

  GW.GwFunction = GwFunction;
  if (typeof module !== 'undefined' && module.exports) module.exports = GW;
})(typeof window !== 'undefined' ? window : (typeof global !== 'undefined' ? global : this));
