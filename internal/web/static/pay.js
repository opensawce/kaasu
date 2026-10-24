// Polls the order status every two seconds and reloads when the state changes. No third-party code.
(function () {
  var root = document.querySelector('.pay');
  if (!root) return;
  var url = root.getAttribute('data-status-url');
  var last = root.getAttribute('data-state');
  var timer = setInterval(function () {
    fetch(url, { headers: { Accept: 'application/json' } })
      .then(function (r) { return r.json(); })
      .then(function (s) {
        if (s.state !== last) { clearInterval(timer); location.reload(); }
      })
      .catch(function () { /* keep waiting; never show a failure while the outcome is unknown */ });
  }, 2000);
})();
