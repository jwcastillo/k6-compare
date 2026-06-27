import http from 'k6/http';
import { check, sleep } from 'k6';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.1.0/index.js';

export const options = {
  scenarios: {
    smoke: {
      executor: 'constant-vus', // closed model -> k6-compare detects it as such
      vus: 50,
      duration: '60s',
    },
  },
  // p(99) and p(99.9) are required: by default k6 omits these tail
  // percentiles from the summary, and k6-compare cannot gate on them.
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)', 'p(99.9)'],
  thresholds: {
    http_req_duration: ['p(95)<500', 'p(99)<1000'],
    http_req_failed: ['rate<0.01'],
  },
};

const BASE = __ENV.TARGET_URL || 'https://test.k6.io';

export default function () {
  const res = http.get(`${BASE}/`);
  check(res, {
    'status is 200': (r) => r.status === 200,
    'body not empty': (r) => r.body && r.body.length > 0,
  });
  sleep(0.3);
}

export function handleSummary(data) {
  return {
    'summary.json': JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
  };
}
