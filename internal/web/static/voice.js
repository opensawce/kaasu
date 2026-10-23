// Speaks confirmed payments on the dashboard. Each order is spoken once, even after a reconnect.
(function () {
  'use strict';
  var KEY = 'kaasu.spoken';
  function seen() {
    try { return JSON.parse(sessionStorage.getItem(KEY) || '[]'); } catch (e) { return []; }
  }
  function remember(id, list) {
    list.push(id);
    while (list.length > 50) list.shift();
    try { sessionStorage.setItem(KEY, JSON.stringify(list)); } catch (e) { /* private mode: keep going */ }
  }
  window.kaasuSpeak = function (orderId, text, bcp47) {
    if (!('speechSynthesis' in window)) return false;
    var list = seen();
    if (list.indexOf(orderId) !== -1) return false;
    remember(orderId, list);
    var u = new SpeechSynthesisUtterance(text);
    u.lang = bcp47;
    speechSynthesis.speak(u);
    return true;
  };
})();
