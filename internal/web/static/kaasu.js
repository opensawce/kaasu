/* Kaasu checkout snippet. Opens the pay page in a redirect or a popup. No dependencies, no inline handlers. */
(function (global) {
  'use strict';
  var Kaasu = {};
  Kaasu.open = function (payUrl, opts) {
    opts = opts || {};
    var origin = new URL(payUrl, global.location.href).origin;
    if (opts.onStatus) {
      global.addEventListener('message', function (e) {
        if (e.origin !== origin || !e.data || e.data.type !== 'kaasu:status') return;
        opts.onStatus(e.data.state);
      });
    }
    if (opts.mode === 'popup') {
      var w = global.open(payUrl, 'kaasu_pay', 'popup,width=420,height=720');
      if (w) return w;
    }
    global.location.assign(payUrl);
    return null;
  };
  global.Kaasu = Kaasu;
})(window);
