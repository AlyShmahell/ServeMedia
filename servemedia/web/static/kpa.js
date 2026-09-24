(function () {
  var KEY = 'servemedia.tab';
  var tab = '';
  try { tab = sessionStorage.getItem(KEY) || ''; } catch (e) {}
  if (!tab) {
    if (window.crypto && typeof crypto.randomUUID === 'function') tab = crypto.randomUUID();
    else tab = Date.now().toString(36) + Math.random().toString(36).slice(2);
    try { sessionStorage.setItem(KEY, tab); } catch (e) {}
  }

  function send() {
    var body = new URLSearchParams();
    body.set('tab', tab);
    body.set('path', location.pathname);
    body.set('title', document.title || '');
    fetch('/hx/kpa', {
      method: 'POST',
      credentials: 'same-origin',
      headers: {'Content-Type': 'application/x-www-form-urlencoded'},
      body: body,
      keepalive: true
    }).catch(function () {});
  }

  send();
  setInterval(send, 10000);
})();
