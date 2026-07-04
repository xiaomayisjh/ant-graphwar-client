// Graphwar client - application glue. Wires the screens (menu / lobby / room /
// game), the lobby + room connections, chat, the function bar, and the angle
// control. Talks to GameData (room) and GlobalClient (lobby) which relay
// through the Node WS<->TCP bridge.
(function (root) {
  'use strict';
  var GW = root.GW;
  var C = GW.C;
  var $ = function (s, el) { return (el || document).querySelector(s); };
  var $$ = function (s, el) { return Array.prototype.slice.call((el || document).querySelectorAll(s)); };

  var MODE_NAMES = ['普通函数', '一阶 ODE', '二阶 ODE'];
  var DEFAULT_V1_ROOM = { host: '127.0.0.1', port: 6112 };
  var DEFAULT_V2_ROOM = { host: '::1', port: 61834 };
  var GW2_ASSET = 'assets/gw2/';
  var V2_SKIN_OPTIONS = [
    'yellow', 'blue', 'gray', 'pink', 'purple', 'red', 'green', 'light_green',
    'true_purple', 'brown', 'calico', 'cyan', 'white_skin', 'arrow', 'target',
    'wargod', 'face_paint_1', 'face_paint_2', 'bot_0', 'bot_1', 'bot_2',
    'bot_3', 'bot_4', 'bot_5'
  ];
  var V2_FACE_OPTIONS = [
    'regular_eyes', 'no_face', 'anime_eyes', 'blue_eyes', 'green_eyes',
    'purple_eyes', 'alien_eyes', 'asian', 'aviator_glare', 'baby_eyes',
    'baggy_eyes', 'cat_eyes', 'cute_eyes', 'eye_patch', 'mustache',
    'nerd_glasses', 'bot_0'
  ];
  var V2_HAT_OPTIONS = [
    'soldier_helmet', 'no_hat', 'blonde_boy', 'blonde_girl', 'brown_boy',
    'brunette_girl', 'anime_hair', 'antenna_0', 'asian_hat', 'baseball_cap',
    'cat_ears', 'chef_hat', 'cowboy', 'crown', 'hair_band', 'hood',
    'horned_helmet', 'mad_hair', 'mohawk', 'mohawk_red', 'ponytail',
    'powdered_wig', 'roman_helmet', 'straw_hat', 'top_hat', 'tricone',
    'unicorn', 'witch_hat'
  ];

  var V2_META = {
    axis: {
      NoAxis: { label: '无坐标轴', icon: 'axis_option_0.svg', hint: '不显示坐标轴和网格' },
      OnlyMain: { label: '仅主轴', icon: 'axis_option_1.svg', hint: '只显示 x/y 主轴' },
      EveryFive: { label: '每 5 格', icon: 'axis_option_2.svg', hint: '每 5 个单位显示辅助网格' },
      EveryUnit: { label: '每 1 格', icon: 'axis_option_3.svg', hint: '每 1 个单位显示辅助网格' }
    },
    func: {
      NormalFunction: { label: 'f(x)', icon: 'fx_option.svg', hint: '输入普通函数作为弹道' },
      DiffEqFunction: { label: 'dx/dy', icon: 'dx_dy_option.svg', hint: '输入关于 x 和 y 的一阶微分方程' }
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

  V2_META.axis = {
    NoAxis: { label: '无坐标轴', icon: 'axis_option_0.svg', hint: '不显示坐标轴和网格' },
    OnlyMain: { label: '仅主轴', icon: 'axis_option_1.svg', hint: '只显示 x/y 主轴' },
    EveryFive: { label: '每 5 格', icon: 'axis_option_2.svg', hint: '每 5 个单位显示辅助网格' },
    EveryUnit: { label: '每 1 格', icon: 'axis_option_3.svg', hint: '每 1 个单位显示辅助网格' }
  };
  V2_META.func = {
    NormalFunction: { label: 'f(x)', icon: 'fx_option.svg', hint: '输入普通函数作为弹道' },
    DiffEqFunction: { label: 'dx/dy', icon: 'dx_dy_option.svg', hint: '输入关于 x 和 y 的一阶微分方程' },
    FirstOrderODE: { label: 'dx/dy', icon: 'dx_dy_option.svg', hint: '兼容旧协议：一阶微分方程' },
    SecondOrderODE: { label: "y''", icon: 'dx_dy_option.svg', hint: '兼容旧协议：二阶微分方程' }
  };
  V2_META.turn = {
    SequentialTurns: { label: '轮流发射', icon: 'turns_sequential.svg', hint: '玩家按顺序输入并发射' },
    SimultaneousTurns: { label: '同时确认', icon: 'turns_simultaneous.svg', hint: '双方都确认函数后统一结算弹道' }
  };
  V2_META.time = {
    Timer30s: { label: '30 秒', icon: 'turn_30s.svg', hint: '每回合 30 秒' },
    Timer1m: { label: '1 分钟', icon: 'turn_1m.svg', hint: '每回合 1 分钟' },
    Timer2m: { label: '2 分钟', icon: 'turn_2m.svg', hint: '每回合 2 分钟' },
    Timer3m: { label: '3 分钟', icon: 'turn_3m.svg', hint: '每回合 3 分钟' },
    Timer5m: { label: '5 分钟', icon: 'turn_5m.svg', hint: '每回合 5 分钟' },
    TimerInf: { label: '无限制', icon: 'turn_inf.svg', hint: '不限制输入时间' }
  };

  function v2Meta(kind, value) {
    if (GW.v2Meta) return GW.v2Meta(kind, value);
    if (GW.V2Meta && GW.V2Meta[kind]) {
      var shared = GW.V2Meta[kind];
      return shared[value] || { label: value || '?', icon: '', hint: value || '' };
    }
    var group = V2_META[kind] || {};
    return group[value] || { label: value || '?', icon: '', hint: value || '' };
  }

  function v2Icon(kind, value) {
    var icon = v2Meta(kind, value).icon;
    return icon ? GW2_ASSET + icon : '';
  }

  function cleanV2GraphicsName(s) {
    s = (s == null ? '' : String(s)).trim();
    return /^[A-Za-z0-9_ -]{0,80}$/.test(s) ? s : '';
  }

  var App = {
    bridgeUrl: '',
    game: null,
    lobby: null,
    renderer: null,
    name: 'Player',
    mode: 'direct',  // 'direct' = self-hosted WS backend; 'bridge' = WS<->TCP bridge
    version: 'v1',
    v2BridgeUrl: '',

    init: function () {
      this.bridgeUrl = localStorage.getItem('gw_bridge') || ('ws://' + location.hostname + ':8080');
      this.v2BridgeUrl = localStorage.getItem('gw_v2_bridge') || '';
      $('#bridgeUrl').value = this.bridgeUrl;
      this.name = localStorage.getItem('gw_name') || 'Player';
      $('#playerName').value = this.name;
      this.mode = localStorage.getItem('gw_mode') || 'direct';
      this.version = localStorage.getItem('gw_version') || 'v1';
      var versionSel = $('#gameVersion');
      if (versionSel) versionSel.value = this.version;
      var modeSel = $('#connMode');
      if (modeSel) modeSel.value = this.mode;
      this._initV2ClientOptions();
      this._applyModeUi();
      this._applyVersionDefaults();
      // blocked-player list (names, case-insensitive) + peeked room rosters
      try { this.blocked = JSON.parse(localStorage.getItem('gw_blocked') || '[]'); } catch (e) { this.blocked = []; }
      this._roomRosters = {};   // key "ip:port" -> {names:[...], at:ts}
      this._lastRooms = [];
      this._lobbyRetryAttempt = 0;
      this._roomRetryAttempt = 0;
      this._lobbyManualStop = false;
      this._manualRoomClose = false;
      this._lobbyReconnectPending = false;
      this._roomReconnectPending = false;

      this._bindMenu();
      this._bindLobby();
      this._bindRoom();
      this._bindGame();
      this._bindTranslate();
      this.show('menu');
    },

    // Configure the translation proxy URL based on connection mode + env.
    _configureTranslate: function () {
      if (!GW.Translate) return;
      var opts = {};
      if (window.GW_DESKTOP && window.GW_DESKTOP.app) {
        opts.desktopApp = window.GW_DESKTOP.app; // Wails App.Translate
      } else if (this.mode === 'bridge' && this.bridgeUrl) {
        // bridge ws://host:port -> http://host:port/translate
        opts.httpUrl = this.bridgeUrl.replace(/^ws/, 'http').replace(/\/+$/, '') + '/translate';
      } else if (this.mode === 'direct') {
        // self-hosted backend exposes translate on lobbyPort+1
        var host = $('#globalHost').value.trim() || '127.0.0.1';
        var lport = parseInt($('#globalPort').value, 10) || 23761;
        opts.httpUrl = 'http://' + host + ':' + (lport + 1) + '/translate';
      }
      GW.Translate.configure(opts);
    },

    _bindTranslate: function () {
      var self = this;
      // "translate received" toggles (one shared flag, three checkboxes mirror it)
      ['#trAutoIn', '#trAutoInLobby', '#trAutoInRoom'].forEach(function (sel) {
        var el = $(sel);
        if (el) el.addEventListener('change', function () {
          self._autoIn = el.checked;
          ['#trAutoIn', '#trAutoInLobby', '#trAutoInRoom'].forEach(function (s2) { var e2 = $(s2); if (e2) e2.checked = self._autoIn; });
          if (self._autoIn && !(GW.Translate && GW.Translate.enabled)) self.toast('翻译服务不可用', 'err');
        });
      });
      // "translate my draft" buttons: translate the input box content in place
      var pairs = [['#gameTrBtn', '#gameChatInput'], ['#lobbyTrBtn', '#lobbyChatInput'], ['#roomTrBtn', '#roomChatInput']];
      pairs.forEach(function (p) {
        var btn = $(p[0]), input = $(p[1]);
        if (btn && input) btn.addEventListener('click', function () {
          var v = input.value.trim();
          if (!v) return;
          if (!(GW.Translate && GW.Translate.enabled)) { self.toast('翻译服务不可用', 'err'); return; }
          btn.disabled = true; var old = btn.textContent; btn.textContent = '...';
          GW.Translate.translate(v).then(function (tr) { input.value = tr; })
            .catch(function () { self.toast('翻译失败', 'err'); })
            .then(function () { btn.disabled = false; btn.textContent = old; });
        });
      });
    },

    _applyModeUi: function () {
      // In direct mode the bridge URL is irrelevant; show/hide the field.
      var br = $('#bridgeRow');
      if (br) br.style.display = (this.mode === 'bridge' || this.version === 'v2') ? '' : 'none';
      var v2Opts = document.querySelector('.v2-client-options');
      if (v2Opts) v2Opts.classList.toggle('active', this.version === 'v2');
      var cm = $('#connMode');
      if (cm) cm.disabled = this.version === 'v2';
      if (this.version === 'v2') this.mode = 'bridge';
      var bu = $('#bridgeUrl');
      if (bu) {
        if (this.version === 'v2' && this.v2BridgeUrl) bu.value = this.v2BridgeUrl;
        if (this.version !== 'v2' && this.bridgeUrl) bu.value = this.bridgeUrl;
      }
    },

    _fillDatalist: function (id, values) {
      var dl = $('#' + id);
      if (!dl) return;
      dl.innerHTML = '';
      values.forEach(function (v) {
        var o = document.createElement('option');
        o.value = v;
        dl.appendChild(o);
      });
    },

    _initV2ClientOptions: function () {
      this._fillDatalist('v2SkinOptions', V2_SKIN_OPTIONS);
      this._fillDatalist('v2FaceOptions', V2_FACE_OPTIONS);
      this._fillDatalist('v2HatOptions', V2_HAT_OPTIONS);
      var skin = $('#v2SkinGraphics');
      var face = $('#v2FaceGraphics');
      var hat = $('#v2HatGraphics');
      if (skin) skin.value = localStorage.getItem('gw_v2_avatar_skin') || '';
      if (face) face.value = localStorage.getItem('gw_v2_avatar_face') || '';
      if (hat) hat.value = localStorage.getItem('gw_v2_avatar_hat') || '';
      this._syncDebugUi();
    },

    _v2AvatarConfig: function () {
      return {
        skin: cleanV2GraphicsName($('#v2SkinGraphics') && $('#v2SkinGraphics').value),
        face: cleanV2GraphicsName($('#v2FaceGraphics') && $('#v2FaceGraphics').value),
        hat: cleanV2GraphicsName($('#v2HatGraphics') && $('#v2HatGraphics').value)
      };
    },

    _applyV2AvatarConfig: function () {
      var cfg = this._v2AvatarConfig();
      localStorage.setItem('gw_v2_avatar_skin', cfg.skin);
      localStorage.setItem('gw_v2_avatar_face', cfg.face);
      localStorage.setItem('gw_v2_avatar_hat', cfg.hat);
      if (this.game && this.game.protocolVersion === 2 && this.game.setAvatarConfig) {
        this.game.setAvatarConfig(cfg);
      }
      return cfg;
    },

    _setV2DebugEnabled: function (enabled) {
      enabled = !!enabled;
      window.GW_V2_DEBUG = enabled;
      localStorage.setItem('gw_v2_debug_enabled', enabled ? '1' : '0');
      var app = this._desktopApp();
      if (app && app.SetV2DebugEnabled) {
        app.SetV2DebugEnabled(enabled).catch(function () {});
      }
      this._syncDebugUi();
    },

    _syncDebugUi: function () {
      var toggle = $('#v2DebugToggle');
      if (toggle) toggle.checked = window.GW_V2_DEBUG === true || localStorage.getItem('gw_v2_debug_enabled') === '1';
      var path = $('#v2DebugPath');
      if (path) {
        var p = (window.GW_DESKTOP && window.GW_DESKTOP.v2DebugLogPath) || '';
        path.textContent = toggle && toggle.checked ? (p || 'debug log enabled') : 'debug log disabled';
        path.title = p || '';
      }
    },

    _normalizeV2RoomHost: function (host) {
      host = (host || '').trim();
      if (!host || host === DEFAULT_V1_ROOM.host || host.toLowerCase() === 'localhost') return DEFAULT_V2_ROOM.host;
      if (host === '[' + DEFAULT_V2_ROOM.host + ']') return DEFAULT_V2_ROOM.host;
      return host;
    },

    _applyVersionDefaults: function () {
      var hostEl = $('#directHost');
      var portEl = $('#directPort');
      if (!hostEl || !portEl) return;
      var host = (hostEl.value || '').trim();
      var port = parseInt(portEl.value, 10) || 0;
      if (this.version === 'v2') {
        if (!host || host === DEFAULT_V1_ROOM.host || host.toLowerCase() === 'localhost') {
          hostEl.value = this._normalizeV2RoomHost(localStorage.getItem('gw_v2_host') || DEFAULT_V2_ROOM.host);
        }
        if (!port || port === DEFAULT_V1_ROOM.port) {
          var savedV2Port = parseInt(localStorage.getItem('gw_v2_port'), 10) || DEFAULT_V2_ROOM.port;
          portEl.value = savedV2Port === DEFAULT_V1_ROOM.port ? DEFAULT_V2_ROOM.port : savedV2Port;
        }
      } else {
        if (!host || host === DEFAULT_V2_ROOM.host || host === '[' + DEFAULT_V2_ROOM.host + ']') {
          hostEl.value = localStorage.getItem('gw_v1_host') || DEFAULT_V1_ROOM.host;
        }
        if (!port || port === DEFAULT_V2_ROOM.port) {
          portEl.value = localStorage.getItem('gw_v1_port') || DEFAULT_V1_ROOM.port;
        }
      }
    },

    _rememberVersionSettings: function (version) {
      version = version || this.version;
      var hostEl = $('#directHost');
      var portEl = $('#directPort');
      var bridgeEl = $('#bridgeUrl');
      if (hostEl && portEl) {
        localStorage.setItem(version === 'v2' ? 'gw_v2_host' : 'gw_v1_host', hostEl.value.trim());
        localStorage.setItem(version === 'v2' ? 'gw_v2_port' : 'gw_v1_port', String(parseInt(portEl.value, 10) || ''));
      }
      if (bridgeEl) {
        if (version === 'v2') localStorage.setItem('gw_v2_bridge', bridgeEl.value.trim());
        else localStorage.setItem('gw_bridge', bridgeEl.value.trim());
      }
    },

    _disconnectForVersionSwitch: function () {
      if (this._v2LobbyTimer) { clearInterval(this._v2LobbyTimer); this._v2LobbyTimer = null; }
      if (this._v2LobbyRetryTimer) { clearTimeout(this._v2LobbyRetryTimer); this._v2LobbyRetryTimer = null; }
      if (this._lobbyRetryTimer) { clearTimeout(this._lobbyRetryTimer); this._lobbyRetryTimer = null; }
      if (this._roomRetryTimer) { clearTimeout(this._roomRetryTimer); this._roomRetryTimer = null; }
      this._lobbyManualStop = true;
      this._manualRoomClose = true;
      if (this._v2LobbyLoading) this._v2LobbyLoading = false;
      if (this._timerInt) { clearInterval(this._timerInt); this._timerInt = null; }
      if (this.renderer) { this.renderer.stop(); this.renderer = null; }
      if (this.game) { try { this.game.disconnect(); } catch (e) {} this.game = null; }
      if (this.lobby) { try { this.lobby.stop(); } catch (e2) {} this.lobby = null; }
      var v2Panel = $('#v2GamePanel');
      if (v2Panel) v2Panel.style.display = 'none';
      this._lastRooms = [];
      this._roomRosters = {};
      this.show('menu');
    },

    _switchVersion: function (nextVersion) {
      nextVersion = nextVersion === 'v2' ? 'v2' : 'v1';
      if (nextVersion === this.version) {
        this._applyVersionDefaults();
        this._applyModeUi();
        return;
      }
      this._rememberVersionSettings(this.version);
      this._disconnectForVersionSwitch();
      this.version = nextVersion;
      localStorage.setItem('gw_version', this.version);
      if (this.version === 'v2') {
        this.mode = 'bridge';
        var desktop = window.GW_DESKTOP || null;
        if (desktop && desktop.v2BridgePort > 0) this.v2BridgeUrl = 'ws://127.0.0.1:' + desktop.v2BridgePort;
        else this.v2BridgeUrl = localStorage.getItem('gw_v2_bridge') || this.v2BridgeUrl || '';
      } else {
        this.mode = localStorage.getItem('gw_mode') || this.mode || 'bridge';
        var d = window.GW_DESKTOP || null;
        if (d && d.bridgePort > 0) this.bridgeUrl = 'ws://127.0.0.1:' + d.bridgePort;
        else this.bridgeUrl = localStorage.getItem('gw_bridge') || this.bridgeUrl || ('ws://' + location.hostname + ':8080');
      }
      var modeSel = $('#connMode');
      if (modeSel) modeSel.value = this.mode;
      this._applyVersionDefaults();
      this._applyModeUi();
      this.toast(this.version === 'v2' ? '已切换到 Graphwar II' : '已切换到 Graphwar I', 'ok');
    },

    // Build connection opts for GameData/GlobalClient given a target host:port.
    // direct mode -> ws to that host:port directly; bridge mode -> via bridge.
    _connOpts: function () {
      var self = this;
      if (this.mode === 'direct') {
        return {
          directUrl: function (host, port) {
            // ws(s) scheme follows the page; host as given, port as the room/lobby port
            var scheme = (location.protocol === 'https:') ? 'wss://' : 'ws://';
            return scheme + host + ':' + port;
          }
        };
      }
      return { bridgeUrl: this.bridgeUrl };
    },

    _v2ConnOpts: function () {
      return { bridgeUrl: this.v2BridgeUrl || this.bridgeUrl, avatar: this._v2AvatarConfig() };
    },

    show: function (screen) {
      $$('.screen').forEach(function (s) { s.classList.remove('active'); });
      $('#screen-' + screen).classList.add('active');
      this.current = screen;
    },

    toast: function (msg, kind) {
      var t = $('#toast');
      t.textContent = msg;
      t.className = 'toast show' + (kind ? ' ' + kind : '');
      clearTimeout(this._toastT);
      // longer for errors (often actionable instructions), shorter for info
      var dur = kind === 'err' ? 8000 : 3500;
      this._toastT = setTimeout(function () { t.className = 'toast'; }, dur);
    },

    _retryDelay: function (attempt, maxMs) {
      attempt = Math.max(0, attempt || 0);
      var base = Math.min(maxMs || 30000, 1000 * Math.pow(2, attempt));
      var jitter = Math.floor(Math.random() * 350);
      return base + jitter;
    },

    _scheduleLobbyReconnect: function (reason) {
      var self = this;
      if (this._lobbyManualStop || this.version === 'v2') return;
      if (this._lobbyReconnectPending) return;
      this._lobbyReconnectPending = true;
      if (this._lobbyRetryTimer) clearTimeout(this._lobbyRetryTimer);
      var attempt = this._lobbyRetryAttempt++;
      var delay = this._retryDelay(attempt, 30000);
      this.toast('大厅连接断开，' + Math.round(delay / 1000) + ' 秒后重试', 'err');
      this._lobbyRetryTimer = setTimeout(function () {
        self._lobbyRetryTimer = null;
        self._lobbyReconnectPending = false;
        if (self._lobbyManualStop) return;
        self.joinLobby();
      }, delay);
    },

    _scheduleRoomReconnect: function (info) {
      var self = this;
      if (this._manualRoomClose || !this.roomHost || !this.roomPort) return false;
      var maxAttempts = this._roomJoinMeta && this.version === 'v2' ? 5 : 3;
      if (this._roomRetryAttempt >= maxAttempts) return false;
      if (this._roomReconnectPending) return true;
      this._roomReconnectPending = true;
      if (this._roomRetryTimer) clearTimeout(this._roomRetryTimer);
      var attempt = this._roomRetryAttempt++;
      var delay = this._retryDelay(attempt, 12000);
      this.toast('房间连接断开，' + Math.round(delay / 1000) + ' 秒后重连', 'err');
      this._roomRetryTimer = setTimeout(function () {
        self._roomRetryTimer = null;
        self._roomReconnectPending = false;
        if (self._manualRoomClose) return;
        self.joinRoom(self.roomHost, self.roomPort, { reconnect: true, room: self._roomJoinMeta });
      }, delay);
      return true;
    },

    _roomJoinLabel: function (host, port, room) {
      room = room || this._roomJoinMeta || null;
      var name = room && room.name ? room.name : '';
      var address = room && room.address ? room.address : (host + ':' + port);
      return (name ? (name + ' ') : '') + '(' + address + ')';
    },

    _v2JoinFailureMessage: function (host, port, room, detail) {
      var parts = ['无法加入二代房间 ' + this._roomJoinLabel(host, port, room)];
      if (room && room.gameState) parts.push('状态：' + (GW.v2StateLabel ? GW.v2StateLabel(room.gameState) : room.gameState));
      if (detail) parts.push('原因：' + detail);
      if (room && room.address) parts.push('官方大厅地址可能已过期或主机不可达，正在刷新大厅列表');
      return parts.join('；');
    },

    _saveSettings: function () {
      var bridgeInput = $('#bridgeUrl').value.trim();
      if (this.version === 'v2') this.v2BridgeUrl = bridgeInput;
      else this.bridgeUrl = bridgeInput;
      this.name = $('#playerName').value.trim() || 'Player';
      var modeSel = $('#connMode');
      var versionSel = $('#gameVersion');
      if (versionSel) { this.version = versionSel.value; localStorage.setItem('gw_version', this.version); }
      if (this.version === 'v2') {
        this.mode = 'bridge';
      } else if (modeSel) {
        this.mode = modeSel.value;
        localStorage.setItem('gw_mode', this.mode);
      }
      var hostEl = $('#directHost');
      var portEl = $('#directPort');
      if (hostEl && portEl) {
        var roomHost = hostEl.value.trim();
        var roomPort = parseInt(portEl.value, 10) || 0;
        if (this.version === 'v2') {
          roomHost = this._normalizeV2RoomHost(roomHost);
          roomPort = (!roomPort || roomPort === DEFAULT_V1_ROOM.port) ? DEFAULT_V2_ROOM.port : roomPort;
          hostEl.value = roomHost;
          portEl.value = roomPort;
        }
        localStorage.setItem(this.version === 'v2' ? 'gw_v2_host' : 'gw_v1_host', roomHost);
        localStorage.setItem(this.version === 'v2' ? 'gw_v2_port' : 'gw_v1_port', String(roomPort || ''));
      }
      if (this.version === 'v2') {
        if (this.v2BridgeUrl) localStorage.setItem('gw_v2_bridge', this.v2BridgeUrl);
        this._applyV2AvatarConfig();
      } else {
        localStorage.setItem('gw_bridge', this.bridgeUrl);
      }
      localStorage.setItem('gw_name', this.name);
    },

    // ---------------- Menu ----------------
    _bindMenu: function () {
      var self = this;
      var modeSel = $('#connMode');
      if (modeSel) modeSel.addEventListener('change', function () {
        self.mode = modeSel.value; localStorage.setItem('gw_mode', self.mode); self._applyModeUi();
      });
      var versionSel = $('#gameVersion');
      if (versionSel) versionSel.addEventListener('change', function () {
        self._switchVersion(versionSel.value);
      });
      ['#v2SkinGraphics', '#v2FaceGraphics', '#v2HatGraphics'].forEach(function (sel) {
        var el = $(sel);
        if (!el) return;
        el.addEventListener('change', function () { self._applyV2AvatarConfig(); });
        el.addEventListener('input', function () {
          clearTimeout(self._v2AvatarInputT);
          self._v2AvatarInputT = setTimeout(function () { self._applyV2AvatarConfig(); }, 350);
        });
      });
      var dbg = $('#v2DebugToggle');
      if (dbg) dbg.addEventListener('change', function () { self._setV2DebugEnabled(dbg.checked); });
      var clearLog = $('#btnV2ClearLog');
      if (clearLog) clearLog.addEventListener('click', function () {
        var app = self._desktopApp();
        if (!app || !app.ClearV2DebugLog) { self.toast('协议日志仅桌面版可用', 'err'); return; }
        app.ClearV2DebugLog().then(function (ok) {
          self.toast(ok ? '已清空 Graphwar II 协议日志' : '清空协议日志失败', ok ? 'ok' : 'err');
        }).catch(function (e) { self.toast('清空协议日志失败: ' + e, 'err'); });
      });
      $('#btnJoinGlobal').addEventListener('click', function () { self._saveSettings(); self.joinLobby(); });
      $('#btnDirectJoin').addEventListener('click', function () {
        self._saveSettings();
        var host = $('#directHost').value.trim();
        var port = parseInt($('#directPort').value, 10) || 6112;
        if (!host) { self.toast('请输入 Host/IP', 'err'); return; }
        self.joinRoom(host, port);
      });

      // ---- one-click host (collapsed by default) ----
      var hostToggle = $('#hostToggle');
      if (hostToggle) hostToggle.addEventListener('click', function () {
        var body = $('#hostBody');
        var open = body.style.display === 'none';
        body.style.display = open ? '' : 'none';
        hostToggle.setAttribute('aria-expanded', open ? 'true' : 'false');
        hostToggle.querySelector('.chev').textContent = open ? 'v' : '>';
        if (open) self._refreshHostAvailability();
        if (open) self._refreshGraphwar2LocalRooms();
      });
      var btnHostRoom = $('#btnHostRoom');
      if (btnHostRoom) btnHostRoom.addEventListener('click', function () { self._saveSettings(); self._hostRoom(); });
      var btnHostLobby = $('#btnHostLobby');
      if (btnHostLobby) btnHostLobby.addEventListener('click', function () { self._saveSettings(); self._hostLobby(); });
      var btnTunnelRoom = $('#btnTunnelRoom');
      if (btnTunnelRoom) btnTunnelRoom.addEventListener('click', function () { self._startCpolarForHostedRoom(); });
      var btnTunnelLobby = $('#btnTunnelLobby');
      if (btnTunnelLobby) btnTunnelLobby.addEventListener('click', function () { self._startCpolarForLobby(); });
      var btnJoinV2Local = $('#btnJoinV2Local');
      if (btnJoinV2Local) btnJoinV2Local.addEventListener('click', function () { self._joinV2LocalRoom(); });
      var btnCreateV2Local = $('#btnCreateV2Local');
      if (btnCreateV2Local) btnCreateV2Local.addEventListener('click', function () { self._openGraphwar2HostHelp(true); });
      var btnPublishV2Official = $('#btnPublishV2Official');
      if (btnPublishV2Official) btnPublishV2Official.addEventListener('click', function () { self._publishGraphwar2OfficialRoom(); });
      var btnStopV2OfficialPublish = $('#btnStopV2OfficialPublish');
      if (btnStopV2OfficialPublish) btnStopV2OfficialPublish.addEventListener('click', function () { self._stopGraphwar2OfficialPublish(); });
      var btnTunnelV2Local = $('#btnTunnelV2Local');
      if (btnTunnelV2Local) btnTunnelV2Local.addEventListener('click', function () { self._startCpolarForV2Local(); });
      var btnCpolarInit = $('#btnCpolarInit');
      if (btnCpolarInit) btnCpolarInit.addEventListener('click', function () { self._initCpolarAccounts(); });
      var btnCpolarRefresh = $('#btnCpolarRefresh');
      if (btnCpolarRefresh) btnCpolarRefresh.addEventListener('click', function () { self._refreshCpolarStatus(); });
      var btnCpolarStopAll = $('#btnCpolarStopAll');
      if (btnCpolarStopAll) btnCpolarStopAll.addEventListener('click', function () { self._stopAllCpolar(); });
    },

    _desktopApp: function () {
      return (window.GW_DESKTOP && window.GW_DESKTOP.app) ? window.GW_DESKTOP.app : null;
    },
    _refreshHostAvailability: function () {
      var canHost = !!this._desktopApp();
      $('#btnHostRoom').disabled = !canHost;
      $('#btnHostLobby').disabled = !canHost;
      var tunnelBtns = ['#btnTunnelRoom', '#btnTunnelLobby', '#btnTunnelV2Local', '#btnPublishV2Official', '#btnStopV2OfficialPublish', '#btnCpolarInit', '#btnCpolarRefresh', '#btnCpolarStopAll'];
      tunnelBtns.forEach(function (sel) { var el = $(sel); if (el) el.disabled = !canHost; });
      $('#hostHint').innerHTML = canHost
        ? '桌面后端已就绪。使用上方按钮即可开服并自动加入。'
        : '浏览器模式无法监听 TCP。请使用桌面版，或手动启动 <code>node server/index.js</code> 后直连加入。';
      if (canHost) this._refreshCpolarStatus();
    },

    _refreshGraphwar2LocalRooms: function () {
      var app = this._desktopApp();
      var st = $('#v2LocalDetectStatus');
      if (!st || !app || !app.Graphwar2LocalRooms) return;
      var self = this;
      app.Graphwar2LocalRooms().then(function (res) {
        var rooms = (res && Array.isArray(res.rooms)) ? res.rooms : [];
        if (!rooms.length) {
          st.textContent = '尚未检测到 Graphwar II 本地房间。';
          return;
        }
        var room = rooms[0];
        if ($('#v2LocalHost') && room.host) $('#v2LocalHost').value = room.host;
        if ($('#v2LocalPort') && room.port) $('#v2LocalPort').value = room.port;
        st.textContent = '已检测到 Graphwar II 本地房间：' + (room.address || (room.host + ':' + room.port));
        try {
          if (room.host) localStorage.setItem('gw_v2_host', room.host);
          if (room.port) localStorage.setItem('gw_v2_port', String(room.port));
        } catch (e) {}
        self._refreshCpolarStatus();
      }).catch(function (e) {
        st.textContent = '检测本地房间失败：' + e;
      });
    },

    _refreshCpolarStatus: function () {
      var self = this;
      var app = this._desktopApp();
      var summary = $('#cpolarSummary');
      var list = $('#cpolarTunnels');
      if (!summary || !list) return;
      if (!app || !app.CpolarStatus) {
        summary.textContent = 'cpolar 只在桌面版可用。';
        list.innerHTML = '';
        return;
      }
      app.CpolarStatus().then(function (st) {
        self._renderCpolarStatus(st || {});
      }).catch(function (e) {
        summary.textContent = '读取 cpolar 状态失败: ' + e;
      });
    },

    _renderCpolarStatus: function (st) {
      var summary = $('#cpolarSummary');
      var list = $('#cpolarTunnels');
      this._renderCpolarFloat(st || {});
      if (!summary || !list) return;
      var found = st.cpolarFound ? '已找到' : '未找到';
      summary.textContent = 'cpolar.exe: ' + found + ' | 账号 ' + (st.accountCount || 0) + '/5 | token ' + (st.tokenCount || 0) + ' | 配置: ' + (st.configPath || '');
      if (st.error) summary.textContent += ' | ' + st.error;
      list.innerHTML = '';
      var tunnels = st.tunnels || [];
      if (!tunnels.length) {
        var empty = document.createElement('div');
        empty.className = 'cpolar-empty';
        empty.textContent = '暂无运行中的 TCP 隧道。';
        list.appendChild(empty);
      }
      var self = this;
      tunnels.forEach(function (t) {
        list.appendChild(self._cpolarTunnelRow(t));
      });
      var last = st.lastTunnels || [];
      if (last.length) {
        var title = document.createElement('div');
        title.className = 'cpolar-last-title';
        title.textContent = '最近地址';
        list.appendChild(title);
        last.slice(0, 3).forEach(function (t) {
          var row = document.createElement('div');
          row.className = 'cpolar-row old';
          row.innerHTML = '<b>' + GW.sanitizeText(t.label || '隧道', 40) + '</b><code>' + GW.sanitizeText(t.publicUrl || ((t.publicHost || '') + ':' + (t.publicPort || '')), 120) + '</code>';
          list.appendChild(row);
        });
      }
    },

    _cpolarTunnelRow: function (t) {
      var self = this;
      var row = document.createElement('div');
      row.className = 'cpolar-row' + (t.running ? '' : ' stopped');
      var text = t.publicUrl || (t.lastError ? ('错误: ' + t.lastError) : '正在等待公网地址...');
      var code = document.createElement('code');
      code.textContent = text;
      var meta = document.createElement('span');
      meta.className = 'cpolar-meta';
      meta.textContent = (t.label || '隧道') + ' -> ' + (t.localTarget || t.localPort || '?') + (t.inspectAddr ? (' | 检查 ' + t.inspectAddr) : '') + (t.running ? ' | 运行中' : ' | 已停止');
      var actions = document.createElement('span');
      actions.className = 'cpolar-actions';
      var copyAddr = document.createElement('button');
      copyAddr.className = 'btn small';
      copyAddr.type = 'button';
      copyAddr.textContent = '复制地址';
      copyAddr.disabled = !t.publicUrl;
      copyAddr.addEventListener('click', function () { self._copyText(t.publicUrl); });
      var copyHost = document.createElement('button');
      copyHost.className = 'btn small';
      copyHost.type = 'button';
      copyHost.textContent = '复制 host:port';
      copyHost.disabled = !(t.publicHost && t.publicPort);
      copyHost.addEventListener('click', function () { self._copyText(t.publicHost + ':' + t.publicPort); });
      var stop = document.createElement('button');
      stop.className = 'btn small ghost';
      stop.type = 'button';
      stop.textContent = '关闭';
      stop.disabled = !t.running;
      stop.addEventListener('click', function () { self._stopCpolar(t.id); });
      actions.appendChild(copyAddr);
      actions.appendChild(copyHost);
      actions.appendChild(stop);
      row.appendChild(meta);
      row.appendChild(code);
      row.appendChild(actions);
      return row;
    },

    _renderCpolarFloat: function (st) {
      var box = $('#cpolarFloat');
      if (!box) return;
      var tunnels = (st.tunnels || []).filter(function (t) { return t && t.running; });
      if (!tunnels.length) {
        box.style.display = 'none';
        box.innerHTML = '';
        return;
      }
      var self = this;
      box.style.display = '';
      box.innerHTML = '';
      var details = document.createElement('details');
      details.className = 'cpolar-float-card';
      if (this._cpolarFloatOpen) details.open = true;
      details.addEventListener('toggle', function () { self._cpolarFloatOpen = details.open; });
      var summary = document.createElement('summary');
      var first = tunnels[0];
      var title = document.createElement('span');
      title.className = 'cpolar-float-title';
      title.textContent = 'cpolar ' + tunnels.length + ' 个隧道';
      var addr = document.createElement('code');
      addr.textContent = first.publicUrl || '等待公网地址...';
      summary.appendChild(title);
      summary.appendChild(addr);
      details.appendChild(summary);
      var list = document.createElement('div');
      list.className = 'cpolar-float-list';
      tunnels.forEach(function (t) {
        var item = document.createElement('div');
        item.className = 'cpolar-float-row';
        var meta = document.createElement('div');
        meta.className = 'cpolar-float-meta';
        meta.textContent = (t.label || '隧道') + ' -> ' + (t.localTarget || t.localPort || '?') + (t.inspectAddr ? (' | 检查 ' + t.inspectAddr) : '');
        var code = document.createElement('code');
          code.textContent = t.publicUrl || (t.lastError ? ('错误: ' + t.lastError) : '正在等待公网地址...');
        var actions = document.createElement('div');
        actions.className = 'cpolar-actions';
        var copyAddr = document.createElement('button');
        copyAddr.className = 'btn small';
        copyAddr.type = 'button';
        copyAddr.textContent = '地址';
        copyAddr.disabled = !t.publicUrl;
        copyAddr.addEventListener('click', function (e) {
          e.preventDefault();
          self._copyText(t.publicUrl);
        });
        var copyHost = document.createElement('button');
        copyHost.className = 'btn small';
        copyHost.type = 'button';
        copyHost.textContent = 'host:port';
        copyHost.disabled = !(t.publicHost && t.publicPort);
        copyHost.addEventListener('click', function (e) {
          e.preventDefault();
          self._copyText(t.publicHost + ':' + t.publicPort);
        });
        var stop = document.createElement('button');
        stop.className = 'btn small ghost';
        stop.type = 'button';
        stop.textContent = '停止';
        stop.addEventListener('click', function (e) {
          e.preventDefault();
          self._stopCpolar(t.id);
        });
        actions.appendChild(copyAddr);
        actions.appendChild(copyHost);
        actions.appendChild(stop);
        item.appendChild(meta);
        item.appendChild(code);
        item.appendChild(actions);
        if (t.recentLog && t.recentLog.length) {
          var log = document.createElement('pre');
          log.className = 'cpolar-float-log';
          log.textContent = t.recentLog.slice(-6).join('\n');
          item.appendChild(log);
        }
        list.appendChild(item);
      });
      details.appendChild(list);
      box.appendChild(details);
    },

    _copyText: function (text) {
      var self = this;
      if (!text) return;
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(function () {
          self.toast('已复制 ' + text, 'ok');
        }).catch(function () {
          self.toast('复制失败，请手动选择地址', 'err');
        });
      } else {
        this.toast(text, 'ok');
      }
    },

    _initCpolarAccounts: function () {
      var self = this, app = this._desktopApp();
      if (!app || !app.CpolarInitAccounts) { this.toast('cpolar 仅桌面版可用', 'err'); return; }
      this.toast('正在初始化 cpolar 账号...');
      app.CpolarInitAccounts().then(function (res) {
        self._renderCpolarStatus((res && res.status) || {});
        if (res && res.ok) self.toast('cpolar 账号/token 已准备好', 'ok');
        else self.toast('cpolar 初始化失败: ' + ((res && res.error) || '未知错误'), 'err');
      }).catch(function (e) { self.toast('cpolar 初始化失败: ' + e, 'err'); });
    },

    _startCpolarTCP: function (label, port, target) {
      var self = this, app = this._desktopApp();
      if (!app || !app.CpolarStartTCP) { this.toast('cpolar 仅桌面版可用', 'err'); return; }
      port = parseInt(port, 10) || 0;
      if (!target && (!port || port <= 0)) { this.toast('没有可穿透的本地端口', 'err'); return; }
      this.toast('正在启动 cpolar TCP 隧道...');
      var call = target && app.CpolarStartTCPForTarget ? app.CpolarStartTCPForTarget(label, target) : app.CpolarStartTCP(label, port);
      call.then(function (res) {
        if (!res || !res.ok) {
          self.toast('cpolar 启动失败: ' + ((res && res.error) || '未知错误'), 'err');
          self._refreshCpolarStatus();
          return;
        }
        self._refreshCpolarStatus();
        var t = res.tunnel || {};
        if (t.publicUrl) self.toast('cpolar 已启动 ' + t.publicUrl, 'ok');
        else self.toast('cpolar 已启动，正在等待公网地址...', 'ok');
        self._scheduleCpolarRefresh();
      }).catch(function (e) { self.toast('cpolar 启动失败: ' + e, 'err'); });
    },

    _scheduleCpolarRefresh: function () {
      var self = this;
      if (this._cpolarPoll) clearInterval(this._cpolarPoll);
      this._cpolarPoll = setInterval(function () {
        self._refreshCpolarStatus();
        var box = $('#cpolarFloat');
        if (box && box.style.display === 'none') {
          clearInterval(self._cpolarPoll);
          self._cpolarPoll = null;
        }
      }, 2500);
    },

    _stopCpolar: function (id) {
      var self = this, app = this._desktopApp();
      if (!app || !app.CpolarStop) return;
      app.CpolarStop(id).then(function () {
        self._refreshCpolarStatus();
        self.toast('cpolar 隧道已关闭', 'ok');
      }).catch(function (e) { self.toast('关闭失败: ' + e, 'err'); });
    },

    _stopAllCpolar: function () {
      var self = this, app = this._desktopApp();
      if (!app || !app.CpolarStopAll) return;
      app.CpolarStopAll().then(function () {
        self._refreshCpolarStatus();
        self.toast('已关闭全部 cpolar 隧道', 'ok');
      }).catch(function (e) { self.toast('关闭失败: ' + e, 'err'); });
    },

    _startCpolarForHostedRoom: function () {
      var port = this._hostedPort || this.roomPort || parseInt($('#hostRoomPort').value, 10) || 0;
      this._startCpolarTCP('graphwar1_room', port);
    },

    _startCpolarForLobby: function () {
      var port = parseInt($('#globalPort').value, 10) || parseInt($('#hostLobbyPort').value, 10) || 0;
      this._startCpolarTCP('graphwar1_lobby', port);
    },

    _joinV2LocalRoom: function () {
      var host = ($('#v2LocalHost').value || '').trim() || '::1';
      var port = parseInt($('#v2LocalPort').value, 10) || DEFAULT_V2_ROOM.port;
      this.version = 'v2';
      var versionSel = $('#gameVersion'); if (versionSel) versionSel.value = 'v2';
      this.mode = 'bridge';
      this._applyModeUi();
      this.joinRoom(host, port);
    },

    _publishGraphwar2OfficialRoom: function () {
      var self = this;
      var app = this._desktopApp();
      if (!app || !app.PublishGraphwar2OfficialRoom) {
        this.toast('发布 Graphwar II 官服房间需要桌面版', 'err');
        return;
      }
      var host = ($('#v2LocalHost').value || '').trim() || '127.0.0.1';
      var port = parseInt($('#v2LocalPort').value, 10) || DEFAULT_V2_ROOM.port;
      var name = (this.name || 'Player') + "'s Room";
      this.toast('正在发布 Graphwar II 房间到官方大厅...');
      app.PublishGraphwar2OfficialRoom(name, host, port).then(function (res) {
        var st = $('#v2LocalDetectStatus');
        if (res && res.ok) {
          var status = res.status || {};
          if (st) st.textContent = '正在发布到官方大厅：' + (status.address || (host + ':' + port)) + '，broker ' + (status.broker || '?');
          self.toast('已开始发布二代房间到官方大厅，等待上榜...', 'ok');
          self._watchGraphwar2OfficialHost(0);
          if (self._v2LobbyRefresh) setTimeout(function () { self._v2LobbyRefresh(true); }, 1800);
        } else {
          var err = (res && res.error) || '未知错误';
          if (st) st.textContent = '发布到官方大厅失败：' + err;
          self.toast('二代官服发布失败：' + err, 'err');
        }
      }).catch(function (e) {
        self.toast('二代官服发布失败：' + e, 'err');
      });
    },

    _stopGraphwar2OfficialPublish: function () {
      var self = this;
      var app = this._desktopApp();
      if (!app || !app.StopGraphwar2OfficialPublisher) {
        this.toast('发布 Graphwar II 官服房间需要桌面版', 'err');
        return;
      }
      app.StopGraphwar2OfficialPublisher().then(function () {
        var st = $('#v2LocalDetectStatus');
        if (st) st.textContent = '已停止向 Graphwar II 官方大厅发布。';
        self.toast('已停止发布二代官服房间', 'ok');
        if (self._v2LobbyRefresh) setTimeout(function () { self._v2LobbyRefresh(true); }, 1000);
      }).catch(function (e) {
        self.toast('停止发布失败：' + e, 'err');
      });
    },

    _startCpolarForV2Local: function () {
      var host = ($('#v2LocalHost').value || '').trim() || '::1';
      var port = parseInt($('#v2LocalPort').value, 10) || DEFAULT_V2_ROOM.port;
      var target = host.indexOf(':') >= 0 && host[0] !== '[' ? ('[' + host + ']:' + port) : (host + ':' + port);
      this._startCpolarTCP('graphwar2_local', port, target);
    },

      // One room + auto-join (standalone GraphServer)
    _hostRoom: function () {
      var self = this;
      var app = this._desktopApp();
      if (!app) { this.toast('开服需要桌面版', 'err'); return; }
      var port = parseInt($('#hostRoomPort').value, 10) || 0;
      this.toast('正在启动房间...');
      app.CreateRoom(this.name + "'s Room", port).then(function (port) {
        if (!port || port <= 0) { self.toast('启动房间失败，端口可能被占用', 'err'); return; }
        self._hostedPort = port;      // remember for room management
        self._isHost = true;
        self.mode = 'direct';
        self.joinRoom('127.0.0.1', port);
        self.toast('Room started on port ' + port + ', joining...', 'ok');
      }).catch(function (e) { self.toast('启动房间失败：' + e, 'err'); });
    },

    // Start the full lobby + room pool, then enter the lobby.
    _hostLobby: function () {
      var self = this;
      var app = this._desktopApp();
      if (!app) { this.toast('开服需要桌面版', 'err'); return; }
      var publicIp = ($('#hostPublicIp').value || '').trim() || '127.0.0.1';
      var lobbyPort = parseInt($('#hostLobbyPort').value, 10) || 0;
      this.toast('正在启动大厅...');
      app.StartBackendOn(publicIp, lobbyPort, 0).then(function (lobbyPort) {
        if (!lobbyPort || lobbyPort <= 0) {
          try { app.LastError().then(function (e) { self.toast('开启大厅失败: ' + (e || ''), 'err'); }); } catch (e) { self.toast('开启大厅失败', 'err'); }
          return;
        }
        // point the UI at our own lobby in direct mode and enter it
        self.mode = 'direct';
        var modeSel = $('#connMode'); if (modeSel) modeSel.value = 'direct';
        $('#globalHost').value = '127.0.0.1';
        $('#globalPort').value = lobbyPort;
        self._applyModeUi();
        self.toast('大厅已启动，端口 ' + lobbyPort + '，正在进入...', 'ok');
        self.joinLobby();
      }).catch(function (e) { self.toast('开启大厅失败: ' + e, 'err'); });
    },

    // ---------------- Lobby ----------------
    joinLobby: function () {
      var self = this;
      if (this.version === 'v2') {
        this._joinGraphwar2Lobby();
        return;
      }
      this._lobbyManualStop = false;
      if (this._lobbyRetryTimer) { clearTimeout(this._lobbyRetryTimer); this._lobbyRetryTimer = null; }
      this._lobbyReconnectPending = false;
      var host = $('#globalHost').value.trim() || 'www.graphwar.com';
      var port = parseInt($('#globalPort').value, 10) || 23761;
      this._configureTranslate();
      if (this.lobby && this.lobby.running) {
        try { this.lobby.stop(); } catch (e) {}
      }
      this.lobby = new GW.GlobalClient(this._connOpts());
      this.lobby.on('joined', function () { self._lobbyRetryAttempt = 0; self.toast('Connected to lobby', 'ok'); self.show('lobby'); });
      this.lobby.on('players', function (ps) { self._renderLobbyPlayers(ps); });
      this.lobby.on('rooms', function (rs) { self._renderRooms(rs); });
      this.lobby.on('chat', function (name, msg) { self._addChat($('#lobbyChat'), name, msg); });
      this.lobby.on('room_invalid', function () {
        // The room itself is fine and you're already in it; only the OFFICIAL
        // lobby's reachability probe failed (NAT/no port-forward), so others
        // can't see it in the lobby. They can still direct-join your IP:port.
        var port = self._hostedRoom ? self._hostedRoom.port : '?';
        self.toast('房间可以游玩，但官方大厅无法验证你的公网端口。其他人仍可用你的 IP:' + port + ' 直连。', 'err');
        self._expectRoomInvalid = false;
      });
      this.lobby.on('disconnected', function (info) {
        self.toast('Lobby disconnected', 'err');
        self._scheduleLobbyReconnect(info);
      });
      this.lobby.on('neterror', function (i) {
        self.toast('桥接/大厅连接错误：' + (i && i.error || '连接失败'), 'err');
        self._scheduleLobbyReconnect(i);
      });
      this.lobby.join(host, port, this.name);
    },

    _joinGraphwar2Lobby: function () {
      var self = this;
      var app = this._desktopApp();
      if (!app || !app.Graphwar2LobbyRooms) {
        this.toast('Graphwar II 大厅需要桌面版', 'err');
        return;
      }
      this._configureTranslate();
      this._v2LobbyRetryAttempt = 0;
      this.lobby = {
        running: true,
        players: [],
        rooms: [],
        stop: function () {
          this.running = false;
          if (self._v2LobbyTimer) { clearInterval(self._v2LobbyTimer); self._v2LobbyTimer = null; }
          if (self._v2LobbyRetryTimer) { clearTimeout(self._v2LobbyRetryTimer); self._v2LobbyRetryTimer = null; }
        },
        sendChat: function () {}
      };
      this.show('lobby');
      this._renderLobbyPlayers([]);
      this._setGraphwar2LobbyStatus('正在获取 Graphwar II 房间列表...');
      this.toast('正在获取 Graphwar II 房间列表...');
      function refresh(silent) {
        if (self._v2LobbyLoading || !self.lobby || !self.lobby.running) return;
        self._v2LobbyLoading = true;
        app.Graphwar2LobbyRooms().then(function (res) {
          if (!self.lobby || !self.lobby.running) return;
          if (!res || res.ok === false) {
            self._setGraphwar2LobbyStatus('Graphwar II 大厅错误：' + ((res && res.error) || '未知错误'));
            if (!silent) self.toast('获取二代官服房间列表失败：' + ((res && res.error) || '未知错误'), 'err');
            self._scheduleV2LobbyRefresh(true);
            return;
          }
          var rooms = Array.isArray(res.rooms) ? res.rooms : [];
          self.lobby.rooms = rooms;
          self._renderRooms(rooms);
          self._v2LobbyRetryAttempt = 0;
          self._setGraphwar2LobbyStatus(rooms.length ? '点击房间即可加入。房间列表会自动刷新。' : '当前官方大厅没有列出 Graphwar II 房间。');
          if (!silent) self.toast('已加载 ' + rooms.length + ' 个 Graphwar II 房间', rooms.length ? 'ok' : '');
          self._scheduleV2LobbyRefresh(false);
        }).catch(function (e) {
          if (!self.lobby || !self.lobby.running) return;
          self._setGraphwar2LobbyStatus('Graphwar II 大厅错误：' + e);
          if (!silent) self.toast('Graphwar II 大厅错误：' + e, 'err');
          self._scheduleV2LobbyRefresh(true);
        }).then(function () {
          self._v2LobbyLoading = false;
        });
      }
      this._v2LobbyRefresh = refresh;
      refresh(false);
    },

    _scheduleV2LobbyRefresh: function (failed) {
      var self = this;
      if (this._v2LobbyRetryTimer) clearTimeout(this._v2LobbyRetryTimer);
      if (!this.lobby || !this.lobby.running) return;
      var delay = failed ? this._retryDelay(this._v2LobbyRetryAttempt++, 30000) : 10000;
      this._v2LobbyRetryTimer = setTimeout(function () {
        self._v2LobbyRetryTimer = null;
        if (self._v2LobbyRefresh) self._v2LobbyRefresh(true);
      }, delay);
    },

    _setGraphwar2LobbyStatus: function (msg) {
      var chat = $('#lobbyChat');
      if (!chat) return;
      chat.innerHTML = '';
      var div = document.createElement('div');
      div.className = 'chat-line system';
      div.textContent = msg || '';
      chat.appendChild(div);
    },

    _bindLobby: function () {
      var self = this;
      $('#btnLobbyBack').addEventListener('click', function () {
        self._lobbyManualStop = true;
        if (self._lobbyRetryTimer) { clearTimeout(self._lobbyRetryTimer); self._lobbyRetryTimer = null; }
        self._lobbyReconnectPending = false;
        if (self.lobby) self.lobby.stop();
        self.show('menu');
      });
      $('#lobbyChatForm').addEventListener('submit', function (e) {
        e.preventDefault();
        var v = $('#lobbyChatInput').value.trim();
        if (self.version === 'v2') { $('#lobbyChatInput').value = ''; return; }
        if (v && self.lobby) { self.lobby.sendChat(v); $('#lobbyChatInput').value = ''; }
      });
      // create-room modal
      $('#btnCreateRoom').addEventListener('click', function () {
        if (self.version === 'v2') { self._openGraphwar2HostHelp(); return; }
        self._openCreateRoom();
      });
      $('#crCancel').addEventListener('click', function () { $('#createRoomModal').style.display = 'none'; });
      $('#crConfirm').addEventListener('click', function () { self._confirmCreateRoom(); });
      // blocklist modal
      $('#btnBlocklist').addEventListener('click', function () { self._openBlocklist(); });
      $('#blClose').addEventListener('click', function () { $('#blocklistModal').style.display = 'none'; self._renderRooms(self._lastRooms); });
      $('#blAddForm').addEventListener('submit', function (e) {
        e.preventDefault();
        var v = $('#blInput').value.trim();
        if (v) { self.blockPlayer(v); $('#blInput').value = ''; self._renderBlocklist(); }
      });
    },

    // ---- blocklist management ----
    _saveBlocked: function () { try { localStorage.setItem('gw_blocked', JSON.stringify(this.blocked)); } catch (e) {} },
    isBlocked: function (name) {
      var l = (name || '').toLowerCase();
      return this.blocked.some(function (n) { return n.toLowerCase() === l; });
    },
    blockPlayer: function (name) {
      name = (name || '').trim();
      if (name && !this.isBlocked(name)) { this.blocked.push(name); this._saveBlocked(); }
      // dropping the cached rosters forces a re-evaluation on next render/peek
      if (this.current === 'lobby') this._renderRooms(this._lastRooms);
    },
    unblockPlayer: function (name) {
      var l = (name || '').toLowerCase();
      this.blocked = this.blocked.filter(function (n) { return n.toLowerCase() !== l; });
      this._saveBlocked();
      if (this.current === 'lobby') this._renderRooms(this._lastRooms);
    },
    _openBlocklist: function () {
      this._renderBlocklist();
      $('#blocklistModal').style.display = 'flex';
    },
    _renderBlocklist: function () {
      var self = this;
      var ul = $('#blList');
      ul.innerHTML = '';
      if (!this.blocked.length) { ul.innerHTML = '<li style="color:var(--muted)">名单为空</li>'; return; }
      this.blocked.forEach(function (n) {
        var li = document.createElement('li');
        li.style.display = 'flex'; li.style.justifyContent = 'space-between'; li.style.alignItems = 'center';
        var span = document.createElement('span'); span.textContent = n; li.appendChild(span);
        var btn = document.createElement('button'); btn.className = 'btn small ghost'; btn.textContent = '移除';
        btn.addEventListener('click', function () { self.unblockPlayer(n); self._renderBlocklist(); });
        li.appendChild(btn);
        ul.appendChild(li);
      });
    },

    _openCreateRoom: function () {
      var desktop = !!(window.GW_DESKTOP && window.GW_DESKTOP.app && window.GW_DESKTOP.app.CreateRoom);
      // Are we connected to the OFFICIAL lobby (it port-probes back to you)?
      var host = ($('#globalHost').value || '').trim();
      var official = this.mode === 'bridge' && /graphwar\.com$/i.test(host);
      this._crOfficial = official;
      $('#crName').value = this.name + "'s Room";
      $('#crPort').value = official ? 6112 : 0; // 0 = OS-assigned for local/self-hosted
      $('#crPublic').checked = !official;       // self-hosted: list by default; official: opt-in
      $('#crPublic').parentElement.style.display = this.lobby ? '' : 'none';
      $('#crConfirm').disabled = !desktop;
      $('#crHint').innerHTML = desktop
        ? (official
            ? '官方大厅会反向验证你的公网端口。请把所选端口转发到本机，否则房间只能直连加入。'
            : '创建本地房间。' + (this.lobby ? ' 可选择是否登记到当前大厅。' : '') + ' 同一端口兼容原版 Java 客户端和网页客户端。')
        : '浏览器模式无法监听 TCP。请使用桌面版，或手动启动 <code>node server/index.js</code> 后直连加入。';
      $('#createRoomModal').style.display = 'flex';
    },

    _openGraphwar2HostHelp: function (local) {
      var self = this;
      var app = this._desktopApp();
      local = local === true;
      var msg = local
        ? '创建内置 Graphwar II 兼容本地房间，然后回到这里加入或穿透本地房间。'
        : '创建内置 Graphwar II 兼容房间，并发布到官方 broker；二代官方开房不需要内网穿透。';
      if (!local) {
        if (app && app.CreateGraphwar2Room) {
        app.CreateGraphwar2Room(this.name + "'s Room", 0, true).then(function (res) {
          if (res && res.ok) {
            self.toast('已创建并发布二代兼容官服房间', 'ok');
            var statusEl = $('#v2LocalDetectStatus');
            if (statusEl) {
              var parts = [];
            if (res.officialListed) parts.push('已上榜');
            if (res.publisher && res.publisher.broker) parts.push('broker ' + res.publisher.broker);
              if (res.verifyError) parts.push('等待验证：' + res.verifyError);
              statusEl.textContent = parts.length ? '官方房间已准备。' + parts.join(' | ') : '官方房间已准备。';
            }
            var rooms = Array.isArray(res.rooms) ? res.rooms : [];
            if (rooms[0]) {
              var hostEl = $('#v2LocalHost');
              var portEl = $('#v2LocalPort');
                if (hostEl) hostEl.value = rooms[0].host || '127.0.0.1';
                if (portEl) portEl.value = rooms[0].port || 0;
              }
            } else self.toast('二代官服房间创建失败：' + ((res && res.error) || '未知错误'), 'err');
            self._watchGraphwar2OfficialHost(0);
            if (self._v2LobbyRefresh) setTimeout(function () { self._v2LobbyRefresh(true); }, 2500);
          }).catch(function () {
            self.toast(msg, 'err');
          });
          return;
        }
        this.toast(msg, 'err');
        return;
      }
      function openPanel() {
        var body = $('#hostBody');
        var toggle = $('#hostToggle');
        if (body && body.style.display === 'none') {
          body.style.display = '';
          if (toggle) {
            toggle.setAttribute('aria-expanded', 'true');
            var chev = toggle.querySelector('.chev'); if (chev) chev.textContent = 'v';
          }
        }
        var host = $('#v2LocalHost');
        var port = $('#v2LocalPort');
        if (host && !host.value.trim()) host.value = DEFAULT_V2_ROOM.host;
        if (port && !parseInt(port.value, 10)) port.value = DEFAULT_V2_ROOM.port;
        self.show('menu');
        self._refreshHostAvailability();
      }
      function applyDetected(rooms, quiet) {
        rooms = Array.isArray(rooms) ? rooms : [];
        if (!rooms.length) return false;
        var room = rooms[0];
        var hostEl = $('#v2LocalHost');
        var portEl = $('#v2LocalPort');
        if (hostEl && room.host) hostEl.value = room.host;
        if (portEl && room.port) portEl.value = room.port;
        var st = $('#v2LocalDetectStatus');
        if (st) st.textContent = '已检测到 Graphwar II 本地房间：' + (room.address || (room.host + ':' + room.port)) + ' (pid ' + (room.pid || '?') + ')';
        if (!quiet) self.toast('已检测到 Graphwar II 本地房间：' + (room.address || (room.host + ':' + room.port)), 'ok');
        try {
          if (room.host) localStorage.setItem('gw_v2_host', room.host);
          if (room.port) localStorage.setItem('gw_v2_port', String(room.port));
        } catch (e) {}
        return true;
      }
      function scanLocalRooms(attempt) {
        if (!app || !app.Graphwar2LocalRooms) return;
        app.Graphwar2LocalRooms().then(function (res) {
          if (applyDetected(res && res.rooms, attempt > 0)) return;
          var st = $('#v2LocalDetectStatus');
          if (st) st.textContent = '正在等待 Graphwar II 本地建房：请在官方客户端的局域网大厅点击创建房间。';
          if (attempt < 24) setTimeout(function () { scanLocalRooms(attempt + 1); }, 1500);
          else if (st) st.textContent = '没有检测到本地房间。请保持 Graphwar II 打开，创建局域网房间后再次点击创建二代本地房间。';
        }).catch(function (e) {
          var st = $('#v2LocalDetectStatus');
          if (st) st.textContent = '检测本地房间失败：' + e;
        });
      }
      if (app && app.CreateGraphwar2Room) {
        app.CreateGraphwar2Room(this.name + "'s Room", parseInt($('#v2LocalPort').value, 10) || DEFAULT_V2_ROOM.port, false).then(function (res) {
          var reason = msg;
          if (res && res.ok) reason = '已创建内置 Graphwar II 兼容本地房间。';
          else if (res && res.error) reason = msg + ' 创建失败: ' + res.error;
          self.toast(reason, res && res.ok ? 'ok' : 'err');
          openPanel();
          if (!applyDetected(res && res.rooms, true)) scanLocalRooms(0);
        }).catch(function () {
          self.toast(msg, 'err');
          openPanel();
          scanLocalRooms(0);
        });
        return;
      }
      this.toast(msg, 'err');
      openPanel();
    },

    _watchGraphwar2OfficialHost: function (attempt) {
      var self = this;
      var app = this._desktopApp();
      if (!app || !app.Graphwar2HostedOfficialRoom) return;
      attempt = attempt || 0;
      app.Graphwar2HostedOfficialRoom().then(function (res) {
        var hosted = res && res.hosted;
        var local = hosted && hosted.localRoom;
        var lobby = hosted && hosted.lobbyRoom;
        if (res && res.ok && lobby && lobby.port) {
          var statusEl = $('#v2LocalDetectStatus');
          if (statusEl) {
            var parts = [];
            if (hosted.reason) parts.push(hosted.reason);
            if (hosted.publisher && hosted.publisher.broker) parts.push('broker ' + hosted.publisher.broker);
              statusEl.textContent = '已匹配官方大厅房间。' + parts.join(' | ');
          }
          var host = lobby.ip || (local && local.host) || DEFAULT_V2_ROOM.host;
          var port = lobby.port || (local && local.port) || DEFAULT_V2_ROOM.port;
          var hostEl = $('#directHost');
          var portEl = $('#directPort');
          if (hostEl) hostEl.value = host;
          if (portEl) portEl.value = port;
          try {
            localStorage.setItem('gw_v2_host', host);
            localStorage.setItem('gw_v2_port', String(port));
          } catch (e) {}
          self.toast('已在官方大厅检测到你创建的二代房间：' + (lobby.name || lobby.address || (host + ':' + port)), 'ok');
          if (self._v2LobbyRefresh) self._v2LobbyRefresh(true);
          return;
        }
        if (local && local.port && attempt === 0) {
          self.toast('已检测到本地二代 RoomServer，等待官方 broker 上榜...', 'ok');
        }
        if (attempt < 45) {
          setTimeout(function () { self._watchGraphwar2OfficialHost(attempt + 1); }, 2000);
        } else {
          var err = (res && res.error) || '未检测到官方大厅房间';
          self.toast('二代官服房间检测超时：' + err, 'err');
        }
      }).catch(function (e) {
        if (attempt < 45) setTimeout(function () { self._watchGraphwar2OfficialHost(attempt + 1); }, 2000);
        else self.toast('二代官服房间检测失败：' + e, 'err');
      });
    },

    _confirmCreateRoom: function () {
      var self = this;
      var app = (window.GW_DESKTOP && window.GW_DESKTOP.app) ? window.GW_DESKTOP.app : null;
      if (!app || !app.CreateRoom) { this.toast('开服需要桌面版', 'err'); return; }
      var name = $('#crName').value.trim() || (this.name + "'s Room");
      var wantPort = parseInt($('#crPort').value, 10) || 0;
      var publiclist = $('#crPublic').checked;
      var official = this._crOfficial;
      $('#createRoomModal').style.display = 'none';
      this.toast('正在创建房间...');

      // Host the room locally on the requested port (0 => OS-assigned). This
      // ALWAYS yields a working room you can play in; lobby listing is separate.
      app.CreateRoom(name, wantPort).then(function (port) {
        if (!port || port <= 0) { self.toast('创建房间失败，端口可能被占用', 'err'); return; }
        self._hostedRoom = { name: name, port: port };
        // Register with the lobby only if the user opted in. The OFFICIAL lobby
        // probes back to verify reachability; if that fails it sends ROOM_INVALID
        // The room still works for you and anyone you give IP:port to.
        if (publiclist && self.lobby && self.lobby.createRoom) {
          self._expectRoomInvalid = official;
          self.lobby.createRoom(name, port);
        }
        self._hostedPort = port; self._isHost = true; // we host this room -> manageable
        self.mode = 'direct';
        self.joinRoom('127.0.0.1', port);
        self.toast('房间已创建在端口 ' + port + '，正在加入...', 'ok');
      }).catch(function (e) { self.toast('创建房间失败：' + e, 'err'); });
    },

    _renderLobbyPlayers: function (players) {
      var self = this;
      var el = $('#lobbyPlayers');
      el.innerHTML = '';
      // Original GlobalClient hides the "SERVERPEWPEW" heartbeat sentinel.
      var visible = players.filter(function (p) { return p.name !== 'SERVERPEWPEW'; });
      visible.forEach(function (p) {
        var li = document.createElement('li');
        li.style.display = 'flex'; li.style.justifyContent = 'space-between'; li.style.alignItems = 'center';
        var span = document.createElement('span');
        span.className = 'pl-name'; span.textContent = p.name;
        if (self.isBlocked(p.name)) span.textContent = '[已屏蔽] ' + p.name;
        li.appendChild(span);
        var btn = document.createElement('button');
        btn.className = 'pl-block'; btn.type = 'button';
        btn.title = self.isBlocked(p.name) ? 'Unblock' : 'Block rooms containing this player';
        btn.textContent = self.isBlocked(p.name) ? 'Unblock' : 'Block';
        btn.addEventListener('click', function () {
          if (self.isBlocked(p.name)) self.unblockPlayer(p.name); else self.blockPlayer(p.name);
          self._renderLobbyPlayers(self.lobby ? self.lobby.players : visible);
        });
        li.appendChild(btn);
        el.appendChild(li);
      });
      $('#lobbyPlayerCount').textContent = visible.length;
    },

    _renderRooms: function (rooms) {
      var self = this;
      this._lastRooms = rooms;
      var el = $('#roomList');
      el.innerHTML = '';
      $('#roomCount').textContent = rooms.length;
      if (!rooms.length) { el.innerHTML = '<div class="empty">暂无房间，点击右上角“创建房间”</div>'; return; }
      var modeShort = ['y', "y'", "y''"];

      // Decide blocked status from peeked rosters; rooms with a blocked player
      // sort to the BOTTOM and render collapsed/dimmed.
      var annotated = rooms.map(function (r) {
        var key = r.ip + ':' + r.port;
        var roster = self._roomRosters[key];
        var hit = roster ? self._blockedInRoster(roster.names) : null;
        return { r: r, blockedBy: hit };
      });
      annotated.sort(function (a, b) {
        var ab = a.blockedBy ? 1 : 0, bb = b.blockedBy ? 1 : 0;
        return ab - bb; // blocked rooms last, stable otherwise
      });

      var anyBlocked = false;
      annotated.forEach(function (a) {
        var r = a.r;
        var div = document.createElement('div');
        div.className = 'room-card' + (a.blockedBy ? ' blocked' : '');
        var rn = document.createElement('span');
        rn.className = 'room-name';
        rn.textContent = r.name + (a.blockedBy ? '  [已屏蔽] ' + a.blockedBy : '');
        var rp = document.createElement('span');
        rp.className = 'room-players';
        rp.textContent = r.numPlayers + '/10';
        var rm = document.createElement('span');
        rm.className = 'room-mode';
        if (self.version === 'v2') {
          var turnLabel = v2Meta('turn', r.turnMode).label || (r.turnMode === 'SimultaneousTurns' ? '同时确认' : '轮流发射');
          rm.textContent = (r.locked ? '锁定 ' : '') + turnLabel;
          rm.title = [
            GW.v2StateLabel ? GW.v2StateLabel(r.gameState) : r.gameState,
            v2Meta('func', r.functionMode).label,
            v2Meta('axis', r.axisMode).label,
            turnLabel,
            v2Meta('time', r.timeMode).label
          ].filter(Boolean).join(' / ');
        } else {
          rm.textContent = modeShort[r.mode] || 'y';
        }
        var ri = document.createElement('span');
        ri.className = 'room-ip';
        ri.textContent = self.version === 'v2'
          ? ((r.ip + ':' + r.port) + (r.gameState ? (' - ' + (GW.v2StateLabel ? GW.v2StateLabel(r.gameState) : r.gameState)) : '') + (r.timeMode ? (' - ' + v2Meta('time', r.timeMode).label) : ''))
          : (r.ip + ':' + r.port);
        div.appendChild(rn);
        div.appendChild(rp);
        div.appendChild(rm);
        div.appendChild(ri);
        div.addEventListener('click', function () { self.joinRoom(r.ip, r.port, { room: r }); });
        if (a.blockedBy) anyBlocked = true;
        el.appendChild(div);
      });
      // separator label before the blocked group, if any
      if (anyBlocked) {
        var firstBlocked = el.querySelector('.room-card.blocked');
        if (firstBlocked) {
          var sep = document.createElement('div');
          sep.className = 'room-sep';
          sep.textContent = '-- 包含已屏蔽玩家的房间 --';
          el.insertBefore(sep, firstBlocked);
        }
      }
      // peek rooms we don't yet have a roster for (rate-limited, background)
      if (this.version !== 'v2') this._peekRooms(rooms);
    },

    _blockedInRoster: function (names) {
      if (!this.blocked || !this.blocked.length) return null;
      var lower = this.blocked.map(function (n) { return n.toLowerCase(); });
      for (var i = 0; i < names.length; i++) {
        if (lower.indexOf(names[i].toLowerCase()) >= 0) return names[i];
      }
      return null;
    },

    // Briefly connect to each room's GraphServer to read who's inside (it sends
    // ADD_PLAYER for everyone on connect), cache the names, then disconnect.
    // Rate-limited so we don't hammer the bridge/servers.
    _peekRooms: function (rooms) {
      var self = this;
      if (this._peeking) return;
      var TTL = 20000; // re-peek a room at most every 20s
      var queue = rooms.filter(function (r) {
        var c = self._roomRosters[r.ip + ':' + r.port];
        return !c || (Date.now() - c.at > TTL);
      });
      if (!queue.length) return;
      this._peeking = true;
      var idx = 0;
      function next() {
        if (idx >= queue.length || self.current !== 'lobby') { self._peeking = false; return; }
        var r = queue[idx++];
        self._peekRoom(r, function () { setTimeout(next, 120); });
      }
      next();
    },

    _peekRoom: function (room, done) {
      var self = this;
      var key = room.ip + ':' + room.port;
      var names = [];
      var finished = false;
      var conn;
      function finish() {
        if (finished) return; finished = true;
        try { conn && conn.close(); } catch (e) {}
        self._roomRosters[key] = { names: names, at: Date.now() };
        // re-render only if this room's blocked status could change ordering
        if (self.current === 'lobby' && self._blockedInRoster(names)) self._renderRooms(self._lastRooms);
        done && done();
      }
      try {
        var opts = this._connOpts();
        conn = new GW.Connection({
          bridgeUrl: opts.bridgeUrl,
          directUrl: opts.directUrl ? opts.directUrl(room.ip, room.port) : null,
          host: room.ip, port: room.port,
          onOpen: function () {},
          onMessage: function (m) {
            var info = m.split('&');
            if (parseInt(info[0], 10) === GW.NP.ADD_PLAYER && info.length >= 3) {
              names.push(GW.urlDecode(info[2]));
            }
          },
          onClose: function () { finish(); },
          onError: function () { finish(); }
        });
        conn.open();
        // rooms blast their roster immediately on connect; give it a moment then leave
        setTimeout(finish, 900);
      } catch (e) { finish(); }
    },

    // ---------------- Room connection ----------------
    joinRoom: function (host, port, opts) {
      var self = this;
      opts = opts || {};
      this._manualRoomClose = false;
      if (!opts.reconnect) {
        this._roomRetryAttempt = 0;
        if (this._roomRetryTimer) { clearTimeout(this._roomRetryTimer); this._roomRetryTimer = null; }
        this._roomReconnectPending = false;
      }
      if (this.version === 'v2' && this._v2LobbyTimer) {
        clearInterval(this._v2LobbyTimer);
        this._v2LobbyTimer = null;
      }
      if (this.version === 'v2' && this._v2LobbyRetryTimer) {
        clearTimeout(this._v2LobbyRetryTimer);
        this._v2LobbyRetryTimer = null;
      }
      if (this.version === 'v2') {
        host = this._normalizeV2RoomHost(host);
        port = parseInt(port, 10) || DEFAULT_V2_ROOM.port;
        if (port === DEFAULT_V1_ROOM.port) port = DEFAULT_V2_ROOM.port;
        var hostEl = $('#directHost');
        var portEl = $('#directPort');
        if (hostEl) hostEl.value = host;
        if (portEl) portEl.value = port;
        var desktop = window.GW_DESKTOP || null;
        if (desktop && desktop.v2BridgePort > 0) {
          this.v2BridgeUrl = 'ws://127.0.0.1:' + desktop.v2BridgePort;
        }
        if (opts.room && !opts.reconnect && !opts.probed && desktop && desktop.app && desktop.app.ProbeGraphwar2Room) {
          this.toast('正在检测二代房间 ' + this._roomJoinLabel(host, port, opts.room) + '...', 'ok');
          desktop.app.ProbeGraphwar2Room(host, port).then(function (probe) {
            if (!probe || !probe.ok) {
              var reason = probe && probe.error ? probe.error : '房间无响应';
              self.toast(self._v2JoinFailureMessage(host, port, opts.room, reason), 'err');
              if (self._v2LobbyRefresh) setTimeout(function () { self._v2LobbyRefresh(true); }, 500);
              return;
            }
            if (probe.events && probe.events.length) {
              var app = window.GW_DESKTOP && window.GW_DESKTOP.app;
              if (app && app.AppendV2DebugLog) {
                try { app.AppendV2DebugLog('ProbeGraphwar2Room ok address=' + (probe.address || (host + ':' + port)) + ' events=' + probe.events.join(',')); } catch (e) {}
              }
            }
            self.joinRoom(host, port, Object.assign({}, opts, { probed: true }));
          }).catch(function (e) {
            self.toast(self._v2JoinFailureMessage(host, port, opts.room, String(e || '探测失败')), 'err');
            if (self._v2LobbyRefresh) setTimeout(function () { self._v2LobbyRefresh(true); }, 500);
          });
          return;
        }
        if (desktop && desktop.app && desktop.app.V2BridgePort && !desktop.v2BridgePort) {
          this.toast('正在启动 Graphwar II 桥接...', 'ok');
          desktop.app.V2BridgePort().then(function (bridgePort) {
            desktop.v2BridgePort = bridgePort || 0;
            if (!bridgePort || bridgePort <= 0) {
              self.toast('Graphwar II 桥接启动失败', 'err');
              return;
            }
            self.v2BridgeUrl = 'ws://127.0.0.1:' + bridgePort;
            localStorage.setItem('gw_v2_bridge', self.v2BridgeUrl);
            self._applyModeUi();
            self.joinRoom(host, port);
          }).catch(function (e) {
            self.toast('Graphwar II 桥接失败：' + e, 'err');
          });
          return;
        }
        if (!this.v2BridgeUrl && !this.bridgeUrl) {
          this.toast('Graphwar II 需要本地二代桥接；请使用桌面版或填写桥接地址。', 'err');
          return;
        }
      } else {
        port = parseInt(port, 10) || DEFAULT_V1_ROOM.port;
      }
      this.roomHost = host; this.roomPort = port;
      this._roomJoinMeta = opts.room || (opts.reconnect ? this._roomJoinMeta : null);
      this._configureTranslate();
      if (this.game && this.game !== null) {
        try { this.game._appSuperseded = true; this.game.disconnect(); } catch (e) {}
      }
      var game = this.version === 'v2' ? new GW.GameDataV2(this._v2ConnOpts()) : new GW.GameData(this._connOpts());
      this.game = game;

      game.on('connected', function () {
        self._roomRetryAttempt = 0;
        self.toast('已加入房间 ' + self._roomJoinLabel(host, port), 'ok');
        // Graphwar I creates the player here; Graphwar II sends
        // NewPlayerRequest(connection_id) after its version handshake.
        game.addPlayer(self.name);
        self.show('room');
        self._renderRoster();
      });
      game.on('roster', function () { self._renderRoster(); });
      game.on('player_added', function () { self._renderRoster(); });
      game.on('mode', function () { self._renderRoster(); });
      game.on('leader', function () { self.isLeader = true; self._renderRoster(); self.toast('You are the room leader'); });
      game.on('v2_status', function () {
        self._renderV2GamePanel();
      });
      game.on('chat', function (p, msg) {
        var target = game.gameState === GW.GameConstants.GAME ? $('#gameChat') : $('#roomChat');
        self._addChat(target, p ? p.name : null, msg, p);
      });
      game.on('game_started', function () { self._enterGame(); });
      game.on('next_turn', function () { self._refreshTurn(); });
      game.on('fire', function () { self._renderV2GamePanel(); /* renderer animates */ });
      game.on('game_finished', function () {
        if (self._timerInt) { clearInterval(self._timerInt); self._timerInt = null; }
        if (self.renderer) self.renderer.stop();
        self.toast('Round finished'); self.show('room'); self._renderRoster();
      });
      game.on('kicked', function (why) { self._manualRoomClose = true; self.toast(why, 'err'); self._leaveRoom(true); });
      game.on('left', function () { /* state cleared */ });
      game.on('disconnected', function (info) {
        if (game._appSuperseded) return;
        var detail = info && info.error ? info.error : '连接已断开';
        var msg = self.version === 'v2'
          ? self._v2JoinFailureMessage(host, port, self._roomJoinMeta, detail)
          : ('房间连接断开：' + detail);
        if (self._scheduleRoomReconnect(info)) return;
        self.toast(msg, 'err');
        if (self.version === 'v2' && self._v2LobbyRefresh) setTimeout(function () { self._v2LobbyRefresh(true); }, 500);
        self._leaveRoom(false);
      });
      game.on('neterror', function (i) {
        if (game._appSuperseded) return;
        var detail = i && i.error || '连接失败';
        self.toast(self.version === 'v2'
          ? self._v2JoinFailureMessage(host, port, self._roomJoinMeta, detail)
          : ('房间错误：' + detail), 'err');
        if (self.version === 'v2' && self._v2LobbyRefresh) setTimeout(function () { self._v2LobbyRefresh(true); }, 500);
        if (!self._scheduleRoomReconnect(i)) self.show(self.lobby && self.lobby.running ? 'lobby' : 'menu');
      });
      game.on('protocolerror', function (raw, err) {
        var msg = '协议错误：' + (err && err.message ? err.message : err || '未知错误');
        self.toast(msg, 'err');
        var app = window.GW_DESKTOP && window.GW_DESKTOP.app;
        if (app && app.AppendV2DebugLog) {
          try { app.AppendV2DebugLog('App protocolerror raw=' + raw + ' err=' + (err && err.stack || err)); } catch (e) {}
        }
      });
      game.on('badfunction', function () { self.toast('函数无效', 'err'); });
      game.on('preview', function (fn) {
        $('#opponentPreview').textContent = fn ? ('opponent: ' + fn) : '';
        self._renderV2GamePanel();
        // when formula rendering is on and it's the opponent's turn, show their
        // formula typeset in the render box (prefixed so it's clearly theirs).
        if (self.renderFormula && !self._canFireNow()) {
          var cur = self.game && self.game.getCurrentTurnPlayer();
          var who = (cur && !cur.local) ? (cur.name + ': ') : 'opponent: ';
          self._renderFormulaBox(fn || '', fn ? who : '');
        }
      });

      game.connect(host, port);
    },

    _leaveRoom: function (manual) {
      if (manual !== false) {
        this._manualRoomClose = true;
        if (this._roomRetryTimer) { clearTimeout(this._roomRetryTimer); this._roomRetryTimer = null; }
        this._roomReconnectPending = false;
      }
      if (this._timerInt) { clearInterval(this._timerInt); this._timerInt = null; }
      if (this._adminTimer) { clearInterval(this._adminTimer); this._adminTimer = null; }
      $('#adminModal').style.display = 'none';
      if (this.renderer) { this.renderer.stop(); this.renderer = null; }
      if (this.game) { try { this.game._appSuperseded = true; this.game.disconnect(); } catch (e) {} }
      var v2Panel = $('#v2GamePanel');
      if (v2Panel) v2Panel.style.display = 'none';
      // note: the hosted room keeps running in the backend; clear our in-room flag
      this._isHost = false;
      this.show(this.lobby && this.lobby.running ? 'lobby' : 'menu');
    },

    // ---------------- Room (pre-game) ----------------
    _bindRoom: function () {
      var self = this;
      $('#btnRoomLeave').addEventListener('click', function () { self._leaveRoom(true); });
      $('#btnReady').addEventListener('click', function () {
        var p = self.game.getFirstLocalPlayer();
        if (p && self.game.setReady(p, !p.ready) === false) self.toast('开始请求未发送：房间连接不可用', 'err');
      });
      $('#btnAddSoldier').addEventListener('click', function () {
        var p = self.game.getFirstLocalPlayer(); if (p) self.game.addSoldier(p);
      });
      $('#btnRemoveSoldier').addEventListener('click', function () {
        var p = self.game.getFirstLocalPlayer(); if (p) self.game.removeSoldier(p);
      });
      $('#btnSwitchTeam').addEventListener('click', function () {
        var p = self.game.getFirstLocalPlayer(); if (p) self.game.switchSide(p);
      });
      var btnAddComputer = $('#btnAddComputer');
      if (btnAddComputer) btnAddComputer.addEventListener('click', function () { self._addComputerPlayer(); });
      $('#btnNextMode').addEventListener('click', function () {
        if (!self.game) return;
        if (self.game.protocolVersion === 2) {
          if (self.game.nextMode) self.game.nextMode();
          return;
        }
        self.game.nextMode();
      });
      var v2Bindings = [
        ['#btnV2AxisMode', 'nextAxisMode'],
        ['#btnV2FunctionMode', 'nextMode'],
        ['#btnV2TurnMode', 'nextTurnMode'],
        ['#btnV2TimeMode', 'nextTimeMode']
      ];
      v2Bindings.forEach(function (pair) {
        var el = $(pair[0]);
        if (el) el.addEventListener('click', function () {
          if (self.game && self.game[pair[1]] && self.game[pair[1]]() === false) self.toast('设置请求未发送：房间连接不可用', 'err');
        });
      });
      $$('.v2-bot-btn').forEach(function (botBtn) {
        botBtn.addEventListener('click', function () {
          var level = parseInt(botBtn.getAttribute('data-level'), 10);
          if (self.game && self.game.addBot && self.game.addBot(level) === false) self.toast('添加机器人请求未发送：房间连接不可用', 'err');
        });
      });
      var lockBtn = $('#btnV2Lock');
      if (lockBtn) lockBtn.addEventListener('click', function () {
        if (self.game && self.game.setLocked && self.game.setLocked(!self.game.locked) === false) self.toast('锁房请求未发送：房间连接不可用', 'err');
      });
      $('#roomChatForm').addEventListener('submit', function (e) {
        e.preventDefault();
        var v = $('#roomChatInput').value.trim();
        if (v) {
          if (self.game.sendChatMessage(v) === false) self.toast('聊天发送失败：房间连接不可用', 'err');
          else $('#roomChatInput').value = '';
        }
      });
      // room management (host only)
      $('#btnRoomAdmin').addEventListener('click', function () { self._openAdmin(); });
      $('#adminClose').addEventListener('click', function () {
        $('#adminModal').style.display = 'none';
        if (self._adminTimer) { clearInterval(self._adminTimer); self._adminTimer = null; }
      });
      $('#adminLock').addEventListener('change', function () {
        var app = self._desktopApp(); if (app) app.AdminSetLocked(self._hostedPort, this.checked);
      });
      $('#adminMax').addEventListener('change', function () {
        var app = self._desktopApp(); if (app) app.AdminSetMaxClients(self._hostedPort, parseInt(this.value, 10) || 10);
      });
      $('#adminReset').addEventListener('click', function () {
        var app = self._desktopApp(); if (app) { app.AdminForceReset(self._hostedPort); self.toast('已强制重置对局'); }
      });
      $('#adminBanForm').addEventListener('submit', function (e) {
        e.preventDefault();
        var v = $('#adminBanInput').value.trim(); if (!v) return;
        var app = self._desktopApp(); if (!app) return;
        // looks like an IP? ban IP, else ban name
        var fn = /^[0-9.:a-fA-F]+$/.test(v) && /[.:]/.test(v) ? app.AdminBanIP : app.AdminBanName;
        fn(self._hostedPort, v).then(function () { $('#adminBanInput').value = ''; self._refreshAdmin(); });
      });
    },

    _openAdmin: function () {
      var self = this, app = this._desktopApp();
      if (!app || !this._hostedPort) { this.toast('仅房主可管理', 'err'); return; }
      $('#adminModal').style.display = 'flex';
      this._refreshAdmin();
      // live refresh while open
      if (this._adminTimer) clearInterval(this._adminTimer);
      this._adminTimer = setInterval(function () { self._refreshAdmin(); }, 2000);
    },

    _refreshAdmin: function () {
      var self = this, app = this._desktopApp(), port = this._hostedPort;
      if (!app || !port) return;
      app.AdminRoomStatus(port).then(function (st) {
        if (!st || !st.ok) return;
        $('#adminRoomInfo').textContent = '端口 ' + st.port + ' | ' + st.players + ' 名玩家' + (st.inGame ? ' | 游戏中' : '');
        $('#adminLock').checked = !!st.locked;
        $('#adminMax').value = st.maxClients;
      });
      app.AdminListPlayers(port).then(function (players) {
        var ul = $('#adminPlayers'); ul.innerHTML = '';
        if (!players.length) { ul.innerHTML = '<li style="color:var(--muted)">暂无玩家</li>'; return; }
        players.forEach(function (p) {
          var li = document.createElement('li');
          li.style.display = 'flex'; li.style.alignItems = 'center'; li.style.gap = '6px';
          var info = document.createElement('span'); info.style.flex = '1';
          info.textContent = p.name + (p.leader ? ' [房主]' : '') + '  ' + p.ip + '  队伍 ' + p.team;
          li.appendChild(info);
          li.appendChild(self._adminBtn('踢出', function () { app.AdminKick(port, p.id).then(function () { self._refreshAdmin(); }); }));
          li.appendChild(self._adminBtn('封禁名字', function () { app.AdminBanPlayer(port, p.id, false).then(function () { self._refreshAdmin(); }); }));
          li.appendChild(self._adminBtn('封禁 IP', function () { app.AdminBanPlayer(port, p.id, true).then(function () { self._refreshAdmin(); }); }));
          ul.appendChild(li);
        });
      });
      app.AdminBans(port).then(function (bans) {
        var ul = $('#adminBans'); ul.innerHTML = '';
        var all = (bans.names || []).map(function (n) { return { v: n, t: '名字' }; })
          .concat((bans.ips || []).map(function (i) { return { v: i, t: 'IP' }; }));
        if (!all.length) { ul.innerHTML = '<li style="color:var(--muted)">暂无封禁</li>'; return; }
        all.forEach(function (b) {
          var li = document.createElement('li');
          li.style.display = 'flex'; li.style.justifyContent = 'space-between'; li.style.alignItems = 'center';
          var s = document.createElement('span'); s.textContent = '[' + b.t + '] ' + b.v; li.appendChild(s);
          li.appendChild(self._adminBtn('解封', function () { app.AdminUnban(port, b.v).then(function () { self._refreshAdmin(); }); }));
          ul.appendChild(li);
        });
      });
    },
    _adminBtn: function (label, fn) {
      var b = document.createElement('button');
      b.className = 'btn small ghost'; b.textContent = label; b.type = 'button';
      b.addEventListener('click', function (e) { fn(e); });
      return b;
    },

    _v2SoldierBtn: function (label, title, fn) {
      var b = document.createElement('button');
      b.className = 'v2-soldier-btn';
      b.textContent = label;
      b.type = 'button';
      b.title = title;
      b.addEventListener('click', function (e) {
        e.stopPropagation();
        fn(e);
      });
      return b;
    },

    _v2RoomActionBtn: function (label, title, fn, danger) {
      var b = document.createElement('button');
      b.className = 'v2-room-action-btn' + (danger ? ' danger' : '');
      b.textContent = label;
      b.type = 'button';
      b.title = title || label;
      b.addEventListener('click', function (e) {
        e.stopPropagation();
        fn(e);
      });
      return b;
    },

    _v2RoomEditToast: function (sent, isLeader, action, allowedSelf) {
      if (!sent) {
        this.toast(action + '请求未发送：房间连接不可用或还未拿到本地玩家 ID', 'err');
        return;
      }
      if (isLeader || allowedSelf) this.toast(action + '请求已发送，等待服务端回显', 'ok');
      else this.toast(action + '请求已发送；官方服务端通常只接受房主修改，若无变化请查看二代协议日志', 'err');
    },

    _appendRoomPlayerControls: function (li, p, g, amHost) {
      if (!p || !g) return;
      var app = this;
      var isV2 = g.protocolVersion === 2;
      var isLeader = isV2 ? !!(g.canEditRoom && g.canEditRoom()) : !!g.leader;
      var canSelfEdit = !!p.local;
      var canHostEdit = isV2 ? isLeader : (isLeader || amHost);
      var canEdit = canHostEdit || canSelfEdit;
      if (!canEdit) return;

      var controls = document.createElement('span');
      controls.className = 'v2-soldier-actions';
      var target = p.name || ('#' + p.id);
      var editHint = canHostEdit
        ? (isV2 ? '房主修改会被官方服务端接受' : '房主可修改该玩家')
        : '只能修改自己';
      controls.title = editHint;
      controls.classList.toggle('limited', !canHostEdit);

      controls.appendChild(App._v2RoomActionBtn('⇄', '切换队伍：' + target + '，' + editHint, function () {
        var sent = app.game && app.game.switchSide(p);
        app._v2RoomEditToast(sent !== false, canHostEdit, '换队', canSelfEdit);
      }));
      if (isV2 && canHostEdit && !p.local) {
        controls.appendChild(App._v2RoomActionBtn('×', '踢出玩家：' + target, function () {
          var sent = app.game && app.game.removePlayer(p);
          app._v2RoomEditToast(sent !== false, canHostEdit, '踢人', false);
        }, true));
      }
      controls.appendChild(App._v2SoldierBtn('-', '减少士兵：' + target + '，' + editHint, function () {
        var sent = app.game && app.game.removeSoldier(p);
        app._v2RoomEditToast(sent !== false, canHostEdit, '减少士兵', canSelfEdit);
      }));
      controls.appendChild(App._v2SoldierBtn('+', '增加士兵：' + target + '，' + editHint, function () {
        var sent = app.game && app.game.addSoldier(p);
        app._v2RoomEditToast(sent !== false, canHostEdit, '增加士兵', canSelfEdit);
      }));
      li.appendChild(controls);
    },

    _playerSoldierCount: function (p) {
      if (!p) return 0;
      if (p.numSoldiers !== undefined && p.numSoldiers !== null) return Math.max(0, p.numSoldiers);
      return Math.max(0, p.soldiers ? p.soldiers.length : 0);
    },

    _playerMaxSoldiers: function (p, protocolVersion) {
      if (protocolVersion === 2) return 5;
      return Math.max(4, this._playerSoldierCount(p));
    },

    _canHostCurrentRoom: function () {
      return !!(this._isHost && this._hostedPort && this.roomPort === this._hostedPort && this._desktopApp());
    },

    _addComputerPlayer: function () {
      var app = this._desktopApp(), port = this._hostedPort;
      if (!app || !port || !this._canHostCurrentRoom()) {
        this.toast('只有本机房主可以添加电脑玩家', 'err');
        return;
      }
      var input = $('#computerLevelInput');
      var level = input ? parseInt(input.value, 10) : 50;
      if (!isFinite(level)) level = 50;
      if (level < 0) level = 0;
      if (level > 9001) level = 9001;
      var name = 'Computer ' + (this.game ? (this.game.players.length + 1) : '');
      var self = this;
      app.AdminAddComputerPlayer(port, name, level).then(function (res) {
        if (res && res.ok) self.toast('已添加电脑玩家', 'ok');
        else self.toast('添加电脑玩家失败：' + ((res && res.error) || '房间已满或不在等待界面'), 'err');
      }).catch(function (e) {
        self.toast('添加电脑玩家失败：' + e, 'err');
      });
    },

    // Host action on a specific player by server playerID (the real identity;
    // names are not unique). `which` = 'kick' | 'ban' (ban = name + IP, the
    // durable identity, since a kicked player can rename & rejoin). Works from
    // the roster, the in-game player list, and chat lines.
    _hostAct: function (which, p) {
      var self = this, app = this._desktopApp(), port = this._hostedPort;
      if (!app || !port) { this.toast('仅房主可操作', 'err'); return; }
      if (!p || p.id == null) { this.toast('找不到该玩家', 'err'); return; }
      if (p.local) { this.toast('不能管理自己', 'err'); return; }
      var done = function () { self.toast((which === 'kick' ? '已踢出 ' : '已封禁名字/IP ') + (p.name || ('#' + p.id)), 'ok'); };
      var fail = function () { self.toast('操作失败：玩家可能已经离开', 'err'); };
      if (which === 'kick') app.AdminKick(port, p.id).then(function (ok) { ok ? done() : fail(); });
      else app.AdminBanPlayer(port, p.id, true).then(function (ok) { ok ? done() : fail(); }); // ban name + IP
    },

    _renderRoster: function () {
      if (!this.game) return;
      var g = this.game;
      if (g.protocolVersion === 2) {
        var modeParts = [v2Meta('func', g.functionMode).label || 'Graphwar II'];
        if (g.axisMode) modeParts.push(v2Meta('axis', g.axisMode).label);
        if (g.turnMode) modeParts.push(v2Meta('turn', g.turnMode).label);
        if (g.timeMode) modeParts.push(v2Meta('time', g.timeMode).label);
        $('#roomMode').textContent = modeParts.join(' / ');
        this._renderV2RoomControls();
      } else {
        $('#roomMode').textContent = MODE_NAMES[g.gameMode] || '';
        this._renderV1RoomControls();
      }
      var t1 = $('#team1List'), t2 = $('#team2List');
      t1.innerHTML = ''; t2.innerHTML = '';
      var c1 = 0, c2 = 0;
      var amHost = !!(this._isHost && this._hostedPort && this.roomPort === this._hostedPort && this._desktopApp());
      var app = this;
      g.players.forEach(function (p) {
        var li = document.createElement('li');
        li.className = 'roster-item' + (p.ready ? ' ready' : '') + (p.local ? ' local' : '') + (p.isBot ? ' bot' : '');
        var soldierCount = app._playerSoldierCount(p);
        var maxSoldiers = app._playerMaxSoldiers(p, g.protocolVersion);
        var dot = document.createElement('span');
        dot.className = 'dot';
        dot.style.background = p.color;
        dot.style.color = p.color;
        var name = document.createElement('span');
        name.className = 'rn';
        name.textContent = p.name + (p.local ? ' (你)' : '');
        if (p.isBot) {
          var botTag = document.createElement('small');
          botTag.className = 'bot-level';
          botTag.textContent = 'AI ' + (p.botLevel != null ? p.botLevel : 0);
          name.appendChild(botTag);
        }
        var soldiers = document.createElement('span');
        soldiers.className = 'rs';
        soldiers.title = '士兵：' + soldierCount + '/' + maxSoldiers;
        for (var i = 0; i < maxSoldiers; i++) {
          var slot = document.createElement('i');
          slot.className = 'soldier-slot' + (i < soldierCount ? ' filled' : '');
          soldiers.appendChild(slot);
        }
        var count = document.createElement('span');
        count.className = 'soldier-count';
        count.textContent = soldierCount + '/' + maxSoldiers;
        soldiers.appendChild(count);
        li.appendChild(dot);
        li.appendChild(name);
        li.appendChild(soldiers);
        app._appendRoomPlayerControls(li, p, g, amHost);
        if (p.ready) {
          var badge = document.createElement('span');
          badge.className = 'badge';
          badge.textContent = 'READY';
          li.appendChild(badge);
        }
        // host-only inline kick/ban (by server playerID, not name)
        if (amHost && !p.local) {
          li.appendChild(App._adminBtn('踢出', function (e) { e.stopPropagation(); App._hostAct('kick', p); }));
          li.appendChild(App._adminBtn('封禁', function (e) { e.stopPropagation(); App._hostAct('ban', p); }));
        }
        if (p.team === GW.GameConstants.TEAM1) { t1.appendChild(li); c1++; }
        else { t2.appendChild(li); c2++; }
      });
      $('#team1Count').textContent = c1;
      $('#team2Count').textContent = c2;
      var me = g.getFirstLocalPlayer();
      if (g.protocolVersion === 2) {
        $('#btnReady').textContent = '开始';
        $('#btnReady').classList.remove('active');
        $('#btnNextMode').style.display = 'none';
      } else {
        $('#btnReady').textContent = me && me.ready ? '取消准备' : '准备';
        $('#btnReady').classList.toggle('active', !!(me && me.ready));
        $('#btnNextMode').style.display = g.leader ? '' : 'none';
      }
      $('#roomLeaderTag').style.display = g.leader ? '' : 'none';
      // show room-management button when we host the room we're in
      var canAdmin = !!(this._isHost && this._hostedPort && this.roomPort === this._hostedPort && this._desktopApp());
      $('#btnRoomAdmin').style.display = canAdmin ? '' : 'none';
    },

    _renderV1RoomControls: function () {
      var panel = $('#v2RoomControls');
      if (panel) panel.style.display = 'none';
      var pc = $('#v1ComputerControls');
      if (pc) pc.style.display = this._canHostCurrentRoom() ? '' : 'none';
      var hint = $('#modeHint');
      if (hint) hint.textContent = "所有玩家准备后开始倒计时。模式：y、y'、y''。";
    },

    _renderV2RoomControls: function () {
      var g = this.game;
      var pc = $('#v1ComputerControls');
      if (pc) pc.style.display = 'none';
      var panel = $('#v2RoomControls');
      if (panel) panel.style.display = '';
      var hint = $('#modeHint');
      var turnMeta = v2Meta('turn', g && g.turnMode);
      if (hint) hint.textContent = 'Graphwar II 二代房间：点击“开始”直接开局；' + (turnMeta.hint || '同时制下双方确认函数后统一结算弹道。');
      if (hint && g && g.protocolVersion === 2 && !g.leader) {
        hint.textContent += ' 当前不是推断房主；士兵数/换队/踢人请求会发送，但官方服务端通常只接受房主。';
      }
      function setModeCard(prefix, kind, value) {
        var meta = v2Meta(kind, value);
        var icon = $('#' + prefix + 'Icon');
        var label = $('#' + prefix + 'Label');
        var btn = icon && icon.closest ? icon.closest('button') : null;
        if (icon && meta.icon) icon.src = v2Icon(kind, value);
        if (label) label.textContent = meta.label;
        if (btn) btn.title = meta.hint || value || '';
      }
      if (g) {
        setModeCard('v2Axis', 'axis', g.axisMode || 'EveryUnit');
        setModeCard('v2Function', 'func', g.functionMode || 'NormalFunction');
        setModeCard('v2Turn', 'turn', g.turnMode || 'SimultaneousTurns');
        setModeCard('v2Time', 'time', g.timeMode || 'Timer1m');
      }
      var summary = $('#v2ModeSummary');
      if (summary && g) {
        summary.innerHTML = '';
        [
          ['坐标轴', v2Meta('axis', g.axisMode).label, v2Meta('axis', g.axisMode).hint],
          ['函数模式', v2Meta('func', g.functionMode).label, v2Meta('func', g.functionMode).hint],
          ['回合制', v2Meta('turn', g.turnMode).label, v2Meta('turn', g.turnMode).hint],
          ['时间', v2Meta('time', g.timeMode).label, v2Meta('time', g.timeMode).hint],
          ['房间锁定', g.locked ? '已锁定' : '未锁定', g.locked ? '新玩家不能加入' : '允许加入']
        ].forEach(function (row) {
          var item = document.createElement('div');
          item.className = 'v2-summary-row';
          item.title = row[2] || '';
          item.innerHTML = '<b></b><span></span>';
          item.querySelector('b').textContent = row[0];
          item.querySelector('span').textContent = row[1] || '?';
          summary.appendChild(item);
        });
      }
      var guide = $('#v2PlayGuide');
      if (guide && g) {
        var axis = v2Meta('axis', g.axisMode || 'EveryUnit');
        var func = v2Meta('func', g.functionMode || 'NormalFunction');
        var turn = v2Meta('turn', g.turnMode || 'SimultaneousTurns');
        var time = v2Meta('time', g.timeMode || 'Timer1m');
        var campaign = GW.V2CampaignGuides || [];
        var advanced = campaign.slice(16, 21).map(function (s) { return '<li>' + GW.escapeHtml(s) + '</li>'; }).join('');
        guide.innerHTML =
          '<details>' +
            '<summary>二代玩法指导</summary>' +
            '<p>' + GW.escapeHtml(axis.hint + '；' + func.hint + '；' + turn.hint + '；' + time.hint + '。') + '</p>' +
            '<p>官方房间机器人外观只有 0-5 六种；等级 0-20 是战役教学等级，不是房间机器人外观列表。</p>' +
            '<ul>' + advanced + '</ul>' +
          '</details>';
      }
      var lockBtn = $('#btnV2Lock');
      if (lockBtn && g) {
        lockBtn.textContent = g.locked ? '解锁房间' : '锁定房间';
        lockBtn.classList.toggle('active', !!g.locked);
      }
      var isLeader = !!(g && g.leader);
      ['#btnV2AxisMode', '#btnV2FunctionMode', '#btnV2TurnMode', '#btnV2TimeMode', '#btnV2Lock'].forEach(function (sel) {
        var el = $(sel);
        if (!el) return;
        el.disabled = false;
        el.title = isLeader ? (el.title || '') : '官方服务端只会接受房主的二代房间设置修改';
      });
      $$('.v2-bot-btn').forEach(function (btn) {
        btn.disabled = false;
        btn.title = isLeader ? (btn.getAttribute('data-title') || btn.title || '') : '官方服务端只会接受房主添加 Bot';
      });
      var leaderTag = $('#roomLeaderTag');
      if (leaderTag && g && g.protocolVersion === 2 && !g.leader) {
        leaderTag.title = '二代服务端未显式发送房主标记；本客户端按现有玩家顺序推断，仅用于 UI 提示。';
      }
    },

    // ---------------- Game ----------------
    _enterGame: function () {
      var self = this;
      this.show('game');
      if (!this.v2FunctionViewMode) this.v2FunctionViewMode = 'recent';
      var canvas = $('#gameCanvas');
      if (!this.renderer) this.renderer = new GW.Renderer(canvas, this.game);
      this.renderer.game = this.game;
      this.renderer._terrainDirty = true;
      this.renderer.start();
      this._refreshTurn();
      this._renderV2GamePanel();
      // turn timer display
      if (this._timerInt) clearInterval(this._timerInt);
      this._timerInt = setInterval(function () { self._tickTimer(); }, 200);
    },

    _tickTimer: function () {
      if (!this.game || this.game.gameState !== GW.GameConstants.GAME) return;
      var ms = this.game.getRemainingTime();
      $('#turnTimer').textContent = ms == null ? '不限' : (Math.ceil(ms / 1000) + 's');
      this._renderV2GamePanel();
    },

    _refreshTurn: function () {
      if (!this.game) return;
      var g = this.game;
      var cur = g.getCurrentTurnPlayer();
      if (!cur) return;
      var simultaneous = !!(g.protocolVersion === 2 && g.isSimultaneousMode && g.isSimultaneousMode());
      var localAction = simultaneous ? (g.getLocalActionPlayer && g.getLocalActionPlayer()) : cur;
      var submitted = !!(g.protocolVersion === 2 && g.hasLocalFunctionSubmitted && g.hasLocalFunctionSubmitted());
      var myTurn = !!(localAction && localAction.local && !g.drawingFunction && !submitted);
      var canFire = this._canFireNow();
      $('#turnInfo').textContent = simultaneous ? (submitted ? '已确认，等待其他玩家' : '输入函数并确认') : (cur.local ? '你的回合' : cur.name + ' 的回合');
      $('#turnInfo').style.color = (localAction && localAction.color) || cur.color;
      $('#funcInput').disabled = false;
      $('#btnFire').disabled = !canFire;
      $('#btnFire').textContent = simultaneous ? (submitted ? '已确认' : '确认函数') : '开火';
      var aimer = simultaneous ? localAction : cur;
      var aimSoldierIdx = aimer && aimer.currentTurnSoldier;
      var sndTurn = aimer && g.gameMode === GW.GameConstants.SND_ODE;
      $('#angleControl').style.display = sndTurn ? '' : 'none';
      if (sndTurn && aimer) {
        var s = aimer.soldiers[aimSoldierIdx];
        var deg = Math.round(((s && s.angle) || 0) * 180 / Math.PI);
        $('#angleSlider').value = deg;
        $('#angleValue').textContent = deg + '°';
      }
      $('#opponentPreview').textContent = '';
      if (this.renderer) this.renderer.setPreviewFunction(null);
      this.drawPoints = [];
      this.targets = [];
      this.endPoint = null;
      this._setSolveStatus(submitted ? '函数已确认，等待其他玩家' : '');
      if (myTurn) $('#funcInput').focus();
      this._renderGamePlayers();
      this._renderV2GamePanel();
    },

    _renderV2GamePanel: function () {
      var panel = $('#v2GamePanel');
      if (!panel) return;
      var g = this.game;
      var isV2 = !!(g && g.protocolVersion === 2 && g.gameState === GW.GameConstants.GAME);
      panel.style.display = isV2 ? '' : 'none';
      if (!isV2) return;
      var remote = g.remoteGameState || 'WaitingForFunctions';
      var submitted = g.getSubmittedFunctionCount ? g.getSubmittedFunctionCount() : 0;
      var alive = g.getAlivePlayerCount ? g.getAlivePlayerCount() : g.players.length;
      var active = g.getActiveV2Function ? g.getActiveV2Function() : null;
      var stateEl = $('#v2RemoteState');
      var submitEl = $('#v2SubmitState');
      var activeEl = $('#v2ActiveFunction');
      if (stateEl) stateEl.textContent = GW.v2StateLabel ? GW.v2StateLabel(remote) : remote.replace(/([a-z])([A-Z])/g, '$1 $2');
      if (submitEl) submitEl.textContent = submitted + '/' + alive + ' 已确认';
      if (activeEl) {
        if (active && active.function) activeEl.textContent = active.name + ': f(x) = ' + active.function;
        else if (g.lastEffect && g.lastEffect.type) activeEl.textContent = '效果: ' + g.lastEffect.type;
        else activeEl.textContent = g.isSimultaneousMode && g.isSimultaneousMode() ? '等待双方确认函数' : '等待当前玩家输入';
      }
      var mode = this.v2FunctionViewMode || 'recent';
      $$('.v2-view-btn').forEach(function (btn) {
        var id = btn.id || '';
        btn.classList.toggle('active',
          (mode === 'recent' && id === 'btnV2ViewRecent') ||
          (mode === 'all' && id === 'btnV2ViewAll') ||
          (mode === 'single' && id === 'btnV2ViewSingle'));
      });
      var view = $('#v2FunctionView');
      if (!view) return;
      view.innerHTML = '';
      var rows = [];
      if (mode === 'single') {
        if (active && active.function) rows.push(active);
      } else if (mode === 'all') {
        g.players.forEach(function (p) {
          var fn = g._turnFunctions && g._turnFunctions[p.id];
          if (fn) rows.push({ name: p.name, color: p.color, local: p.local, function: fn });
        });
      } else {
        rows = g.getRecentV2Functions ? g.getRecentV2Functions() : [];
      }
      if (!rows.length) {
        var empty = document.createElement('div');
        empty.className = 'v2-function-empty';
        empty.textContent = mode === 'single' ? '当前没有正在绘制的弹道' : '本轮还没有函数可查看';
        view.appendChild(empty);
        return;
      }
      rows.forEach(function (r) {
        var line = document.createElement('div');
        line.className = 'v2-function-line';
        var dot = document.createElement('span');
        dot.className = 'v2-function-dot';
        dot.style.background = r.color || 'var(--accent)';
        var text = document.createElement('span');
        text.textContent = (r.name || '玩家') + (r.local ? ' (你)' : '') + ': f(x) = ' + r.function;
        line.appendChild(dot);
        line.appendChild(text);
        view.appendChild(line);
      });
    },

    // In-game scoreboard: friend/foe split with alive-soldier counts + whose turn.
    _renderGamePlayers: function () {
      var el = $('#gamePlayers');
      if (!el || !this.game) return;
      var self = this, g = this.game;
      var myTeam = null, cur = g.getCurrentTurnPlayer();
      for (var i = 0; i < g.players.length; i++) if (g.players[i].local) { myTeam = g.players[i].team; break; }
      var amHost = !!(this._isHost && this._hostedPort && this.roomPort === this._hostedPort && this._desktopApp());
      function aliveCount(p) { var n = 0; for (var s = 0; s < p.numSoldiers; s++) if (p.soldiers[s] && p.soldiers[s].alive) n++; return n; }
      el.innerHTML = '';
      var title = document.createElement('div');
      title.className = 'gp-title'; title.textContent = '玩家 (' + g.players.length + ')';
      el.appendChild(title);
      g.players.forEach(function (p, pi) {
        var foe = (myTeam !== null && p.team !== myTeam);
        var isTurn = (cur === p);
        var alive = aliveCount(p);
        var row = document.createElement('div');
        row.className = 'gp-row ' + (foe ? 'foe' : 'ally') + (isTurn ? ' turn' : '');
        var dot = document.createElement('span');
        dot.className = 'gp-dot';
        dot.style.background = p.color;
        var name = document.createElement('span');
        name.className = 'gp-name';
        name.textContent = p.name + (p.local ? ' (你)' : '');
        var tag = document.createElement('span');
        tag.className = 'gp-tag';
        tag.textContent = foe ? '敌方' : '友方';
        var soldierPips = document.createElement('span');
        soldierPips.className = 'gp-pips';
        soldierPips.title = '士兵：' + alive + '/' + p.numSoldiers;
        for (var k = 0; k < p.numSoldiers; k++) {
          var slot = document.createElement('i');
          slot.className = 'soldier-slot' + (k < alive ? ' filled' : '');
          soldierPips.appendChild(slot);
        }
        var count = document.createElement('span');
        count.className = 'soldier-count';
        count.textContent = alive + '/' + p.numSoldiers;
        soldierPips.appendChild(count);
        row.appendChild(dot);
        row.appendChild(name);
        row.appendChild(tag);
        row.appendChild(soldierPips);
        if (amHost && !p.local) {
          row.appendChild(App._adminBtn('踢出', function () { App._hostAct('kick', p); }));
          row.appendChild(App._adminBtn('封禁', function () { App._hostAct('ban', p); }));
        }
        // hover: show each alive soldier's game coords + ring them on the field
        (function (player, playerIdx) {
          function updateTipPosition(ev) {
            var tip = self._coordTip();
            var pad = 12;
            var left = ev.clientX + pad;
            var top = ev.clientY + pad;
            tip.style.left = left + 'px';
            tip.style.top = top + 'px';
            var rect = tip.getBoundingClientRect();
            if (rect.right > window.innerWidth - 8) tip.style.left = Math.max(8, ev.clientX - rect.width - pad) + 'px';
            if (rect.bottom > window.innerHeight - 8) tip.style.top = Math.max(8, ev.clientY - rect.height - pad) + 'px';
          }
          row.addEventListener('mouseenter', function (ev) {
            var lines = [], hl = [];
            for (var s = 0; s < player.numSoldiers; s++) {
              var so = player.soldiers[s];
              if (!so || !so.alive) continue;
              // read coords in this soldier's own on-screen orientation
              var inv = player.team !== GW.GameConstants.TEAM1;
              var gp = self._gameCoordsView(so.x, so.y, inv);
              lines.push((s + 1) + ': (x=' + gp.x.toFixed(1) + ', y=' + gp.y.toFixed(1) + ')');
              hl.push({ player: playerIdx, soldier: s });
            }
            var coordTip = self._coordTip();
            coordTip.textContent = lines.length ? lines.join('\n') : '没有存活士兵';
            coordTip.style.display = lines.length ? '' : 'none';
            updateTipPosition(ev);
            if (self.renderer) self.renderer.setHighlightSoldiers(hl);
          });
          row.addEventListener('mousemove', updateTipPosition);
          row.addEventListener('mouseleave', function () {
            var coordTip = self._coordTip();
            coordTip.style.display = 'none';
            if (self.renderer) self.renderer.setHighlightSoldiers(null);
          });
        })(p, pi);
        el.appendChild(row);
      });
    },

    _coordTip: function () {
      var tip = document.getElementById('gpCoordTip');
      if (!tip) {
        tip = document.createElement('div');
        tip.id = 'gpCoordTip';
        tip.className = 'gp-coords';
        tip.style.display = 'none';
        document.body.appendChild(tip);
      }
      return tip;
    },

    _gameCoordsView: function (px, py, inverted) {
      var x = inverted ? (C.PLANE_LENGTH - px) : px;
      return {
        x: C.PLANE_GAME_LENGTH * (x - C.PLANE_LENGTH / 2) / C.PLANE_LENGTH,
        y: C.PLANE_GAME_LENGTH * (-py + C.PLANE_HEIGHT / 2) / C.PLANE_LENGTH
      };
    },

    _disabledAutoPlay: function () {
      return false;
    },

    _bindGame: function () {
      var self = this;
      $('#btnGameLeave').addEventListener('click', function () { self._leaveRoom(true); });
      // Formula-render toggle: show typed formula as math typesetting.
      var rf = $('#renderFormulaToggle');
      if (rf) rf.addEventListener('change', function () {
        self.renderFormula = rf.checked;
        var box = $('#formulaRender');
        if (box) box.style.display = self.renderFormula ? '' : 'none';
        if (self.renderFormula) {
          if (GW.MathRender && !GW.MathRender.available()) self.toast('Formula renderer is unavailable; showing plain text', 'err');
          self._renderFormulaBox($('#funcInput').value.trim());
        }
      });
      [
        ['#btnV2ViewRecent', 'recent'],
        ['#btnV2ViewAll', 'all'],
        ['#btnV2ViewSingle', 'single']
      ].forEach(function (pair) {
        var btn = $(pair[0]);
        if (btn) btn.addEventListener('click', function () {
          self.v2FunctionViewMode = pair[1];
          self._renderV2GamePanel();
        });
      });
      $('#funcForm').addEventListener('submit', function (e) {
        e.preventDefault();
        var v = $('#funcInput').value.trim();
        if (v) {
          if (self.game.sendFunction(v) === false) self.toast('函数没有发送成功；房间连接还未就绪。', 'err');
          else self._refreshTurn();
        }
      });
      $('#funcInput').addEventListener('input', function () {
        var v = $('#funcInput').value.trim();
        self._updatePreview(v);
        // only broadcast a live preview on our own turn (sendFunctionPreview
        // self-gates too, but this avoids work while spectating/pre-computing)
        if (v && self._canFireNow()) self.game.sendFunctionPreview(v);
      });
      // GeoGebra-like formula palette + live validity indicator
      if (GW.FormulaInput) {
        GW.FormulaInput.attach($('#funcInput'), {
          container: $('#formulaPalette'),
          onValidity: function (ok) {
            var el = $('#funcValidity');
            if (ok === null || $('#funcInput').value.trim() === '') { el.textContent = ''; el.className = 'validity'; }
            else { el.textContent = ok ? 'OK' : 'ERR'; el.className = 'validity ' + (ok ? 'ok' : 'err'); }
          }
        });
      }
      $('#gameChatForm').addEventListener('submit', function (e) {
        e.preventDefault();
        var v = $('#gameChatInput').value.trim();
        if (v) {
          if (self.game.sendChatMessage(v) === false) self.toast('聊天发送失败：房间连接不可用', 'err');
          else $('#gameChatInput').value = '';
        }
      });
      // angle control for SND_ODE
      var angle = $('#angleSlider');
      angle.addEventListener('input', function () {
        var rad = parseFloat(angle.value) * Math.PI / 180;
        self.game.setAngle(rad);
        $('#angleValue').textContent = angle.value + '°';
        self._updatePreview($('#funcInput').value.trim());
      });
    },

    _togglePickMode: function (mode) {
      this._setPickMode(this.pickMode === mode ? null : mode);
    },

    _setPickMode: function (mode) {
      this.pickMode = null;
      this.targetMode = false;
    },

    _updateTargetOverlay: function () {
      if (this.renderer) this.renderer.setTargets(this.targets || [], this.endPoint || null);
      var n = (this.targets && this.targets.length) || 0;
      $('#btnClearTargets').style.display = (n || this.endPoint) ? '' : 'none';
    },

    _targetLabel: function () {
      var n = (this.targets && this.targets.length) || 0;
      var end = this.endPoint ? '，已设置终点' : '';
      return n ? ('已选择 ' + n + ' 个目标' + end + (n >= 2 ? '（多目标）' : '')) : (this.endPoint ? '已设置终点' : '');
    },

    _pickTargetAt: function (pt) {
      return false;
    },

    _clearTargets: function () {
      this.targets = [];
      this.endPoint = null;
      this._updateTargetOverlay();
    },

    // Push a candidate function into the renderer's live preview layer.
    _updatePreview: function (str) {
      if (!this.renderer) return;
      var angle = 0;
      if (this.game && this.game.gameMode === GW.GameConstants.SND_ODE) {
        angle = (parseFloat($('#angleSlider').value) || 0) * Math.PI / 180;
      }
      this.renderer.setPreviewFunction(str || null, angle, null);
      if (this.renderFormula) this._renderFormulaBox(str);
    },

    // Render the current formula (mine) into the math box, if rendering is on.
    _renderFormulaBox: function (str, prefix) {
      if (!this.renderFormula) return;
      var box = $('#formulaRender');
      if (!box || !GW.MathRender) return;
      if (!str || !str.trim()) { box.innerHTML = ''; return; }
      GW.MathRender.render(box, str, { prefix: prefix || '', display: false });
    },

    _activeView: function () {
      return null;
    },
    _myNextView: function () {
      return null;
    },
    // True when firing is allowed right now.
    _canFireNow: function () {
      var g = this.game; if (!g) return false;
      if (g.protocolVersion === 2 && g.isSimultaneousMode && g.isSimultaneousMode()) {
        if (g.hasLocalFunctionSubmitted && g.hasLocalFunctionSubmitted()) return false;
        return !!(g.gameState === GW.GameConstants.GAME && g.getLocalActionPlayer && g.getLocalActionPlayer() && !g.drawingFunction);
      }
      var cur = g.getCurrentTurnPlayer();
      return !!(cur && cur.local && !g.drawingFunction);
    },

    _pickSpectateAt: function (pt) {
      return false;
    },

    _setSolveStatus: function (msg, kind) {
      var el = $('#solveStatus');
      if (!el) return;
      el.textContent = msg || '';
      el.className = 'solve-status' + (kind ? ' ' + kind : '');
    },

    _autoSolve: function () {
      return false;
    },

    _toggleDrawMode: function () {
      return false;
    },
    _clearDraw: function () {
      this.drawPoints = [];
      this.renderer && this.renderer.setPreviewFunction(null);
    },

    // Map a pointer event to field pixel coords (canvas is scaled to the field).
    _eventToField: function (ev) {
      var canvas = $('#gameCanvas');
      var r = canvas.getBoundingClientRect();
      var cs = window.getComputedStyle ? window.getComputedStyle(canvas) : null;
      var bl = cs ? (parseFloat(cs.borderLeftWidth) || 0) : 0;
      var br = cs ? (parseFloat(cs.borderRightWidth) || 0) : 0;
      var bt = cs ? (parseFloat(cs.borderTopWidth) || 0) : 0;
      var bb = cs ? (parseFloat(cs.borderBottomWidth) || 0) : 0;
      var contentW = Math.max(1, r.width - bl - br);
      var contentH = Math.max(1, r.height - bt - bb);
      var x = (ev.clientX - r.left - bl) / contentW * C.PLANE_LENGTH;
      var y = (ev.clientY - r.top - bt) / contentH * C.PLANE_HEIGHT;
      if (this.game && this.game.isTerrainReversed && this.game.isTerrainReversed()) x = C.PLANE_LENGTH - x;
      return { x: GW.clamp(x, 0, C.PLANE_LENGTH), y: GW.clamp(y, 0, C.PLANE_HEIGHT) };
    },

    _bindCanvasDrawing: function () {
      return false;
    },

    _fitDrawnPath: function () {
      return false;
    },

    _addChat: function (container, name, msg, player) {
      if (!container) return;
      name = GW.sanitizeText(name || '', 48);
      msg = GW.sanitizeText(msg || '', 600);
      var div = document.createElement('div');
      div.className = 'chat-line' + (name ? '' : ' system');
      if (name) {
        var who = document.createElement('b');
        who.textContent = name + ':';
        div.appendChild(who);
        div.appendChild(document.createTextNode(' '));
        var ct = document.createElement('span');
        ct.className = 'ct';
        ct.textContent = msg;
        div.appendChild(ct);
        if (GW.Translate && GW.Translate.enabled) {
          var btn = document.createElement('button');
          btn.className = 'tr-inline'; btn.type = 'button'; btn.title = '翻译'; btn.textContent = '译';
          btn.addEventListener('click', function () { App._translateLine(div, msg); });
          div.appendChild(btn);
        }
        // host-only: ban the sender (by server playerID) straight from chat
        var amHost = !!(this._isHost && this._hostedPort && this.roomPort === this._hostedPort && this._desktopApp());
        if (amHost && player && !player.local && player.id != null) {
          var bb = document.createElement('button');
          bb.className = 'tr-inline'; bb.type = 'button'; bb.title = '封禁该玩家'; bb.textContent = '封禁';
          bb.style.color = 'var(--danger)';
          bb.addEventListener('click', function () { App._hostAct('ban', player); });
          div.appendChild(bb);
        }
      } else {
        div.textContent = msg;
      }
      container.appendChild(div);
      while (container.children.length > 200) container.removeChild(container.firstElementChild);
      container.scrollTop = container.scrollHeight;
      if (name && this._autoIn && GW.Translate && GW.Translate.enabled) {
        this._translateLine(div, msg);
      }
    },

    // Append a translation under a chat line (idempotent per line).
    _translateLine: function (lineEl, srcText) {
      if (lineEl._trDone) return;
      lineEl._trDone = true;
      var holder = document.createElement('span');
      holder.className = 'tr-result';
      holder.textContent = ' ...';
      lineEl.appendChild(holder);
      GW.Translate.translate(srcText).then(function (tr) {
        holder.textContent = ' -> ' + tr;
      }).catch(function () {
        holder.textContent = ' (翻译失败)';
        lineEl._trDone = false;
      });
    }
  };

  GW.App = App;
  document.addEventListener('DOMContentLoaded', function () { App.init(); });
})(typeof window !== 'undefined' ? window : this);
