// Graphwar II networking. The browser speaks WebSocket to the local Go bridge;
// the bridge speaks the game's u16be-framed JSON TCP protocol.
(function (root) {
  'use strict';

  var MAX_EVENT_LEN = 65535;

  function V2Connection(opts) {
    this.bridgeUrl = opts.bridgeUrl;
    this.host = opts.host;
    this.port = opts.port;
    this.onEvent = opts.onEvent || function () {};
    this.onOpen = opts.onOpen || function () {};
    this.onClose = opts.onClose || function () {};
    this.onError = opts.onError || function () {};
    this.ws = null;
    this.connected = false;
    this._closing = false;
    this._closed = false;
  }

  V2Connection.prototype.open = function () {
    var self = this;
    var ws = new WebSocket(this.bridgeUrl);
    this.ws = ws;
    ws.onopen = function () {
      ws.send(JSON.stringify({ host: self.host, port: self.port }));
    };
    ws.onmessage = function (ev) {
      var data = ev.data;
      if (typeof data !== 'string') return;
      if (data.charCodeAt(0) === 0) {
        var ctrl;
        try { ctrl = JSON.parse(data.slice(1)); } catch (e) { return; }
        if (ctrl.type === 'connected') {
          self._closed = false;
          self.connected = true;
          self.onOpen(ctrl);
        } else if (ctrl.type === 'tcp_error') {
          self.onError(ctrl);
          self._finishClose(ctrl);
        } else if (ctrl.type === 'tcp_closed') {
          self._finishClose(ctrl);
        } else if (ctrl.type === 'error') {
          self.onError(ctrl);
        }
        return;
      }
      if (data.length > MAX_EVENT_LEN) {
        self.onError({ type: 'protocol_error', error: 'event_too_long' });
        try { ws.close(); } catch (e) {}
        return;
      }
      var event;
      try { event = JSON.parse(data); } catch (e) {
        self.onError({ type: 'protocol_error', error: 'bad_json' });
        return;
      }
      self.onEvent(event, data);
    };
    ws.onerror = function () { self.onError({ type: 'ws_error' }); };
    ws.onclose = function () { self._finishClose({ type: self._closing ? 'manual_close' : 'ws_close' }); };
  };

  V2Connection.prototype.sendEvent = function (variant, fields) {
    if (!this.ws || this.ws.readyState !== 1) return false;
    var event = {};
    event[variant] = fields || {};
    var text = JSON.stringify(event);
    if (text.length > MAX_EVENT_LEN) {
      this.onError({ type: 'protocol_error', error: 'send_event_too_long' });
      return false;
    }
    this.ws.send(text);
    return true;
  };

  V2Connection.prototype.close = function () {
    this._closing = true;
    if (this.ws) {
      try { this.ws.close(); } catch (e) {}
    }
    if (!this.ws || this.ws.readyState === 3) this._finishClose({ type: 'manual_close' });
  };

  V2Connection.prototype._finishClose = function (info) {
    if (this._closed) return;
    this._closed = true;
    this.connected = false;
    this.onClose(info || { type: 'closed' });
  };

  function variantOf(event) {
    if (!event || typeof event !== 'object') return null;
    var keys = Object.keys(event);
    return keys.length === 1 ? keys[0] : null;
  }

  function fieldsOf(event) {
    var v = variantOf(event);
    return v ? (event[v] || {}) : {};
  }

  root.GW = root.GW || {};
  root.GW.V2Connection = V2Connection;
  root.GW.V2 = {
    variantOf: variantOf,
    fieldsOf: fieldsOf,
    newConnectionRequest: function () {
      return { major_version: '2.050', minor_version: 'windows' };
    }
  };
})(typeof window !== 'undefined' ? window : this);
