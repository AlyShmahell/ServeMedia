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

  function stopFinishedPolls() {
    var flags = document.querySelectorAll('.library-card-status [data-poll="0"]');
    for (var i = 0; i < flags.length; i++) {
      var status = flags[i].closest('.library-card-status');
      if (!status) continue;
      status.setAttribute('data-poll', '0');
      status.removeAttribute('hx-get');
      status.removeAttribute('hx-trigger');
    }
  }

  function posterSrc(img) {
    return img.getAttribute('src') || '';
  }

  function patchCollage(collage, wanted) {
    var imgs = collage.querySelectorAll('img');
    var same = imgs.length === wanted.length;
    for (var i = 0; same && i < imgs.length; i++) {
      if (posterSrc(imgs[i]) !== wanted[i]) same = false;
    }
    if (same) return;
    var pool = {};
    for (var k = 0; k < imgs.length; k++) {
      var src = posterSrc(imgs[k]);
      if (src && !pool[src]) pool[src] = imgs[k];
    }
    while (collage.firstChild) collage.removeChild(collage.firstChild);
    if (wanted.length === 0) {
      var empty = document.createElement('div');
      empty.className = 'library-collage-empty muted';
      empty.textContent = 'No posters yet';
      collage.appendChild(empty);
      return;
    }
    for (var n = 0; n < wanted.length; n++) {
      var next = wanted[n];
      var img = pool[next];
      if (img) delete pool[next];
      else {
        img = document.createElement('img');
        img.alt = '';
        img.setAttribute('src', next);
      }
      collage.appendChild(img);
    }
  }

  function syncCollages() {
    var markers = document.querySelectorAll('.library-collage-next');
    for (var i = 0; i < markers.length; i++) {
      var card = markers[i].closest('article');
      if (!card) continue;
      var collage = card.querySelector('.library-collage');
      if (!collage) continue;
      var wanted = [];
      var nodes = markers[i].querySelectorAll('[data-poster]');
      for (var j = 0; j < nodes.length; j++) {
        var src = nodes[j].getAttribute('data-poster') || '';
        if (src) wanted.push(src);
      }
      patchCollage(collage, wanted);
    }
  }

  function scan() {
    stopFinishedPolls();
    syncCollages();
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
    var path = fieldValue(form, 'path');
    if (path) params.set('path', path);
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

  document.addEventListener('click', function (e) {
    var bang = e.target.closest('.match-bang');
    if (!bang) return;
    e.preventDefault();
    e.stopPropagation();
    var id = bang.getAttribute('data-media-id');
    if (id && window.openMatchModal) openMatchModal(id);
  }, true);

  function showMatchDialogError(msg) {
    var el = document.getElementById('match-pick-error');
    if (!el) return false;
    el.textContent = msg || '';
    if (msg) {
      el.removeAttribute('hidden');
    } else {
      el.setAttribute('hidden', '');
    }
    return true;
  }

  function ensureMatchErrorDialog(msg) {
    var host = matchHost();
    if (!host) {
      console.error(msg);
      return;
    }
    host.innerHTML =
      '<dialog id="match-pick-dialog" class="modal match-dialog" open>' +
      '<form method="dialog" class="modal-form match-dialog-form">' +
      '<header class="modal-head"><h2>Choose match</h2>' +
      '<button type="button" class="btn-icon secondary" onclick="document.getElementById(\'match-pick-dialog\').close()" aria-label="Close">×</button></header>' +
      '<p id="match-pick-error" class="error"></p>' +
      '<footer class="modal-foot"><button type="button" class="secondary" onclick="document.getElementById(\'match-pick-dialog\').close()">Close</button></footer>' +
      '</form></dialog>';
    var dlg = document.getElementById('match-pick-dialog');
    showMatchDialogError(msg);
    if (dlg && dlg.showModal) {
      try {
        dlg.showModal();
      } catch (e) {}
    }
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
        ensureMatchErrorDialog(err.message || String(err));
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
    showMatchDialogError('');
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
          var dlg = document.getElementById('match-pick-dialog');
          if (dlg && dlg.close) dlg.close();
          var live = document.getElementById('items-live');
          if (live && window.htmx) {
            var url = live.getAttribute('hx-get');
            if (url) {
              htmx.ajax('GET', url, {target: live, swap: 'outerHTML', source: document.querySelector('.library-toolbar') || live});
              return;
            }
          }
          location.reload();
          return;
        }
        return r.text().then(function (t) {
          setBusy(false);
          if (!showMatchDialogError(t || r.statusText)) {
            console.error(t || r.statusText);
          }
        });
      })
      .catch(function (err) {
        setBusy(false);
        var msg = err.message || String(err);
        if (!showMatchDialogError(msg)) {
          console.error(msg);
        }
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

  function fitMiddle(el) {
    var full = el.getAttribute('data-mid') || '';
    el.textContent = full;
    if (!full || el.clientWidth <= 0 || el.scrollWidth <= el.clientWidth + 1) return;
    var ell = '\u2026';
    var lo = 1;
    var hi = full.length;
    var best = ell;
    while (lo <= hi) {
      var n = (lo + hi) >> 1;
      var left = Math.ceil(n / 2);
      var right = n - left;
      var text = full.slice(0, left) + ell + (right ? full.slice(full.length - right) : '');
      el.textContent = text;
      if (el.scrollWidth <= el.clientWidth + 1) {
        best = text;
        lo = n + 1;
      } else {
        hi = n - 1;
      }
    }
    el.textContent = best;
  }

  function fitMiddleTitles(root) {
    var sel = '.scan-title';
    var scope = root && root.querySelectorAll ? root : document;
    if (scope.matches && scope.matches(sel)) fitMiddle(scope);
    var nodes = scope.querySelectorAll(sel);
    for (var i = 0; i < nodes.length; i++) fitMiddle(nodes[i]);
  }

  document.addEventListener('htmx:afterSwap', function (e) {
    afterEntryScanPanel(e.detail && e.detail.elt);
    fitMiddleTitles(e.detail && e.detail.elt);
  });
  document.addEventListener('DOMContentLoaded', function () { fitMiddleTitles(document); });
  window.addEventListener('resize', function () { fitMiddleTitles(document); });
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
    if (!el) {
      var id = btn.getAttribute('data-edit-target');
      if (id) el = document.getElementById(id);
    }
    if (!el) return;
    e.preventDefault();
    e.stopPropagation();
    startEdit(el);
  }, true);

  var PLACEHOLDER = '/static/placeholder.svg';
  var posterState = {
    url: '',
    generateUrl: '',
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

  window.openPosterModal = function (wrap, role) {
    if (!wrap) return;
    var dlg = posterDlg();
    if (!dlg) return;
    resetPosterStaging();
    var isStill = role === 'still';
    posterState.url = wrap.getAttribute('data-upload-url') || '';
    posterState.generateUrl = isStill ? (wrap.getAttribute('data-generate-url') || '') : '';
    posterState.pageImg = wrap.querySelector('img.poster') || wrap.querySelector('img');
    posterState.originalSrc = posterState.pageImg ? posterState.pageImg.getAttribute('src') : '';
    posterState.hasOriginal = !isPlaceholder(posterState.originalSrc);
    var title = document.getElementById('poster-edit-title');
    if (title) title.textContent = isStill ? 'Still' : 'Poster';
    var gen = document.getElementById('poster-edit-generate');
    if (gen) gen.hidden = !posterState.generateUrl;
    setPosterPreview(posterState.hasOriginal ? posterState.originalSrc : PLACEHOLDER);
    syncPosterButtons();
    if (dlg.showModal) dlg.showModal();
  };

  document.addEventListener('click', function (e) {
    var btn = e.target.closest('[data-role="upload"], [data-role="still"]');
    if (!btn) return;
    e.preventDefault();
    e.stopPropagation();
    var wrap = btn.closest('[data-upload-url]');
    if (!wrap) return;
    openPosterModal(wrap, btn.getAttribute('data-role'));
  }, true);

  document.addEventListener('DOMContentLoaded', function () {
    var dlg = posterDlg();
    if (!dlg) return;
    var file = document.getElementById('poster-edit-file');
    var upload = document.getElementById('poster-edit-upload');
    var generate = document.getElementById('poster-edit-generate');
    var discard = document.getElementById('poster-edit-discard');
    var save = document.getElementById('poster-edit-save');
    var remove = document.getElementById('poster-edit-remove');
    var exitBtn = document.getElementById('poster-edit-exit');

    if (upload) {
      upload.addEventListener('click', function () {
        if (file) file.click();
      });
    }
    if (generate) {
      generate.addEventListener('click', function () {
        if (!posterState.generateUrl || generate.disabled) return;
        generate.disabled = true;
        fetch(posterState.generateUrl, { method: 'POST', credentials: 'same-origin' })
          .then(function (r) {
            if (!r.ok) {
              return r.text().then(function (t) {
                throw new Error(t || r.statusText);
              });
            }
            return r.blob();
          })
          .then(function (blob) {
            revokePosterObject();
            posterState.file = new File([blob], 'still.jpg', { type: blob.type || 'image/jpeg' });
            posterState.removing = false;
            posterState.objectUrl = URL.createObjectURL(posterState.file);
            setPosterPreview(posterState.objectUrl);
            syncPosterButtons();
          })
          .catch(function (err) {
            alert(err.message || String(err));
          })
          .then(function () {
            generate.disabled = false;
          });
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

(function () {
  var restoreFocus = null;

  function isField(el) {
    if (!el || !el.tagName) return false;
    var tag = el.tagName.toLowerCase();
    return tag === 'input' || tag === 'textarea' || tag === 'select';
  }

  function selectionIn(target) {
    var sel = window.getSelection && window.getSelection();
    if (!sel || sel.isCollapsed || sel.rangeCount < 1) return false;
    var node = sel.anchorNode;
    if (node && node.nodeType === 3) node = node.parentElement;
    return !!(target && node && target.contains && target.contains(node));
  }

  function openDialog() {
    return document.querySelector('dialog[open]');
  }

  function focusKey(el) {
    if (!el || !el.getAttribute) return '';
    if (el.id) return '#' + el.id;
    var href = el.getAttribute('href');
    if (href) return 'a[href="' + href.replace(/"/g, '') + '"]';
    var mid = el.getAttribute('data-media-id');
    if (mid) return '[data-media-id="' + mid + '"]';
    var sk = el.getAttribute('data-scroll-key');
    if (sk) return '[data-scroll-key="' + sk.replace(/"/g, '') + '"]';
    var al = el.getAttribute('aria-label');
    if (al) return '[aria-label="' + al.replace(/"/g, '') + '"]';
    return '';
  }

  var confirmRequest = null;
  var confirmDialog = document.getElementById('confirm-dialog');
  if (confirmDialog) {
    confirmDialog.addEventListener('close', function () {
      var go = confirmRequest;
      confirmRequest = null;
      if (confirmDialog.returnValue === 'ok' && typeof go === 'function') go(true);
    });
  }
  document.addEventListener('htmx:confirm', function (e) {
    var q = e.detail && e.detail.question;
    var text = document.getElementById('confirm-dialog-text');
    if (!q || !confirmDialog || !text || !e.detail || typeof e.detail.issueRequest !== 'function') return;
    e.preventDefault();
    text.textContent = q;
    confirmRequest = e.detail.issueRequest;
    confirmDialog.returnValue = '';
    if (confirmDialog.open) return;
    try {
      confirmDialog.showModal();
    } catch (err) {
      confirmDialog.show();
    }
  });

  // htmx fires beforeSwap on the swap target. Remember the element that sent
  // the request so a scan dialog can update the library card behind it.
  var requestSource = new WeakMap();
  document.addEventListener('htmx:beforeRequest', function (e) {
    var xhr = e.detail && e.detail.xhr;
    var src = e.detail && e.detail.elt;
    if (xhr && src) requestSource.set(xhr, src);
  });

  function responseHTML(detail) {
    if (!detail) return '';
    if (typeof detail.serverResponse === 'string') return detail.serverResponse;
    return (detail.xhr && detail.xhr.responseText) || '';
  }

  function reusePoster(cur, next) {
    var a = cur.querySelector('img.poster');
    var b = next.querySelector('img.poster');
    if (!a || !b) return;
    if (a.getAttribute('src') === b.getAttribute('src')) b.replaceWith(a);
  }

  function syncKeyed(parent, curNodes, nextNodes, before) {
    var byKey = {};
    for (var i = 0; i < curNodes.length; i++) {
      var key = curNodes[i].getAttribute('href') || '';
      if (key) byKey[key] = curNodes[i];
    }
    var used = {};
    var ordered = [];
    for (var j = 0; j < nextNodes.length; j++) {
      var incoming = nextNodes[j];
      var href = incoming.getAttribute('href') || '';
      var existing = href && byKey[href];
      if (existing && !used[href]) {
        used[href] = true;
        if (existing.outerHTML !== incoming.outerHTML) {
          reusePoster(existing, incoming);
          existing.replaceWith(incoming);
          ordered.push(incoming);
        } else {
          ordered.push(existing);
        }
      } else {
        ordered.push(incoming);
      }
    }
    for (var name in byKey) {
      if (!used[name]) byKey[name].remove();
    }
    for (var n = 0; n < ordered.length; n++) {
      parent.insertBefore(ordered[n], before || null);
    }
  }

  function directCards(root) {
    var out = [];
    if (!root) return out;
    var kids = root.children;
    for (var i = 0; i < kids.length; i++) {
      if (kids[i].classList && kids[i].classList.contains('card')) out.push(kids[i]);
    }
    return out;
  }

  // patchItemsLive updates the open library grid from a poll without replacing
  // cards whose poster is already on screen. Returns false when the response
  // is not a grid, so the caller can fall back to a full swap.
  function patchItemsLive(current, html) {
    var doc = new DOMParser().parseFromString(html, 'text/html');
    var next = doc.getElementById('items-live');
    if (!next) return false;
    var trig = next.getAttribute('hx-trigger');
    var prev = current.getAttribute('hx-trigger');
    if (trig) current.setAttribute('hx-trigger', trig);
    else current.removeAttribute('hx-trigger');
    if (trig !== prev && window.htmx && typeof htmx.process === 'function') htmx.process(current);

    var count = doc.getElementById('library-count');
    var liveCount = document.getElementById('library-count');
    if (count && liveCount && liveCount.textContent !== count.textContent) {
      liveCount.textContent = count.textContent;
    }

    var nextHero = next.querySelector('.folder-hero');
    var curHero = current.querySelector('.folder-hero');
    if (nextHero && curHero) {
      if (curHero.outerHTML !== nextHero.outerHTML) {
        reusePoster(curHero, nextHero);
        curHero.replaceWith(nextHero);
      }
    } else if (nextHero && !curHero) {
      current.insertBefore(nextHero, current.firstChild);
    } else if (!nextHero && curHero) {
      curHero.remove();
    }

    var curGrid = current.querySelector('#items');
    var nextGrid = next.querySelector('#items');
    if (!curGrid || !nextGrid) return true;
    var curList = curGrid.querySelector('.file-list');
    var nextList = nextGrid.querySelector('.file-list');
    syncKeyed(curGrid, directCards(curGrid), directCards(nextGrid), curList || curGrid.querySelector('p.muted'));
    curList = curGrid.querySelector('.file-list');
    if (nextList) {
      if (!curList) {
        curGrid.appendChild(nextList);
      } else {
        syncKeyed(curList, curList.querySelectorAll('a.file-card'), nextList.querySelectorAll('a.file-card'), null);
      }
    } else if (curList) {
      curList.remove();
    }
    var curEmpty = curGrid.querySelector(':scope > p.muted');
    var nextEmpty = nextGrid.querySelector(':scope > p.muted');
    if (nextEmpty) {
      if (!curEmpty) curGrid.appendChild(nextEmpty);
      else if (curEmpty.textContent !== nextEmpty.textContent) curEmpty.textContent = nextEmpty.textContent;
    } else if (curEmpty) {
      curEmpty.remove();
    }
    return true;
  }

  function pollSource(e) {
    var xhr = e.detail && e.detail.xhr;
    return xhr && requestSource.get(xhr);
  }

  // Skip background polls while a dialog is open. Allow a swap whose target is
  // inside a dialog, or whose request came from the open dialog.
  document.addEventListener('htmx:beforeSwap', function (e) {
    var target = e.detail && e.detail.target;
    if (!target || !target.contains) return;
    var dlg = openDialog();
    if (dlg && !dlg.contains(target) && !target.closest('dialog')) {
      var src = pollSource(e);
      if (!(src && dlg.contains(src))) {
        e.detail.shouldSwap = false;
        return;
      }
    }
    var active = document.activeElement;
    if (active && target.contains(active)) {
      if ((active.closest && active.closest('.editable.is-editing')) || isField(active)) {
        e.detail.shouldSwap = false;
        return;
      }
    }
    if (selectionIn(target)) {
      e.detail.shouldSwap = false;
      return;
    }
    var src = pollSource(e);
    if (target.id === 'items-live' && src && src.id === 'items-live') {
      if (patchItemsLive(target, responseHTML(e.detail))) {
        e.detail.shouldSwap = false;
        return;
      }
    }
    restoreFocus = null;
    if (active && target.contains(active)) {
      restoreFocus = focusKey(active);
    }
  });

  document.addEventListener('htmx:afterSwap', function (e) {
    if (!restoreFocus) return;
    var root = (e.detail && e.detail.target) || document;
    var el = null;
    try {
      el = root.querySelector(restoreFocus);
    } catch (err) {}
    if (!el && restoreFocus.charAt(0) === '#') {
      el = document.getElementById(restoreFocus.slice(1));
    }
    restoreFocus = null;
    if (el && typeof el.focus === 'function') el.focus();
  });
})();

(function () {
  var cm = null;
  var loading = null;

  function loadScript(src) {
    return new Promise(function (resolve, reject) {
      var existing = document.querySelector('script[src="' + src + '"]');
      if (existing) {
        if (existing.getAttribute('data-loaded') === '1' || (src.indexOf('codemirror.min.js') !== -1 && typeof CodeMirror !== 'undefined')) {
          resolve();
          return;
        }
        existing.addEventListener('load', function () { resolve(); });
        existing.addEventListener('error', function () { reject(new Error(src)); });
        return;
      }
      var s = document.createElement('script');
      s.src = src;
      s.onload = function () { s.setAttribute('data-loaded', '1'); resolve(); };
      s.onerror = function () { reject(new Error(src)); };
      document.head.appendChild(s);
    });
  }

  function loadCSS(href) {
    if (document.querySelector('link[href="' + href + '"]')) return;
    var link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = href;
    document.head.appendChild(link);
  }

  function editorFor(ta) {
    var sib = ta.previousElementSibling;
    if (sib && sib.CodeMirror) return sib.CodeMirror;
    sib = ta.nextElementSibling;
    if (sib && sib.CodeMirror) return sib.CodeMirror;
    return null;
  }

  function initCM() {
    var ta = document.getElementById('overlay-yaml');
    if (!ta || typeof CodeMirror === 'undefined') return;
    var existing = editorFor(ta);
    if (existing) {
      cm = existing;
      ta.setAttribute('data-cm-ready', '1');
      cm.refresh();
      return;
    }
    if (ta.getAttribute('data-cm-ready') === '1' && cm && cm.getTextArea && cm.getTextArea() === ta) {
      cm.refresh();
      return;
    }
    cm = CodeMirror.fromTextArea(ta, { mode: 'yaml', lineNumbers: true, lineWrapping: true, theme: 'servemedia' });
    ta.setAttribute('data-cm-ready', '1');
    cm.refresh();
  }

  function saveCM() {
    if (cm && cm.save) cm.save();
  }

  function ensureCM() {
    if (!document.getElementById('overlay-yaml')) return;
    loadCSS('/static/vendor/codemirror/codemirror.min.css');
    if (typeof CodeMirror !== 'undefined') {
      initCM();
      return;
    }
    if (!loading) {
      loading = loadScript('/static/vendor/codemirror/codemirror.min.js').then(function () {
        return loadScript('/static/vendor/codemirror/yaml.min.js');
      });
    }
    loading.then(initCM).catch(function () {});
  }

  document.addEventListener('htmx:afterSwap', ensureCM);
  document.addEventListener('htmx:configRequest', function (e) {
    var elt = e.detail && e.detail.elt;
    if (!elt || !elt.closest) return;
    if (elt.id === 'engines-matchmedia-form' || elt.closest('#engines-matchmedia-form') || elt.id === 'engines-save' || elt.id === 'engines-restore') {
      saveCM();
    }
  });
  document.addEventListener('submit', function (e) {
    if (e.target && e.target.id === 'engines-matchmedia-form') saveCM();
  }, true);

  document.addEventListener('click', function (e) {
    var t = e.target;
    if (!t || !t.closest) return;
    if (t.closest('#webhook-copy-key')) {
      var key = document.getElementById('webhook-api-key');
      if (key && navigator.clipboard) navigator.clipboard.writeText(key.value || '').catch(function () {});
      return;
    }
    if (t.closest('#test-webhooks')) {
      var testResult = document.getElementById('webhook-test-result');
      if (!testResult) return;
      testResult.textContent = 'Sending test…';
      fetch('/hx/integrations/webhooks/test', { method: 'POST', credentials: 'same-origin' })
        .then(function (r) { return r.text(); })
        .then(function (text) { testResult.textContent = text; })
        .catch(function (err) { testResult.textContent = String(err); });
      return;
    }
    if (!t.closest('#add-destination')) return;
    var destWrap = document.getElementById('webhook-destinations');
    var destCount = document.getElementById('dest-count');
    if (!destWrap || !destCount) return;
    var n = parseInt(destCount.value || '0', 10);
    var first = destWrap.querySelector('.webhook-destination');
    if (!first) return;
    var clone = first.cloneNode(true);
    clone.setAttribute('data-index', String(n));
    var legend = clone.querySelector('legend');
    if (legend) legend.textContent = 'Destination ' + (n + 1);
    clone.querySelectorAll('[name]').forEach(function (el) {
      el.name = el.name.replace(/^dest_\d+_/, 'dest_' + n + '_');
      if (el.type === 'checkbox') el.checked = false;
      else if (el.tagName === 'TEXTAREA') el.value = '';
      else if (el.type !== 'hidden') el.value = '';
    });
    var hc = clone.querySelector('[name="dest_' + n + '_header_count"]');
    if (hc) hc.value = '1';
    destWrap.appendChild(clone);
    destCount.value = String(n + 1);
  });

  function heightNumber(raw) {
    var text = String(raw == null ? '' : raw).trim();
    if (!/^[1-9]\d*$/.test(text)) return 0;
    var n = parseInt(text, 10);
    if (!isFinite(n) || n <= 0) return 0;
    return n;
  }

  function addHeightChip() {
    var input = document.getElementById('height-custom');
    var wrap = document.getElementById('height-chips');
    if (!input || !wrap) return;
    var n = heightNumber(input.value);
    if (!n) return;
    var existing = wrap.querySelectorAll('input[name="heights"]');
    for (var i = 0; i < existing.length; i++) {
      if (existing[i].value === String(n)) {
        existing[i].checked = true;
        input.value = '';
        return;
      }
    }
    var label = document.createElement('label');
    label.className = 'chip chip-custom';
    var box = document.createElement('input');
    box.type = 'checkbox';
    box.name = 'heights';
    box.value = String(n);
    box.checked = true;
    var span = document.createElement('span');
    span.textContent = n + 'p';
    var remove = document.createElement('button');
    remove.type = 'button';
    remove.className = 'chip-remove';
    remove.setAttribute('aria-label', 'Remove ' + n + 'p');
    remove.textContent = '\u00d7';
    label.appendChild(box);
    label.appendChild(span);
    label.appendChild(remove);
    wrap.appendChild(label);
    input.value = '';
  }

  document.addEventListener('click', function (e) {
    var t = e.target;
    if (!t || !t.closest) return;
    if (t.closest('#height-add')) {
      e.preventDefault();
      addHeightChip();
      return;
    }
    var remove = t.closest('.chip-remove');
    if (!remove) return;
    e.preventDefault();
    var chip = remove.closest('.chip');
    if (chip) chip.remove();
  });

  document.addEventListener('keydown', function (e) {
    if (e.key !== 'Enter') return;
    var t = e.target;
    if (!t || t.id !== 'height-custom') return;
    e.preventDefault();
    addHeightChip();
  });

  var contextField = null;
  var textInputTypes = { text: 1, search: 1, email: 1, url: 1, tel: 1, password: 1, number: 1 };

  function textFieldTarget(el) {
    if (!el || !el.closest) return null;
    if (el.closest('.CodeMirror')) return null;
    var area = el.closest('textarea');
    if (area) return area;
    var input = el.closest('input');
    if (input) {
      var type = (input.getAttribute('type') || 'text').toLowerCase();
      return textInputTypes[type] ? input : null;
    }
    var rich = el.closest('[contenteditable]');
    if (!rich || rich.getAttribute('contenteditable') === 'false') return null;
    return rich;
  }

  function fieldText(field) {
    if (!field) return '';
    if (field.isContentEditable) return field.textContent || '';
    return field.value || '';
  }

  function fieldSelectedText(field) {
    if (!field) return '';
    if (field.isContentEditable) {
      var sel = window.getSelection();
      if (!sel || !sel.rangeCount || !field.contains(sel.anchorNode)) return '';
      return sel.toString();
    }
    if (field.selectionStart == null || field.selectionEnd == null || field.selectionStart === field.selectionEnd) return '';
    return String(field.value || '').slice(field.selectionStart, field.selectionEnd);
  }

  function fieldLocked(field) {
    return !!(field && (field.readOnly || field.disabled));
  }

  function commandAvailable(name) {
    try {
      return document.queryCommandEnabled(name);
    } catch (err) {
      return false;
    }
  }

  function commandEnabled(id, field) {
    if (!field || field.disabled) return false;
    var locked = fieldLocked(field);
    if (id === 'selectall') return fieldText(field).length > 0;
    if (id === 'copy') return fieldSelectedText(field).length > 0;
    if (id === 'cut') return fieldSelectedText(field).length > 0 && !locked;
    if (id === 'paste') return !locked;
    if (id === 'undo') return !locked && commandAvailable('undo');
    if (id === 'redo') return !locked && commandAvailable('redo');
    return false;
  }

  function contextMenu() {
    var menu = document.getElementById('app-context-menu');
    if (menu) return menu;
    menu = document.createElement('div');
    menu.id = 'app-context-menu';
    menu.hidden = true;
    menu.setAttribute('role', 'menu');
    var empty = document.createElement('p');
    empty.className = 'app-context-menu-empty';
    empty.textContent = '¯\_(ツ)_/¯';
    menu.appendChild(empty);
    var list = document.createElement('div');
    list.className = 'app-context-menu-items';
    list.hidden = true;
    var cmds = [
      ['selectall', 'Select all'],
      ['copy', 'Copy'],
      ['cut', 'Cut'],
      ['paste', 'Paste'],
      ['undo', 'Undo'],
      ['redo', 'Redo']
    ];
    for (var i = 0; i < cmds.length; i++) {
      var item = document.createElement('div');
      item.setAttribute('role', 'menuitem');
      item.setAttribute('data-cmd', cmds[i][0]);
      item.textContent = cmds[i][1];
      list.appendChild(item);
    }
    menu.appendChild(list);
    menu.addEventListener('dragstart', function (e) {
      e.preventDefault();
    });
    document.body.appendChild(menu);
    return menu;
  }

  function menuHost() {
    var dialogs = document.querySelectorAll('dialog[open]');
    return dialogs.length ? dialogs[dialogs.length - 1] : document.body;
  }

  function menuItemFromEvent(e) {
    var t = e.target;
    if (!t || !t.closest) return null;
    return t.closest('#app-context-menu [data-cmd]');
  }

  function hideContextMenu() {
    var menu = document.getElementById('app-context-menu');
    if (menu) menu.hidden = true;
    contextField = null;
  }

  function placeContextMenu(menu, x, y) {
    var host = menuHost();
    if (menu.parentNode !== host) host.appendChild(menu);
    menu.hidden = false;
    var left = x;
    var top = y;
    if (host === document.body) {
      menu.style.position = 'fixed';
    } else {
      var box = host.getBoundingClientRect();
      var style = window.getComputedStyle(host);
      var borderL = parseFloat(style.borderLeftWidth) || 0;
      var borderT = parseFloat(style.borderTopWidth) || 0;
      menu.style.position = 'absolute';
      left = x - box.left - borderL + host.scrollLeft;
      top = y - box.top - borderT + host.scrollTop;
    }
    menu.style.left = left + 'px';
    menu.style.top = top + 'px';
    var rect = menu.getBoundingClientRect();
    if (rect.right > window.innerWidth) {
      menu.style.left = (left - (rect.right - window.innerWidth) - 4) + 'px';
    }
    if (rect.bottom > window.innerHeight) {
      menu.style.top = (top - (rect.bottom - window.innerHeight) - 4) + 'px';
    }
  }

  function showContextMenu(x, y, field) {
    var menu = contextMenu();
    contextField = field || null;
    var empty = menu.querySelector('.app-context-menu-empty');
    var list = menu.querySelector('.app-context-menu-items');
    if (field) {
      menu.classList.add('has-items');
      if (empty) empty.hidden = true;
      if (list) {
        list.hidden = false;
        var buttons = list.querySelectorAll('[data-cmd]');
        for (var i = 0; i < buttons.length; i++) {
          if (commandEnabled(buttons[i].getAttribute('data-cmd'), field)) buttons[i].removeAttribute('aria-disabled');
          else buttons[i].setAttribute('aria-disabled', 'true');
        }
      }
    } else {
      menu.classList.remove('has-items');
      if (empty) empty.hidden = false;
      if (list) list.hidden = true;
    }
    placeContextMenu(menu, x, y);
  }

  function selectFieldAll(field) {
    field.focus();
    if (field.isContentEditable) {
      document.execCommand('selectAll');
      return;
    }
    var len = String(field.value || '').length;
    try {
      field.setSelectionRange(0, len);
    } catch (err) {}
  }

  function deleteFieldSelection(field) {
    field.focus();
    if (document.execCommand('delete')) return;
    if (typeof field.setRangeText === 'function' && field.selectionStart != null) {
      field.setRangeText('', field.selectionStart, field.selectionEnd, 'end');
    }
  }

  function insertFieldText(field, text) {
    field.focus();
    if (document.execCommand('insertText', false, text)) return;
    if (typeof field.setRangeText === 'function') {
      var start = field.selectionStart == null ? String(field.value || '').length : field.selectionStart;
      var end = field.selectionEnd == null ? start : field.selectionEnd;
      field.setRangeText(text, start, end, 'end');
    }
  }

  function runFieldCommand(id, field) {
    if (!field) return;
    field.focus();
    if (id === 'selectall') {
      selectFieldAll(field);
      return;
    }
    if (id === 'copy' || id === 'cut') {
      var selected = fieldSelectedText(field);
      if (!selected || !navigator.clipboard) return;
      navigator.clipboard.writeText(selected).then(function () {
        if (id === 'cut') deleteFieldSelection(field);
      }).catch(function () {});
      return;
    }
    if (id === 'paste') {
      if (!navigator.clipboard || !navigator.clipboard.readText) return;
      navigator.clipboard.readText().then(function (text) {
        insertFieldText(field, text);
      }).catch(function () {});
      return;
    }
    if (id === 'undo') document.execCommand('undo');
    if (id === 'redo') document.execCommand('redo');
  }

  document.addEventListener('contextmenu', function (e) {
    if (e.target && e.target.closest && e.target.closest('select, .CodeMirror')) {
      hideContextMenu();
      return;
    }
    e.preventDefault();
    var field = textFieldTarget(e.target);
    if (field && document.activeElement !== field) field.focus();
    showContextMenu(e.clientX, e.clientY, field);
  }, true);

  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') hideContextMenu();
  });
  document.addEventListener('pointerdown', function (e) {
    var menu = document.getElementById('app-context-menu');
    if (!menu || menu.hidden) return;
    if (e.target && menu.contains(e.target)) {
      e.preventDefault();
      return;
    }
    hideContextMenu();
  });
  document.addEventListener('pointerup', function (e) {
    var menu = document.getElementById('app-context-menu');
    if (!menu || menu.hidden) return;
    var item = menuItemFromEvent(e);
    if (!item || item.getAttribute('aria-disabled') === 'true' || !contextField) return;
    var field = contextField;
    var id = item.getAttribute('data-cmd');
    hideContextMenu();
    runFieldCommand(id, field);
  });
  window.addEventListener('resize', hideContextMenu);
  window.addEventListener('scroll', hideContextMenu, true);

  document.body.addEventListener('htmx:responseError', function (e) {
    var detail = e.detail;
    if (!detail || !detail.xhr) return;
    var elt = detail.elt;
    var target = detail.target;
    var aimed = target && target.id === 'settings-panel';
    if (!aimed && elt && elt.getAttribute && elt.getAttribute('hx-target') === '#settings-panel') aimed = true;
    if (!aimed && elt && elt.closest && elt.closest('#settings-panel')) aimed = true;
    if (!aimed) return;
    var panel = document.getElementById('settings-panel');
    if (!panel) return;
    var level = detail.xhr.status >= 500 ? 'error' : 'warn';
    var text = String(detail.xhr.responseText || '').replace(/^\s+|\s+$/g, '');
    if (!text) text = level === 'error' ? 'Save failed' : 'Could not save';
    var note = panel.querySelector('.settings-notice');
    if (!note) {
      note = document.createElement('p');
      note.setAttribute('role', 'status');
      panel.insertBefore(note, panel.firstChild);
    }
    note.className = 'settings-notice settings-notice-' + level;
    note.textContent = text;
  });
})();
