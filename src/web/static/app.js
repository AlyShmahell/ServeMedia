(function () {
  var PREFIX = 'servemedia:scroll:';

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
  var STATE_PREFIX = 'servemedia:state:';
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
      target: '#items-live',
      swap: 'outerHTML',
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
    postMatchChoice(itemId, function (body) {
      body.set('provider', provider);
      body.set('id', id);
    });
  };

  window.skipMatchCandidate = function (itemId) {
    postMatchChoice(itemId, function (body) {
      body.set('skip', '1');
    });
  };

  function postMatchChoice(itemId, fill) {
    var dlg = document.getElementById('match-pick-dialog');
    if (dlg && dlg.classList.contains('is-busy')) return;
    function setBusy(on) {
      if (!dlg) return;
      dlg.classList.toggle('is-busy', on);
      if (on) {
        dlg.setAttribute('aria-busy', 'true');
      } else {
        dlg.removeAttribute('aria-busy');
      }
      var cands = dlg.querySelectorAll('.cand');
      for (var i = 0; i < cands.length; i++) {
        cands[i].disabled = on;
      }
      var skip = document.getElementById('match-skip');
      if (skip) skip.disabled = on;
    }
    setBusy(true);
    var body = new URLSearchParams();
    fill(body);
    fetch('/hx/media/' + itemId + '/match', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body,
    })
      .then(function (r) {
        if (r.ok) {
          location.reload();
          return;
        }
        return r.text().then(function (t) {
          setBusy(false);
          alert(t || r.statusText);
        });
      })
      .catch(function (err) {
        setBusy(false);
        alert(err.message || String(err));
      });
  };

  function afterEntryScanPanel(root) {
    var el = root;
    if (!el || !el.classList) return;
    if (!el.classList.contains('job-progress')) {
      el = el.querySelector ? el.querySelector('.job-progress') : null;
    }
    if (!el) return;
    var mediaId = el.getAttribute('data-media-id');
    if (el.getAttribute('data-need-pick') === '1' && mediaId) {
      var fetchDlg = document.getElementById('fetch-modal');
      if (fetchDlg && fetchDlg.close) fetchDlg.close();
      if (window.openMatchModal) openMatchModal(mediaId);
      return;
    }
    if (el.getAttribute('data-scan-reload') === '1') {
      location.reload();
    }
  }

  document.addEventListener('htmx:afterSwap', function (e) {
    afterEntryScanPanel(e.detail && e.detail.elt);
  });
})();

(function () {
  function emptyLabel(el) {
    return el.getAttribute('data-empty-label') || '';
  }

  function currentText(el) {
    var t = el.querySelector('.editable-text');
    if (!t) return '';
    var label = emptyLabel(el);
    var v = (t.textContent || '').replace(/^\s+|\s+$/g, '');
    if (label && v === label) return '';
    return v;
  }

  function startEdit(el) {
    if (el.classList.contains('is-editing')) return;
    el.classList.add('is-editing');
    var multiline = el.getAttribute('data-edit-multiline') === '1';
    var input = document.createElement(multiline ? 'textarea' : 'input');
    if (!multiline) input.type = 'text';
    input.className = 'editable-input';
    input.value = currentText(el);
    var text = el.querySelector('.editable-text');
    if (text) text.style.display = 'none';
    var btn = el.querySelector('[data-role="edit"]');
    if (btn) btn.style.display = 'none';
    el.insertBefore(input, btn || null);
    var saveBtn = null;
    if (multiline) {
      saveBtn = document.createElement('button');
      saveBtn.type = 'button';
      saveBtn.className = 'field-icon field-save';
      saveBtn.textContent = 'Save';
      el.appendChild(saveBtn);
    }
    input.focus();
    if (input.select) input.select();

    var done = false;
    function restore() {
      el.classList.remove('is-editing');
      if (input.parentNode) input.remove();
      if (saveBtn && saveBtn.parentNode) saveBtn.remove();
      if (text) text.style.display = '';
      if (btn) btn.style.display = '';
    }
    function cancel() {
      if (done) return;
      done = true;
      restore();
    }
    function save() {
      if (done) return;
      done = true;
      var url = el.getAttribute('data-edit-url');
      var field = el.getAttribute('data-edit-field') || 'title';
      var val = input.value;
      if (field === 'title' && !String(val).replace(/^\s+|\s+$/g, '')) {
        restore();
        return;
      }
      var body = new URLSearchParams();
      body.set(field, val);
      fetch(url, {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: body.toString()
      }).then(function (r) {
        return r.json().then(function (j) {
          if (!r.ok) throw new Error((j && j.error) || r.statusText);
          return j;
        }, function () {
          if (!r.ok) throw new Error(r.statusText);
          return {};
        });
      }).then(function (j) {
        var next = (j && j[field] != null) ? String(j[field]) : val;
        if (text) {
          var label = emptyLabel(el);
          if (field === 'plot' && !next.replace(/^\s+|\s+$/g, '') && label) {
            text.textContent = label;
            el.classList.add('is-empty');
          } else {
            text.textContent = next;
            el.classList.remove('is-empty');
          }
        }
        restore();
      }).catch(function (err) {
        alert(err.message || String(err));
        restore();
      });
    }
    input.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') {
        e.preventDefault();
        cancel();
      }
      if (e.key === 'Enter' && !multiline) {
        e.preventDefault();
        save();
      }
    });
    if (saveBtn) {
      saveBtn.addEventListener('mousedown', function (e) { e.preventDefault(); });
      saveBtn.addEventListener('click', save);
    }
    input.addEventListener('blur', function () {
      setTimeout(function () {
        if (!done) save();
      }, 0);
    });
  }

  document.addEventListener('click', function (e) {
    var btn = e.target.closest('[data-role="edit"]');
    if (!btn) return;
    var el = btn.closest('.editable');
    if (!el) return;
    e.preventDefault();
    e.stopPropagation();
    startEdit(el);
  }, true);

  var PLACEHOLDER = '/static/placeholder.svg';
  var posterState = {
    url: '',
    pageImg: null,
    originalSrc: '',
    hasOriginal: false,
    file: null,
    removing: false,
    objectUrl: ''
  };

  function isPlaceholder(src) {
    return !src || src.indexOf('placeholder.svg') !== -1;
  }

  function posterDlg() {
    return document.getElementById('poster-edit-dialog');
  }

  function revokePosterObject() {
    if (posterState.objectUrl) {
      URL.revokeObjectURL(posterState.objectUrl);
      posterState.objectUrl = '';
    }
  }

  function resetPosterStaging() {
    revokePosterObject();
    posterState.file = null;
    posterState.removing = false;
    var file = document.getElementById('poster-edit-file');
    if (file) file.value = '';
  }

  function setPosterPreview(src) {
    var img = document.getElementById('poster-edit-img');
    if (img) img.src = src || PLACEHOLDER;
  }

  function syncPosterButtons() {
    var dirty = !!(posterState.file || posterState.removing);
    var discard = document.getElementById('poster-edit-discard');
    var save = document.getElementById('poster-edit-save');
    var remove = document.getElementById('poster-edit-remove');
    var upload = document.getElementById('poster-edit-upload');
    if (discard) discard.disabled = !dirty;
    if (save) save.disabled = !dirty;
    if (remove) remove.disabled = posterState.removing || (!posterState.hasOriginal && !posterState.file);
    if (upload) upload.disabled = false;
  }

  function closePosterModal() {
    resetPosterStaging();
    var dlg = posterDlg();
    if (dlg && dlg.close) dlg.close();
  }

  window.openPosterModal = function (wrap) {
    if (!wrap) return;
    var dlg = posterDlg();
    if (!dlg) return;
    resetPosterStaging();
    posterState.url = wrap.getAttribute('data-upload-url') || '';
    posterState.pageImg = wrap.querySelector('img.poster') || wrap.querySelector('img');
    posterState.originalSrc = posterState.pageImg ? posterState.pageImg.getAttribute('src') : '';
    posterState.hasOriginal = !isPlaceholder(posterState.originalSrc);
    setPosterPreview(posterState.hasOriginal ? posterState.originalSrc : PLACEHOLDER);
    syncPosterButtons();
    if (dlg.showModal) dlg.showModal();
  };

  document.addEventListener('click', function (e) {
    var btn = e.target.closest('[data-role="upload"]');
    if (!btn) return;
    e.preventDefault();
    e.stopPropagation();
    var wrap = btn.closest('[data-upload-url]');
    if (!wrap) return;
    openPosterModal(wrap);
  }, true);

  document.addEventListener('DOMContentLoaded', function () {
    var dlg = posterDlg();
    if (!dlg) return;
    var file = document.getElementById('poster-edit-file');
    var upload = document.getElementById('poster-edit-upload');
    var discard = document.getElementById('poster-edit-discard');
    var save = document.getElementById('poster-edit-save');
    var remove = document.getElementById('poster-edit-remove');
    var exitBtn = document.getElementById('poster-edit-exit');

    if (upload) {
      upload.addEventListener('click', function () {
        if (file) file.click();
      });
    }
    if (file) {
      file.addEventListener('change', function () {
        if (!file.files || !file.files[0]) return;
        revokePosterObject();
        posterState.file = file.files[0];
        posterState.removing = false;
        posterState.objectUrl = URL.createObjectURL(posterState.file);
        setPosterPreview(posterState.objectUrl);
        syncPosterButtons();
      });
    }
    if (discard) {
      discard.addEventListener('click', function () {
        resetPosterStaging();
        setPosterPreview(posterState.hasOriginal ? posterState.originalSrc : PLACEHOLDER);
        syncPosterButtons();
      });
    }
    if (remove) {
      remove.addEventListener('click', function () {
        resetPosterStaging();
        posterState.removing = true;
        setPosterPreview(PLACEHOLDER);
        syncPosterButtons();
      });
    }
    if (save) {
      save.addEventListener('click', function () {
        if (!posterState.url) return;
        if (posterState.removing) {
          fetch(posterState.url, { method: 'DELETE', credentials: 'same-origin' })
            .then(function (r) {
              return r.json().then(function (j) {
                if (!r.ok) throw new Error((j && j.error) || r.statusText);
                return j;
              });
            })
            .then(function (j) {
              if (posterState.pageImg) posterState.pageImg.src = (j && j.src) || PLACEHOLDER;
              closePosterModal();
            })
            .catch(function (err) {
              alert(err.message || String(err));
            });
          return;
        }
        if (!posterState.file) return;
        var fd = new FormData();
        fd.append('file', posterState.file);
        fetch(posterState.url, { method: 'POST', credentials: 'same-origin', body: fd })
          .then(function (r) {
            return r.json().then(function (j) {
              if (!r.ok) throw new Error((j && j.error) || r.statusText);
              return j;
            });
          })
          .then(function (j) {
            if (posterState.pageImg && j.src) posterState.pageImg.src = j.src;
            closePosterModal();
          })
          .catch(function (err) {
            alert(err.message || String(err));
          });
      });
    }
    if (exitBtn) exitBtn.addEventListener('click', closePosterModal);
    dlg.addEventListener('click', function (e) {
      if (e.target === dlg) closePosterModal();
    });
    dlg.addEventListener('cancel', function (e) {
      e.preventDefault();
      closePosterModal();
    });
  });
})();
