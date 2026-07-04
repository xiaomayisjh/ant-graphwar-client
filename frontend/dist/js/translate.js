// Chat translation — one-tap translate for incoming (others') and outgoing
// (your own) chat. Uses the YouDao "灵动翻译" service via whatever proxy is
// available (browsers/WebView can't call it directly due to CORS):
//   - Desktop (Wails): window.go.main.App.Translate(text, to)
//   - Web bridge mode: POST <bridgeHttp>/translate
//   - Web direct mode: POST <selfhost http>/translate
// Source language is auto-detected by YouDao; we only choose the TARGET.
(function (root) {
  'use strict';
  var GW = root.GW || (root.GW = {});

  // crude script detection to choose a sensible default target:
  // if text has CJK -> translate to English; else -> translate to Chinese.
  function hasCJK(s) { return /[㐀-鿿豈-﫿぀-ヿ]/.test(s); }
  function autoTarget(text) { return hasCJK(text) ? 'en' : 'zh-CHS'; }

  var Translate = {
    // configured by app.js depending on connection mode
    httpUrl: null,        // e.g. 'http://127.0.0.1:8080/translate' (bridge or selfhost)
    desktopApp: null,     // window.go.main.App when running in Wails
    enabled: false,
    autoIncoming: false,  // auto-translate others' messages
    autoOutgoing: false,  // auto-translate your own messages before/after sending

    configure: function (opts) {
      opts = opts || {};
      if (opts.httpUrl !== undefined) this.httpUrl = opts.httpUrl;
      if (opts.desktopApp !== undefined) this.desktopApp = opts.desktopApp;
      this.enabled = !!(this.httpUrl || this.desktopApp);
      return this.enabled;
    },

    autoTarget: autoTarget,

    // Translate `text`; if `to` omitted, auto-pick (CJK->en else ->zh).
    // Returns a Promise<string>. Rejects on failure.
    translate: function (text, to) {
      to = to || autoTarget(text);
      var self = this;
      if (this.desktopApp && this.desktopApp.Translate) {
        return this.desktopApp.Translate(text, to).then(function (r) {
          if (typeof r === 'string' && r.indexOf('ERR:') === 0) throw new Error(r.slice(4));
          return r;
        });
      }
      if (this.httpUrl) {
        return fetch(this.httpUrl, {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ text: text, to: to })
        }).then(function (resp) { return resp.json(); }).then(function (j) {
          if (j.error) throw new Error(j.error);
          return j.tr;
        });
      }
      return Promise.reject(new Error('translation not available'));
    }
  };

  GW.Translate = Translate;
})(typeof window !== 'undefined' ? window : this);
