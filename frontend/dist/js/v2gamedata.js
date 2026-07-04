// Graphwar II state adapter. It maps Rust JSON events onto the same small
// GameData surface the existing UI already consumes.
(function (root) {
  'use strict';
  var GW = root.GW;
  var C = GW.GameConstants;

  function Emitter() { this._h = {}; }
  Emitter.prototype.on = function (ev, fn) { (this._h[ev] = this._h[ev] || []).push(fn); return this; };
  Emitter.prototype.emit = function (ev) {
    var a = Array.prototype.slice.call(arguments, 1);
    (this._h[ev] || []).forEach(function (fn) { fn.apply(null, a); });
  };

  function cleanText(s, n) {
    if (GW.sanitizeText) return GW.sanitizeText(s == null ? '' : String(s), n || 256);
    s = s == null ? '' : String(s);
    return s.length > (n || 256) ? s.slice(0, n || 256) : s;
  }

  function asId(v) {
    var n = Number(v);
    return isFinite(n) ? Math.trunc(n) : null;
  }

  function asNum(v) {
    var n = Number(v);
    return isFinite(n) ? n : null;
  }

  function gameXToPixel(x) {
    x = asNum(x);
    return x == null ? null : GW.convertX(x);
  }

  function gameYToPixel(y) {
    y = asNum(y);
    return y == null ? null : GW.convertY(y);
  }

  function gameRadiusToPixel(r) {
    r = asNum(r);
    var planeLen = (GW.C && GW.C.PLANE_LENGTH) || (GW.GameConstants && GW.GameConstants.PLANE_LENGTH) || 770;
    var gameLen = (GW.C && GW.C.PLANE_GAME_LENGTH) || (GW.GameConstants && GW.GameConstants.PLANE_GAME_LENGTH) || 50;
    return r == null ? null : Math.max(1, Math.abs(r) * planeLen / gameLen);
  }

  function botLabel(level) {
    level = asId(level);
    if (level == null) level = 0;
    return '机器人 ' + Math.max(0, Math.min(5, level));
  }

  function makeSoldier(id) {
    var radius = (GW.C && GW.C.SOLDIER_RADIUS) || (GW.GameConstants && GW.GameConstants.SOLDIER_RADIUS) || 7;
    return {
      id: id, x: 0, y: 0, radius: radius, angle: 0, alive: true,
      exploding: false, timeExplodingStarted: 0, killPosition: 0, fn: ''
    };
  }

  function makePlayer(id) {
    return {
      id: id, entityId: id, name: '玩家 ' + id, team: C.TEAM1, local: false,
      ready: false, connectionID: null, numSoldiers: 0, soldiers: [], currentTurnSoldier: 0,
      disconnected: false, color: GW.colorForId ? GW.colorForId(id) : '#66ccff'
    };
  }

  function teamToConst(team) {
    if (team === 'Team2' || team === 2 || team === true) return C.TEAM2;
    return C.TEAM1;
  }

  var V2_META = {
    axis: {
      NoAxis: { label: '无坐标轴', icon: 'axis_option_0.svg', hint: '不显示坐标轴和网格' },
      OnlyMain: { label: '仅主轴', icon: 'axis_option_1.svg', hint: '只显示 x/y 主轴' },
      EveryFive: { label: '每 5 格', icon: 'axis_option_2.svg', hint: '每 5 个单位显示辅助网格' },
      EveryUnit: { label: '每 1 格', icon: 'axis_option_3.svg', hint: '每 1 个单位显示辅助网格' }
    },
    func: {
      NormalFunction: { label: 'f(x)', icon: 'fx_option.svg', hint: '输入普通函数作为弹道' },
      DiffEqFunction: { label: 'dx/dy', icon: 'dx_dy_option.svg', hint: '输入关于 x 和 y 的一阶微分方程' },
      FirstOrderODE: { label: 'dx/dy', icon: 'dx_dy_option.svg', hint: '兼容旧协议：一阶微分方程' },
      SecondOrderODE: { label: "y''", icon: 'dx_dy_option.svg', hint: '兼容旧协议：二阶微分方程' }
    },
    turn: {
      SequentialTurns: { label: '轮流发射', icon: 'turns_sequential.svg', hint: '玩家按顺序输入并发射' },
      SimultaneousTurns: { label: '同时确认', icon: 'turns_simultaneous.svg', hint: '双方都确认函数后统一结算弹道' }
    },
    time: {
      Timer30s: { label: '30 秒', icon: 'turn_30s.svg', hint: '每回合 30 秒' },
      Timer1m: { label: '1 分钟', icon: 'turn_1m.svg', hint: '每回合 1 分钟' },
      Timer2m: { label: '2 分钟', icon: 'turn_2m.svg', hint: '每回合 2 分钟' },
      Timer3m: { label: '3 分钟', icon: 'turn_3m.svg', hint: '每回合 3 分钟' },
      Timer5m: { label: '5 分钟', icon: 'turn_5m.svg', hint: '每回合 5 分钟' },
      TimerInf: { label: '无限制', icon: 'turn_inf.svg', hint: '不限制输入时间' }
    }
  };

  var V2_STATE_LABELS = {
    Setup: '房间设置',
    WaitingForFunctions: '等待函数',
    CalculatingResults: '计算结果',
    SynchronizingResults: '同步结果',
    ExecutingResults: '执行弹道',
    EndingGame: '结束对局',
    Disconnected: '已断开'
  };

  var V2_CAMPAIGN_GUIDES = [
    '第 0 关：写一个关于 x 的函数击中目标。',
    '第 1 关：游戏会给函数自动加上常数项，常数显示在右下角，保证弹道从士兵位置发出。',
    '第 2 关：平面上有障碍物，函数不能穿过障碍物，需要绕开。',
    '第 3 关：队伍中有多个士兵，每回合从不同士兵发射。',
    '第 4 关：在障碍物更多的平面上绕行。',
    '第 5 关：对手会反击。',
    '第 6 关：必须一发命中这些目标才能摧毁它们。',
    '第 7 关：第一次尝试就摧毁目标。',
    '第 8 关：一发命中所有目标才能摧毁它们。',
    '第 9 关：对手更聪明。',
    '第 10 关：一发命中所有目标，细网格线消失。',
    '第 11 关：网格更少，并且你和对手会同时发射。',
    '第 12 关：大多数网格线消失，尝试命中目标。',
    '第 13 关：一发摧毁两个目标。',
    '第 14 关：在几乎没有网格的平面上对战。',
    '第 15 关：主轴消失，对手更强。',
    '第 16 关：改为输入微分方程。输入关于 x 和 y 的一阶微分方程，弹道是以发射士兵位置为初始条件的解。',
    '第 17 关：用微分方程一发命中两个目标。',
    '第 18 关：用微分方程对战。',
    '第 19 关：在更少网格下用微分方程对战。',
    '第 20 关：在无网格下用微分方程对战。'
  ];

  function v2Meta(kind, value) {
    var group = V2_META[kind] || {};
    return group[value] || { label: value || '?', icon: '', hint: value || '' };
  }
  function modeToConst(mode) {
    if (mode === 'FirstOrderODE' || mode === 'FirstOrderOde' || mode === 'FstODE' || mode === 'DiffEqFunction') return C.FST_ODE;
    if (mode === 'SecondOrderODE' || mode === 'SecondOrderOde' || mode === 'SndODE' || mode === 'SecondDiffEqFunction') return C.SND_ODE;
    return C.NORMAL_FUNC;
  }

  function modeLabel(mode) {
    if (!mode) return 'Graphwar II';
    return v2Meta('func', mode).label || String(mode).replace(/([a-z])([A-Z])/g, '$1 $2');
  }

  function debugClip(s) {
    s = String(s == null ? '' : s);
    return s.length > 2000 ? s.slice(0, 2000) + '...<truncated>' : s;
  }

  function debugLog(line) {
    if (!root.GW_V2_DEBUG) return;
    line = debugClip(line);
    try { console.log('[GW2]', line); } catch (e) {}
    var app = root.GW_DESKTOP && root.GW_DESKTOP.app;
    if (app && app.AppendV2DebugLog) {
      try { app.AppendV2DebugLog(line); } catch (e2) {}
    }
  }

  function cleanGraphicsName(s) {
    s = cleanText(s || '', 80).trim();
    return /^[A-Za-z0-9_ -]+$/.test(s) ? s : '';
  }

  function GameDataV2(opts) {
    Emitter.call(this);
    this.protocolVersion = 2;
    this.opts = opts || {};
    this.avatar = this.opts.avatar || {};
    this.conn = null;
    this.players = [];
    this.playersById = {};
    this.soldierOwner = {};
    this.soldierById = {};
    this.entityMeta = {};
    this.connectionID = null;
    this.localEntityID = null;
    this.gameState = C.NONE;
    this.gameMode = C.NORMAL_FUNC;
    this.functionMode = 'NormalFunction';
    this.axisMode = '';
    this.turnMode = '';
    this.timeMode = '';
    this.turnLimit = null;
    this.modeLabel = '普通函数';
    this.gameName = 'Graphwar II';
    this.leader = false;
    this.currentTurn = -1;
    this.timeTurnStarted = Date.now();
    this.drawingFunction = false;
    this.exploding = false;
    this.turnTimeUp = false;
    this.previewFunction = '';
    this.func = null;
    this.funcResult = null;
    this.activeFunctionPlayerID = null;
    this.activeFunctionSoldierID = null;
    this.soldiersHit = [];
    this.timeStartedDrawingFunction = 0;
    this.timeStartedExploding = 0;
    this.nextTurnSent = false;
    this.obstacle = null;
    this.trajectoryObstacle = null;
    this._obstacleSafetyPx = Math.max(2, Math.round(((GW.C && GW.C.SOLDIER_RADIUS) || (GW.GameConstants && GW.GameConstants.SOLDIER_RADIUS) || 7) * 0.28));
    this._obstacleCircles = [];
    this._queuedObstacleDestroys = [];
    this.currentTick = 0;
    this._tickTimer = null;
    this._functionCalcTimer = null;
    this._functionCalcSent = false;
    this._localFunctionSubmitted = false;
    this._pendingFunctions = {};
    this._turnFunctions = {};
    this._recentFunctions = [];
    this._functionReady = {};
    this._firedOwners = {};
    this._fireQueue = [];
    this._preparedFunction = null;
    this._avatarSent = {};
    this.pendingName = '';
    this._connectedEmitted = false;
    this._requestedPlayer = false;
    this._leaderReason = 'unknown';
    this._roomEditSeq = 0;
    this._pendingRoomEdits = [];
    this._closing = false;
  }
  GameDataV2.prototype = Object.create(Emitter.prototype);

  GameDataV2.prototype.connect = function (host, port) {
    var self = this;
    this.gameState = C.PRE_GAME;
    debugLog('connect host=' + host + ' port=' + port + ' bridge=' + (this.opts.bridgeUrl || ''));
    this.conn = new GW.V2Connection({
      bridgeUrl: this.opts.bridgeUrl,
      host: host,
      port: port,
      onOpen: function () {
        self.send('NewConnectionRequest', GW.V2.newConnectionRequest());
      },
      onEvent: function (event, raw) { self.handleEvent(event, raw); },
      onClose: function (info) { if (!self._closing) self.emit('disconnected', info); },
      onError: function (info) { self.emit('neterror', info); }
    });
    this.conn.open();
    this._startTickLoop();
  };

  GameDataV2.prototype.disconnect = function () {
    this._stopTickLoop();
    this._clearFunctionCalcTimer();
    debugLog('disconnect connection_id=' + this.connectionID + ' local_entity=' + this.localEntityID);
    if (this.conn) {
      this._closing = true;
      if (this.connectionID != null) this.send('ConnectionRemoved', { connection_id: this.connectionID, reason: 'ClientDisconnect' });
      this.conn.close();
    }
    this.gameState = C.NONE;
  };

  GameDataV2.prototype._startTickLoop = function () {
    var self = this;
    this._stopTickLoop();
    this._tickTimer = setInterval(function () {
      if (self.conn && self.conn.connected) self.send('TickRequest', {});
    }, 500);
  };

  GameDataV2.prototype._stopTickLoop = function () {
    if (this._tickTimer) {
      clearInterval(this._tickTimer);
      this._tickTimer = null;
    }
  };

  GameDataV2.prototype._clearFunctionCalcTimer = function () {
    if (this._functionCalcTimer) {
      clearTimeout(this._functionCalcTimer);
      this._functionCalcTimer = null;
    }
  };

  GameDataV2.prototype.send = function (variant, fields) {
    fields = fields || {};
    var state = this.conn ? this.conn.ws && this.conn.ws.readyState : 'no-conn';
    var ready = !!(this.conn && this.conn.connected);
    debugLog('C->S ' + variant + ' ready=' + ready + ' ws=' + state + ' ' + JSON.stringify(fields));
    if (!this.conn || !this.conn.connected) {
      debugLog('C->S blocked ' + variant + ' reason=not_connected connection_id=' + this.connectionID + ' local_entity=' + this.localEntityID);
      return false;
    }
    var ok = this.conn.sendEvent(variant, fields);
    if (!ok) debugLog('C->S failed ' + variant + ' reason=websocket_not_open connection_id=' + this.connectionID + ' local_entity=' + this.localEntityID);
    return ok;
  };

  GameDataV2.prototype._localRequestReady = function () {
    return !!(this.conn && this.conn.connected && this.connectionID != null && this.localEntityID != null);
  };

  GameDataV2.prototype.getFirstLocalPlayer = function () {
    for (var i = 0; i < this.players.length; i++) if (this.players[i].local) return this.players[i];
    return null;
  };
  GameDataV2.prototype.getCurrentTurnPlayer = function () {
    return this.players[this.currentTurn] || this.getFirstLocalPlayer() || this.players[0] || null;
  };
  GameDataV2.prototype.isSimultaneousMode = function () {
    return /simultaneous|concurrent|both/i.test(this.turnMode || '');
  };
  GameDataV2.prototype.getLocalActionPlayer = function () {
    return this.getFirstLocalPlayer() || null;
  };
  GameDataV2.prototype.getPlayer = function (id) {
    id = asId(id);
    return id == null ? null : (this.playersById[id] || null);
  };
  GameDataV2.prototype._actionPlayer = function () {
    if (this.isSimultaneousMode()) return this.getLocalActionPlayer();
    var p = this.getCurrentTurnPlayer();
    return p && p.local ? p : null;
  };
  GameDataV2.prototype.isTerrainReversed = function () {
    var p = this.getFirstLocalPlayer();
    return p ? p.team === C.TEAM2 : false;
  };
  GameDataV2.prototype.isFunctionReversed = function () {
    var p = this.getActiveFunctionPlayer && this.getActiveFunctionPlayer();
    if (!p) p = this.isSimultaneousMode() ? this.getLocalActionPlayer() : this.getCurrentTurnPlayer();
    return p ? p.team === C.TEAM2 : false;
  };
  GameDataV2.prototype.getActiveFunctionPlayer = function () {
    return this.activeFunctionPlayerID != null ? this.getPlayer(this.activeFunctionPlayerID) : null;
  };

  GameDataV2.prototype.addPlayer = function (name) {
    this.pendingName = cleanText(name || 'Player', 48);
    if (this.connectionID == null) {
      debugLog('NewPlayerRequest deferred reason=no_connection_id name=' + this.pendingName);
      return;
    }
    if (this._requestedPlayer) {
      debugLog('NewPlayerRequest skipped reason=already_requested connection_id=' + this.connectionID);
      return;
    }
    this._requestedPlayer = true;
    debugLog('request local player for connection_id=' + this.connectionID + ' name=' + this.pendingName);
    this.send('NewPlayerRequest', { connection_id: this.connectionID });
  };

  GameDataV2.prototype._trackRoomEdit = function (variant, p, fields) {
    var edit = {
      seq: ++this._roomEditSeq,
      variant: variant,
      playerID: p && p.id,
      beforeSoldiers: p && p.numSoldiers,
      beforeTeam: p && p.team,
      leader: !!this.leader,
      leaderReason: this._leaderReason || 'unknown',
      at: Date.now()
    };
    this._pendingRoomEdits.push(edit);
    if (this._pendingRoomEdits.length > 16) this._pendingRoomEdits.shift();
    debugLog('room edit #' + edit.seq + ' ' + variant +
      ' target=' + edit.playerID +
      ' local=' + !!(p && p.local) +
      ' leader=' + edit.leader +
      ' leader_reason=' + edit.leaderReason +
      ' fields=' + JSON.stringify(fields || {}));
    return edit;
  };

  GameDataV2.prototype._noteRoomEditApplied = function (kind, playerID, detail) {
    playerID = asId(playerID);
    for (var i = this._pendingRoomEdits.length - 1; i >= 0; i--) {
      var e = this._pendingRoomEdits[i];
      if (playerID != null && e.playerID !== playerID) continue;
      var ok =
        (kind === 'team' && e.variant === 'ChangeTeamRequest') ||
        (kind === 'soldiers' && (e.variant === 'AddSoldierRequest' || e.variant === 'RemoveSoldierRequest')) ||
        (kind === 'remove' && e.variant === 'RemovePlayerRequest');
      if (!ok) continue;
      debugLog('room edit #' + e.seq + ' applied kind=' + kind + ' detail=' + debugClip(JSON.stringify(detail || {})));
      this._pendingRoomEdits.splice(i, 1);
      return;
    }
  };

  GameDataV2.prototype.removePlayer = function (p) {
    if (!p) { debugLog('RemovePlayerRequest blocked reason=no_player'); return false; }
    var fields = { player_id: p.id, entity_id: p.id };
    this._trackRoomEdit('RemovePlayerRequest', p, fields);
    return this.send('RemovePlayerRequest', fields);
  };
  GameDataV2.prototype._requestSoldierID = function (p) {
    if (!p || !p.soldiers || !p.soldiers.length) return 0;
    var idx = asId(p.currentTurnSoldier);
    if (idx == null || idx < 0 || idx >= p.soldiers.length) idx = 0;
    return p.soldiers[idx] && p.soldiers[idx].id ? p.soldiers[idx].id : 0;
  };
  GameDataV2.prototype.addSoldier = function (p) {
    if (!p) { debugLog('AddSoldierRequest blocked reason=no_player'); return false; }
    var fields = { soldier_id: this._requestSoldierID(p), player_id: p.id, entity_id: p.id };
    this._trackRoomEdit('AddSoldierRequest', p, fields);
    return this.send('AddSoldierRequest', fields);
  };
  GameDataV2.prototype.removeSoldier = function (p) {
    if (!p) { debugLog('RemoveSoldierRequest blocked reason=no_player'); return false; }
    var fields = { soldier_id: this._requestSoldierID(p), player_id: p.id, entity_id: p.id };
    this._trackRoomEdit('RemoveSoldierRequest', p, fields);
    return this.send('RemoveSoldierRequest', fields);
  };
  GameDataV2.prototype.switchSide = function (p) {
    if (!p) { debugLog('ChangeTeamRequest blocked reason=no_player'); return false; }
    var fields = { player_id: p.id, entity_id: p.id };
    this._trackRoomEdit('ChangeTeamRequest', p, fields);
    return this.send('ChangeTeamRequest', fields);
  };
  GameDataV2.prototype.canEditRoom = function () {
    this._updateLeader();
    return !!this.leader;
  };
  GameDataV2.prototype.addBot = function (level) {
    level = Math.trunc(Number(level));
    if (!isFinite(level)) level = 0;
    level = Math.max(0, Math.min(5, level));
    debugLog('room edit add-bot level=' + level + ' leader=' + !!this.leader + ' connection_id=' + this.connectionID);
    return this.send('NewBotRequest', { connection_id: this.connectionID || 0, level: level });
  };
  GameDataV2.prototype.setReady = function (p, ready) {
    if (!ready) return;
    debugLog('room edit game-start local_player=' + (p && p.id) + ' leader=' + !!this.leader);
    return this.send('GameStartRequest', {});
  };
  GameDataV2.prototype.nextMode = function () { return this.send('FunctionModeChangeRequest', {}); };
  GameDataV2.prototype.nextAxisMode = function () { return this.send('AxisModeChangeRequest', {}); };
  GameDataV2.prototype.nextTurnMode = function () { return this.send('TurnModeChangeRequest', {}); };
  GameDataV2.prototype.nextTimeMode = function () { return this.send('TimeModeChangeRequest', {}); };
  GameDataV2.prototype.setLocked = function (locked) { return this.send('LockRequest', { lock: !!locked }); };
  GameDataV2.prototype.sendChatMessage = function (chat) {
    chat = cleanText(chat, 600);
    if (!chat) return false;
    var entityID = this.connectionID != null ? this.connectionID : this.localEntityID;
    if (entityID == null) {
      debugLog('ChatMessageRequest blocked reason=no_entity_id');
      return false;
    }
    debugLog('chat send entity_id=' + entityID + ' connection_id=' + this.connectionID + ' len=' + chat.length);
    return this.send('ChatMessageRequest', { entity_id: entityID, message: chat });
  };
  GameDataV2.prototype.setAvatarConfig = function (avatar) {
    this.avatar = avatar || {};
    this._avatarSent = {};
    this._sendAvatarForLocalSoldiers();
  };
  GameDataV2.prototype._avatarConfig = function () {
    return {
      skin: cleanGraphicsName(this.avatar && this.avatar.skin),
      face: cleanGraphicsName(this.avatar && this.avatar.face),
      hat: cleanGraphicsName(this.avatar && this.avatar.hat)
    };
  };
  GameDataV2.prototype._sendAvatarForSoldier = function (soldierID) {
    soldierID = asId(soldierID);
    if (soldierID == null) return;
    var cfg = this._avatarConfig();
    if (!cfg.skin && !cfg.face && !cfg.hat) return;
    var key = soldierID + ':' + cfg.skin + ':' + cfg.face + ':' + cfg.hat;
    if (this._avatarSent[soldierID] === key) return;
    this._avatarSent[soldierID] = key;
    if (cfg.skin) this.send('SkinGraphicsRequest', { entity_id: soldierID, graphics_str: cfg.skin });
    if (cfg.face) this.send('FaceGraphicsRequest', { entity_id: soldierID, graphics_str: cfg.face });
    if (cfg.hat) this.send('HatGraphicsRequest', { entity_id: soldierID, graphics_str: cfg.hat });
  };
  GameDataV2.prototype._sendAvatarForLocalSoldiers = function () {
    var p = this.getFirstLocalPlayer();
    if (!p || !p.soldiers) return;
    for (var i = 0; i < p.soldiers.length; i++) {
      if (p.soldiers[i]) this._sendAvatarForSoldier(p.soldiers[i].id);
    }
  };
  GameDataV2.prototype.sendFunctionPreview = function (preview) {
    preview = cleanText(preview, 4096);
    var p = this._actionPlayer();
    if (p) this.send('FunctionUpdateRequest', { player_id: p.id, function: preview });
  };
  GameDataV2.prototype.hasLocalFunctionSubmitted = function () { return !!this._localFunctionSubmitted; };
  GameDataV2.prototype._aliveSoldierCount = function (p) {
    var n = 0;
    if (!p) return 0;
    for (var i = 0; i < p.numSoldiers; i++) if (p.soldiers[i] && p.soldiers[i].alive) n++;
    return n;
  };
  GameDataV2.prototype.getAlivePlayerCount = function () {
    var n = 0;
    for (var i = 0; i < this.players.length; i++) {
      if (this.gameState === C.GAME) {
        if (this._aliveSoldierCount(this.players[i]) > 0) n++;
      } else {
        n++;
      }
    }
    return n;
  };
  GameDataV2.prototype.getSubmittedFunctionCount = function () {
    return Object.keys(this._turnFunctions || {}).length;
  };
  GameDataV2.prototype._rememberFunction = function (playerID, functionStr) {
    playerID = asId(playerID);
    functionStr = cleanText(functionStr, 4096);
    if (playerID == null || !functionStr) return;
    var p = this.getPlayer(playerID);
    var rec = {
      playerID: playerID,
      name: p ? p.name : ('Player ' + playerID),
      local: !!(p && p.local),
      color: p ? p.color : '',
      function: functionStr,
      at: Date.now()
    };
    var keep = [];
    for (var i = 0; i < this._recentFunctions.length; i++) {
      var old = this._recentFunctions[i];
      if (old.playerID !== playerID || old.function !== functionStr) keep.push(old);
    }
    keep.unshift(rec);
    this._recentFunctions = keep.slice(0, 8);
  };
  GameDataV2.prototype.getRecentV2Functions = function () {
    return (this._recentFunctions || []).slice(0, 8);
  };
  GameDataV2.prototype.getActiveV2Function = function () {
    var playerID = this.activeFunctionPlayerID;
    if (playerID == null) return null;
    var p = this.getPlayer(playerID);
    var soldierID = this.activeFunctionSoldierID;
    return {
      playerID: playerID,
      soldierID: soldierID,
      name: p ? p.name : ('Player ' + playerID),
      color: p ? p.color : '',
      function: this._pendingFunctions[playerID] || ''
    };
  };
  GameDataV2.prototype.sendFunction = function (functionStr) {
    functionStr = cleanText(functionStr, 4096);
    if (!functionStr) return;
    var p = this._actionPlayer();
    if (!p || this._localFunctionSubmitted) return;
    this._pendingFunctions[p.id] = functionStr;
    this._turnFunctions[p.id] = functionStr;
    this._rememberFunction(p.id, functionStr);
    if (this.send('FunctionUpdateRequest', { player_id: p.id, function: functionStr }) === false) return false;
    this._localFunctionSubmitted = true;
    this.emit('v2_status');
    return this.send('FunctionFireRequest', { player_id: p.id });
  };
  GameDataV2.prototype.setAngle = function (angle) {
    var p = this._actionPlayer();
    if (!p) return;
    var s = p.soldiers[p.currentTurnSoldier];
    if (!s) return;
    var n = Number(angle);
    if (!isFinite(n)) return;
    s.angle = n;
    this.emit('angle');
  };
  GameDataV2.prototype.handleCommands = function () {};

  GameDataV2.prototype.getRemainingTime = function () {
    var limit = this._turnLimitMs();
    if (limit == null) return null;
    return Math.max(0, limit - (Date.now() - this.timeTurnStarted));
  };
  GameDataV2.prototype._turnLimitMs = function () {
    if (/inf/i.test(this.timeMode || '')) return null;
    if (this.turnLimit != null && this.turnLimit > 0) {
      return this.turnLimit < 1000 ? this.turnLimit * 1000 : this.turnLimit;
    }
    var m = String(this.timeMode || '').match(/Timer(\d+)(s|m)?/i);
    if (m) {
      var n = Number(m[1]);
      if (isFinite(n) && n > 0) return /m/i.test(m[2] || '') ? n * 60000 : n * 1000;
    }
    return C.TURN_TIME;
  };
  GameDataV2.prototype.checkGameFinished = function () { return false; };
  GameDataV2.prototype.nextTurn = function () {};
  GameDataV2.prototype.updateDrawingStuff = function () {
    if (this.drawingFunction) this.getCurrentFunctionPosition();
    if (this.exploding) this.getTimeExploding();
    this._applyQueuedState();
    var now = Date.now();
    for (var id in this.soldierById) {
      if (!Object.prototype.hasOwnProperty.call(this.soldierById, id)) continue;
      var s = this.soldierById[id];
      if (s.exploding && now - s.timeExplodingStarted > C.SOLDIER_MAX_DEATH_TIME) s.exploding = false;
    }
  };
  GameDataV2.prototype.getCurrentFunctionPosition = function () {
    if (!this.funcResult) return 0;
    if (this.exploding) return this.funcResult.numSteps;
    var numDrawSteps = Math.trunc((Date.now() - this.timeStartedDrawingFunction) * C.FUNCTION_VELOCITY / 1000);
    if (numDrawSteps > this.funcResult.numSteps && this.drawingFunction) {
      numDrawSteps = this.funcResult.numSteps;
      this.exploding = true;
      this.timeStartedExploding = Date.now();
    }
    for (var i = 0; i < this.soldiersHit.length; i++) {
      var sol = this.soldiersHit[i];
      if (sol.alive) {
        if (sol.exploding) {
          if (Date.now() - sol.timeExplodingStarted > C.SOLDIER_MAX_DEATH_TIME) sol.exploding = false;
        } else if (numDrawSteps > sol.killPosition) {
          sol.exploding = true; sol.timeExplodingStarted = Date.now();
        }
      }
    }
    return numDrawSteps;
  };
  GameDataV2.prototype.getTimeExploding = function () {
    var t = Date.now() - this.timeStartedExploding;
    if (t > C.NEXT_TURN_DELAY && this.exploding) {
      this.drawingFunction = false;
      this.exploding = false;
    }
    return t;
  };
  GameDataV2.prototype._applyQueuedState = function () {
    if (this.currentTick == null) return;
    for (var id in this.soldierById) {
      if (!Object.prototype.hasOwnProperty.call(this.soldierById, id)) continue;
      var s = this.soldierById[id];
      if (s.alive && s.queuedDeathTick != null && this.currentTick >= s.queuedDeathTick) {
        s.alive = false;
        s.exploding = true;
        s.timeExplodingStarted = Date.now();
        s.queuedDeathTick = null;
      }
    }
    if (this._queuedObstacleDestroys.length) {
      var keep = [];
      for (var i = 0; i < this._queuedObstacleDestroys.length; i++) {
        var q = this._queuedObstacleDestroys[i];
        if (q.tick != null && this.currentTick < q.tick) keep.push(q);
        else this._applyDestroyObstaclePixels(q.x, q.y, q.r);
      }
      this._queuedObstacleDestroys = keep;
    }
  };
  GameDataV2.prototype.kickFromGame = function () {};
  GameDataV2.prototype.disconnectKick = function () { this.disconnect(); };

  GameDataV2.prototype._player = function (id) {
    id = asId(id);
    if (id == null) return null;
    var p = this.playersById[id];
    if (!p) {
      p = makePlayer(id);
      var meta = this.entityMeta[id] || {};
      if (meta.name) p.name = meta.name;
      if (meta.team != null) p.team = meta.team;
      if (meta.color) p.color = meta.color;
      if (meta.rank != null) p.rank = meta.rank;
      this.playersById[id] = p;
      this.players.push(p);
      this.emit('player_added', p);
    }
    return p;
  };

  GameDataV2.prototype._applyBotDefaults = function (p) {
    if (!p || !p.isBot) return;
    var level = asId(p.botLevel);
    if (level == null) level = 0;
    level = Math.max(0, Math.min(5, level));
    p.botLevel = level;
    if (!p.name || /^Player\s+\d+$/i.test(p.name)) p.name = botLabel(level);
    for (var i = 0; i < p.soldiers.length; i++) {
      var s = p.soldiers[i];
      if (!s) continue;
      var meta = this._meta(s.id);
      if (meta && !meta.skinGraphics) meta.skinGraphics = 'bot_' + level;
      if (meta && !meta.faceGraphics) meta.faceGraphics = 'bot_0';
      if (!s.skinGraphics) s.skinGraphics = 'bot_' + level;
      if (!s.faceGraphics) s.faceGraphics = 'bot_0';
      if (s.hatGraphics == null) s.hatGraphics = '';
    }
  };

  GameDataV2.prototype._markLocal = function (id) {
    var p = this._player(id);
    if (!p) return;
    var wasLocal = p.local;
    this.localEntityID = p.id;
    for (var i = 0; i < this.players.length; i++) this.players[i].local = this.players[i].id === p.id;
    if (this.pendingName && !wasLocal) this.send('NameRequest', { entity_id: p.id, name: this.pendingName });
    this._sendAvatarForLocalSoldiers();
    this.emit('roster');
  };

  GameDataV2.prototype._applyMetaToEntity = function (ent, meta) {
    if (!ent || !meta) return ent;
    if (meta.name && ent.soldiers) ent.name = meta.name;
    if (meta.team != null) ent.team = meta.team;
    if (meta.color) ent.color = meta.color;
    if (meta.rank != null && ent.soldiers) ent.rank = meta.rank;
    if (meta.x != null) ent.x = meta.x;
    if (meta.y != null) ent.y = meta.y;
    if (meta.radius != null) ent.radius = meta.radius;
    if (meta.alive != null) ent.alive = meta.alive;
    if (meta.queuedDeathTick != null) ent.queuedDeathTick = meta.queuedDeathTick;
    if (meta.effectType != null) ent.effectType = meta.effectType;
    if (meta.skinGraphics != null) ent.skinGraphics = meta.skinGraphics;
    if (meta.faceGraphics != null) ent.faceGraphics = meta.faceGraphics;
    if (meta.hatGraphics != null) ent.hatGraphics = meta.hatGraphics;
    return ent;
  };

  GameDataV2.prototype._handleGraphicsInfo = function (kind, f) {
    var id = asId(f.entity_id);
    var meta = this._meta(id);
    if (meta) meta[kind] = cleanText(f.graphics_str || '', 80);
    var ent = this._findEntity(id);
    if (ent) ent[kind] = meta ? meta[kind] : cleanText(f.graphics_str || '', 80);
  };

  GameDataV2.prototype._soldier = function (soldierID, playerID) {
    soldierID = asId(soldierID);
    playerID = asId(playerID);
    if (soldierID == null) return null;
    var s = this.soldierById[soldierID];
    if (!s) {
      s = makeSoldier(soldierID);
      var meta = this.entityMeta[soldierID] || {};
      this._applyMetaToEntity(s, meta);
      this.soldierById[soldierID] = s;
    }
    if (playerID != null) {
      var oldOwner = this.soldierOwner[soldierID];
      if (oldOwner != null && oldOwner !== playerID && this.playersById[oldOwner]) {
        var oldPlayer = this.playersById[oldOwner];
        oldPlayer.soldiers = oldPlayer.soldiers.filter(function (x) { return x.id !== soldierID; });
        oldPlayer.numSoldiers = oldPlayer.soldiers.length;
      }
      var p = this._player(playerID);
      this.soldierOwner[soldierID] = playerID;
      if (p && p.soldiers.indexOf(s) < 0) {
        p.soldiers.push(s);
        p.numSoldiers = p.soldiers.length;
        this._noteRoomEditApplied('soldiers', playerID, { soldier_id: soldierID, numSoldiers: p.numSoldiers });
      }
      if (p && p.isBot) this._applyBotDefaults(p);
      if (p && p.local) this._sendAvatarForSoldier(soldierID);
    }
    this._applyMetaToEntity(s, this.entityMeta[soldierID] || {});
    return s;
  };

  GameDataV2.prototype._findEntity = function (id) {
    id = asId(id);
    if (id == null) return null;
    if (this.playersById[id]) return this.playersById[id];
    if (this.soldierById[id]) return this.soldierById[id];
    return null;
  };

  GameDataV2.prototype._meta = function (id) {
    id = asId(id);
    if (id == null) return null;
    return this.entityMeta[id] || (this.entityMeta[id] = {});
  };

  GameDataV2.prototype._removeEntity = function (id) {
    id = asId(id);
    if (id == null) return;
    var p = this.playersById[id];
    if (p) {
      delete this.playersById[id];
      this.players = this.players.filter(function (x) { return x.id !== id; });
      delete this.entityMeta[id];
      this._noteRoomEditApplied('remove', id, {});
      this._updateLeader();
      this.emit('roster');
      return;
    }
    var owner = this.soldierOwner[id];
    var s = this.soldierById[id];
    if (owner != null && s && this.playersById[owner]) {
      var player = this.playersById[owner];
      player.soldiers = player.soldiers.filter(function (x) { return x.id !== id; });
      player.numSoldiers = player.soldiers.length;
      this._noteRoomEditApplied('soldiers', owner, { removed_soldier_id: id, numSoldiers: player.numSoldiers });
    }
    delete this.soldierOwner[id];
    delete this.soldierById[id];
    delete this.entityMeta[id];
    this.emit('roster');
  };

  GameDataV2.prototype._removeConnection = function (connectionID) {
    connectionID = asId(connectionID);
    if (connectionID == null) return;
    var ids = [];
    for (var i = 0; i < this.players.length; i++) {
      if (asId(this.players[i].connectionID) === connectionID) ids.push(this.players[i].id);
    }
    for (var j = 0; j < ids.length; j++) this._removeEntity(ids[j]);
  };

  GameDataV2.prototype._updateLeader = function () {
    var leader = null;
    var reason = 'no_players';
    var rankedLocalLeader = false;
    for (var i = 0; i < this.players.length; i++) {
      var p = this.players[i];
      if (p.local && asId(p.rank) === 1) {
        rankedLocalLeader = true;
        leader = p;
        reason = 'rank_info_player:' + p.id;
      }
      if (!rankedLocalLeader && (!leader || p.id < leader.id)) leader = p;
      p.leader = false;
    }
    if (!rankedLocalLeader && this.connectionID != null) {
      var connMeta = this.entityMeta[this.connectionID];
      if (connMeta && asId(connMeta.rank) === 1) {
        rankedLocalLeader = true;
        reason = 'rank_info_connection:' + this.connectionID;
        for (var j = 0; j < this.players.length; j++) {
          if (this.players[j].local) {
            leader = this.players[j];
            break;
          }
        }
      }
    }
    if (!rankedLocalLeader && this.localEntityID != null) {
      var localMeta = this.entityMeta[this.localEntityID];
      if (localMeta && asId(localMeta.rank) === 1) {
        rankedLocalLeader = true;
        reason = 'rank_info_entity:' + this.localEntityID;
        leader = this.playersById[this.localEntityID] || leader;
      }
    }
    if (leader) {
      leader.leader = true;
      if (!rankedLocalLeader) reason = 'lowest_player_id:' + leader.id;
    }
    this.leader = rankedLocalLeader || !!(leader && leader.local);
    this._leaderReason = reason;
    debugLog('leader update leader_player=' + (leader ? leader.id : 'none') + ' local=' + this.leader + ' reason=' + reason + ' connection_id=' + this.connectionID + ' local_entity=' + this.localEntityID);
  };

  GameDataV2.prototype._functionOwner = function (owner) {
    owner = asId(owner);
    if (owner == null) return { playerID: null, soldierID: null };
    var playerID = this.soldierOwner[owner] != null ? this.soldierOwner[owner] : owner;
    var soldierID = this.soldierOwner[owner] != null ? owner : null;
    var p = this.playersById[playerID];
    if (p && soldierID != null) {
      for (var i = 0; i < p.soldiers.length; i++) {
        if (p.soldiers[i] && p.soldiers[i].id === soldierID) {
          p.currentTurnSoldier = i;
          break;
        }
      }
    }
    return { playerID: playerID, soldierID: soldierID };
  };

  GameDataV2.prototype.handleEvent = function (event, raw) {
    var variant = GW.V2.variantOf(event);
    var f = GW.V2.fieldsOf(event);
    if (!variant) return;
    debugLog('S->C ' + variant + ' ' + debugClip(JSON.stringify(f || {})));
    try {
      switch (variant) {
        case 'NewConnection':
          this.connectionID = asId(f.connection_id);
          if (!this._connectedEmitted) {
            this._connectedEmitted = true;
            this.emit('connected');
          }
          if (this.pendingName) this.addPlayer(this.pendingName);
          break;
        case 'ConnectedToServer':
          debugLog('tcp connected ack received; waiting for NewConnection connection_id=' + this.connectionID);
          if (this.connectionID != null && this.pendingName) this.addPlayer(this.pendingName);
          break;
        case 'PlayerInfo':
          this._handlePlayerInfo(f);
          break;
        case 'NameInfo':
          this._handleNameInfo(f);
          break;
        case 'ColorInfo':
          this._handleColorInfo(f);
          break;
        case 'RankInfo':
          this._handleRankInfo(f);
          break;
        case 'TeamInfo':
          this._handleTeamInfo(f);
          break;
        case 'SoldierInfo':
          this._soldier(f.soldier_id, f.player_id);
          if (asId(f.player_id) != null && this.connectionID != null && asId(f.player_id) === this.localEntityID) this._markLocal(f.player_id);
          this.emit('roster');
          break;
        case 'BotAdded':
          if (f.player_id != null) {
            var bot = this._player(f.player_id);
            if (bot) {
              bot.connectionID = asId(f.connection_id);
              bot.botLevel = asId(f.level);
              bot.isBot = true;
              this._applyBotDefaults(bot);
            }
          }
          this.emit('roster');
          break;
        case 'EntityRemoved':
          this._removeEntity(f.entity_id);
          break;
        case 'GameStateInfo':
          this._handleGameState(f.game_state);
          break;
        case 'GameNameInfo':
          this.gameName = cleanText(f.game_name || this.gameName, 80);
          break;
        case 'AxisModeInfo':
          this.axisMode = f.axis_mode || '';
          this.emit('mode');
          break;
        case 'FunctionModeInfo':
          this.functionMode = f.function_mode || '';
          this.gameMode = modeToConst(this.functionMode);
          this.modeLabel = modeLabel(this.functionMode);
          this.emit('mode');
          this.emit('roster');
          break;
        case 'TurnModeInfo':
          this.turnMode = f.turn_mode || '';
          this.emit('mode');
          break;
        case 'TimeModeInfo':
          this.timeMode = f.time_mode || '';
          this.emit('mode');
          break;
        case 'TurnInfo':
          this._handleTurnInfo(f);
          break;
        case 'EndOfTurnInfo':
          this.timeTurnStarted = Date.now();
          break;
        case 'PosInfo':
          this._handlePosInfo(f);
          break;
        case 'LifeInfo':
          this._handleLifeInfo(f);
          break;
        case 'LifeQueuedDeath':
          this._handleLifeQueuedDeath(f);
          break;
        case 'ClearObstacles':
          this._obstacleCircles = [];
          this._queuedObstacleDestroys = [];
          this.obstacle = new GW.Obstacle(0, []);
          this.trajectoryObstacle = new GW.Obstacle(0, []);
          this.emit('explosion');
          break;
        case 'AddObstacle':
          this._handleAddObstacle(f);
          break;
        case 'DestroyObstacle':
          this._handleDestroyObstacle(f);
          break;
        case 'QueueDestroyObstacle':
          this._handleQueueDestroyObstacle(f);
          break;
        case 'FunctionUpdateRequest':
        case 'FunctionInfo':
          this._handleFunctionInfo(f);
          this.emit('preview', this.previewFunction);
          break;
        case 'FunctionFire':
          this._handleFunctionFire(f);
          break;
        case 'FunctionActive':
          this._handleFunctionActive(f);
          break;
        case 'FunctionPoints':
          this._handleFunctionPoints(f);
          break;
        case 'FunctionsCalculated':
          this._handleFunctionsCalculated();
          break;
        case 'GameHeartbeat':
          break;
        case 'SkinGraphicsInfo':
          this._handleGraphicsInfo('skinGraphics', f);
          break;
        case 'FaceGraphicsInfo':
          this._handleGraphicsInfo('faceGraphics', f);
          break;
        case 'HatGraphicsInfo':
          this._handleGraphicsInfo('hatGraphics', f);
          break;
        case 'TurnLimitInfo':
          this.turnLimit = asId(f.turn_limit);
          this.emit('mode');
          break;
        case 'TurnCountInfo':
          this.turnCount = asId(f.turn_count);
          break;
        case 'EffectInfo':
          this._handleEffectInfo(f);
          break;
        case 'EntityEffectInfo':
          this._handleEntityEffectInfo(f);
          break;
        case 'ChatMessage':
          this._handleChat(f);
          break;
        case 'TickReply':
        case 'Tick':
          this.currentTick = asId(f.current_tick);
          this._applyQueuedState();
          break;
        case 'LockInfo':
          this.locked = !!f.lock;
          this.emit('mode');
          break;
        case 'ConnectionRemoved':
          if (f.entity_id != null || f.player_id != null) this._removeEntity(f.entity_id != null ? f.entity_id : f.player_id);
          else this._removeConnection(f.connection_id);
          break;
        case 'RemoveTeam':
          var mt = this._meta(f.entity_id);
          if (mt) delete mt.team;
          break;
        default:
          this.lastUnknownEvent = raw || JSON.stringify(event);
          debugLog('unknown event ' + this.lastUnknownEvent);
          break;
      }
    } catch (e) {
      debugLog('protocolerror event=' + (raw || JSON.stringify(event)) + ' error=' + (e && e.stack || e));
      this.emit('protocolerror', raw || JSON.stringify(event), e);
    }
  };

  GameDataV2.prototype._handlePlayerInfo = function (f) {
    var id = asId(f.player_id != null ? f.player_id : f.entity_id);
    var p = this._player(id);
    if (!p) return;
    if (f.connection_id != null) p.connectionID = asId(f.connection_id);
    if (f.name != null) p.name = cleanText(f.name, 48);
    if (f.team != null) p.team = teamToConst(f.team);
    if (this.connectionID != null && asId(f.connection_id) === this.connectionID) {
      debugLog('local player identified player_id=' + p.id + ' connection_id=' + this.connectionID);
      this._markLocal(p.id);
    }
    this._updateLeader();
    this.emit('roster');
  };

  GameDataV2.prototype._handleNameInfo = function (f) {
    var id = asId(f.entity_id);
    var name = cleanText(f.name, 48);
    var meta = this._meta(id);
    if (meta) meta.name = name;
    var p = this.playersById[id];
    if (p) {
      p.name = name || p.name;
      this.emit('roster');
    }
  };

  GameDataV2.prototype._handleColorInfo = function (f) {
    if (!f.color) return;
    var r = asId(f.color.r), g = asId(f.color.g), b = asId(f.color.b);
    if (r == null || g == null || b == null) return;
    var color = 'rgb(' + r + ',' + g + ',' + b + ')';
    var meta = this._meta(f.entity_id);
    if (meta) meta.color = color;
    var ent = this._findEntity(f.entity_id);
    if (ent) ent.color = color;
    if (this.playersById[asId(f.entity_id)]) this.emit('roster');
  };

  GameDataV2.prototype._handleRankInfo = function (f) {
    var id = asId(f.entity_id);
    var rank = asId(f.rank) || 0;
    var meta = this._meta(id);
    if (meta) meta.rank = rank;
    var p = this.playersById[id];
    if (p) p.rank = rank;
    this._updateLeader();
    this.emit('roster');
  };

  GameDataV2.prototype._handleTeamInfo = function (f) {
    var id = asId(f.entity_id);
    var team = teamToConst(f.team);
    var meta = this._meta(id);
    if (meta) meta.team = team;
    var ent = this._findEntity(id);
    if (ent) ent.team = team;
    if (this.playersById[id]) this._noteRoomEditApplied('team', id, { team: f.team });
    if (this.playersById[id]) this.emit('roster');
  };

  GameDataV2.prototype._handleGameState = function (state) {
    var previousRemote = this.remoteGameState || '';
    this.remoteGameState = state || '';
    var was = this.gameState;
    if (state === 'Setup') this.gameState = C.PRE_GAME;
    else this.gameState = C.GAME;
    debugLog('state ' + was + ' -> ' + this.gameState + ' remote=' + this.remoteGameState);
    if (this.gameState === C.GAME && was !== C.GAME) {
      this.drawingFunction = false;
      this.exploding = false;
      this.funcResult = null;
      this.activeFunctionPlayerID = null;
      this.activeFunctionSoldierID = null;
      this.timeStartedDrawingFunction = Date.now();
      this.timeStartedExploding = 0;
      this._functionCalcSent = false;
      this._localFunctionSubmitted = false;
      this._turnFunctions = {};
      this._firedOwners = {};
      this._fireQueue = [];
      this._preparedFunction = null;
      this._queuedObstacleDestroys = [];
      this._clearFunctionCalcTimer();
      this.emit('game_started');
    }
    if (this.gameState === C.GAME && this.isSimultaneousMode && this.isSimultaneousMode()) {
      if (this.remoteGameState === 'WaitingForFunctions' && previousRemote && previousRemote !== 'WaitingForFunctions') {
        this.drawingFunction = false;
        this.exploding = false;
        this.funcResult = null;
        this.activeFunctionPlayerID = null;
        this.activeFunctionSoldierID = null;
        this.soldiersHit = [];
        this._functionCalcSent = false;
        this._localFunctionSubmitted = false;
        this._turnFunctions = {};
        this._firedOwners = {};
        this._fireQueue = [];
        this._preparedFunction = null;
        this._clearFunctionCalcTimer();
        this.previewFunction = '';
        this.timeTurnStarted = Date.now();
        this.emit('v2_status');
        this.emit('next_turn');
      }
    }
    if (this.gameState === C.PRE_GAME && was === C.GAME) this.emit('game_finished');
  };

  GameDataV2.prototype._handleTurnInfo = function (f) {
    var id = asId(f.player_id != null ? f.player_id : (f.entity_id != null ? f.entity_id : f.target_id));
    var soldierID = asId(f.soldier_id);
    if (id == null) return;
    for (var i = 0; i < this.players.length; i++) {
      if (this.players[i].id === id) {
        this.currentTurn = i;
        if (soldierID != null) {
          for (var j = 0; j < this.players[i].soldiers.length; j++) {
            if (this.players[i].soldiers[j].id === soldierID) {
              this.players[i].currentTurnSoldier = j;
              break;
            }
          }
        }
        if (this.players[i].local) this.lastLocalHumanPlayer = this.players[i];
        this.timeTurnStarted = Date.now();
        this.turnTimeUp = false;
        this.drawingFunction = false;
        this.exploding = false;
        this.funcResult = null;
        this.activeFunctionPlayerID = null;
        this.activeFunctionSoldierID = null;
        this.soldiersHit = [];
        this._functionCalcSent = false;
        this._localFunctionSubmitted = false;
        this._turnFunctions = {};
        this._firedOwners = {};
        this._fireQueue = [];
        this._preparedFunction = null;
        this._clearFunctionCalcTimer();
        this.previewFunction = '';
        debugLog('turn player_id=' + id + ' soldier_id=' + soldierID + ' index=' + i + ' soldier_index=' + this.players[i].currentTurnSoldier);
        this.emit('v2_status');
        this.emit('next_turn');
        return;
      }
    }
  };

  GameDataV2.prototype._handlePosInfo = function (f) {
    var id = asId(f.entity_id != null ? f.entity_id : (f.soldier_id != null ? f.soldier_id : f.target_id));
    var meta = this._meta(id);
    var x = gameXToPixel(f.pos_x);
    var y = gameYToPixel(f.pos_y);
    var r = gameRadiusToPixel(f.radius);
    if (meta) {
      if (x != null) meta.x = x;
      if (y != null) meta.y = y;
      if (r != null) meta.radius = r;
    }
    var ent = this._findEntity(id);
    if (!ent) return;
    if (x != null) ent.x = x;
    if (y != null) ent.y = y;
    if (r != null) ent.radius = r;
    debugLog('pos entity=' + id + ' pixel=(' + ent.x + ',' + ent.y + ') radius=' + ent.radius);
    if (this.soldierById[id]) this.emit('roster');
  };

  GameDataV2.prototype._handleLifeInfo = function (f) {
    var id = asId(f.entity_id != null ? f.entity_id : f.soldier_id);
    var meta = this._meta(id);
    if (meta) meta.alive = !!f.alive;
    var ent = this._findEntity(id);
    if (!ent) return;
    ent.alive = !!f.alive;
    if (!ent.alive) {
      ent.exploding = true;
      ent.timeExplodingStarted = Date.now();
    }
  };

  GameDataV2.prototype._handleLifeQueuedDeath = function (f) {
    var id = asId(f.entity_id != null ? f.entity_id : f.soldier_id);
    var tick = asId(f.tick);
    var meta = this._meta(id);
    if (meta) meta.queuedDeathTick = tick;
    var ent = this._findEntity(id);
    if (!ent) return;
    ent.queuedDeathTick = tick;
  };

  GameDataV2.prototype._handleAddObstacle = function (f) {
    var x = gameXToPixel(f.pos_x != null ? f.pos_x : (f.x != null ? f.x : f.center_x));
    var y = gameYToPixel(f.pos_y != null ? f.pos_y : (f.y != null ? f.y : f.center_y));
    var r = gameRadiusToPixel(f.radius != null ? f.radius : f.r);
    if (x == null || y == null || r == null) return;
    this._obstacleCircles.push(Math.trunc(x), Math.trunc(y), Math.trunc(r));
    this._rebuildObstacles();
    debugLog('add obstacle pixel=(' + x + ',' + y + ') radius=' + r + ' count=' + (this._obstacleCircles.length / 3));
    this.emit('explosion');
  };

  GameDataV2.prototype._handleDestroyObstacle = function (f) {
    var x = gameXToPixel(f.pos_x != null ? f.pos_x : (f.x != null ? f.x : f.center_x));
    var y = gameYToPixel(f.pos_y != null ? f.pos_y : (f.y != null ? f.y : f.center_y));
    var r = gameRadiusToPixel(f.radius != null ? f.radius : f.r);
    if (x == null || y == null || r == null) return;
    this._applyDestroyObstaclePixels(x, y, r);
    debugLog('destroy obstacle pixel=(' + x + ',' + y + ') radius=' + r);
  };

  GameDataV2.prototype._handleQueueDestroyObstacle = function (f) {
    var x = gameXToPixel(f.pos_x != null ? f.pos_x : (f.x != null ? f.x : f.center_x));
    var y = gameYToPixel(f.pos_y != null ? f.pos_y : (f.y != null ? f.y : f.center_y));
    var r = gameRadiusToPixel(f.radius != null ? f.radius : f.r);
    var tick = asId(f.tick);
    if (x == null || y == null || r == null) return;
    this._queuedObstacleDestroys.push({ x: x, y: y, r: r, tick: tick });
    debugLog('queue destroy obstacle pixel=(' + x + ',' + y + ') radius=' + r + ' tick=' + tick);
    this._applyQueuedState();
  };

  GameDataV2.prototype._applyDestroyObstaclePixels = function (x, y, r) {
    if (!this.obstacle || !this.obstacle.setExplosion) return;
    this.obstacle.setExplosion(Math.trunc(x), Math.trunc(y), Math.trunc(r));
    this.obstacle.explodePoint();
    if (this.trajectoryObstacle && this.trajectoryObstacle.setExplosion) {
      this.trajectoryObstacle.setExplosion(Math.trunc(x), Math.trunc(y), Math.max(1, Math.trunc(r - (this._obstacleSafetyPx || 0))));
      this.trajectoryObstacle.explodePoint();
    }
    this.emit('explosion');
  };

  GameDataV2.prototype._rebuildObstacles = function () {
    var n = this._obstacleCircles.length / 3;
    this.obstacle = new GW.Obstacle(n, this._obstacleCircles);
    var safety = this._obstacleSafetyPx || 0;
    if (safety > 0) {
      var inflated = [];
      for (var i = 0; i < this._obstacleCircles.length; i += 3) {
        inflated.push(this._obstacleCircles[i], this._obstacleCircles[i + 1], this._obstacleCircles[i + 2] + safety);
      }
      this.trajectoryObstacle = new GW.Obstacle(n, inflated);
    } else {
      this.trajectoryObstacle = this.obstacle;
    }
  };

  GameDataV2.prototype._handleFunctionInfo = function (f) {
    var owner = asId(f.owner_id != null ? f.owner_id : (f.player_id != null ? f.player_id : f.entity_id));
    var resolved = this._functionOwner(owner);
    var ownerPlayer = resolved.playerID;
    var fn = cleanText(f.corrected_function || f.function || '', 4096);
    this.previewFunction = fn;
    if (ownerPlayer != null && fn) {
      this._pendingFunctions[ownerPlayer] = fn;
      this._turnFunctions[ownerPlayer] = fn;
      this._rememberFunction(ownerPlayer, fn);
      this.emit('v2_status');
    }
    debugLog('function info owner=' + owner + ' player=' + ownerPlayer + ' soldier=' + resolved.soldierID + ' fn=' + fn);
  };

  GameDataV2.prototype._handleFunctionFire = function (f) {
    var owner = asId(f.soldier_id != null ? f.soldier_id : (f.owner_id != null ? f.owner_id : (f.player_id != null ? f.player_id : f.entity_id)));
    var resolved = this._functionOwner(owner);
    var ownerPlayer = resolved.playerID;
    var cur = this.getCurrentTurnPlayer();
    if (ownerPlayer == null && cur) ownerPlayer = cur.id;
    var fn = ownerPlayer != null ? this._pendingFunctions[ownerPlayer] : this.previewFunction;
    if (!fn && ownerPlayer != null && this._turnFunctions) fn = this._turnFunctions[ownerPlayer];
    if (ownerPlayer != null && fn) this._prepareFunctionCalculation(ownerPlayer, fn, false);
    if (!this.funcResult) this._scheduleFunctionsCalculated('fire-no-local-result');
    if (ownerPlayer != null) this._firedOwners[ownerPlayer] = true;
    if (ownerPlayer != null && fn) {
      this._fireQueue.push({ playerID: ownerPlayer, soldierID: resolved.soldierID, function: fn });
      if (this._fireQueue.length > 16) this._fireQueue.shift();
    }
    this.activeFunctionPlayerID = ownerPlayer;
    this.activeFunctionSoldierID = resolved.soldierID;
    debugLog('function fire owner=' + owner + ' player=' + ownerPlayer + ' soldier=' + resolved.soldierID);
    this.emit('v2_status');
  };

  GameDataV2.prototype._handleFunctionActive = function (f) {
    this.activeTick = asId(f.active_tick);
    var owner = asId(f.soldier_id != null ? f.soldier_id : (f.owner_id != null ? f.owner_id : (f.player_id != null ? f.player_id : f.entity_id)));
    var resolved = this._functionOwner(owner);
    var ownerPlayer = resolved.playerID;
    var cur = this.getCurrentTurnPlayer();
    if (ownerPlayer == null && this._fireQueue.length) ownerPlayer = this._fireQueue[0].playerID;
    if (ownerPlayer == null && cur) ownerPlayer = cur.id;
    var fn = ownerPlayer != null ? this._pendingFunctions[ownerPlayer] : this.previewFunction;
    if (!fn && this._fireQueue.length) fn = this._fireQueue[0].function;
    if (ownerPlayer != null && fn) this._prepareFunctionCalculation(ownerPlayer, fn, true);
    if (!this.funcResult) this._scheduleFunctionsCalculated('active-no-local-result');
    var activeSoldier = resolved.soldierID != null
      ? resolved.soldierID
      : (this._preparedFunction && this._preparedFunction.playerID === ownerPlayer ? this._preparedFunction.soldierID : null);
    this._startPreparedFunctionAnimation(ownerPlayer, activeSoldier);
    this.activeFunctionPlayerID = ownerPlayer;
    this.activeFunctionSoldierID = activeSoldier;
    debugLog('function active owner=' + owner + ' player=' + ownerPlayer + ' soldier=' + activeSoldier + ' active_tick=' + this.activeTick);
    this.emit('v2_status');
    this.emit('fire');
  };

  GameDataV2.prototype._prepareFunctionCalculation = function (ownerID, functionStr, playNow) {
    if (!functionStr) return;
    var p = this.getPlayer(ownerID);
    if (!p || !this.obstacle) {
      debugLog('function calc delayed missing player/obstacle owner=' + ownerID + ' obstacle=' + !!this.obstacle);
      this._scheduleFunctionsCalculated('missing-player-or-obstacle');
      return;
    }
    var idx = this.players.indexOf(p);
    if (idx < 0) return;
    if (!this._processFunction(p, idx, functionStr)) {
      debugLog('function calc failed owner=' + ownerID);
      this._scheduleFunctionsCalculated('calc-failed');
      return;
    }
    this.activeFunctionPlayerID = ownerID;
    this.activeFunctionSoldierID = p.soldiers[p.currentTurnSoldier] ? p.soldiers[p.currentTurnSoldier].id : null;
    this._preparedFunction = { playerID: ownerID, soldierID: this.activeFunctionSoldierID, at: Date.now() };
    debugLog('function calc ready owner=' + ownerID + ' steps=' + (this.funcResult && this.funcResult.numSteps));
    this._scheduleFunctionsCalculated('calc-ready');
    if (playNow) this._startPreparedFunctionAnimation(ownerID, this.activeFunctionSoldierID);
  };

  GameDataV2.prototype._startPreparedFunctionAnimation = function (ownerID, soldierID) {
    if (!this.funcResult) {
      this.drawingFunction = false;
      this.exploding = false;
      return false;
    }
    this.drawingFunction = true;
    this.exploding = false;
    this.timeStartedDrawingFunction = Date.now();
    this.timeStartedExploding = 0;
    this.nextTurnSent = false;
    this.activeFunctionPlayerID = ownerID != null ? ownerID : this.activeFunctionPlayerID;
    this.activeFunctionSoldierID = soldierID != null ? soldierID : this.activeFunctionSoldierID;
    return true;
  };

  GameDataV2.prototype._processFunction = function (player, playerIndex, functionStr) {
    var fn;
    try { fn = new GW.GwFunction(functionStr); }
    catch (e) { this.funcResult = null; return false; }
    var soldier = player.soldiers[player.currentTurnSoldier];
    if (!soldier) return false;
    soldier.fn = functionStr;
    var inverted = player.team !== C.TEAM1;
    var angle = soldier.angle || 0;
    var res;
    try { res = fn.process(this.gameMode, this.obstacle, this.players, playerIndex, angle, inverted); }
    catch (e2) { this.funcResult = null; return false; }
    this.soldiersHit = [];
    for (var i = 0; i < res.hits.length; i++) {
      var h = res.hits[i];
      if (this.players[h.player] && this.players[h.player].soldiers[h.soldier]) {
        var sol = this.players[h.player].soldiers[h.soldier];
        sol.killPosition = h.position;
        this.soldiersHit.push(sol);
      }
    }
    soldier.angle = res.fireAngle;
    this.func = fn;
    this.funcResult = res;
    this.emit('angle');
    return true;
  };

  GameDataV2.prototype._scheduleFunctionsCalculated = function (reason) {
    if (this._functionCalcSent) return;
    this._functionCalcSent = true;
    debugLog('local function calculation ready reason=' + reason + ' send=disabled');
  };

  GameDataV2.prototype._sendFunctionsCalculatedOnce = function (reason) {
    if (this._functionCalcSent) return;
    this._functionCalcSent = true;
    debugLog('skip FunctionsCalculated reason=' + reason + ' send=disabled');
  };

  GameDataV2.prototype._handleFunctionPoints = function (f) {
    var sx = gameXToPixel(f.start_x);
    var sy = gameYToPixel(f.start_y);
    var ex = gameXToPixel(f.end_x);
    this.remoteFunctionPoints = { startX: sx, startY: sy, endX: ex };
    debugLog('function points ' + JSON.stringify(this.remoteFunctionPoints));
    this.emit('v2_status');
  };

  GameDataV2.prototype._handleFunctionsCalculated = function () {
    this._clearFunctionCalcTimer();
    debugLog('functions calculated');
    this.emit('v2_status');
  };

  GameDataV2.prototype._handleEffectInfo = function (f) {
    this.lastEffect = {
      id: asId(f.effect_id),
      type: f.effect_type || '',
      startTick: asId(f.start_tick),
      x: gameXToPixel(f.pos_x),
      y: gameYToPixel(f.pos_y)
    };
    this.emit('v2_status');
  };

  GameDataV2.prototype._handleEntityEffectInfo = function (f) {
    var id = asId(f.entity_id);
    var meta = this._meta(id);
    if (meta) meta.effectType = f.effect_type || '';
    var ent = this._findEntity(id);
    if (ent) ent.effectType = f.effect_type || '';
    this.emit('v2_status');
  };

  GameDataV2.prototype._handleChat = function (f) {
    var msg = cleanText(f.message, 600);
    var p = null;
    var id = asId(f.entity_id != null ? f.entity_id : (f.player_id != null ? f.player_id : f.connection_id));
    if (id != null) {
      p = this.getPlayer(id);
      if (!p) {
        for (var i = 0; i < this.players.length; i++) {
          if (asId(this.players[i].connectionID) === id) {
            p = this.players[i];
            break;
          }
        }
      }
      if (!p && id === this.connectionID) p = this.getFirstLocalPlayer();
    }
    this.emit('chat', p, msg);
  };

  root.GW.GameDataV2 = GameDataV2;
  root.GW.V2Meta = V2_META;
  root.GW.v2Meta = v2Meta;
  root.GW.V2StateLabels = V2_STATE_LABELS;
  root.GW.v2StateLabel = function (state) { return V2_STATE_LABELS[state] || (state ? String(state).replace(/([a-z])([A-Z])/g, '$1 $2') : ''); };
  root.GW.V2CampaignGuides = V2_CAMPAIGN_GUIDES;
})(typeof window !== 'undefined' ? window : this);

