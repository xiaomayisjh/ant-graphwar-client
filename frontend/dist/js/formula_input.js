// Graphwar formula input widget — a GeoGebra-like button palette that sits next
// to the plain <input id="funcInput"> and inserts math symbols / functions at
// the caret.  The actual value is always kept as a plain-text string compatible
// with the existing parser (GW.PolishFunction), so nothing downstream changes.
//
// Public API:  GW.FormulaInput.attach(inputEl, options) -> { setValue, getValue, focus, destroy }
//
// Styling is class-only (all classes prefixed `fx-`); colors live in CSS.
// Works in the browser (window.GW). No frameworks, no dependencies.
(function (root) {
  'use strict';

  root.GW = root.GW || {};
  var GW = root.GW;

  // Palette layout. Each entry describes one button.
  //   label : text shown on the button
  //   insert: text inserted at the caret (omit for special kinds)
  //   caret : caret offset from the END of `insert` after insertion
  //           (e.g. -1 to land just before a trailing ")")
  //   cls   : extra class on top of `fx-key` (fn / op / del)
  //   kind  : 'text' (default) | 'backspace' | 'clear'
  // Functions insert "name()" and drop the caret inside the parens (caret:-1).
  var KEYS = [
    { label: 'x', insert: 'x' },
    { label: 'x²', insert: '^2' },
    { label: '^', insert: '^', cls: 'fx-key-op' },
    { label: '(', insert: '(' },
    { label: ')', insert: ')' },

    { label: 'sin', insert: 'sin()', caret: -1, cls: 'fx-key-fn' },
    { label: 'cos', insert: 'cos()', caret: -1, cls: 'fx-key-fn' },
    { label: 'tan', insert: 'tan()', caret: -1, cls: 'fx-key-fn' },
    { label: '√', insert: 'sqrt()', caret: -1, cls: 'fx-key-fn' },
    { label: 'abs', insert: 'abs()', caret: -1, cls: 'fx-key-fn' },
    { label: 'ln', insert: 'ln()', caret: -1, cls: 'fx-key-fn' },
    { label: 'log', insert: 'log()', caret: -1, cls: 'fx-key-fn' },
    { label: 'exp', insert: 'exp()', caret: -1, cls: 'fx-key-fn' },

    { label: 'π', insert: 'pi' },
    { label: 'e', insert: 'e' },

    { label: '+', insert: '+', cls: 'fx-key-op' },
    { label: '−', insert: '-', cls: 'fx-key-op' },
    { label: '×', insert: '*', cls: 'fx-key-op' },
    { label: '÷', insert: '/', cls: 'fx-key-op' },

    { label: '⌫', kind: 'backspace', cls: 'fx-key-del' },
    { label: 'C', kind: 'clear', cls: 'fx-key-del' }
  ];

  // Insert `text` into `input` at the current selection, replacing any
  // selected range. Returns the caret position after the inserted text.
  function insertAtCaret(input, text) {
    var start = input.selectionStart;
    var end = input.selectionEnd;
    // Fall back to end-of-value if the field doesn't expose a selection
    // (shouldn't happen for <input type=text>, but stay safe).
    if (start == null || end == null) { start = end = input.value.length; }
    var v = input.value;
    input.value = v.slice(0, start) + text + v.slice(end);
    return start + text.length;
  }

  // Place the caret at `pos` and keep focus on the input.
  function setCaret(input, pos) {
    try {
      input.focus();
      input.setSelectionRange(pos, pos);
    } catch (e) { /* setSelectionRange unsupported -> ignore */ }
  }

  // Fire a synthetic 'input' event so existing listeners (live preview,
  // validity) react exactly as if the user had typed.
  function fireInput(input) {
    input.dispatchEvent(new Event('input', { bubbles: true }));
  }

  // Validate the current value via the real parser when available. Never
  // throws; returns true/false, or null when validation can't run.
  function computeValidity(value) {
    if (!GW.PolishFunction) return null;          // engine not loaded yet
    if (!value || !value.trim()) return false;    // empty isn't a function
    try { new GW.PolishFunction(value); return true; }
    catch (e) { return false; }
  }

  // Apply one palette key to the input, then sync caret + events + validity.
  function applyKey(input, key, notifyValidity) {
    if (key.kind === 'clear') {
      input.value = '';
      setCaret(input, 0);
    } else if (key.kind === 'backspace') {
      var start = input.selectionStart, end = input.selectionEnd;
      if (start == null) { start = end = input.value.length; }
      var v = input.value;
      if (start !== end) {
        // Delete the selection.
        input.value = v.slice(0, start) + v.slice(end);
        setCaret(input, start);
      } else if (start > 0) {
        // Delete one char before the caret.
        input.value = v.slice(0, start - 1) + v.slice(start);
        setCaret(input, start - 1);
      } else {
        setCaret(input, 0);
      }
    } else {
      var caret = insertAtCaret(input, key.insert);
      if (typeof key.caret === 'number') caret += key.caret;
      setCaret(input, caret);
    }
    fireInput(input);
    notifyValidity();
  }

  // Build the palette DOM into `container` and wire up the buttons.
  function buildPalette(container, input, notifyValidity) {
    var palette = document.createElement('div');
    palette.className = 'fx-palette';

    var handlers = [];
    KEYS.forEach(function (key) {
      var btn = document.createElement('button');
      btn.type = 'button';                       // never submit the form
      btn.className = 'fx-key' + (key.cls ? ' ' + key.cls : '');
      btn.textContent = key.label;
      btn.setAttribute('aria-label', key.label);
      btn.setAttribute('tabindex', '-1');        // keep keyboard flow in the input
      var onClick = function (ev) {
        ev.preventDefault();                     // keep the input's selection
        if (input.disabled) return;
        applyKey(input, key, notifyValidity);
      };
      // Use mousedown to act before the input loses focus / selection.
      btn.addEventListener('mousedown', onClick);
      handlers.push({ btn: btn, fn: onClick });
      palette.appendChild(btn);
    });

    container.appendChild(palette);
    return { palette: palette, handlers: handlers };
  }

  // attach(inputEl, options)
  //   options.container  : element to render the palette into.
  //                        Defaults to a new <div> inserted right after inputEl.
  //   options.onValidity : optional fn(isValidOrNull) called after every change.
  // Returns { setValue, getValue, focus, destroy }. Idempotent: a second call
  // on the same element returns the existing API instead of double-attaching.
  function attach(inputEl, options) {
    if (!inputEl) throw new Error('GW.FormulaInput.attach: inputEl is required');
    options = options || {};

    // Guard against double-attach; reuse the prior API if present.
    if (inputEl.__fxAttached && inputEl.__fxApi) return inputEl.__fxApi;

    // Resolve / create the palette container.
    var ownsContainer = false;
    var container = options.container;
    if (!container) {
      container = document.createElement('div');
      container.className = 'fx-container';
      if (inputEl.parentNode) {
        inputEl.parentNode.insertBefore(container, inputEl.nextSibling);
      }
      ownsContainer = true;
    }

    var onValidity = typeof options.onValidity === 'function' ? options.onValidity : null;
    function notifyValidity() {
      if (onValidity) onValidity(computeValidity(inputEl.value));
    }

    var built = buildPalette(container, inputEl, notifyValidity);

    // Reflect user typing in the validity callback too (preview already has
    // its own 'input' listener; we only add validity, and only if requested).
    var onUserInput = function () { notifyValidity(); };
    if (onValidity) inputEl.addEventListener('input', onUserInput);

    // Initial validity pass for whatever value is already there.
    notifyValidity();

    var api = {
      setValue: function (str) {
        inputEl.value = str == null ? '' : String(str);
        setCaret(inputEl, inputEl.value.length);
        fireInput(inputEl);
        notifyValidity();
      },
      getValue: function () { return inputEl.value; },
      focus: function () { setCaret(inputEl, inputEl.value.length); },
      destroy: function () {
        built.handlers.forEach(function (h) {
          h.btn.removeEventListener('mousedown', h.fn);
        });
        if (onValidity) inputEl.removeEventListener('input', onUserInput);
        if (built.palette.parentNode) {
          built.palette.parentNode.removeChild(built.palette);
        }
        // Remove the container only if we created it.
        if (ownsContainer && container.parentNode && !container.childNodes.length) {
          container.parentNode.removeChild(container);
        }
        inputEl.__fxAttached = false;
        inputEl.__fxApi = null;
      }
    };

    inputEl.__fxAttached = true;
    inputEl.__fxApi = api;
    return api;
  }

  GW.FormulaInput = { attach: attach };
})(typeof window !== 'undefined' ? window : (typeof global !== 'undefined' ? global : this));
