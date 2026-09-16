(function () {
  var SELECTOR = 'a:not(.card-action), button:not(.card-action), input, select, textarea';
  var isPlayer = !!document.querySelector('#v.video-js');
  var SLACK = 8;
  var held = {};
  var repeatAt = {};
  var padRest = {};
  var spatial = false;
  var REPEAT_FIRST = 420;
  var REPEAT_NEXT = 180;
  var STICK_DEAD = 0.55;
  var HAT_DELTA = 0.15;
  var SEEK_STEP = 10;
  var VOL_STEP = 0.1;

  function setSpatial(on) {
    spatial = !!on;
    if (document.body) document.body.classList.toggle('spatial-nav', spatial);
  }

  function scopeRoot() {
    return document.querySelector('dialog[open]') || document;
  }

  function skipped(el) {
    if (!el || el.nodeType !== 1) return true;
    if (el.disabled) return true;
    var n = el;
    while (n && n !== document) {
      if (n.inert) return true;
      if (n.hidden) return true;
      if (n.getAttribute && n.getAttribute('aria-hidden') === 'true') return true;
      n = n.parentElement;
    }
    var r = el.getBoundingClientRect();
    if (r.width < 1 || r.height < 1) return true;
    var style = window.getComputedStyle(el);
    if (!style || style.display === 'none' || style.visibility === 'hidden') return true;
    return false;
  }

  function isCandidate(el) {
    if (!el || typeof el.matches !== 'function') return false;
    if (!el.matches(SELECTOR)) return false;
    if (!scopeRoot().contains(el)) return false;
    return !skipped(el);
  }

  function candidates() {
    var nodes = scopeRoot().querySelectorAll(SELECTOR);
    var out = [];
    for (var i = 0; i < nodes.length; i++) {
      if (!skipped(nodes[i])) out.push(nodes[i]);
    }
    return out;
  }

  function focusEl(el) {
    if (!el || typeof el.focus !== 'function') return;
    el.focus();
    if (typeof el.scrollIntoView === 'function') {
      try {
        el.scrollIntoView({ block: 'nearest', inline: 'nearest' });
      } catch (e) {
        el.scrollIntoView(false);
      }
    }
  }

  function isCloseControl(el) {
    if (!el || typeof el.matches !== 'function') return false;
    if (el.id === 'add-library-close') return true;
    if ((el.getAttribute('aria-label') || '') === 'Close') return true;
    if (el.matches('.modal-head .btn-icon')) return true;
    return false;
  }

  function visualFirst(skipClose) {
    var list = candidates();
    var out = [];
    for (var i = 0; i < list.length; i++) {
      if (skipClose && isCloseControl(list[i])) continue;
      out.push(list[i]);
    }
    out.sort(function (a, b) {
      var ra = a.getBoundingClientRect();
      var rb = b.getBoundingClientRect();
      if (ra.top !== rb.top) return ra.top - rb.top;
      return ra.left - rb.left;
    });
    return out[0] || null;
  }

  function landingFocus() {
    var dlg = document.querySelector('dialog[open]');
    if (dlg) {
      var dirs = dlg.querySelectorAll('.media-browser-dir');
      for (var i = 0; i < dirs.length; i++) {
        if (!skipped(dirs[i])) return dirs[i];
      }
      return visualFirst(true);
    }
    var back = document.querySelector('.topbar-back');
    if (back && isCandidate(back)) return back;
    return visualFirst(true);
  }

  function applyLanding() {
    var el = landingFocus();
    if (el) focusEl(el);
    return el;
  }

  function ensureFocus() {
    var active = document.activeElement;
    if (isCandidate(active) && !isCloseControl(active)) return active;
    return applyLanding();
  }

  function score(from, to, dir) {
    var a = from.getBoundingClientRect();
    var b = to.getBoundingClientRect();
    var acx = a.left + a.width / 2;
    var acy = a.top + a.height / 2;
    var bcx = b.left + b.width / 2;
    var bcy = b.top + b.height / 2;
    var primary;
    var perp;
    if (dir === 'right') {
      if (bcx <= acx - SLACK) return Infinity;
      primary = b.left - a.right;
      perp = Math.abs(acy - bcy);
    } else if (dir === 'left') {
      if (bcx >= acx + SLACK) return Infinity;
      primary = a.left - b.right;
      perp = Math.abs(acy - bcy);
    } else if (dir === 'down') {
      if (bcy <= acy - SLACK) return Infinity;
      primary = b.top - a.bottom;
      perp = Math.abs(acx - bcx);
    } else if (dir === 'up') {
      if (bcy >= acy + SLACK) return Infinity;
      primary = a.top - b.bottom;
      perp = Math.abs(acx - bcx);
    } else {
      return Infinity;
    }
    if (primary < -SLACK) return Infinity;
    return Math.max(0, primary) + 2 * perp;
  }

  function moveFocus(dir) {
    var from = ensureFocus();
    if (!from) return;
    var list = candidates();
    var best = null;
    var bestScore = Infinity;
    for (var i = 0; i < list.length; i++) {
      if (list[i] === from) continue;
      var s = score(from, list[i], dir);
      if (s < bestScore) {
        bestScore = s;
        best = list[i];
      }
    }
    if (best) focusEl(best);
  }

  function typingTarget(el) {
    if (!el || !el.tagName) return false;
    var tag = el.tagName.toLowerCase();
    if (tag === 'textarea' || tag === 'select') return true;
    if (tag === 'input') {
      var type = (el.type || 'text').toLowerCase();
      return type !== 'button' && type !== 'submit' && type !== 'checkbox' && type !== 'radio' && type !== 'file';
    }
    return el.isContentEditable;
  }

  function goBack() {
    var dlg = document.querySelector('dialog[open]');
    if (dlg && typeof dlg.close === 'function') {
      dlg.close();
      return;
    }
    var back = document.querySelector('.topbar-back');
    if (back && back.href) {
      back.click();
      return;
    }
    if (history.length > 1) history.back();
  }

  function setHeld(name, down, now) {
    if (down) {
      if (!held[name]) {
        held[name] = true;
        repeatAt[name] = now + REPEAT_FIRST;
        return true;
      }
      if (now >= (repeatAt[name] || 0)) {
        repeatAt[name] = now + REPEAT_NEXT;
        return true;
      }
      return false;
    }
    held[name] = false;
    return false;
  }

  function btnPressed(btns, i) {
    return !!(btns[i] && btns[i].pressed);
  }

  function copyAxes(axes) {
    var out = [];
    for (var i = 0; i < axes.length; i++) out[i] = axes[i];
    return out;
  }

  function axisMoved(rest, axes, i) {
    if (!rest || axes[i] == null || rest[i] == null) return false;
    return axes[i] !== rest[i];
  }

  function axisNeg(rest, axes, i) {
    return axisMoved(rest, axes, i) && axes[i] < -STICK_DEAD;
  }

  function axisPos(rest, axes, i) {
    return axisMoved(rest, axes, i) && axes[i] > STICK_DEAD;
  }

  function hat9(axes, restVal) {
    var out = { up: false, down: false, left: false, right: false };
    var v = axes[9];
    if (v == null || v < -0.9 || v > 1.01) return out;
    if (restVal == null || Math.abs(v - restVal) < HAT_DELTA) return out;
    var sector = Math.round(v * 8) % 8;
    out.up = sector === 0 || sector === 1 || sector === 7;
    out.right = sector === 1 || sector === 2 || sector === 3;
    out.down = sector === 3 || sector === 4 || sector === 5;
    out.left = sector === 5 || sector === 6 || sector === 7;
    return out;
  }

  function getVjs() {
    var el = document.getElementById('v');
    if (el && el.player) return el.player;
    if (typeof videojs === 'function' && typeof videojs.getPlayer === 'function') {
      try {
        return videojs.getPlayer('v');
      } catch (e) {}
    }
    return null;
  }

  function onDir(dir) {
    if (isPlayer) {
      var p = getVjs();
      if (!p) return;
      if (dir === 'left') p.currentTime(Math.max(0, p.currentTime() - SEEK_STEP));
      else if (dir === 'right') p.currentTime(p.currentTime() + SEEK_STEP);
      else if (dir === 'up') p.volume(Math.min(1, (p.volume() || 0) + VOL_STEP));
      else if (dir === 'down') p.volume(Math.max(0, (p.volume() || 0) - VOL_STEP));
      return;
    }
    moveFocus(dir);
  }

  function onA() {
    if (isPlayer) {
      var p = getVjs();
      if (!p) return;
      if (p.paused()) p.play();
      else p.pause();
      return;
    }
    var el = document.activeElement;
    if (el && el !== document.body && el !== document.documentElement && typeof el.click === 'function') {
      el.click();
    }
  }

  function applyPad(pad, now) {
    var btns = pad.buttons || [];
    var axes = pad.axes || [];
    var idx = pad.index;
    var rest = padRest[idx];
    if (!rest) {
      padRest[idx] = copyAxes(axes);
      rest = null;
    }
    var h9 = { up: false, down: false, left: false, right: false };
    if (rest && pad.mapping !== 'standard') h9 = hat9(axes, rest[9]);
    var up = btnPressed(btns, 12) || (rest && axisNeg(rest, axes, 1)) || (rest && axisNeg(rest, axes, 7)) || h9.up;
    var down = btnPressed(btns, 13) || (rest && axisPos(rest, axes, 1)) || (rest && axisPos(rest, axes, 7)) || h9.down;
    var left = btnPressed(btns, 14) || (rest && axisNeg(rest, axes, 0)) || (rest && axisNeg(rest, axes, 6)) || h9.left;
    var right = btnPressed(btns, 15) || (rest && axisPos(rest, axes, 0)) || (rest && axisPos(rest, axes, 6)) || h9.right;
    var a = btnPressed(btns, 0);
    var b = btnPressed(btns, 1);
    if ((up || down || left || right) && !isPlayer) setSpatial(true);
    if (setHeld('up', up, now)) onDir('up');
    if (setHeld('down', down, now)) onDir('down');
    if (setHeld('left', left, now)) onDir('left');
    if (setHeld('right', right, now)) onDir('right');
    if (setHeld('a', a, now)) onA();
    if (setHeld('b', b, now)) goBack();
  }

  function pollPad(now) {
    if (!navigator.getGamepads) return;
    var pads = navigator.getGamepads();
    for (var i = 0; i < pads.length; i++) {
      if (pads[i]) applyPad(pads[i], now);
    }
  }

  function loop(ts) {
    pollPad(ts || 0);
    requestAnimationFrame(loop);
  }

  function wakePads() {
    if (!navigator.getGamepads) return;
    try {
      navigator.getGamepads();
    } catch (e) {}
  }

  function onKeyDown(e) {
    wakePads();
    if (isPlayer) return;
    if (e.altKey || e.ctrlKey || e.metaKey) return;
    var dir =
      e.key === 'ArrowUp'
        ? 'up'
        : e.key === 'ArrowDown'
          ? 'down'
          : e.key === 'ArrowLeft'
            ? 'left'
            : e.key === 'ArrowRight'
              ? 'right'
              : '';
    if (!dir) return;
    if (typingTarget(document.activeElement)) return;
    e.preventDefault();
    setSpatial(true);
    moveFocus(dir);
  }

  function onDialogToggle(e) {
    if (isPlayer || !spatial) return;
    var dlg = e.target;
    if (!dlg || dlg.tagName !== 'DIALOG') return;
    applyLanding();
  }

  function start() {
    window.addEventListener('keydown', onKeyDown);
    window.addEventListener('pointerdown', function () {
      wakePads();
      setSpatial(false);
    }, true);
    window.addEventListener('toggle', onDialogToggle, true);
    window.addEventListener('gamepadconnected', function () {
      wakePads();
    });
    window.addEventListener('gamepaddisconnected', function (e) {
      held = {};
      repeatAt = {};
      if (e && e.gamepad && e.gamepad.index != null) delete padRest[e.gamepad.index];
      else padRest = {};
    });
    requestAnimationFrame(loop);
  }

  function afterSettle() {
    if (isPlayer || !spatial) return;
    if (document.querySelector('dialog[open] .media-browser-dir')) {
      applyLanding();
      return;
    }
    ensureFocus();
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', start);
  } else {
    start();
  }
  document.addEventListener('htmx:afterSettle', afterSettle);
})();
