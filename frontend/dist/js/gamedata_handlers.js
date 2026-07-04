// Graphwar client — incoming message handlers (port of GameData.handleMessage
// and its helpers). Augments GW.GameData. The START_GAME / FIRE_FUNC paths
// drive the verified trajectory engine so kill decisions match Java clients.
(function (root) {
  'use strict';
  var GW = root.GW;
  var NP = GW.NP, C = GW.C;
  var GameData = GW.GameData;
  var Constants = GW.GameConstants;

  GameData.prototype.handleMessage = function (message) {
    if (typeof message !== 'string' || message.length > 8192) return;
    var info = message.split('&');
    var type = parseInt(info[0], 10);
    try {
      switch (type) {
        case NP.NO_INFO: break;
        case NP.ADD_PLAYER: this._addPlayerMsg(info); break;
        case NP.SET_TEAM: this._setTeamMsg(info); break;
        case NP.REMOVE_PLAYER: this._removePlayerMsg(info); break;
        case NP.ADD_SOLDIER: this._addSoldierMsg(info); break;
        case NP.REMOVE_SOLDIER: this._removeSoldierMsg(info); break;
        case NP.CHAT_MSG: this._chatMsg(info); break;
        case NP.SET_MODE: this._setModeMsg(info); break;
        case NP.SET_READY: this._setReadyMsg(info); break;
        case NP.START_GAME: this._startGameMsg(info); break;
        case NP.NEXT_TURN: this._nextTurnMsg(info); break;
        case NP.FIRE_FUNC: this._fireFuncMsg(info); break;
        case NP.GAME_FINISHED: this._finishGameMsg(info); break;
        case NP.SET_ANGLE: this._setAngleMsg(info); break;
        case NP.NEW_LEADER: this._newLeaderMsg(info); break;
        case NP.START_COUNTDOWN: this._startCountdownMsg(info); break;
        case NP.REORDER: this._reorderMsg(info); break;
        case NP.FUNCTION_PREVIEW: this._previewMsg(info); break;
        case NP.GAME_FULL:
        case NP.DISCONNECT:
          this.emit('kicked', type === NP.GAME_FULL ? 'Game is full.' : 'Disconnected by server.');
          if (this.conn) this.conn.close();
          this.kickFromGame();
          break;
      }
    } catch (e) {
      this.emit('protocolerror', message, e);
    }
  };

  GameData.prototype._addPlayerMsg = function (info) {
    var id = parseInt(info[1], 10);
    var name = GW.sanitizeText(GW.urlDecode(info[2]), 48);
    var team = parseInt(info[3], 10);
    var local = parseInt(info[4], 10) !== 0;
    var numSoldiers = parseInt(info[5], 10);
    var ready = parseInt(info[6], 10) !== 0;
    var p = this._makePlayer(name, id, team, local, numSoldiers, ready);
    this.players.push(p);
    this.emit('player_added', p);
    this.emit('chat', null, name + ' has joined the game.');
    this.emit('roster');
  };

  GameData.prototype._makePlayer = function (name, id, team, local, numSoldiers, ready) {
    var s = [];
    for (var i = 0; i < 4; i++) s.push({ x: 0, y: 0, angle: 0, alive: false, exploding: false, timeExplodingStarted: 0, killPosition: 0, fn: '' });
    return {
      name: name, id: id, team: team, local: local,
      numSoldiers: numSoldiers, ready: ready, soldiers: s,
      currentTurnSoldier: 0, disconnected: false,
      isBot: /^Computer\s+\d+$/i.test(name),
      botLevel: /^Computer\s+\d+$/i.test(name) ? 50 : null,
      color: GW.colorForId ? GW.colorForId(id) : '#66ccff'
    };
  };

  GameData.prototype._setTeamMsg = function (info) {
    var team = parseInt(info[1], 10), id = parseInt(info[2], 10);
    var p = this.getPlayer(id); if (p) { p.team = team; this.emit('roster'); }
  };

  GameData.prototype._removePlayerMsg = function (info) {
    var id = parseInt(info[1], 10);
    var p = this.getPlayer(id); if (!p) return;
    this.emit('chat', null, p.name + ' has left the game.');
    if (this.gameState === Constants.GAME) {
      if (this.players[this.currentTurn] && this.players[this.currentTurn].id === id && !this.drawingFunction) this.nextTurn();
      for (var i = 0; i < p.numSoldiers; i++) {
        if (p.soldiers[i].alive) { p.soldiers[i].alive = false; this._setExploding(p.soldiers[i], true); }
      }
      p.disconnected = true;
    } else {
      var idx = this.players.indexOf(p);
      if (idx >= 0) this.players.splice(idx, 1);
      this.emit('roster');
      if (!this._haveLocals()) this.disconnectKick();
    }
  };

  GameData.prototype._haveLocals = function () {
    return this.players.some(function (p) { return p.local; });
  };

  GameData.prototype._addSoldierMsg = function (info) {
    var p = this.getPlayer(parseInt(info[1], 10)); if (p) { p.numSoldiers++; this.emit('roster'); }
  };
  GameData.prototype._removeSoldierMsg = function (info) {
    var p = this.getPlayer(parseInt(info[1], 10)); if (p) { p.numSoldiers--; this.emit('roster'); }
  };

  GameData.prototype._chatMsg = function (info) {
    var id = parseInt(info[1], 10);
    var msg = GW.sanitizeText(GW.urlDecode(info[2]), 600);
    var p = this.getPlayer(id);
    this.emit('chat', p, msg);
  };

  GameData.prototype._setModeMsg = function (info) {
    this.gameMode = parseInt(info[1], 10);
    this.emit('mode', this.gameMode);
    this.emit('roster');
  };

  GameData.prototype._setReadyMsg = function (info) {
    var id = parseInt(info[1], 10), ready = parseInt(info[2], 10) !== 0;
    var p = this.getPlayer(id); if (!p) return;
    if (p.ready === ready) return;
    p.ready = ready;
    this.emit('roster');
    if (!ready && this.countingDown) { this.countingDown = false; this.emit('chat', null, 'Game start cancelled.'); }
  };

  GameData.prototype._newLeaderMsg = function () {
    this.leader = true;
    this.emit('leader');
    this.emit('chat', null, 'You are now the room leader.');
  };

  GameData.prototype._startCountdownMsg = function () {
    var self = this;
    if (this.countingDown) return;
    this.countingDown = true;
    var n = 5;
    var tick = function () {
      if (!self.countingDown) return;
      self.emit('chat', null, 'Game starting in ' + n + '...');
      n--;
      if (n > 0) setTimeout(tick, 1000); else self.countingDown = false;
    };
    tick();
  };

  GameData.prototype._reorderMsg = function (info) {
    var np = [];
    for (var i = 1; i < info.length; i++) np.push(this.getPlayer(parseInt(info[i], 10)));
    this.players = np.filter(Boolean);
  };

  GameData.prototype._previewMsg = function (info) {
    if (info.length === 3 && !this.drawingFunction) {
      var id = parseInt(info[1], 10);
      var fn = GW.sanitizeText(GW.urlDecode(info[2]), 4096);
      var p = this.getPlayer(id);
      if (this.players[this.currentTurn] && this.players[this.currentTurn].id === id && p && !p.local) {
        this.previewFunction = fn;
        this.emit('preview', fn);
      }
    }
  };

  GameData.prototype._setExploding = function (soldier, v) {
    soldier.exploding = v;
    if (v) soldier.timeExplodingStarted = Date.now();
  };

  // ---- START_GAME: parse circles + soldier positions + start player ----
  GameData.prototype._startGameMsg = function (info) {
    this.gameState = Constants.GAME;
    this.drawingFunction = false;
    this.exploding = false;

    var numCircles = parseInt(info[1], 10);
    // Sanity-bound against a malformed/hostile START_GAME (the field count must
    // also be consistent). Each circle is 3 ints; bail if the message is short.
    if (!(numCircles >= 0) || numCircles > 10000 || info.length < 2 + numCircles * 3) {
      this.emit('protocolerror', 'bad START_GAME numCircles=' + numCircles);
      return;
    }
    var circles = new Array(numCircles * 3);
    for (var i = 0; i < numCircles * 3; i++) circles[i] = parseInt(info[2 + i], 10);
    this.obstacle = new GW.Obstacle(numCircles, circles);
    this.circlesRaw = circles;

    var idx = 3 * numCircles + 2; // first soldier coordinate
    for (var pi = 0; pi < this.players.length; pi++) {
      var p = this.players[pi];
      for (var s = 0; s < p.numSoldiers; s++) {
        var x = parseInt(info[idx], 10), y = parseInt(info[idx + 1], 10);
        idx += 2;
        p.soldiers[s] = { x: x, y: y, angle: 0, alive: true, exploding: false, timeExplodingStarted: 0, killPosition: 0, fn: '' };
      }
      p.currentTurnSoldier = 0;
    }

    var startPlayer = Math.abs(parseInt(info[idx], 10)) % this.players.length;
    this.currentTurn = startPlayer;
    this.timeTurnStarted = Date.now();
    this.turnTimeUp = false;
    this.nextTurnSent = false;
    this.previewFunction = '';

    var cur = this.players[this.currentTurn];
    if (cur && cur.local) this.lastLocalHumanPlayer = cur;

    this.emit('game_started');
  };

  GameData.prototype._nextTurnMsg = function () {
    if (this.checkGameFinished()) this.conn.send('' + NP.GAME_FINISHED);
    var n = this.players.length;
    for (var i = 0; i < n; i++) {
      this.currentTurn = (this.currentTurn + 1) % n;
      if (this._playerNextTurn(this.players[this.currentTurn])) break;
    }
    var cur = this.players[this.currentTurn];
    if (cur && cur.local) this.lastLocalHumanPlayer = cur;
    this.timeTurnStarted = Date.now();
    this.turnTimeUp = false;
    this.drawingFunction = false;
    this.exploding = false;
    this.nextTurnSent = false;
    this.previewFunction = '';
    this.funcResult = null;
    this.emit('next_turn');
  };

  GameData.prototype._playerNextTurn = function (p) {
    for (var i = 0; i < p.numSoldiers; i++) {
      p.currentTurnSoldier = (p.currentTurnSoldier + 1) % p.numSoldiers;
      if (p.soldiers[p.currentTurnSoldier].alive) return true;
    }
    return false;
  };

  // ---- FIRE_FUNC: compute trajectory with the verified engine ----
  GameData.prototype._fireFuncMsg = function (info) {
    if (info.length !== 3 || this.drawingFunction) return;
    var id = parseInt(info[1], 10);
    var functionStr = GW.sanitizeText(GW.urlDecode(info[2]), 4096);
    if (!this.players[this.currentTurn] || this.players[this.currentTurn].id !== id) return;
    var player = this.getPlayer(id);
    // Match Java: processFunction throws on a malformed function BEFORE drawing
    // state is set, so the message is effectively ignored. We must not flip
    // drawingFunction=true with a null result, or this client would never send
    // READY_NEXT_TURN and would stall the turn for everyone. (The engine is
    // verified bit-close to Java so a peer-accepted function parsing here is
    // near-impossible, but we stay faithful and safe.)
    if (!this._processFunction(player, functionStr)) return;
    this.drawingFunction = true;
    this.timeStartedDrawingFunction = Date.now();
    this.emit('fire', player, functionStr, this.funcResult);
    if (this.sayFunc) this.emit('chat', player, functionStr);
  };

  GameData.prototype._processFunction = function (player, functionStr) {
    var fn;
    try { fn = new GW.GwFunction(functionStr); }
    catch (e) { this.funcResult = null; return false; }

    player.soldiers[player.currentTurnSoldier].fn = functionStr;
    var inverted = player.team !== Constants.TEAM1;
    var angle = player.soldiers[player.currentTurnSoldier].angle;
    var res = fn.process(this.gameMode, this.obstacle, this.players, this.currentTurn, angle, inverted);

    // build soldiersHit list with kill positions
    this.soldiersHit = [];
    for (var i = 0; i < res.hits.length; i++) {
      var h = res.hits[i];
      var soldier = this.players[h.player].soldiers[h.soldier];
      soldier.killPosition = h.position;
      this.soldiersHit.push(soldier);
    }
    player.soldiers[player.currentTurnSoldier].angle = res.fireAngle;
    this.func = fn;
    this.funcResult = res;
    this.emit('angle');
    return true;
  };

  GameData.prototype._setAngleMsg = function (info) {
    var id = parseInt(info[1], 10);
    var soldierIndex = parseInt(info[2], 10);
    var angle = parseFloat(info[3]);
    var p = this.getPlayer(id);
    // bounds-check the index (a hostile server could send an out-of-range one)
    if (p && !p.local && p.soldiers && soldierIndex >= 0 && soldierIndex < p.soldiers.length &&
        p.soldiers[soldierIndex] && isFinite(angle)) {
      p.soldiers[soldierIndex].angle = angle;
      this.emit('angle');
    }
  };

  GameData.prototype._finishGameMsg = function () {
    this.gameState = Constants.PRE_GAME;
    this.drawingFunction = false;
    this.exploding = false;
    this.nextTurnSent = false;
    // determine winner for chat
    for (var i = 0; i < this.players.length; i++) {
      var p = this.players[i];
      for (var j = 0; j < p.soldiers.length; j++) {
        if (p.soldiers[j].alive) { this.emit('chat', null, p.name + ' won the game.'); break; }
      }
    }
    this.players = this.players.filter(function (p) { return !p.disconnected; });
    this.emit('game_finished');
  };

  // ---- animation tick (port of getCurrentFunctionPosition / getTimeExploding) ----
  GameData.prototype.getCurrentFunctionPosition = function () {
    if (!this.funcResult) return 0;
    if (this.exploding) return this.funcResult.numSteps;
    var numDrawSteps = Math.trunc((Date.now() - this.timeStartedDrawingFunction) * Constants.FUNCTION_VELOCITY / 1000);
    if (numDrawSteps > this.funcResult.numSteps && this.drawingFunction) {
      numDrawSteps = this.funcResult.numSteps;
      this.exploding = true;
      this.timeStartedExploding = Date.now();
      var lastX = this.funcResult.lastX, lastY = this.funcResult.lastY;
      if (this.isFunctionReversed())
        this.obstacle.setExplosion(Constants.PLANE_LENGTH - Math.trunc(lastX), Math.trunc(lastY), Constants.EXPLOSION_RADIUS);
      else
        this.obstacle.setExplosion(Math.trunc(lastX), Math.trunc(lastY), Constants.EXPLOSION_RADIUS);
      this.obstacle.explodePoint();
      this.emit('explosion');
    }
    for (var i = 0; i < this.soldiersHit.length; i++) {
      var sol = this.soldiersHit[i];
      if (sol.alive) {
        if (sol.exploding) {
          if (Date.now() - sol.timeExplodingStarted > Constants.SOLDIER_MAX_DEATH_TIME) sol.exploding = false;
        } else if (numDrawSteps > sol.killPosition) {
          sol.exploding = true; sol.timeExplodingStarted = Date.now(); sol.alive = false;
        }
      }
    }
    return numDrawSteps;
  };

  GameData.prototype.getTimeExploding = function () {
    var t = Date.now() - this.timeStartedExploding;
    if (t > Constants.NEXT_TURN_DELAY && this.exploding && !this.nextTurnSent) {
      this.nextTurn();
      this.nextTurnSent = true;
    }
    return t;
  };

  GameData.prototype.updateDrawingStuff = function () {
    if (this.drawingFunction) this.getCurrentFunctionPosition();
    if (this.exploding) this.getTimeExploding();
    this.getRemainingTime();
  };

  GameData.prototype.kickFromGame = function () {
    this.gameState = Constants.NONE;
    this.drawingFunction = false;
    this.exploding = false;
    this.leader = false;
    this.emit('left');
  };
  GameData.prototype.disconnectKick = function () {
    if (this.conn) { this.conn.send('' + NP.DISCONNECT); this.conn.close(); }
    this.kickFromGame();
  };
})(typeof window !== 'undefined' ? window : this);
