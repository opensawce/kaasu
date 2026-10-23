// Load shape for order creation: 50 orders a second for a minute, with idempotency keys.
// Run against a local instance: k6 run -e BASE=http://127.0.0.1:8080 -e KEY=kaasu_live_... load/k6/orders.js
import http from 'k6/http';
import { check } from 'k6';
import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';

export const options = {
  scenarios: { create: { executor: 'constant-arrival-rate', rate: 50, timeUnit: '1s', duration: '60s', preAllocatedVUs: 20 } },
  thresholds: {
    http_req_duration: ['p(95)<250'],
    http_req_failed: ['rate<0.01'],
  },
};

export default function () {
  const res = http.post(`${__ENV.BASE}/v1/orders`, JSON.stringify({ amount_paise: 49907, note: 'load' }), {
    headers: { Authorization: `Bearer ${__ENV.KEY}`, 'Content-Type': 'application/json', 'Idempotency-Key': uuidv4() },
  });
  check(res, { created: (r) => r.status === 201 });
}
