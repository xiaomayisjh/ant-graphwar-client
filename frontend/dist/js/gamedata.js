// Graphwar client game state — port of GameData.java's networked logic, minus
// the Swing/AWT coupling. Drives the verified trajectory engine and emits
// events for the renderer/UI. One instance per room connection.
(function (root) {
  'use strict';
  var GW = root.GW;
  var NP = GW.NP, C = GW.C;

  var Constants = {
    PRE_GAME: 1, GAME: 2, NONE: 0,
    TURN_TIME: 60000,
    FUNCTION_VELOCITY: 1500,
    NEXT_TURN_DELAY: 3000,
    EXPLOSION_RADIUS: 12,
    PLANE_LENGTH: 770, PLANE_HEIGHT: 450,
    SOLDIER_MAX_DEATH_TIME: 6000,
    MAX_PLAYERS: 10,
    NORMAL_FUNC: 0, FST_ODE: 1, SND_ODE: 2,
    TEAM1: 1, TEAM2: 2
  };

  function Emitter() { this._h = {}; }
  Emitter.prototype.on = function (ev, fn) { (this._h[ev] = this._h[ev] || []).push(fn); return this; };
  Emitter.prototype.emit = function (ev) {
    var a = Array.prototype.slice.call(arguments, 1);
    (this._h[ev] || []).forEach(function (fn) { fn.apply(null, a); });
  };

  // ---- Player / Soldier client models ----
  function makeSoldier(x, y, alive) {
    return {
      x: x || 0, y: y || 0, angle: 0, alive: !!alive,
      exploding: false, timeExplodingStarted: 0, killPosition: 0, fn: ''
    };
  }
  function makePlayer(name, id, team, local, numSoldiers, ready) {
    var s = [];
    for (var i = 0; i < 4; i++) s.push(makeSoldier(0, 0, false));
    return {
      name: name, id: id, team: team, local: local,
      numSoldiers: numSoldiers, ready: ready, soldiers: s,
      currentTurnSoldier: 0, disconnected: false,
      color: GW.colorForId ? GW.colorForId(id) : '#6cf'
    };
  }

  function GameData(opts) {
    Emitter.call(this);
    this.conn = null;
    this.opts = opts || {};
    this.players = [];
    this.obstacle = null;
    this.gameMode = Constants.NORMAL_FUNC;
    this.gameState = Constants.NONE;
    this.leader = false;
    this.currentTurn = -1;
    this.lastLocalHumanPlayer = null;
    this.timeTurnStarted = 0;
    this.turnTimeUp = false;
    this.nextTurnSent = false;
    this.drawingFunction = false;
    this.timeStartedDrawingFunction = 0;
    this.exploding = false;
    this.timeStartedExploding = 0;
    this.soldiersHit = [];
    this.func = null;           // current GwFunction result
    this.funcResult = null;
    this.sayFunc = true;
    this.previewFunction = '';  // opponent's live preview (FUNCTION_PREVIEW)
    this.nextPCNames = [];      // not used (no AI) but kept for parity
  }
  GameData.prototype = Object.create(Emitter.prototype);

  GameData.prototype.connect = function (host, port) {
    var self = this;
    this.gameState = Constants.PRE_GAME;
    this.players = [];
    this.currentTurn = -1;
    this.lastLocalHumanPlayer = null;   // reset so terrain orientation is fresh per game
    this.drawingFunction = false;
    this.exploding = false;
    this.nextTurnSent = false;
    this.conn = new GW.Connection({
      bridgeUrl: this.opts.bridgeUrl,
      directUrl: this.opts.directUrl ? this.opts.directUrl(host, port) : null,
      host: host, port: port,
      onOpen: function () { self.emit('connected'); },
      onMessage: function (m) { self.handleMessage(m); },
      onClose: function (info) { self.emit('disconnected', info); self.kickFromGame(); },
      onError: function (info) { self.emit('neterror', info); }
    });
    this.conn.open();
  };

  GameData.prototype.disconnect = function () {
    if (this.conn) { this.conn.send('' + NP.DISCONNECT); this.conn.close(); }
    this.gameState = Constants.NONE;
  };

  GameData.prototype.getPlayer = function (id) {
    for (var i = 0; i < this.players.length; i++) if (this.players[i].id === id) return this.players[i];
    return null;
  };
  GameData.prototype.getFirstLocalPlayer = function () {
    for (var i = 0; i < this.players.length; i++) if (this.players[i].local) return this.players[i];
    return null;
  };
  GameData.prototype.getCurrentTurnPlayer = function () { return this.players[this.currentTurn]; };

  GameData.prototype.isTerrainReversed = function () {
    var ref = this.lastLocalHumanPlayer || this.getFirstLocalPlayer();
    return ref ? ref.team === Constants.TEAM2 : false;
  };
  GameData.prototype.isFunctionReversed = function () {
    var p = this.getCurrentTurnPlayer();
    return p ? p.team === Constants.TEAM2 : false;
  };

  // ---- outgoing actions ----
  GameData.prototype.addPlayer = function (name) {
    if (this.players.length < Constants.MAX_PLAYERS)
      this.conn.send([NP.ADD_PLAYER, GW.urlEncode(GW.sanitizeText(name, 48))]);
  };
  GameData.prototype.removePlayer = function (p) { this.conn.send([NP.REMOVE_PLAYER, p.id]); };
  GameData.prototype.addSoldier = function (p) { this.conn.send([NP.ADD_SOLDIER, p.id]); };
  GameData.prototype.removeSoldier = function (p) { this.conn.send([NP.REMOVE_SOLDIER, p.id]); };
  GameData.prototype.switchSide = function (p) {
    var other = p.team === Constants.TEAM1 ? Constants.TEAM2 : Constants.TEAM1;
    this.conn.send([NP.SET_TEAM, other, p.id]);
  };
  GameData.prototype.setReady = function (p, ready) { this.conn.send([NP.SET_READY, p.id, ready ? 1 : 0]); };
  GameData.prototype.nextMode = function () { this.conn.send('' + NP.NEXT_MODE); };

  GameData.prototype.sendChatMessage = function (chat) {
    chat = GW.sanitizeText(chat, 600);
    var p = this.getFirstLocalPlayer();
    var id = p ? p.id : -1;
    this.conn.send([NP.CHAT_MSG, id, GW.urlEncode(chat)]);
    this.handleCommands(chat);
  };

  GameData.prototype.sendFunctionPreview = function (preview) {
    preview = GW.sanitizeText(preview, 4096);
    var cur = this.getCurrentTurnPlayer();
    if (cur && cur.local && !this.drawingFunction)
      this.conn.send([NP.FUNCTION_PREVIEW, cur.id, GW.urlEncode(preview)]);
  };

  GameData.prototype.sendFunction = function (functionStr) {
    functionStr = GW.sanitizeText(functionStr, 4096);
    var cur = this.getCurrentTurnPlayer();
    if (cur && cur.local && !this.drawingFunction) {
      try { new GW.PolishFunction(functionStr); } // validate; bad -> don't send
      catch (e) { this.emit('badfunction', functionStr); return; }
      this.conn.send([NP.FIRE_FUNC, cur.id, GW.urlEncode(functionStr)]);
    }
  };

  GameData.prototype.setAngle = function (angle) {
    var cur = this.getCurrentTurnPlayer();
    if (cur && cur.local) {
      cur.soldiers[cur.currentTurnSoldier].angle = angle;
      this.conn.send([NP.SET_ANGLE, cur.id, cur.currentTurnSoldier, angle]);
      this.emit('angle');
    }
  };

  GameData.prototype.handleCommands = function (msg) {
    if (msg.charAt(0) !== '-') return;
    var m = msg.toLowerCase();
    if (m === '-sayfunc') this.sayFunc = true;
    else if (m === '-stopsayfunc') this.sayFunc = false;
    else if (m === '-shownext') this.emit('shownext', true);
    else if (m === '-stopshownext') this.emit('shownext', false);
  };

  // ---- turn / time ----
  GameData.prototype.getRemainingTime = function () {
    var t = Constants.TURN_TIME - (Date.now() - this.timeTurnStarted);
    if (this.drawingFunction || this.exploding)
      t = Constants.TURN_TIME - (this.timeStartedDrawingFunction - this.timeTurnStarted);
    if (t < 0) {
      t = 0;
      if (!this.turnTimeUp) {
        this.turnTimeUp = true;
        if (this.gameState === Constants.GAME) this.conn.send('' + NP.TIME_UP);
      }
    }
    return t;
  };

  GameData.prototype.checkGameFinished = function () {
    var t1 = false, t2 = false;
    for (var i = 0; i < this.players.length; i++) {
      var p = this.players[i];
      for (var j = 0; j < p.numSoldiers; j++) {
        if (p.soldiers[j].alive) { if (p.team === Constants.TEAM1) t1 = true; else t2 = true; }
      }
    }
    return !t1 || !t2;
  };

  GameData.prototype.nextTurn = function () {
    if (this.checkGameFinished()) this.conn.send('' + NP.GAME_FINISHED);
    else this.conn.send('' + NP.READY_NEXT_TURN);
  };

  root.GW.GameData = GameData;
  root.GW.GameConstants = Constants;
  // message handling defined in gamedata_handlers.js
})(typeof window !== 'undefined' ? window : this);
