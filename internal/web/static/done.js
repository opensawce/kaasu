// Counts up the time since the payment was confirmed. Respects reduced motion by updating once a second, not animating.
(function () {
  var el = document.getElementById('ago');
  var live = document.querySelector('.live');
  if (!el || !live) return;
  var since = parseInt(live.getAttribute('data-since'), 10) * 1000;
  function tick() {
    var s = Math.max(0, Math.floor((Date.now() - since) / 1000));
    el.textContent = s < 60 ? s + ' s' : Math.floor(s / 60) + ' min';
  }
  tick();
  setInterval(tick, 1000);
})();
