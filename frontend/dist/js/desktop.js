// Desktop bootstrap (Wails only). The desktop app should join the OFFICIAL
// global server by default so you play with worldwide players. WebView can't
// open raw TCP, so an embedded WS<->TCP bridge (Go side) relays to the official
// raw-TCP lobby. We default the UI to bridge-mode -> www.graphwar.com:23761.
// The local embedded backend (for LAN/offline hosting) is started on demand
// only when the user picks "直连自建后端".
(function () {
  'use strict';
  function wailsApp() {
    return (window.go && window.go.main && window.go.main.App) ? window.go.main.App : null;
  }

  window.GW_DESKTOP = { isWails: false, bridgePort: 0, v2BridgePort: 0, localStarted: false };

  function init() {
    var app = wailsApp();
    if (!app) return; // plain browser
    window.GW_DESKTOP.isWails = true;
    document.documentElement.classList.add('wails');
    window.GW_DESKTOP.app = app;
    window.GW_V2_DEBUG = localStorage.getItem('gw_v2_debug_enabled') !== '0';
    if (app.SetV2DebugEnabled) {
      app.SetV2DebugEnabled(!!window.GW_V2_DEBUG).catch(function () {});
    }
    if (app.V2DebugEnabled) {
      app.V2DebugEnabled().then(function (enabled) {
        window.GW_V2_DEBUG = !!enabled;
        try { localStorage.setItem('gw_v2_debug_enabled', enabled ? '1' : '0'); } catch (e) {}
        if (window.GW && GW.App && GW.App._syncDebugUi) GW.App._syncDebugUi();
      }).catch(function () {});
    }
    if (app.V2DebugLogPath) {
      app.V2DebugLogPath().then(function (path) {
        window.GW_DESKTOP.v2DebugLogPath = path || '';
        if (window.GW && GW.App && GW.App._syncDebugUi) GW.App._syncDebugUi();
      }).catch(function () {});
    }

    // Get the embedded bridge port, then default to OFFICIAL global lobby.
    app.BridgePort().then(function (port) {
      window.GW_DESKTOP.bridgePort = port || 0;
      if (!window.GW || !GW.App) return;
      if (port > 0) {
        // Route bridge-mode traffic through the embedded local bridge.
        var bridgeUrl = 'ws://127.0.0.1:' + port;
        GW.App.bridgeUrl = bridgeUrl;
        try { localStorage.setItem('gw_bridge', bridgeUrl); } catch (e) {}
        if (GW.App.version !== 'v2') {
          GW.App.mode = 'bridge';
          try { localStorage.setItem('gw_mode', 'bridge'); } catch (e2) {}
          var modeSel = document.getElementById('connMode');
          if (modeSel) modeSel.value = 'bridge';
          // point the lobby at the official server by default
          var gh = document.getElementById('globalHost'); if (gh) gh.value = 'www.graphwar.com';
          var gp = document.getElementById('globalPort'); if (gp) gp.value = 23761;
          var bu = document.getElementById('bridgeUrl'); if (bu) bu.value = GW.App.bridgeUrl;
          if (GW.App._applyModeUi) GW.App._applyModeUi();
          if (GW.App.toast) GW.App.toast('已连接内置桥接，进大厅即可与全球玩家联机', 'ok');
        }
      } else {
        if (GW.App.toast) GW.App.toast('桥接启动失败，可改用直连自建后端', 'err');
      }
    }).catch(function (e) { console.error('BridgePort failed', e); });

    if (app.V2BridgePort) {
      app.V2BridgePort().then(function (port) {
        window.GW_DESKTOP.v2BridgePort = port || 0;
        if (port > 0 && window.GW && GW.App) {
          GW.App.v2BridgeUrl = 'ws://127.0.0.1:' + port;
          try { localStorage.setItem('gw_v2_bridge', GW.App.v2BridgeUrl); } catch (e) {}
          if (GW.App.version === 'v2') {
            var bu = document.getElementById('bridgeUrl'); if (bu) bu.value = GW.App.v2BridgeUrl;
            if (GW.App._applyModeUi) GW.App._applyModeUi();
          }
        }
      }).catch(function (e) { console.error('V2BridgePort failed', e); });
    }

    if (app.Graphwar2DLLInfo) {
      app.Graphwar2DLLInfo().then(function (info) {
        window.GW_DESKTOP.gw2DLL = info || null;
        if (app.AppendV2DebugLog) {
          app.AppendV2DebugLog('Graphwar II DLL capability: ' + (info.capability || 'unknown') + ' path=' + (info.path || '') + ' reason=' + (info.reason || ''));
        }
      }).catch(function (e) { console.error('Graphwar2DLLInfo failed', e); });
    }

    // Hook the connection-mode selector: starting the LOCAL backend lazily when
    // the user switches to "直连自建后端", and report its (random) lobby port.
    var modeSel = document.getElementById('connMode');
    if (modeSel) {
      modeSel.addEventListener('change', function () {
        if (modeSel.value === 'direct') ensureLocalBackend(app);
      });
    }
  }

  // Start the embedded local backend on demand; fill the lobby port for direct mode.
  function ensureLocalBackend(app) {
    if (window.GW_DESKTOP.localStarted) {
      applyLocal(window.GW_DESKTOP.localLobbyPort);
      return;
    }
    app.StartBackend('').then(function (lobbyPort) {
      if (!lobbyPort || lobbyPort <= 0) {
        try { app.LastError().then(function (e) { fail(e || 'unknown'); }); } catch (e) { fail('unknown'); }
        return;
      }
      window.GW_DESKTOP.localStarted = true;
      window.GW_DESKTOP.localLobbyPort = lobbyPort;
      applyLocal(lobbyPort);
    }).catch(function (e) { fail(String(e)); });
  }
  function applyLocal(lobbyPort) {
    if (!window.GW || !GW.App) return;
    var gh = document.getElementById('globalHost'); if (gh) gh.value = '127.0.0.1';
    var gp = document.getElementById('globalPort'); if (gp) gp.value = lobbyPort;
    if (GW.App.toast) GW.App.toast('本地后端已就绪 (端口 ' + lobbyPort + ')', 'ok');
  }
  function fail(err) {
    console.error('local backend start failed:', err);
    if (window.GW && GW.App && GW.App.toast) GW.App.toast('本地后端启动失败: ' + err, 'err');
  }

  function waitForWails(tries) {
    if (wailsApp()) { init(); return; }
    if (tries <= 0) return;
    setTimeout(function () { waitForWails(tries - 1); }, 100);
  }
  document.addEventListener('DOMContentLoaded', function () { waitForWails(30); });
})();
