// Graphwar client networking — protocol codec + connections over the WS bridge.
// Mirrors NetworkProtocol.java / Connection.java semantics. All connections go
// through the Node WS<->TCP bridge (browsers can't do raw TCP).
(function (root) {
  'use strict';

  var NP = {
    NO_INFO: 10, ALL_INFO: 11, SET_NAME: 12, CHANGE_MODE: 13, CHAT_MSG: 14,
    CLOSE_CONNECTION: 15, ADD_PLAYER: 16, ADD_SOLDIER: 17, SET_SOLDIER: 18,
    REMOVE_SOLDIER: 19, SET_TEAM: 20, SET_READY: 21, START_GAME: 22,
    CHANGE_GAME_TYPE: 23, FIRE_FUNC: 24, NEXT_TURN: 25, SEND_FUNC: 26,
    READY_NEXT_TURN: 27, SET_ANGLE: 28, REMOVE_PLAYER: 29, END_GAME: 30,
    NEXT_MODE: 31, PREVIOUS_MODE: 32, SET_MODE: 33, CHANGE_ANGLE: 34,
    KILL_PLAYER: 35, CONNECTION_ACCEPTED: 36, TIME_UP: 37, GAME_FULL: 38,
    DISCONNECT: 39, GAME_FINISHED: 40, NEW_LEADER: 41, START_COUNTDOWN: 42,
    REORDER: 43, FUNCTION_PREVIEW: 44,
    JOIN: 101, SAY_CHAT: 102, LIST_PLAYERS: 103, LIST_ROOMS: 104,
    ROOM_STATUS: 105, QUIT: 106, CLOSE_ROOM: 107, CREATE_ROOM: 108,
    ROOM_INVALID: 109
  };

  // Java URLEncoder.encode(s,"UTF-8"): like encodeURIComponent but space->'+'
  // and it leaves * - _ . unescaped while escaping ! ' ( ) ~. We match that
  // exactly so chat/names/functions are byte-identical to what Java clients
  // put on the wire (* is the multiply operator and stays literal).
  function urlEncode(s) {
    return encodeURIComponent(s)
      .replace(/%20/g, '+')
      .replace(/[!'()~]/g, function (c) {
        return '%' + c.charCodeAt(0).toString(16).toUpperCase();
      });
  }
  // Java URLDecoder.decode: '+' -> space, then %xx. Guards against non-string
  // input (e.g. a malformed message with fewer fields than its count claims).
  function urlDecode(s) {
    if (typeof s !== 'string') return '';
    try { return decodeURIComponent(s.replace(/\+/g, ' ')); }
    catch (e) { return s.replace(/\+/g, ' '); }
  }

  var TIMEOUT_KEEPALIVE = 5000;
  var TIMEOUT_DROP = 45000;   // no data received in this long -> treat as dead
  var MAX_LINE_LEN = 8192;

  // A bridged line connection. `opts.host`/`opts.port` is the TCP target the
  // A connection to a Graphwar line endpoint. Two transports:
  //  - bridge mode (default): connect to the Node WS<->TCP bridge at
  //    opts.bridgeUrl, which dials opts.host/opts.port (for reaching arbitrary
  //    raw-TCP servers, incl. the official server).
  //  - direct mode (opts.directUrl): connect straight to a Graphwar server that
  //    speaks WebSocket itself (our self-hosted backend) — no bridge, no
  //    JSON handshake, ws.onopen means connected.
  function Connection(opts) {
    this.directUrl = opts.directUrl || null;
    this.bridgeUrl = opts.bridgeUrl;
    this.host = opts.host;
    this.port = opts.port;
    this.onMessage = opts.onMessage || function () {};
    this.onOpen = opts.onOpen || function () {};
    this.onClose = opts.onClose || function () {};
    this.onError = opts.onError || function () {};
    this.ws = null;
    this.connected = false;
    this._lastSent = 0;
    this._lastReceived = 0;
    this._keepAlive = null;
    this._closing = false;
    this._closed = false;
    // First raw line to send before any coded message (lobby sends the name).
    this.preamble = opts.preamble || null;
  }

  Connection.prototype.open = function () {
    var self = this;
    var url = this.directUrl || this.bridgeUrl;
    var ws = new WebSocket(url);
    this.ws = ws;
    ws.onopen = function () {
      if (self.directUrl) {
        // Direct mode: the server speaks the line protocol natively.
        self.connected = true;
        if (self.preamble !== null) self._send(self.preamble);
        self._startKeepAlive();
        self.onOpen();
      } else {
        // Bridge mode: tell the bridge where to dial.
        ws.send(JSON.stringify({ host: self.host, port: self.port }));
      }
    };
    ws.onmessage = function (ev) {
      var data = ev.data;
      if (typeof data !== 'string') return;
      if (!self.directUrl && data.charCodeAt(0) === 0) {
        // bridge control message
        var ctrl;
        try { ctrl = JSON.parse(data.slice(1)); } catch (e) { return; }
        if (ctrl.type === 'connected') {
          self._closed = false;
          self.connected = true;
          self._lastReceived = Date.now();
          if (self.preamble !== null) self._send(self.preamble);
          self._startKeepAlive();
          self.onOpen();
        } else if (ctrl.type === 'tcp_closed' || ctrl.type === 'tcp_error') {
          self._finishClose(ctrl);
        } else if (ctrl.type === 'error') {
          self.onError(ctrl);
        }
        return;
      }
      if (data.length > MAX_LINE_LEN) {
        self.onError({ type: 'protocol_error', error: 'line_too_long' });
        try { ws.close(); } catch (e) {}
        return;
      }
      self._lastReceived = Date.now();
      self.onMessage(data);
    };
    ws.onerror = function (e) { self.onError({ type: 'ws_error' }); };
    ws.onclose = function () { self._finishClose({ type: self._closing ? 'manual_close' : 'ws_close' }); };
  };

  Connection.prototype._send = function (line) {
    if (!this.ws || this.ws.readyState !== 1) return;
    this.ws.send(line);
    this._lastSent = Date.now();
  };

  // Send a protocol message (array of parts joined by '&', or a raw string).
  Connection.prototype.send = function (parts) {
    var line = Array.isArray(parts) ? parts.join('&') : String(parts);
    if (line.length > MAX_LINE_LEN) {
      this.onError({ type: 'protocol_error', error: 'send_line_too_long' });
      return;
    }
    this._send(line);
  };

  Connection.prototype.sendKeepAlive = function () { this._send('' + NP.NO_INFO); };

  Connection.prototype._startKeepAlive = function () {
    var self = this;
    this._lastSent = Date.now();
    this._lastReceived = Date.now();
    this._keepAlive = setInterval(function () {
      // Watchdog: drop a silent half-open connection (mirrors TIMEOUT_DROP in
      // ServerConnection.java). The bridge can't always surface a dead TCP peer.
      if (Date.now() - self._lastReceived > TIMEOUT_DROP) {
        self._finishClose({ type: 'timeout' });
        try { self.ws.close(); } catch (e) {}
        return;
      }
      if (Date.now() - self._lastSent > TIMEOUT_KEEPALIVE) self.sendKeepAlive();
    }, 1000);
  };
  Connection.prototype._stopKeepAlive = function () {
    if (this._keepAlive) { clearInterval(this._keepAlive); this._keepAlive = null; }
  };

  Connection.prototype._finishClose = function (info) {
    if (this._closed) return;
    this._closed = true;
    this.connected = false;
    this._stopKeepAlive();
    this.onClose(info || { type: 'closed' });
  };

  Connection.prototype.close = function () {
    this._closing = true;
    this._stopKeepAlive();
    if (this.ws) { try { this.ws.close(); } catch (e) {} }
    if (!this.ws || this.ws.readyState === 3) this._finishClose({ type: 'manual_close' });
  };

  root.GW = root.GW || {};
  root.GW.NP = NP;
  root.GW.urlEncode = urlEncode;
  root.GW.urlDecode = urlDecode;
  root.GW.Connection = Connection;
})(typeof window !== 'undefined' ? window : this);
