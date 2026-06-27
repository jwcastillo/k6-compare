import http from 'k6/http';
import { check } from 'k6';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.1.0/index.js';

export const options = {
  scenarios: {
    constant_load: {
      executor: 'constant-arrival-rate', // OPEN model
      rate: 200,            // 200 iterations per timeUnit
      timeUnit: '1s',       // => 200 RPS target, constant
      duration: '1m',
      preAllocatedVUs: 50,  // start with 50 VUs
      maxVUs: 200,          // scale up to 200 if the server slows down
    },
  },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)', 'p(99.9)'],
  thresholds: {
    http_req_duration: ['p(99)<1000'],
    http_req_failed: ['rate<0.01'],
    dropped_iterations: ['count<100'],
  },
};

const BASE = __ENV.TARGET_URL || 'https://test.k6.io';

export default function () {
  const res = http.get(`${BASE}/`);
  check(res, { 'status is 200': (r) => r.status === 200 });
}

export function handleSummary(data) {
  return {
    'summary.json': JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
  };
}
