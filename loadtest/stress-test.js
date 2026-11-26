import http from 'k6/http';
import exec from 'k6/execution';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

const BASE_URL = (__ENV.BASE_URL || 'http://localhost:8080').replace(/\/+$/, '');
const FIXTURE_PATH = __ENV.FIXTURE_PATH || './.k6-fixture.json';
const SUMMARY_PATH = __ENV.SUMMARY_PATH || './results/summary.json';
const IS_QUICK = __ENV.QUICK === '1' || __ENV.QUICK === 'true';

const STAGE_SEC = Number(__ENV.STAGE_SEC || (IS_QUICK ? 20 : 60));
const RAMP_SEC = Number(__ENV.RAMP_SEC || (IS_QUICK ? 5 : 10));
const HOLD_SEC = Math.max(5, STAGE_SEC - RAMP_SEC);
const POLL_INTERVAL_SEC = Number(__ENV.POLL_INTERVAL_SEC || 0.5);
const POLL_TIMEOUT_SEC = Number(__ENV.POLL_TIMEOUT_SEC || (IS_QUICK ? 60 : 120));
const GRACEFUL_STOP = __ENV.GRACEFUL_STOP || (IS_QUICK ? '65s' : '130s');

const API_NORMAL_VUS = Number(__ENV.API_NORMAL_VUS || (IS_QUICK ? 5 : 10));
const API_HIGH_VUS = Number(__ENV.API_HIGH_VUS || (IS_QUICK ? 25 : 80));
const API_STRESS_VUS = Number(__ENV.API_STRESS_VUS || (IS_QUICK ? 60 : 220));
const API_RECOVERY_VUS = Number(__ENV.API_RECOVERY_VUS || API_NORMAL_VUS);

const SAME_NORMAL_RPM = Number(__ENV.SAME_NORMAL_RPM || (IS_QUICK ? 6 : 4));
const SAME_HIGH_RPM = Number(__ENV.SAME_HIGH_RPM || (IS_QUICK ? 18 : 18));
const SAME_STRESS_RPM = Number(__ENV.SAME_STRESS_RPM || (IS_QUICK ? 36 : 48));
const SAME_RECOVERY_RPM = Number(__ENV.SAME_RECOVERY_RPM || SAME_NORMAL_RPM);

const DIFF_NORMAL_RPM = Number(__ENV.DIFF_NORMAL_RPM || (IS_QUICK ? 12 : 9));
const DIFF_HIGH_RPM = Number(__ENV.DIFF_HIGH_RPM || (IS_QUICK ? 36 : 36));
const DIFF_STRESS_RPM = Number(__ENV.DIFF_STRESS_RPM || (IS_QUICK ? 60 : 90));
const DIFF_RECOVERY_RPM = Number(__ENV.DIFF_RECOVERY_RPM || DIFF_NORMAL_RPM);

const STAGES = ['normal', 'high', 'stress', 'recovery'];
const TERMINAL_STATUSES = {
  success: true,
  failed: true,
  partial: true,
  rejected: true,
  compensated: true,
};

const fixture = JSON.parse(open(FIXTURE_PATH));
if (!fixture.sessions || fixture.sessions.length === 0) {
  throw new Error(`No sessions found in fixture at ${FIXTURE_PATH}`);
}
if (!fixture.databaseIds || fixture.databaseIds.length === 0) {
  throw new Error(`No databaseIds found in fixture at ${FIXTURE_PATH}`);
}

// Overall API metrics (excluding background execution polling requests)
const apiReqDuration = new Trend('api_req_duration', true);
const apiReqs = new Counter('api_reqs');
const apiErrorRate = new Rate('api_error_rate');

// Per-stage API metrics
const apiStageDuration = {
  normal: new Trend('api_duration_normal', true),
  high: new Trend('api_duration_high', true),
  stress: new Trend('api_duration_stress', true),
  recovery: new Trend('api_duration_recovery', true),
};
const apiStageReqs = {
  normal: new Counter('api_reqs_normal'),
  high: new Counter('api_reqs_high'),
  stress: new Counter('api_reqs_stress'),
  recovery: new Counter('api_reqs_recovery'),
};
const apiStageErrorRate = {
  normal: new Rate('api_error_rate_normal'),
  high: new Rate('api_error_rate_high'),
  stress: new Rate('api_error_rate_stress'),
  recovery: new Rate('api_error_rate_recovery'),
};

// Overall migration metrics (separating 202 enqueue acceptance from terminal completion)
const migrationEnqueueDuration = new Trend('migration_enqueue_duration', true);
const migrationEnqueueAccepted = new Counter('migration_enqueue_accepted');
const migrationJobsSucceeded = new Counter('migration_jobs_succeeded');
const migrationJobsFailed = new Counter('migration_jobs_failed');
const migrationJobsTimedOut = new Counter('migration_jobs_timed_out');
const migrationJobErrorRate = new Rate('migration_job_error_rate');

const migrationQueueWaitMs = new Trend('migration_queue_wait_ms', true);
const migrationExecDurationMs = new Trend('migration_exec_duration_ms', true);
const migrationE2eDurationMs = new Trend('migration_e2e_duration_ms', true);

// Per-stage migration metrics (attributed to submitStage)
const migStageAccepted = {
  normal: new Counter('mig_accepted_normal'),
  high: new Counter('mig_accepted_high'),
  stress: new Counter('mig_accepted_stress'),
  recovery: new Counter('mig_accepted_recovery'),
};
const migStageSucceeded = {
  normal: new Counter('mig_succeeded_normal'),
  high: new Counter('mig_succeeded_high'),
  stress: new Counter('mig_succeeded_stress'),
  recovery: new Counter('mig_succeeded_recovery'),
};
const migStageFailed = {
  normal: new Counter('mig_failed_normal'),
  high: new Counter('mig_failed_high'),
  stress: new Counter('mig_failed_stress'),
  recovery: new Counter('mig_failed_recovery'),
};
const migStageErrorRate = {
  normal: new Rate('mig_job_error_rate_normal'),
  high: new Rate('mig_job_error_rate_high'),
  stress: new Rate('mig_job_error_rate_stress'),
  recovery: new Rate('mig_job_error_rate_recovery'),
};
const migStageQueueWait = {
  normal: new Trend('mig_queue_wait_normal', true),
  high: new Trend('mig_queue_wait_high', true),
  stress: new Trend('mig_queue_wait_stress', true),
  recovery: new Trend('mig_queue_wait_recovery', true),
};
const migStageExecDuration = {
  normal: new Trend('mig_exec_duration_normal', true),
  high: new Trend('mig_exec_duration_high', true),
  stress: new Trend('mig_exec_duration_stress', true),
  recovery: new Trend('mig_exec_duration_recovery', true),
};
const migStageE2eDuration = {
  normal: new Trend('mig_e2e_duration_normal', true),
  high: new Trend('mig_e2e_duration_high', true),
  stress: new Trend('mig_e2e_duration_stress', true),
  recovery: new Trend('mig_e2e_duration_recovery', true),
};

// Same-DB vs Different-DB comparison metrics
const migModeAccepted = {
  same_db: new Counter('mig_accepted_same_db'),
  diff_db: new Counter('mig_accepted_diff_db'),
};
const migModeSucceeded = {
  same_db: new Counter('mig_succeeded_same_db'),
  diff_db: new Counter('mig_succeeded_diff_db'),
};
const migModeFailed = {
  same_db: new Counter('mig_failed_same_db'),
  diff_db: new Counter('mig_failed_diff_db'),
};
const migModeErrorRate = {
  same_db: new Rate('mig_job_error_rate_same_db'),
  diff_db: new Rate('mig_job_error_rate_diff_db'),
};
const migModeQueueWait = {
  same_db: new Trend('mig_queue_wait_same_db', true),
  diff_db: new Trend('mig_queue_wait_diff_db', true),
};
const migModeExecDuration = {
  same_db: new Trend('mig_exec_duration_same_db', true),
  diff_db: new Trend('mig_exec_duration_diff_db', true),
};
const migModeE2eDuration = {
  same_db: new Trend('mig_e2e_duration_same_db', true),
  diff_db: new Trend('mig_e2e_duration_diff_db', true),
};

function makeStages(v1, v2, v3, v4) {
  return [
    { duration: `${RAMP_SEC}s`, target: v1 },
    { duration: `${HOLD_SEC}s`, target: v1 },
    { duration: `${RAMP_SEC}s`, target: v2 },
    { duration: `${HOLD_SEC}s`, target: v2 },
    { duration: `${RAMP_SEC}s`, target: v3 },
    { duration: `${HOLD_SEC}s`, target: v3 },
    { duration: `${RAMP_SEC}s`, target: v4 },
    { duration: `${HOLD_SEC}s`, target: v4 },
  ];
}

export const options = {
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max', 'count'],
  scenarios: {
    api_access: {
      executor: 'ramping-vus',
      startVUs: 1,
      stages: makeStages(API_NORMAL_VUS, API_HIGH_VUS, API_STRESS_VUS, API_RECOVERY_VUS),
      gracefulRampDown: '5s',
      gracefulStop: '10s',
      exec: 'runApiAccess',
    },
    migrations_same_db: {
      executor: 'ramping-arrival-rate',
      startRate: 1,
      timeUnit: '1m',
      preAllocatedVUs: 15,
      maxVUs: 80,
      stages: makeStages(SAME_NORMAL_RPM, SAME_HIGH_RPM, SAME_STRESS_RPM, SAME_RECOVERY_RPM),
      gracefulStop: GRACEFUL_STOP,
      exec: 'runSameDbMigration',
    },
    migrations_diff_db: {
      executor: 'ramping-arrival-rate',
      startRate: 1,
      timeUnit: '1m',
      preAllocatedVUs: 20,
      maxVUs: 90,
      stages: makeStages(DIFF_NORMAL_RPM, DIFF_HIGH_RPM, DIFF_STRESS_RPM, DIFF_RECOVERY_RPM),
      gracefulStop: GRACEFUL_STOP,
      exec: 'runDiffDbMigration',
    },
  },
  thresholds: {
    'http_req_duration{kind:api}': ['p(95)<500'],
    api_req_duration: ['p(95)<500'],
    api_duration_normal: ['p(95)<150'],
    api_duration_recovery: ['p(95)<250'],
    http_req_failed: ['rate<0.01'],
    api_error_rate: ['rate<0.01'],
    migration_job_error_rate: ['rate<0.05'],
  },
};

function currentStage() {
  const elapsedSec = (exec.instance.currentTestRunDuration || 0) / 1000;
  if (elapsedSec < STAGE_SEC) return 'normal';
  if (elapsedSec < 2 * STAGE_SEC) return 'high';
  if (elapsedSec < 3 * STAGE_SEC) return 'stress';
  return 'recovery';
}

function sessionForVu() {
  const sessions = fixture.sessions;
  const idx = (exec.vu.idInTest + exec.scenario.iterationInTest) % sessions.length;
  return sessions[idx];
}

function authHeaders(session, includeJsonContentType = false) {
  const headers = {
    Cookie: session.cookie,
    'X-CSRF-Token': session.csrf,
  };
  if (includeJsonContentType) {
    headers['Content-Type'] = 'application/json';
  }
  return headers;
}

export function runApiAccess() {
  const stage = currentStage();
  const session = sessionForVu();
  const dbIds = fixture.databaseIds;
  const dbId = dbIds[(exec.vu.idInTest + exec.vu.iterationInScenario) % dbIds.length];

  const r = Math.random();
  let endpoint = 'databases';
  let res;

  if (r < 0.10) {
    endpoint = 'databases_list';
    res = http.get(`${BASE_URL}/databases`, {
      headers: authHeaders(session),
      tags: { kind: 'api', endpoint, stage },
    });
  } else if (r < 0.25) {
    endpoint = 'schemas';
    res = http.get(`${BASE_URL}/databases/${dbId}/schemas`, {
      headers: authHeaders(session),
      tags: { kind: 'api', endpoint, stage },
    });
  } else if (r < 0.40) {
    endpoint = 'tables';
    res = http.get(`${BASE_URL}/databases/${dbId}/tables?schema=public`, {
      headers: authHeaders(session),
      tags: { kind: 'api', endpoint, stage },
    });
  } else if (r < 0.55) {
    endpoint = 'table_structure';
    res = http.get(`${BASE_URL}/databases/${dbId}/table-structure?schema=public&table=trainees`, {
      headers: authHeaders(session),
      tags: { kind: 'api', endpoint, stage },
    });
  } else if (r < 0.70) {
    endpoint = 'table_rows';
    res = http.get(`${BASE_URL}/databases/${dbId}/table-rows?schema=public&table=trainees&page=1&page_size=25`, {
      headers: authHeaders(session),
      tags: { kind: 'api', endpoint, stage },
    });
  } else if (r < 0.90) {
    endpoint = 'viewer_query';
    const sqlPayload = JSON.stringify({
      sql: 'SELECT t.id, t.name, tm.name AS team_name, t.running_style FROM public.trainees t LEFT JOIN public.teams tm ON tm.id = t.team_id ORDER BY t.id LIMIT 25',
      page: 1,
      page_size: 25,
    });
    res = http.post(`${BASE_URL}/databases/${dbId}/query`, sqlPayload, {
      headers: authHeaders(session, true),
      tags: { kind: 'api', endpoint, stage },
    });
  } else {
    endpoint = 'migration_history';
    res = http.get(`${BASE_URL}/databases/${dbId}/migrations/history?page=1&page_size=20`, {
      headers: authHeaders(session),
      tags: { kind: 'api', endpoint, stage },
    });
  }

  const ok = check(res, {
    'api status is 200': (resp) => resp.status === 200,
  });

  const dur = res.timings.duration;
  apiReqDuration.add(dur);
  apiReqs.add(1);
  apiErrorRate.add(ok ? 0 : 1);

  apiStageDuration[stage].add(dur);
  apiStageReqs[stage].add(1);
  apiStageErrorRate[stage].add(ok ? 0 : 1);

  sleep(0.03 + Math.random() * 0.04);
}

export function runSameDbMigration() {
  const idx = exec.scenario.iterationInTest;
  const ids = fixture.sameDbExecutionIds || [];
  if (idx >= ids.length) {
    console.warn(`Exhausted pre-approved sameDbExecutionIds at index ${idx} (pool=${ids.length})`);
    const stage = currentStage();
    migrationJobsFailed.add(1);
    migStageFailed[stage].add(1);
    migModeFailed.same_db.add(1);
    migrationJobErrorRate.add(1);
    migStageErrorRate[stage].add(1);
    migModeErrorRate.same_db.add(1);
    return;
  }
  runPreApprovedMigration(ids[idx], 'same_db');
}

export function runDiffDbMigration() {
  const idx = exec.scenario.iterationInTest;
  const ids = fixture.diffDbExecutionIds || [];
  if (idx >= ids.length) {
    console.warn(`Exhausted pre-approved diffDbExecutionIds at index ${idx} (pool=${ids.length})`);
    const stage = currentStage();
    migrationJobsFailed.add(1);
    migStageFailed[stage].add(1);
    migModeFailed.diff_db.add(1);
    migrationJobErrorRate.add(1);
    migStageErrorRate[stage].add(1);
    migModeErrorRate.diff_db.add(1);
    return;
  }
  runPreApprovedMigration(ids[idx], 'diff_db');
}

function recordTerminalFailure(stage, targetMode) {
  migrationJobsFailed.add(1);
  migStageFailed[stage].add(1);
  migModeFailed[targetMode].add(1);

  migrationJobErrorRate.add(1);
  migStageErrorRate[stage].add(1);
  migModeErrorRate[targetMode].add(1);
}

function runPreApprovedMigration(executionId, targetMode) {
  const submitStage = currentStage();
  const session = sessionForVu();
  const enqueueStartMs = Date.now();

  const enqRes = http.post(
    `${BASE_URL}/migrations/executions/${executionId}/enqueue`,
    null,
    {
      headers: authHeaders(session),
      tags: { kind: 'migration_enqueue', target_mode: targetMode, stage: submitStage },
    }
  );

  migrationEnqueueDuration.add(enqRes.timings.duration);

  const enqueueAccepted = check(enqRes, {
    'migration enqueue accepted (202/200)': (resp) => resp.status === 202 || resp.status === 200,
  });

  if (!enqueueAccepted) {
    console.warn(`Enqueue failed for execution ${executionId} (${targetMode}, ${submitStage}): HTTP ${enqRes.status}`);
    recordTerminalFailure(submitStage, targetMode);
    return;
  }

  // Enqueue 202 counts ONLY as queued acceptance, NEVER as execution success.
  migrationEnqueueAccepted.add(1);
  migStageAccepted[submitStage].add(1);
  migModeAccepted[targetMode].add(1);

  let clientStartedAtMs = null;
  const deadlineMs = Date.now() + POLL_TIMEOUT_SEC * 1000;

  while (Date.now() < deadlineMs) {
    sleep(POLL_INTERVAL_SEC);

    const pollRes = http.get(`${BASE_URL}/migrations/executions/${executionId}`, {
      headers: authHeaders(session),
      tags: { kind: 'migration_poll', target_mode: targetMode, stage: submitStage },
    });

    if (pollRes.status !== 200) {
      continue;
    }

    let payload;
    try {
      payload = pollRes.json();
    } catch (_err) {
      continue;
    }

    const status = payload && payload.status;
    if (status === 'running' && clientStartedAtMs === null) {
      clientStartedAtMs = Date.now();
    }

    if (!status || !TERMINAL_STATUSES[status]) {
      continue;
    }

    const clientFinishedAtMs = Date.now();
    const queuedAtMs = payload.queued_at ? Date.parse(payload.queued_at) : enqueueStartMs;
    const startedAtMs = payload.started_at
      ? Date.parse(payload.started_at)
      : (clientStartedAtMs || clientFinishedAtMs);
    const finishedAtMs = payload.finished_at ? Date.parse(payload.finished_at) : clientFinishedAtMs;

    const queueWaitMs = Math.max(0, startedAtMs - queuedAtMs);
    const execDurationMs = Math.max(0, finishedAtMs - startedAtMs);
    const e2eDurationMs = Math.max(0, finishedAtMs - queuedAtMs);

    migrationQueueWaitMs.add(queueWaitMs);
    migStageQueueWait[submitStage].add(queueWaitMs);
    migModeQueueWait[targetMode].add(queueWaitMs);

    migrationExecDurationMs.add(execDurationMs);
    migStageExecDuration[submitStage].add(execDurationMs);
    migModeExecDuration[targetMode].add(execDurationMs);

    migrationE2eDurationMs.add(e2eDurationMs);
    migStageE2eDuration[submitStage].add(e2eDurationMs);
    migModeE2eDuration[targetMode].add(e2eDurationMs);

    const succeeded = check(payload, {
      'migration terminal status is success': (p) => p.status === 'success',
    });

    if (succeeded) {
      migrationJobsSucceeded.add(1);
      migStageSucceeded[submitStage].add(1);
      migModeSucceeded[targetMode].add(1);

      migrationJobErrorRate.add(0);
      migStageErrorRate[submitStage].add(0);
      migModeErrorRate[targetMode].add(0);
    } else {
      console.warn(`Migration execution ${executionId} (${targetMode}, ${submitStage}) ended with status=${status}: ${payload.error_message || ''}`);
      recordTerminalFailure(submitStage, targetMode);
    }
    return;
  }

  console.warn(`Migration execution ${executionId} (${targetMode}, ${submitStage}) timed out after ${POLL_TIMEOUT_SEC}s`);
  migrationJobsTimedOut.add(1);
  recordTerminalFailure(submitStage, targetMode);
}

function metricValues(data, name) {
  const m = data.metrics && data.metrics[name];
  return (m && m.values) ? m.values : {};
}

function trendStats(data, name) {
  const v = metricValues(data, name);
  return {
    count: v.count || 0,
    avgMs: v.avg || 0,
    minMs: v.min || 0,
    p50Ms: v.med || 0,
    p90Ms: v['p(90)'] || 0,
    p95Ms: v['p(95)'] || 0,
    p99Ms: v['p(99)'] || 0,
    maxMs: v.max || 0,
  };
}

function countVal(data, name) {
  const v = metricValues(data, name);
  return v.count || 0;
}

function rateStats(data, name) {
  const v = metricValues(data, name);
  return {
    rate: v.rate || 0,
    passes: v.passes || 0,
    fails: v.fails || 0,
  };
}

function fmtMs(ms) {
  if (!ms) return '0.0 ms';
  if (ms >= 1000) return `${(ms / 1000).toFixed(2)} s`;
  return `${ms.toFixed(1)} ms`;
}

function fmtPct(rate) {
  return `${(rate * 100).toFixed(2)}%`;
}

export function handleSummary(data) {
  const activeRunSec = STAGE_SEC * 4;
  const activeRunMin = activeRunSec / 60;
  const stageMin = STAGE_SEC / 60;
  const totalWallSec = ((data.state && data.state.testRunDurationMs) || activeRunSec * 1000) / 1000;
  const totalWallMin = totalWallSec / 60;

  const totalHttpReqs = countVal(data, 'http_reqs');
  const httpReqsRate = metricValues(data, 'http_reqs').rate || (totalHttpReqs / activeRunSec);
  const httpErrRate = rateStats(data, 'http_req_failed').rate;

  const overallApiTrend = trendStats(data, 'api_req_duration');
  const overallApiCount = countVal(data, 'api_reqs');
  const overallApiRate = rateStats(data, 'api_error_rate').rate;

  const acceptedTotal = countVal(data, 'migration_enqueue_accepted');
  const succeededTotal = countVal(data, 'migration_jobs_succeeded');
  const failedTotal = countVal(data, 'migration_jobs_failed');
  const timedOutTotal = countVal(data, 'migration_jobs_timed_out');
  const jobErrRate = rateStats(data, 'migration_job_error_rate').rate;

  const enqueueTrend = trendStats(data, 'migration_enqueue_duration');
  const queueWaitTrend = trendStats(data, 'migration_queue_wait_ms');
  const execDurTrend = trendStats(data, 'migration_exec_duration_ms');
  const e2eDurTrend = trendStats(data, 'migration_e2e_duration_ms');

  const stageRows = {};
  for (const s of STAGES) {
    const apiTrend = trendStats(data, `api_duration_${s}`);
    const apiCount = countVal(data, `api_reqs_${s}`);
    const apiErr = rateStats(data, `api_error_rate_${s}`).rate;
    const acc = countVal(data, `mig_accepted_${s}`);
    const succ = countVal(data, `mig_succeeded_${s}`);
    const fail = countVal(data, `mig_failed_${s}`);
    const jErr = rateStats(data, `mig_job_error_rate_${s}`).rate;
    const qw = trendStats(data, `mig_queue_wait_${s}`);
    const ed = trendStats(data, `mig_exec_duration_${s}`);
    const e2e = trendStats(data, `mig_e2e_duration_${s}`);

    stageRows[s] = {
      durationSec: STAGE_SEC,
      api: {
        requests: apiCount,
        throughputRps: apiCount / STAGE_SEC,
        errorRate: apiErr,
        p50Ms: apiTrend.p50Ms,
        p95Ms: apiTrend.p95Ms,
        avgMs: apiTrend.avgMs,
        maxMs: apiTrend.maxMs,
      },
      migrations: {
        enqueueAccepted: acc,
        succeeded: succ,
        failed: fail,
        jobErrorRate: jErr,
        completionThroughputPerMin: succ / stageMin,
        completionThroughputPerSec: succ / STAGE_SEC,
        queueWaitMs: qw,
        execDurationMs: ed,
        e2eDurationMs: e2e,
      },
    };
  }

  const modeRows = {};
  for (const mode of ['same_db', 'diff_db']) {
    const acc = countVal(data, `mig_accepted_${mode}`);
    const succ = countVal(data, `mig_succeeded_${mode}`);
    const fail = countVal(data, `mig_failed_${mode}`);
    const jErr = rateStats(data, `mig_job_error_rate_${mode}`).rate;
    modeRows[mode] = {
      enqueueAccepted: acc,
      succeeded: succ,
      failed: fail,
      jobErrorRate: jErr,
      completionThroughputPerMin: succ / activeRunMin,
      completionThroughputPerSec: succ / activeRunSec,
      queueWaitMs: trendStats(data, `mig_queue_wait_${mode}`),
      execDurationMs: trendStats(data, `mig_exec_duration_${mode}`),
      e2eDurationMs: trendStats(data, `mig_e2e_duration_${mode}`),
    };
  }

  const thresholds = {};
  for (const [metricName, m] of Object.entries(data.metrics || {})) {
    if (m.thresholds) {
      thresholds[metricName] = m.thresholds;
    }
  }

  const report = {
    config: {
      quick: IS_QUICK,
      stageSec: STAGE_SEC,
      rampSec: RAMP_SEC,
      holdSec: HOLD_SEC,
      activeDurationSec: activeRunSec,
      totalWallSec,
      apiVUs: { normal: API_NORMAL_VUS, high: API_HIGH_VUS, stress: API_STRESS_VUS, recovery: API_RECOVERY_VUS },
      sameDbRpm: { normal: SAME_NORMAL_RPM, high: SAME_HIGH_RPM, stress: SAME_STRESS_RPM, recovery: SAME_RECOVERY_RPM },
      diffDbRpm: { normal: DIFF_NORMAL_RPM, high: DIFF_HIGH_RPM, stress: DIFF_STRESS_RPM, recovery: DIFF_RECOVERY_RPM },
    },
    overall: {
      totalHttpReqs,
      httpThroughputRps: httpReqsRate,
      httpErrorRate: httpErrRate,
      apiRequests: overallApiCount,
      apiThroughputRps: overallApiCount / activeRunSec,
      apiErrorRate: overallApiRate,
      apiDurationMs: overallApiTrend,
      migrationEnqueueDurationMs: enqueueTrend,
      migrationsEnqueueAccepted: acceptedTotal,
      migrationsSucceeded: succeededTotal,
      migrationsFailed: failedTotal,
      migrationsTimedOut: timedOutTotal,
      migrationJobErrorRate: jobErrRate,
      migrationThroughputActivePerMin: succeededTotal / activeRunMin,
      migrationThroughputWallPerMin: succeededTotal / totalWallMin,
      queueWaitMs: queueWaitTrend,
      execDurationMs: execDurTrend,
      e2eDurationMs: e2eDurTrend,
    },
    stages: stageRows,
    targetComparison: modeRows,
    thresholds,
  };

  const lines = [
    '',
    '================================================================================',
    '  BScheme k6 Stress Test Summary (API + Same-DB vs Diff-DB Pre-Approved Jobs)',
    '================================================================================',
    `Active stages duration : ${activeRunSec}s (${STAGE_SEC}s/stage, ramp ${RAMP_SEC}s + hold ${HOLD_SEC}s) | Total wall time: ${totalWallSec.toFixed(1)}s`,
    `Overall API requests   : ${overallApiCount} (${(overallApiCount / activeRunSec).toFixed(2)} req/s) | p50=${fmtMs(overallApiTrend.p50Ms)} | p95=${fmtMs(overallApiTrend.p95Ms)} | err=${fmtPct(overallApiRate)}`,
    `Overall HTTP requests  : ${totalHttpReqs} (${httpReqsRate.toFixed(2)} req/s) | http_req_failed=${fmtPct(httpErrRate)}`,
    `Migrations accepted    : ${acceptedTotal} (HTTP 202 enqueue, avg=${fmtMs(enqueueTrend.avgMs)})`,
    `Migrations terminal    : ${succeededTotal} success, ${failedTotal} failed (${timedOutTotal} timed out) | job_err=${fmtPct(jobErrRate)} | throughput=${(succeededTotal / activeRunMin).toFixed(2)} jobs/min`,
    `Migration queue wait   : p50=${fmtMs(queueWaitTrend.p50Ms)} | p95=${fmtMs(queueWaitTrend.p95Ms)} | max=${fmtMs(queueWaitTrend.maxMs)}`,
    `Migration exec dur     : p50=${fmtMs(execDurTrend.p50Ms)} | p95=${fmtMs(execDurTrend.p95Ms)} | max=${fmtMs(execDurTrend.maxMs)}`,
    '',
    '--- Per-Stage Breakdown ---',
    'Stage      | API req/s | API p50   | API p95   | API Err | Jobs Acc/OK | QWait p50 | QWait p95 | Exec p50  | Exec p95  | Jobs/min',
    '-----------+-----------+-----------+-----------+---------+-------------+-----------+-----------+-----------+-----------+---------',
  ];

  for (const s of STAGES) {
    const r = stageRows[s];
    const row = [
      s.padEnd(10),
      r.api.throughputRps.toFixed(1).padStart(9),
      fmtMs(r.api.p50Ms).padStart(9),
      fmtMs(r.api.p95Ms).padStart(9),
      fmtPct(r.api.errorRate).padStart(7),
      `${r.migrations.enqueueAccepted}/${r.migrations.succeeded}`.padStart(11),
      fmtMs(r.migrations.queueWaitMs.p50Ms).padStart(9),
      fmtMs(r.migrations.queueWaitMs.p95Ms).padStart(9),
      fmtMs(r.migrations.execDurationMs.p50Ms).padStart(9),
      fmtMs(r.migrations.execDurationMs.p95Ms).padStart(9),
      r.migrations.completionThroughputPerMin.toFixed(1).padStart(8),
    ].join(' | ');
    lines.push(row);
  }

  lines.push('');
  lines.push('--- Same DB vs Different DBs Comparison ---');
  lines.push('Mode     | Acc/OK/Fail  | Job Err | QWait p50 | QWait p95 | QWait Max | Exec p50  | Exec p95  | E2E p95   | Jobs/min');
  lines.push('---------+--------------+---------+-----------+-----------+-----------+-----------+-----------+-----------+---------');
  for (const mode of ['same_db', 'diff_db']) {
    const m = modeRows[mode];
    lines.push([
      mode.padEnd(8),
      `${m.enqueueAccepted}/${m.succeeded}/${m.failed}`.padStart(12),
      fmtPct(m.jobErrorRate).padStart(7),
      fmtMs(m.queueWaitMs.p50Ms).padStart(9),
      fmtMs(m.queueWaitMs.p95Ms).padStart(9),
      fmtMs(m.queueWaitMs.maxMs).padStart(9),
      fmtMs(m.execDurationMs.p50Ms).padStart(9),
      fmtMs(m.execDurationMs.p95Ms).padStart(9),
      fmtMs(m.e2eDurationMs.p95Ms).padStart(9),
      m.completionThroughputPerMin.toFixed(1).padStart(8),
    ].join(' | '));
  }

  lines.push('');
  lines.push('--- Thresholds ---');
  for (const [metricName, thMap] of Object.entries(thresholds)) {
    for (const [expr, res] of Object.entries(thMap)) {
      lines.push(`  [${res.ok ? 'PASS' : 'FAIL'}] ${metricName} :: ${expr}`);
    }
  }
  lines.push('================================================================================');
  lines.push('');

  return {
    stdout: lines.join('\n'),
    [SUMMARY_PATH]: JSON.stringify(report, null, 2),
  };
}
