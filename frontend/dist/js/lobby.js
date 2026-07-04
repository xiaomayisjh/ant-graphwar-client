// Graphwar global lobby client — port of GlobalClient.java's protocol half.
// Connects (via the bridge) to the GlobalServer (default www.graphwar.com:23761),
// lists online players and rooms, and lets the user create/browse rooms.
// The actual room is a separate GraphServer TCP endpoint reached with GameData.
(function (root) {
  'use strict';
  var GW = root.GW;
  var NP = GW.NP;

  function Emitter() { this._h = {}; }
  Emitter.prototype.on = function (ev, fn) { (this._h[ev] = this._h[ev] || []).push(fn); return this; };
  Emitter.prototype.emit = function (ev) {
    var a = Array.prototype.slice.call(arguments, 1);
    (this._h[ev] || []).forEach(function (fn) { fn.apply(null, a); });
  };

  var DUMMY_NAME = '23E(S_%24%40)!Xc';

  function GlobalClient(opts) {
    Emitter.call(this);
    this.opts = opts || {};
    this.conn = null;
    this.players = [];   // {name,id}
    this.rooms = [];     // {name,id,ip,port,mode,numPlayers}
    this.localPlayer = 'Player';
    this.running = false;
    this.roomCreated = false;
    this.roomHidden = false;
    this.roomInvalid = false;
    this.roomName = 'Room';
    this.roomPort = 6112;
    this._manualStop = false;
  }
  GlobalClient.prototype = Object.create(Emitter.prototype);

  GlobalClient.prototype.join = function (host, port, playerName) {
    var self = this;
    this.localPlayer = playerName;
    this._manualStop = false;
    // GlobalServer expects the FIRST line to be just the URL-encoded name.
    this.conn = new GW.Connection({
      bridgeUrl: this.opts.bridgeUrl,
      directUrl: this.opts.directUrl ? this.opts.directUrl(host, port) : null,
      host: host, port: port,
      preamble: GW.urlEncode(playerName),
      onOpen: function () { self.running = true; self.emit('joined'); },
      onMessage: function (m) { self.handleMessage(m); },
      onClose: function (info) {
        self.running = false;
        if (self._manualStop) return;
        self.emit('disconnected', info);
      },
      onError: function (info) { self.emit('neterror', info); }
    });
    this.conn.open();
  };

  GlobalClient.prototype.handleMessage = function (message) {
    if (typeof message !== 'string' || message.length > 8192) return;
    var info = message.split('&');
    if (!info.length) return;
    var code = parseInt(info[0], 10);
    switch (code) {
      case NP.NO_INFO: break;
      case NP.JOIN:
        if (info.length === 3) {
          this.players.push({ name: GW.sanitizeText(GW.urlDecode(info[1]), 48), id: parseInt(info[2], 10) });
          this.emit('players', this.players);
        }
        break;
      case NP.SAY_CHAT:
        if (info.length === 3) {
          var id = parseInt(info[1], 10);
          var msg = GW.sanitizeText(GW.urlDecode(info[2]), 600);
          var name = this._nameFor(id);
          this.emit('chat', name, msg);
        }
        break;
      case NP.ROOM_STATUS:
        if (info.length === 4) {
          this._updateRoom(parseInt(info[1], 10), parseInt(info[2], 10), parseInt(info[3], 10));
        }
        break;
      case NP.CREATE_ROOM:
        if (info.length === 5) {
          this.rooms.push({
            name: GW.sanitizeText(GW.urlDecode(info[1]), 80), id: parseInt(info[2], 10),
            ip: GW.sanitizeText(GW.urlDecode(info[3]), 253), port: parseInt(info[4], 10),
            mode: 0, numPlayers: 0
          });
          this.emit('rooms', this.rooms);
        }
        break;
      case NP.LIST_PLAYERS:
        if (info.length >= 2) {
          this.players = [];
          var np = parseInt(info[1], 10);
          for (var i = 0; i < np && (3 + 2 * i) < info.length; i++) {
            this.players.push({ name: GW.sanitizeText(GW.urlDecode(info[2 + 2 * i]), 48), id: parseInt(info[3 + 2 * i], 10) });
          }
          this.emit('players', this.players);
        }
        break;
      case NP.LIST_ROOMS:
        if (info.length >= 2) {
          this.rooms = [];
          var nr = parseInt(info[1], 10);
          for (var j = 0; j < nr && (7 + 6 * j) < info.length; j++) {
            this.rooms.push({
              name: GW.sanitizeText(GW.urlDecode(info[2 + 6 * j]), 80), id: parseInt(info[3 + 6 * j], 10),
              ip: GW.sanitizeText(GW.urlDecode(info[4 + 6 * j]), 253), port: parseInt(info[5 + 6 * j], 10),
              mode: parseInt(info[6 + 6 * j], 10), numPlayers: parseInt(info[7 + 6 * j], 10)
            });
          }
          this.emit('rooms', this.rooms);
        }
        break;
      case NP.CLOSE_ROOM:
        if (info.length === 2) { this._removeRoom(parseInt(info[1], 10)); }
        break;
      case NP.ROOM_INVALID:
        this.roomInvalid = true;
        this.emit('room_invalid');
        break;
      case NP.QUIT:
        if (info.length === 2) { this._removePlayer(parseInt(info[1], 10)); }
        break;
    }
  };

  GlobalClient.prototype._nameFor = function (id) {
    for (var i = 0; i < this.players.length; i++) if (this.players[i].id === id) return this.players[i].name;
    return 'Anon';
  };
  GlobalClient.prototype._removePlayer = function (id) {
    this.players = this.players.filter(function (p) { return p.id !== id; });
    this.emit('players', this.players);
  };
  GlobalClient.prototype._removeRoom = function (id) {
    this.rooms = this.rooms.filter(function (r) { return r.id !== id; });
    this.emit('rooms', this.rooms);
  };
  GlobalClient.prototype._updateRoom = function (id, mode, numPlayers) {
    for (var i = 0; i < this.rooms.length; i++) {
      if (this.rooms[i].id === id) { this.rooms[i].mode = mode; this.rooms[i].numPlayers = numPlayers; break; }
    }
    this.emit('rooms', this.rooms);
  };

  GlobalClient.prototype.sendChat = function (msg) {
    if (this.conn) this.conn.send([NP.SAY_CHAT, GW.urlEncode(GW.sanitizeText(msg, 600))]);
  };
  GlobalClient.prototype.createRoom = function (name, port) {
    name = GW.sanitizeText(name, 80);
    this.roomName = name; this.roomPort = port; this.roomCreated = true;
    if (this.conn) this.conn.send([NP.CREATE_ROOM, GW.urlEncode(name), port]);
  };
  GlobalClient.prototype.closeRoom = function () {
    if (this.roomCreated) { this.roomCreated = false; this.roomHidden = false; this.roomInvalid = false; if (this.conn) this.conn.send('' + NP.CLOSE_ROOM); }
  };
  GlobalClient.prototype.hideRoom = function () {
    if (this.roomCreated) { this.roomCreated = false; this.roomHidden = true; if (this.conn) this.conn.send('' + NP.CLOSE_ROOM); }
  };
  GlobalClient.prototype.stop = function () {
    this._manualStop = true;
    if (this.roomCreated) this.closeRoom();
    if (this.conn) { this.conn.send('' + NP.QUIT); this.conn.close(); }
    this.running = false;
    this.emit('stopped');
  };

  GW.GlobalClient = GlobalClient;
  GW.DUMMY_NAME = DUMMY_NAME;
})(typeof window !== 'undefined' ? window : this);
