// Graphwar math renderer — converts the game's plain-text function syntax into
// LaTeX, then renders it with KaTeX into a target element. The function syntax
// is GeoGebra/Graphwar style (^ for power, sqrt()/abs()/sin()/ln()/e^()/pi,
// implicit multiplication, / for division), NOT LaTeX, so we parse it into an
// expression tree and emit LaTeX with proper fractions, exponents and roots.
//
// Public API:
//   GW.MathRender.funcToLatex(str)            -> LaTeX string (throws on parse error)
//   GW.MathRender.render(el, str, opts)        -> true if rendered, false on error
//   GW.MathRender.available()                  -> KaTeX present?
//
// No dependency beyond (optional) window.katex. funcToLatex itself is pure and
// is unit-tested in verify/.
(function (root) {
  'use strict';
  root.GW = root.GW || {};
  var GW = root.GW;

  // ---- tokenizer (mirrors engine.js token vocabulary, but keeps structure) ----
  // We tokenize a normalized lowercase string. Unlike the engine we DON'T expand
  // exp->e^ or - -> +- here; we keep human structure for nicer LaTeX, but accept
  // the exact same surface syntax players type.
  var FUNCS = ['sqrt', 'log', 'abs', 'sin', 'sen', 'cos', 'tan', 'tg', 'ln', 'exp'];
  function tokenize(src) {
    var s = String(src).toLowerCase().replace(/,/g, '.');
    var toks = [];
    var i = 0, n = s.length;
    while (i < n) {
      var c = s[i];
      if (c === ' ' || c === '\t') { i++; continue; }
      // number
      if ((c >= '0' && c <= '9') || (c === '.' && i + 1 < n && s[i + 1] >= '0' && s[i + 1] <= '9')) {
        var j = i + 1;
        while (j < n && ((s[j] >= '0' && s[j] <= '9') || s[j] === '.')) j++;
        toks.push({ t: 'num', v: s.slice(i, j) }); i = j; continue;
      }
      // identifier / function name / constant
      if (c >= 'a' && c <= 'z') {
        var matched = null;
        for (var f = 0; f < FUNCS.length; f++) {
          if (s.substr(i, FUNCS[f].length) === FUNCS[f]) { matched = FUNCS[f]; break; }
        }
        if (matched) { toks.push({ t: 'func', v: matched }); i += matched.length; continue; }
        if (s.substr(i, 2) === 'pi') { toks.push({ t: 'const', v: 'pi' }); i += 2; continue; }
        // y' (treated as y by the engine, but render the prime for fidelity)
        if (c === 'y' && i + 1 < n && s[i + 1] === "'") { toks.push({ t: 'var', v: "y'" }); i += 2; continue; }
        if (c === 'x' || c === 'y') { toks.push({ t: 'var', v: c }); i++; continue; }
        if (c === 'e') { toks.push({ t: 'const', v: 'e' }); i++; continue; }
        // unknown letter: emit as a literal variable so we never crash
        toks.push({ t: 'var', v: c }); i++; continue;
      }
      if (c === '(') { toks.push({ t: 'lp' }); i++; continue; }
      if (c === ')') { toks.push({ t: 'rp' }); i++; continue; }
      if (c === '+') { toks.push({ t: 'op', v: '+' }); i++; continue; }
      if (c === '-') { toks.push({ t: 'op', v: '-' }); i++; continue; }
      if (c === '*') { toks.push({ t: 'op', v: '*' }); i++; continue; }
      if (c === '/') { toks.push({ t: 'op', v: '/' }); i++; continue; }
      if (c === '^') { toks.push({ t: 'op', v: '^' }); i++; continue; }
      // anything else: skip (lenient)
      i++;
    }
    return toks;
  }

  // ---- recursive-descent parser with implicit multiplication ----
  // Grammar (precedence low->high): expr = term (('+'|'-') term)*
  //   term  = factor ( ('*'|'/'| implicit) factor )*
  //   factor = ('-'|'+') factor | power      (unary binds looser than ^)
  //   power = primary ('^' factor)?          (right-assoc; exponent may be unary)
  //   primary = num | const | var | func '(' expr ')' | '(' expr ')'
  // Parentheses are NOT kept as nodes; precedence-based wrapping re-inserts them
  // on emit so grouping is always correct and never duplicated.
  function parse(toks) {
    var pos = 0;
    function peek() { return toks[pos]; }
    function next() { return toks[pos++]; }
    function expect(t) { var k = next(); if (!k || k.t !== t) throw new Error('expected ' + t); return k; }

    function parseExpr() {
      var node = parseTerm();
      while (peek() && peek().t === 'op' && (peek().v === '+' || peek().v === '-')) {
        var op = next().v;
        var rhs = parseTerm();
        node = { k: op === '+' ? 'add' : 'sub', a: node, b: rhs };
      }
      return node;
    }
    // implicit multiplication: a factor directly followed by something that can
    // start a factor (number, var, const, func, '(') with no operator between.
    function startsFactor(tk) {
      if (!tk) return false;
      return tk.t === 'num' || tk.t === 'var' || tk.t === 'const' || tk.t === 'func' || tk.t === 'lp';
    }
    function parseTerm() {
      var node = parseFactor();
      while (true) {
        var tk = peek();
        if (tk && tk.t === 'op' && (tk.v === '*' || tk.v === '/')) {
          var op = next().v;
          var rhs = parseFactor();
          node = { k: op === '*' ? 'mul' : 'div', a: node, b: rhs };
        } else if (startsFactor(tk)) {
          var rhs2 = parseFactor();
          node = { k: 'imul', a: node, b: rhs2 };
        } else break;
      }
      return node;
    }
    function parseFactor() {
      var tk = peek();
      if (tk && tk.t === 'op' && tk.v === '-') { next(); return { k: 'neg', a: parseFactor() }; }
      if (tk && tk.t === 'op' && tk.v === '+') { next(); return parseFactor(); }
      return parsePower();
    }
    function parsePower() {
      var base = parsePrimary();
      var tk = peek();
      if (tk && tk.t === 'op' && tk.v === '^') {
        next();
        var exp = parseFactor(); // right associative, exponent may be unary (e^-x)
        return { k: 'pow', a: base, b: exp };
      }
      return base;
    }
    function parsePrimary() {
      var tk = next();
      if (!tk) throw new Error('unexpected end');
      if (tk.t === 'num') return { k: 'num', v: tk.v };
      if (tk.t === 'var') return { k: 'var', v: tk.v };
      if (tk.t === 'const') return { k: 'const', v: tk.v };
      if (tk.t === 'lp') { var e = parseExpr(); expect('rp'); return e; } // parens implicit via precedence
      if (tk.t === 'func') {
        if (peek() && peek().t === 'lp') { next(); var arg = parseExpr(); expect('rp'); return { k: 'func', name: tk.v, a: arg }; }
        var arg2 = parsePower();
        return { k: 'func', name: tk.v, a: arg2 };
      }
      throw new Error('unexpected token ' + tk.t);
    }

    var root = parseExpr();
    if (pos < toks.length) throw new Error('trailing tokens');
    return root;
  }

  // ---- LaTeX emitter ----
  var FUNC_LATEX = {
    sin: '\\sin', cos: '\\cos', tan: '\\tan', tg: '\\tan',
    sen: '\\sin', ln: '\\ln', log: '\\log'
  };
  // precedence for deciding parentheses when emitting
  function prec(node) {
    switch (node.k) {
      case 'add': case 'sub': return 1;
      case 'neg': return 2;            // unary minus binds looser than * and ^
      case 'mul': case 'div': case 'imul': return 3;
      case 'pow': return 4;
      default: return 5; // atoms, funcs
    }
  }
  function emit(node) {
    switch (node.k) {
      case 'num': return node.v;
      case 'const': return node.v === 'pi' ? '\\pi' : 'e';
      case 'var': return node.v === "y'" ? "y'" : node.v;
      case 'neg': return '-' + wrap(node.a, 3);
      case 'add': return emit(node.a) + ' + ' + emit(node.b);
      case 'sub': return emit(node.a) + ' - ' + wrap(node.b, 2);
      case 'mul': return wrap(node.a, 3) + ' \\cdot ' + wrap(node.b, 3);
      case 'imul': return emitImplicit(node.a, node.b);
      case 'div': return '\\frac{' + emit(node.a) + '}{' + emit(node.b) + '}';
      case 'pow': return emitPow(node.a, node.b);
      case 'func': return emitFunc(node);
    }
    return '';
  }
  // wrap child in () if its precedence is below `minPrec`
  function wrap(node, minPrec) {
    var s = emit(node);
    if (prec(node) < minPrec) return '\\left(' + s + '\\right)';
    return s;
  }
  function emitImplicit(a, b) {
    var la = wrap(a, 3), lb = wrap(b, 3);
    // avoid "2 3" ambiguity: if both look numeric, force \cdot
    var aNum = a.k === 'num', bNum = b.k === 'num' || b.k === 'const';
    if (aNum && bNum) return la + ' \\cdot ' + lb;
    return la + ' ' + lb;
  }
  function emitPow(base, exp) {
    // base needs parens unless it's an atom or function call; exponent never
    // needs outer parens (it's inside ^{...}).
    var b = (prec(base) >= 4 || base.k === 'func') ? emit(base) : '\\left(' + emit(base) + '\\right)';
    return b + '^{' + emit(exp) + '}';
  }
  function emitFunc(node) {
    var arg = emit(node.a);
    if (node.name === 'sqrt') return '\\sqrt{' + arg + '}';
    if (node.name === 'abs') return '\\left|' + arg + '\\right|';
    if (node.name === 'exp') return 'e^{' + arg + '}';
    var lf = FUNC_LATEX[node.name] || ('\\operatorname{' + node.name + '}');
    return lf + '\\left(' + arg + '\\right)';
  }

  function funcToLatex(str) {
    if (str == null || String(str).trim() === '') return '';
    var toks = tokenize(str);
    if (!toks.length) return '';
    var tree = parse(toks);
    return emit(tree);
  }

  function available() { return typeof root.katex !== 'undefined' && root.katex && typeof root.katex.render === 'function'; }

  // Render `str` into element `el`. Returns true on success. On parse/render
  // failure, falls back to showing the raw string and returns false. An
  // optional `prefix` (e.g. a player name) is shown as plain text before the
  // formula — kept OUT of the LaTeX so CJK/labels render correctly.
  function render(el, str, opts) {
    if (!el) return false;
    opts = opts || {};
    if (str == null || String(str).trim() === '') { el.textContent = ''; return true; }
    el.textContent = '';
    if (opts.prefix) {
      var pre = (root.document && root.document.createElement) ? root.document.createElement('span') : null;
      if (pre) { pre.className = 'formula-prefix'; pre.textContent = opts.prefix; pre.style.opacity = '0.7'; pre.style.marginRight = '6px'; el.appendChild(pre); }
    }
    var latex;
    try { latex = funcToLatex(str); }
    catch (e) { el.appendChild(textNode((opts.prefix ? '' : '') + str)); return false; }
    if (!available()) { el.appendChild(textNode(str)); return false; }
    var span = root.document.createElement('span');
    el.appendChild(span);
    try {
      root.katex.render(latex, span, { throwOnError: false, displayMode: !!opts.display, output: 'html' });
      return true;
    } catch (e) {
      span.textContent = str;
      return false;
    }
  }
  function textNode(s) { return root.document.createTextNode(s); }

  GW.MathRender = { funcToLatex: funcToLatex, render: render, available: available };
  if (typeof module !== 'undefined' && module.exports) module.exports = GW.MathRender;
})(typeof window !== 'undefined' ? window : (typeof global !== 'undefined' ? global : this));
