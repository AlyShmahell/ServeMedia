(function () {
  var PREFIX = 'medora:scroll:';

  function storageKey(el) {
    var id = el.getAttribute('data-scroll-key');
    if (!id) return '';
    if (id.indexOf('nav:') === 0) {
      return PREFIX + id;
    }
    return PREFIX + location.pathname + ':' + id;
  }

  function save(el) {
    var key = storageKey(el);
    if (!key) return;
    try {
      sessionStorage.setItem(key, String(el.scrollTop));
    } catch (e) {}
  }

  function restore(el) {
    var key = storageKey(el);
    if (!key) return;
    var raw;
    try {
      raw = sessionStorage.getItem(key);
    } catch (e) {
      return;
    }
    if (raw == null || raw === '') return;
    var top = parseInt(raw, 10);
    if (!isFinite(top) || top < 0) return;
    el.scrollTop = top;
  }

  function bind(el) {
    if (el.dataset.scrollBound === '1') return;
    el.dataset.scrollBound = '1';
    var ticking = false;
    el.addEventListener(
      'scroll',
      function () {
        if (ticking) return;
        ticking = true;
        requestAnimationFrame(function () {
          ticking = false;
          save(el);
        });
      },
      { passive: true }
    );
  }

  function scan() {
    var nodes = document.querySelectorAll('[data-scroll-key]');
    for (var i = 0; i < nodes.length; i++) {
      bind(nodes[i]);
      restore(nodes[i]);
    }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', scan);
  } else {
    scan();
  }
  document.addEventListener('htmx:afterSettle', scan);
})();

(function () {
  var STATE_PREFIX = 'medora:state:';
  var restored = {};

  function stateKey(form) {
    var id = form.getAttribute('data-page-state');
    if (!id) return '';
    return STATE_PREFIX + location.pathname + ':' + id;
  }

  function fieldValue(form, name) {
    var el = form.querySelector('[name="' + name + '"]');
    return el ? el.value : '';
  }

  function saveState(form) {
    var key = stateKey(form);
    if (!key) return;
    var data = {
      q: fieldValue(form, 'q'),
      sort: fieldValue(form, 'sort'),
    };
    try {
      sessionStorage.setItem(key, JSON.stringify(data));
    } catch (e) {}
  }

  function syncClearBtn(form) {
    var q = form.querySelector('[name="q"]');
    var btn = form.querySelector('.library-search-clear');
    if (!btn || !q) return;
    btn.hidden = !q.value;
  }

  function applyState(form, data) {
    var changed = false;
    var q = form.querySelector('[name="q"]');
    var sort = form.querySelector('[name="sort"]');
    if (q && data.q != null && q.value !== String(data.q)) {
      q.value = String(data.q);
      changed = true;
    }
    if (sort && data.sort != null && sort.value !== String(data.sort)) {
      sort.value = String(data.sort);
      changed = true;
    }
    syncClearBtn(form);
    return changed;
  }

  function refetchItems(form) {
    if (typeof htmx === 'undefined') return;
    var url = form.getAttribute('hx-get');
    if (!url) return;
    var params = new URLSearchParams();
    params.set('q', fieldValue(form, 'q'));
    params.set('sort', fieldValue(form, 'sort'));
    var sep = url.indexOf('?') >= 0 ? '&' : '?';
    htmx.ajax('GET', url + sep + params.toString(), {
      target: '#items',
      swap: 'innerHTML',
    });
  }

  function bindForm(form) {
    if (form.dataset.pageStateBound === '1') return;
    form.dataset.pageStateBound = '1';

    form.addEventListener('input', function () {
      saveState(form);
      syncClearBtn(form);
    });
    form.addEventListener('change', function () {
      saveState(form);
      syncClearBtn(form);
    });

    var clearBtn = form.querySelector('.library-search-clear');
    if (clearBtn) {
      clearBtn.addEventListener('click', function (e) {
        e.preventDefault();
        var q = form.querySelector('[name="q"]');
        if (q) q.value = '';
        saveState(form);
        syncClearBtn(form);
        if (typeof htmx !== 'undefined') {
          htmx.trigger(form, 'change');
        }
      });
    }
  }

  function restoreOnce(form) {
    var key = stateKey(form);
    if (!key || restored[key]) return;
    restored[key] = true;

    var raw;
    try {
      raw = sessionStorage.getItem(key);
    } catch (e) {
      syncClearBtn(form);
      return;
    }
    if (!raw) {
      syncClearBtn(form);
      return;
    }

    var data;
    try {
      data = JSON.parse(raw);
    } catch (e) {
      syncClearBtn(form);
      return;
    }

    var changed = applyState(form, data);
    if (changed) {
      refetchItems(form);
    } else {
      syncClearBtn(form);
    }
  }

  function scanPageState() {
    var forms = document.querySelectorAll('[data-page-state]');
    for (var i = 0; i < forms.length; i++) {
      bindForm(forms[i]);
      restoreOnce(forms[i]);
    }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', scanPageState);
  } else {
    scanPageState();
  }
  document.addEventListener('htmx:afterSettle', scanPageState);
})();

(function () {
  function matchHost() {
    var host = document.getElementById('match-modal-host');
    if (!host) {
      host = document.createElement('div');
      host.id = 'match-modal-host';
      document.body.appendChild(host);
    }
    return host;
  }

  window.openMatchModal = function (id) {
    fetch('/hx/media/' + id + '/match', { credentials: 'same-origin' })
      .then(function (r) {
        return r.text().then(function (html) {
          if (!r.ok) {
            throw new Error(html || r.statusText);
          }
          return html;
        });
      })
      .then(function (html) {
        var host = matchHost();
        host.innerHTML = html;
        var dlg = document.getElementById('match-pick-dialog');
        if (dlg && dlg.showModal) {
          dlg.showModal();
        }
      })
      .catch(function (err) {
        alert(err.message || String(err));
      });
  };

  window.pickMatchCandidate = function (itemId, provider, id) {
    var body = new URLSearchParams();
    body.set('provider', provider);
    body.set('id', id);
    fetch('/hx/media/' + itemId + '/match', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body,
    }).then(function (r) {
      if (r.ok) {
        location.reload();
        return;
      }
      return r.text().then(function (t) {
        alert(t || r.statusText);
      });
    });
  };
})();
