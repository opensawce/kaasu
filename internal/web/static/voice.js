// Speaks confirmed payments on the dashboard. Each order is spoken once, even after a reconnect.
(function () {
  'use strict';
  var KEY = 'kaasu.spoken';
  var memory = [];        // fallback history when sessionStorage is blocked
  var pending = {};       // orders queued to speak but not finished yet
  function seen() {
    try { return JSON.parse(sessionStorage.getItem(KEY) || '[]').concat(memory); } catch (e) { return memory.slice(); }
  }
  function remember(id) {
    memory.push(id);
    while (memory.length > 50) memory.shift();
    try {
      var list = JSON.parse(sessionStorage.getItem(KEY) || '[]');
      list.push(id);
      while (list.length > 50) list.shift();
      sessionStorage.setItem(KEY, JSON.stringify(list));
    } catch (e) { /* private mode: the in-memory list still suppresses repeats */ }
  }
  window.kaasuSpeak = function (orderId, text, bcp47) {
    if (!('speechSynthesis' in window)) return false;
    if (pending[orderId] || seen().indexOf(orderId) !== -1) return false;
    pending[orderId] = true;
    var u = new SpeechSynthesisUtterance(text);
    u.lang = bcp47;
    u.onend = function () { delete pending[orderId]; remember(orderId); };
    u.onerror = function () { delete pending[orderId]; }; // failed or cancelled speech may be retried
    speechSynthesis.speak(u);
    return true;
  };
})();
