const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

const projectRoot = path.resolve(__dirname, '..', '..');
const coreSource = fs.readFileSync(path.join(projectRoot, 'web/static/js/core.js'), 'utf8');
// backup.js를 역할별 모듈로 분할한 뒤에도 기존 테스트가 같은 전역 함수 집합을 보도록
// 실제 로드 순서(run-ui → run-tracking → run-results → verify → backup)로 이어 붙인다.
const backupSource = [
    'web/static/js/run-ui.js',
    'web/static/js/run-tracking.js',
    'web/static/js/run-results.js',
    'web/static/js/verify.js',
    'web/static/js/backup.js'
].map((file) => fs.readFileSync(path.join(projectRoot, file), 'utf8')).join('\n');
const userDataApiSource = fs.readFileSync(path.join(projectRoot, 'web/static/js/userdata-api.js'), 'utf8');

// 각 테스트가 만든 컨텍스트를 추적해, 테스트가 남긴 durable observer(무기한 폴링
// 루프)를 다음 테스트 전에 반드시 중단시킨다. token을 올리면 관찰자는 다음 회차의
// token 검사에서 스스로 종료한다. 이 정리가 없으면 종료 안 된 관찰자가 microtask를
// 독점해 이후 setImmediate 기반 테스트를 굶긴다.
const createdContexts = [];

test.afterEach(() => {
    for (const ctx of createdContexts) {
        try {
            vm.runInContext('statusConvergenceToken++; runningObserverToken = null;', ctx);
        } catch (_error) { /* 컨텍스트가 유효하지 않으면 무시 */ }
    }
    createdContexts.length = 0;
});

function createContext(sessionStorage = null, localStorage = null) {
    const elements = new Map();
    const getElement = (id) => {
        if (!elements.has(id)) {
            elements.set(id, {
                classList: { add() {}, remove() {} },
                disabled: id === 'cancelBtn',
                innerHTML: '',
                style: {},
                textContent: '',
                value: ''
            });
        }
        return elements.get(id);
    };

    const context = vm.createContext({
        WebSocket: { OPEN: 1 },
        addLogEntry() {},
        alert() {},
        cancelBackupRunOnServer: async () => ({ success: true, status: 200 }),
        getBackupRunStatusFromServer: async () => ({ success: true, status: 200, runStatus: 'idle', runId: null }),
        console,
        document: { getElementById: getElement },
        formatBytes: () => '',
        formatDuration: () => '',
        formatSpeed: () => '',
        // 테스트에서는 backoff를 즉시 실행해 상태 수렴 루프를 결정적으로 구동한다.
        setTimeout: (fn) => { fn(); return 0; },
        clearTimeout: () => {},
        window: {
            addEventListener() {},
            location: { host: 'localhost:8080', protocol: 'http:' },
            sessionStorage,
            localStorage
        }
    });

    vm.runInContext(coreSource, context);
    vm.runInContext(backupSource, context);
    createdContexts.push(context);
    return { context, getElement };
}

test('run configuration summary and dry-run action label follow the live form values', () => {
    const { context, getElement } = createContext();
    getElement('source').value = '/photos/source';
    getElement('dest').value = '/photos/dest';
    getElement('dateFilterStart').value = '2026-01-01';
    getElement('dateFilterEnd').value = '2026-01-31';
    getElement('dryRun').checked = true;

    vm.runInContext('updateRunConfigurationSummary(); updateRunActionLabels();', context);

    assert.match(getElement('runConfigurationSummary').textContent, /\/photos\/source → \/photos\/dest/);
    assert.match(getElement('runConfigurationSummary').textContent, /2026-01-01 ~ 2026-01-31/);
    assert.match(getElement('runConfigurationSummary').textContent, /확장자 \d+개 · 시뮬레이션/);
    assert.equal(getElement('startBtn').textContent, '시뮬레이션 시작');
    assert.equal(getElement('verifyBtn').textContent, '검증 시작');
    assert.equal(getElement('workspaceTitle').textContent, '새 백업');

    getElement('mode-verify').checked = true;
    vm.runInContext('updateRunActionLabels();', context);

    assert.equal(getElement('workspaceTitle').textContent, '백업 검증');
    assert.match(getElement('workspaceDescription').textContent, /백업 상태를 진단/);
});

test('run visual state locks the editable form while leaving the action area available', () => {
    const { context, getElement } = createContext();
    getElement('backupForm').inert = false;

    vm.runInContext("setRunVisualState('running');", context);

    assert.equal(getElement('backupForm').inert, true);
    assert.equal(getElement('startBtn').hidden, true);
    assert.equal(getElement('verifyBtn').hidden, true);
    assert.equal(getElement('cancelBtn').hidden, false);
    vm.runInContext("setRunVisualState('complete');", context);
    assert.equal(getElement('backupForm').inert, false);
    assert.equal(getElement('startBtn').hidden, false);
    assert.equal(getElement('verifyBtn').hidden, false);
    assert.equal(getElement('cancelBtn').hidden, true);
});

test('run readout and inline error use safe text-only status surfaces', () => {
    const { context, getElement } = createContext();
    getElement('runInlineError').id = 'runInlineError';

    vm.runInContext("updateProgressReadout({ current: 3, total: 8 }); reportRunMessage('<b>network</b>', 'error');", context);

    assert.equal(getElement('runReadout').textContent, 'SRC 8 · DONE 3 · 38%');
    assert.equal(getElement('runInlineError').textContent, '<b>network</b>');
    assert.equal(getElement('runInlineError').hidden, false);
});

test('cancel without WebSocket recovers via reconnect and status sync', async () => {
    const { context, getElement } = createContext();
    // 취소는 성공하지만 WebSocket이 없어 terminal 이벤트를 받을 수 없다.
    context.cancelBackupRunOnServer = async () => ({ success: true, status: 200, runId: 'run-1' });
    // 재연결은 실패하고, 상태 조회가 취소 완료를 알려 UI 잠김을 해소한다.
    vm.runInContext('connectWebSocket = async () => { throw new Error("no ws"); };', context);
    context.getBackupRunStatusFromServer = async () => ({
        success: true, status: 200, runStatus: 'cancelled', runId: 'run-1',
        message: '백업이 취소되었습니다.',
        summary: { Duration: 0, BytesCopied: 0, BytesPerSecond: 0, ScannedFiles: 0, TotalFiles: 0,
            Copied: 0, Skipped: 0, Renamed: 0, Overwritten: 0, Quarantined: 0, Failed: 0, Unclassified: 0 }
    });
    vm.runInContext(`beginTrackingRun('run-1'); runStartPending = false; runRequestSent = true; ws = null;`, context);
    getElement('startBtn').disabled = true;

    await vm.runInContext('cancelBackup()', context);

    // terminal 상태로 복구되어 UI 잠김이 풀려야 한다.
    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(getElement('startBtn').disabled, false);
    assert.equal(getElement('cancelBtn').disabled, true);
});

test('WebSocket close during cancellation does not mark run completed', () => {
    const { context, getElement } = createContext();
    vm.runInContext(`
        isRunning = true;
        runStartPending = false;
        runRequestSent = true;
        runCancelPending = false;
        runCancelRequested = true;
        ws = {};
    `, context);

    const onClose = vm.runInContext(`(() => {
        const backupMayStillBeRunning = runRequestSent || (!runStartPending && isRunning);
        const cancelInProgress = runCancelPending || runCancelRequested;
        return () => handleWebSocketClose({ code: 1006 }, backupMayStillBeRunning, cancelInProgress);
    })()`, context);
    onClose();

    assert.equal(vm.runInContext('isRunning', context), true);
    assert.equal(vm.runInContext('runCancelRequested', context), true);
    assert.equal(getElement('cancelBtn').disabled, true);
});

test('WebSocket disconnect keeps HTTP cancellation available for a running backup', () => {
    const { context, getElement } = createContext();
    // 관찰자가 붙기 전 동기 시점의 상태를 검사한다. 재연결/조회는 실패시켜 관찰자가
    // 즉시 폴링 대기로 들어가되(다음 async 회차) 이 테스트의 동기 단언에는 영향 없게 한다.
    vm.runInContext('connectWebSocket = async () => { throw new Error("no ws"); };', context);
    context.getBackupRunStatusFromServer = async () => ({ success: false, error: 'network' });
    vm.runInContext(`
        isRunning = true;
        runStatus = 'running';
        currentRunId = 'run-1';
        runStartPending = false;
        runRequestSent = true;
        ws = {};
        handleWebSocketClose({ code: 1006 }, true, false);
    `, context);

    assert.equal(vm.runInContext('isRunning', context), true);
    assert.equal(getElement('cancelBtn').disabled, false);
    // 관찰자 lease가 잡혀 있어야 한다(정상 실행 중 WS 종료 → terminal 관찰 시작).
    assert.notEqual(vm.runInContext('runningObserverToken', context), null);
    // 이 테스트가 남긴 관찰자를 정리해 다음 테스트를 굶기지 않는다.
    vm.runInContext('statusConvergenceToken++; runningObserverToken = null;', context);
});

test('terminal WebSocket event cannot be overwritten by a later cancel response', async () => {
    let resolveCancel;
    const { context, getElement } = createContext();
    context.cancelBackupRunOnServer = () => new Promise((resolve) => { resolveCancel = resolve; });
    vm.runInContext(`
        isRunning = true;
        runStatus = 'running';
        currentRunId = 'run-1';
        runStartPending = false;
        runRequestSent = true;
    `, context);

    const cancelPromise = vm.runInContext('cancelBackup()', context);
    vm.runInContext(`handleProgressUpdate({ type: 'cancelled', run_id: 'run-1', message: '백업이 취소되었습니다.' })`, context);
    resolveCancel({ success: true, status: 200, runId: 'run-1' });
    await cancelPromise;

    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(vm.runInContext('runCancelRequested', context), false);
    assert.equal(getElement('progressText').textContent, '백업이 취소되었습니다.');
});

test('progress events from a different run are ignored', () => {
    const { context, getElement } = createContext();
    vm.runInContext(`
        isRunning = true;
        runStatus = 'running';
        currentRunId = 'run-current';
        handleProgressUpdate({ type: 'status', run_id: 'run-old', message: 'wrong run' });
    `, context);

    assert.notEqual(getElement('progressText').textContent, 'wrong run');
    assert.equal(vm.runInContext('isRunning', context), true);
});

test('status synchronization restores a running backup after reload', async () => {
    const { context, getElement } = createContext();
    context.getBackupRunStatusFromServer = async () => ({
        success: true,
        status: 200,
        runStatus: 'running',
        runId: 'restored-run'
    });

    await vm.runInContext('synchronizeRunStatus()', context);

    assert.equal(vm.runInContext('isRunning', context), true);
    assert.equal(vm.runInContext('currentRunId', context), 'restored-run');
    assert.equal(getElement('startBtn').disabled, true);
    assert.equal(getElement('cancelBtn').disabled, false);
});

test('terminal event wins over an older in-flight status response', async () => {
    let resolveStatus;
    const { context } = createContext();
    context.getBackupRunStatusFromServer = () => new Promise((resolve) => { resolveStatus = resolve; });
    vm.runInContext(`
        isRunning = true;
        runStatus = 'running';
        currentRunId = 'run-1';
    `, context);

    const syncPromise = vm.runInContext('synchronizeRunStatus()', context);
    vm.runInContext(`handleProgressUpdate({ type: 'complete', run_id: 'run-1', summary: {
        Duration: 0, BytesCopied: 0, BytesPerSecond: 0, ScannedFiles: 0, TotalFiles: 0,
        Copied: 0, Skipped: 0, Renamed: 0, Overwritten: 0, Quarantined: 0, Failed: 0, Unclassified: 0
    } })`, context);
    resolveStatus({ success: true, status: 200, runStatus: 'running', runId: 'run-1' });
    await syncPromise;

    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(vm.runInContext('runStatus', context), 'idle');
});

test('fast completion cannot be overwritten by a later start response', async () => {
    let resolveStart;
    const { context, getElement } = createContext();
    context.startBackupRunOnServer = () => new Promise((resolve) => { resolveStart = resolve; });
    vm.runInContext('connectWebSocket = async () => { ws = { readyState: WebSocket.OPEN, close() {} }; };', context);
    getElement('source').value = '/source';
    getElement('dest').value = '/dest';

    const startPromise = vm.runInContext('startBackup()', context);
    await new Promise((resolve) => setImmediate(resolve));
    const runId = vm.runInContext('currentRunId', context);
    vm.runInContext(`handleProgressUpdate({ type: 'complete', run_id: ${JSON.stringify(runId)}, summary: {
        Duration: 0, BytesCopied: 0, BytesPerSecond: 0, ScannedFiles: 0, TotalFiles: 0,
        Copied: 0, Skipped: 0, Renamed: 0, Overwritten: 0, Quarantined: 0, Failed: 0, Unclassified: 0
    } })`, context);
    resolveStart({ success: true, status: 200, runId });
    await startPromise;

    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(vm.runInContext('runStatus', context), 'idle');
    assert.equal(getElement('progressText').textContent, '완료!');
    assert.equal(getElement('cancelBtn').disabled, true);
});

test('lost start response reconciles the accepted run by the same run ID', async () => {
    const { context, getElement } = createContext();
    context.startBackupRunOnServer = async () => ({ success: false, error: 'network lost' });
    vm.runInContext('connectWebSocket = async () => { ws = { readyState: WebSocket.OPEN, close() {} }; };', context);
    getElement('source').value = '/source';
    getElement('dest').value = '/dest';
    context.getBackupRunStatusFromServer = async () => ({
        success: true,
        status: 200,
        runStatus: 'running',
        runId: vm.runInContext('currentRunId', context),
        revision: 2
    });

    await vm.runInContext('startBackup()', context);

    assert.equal(vm.runInContext('isRunning', context), true);
    assert.equal(vm.runInContext('runStartPending', context), false);
    assert.equal(getElement('cancelBtn').disabled, false);
    assert.equal(getElement('progressText').textContent, '실행 중인 백업에 다시 연결되었습니다.');
});

test('unresolved start response loss preserves uncertain cancellable state', async () => {
    const { context, getElement } = createContext();
    context.startBackupRunOnServer = async () => ({ success: false, error: 'network lost' });
    context.getBackupRunStatusFromServer = async () => ({ success: false, error: 'still offline' });
    vm.runInContext('connectWebSocket = async () => { ws = { readyState: WebSocket.OPEN, close() {} }; };', context);
    getElement('source').value = '/source';
    getElement('dest').value = '/dest';

    await vm.runInContext('startBackup()', context);

    assert.equal(vm.runInContext('isRunning', context), true);
    assert.equal(vm.runInContext('runStartPending', context), false);
    assert.equal(vm.runInContext('runRequestSent', context), true);
    assert.equal(getElement('cancelBtn').disabled, false);
    assert.equal(getElement('progressText').textContent, '서버 응답 유실 - 실행 상태 확인 필요');
});

test('idle reconciliation confirms a lost start request was not accepted', async () => {
    let closeCount = 0;
    const { context, getElement } = createContext();
    context.startBackupRunOnServer = async () => ({ success: false, error: 'network lost' });
    context.getBackupRunStatusFromServer = async () => ({
        success: true,
        status: 200,
        runStatus: 'idle',
        runId: null,
        revision: 2
    });
    vm.runInContext('connectWebSocket = async () => { ws = { readyState: WebSocket.OPEN, close() { closeCount(); } }; };', context);
    context.closeCount = () => { closeCount++; };
    getElement('source').value = '/source';
    getElement('dest').value = '/dest';

    await vm.runInContext('startBackup()', context);

    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(vm.runInContext('currentRunId', context), null);
    assert.equal(getElement('cancelBtn').disabled, true);
    assert.match(getElement('progressText').textContent, /오류: 백업 시작 실패/);
    assert.equal(closeCount, 1);
});

test('an observed terminal from another run cannot leave a lost start pending', async () => {
    const values = new Map([['shutterpipe.lastObservedTerminal', 'server-1:old-run']]);
    const storage = {
        getItem(key) { return values.has(key) ? values.get(key) : null; },
        setItem(key, value) { values.set(key, String(value)); }
    };
    const { context, getElement } = createContext(storage);
    context.startBackupRunOnServer = async () => ({ success: false, error: 'network lost' });
    context.getBackupRunStatusFromServer = async () => ({
        success: true, status: 200, runStatus: 'complete', runId: 'old-run', serverId: 'server-1', revision: 8
    });
    vm.runInContext('connectWebSocket = async () => { ws = { readyState: WebSocket.OPEN, close() {} }; };', context);
    getElement('source').value = '/source';
    getElement('dest').value = '/dest';

    await vm.runInContext('startBackup()', context);

    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(vm.runInContext('runStartPending', context), false);
    assert.equal(vm.runInContext('currentRunId', context), null);
    assert.equal(getElement('startBtn').disabled, false);
    assert.equal(getElement('cancelBtn').disabled, true);
});

test('cancel 409 with another terminal clears the stale tracked run', async () => {
    const { context, getElement } = createContext();
    context.cancelBackupRunOnServer = async () => ({ success: false, status: 409, error: 'run mismatch' });
    context.getBackupRunStatusFromServer = async () => ({
        success: true, status: 200, runStatus: 'complete', runId: 'other-run', serverId: 'server-1', revision: 12,
        summary: { Duration: 0, BytesCopied: 0, BytesPerSecond: 0, ScannedFiles: 0, TotalFiles: 0,
            Copied: 0, Skipped: 0, Renamed: 0, Overwritten: 0, Quarantined: 0, Failed: 0, Unclassified: 0 }
    });
    vm.runInContext(`beginTrackingRun('stale-run'); runStartPending = false; runRequestSent = true;`, context);
    getElement('startBtn').disabled = true;

    await vm.runInContext('cancelBackup()', context);

    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(vm.runInContext('currentRunId', context), null);
    assert.equal(getElement('startBtn').disabled, false);
    assert.equal(getElement('cancelBtn').disabled, true);
});

test('cancel response revision rejects older progress updates', async () => {
    const { context, getElement } = createContext();
    context.cancelBackupRunOnServer = async () => ({
        success: true, status: 200, runStatus: 'cancelling', runId: 'run-1', serverId: 'server-1', revision: 10
    });
    // 연결된 상태에서 취소하는 시나리오이므로 OPEN socket을 둔다(수렴 폴링 경로가 아님).
    vm.runInContext(`beginTrackingRun('run-1'); runStartPending = false; runRequestSent = true; lastServerId = 'server-1'; ws = { readyState: 1, close() {} };`, context);

    await vm.runInContext('cancelBackup()', context);
    vm.runInContext(`handleProgressUpdate({ type: 'status', run_id: 'run-1', server_id: 'server-1', revision: 9, message: '복사 중' })`, context);

    assert.equal(vm.runInContext('lastServerRevision', context), 10);
    assert.equal(vm.runInContext('runStatus', context), 'cancelling');
    assert.equal(getElement('progressText').textContent, '취소 요청 중...');
});

test('terminal status snapshot restores completion after reload', async () => {
    const { context, getElement } = createContext();
    context.getBackupRunStatusFromServer = async () => ({
        success: true,
        status: 200,
        runStatus: 'complete',
        runId: 'completed-run',
        revision: 4,
        summary: {
            Duration: 0, BytesCopied: 0, BytesPerSecond: 0, ScannedFiles: 1, TotalFiles: 1,
            Copied: 1, Skipped: 0, Renamed: 0, Overwritten: 0, Quarantined: 0, Failed: 0, Unclassified: 0
        }
    });

    await vm.runInContext('synchronizeRunStatus()', context);

    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(vm.runInContext('terminalRunId', context), 'completed-run');
    assert.equal(getElement('progressText').textContent, '완료!');
    assert.equal(getElement('summarySection').hidden, false);
});

test('terminal reload recovery still completes when the optional progress bar is absent', async () => {
    const { context, getElement } = createContext();
    const originalGetElementById = context.document.getElementById;
    context.document.getElementById = (id) => id === 'progressBar' ? null : originalGetElementById(id);
    context.getBackupRunStatusFromServer = async () => ({
        success: true,
        status: 200,
        runStatus: 'complete',
        runId: 'completed-without-bar',
        revision: 5,
        summary: {
            Duration: 0, BytesCopied: 0, BytesPerSecond: 0, ScannedFiles: 1, TotalFiles: 1,
            Copied: 1, Skipped: 0, Renamed: 0, Overwritten: 0, Quarantined: 0, Failed: 0, Unclassified: 0
        }
    });

    await vm.runInContext('synchronizeRunStatus()', context);

    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(vm.runInContext('terminalRunId', context), 'completed-without-bar');
    assert.equal(getElement('summarySection').hidden, false);
});

test('terminal status snapshot restores cancellation after reload', async () => {
    const { context, getElement } = createContext();
    context.getBackupRunStatusFromServer = async () => ({
        success: true,
        status: 200,
        runStatus: 'cancelled',
        runId: 'cancelled-run',
        revision: 7
    });

    await vm.runInContext('synchronizeRunStatus()', context);

    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(vm.runInContext('terminalRunId', context), 'cancelled-run');
    assert.equal(getElement('progressText').textContent, '백업이 취소되었습니다.');
});

test('terminal status snapshot restores an error after reload', async () => {
    const { context, getElement } = createContext();
    context.getBackupRunStatusFromServer = async () => ({
        success: true,
        status: 200,
        runStatus: 'error',
        runId: 'failed-run',
        revision: 8,
        error: 'disk unavailable'
    });

    await vm.runInContext('synchronizeRunStatus()', context);

    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(vm.runInContext('terminalRunId', context), 'failed-run');
    assert.equal(getElement('progressText').textContent, '오류: disk unavailable');
});

test('an observed terminal snapshot is not replayed on the next reload', async () => {
    const values = new Map();
    const storage = {
        getItem(key) { return values.has(key) ? values.get(key) : null; },
        setItem(key, value) { values.set(key, String(value)); }
    };
    const terminalStatus = {
        success: true,
        status: 200,
        runStatus: 'error',
        runId: 'old-failed-run',
        serverId: 'server-terminal',
        revision: 8,
        error: 'old disk error'
    };

    const first = createContext(storage);
    first.context.getBackupRunStatusFromServer = async () => terminalStatus;
    await vm.runInContext('synchronizeRunStatus()', first.context);

    const second = createContext(storage);
    second.context.getBackupRunStatusFromServer = async () => terminalStatus;
    const result = await vm.runInContext('synchronizeRunStatus()', second.context);

    assert.doesNotMatch(backupSource, /alert\s*\(/);
    assert.equal(result.observed, true);
    assert.notEqual(second.getElement('progressText').textContent, '오류 발생');
});

test('status requested after terminal cannot restore the same run', async () => {
    const { context, getElement } = createContext();
    vm.runInContext(`handleProgressUpdate({ type: 'cancelled', run_id: 'run-1', message: 'terminal', revision: 5 })`, context);
    context.getBackupRunStatusFromServer = async () => ({
        success: true,
        status: 200,
        runStatus: 'running',
        runId: 'run-1',
        revision: 4
    });

    await vm.runInContext('synchronizeRunStatus()', context);

    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(vm.runInContext('terminalRunId', context), 'run-1');
    assert.equal(getElement('progressText').textContent, 'terminal');
});

test('late idle status cannot overwrite a terminal event', async () => {
    let resolveStatus;
    const { context, getElement } = createContext();
    context.getBackupRunStatusFromServer = () => new Promise((resolve) => { resolveStatus = resolve; });
    vm.runInContext(`
        beginTrackingRun('run-1');
        const pendingStatus = synchronizeRunStatus();
        handleProgressUpdate({ type: 'cancelled', run_id: 'run-1', message: 'terminal cancellation', revision: 5 });
        globalThis.pendingStatus = pendingStatus;
    `, context);
    resolveStatus({ success: true, status: 200, runStatus: 'idle', runId: null, revision: 4 });
    await vm.runInContext('pendingStatus', context);

    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(vm.runInContext('terminalRunId', context), 'run-1');
    assert.equal(getElement('progressText').textContent, 'terminal cancellation');
});

test('older server revision cannot move a running state to cancelling', async () => {
    const { context } = createContext();
    vm.runInContext(`beginTrackingRun('run-1'); lastServerRevision = 10;`, context);
    context.getBackupRunStatusFromServer = async () => ({
        success: true,
        status: 200,
        runStatus: 'cancelling',
        runId: 'run-1',
        revision: 9
    });

    await vm.runInContext('synchronizeRunStatus()', context);

    assert.equal(vm.runInContext('runStatus', context), 'running');
    assert.equal(vm.runInContext('lastServerRevision', context), 10);
});

test('a new run keeps the same-server revision watermark', () => {
    const { context } = createContext();
    vm.runInContext(`
        currentRunId = null;
        terminalRunId = 'old-run';
        lastServerRevision = 50;
        beginTrackingRun('new-run');
    `, context);

    assert.equal(vm.runInContext('lastServerRevision', context), 50);
    assert.equal(vm.runInContext('currentRunId', context), 'new-run');
});

test('a new server instance accepts a lower-revision WebSocket run', () => {
    const { context } = createContext();
    vm.runInContext(`
        beginTrackingRun('old-run');
        lastServerId = 'old-server';
        lastServerRevision = 50;
        handleProgressUpdate({
            type: 'status', run_id: 'new-run', server_id: 'new-server',
            revision: 1, message: 'new server run'
        });
    `, context);

    assert.equal(vm.runInContext('currentRunId', context), 'new-run');
    assert.equal(vm.runInContext('lastServerId', context), 'new-server');
    assert.equal(vm.runInContext('lastServerRevision', context), 1);
});

test('a new server instance accepts a lower-revision status snapshot', async () => {
    const { context } = createContext();
    vm.runInContext(`beginTrackingRun('old-run'); lastServerId = 'old-server'; lastServerRevision = 50;`, context);
    context.getBackupRunStatusFromServer = async () => ({
        success: true,
        status: 200,
        runStatus: 'running',
        runId: 'new-run',
        serverId: 'new-server',
        revision: 1
    });

    await vm.runInContext('synchronizeRunStatus()', context);

    assert.equal(vm.runInContext('currentRunId', context), 'new-run');
    assert.equal(vm.runInContext('lastServerRevision', context), 1);
});

test('an old status response cannot restore a previous server epoch', async () => {
    let resolveStatus;
    const { context } = createContext();
    context.getBackupRunStatusFromServer = () => new Promise((resolve) => { resolveStatus = resolve; });
    vm.runInContext(`
        beginTrackingRun('old-run');
        lastServerId = 'old-server';
        lastServerRevision = 50;
        globalThis.pendingOldStatus = synchronizeRunStatus();
        handleProgressUpdate({
            type: 'status', run_id: 'new-run', server_id: 'new-server',
            revision: 1, message: 'new server run'
        });
    `, context);
    resolveStatus({
        success: true,
        status: 200,
        runStatus: 'running',
        runId: 'old-run',
        serverId: 'old-server',
        revision: 50
    });

    await vm.runInContext('pendingOldStatus', context);

    assert.equal(vm.runInContext('lastServerId', context), 'new-server');
    assert.equal(vm.runInContext('currentRunId', context), 'new-run');
});

test('a delayed old-server WebSocket event cannot bounce the server epoch backward', () => {
    const { context } = createContext();
    vm.runInContext(`
        lastServerId = 'old-server';
        lastServerRevision = 50;
        handleProgressUpdate({
            type: 'status', run_id: 'new-run', server_id: 'new-server',
            revision: 1, message: 'new server run'
        });
        handleProgressUpdate({
            type: 'status', run_id: 'old-run', server_id: 'old-server',
            revision: 51, message: 'delayed old server event'
        });
    `, context);

    assert.equal(vm.runInContext('lastServerId', context), 'new-server');
    assert.equal(vm.runInContext('currentRunId', context), 'new-run');
    assert.equal(vm.runInContext('lastServerRevision', context), 1);
});

test('run API helpers preserve run IDs in requests and responses', async () => {
    const requests = [];
    const responses = [
        { status: 200, body: { status: 'started', kind: 'backup', run_id: 'run-start', server_id: 'server-1', revision: 6 } },
        { status: 200, body: { status: 'cancelling', kind: 'backup', run_id: 'run-start', server_id: 'server-1', revision: 7 } },
        {
            status: 200,
            body: {
                status: 'complete',
                kind: 'verify',
                run_id: 'run-start',
                server_id: 'server-1',
                summary: { Copied: 1 },
                verify_summary: { mode: 'quick', ok: 1 },
                revision: 8
            }
        }
    ];
    const apiContext = vm.createContext({
        console,
        document: { addEventListener() {}, readyState: 'loading' },
        fetch: async (url, options = {}) => {
            requests.push({ url, options });
            const response = responses.shift();
            return {
                headers: { get: () => 'application/json' },
                json: async () => response.body,
                ok: true,
                status: response.status,
                text: async () => ''
            };
        }
    });
    vm.runInContext(userDataApiSource, apiContext);

    const start = await vm.runInContext(`startBackupRunOnServer({ source: '/a', run_id: 'run-start' })`, apiContext);
    const cancel = await vm.runInContext(`cancelBackupRunOnServer('run-start', 'server-1')`, apiContext);
    const status = await vm.runInContext(`getBackupRunStatusFromServer('run-start')`, apiContext);

    assert.equal(start.runId, 'run-start');
    assert.equal(start.serverId, 'server-1');
    assert.equal(start.revision, 6);
    assert.equal(start.runKind, 'backup');
    assert.deepEqual(JSON.parse(requests[0].options.body), { source: '/a', run_id: 'run-start' });
    assert.equal(cancel.runId, 'run-start');
    assert.equal(cancel.runStatus, 'cancelling');
    assert.equal(cancel.revision, 7);
    assert.equal(cancel.runKind, 'backup');
    assert.deepEqual(JSON.parse(requests[1].options.body), { run_id: 'run-start', server_id: 'server-1' });
    assert.equal(status.runStatus, 'complete');
    assert.equal(status.runId, 'run-start');
    assert.deepEqual(status.summary, { Copied: 1 });
    assert.deepEqual(status.verifySummary, { mode: 'quick', ok: 1 });
    assert.equal(status.revision, 8);
    assert.equal(status.serverId, 'server-1');
    assert.equal(status.runKind, 'verify');
    assert.equal(requests[2].url, '/api/run/status?run_id=run-start');
});

test('verify API helpers upload multipart data and requeue by run identity', async () => {
    const requests = [];
    const responses = [
        {
            status: 200,
            body: {
                status: 'started',
                kind: 'verify',
                run_id: 'verify-run',
                server_id: 'server-1',
                revision: 9
            }
        },
        { status: 200, body: { applied: 2, skipped: 1 } }
    ];
    const apiContext = vm.createContext({
        console,
        document: { addEventListener() {}, readyState: 'loading' },
        File,
        FormData,
        fetch: async (url, options = {}) => {
            requests.push({ url, options });
            const response = responses.shift();
            return {
                headers: { get: () => 'application/json' },
                json: async () => response.body,
                ok: true,
                status: response.status,
                text: async () => ''
            };
        }
    });
    vm.runInContext(userDataApiSource, apiContext);
    apiContext.manifest = new File(['hash  *photo.jpg\n'], 'hashes.txt', { type: 'text/plain' });

    const start = await vm.runInContext(
        `startVerifyRunOnServer(
            { source: '/source', dest: '/dest', verify_mode: 'hash', run_id: 'verify-run' },
            manifest
        )`,
        apiContext
    );
    const requeue = await vm.runInContext(
        `requeueVerifyOnServer('verify-run', 'server-1')`,
        apiContext
    );

    assert.equal(start.runStatus, 'started');
    assert.equal(start.runId, 'verify-run');
    assert.equal(start.serverId, 'server-1');
    assert.equal(start.revision, 9);
    assert.equal(start.runKind, 'verify');
    assert.equal(requests[0].url, '/api/verify');
    assert.equal(requests[0].options.method, 'POST');
    assert.equal(requests[0].options.headers, undefined);
    assert.ok(requests[0].options.body instanceof FormData);
    assert.deepEqual(
        JSON.parse(requests[0].options.body.get('config')),
        { source: '/source', dest: '/dest', verify_mode: 'hash', run_id: 'verify-run' }
    );
    assert.equal(requests[0].options.body.get('hash_manifest').name, 'hashes.txt');

    assert.equal(requeue.success, true);
    assert.equal(requeue.applied, 2);
    assert.equal(requeue.skipped, 1);
    assert.equal(requests[1].url, '/api/verify/requeue');
    assert.equal(requests[1].options.headers['Content-Type'], 'application/json');
    assert.deepEqual(
        JSON.parse(requests[1].options.body),
        { run_id: 'verify-run', server_id: 'server-1' }
    );
});

test('cancel API helper refuses an ID-less request before fetch', async () => {
    let fetchCalled = false;
    const apiContext = vm.createContext({
        console,
        document: { addEventListener() {}, readyState: 'loading' },
        fetch: async () => {
            fetchCalled = true;
            throw new Error('must not fetch');
        }
    });
    vm.runInContext(userDataApiSource, apiContext);

    const result = await vm.runInContext('cancelBackupRunOnServer()', apiContext);

    assert.equal(result.success, false);
    assert.equal(result.status, 400);
    assert.equal(result.error, 'run_id is required');
	assert.equal(fetchCalled, false);
});

test('cancel API helper refuses a server-ID-less request before fetch', async () => {
    let fetchCalled = false;
    const apiContext = vm.createContext({
        console,
        document: { addEventListener() {}, readyState: 'loading' },
        fetch: async () => {
            fetchCalled = true;
            throw new Error('must not fetch');
        }
    });
    vm.runInContext(userDataApiSource, apiContext);

    const result = await vm.runInContext(`cancelBackupRunOnServer('run-start')`, apiContext);

    assert.equal(result.success, false);
    assert.equal(result.status, 400);
    assert.equal(result.error, 'server_id is required');
    assert.equal(fetchCalled, false);
});

test('run API helpers time out requests that never settle', async () => {
    class TestAbortController {
        constructor() {
            const listeners = [];
            this.signal = {
                aborted: false,
                addEventListener(type, listener) {
                    if (type === 'abort') listeners.push(listener);
                }
            };
            this.abort = () => {
                this.signal.aborted = true;
                for (const listener of listeners) listener();
            };
        }
    }
    const apiContext = vm.createContext({
        AbortController: TestAbortController,
        clearTimeout,
        console,
        document: { addEventListener() {}, readyState: 'loading' },
        fetch: (_url, options) => new Promise((_resolve, reject) => {
            options.signal.addEventListener('abort', () => {
                const error = new Error('aborted');
                error.name = 'AbortError';
                reject(error);
            });
        }),
        setTimeout
    });
    vm.runInContext(userDataApiSource, apiContext);
    vm.runInContext('runApiTimeoutMs = 5', apiContext);

    const results = await Promise.all([
        vm.runInContext(`startBackupRunOnServer({ run_id: 'start-timeout' })`, apiContext),
        vm.runInContext(`cancelBackupRunOnServer('cancel-timeout', 'server-1')`, apiContext),
        vm.runInContext('getBackupRunStatusFromServer()', apiContext)
    ]);

    for (const result of results) {
        assert.equal(result.success, false);
        assert.match(result.error, /요청 시간이 5ms를 초과/);
    }
});

test('initial status synchronization cannot clear a run started while WebSocket connects', async () => {
    let resolveConnect;
    let statusCalls = 0;
    const { context } = createContext();
    vm.runInContext(`connectWebSocket = () => new Promise((resolve) => { globalThis.resolveInitialConnect = resolve; });`, context);
    context.getBackupRunStatusFromServer = async () => {
        statusCalls++;
        return { success: true, status: 200, runStatus: 'idle', runId: null };
    };

    const initialization = vm.runInContext('initializeRunTracking()', context);
    vm.runInContext(`beginTrackingRun('user-started-run'); runStartPending = true;`, context);
    resolveConnect = context.resolveInitialConnect;
    resolveConnect();
    await initialization;

    assert.equal(statusCalls, 0);
    assert.equal(vm.runInContext('currentRunId', context), 'user-started-run');
    assert.equal(vm.runInContext('isRunning', context), true);
    assert.equal(vm.runInContext('runStartPending', context), true);
});

test('unscoped idle status cannot clear a pending start', async () => {
    const { context } = createContext();
    vm.runInContext(`beginTrackingRun('pending-run'); runStartPending = true;`, context);
    context.getBackupRunStatusFromServer = async () => ({
        success: true, status: 200, runStatus: 'idle', runId: null, revision: 1
    });

    await vm.runInContext('synchronizeRunStatus()', context);

    assert.equal(vm.runInContext('currentRunId', context), 'pending-run');
    assert.equal(vm.runInContext('isRunning', context), true);
    assert.equal(vm.runInContext('runStartPending', context), true);
});

test('late callbacks from an old WebSocket cannot clear the replacement socket', async () => {
    const sockets = [];
    let alerts = 0;
    const { context } = createContext();
    context.alert = () => { alerts++; };
    context.WebSocket = class FakeWebSocket {
        static OPEN = 1;
        constructor(url) {
            this.url = url;
            this.readyState = 0;
            sockets.push(this);
        }
        close() { this.readyState = 3; }
    };

    const firstConnect = vm.runInContext('connectWebSocket()', context);
    sockets[0].readyState = 1;
    sockets[0].onopen();
    await firstConnect;

    vm.runInContext('ws = null;', context);
    const secondConnect = vm.runInContext('connectWebSocket()', context);
    const secondPromise = vm.runInContext('wsConnectPromise', context);
    sockets[0].onerror(new Error('late old error'));
    sockets[0].onmessage({ data: JSON.stringify({ type: 'status', message: 'stale id-less event' }) });
    sockets[0].onclose({ code: 1000 });

    context.expectedSocket = sockets[1];
    assert.equal(vm.runInContext('ws === expectedSocket', context), true);
    assert.equal(vm.runInContext('wsConnectPromise', context), secondPromise);
    assert.equal(alerts, 0);

    sockets[1].readyState = 1;
    sockets[1].onopen();
    await secondConnect;
});

test('error terminal with a summary keeps partial results visible', () => {
    const { context, getElement } = createContext();
    vm.runInContext(`
        isRunning = true;
        runStatus = 'running';
        currentRunId = 'run-1';
        runStartPending = false;
        runRequestSent = true;
    `, context);
    // 실제로 처리된 파일 목록을 흉내낸다.
    getElement('fileList').innerHTML = '<div>[복사] a.jpg</div>';

    vm.runInContext(`handleProgressUpdate({ type: 'error', run_id: 'run-1', error: 'boom', summary: {
        Duration: 0, BytesCopied: 0, BytesPerSecond: 0, ScannedFiles: 100, TotalFiles: 100,
        Copied: 99, Skipped: 0, Renamed: 0, Overwritten: 0, Quarantined: 0, Failed: 1, Unclassified: 0
    } })`, context);

    // summary가 표시되고 처리 목록이 초기화되지 않아야 한다.
    assert.equal(getElement('summarySection').hidden, false);
    assert.equal(getElement('fileList').innerHTML, '<div>[복사] a.jpg</div>');
});

test('error terminal without a summary falls back to a reset UI', () => {
    const { context, getElement } = createContext();
    vm.runInContext(`
        isRunning = true;
        runStatus = 'running';
        currentRunId = 'run-1';
        runStartPending = false;
        runRequestSent = true;
    `, context);
    getElement('fileList').innerHTML = '<div>[복사] a.jpg</div>';

    vm.runInContext(`handleProgressUpdate({ type: 'error', run_id: 'run-1', error: 'boom' })`, context);

    // summary가 없으면 기존처럼 목록을 초기화한다.
    assert.equal(getElement('summarySection').hidden, true);
    assert.notEqual(getElement('fileList').innerHTML, '<div>[복사] a.jpg</div>');
});

test('409 start response synchronizes to the already-active run', async () => {
    const { context, getElement } = createContext();
    context.startBackupRunOnServer = async () => ({ success: false, status: 409, error: '다른 백업이 이미 실행 중입니다.' });
    context.getBackupRunStatusFromServer = async () => ({
        success: true,
        status: 200,
        runStatus: 'running',
        runId: 'active-run',
        revision: 3
    });
    vm.runInContext('connectWebSocket = async () => { ws = { readyState: WebSocket.OPEN, close() {} }; };', context);
    getElement('source').value = '/source';
    getElement('dest').value = '/dest';

    await vm.runInContext('startBackup()', context);

    assert.equal(vm.runInContext('isRunning', context), true);
    assert.equal(vm.runInContext('currentRunId', context), 'active-run');
    assert.equal(getElement('startBtn').disabled, true);
    assert.equal(getElement('cancelBtn').disabled, false);
    // WebSocket을 유지해 진행 이벤트를 계속 수신해야 한다.
    assert.notEqual(vm.runInContext('ws', context), null);
});

test('progress event from another tab restores the running UI', () => {
    const { context, getElement } = createContext();
    // 이 탭은 idle 상태에서 다른 탭이 시작한 실행의 진행 이벤트를 처음 받는다.
    assert.equal(getElement('startBtn').disabled, false);

    vm.runInContext(`handleProgressUpdate({
        type: 'analysis_progress', run_id: 'other-tab-run', current: 1, total: 5,
        message: '메타데이터 분석 중...'
    })`, context);

    assert.equal(vm.runInContext('isRunning', context), true);
    assert.equal(vm.runInContext('currentRunId', context), 'other-tab-run');
    assert.equal(getElement('startBtn').disabled, true);
    assert.equal(getElement('cancelBtn').disabled, false);
});

test('start 409 with failed status query keeps a conservative running state', async () => {
    const { context, getElement } = createContext();
    context.startBackupRunOnServer = async () => ({ success: false, status: 409, error: 'another run active' });
    context.getBackupRunStatusFromServer = async () => ({ success: false, error: 'offline' });
    vm.runInContext('connectWebSocket = async () => { ws = { readyState: WebSocket.OPEN, close() {} }; };', context);
    getElement('source').value = '/source';
    getElement('dest').value = '/dest';

    await vm.runInContext('startBackup()', context);

    // 409는 실행 중임을 확정하므로 idle로 되돌리지 않고 WebSocket을 유지한다.
    assert.equal(vm.runInContext('isRunning', context), true);
    assert.equal(getElement('startBtn').disabled, true);
    assert.notEqual(vm.runInContext('ws', context), null);
});

test('cancel 409 after server restart restores the start button despite epoch reset', async () => {
    const { context, getElement } = createContext();
    context.cancelBackupRunOnServer = async () => ({ success: false, status: 409, error: 'mismatch' });
    // 상태 조회가 새 server epoch(다른 serverId)를 보고하면 currentRunId가 초기화된다.
    context.getBackupRunStatusFromServer = async () => ({
        success: true, status: 200, runStatus: 'idle', runId: null, serverId: 'server-2'
    });
    vm.runInContext(`lastServerId = 'server-1'; beginTrackingRun('run-1'); runStartPending = false; runRequestSent = true;`, context);
    getElement('startBtn').disabled = true;

    await vm.runInContext('cancelBackup()', context);

    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(getElement('startBtn').disabled, false);
});

test('another tab progress restores UI even when start button is disabled by config', async () => {
    const { context, getElement } = createContext();
    // 경로 미입력 등으로 시작 버튼이 이미 비활성인 탭에서도 실행 UI가 복구되어야 한다.
    getElement('startBtn').disabled = true;

    vm.runInContext(`handleProgressUpdate({
        type: 'analysis_progress', run_id: 'other-tab-run', current: 1, total: 5, message: '메타데이터 분석 중...'
    })`, context);

    assert.equal(vm.runInContext('isRunning', context), true);
    assert.equal(getElement('cancelBtn').disabled, false);
});

test('progress during an in-flight cancel does not reset cancel-pending state', () => {
    const { context, getElement } = createContext();
    vm.runInContext(`
        isRunning = true;
        currentRunId = 'run-1';
        runStartPending = false;
        runRequestSent = true;
        runCancelPending = true;
        runCancelRequested = false;
    `, context);
    // 취소 HTTP 요청이 진행 중(취소 버튼 비활성)일 때 진행 이벤트가 도착한다.
    getElement('cancelBtn').disabled = true;

    vm.runInContext(`handleProgressUpdate({ type: 'analysis_progress', run_id: 'run-1', current: 1, total: 5, message: '분석 중' })`, context);

    // restoreRunningUI가 호출되어 runCancelPending을 초기화하거나 취소 버튼을 다시
    // 활성화하면 안 된다.
    assert.equal(vm.runInContext('runCancelPending', context), true);
    assert.equal(getElement('cancelBtn').disabled, true);
});

test('start 409 that resolves to idle restores the idle UI', async () => {
    const { context, getElement } = createContext();
    context.startBackupRunOnServer = async () => ({ success: false, status: 409, error: 'another run active' });
    // 409를 준 실행이 조회 시점엔 이미 끝나 서버가 idle을 보고한다.
    context.getBackupRunStatusFromServer = async () => ({ success: true, status: 200, runStatus: 'idle', runId: null });
    vm.runInContext('connectWebSocket = async () => { ws = { readyState: WebSocket.OPEN, close() {} }; };', context);
    getElement('source').value = '/source';
    getElement('dest').value = '/dest';

    await vm.runInContext('startBackup()', context);

    // 불확실한 running 상태로 고정되지 않고 idle UI로 복구되어야 한다.
    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(getElement('startBtn').disabled, false);
    assert.equal(getElement('cancelBtn').disabled, true);
});

test('start 409 with a terminal arriving during status query does not revive the run', async () => {
    let resolveStatus;
    const { context, getElement } = createContext();
    context.startBackupRunOnServer = async () => ({ success: false, status: 409, error: 'another run active' });
    context.getBackupRunStatusFromServer = () => new Promise((resolve) => { resolveStatus = resolve; });
    vm.runInContext('connectWebSocket = async () => { ws = { readyState: WebSocket.OPEN, close() {} }; };', context);
    getElement('source').value = '/source';
    getElement('dest').value = '/dest';

    const startPromise = vm.runInContext('startBackup()', context);
    await new Promise((resolve) => setImmediate(resolve));
    // 상태 조회 대기 중 활성 실행의 complete 이벤트가 도착한다(revision 변화 → stale).
    vm.runInContext(`handleProgressUpdate({ type: 'complete', run_id: 'active-run', summary: {
        Duration: 0, BytesCopied: 0, BytesPerSecond: 0, ScannedFiles: 0, TotalFiles: 0,
        Copied: 0, Skipped: 0, Renamed: 0, Overwritten: 0, Quarantined: 0, Failed: 0, Unclassified: 0
    } })`, context);
    // 늦게 도착한 status 응답
    resolveStatus({ success: true, status: 200, runStatus: 'idle', runId: null });
    await startPromise;

    // 완료된 실행이 currentRunId·WebSocket 없이 running으로 부활하면 안 된다.
    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(vm.runInContext('currentRunId', context), null);
    assert.equal(getElement('startBtn').disabled, false);
});

test('terminal observed in another tab is still rendered in this tab', async () => {
    const map = new Map();
    const localStorage = {
        getItem: (k) => (map.has(k) ? map.get(k) : null),
        setItem: (k, v) => { map.set(k, String(v)); }
    };
    localStorage.setItem('shutterpipe.lastObservedTerminal', 'server-1:run-1');
    const { context, getElement } = createContext(null, localStorage);
    context.getBackupRunStatusFromServer = async () => ({
        success: true, status: 200, runStatus: 'complete', runId: 'run-1', serverId: 'server-1', revision: 5,
        summary: { Duration: 0, BytesCopied: 0, BytesPerSecond: 0, ScannedFiles: 0, TotalFiles: 0,
            Copied: 0, Skipped: 0, Renamed: 0, Overwritten: 0, Quarantined: 0, Failed: 0, Unclassified: 0 }
    });
    vm.runInContext(`lastServerId = 'server-1'; beginTrackingRun('run-1'); runStartPending = false; runRequestSent = true;`, context);
    getElement('startBtn').disabled = true;

    await vm.runInContext('synchronizeRunStatus()', context);

    // localStorage의 다른 탭 marker는 이 탭의 terminal 결과를 숨기지 않아야 한다.
    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(vm.runInContext('currentRunId', context), null);
    assert.equal(getElement('startBtn').disabled, false);
    assert.equal(getElement('progressText').textContent, '완료!');
    assert.equal(getElement('summarySection').hidden, false);
});

test('cancel without WebSocket converges to terminal via bounded status polling', async () => {
    const { context, getElement } = createContext();
    let statusCallCount = 0;
    context.cancelBackupRunOnServer = async () => ({ success: true, status: 200, runId: 'run-1' });
    // 재연결은 실패한다 → bounded backoff 폴링으로 수렴해야 한다.
    vm.runInContext('connectWebSocket = async () => { throw new Error("no ws"); };', context);
    context.getBackupRunStatusFromServer = async () => {
        statusCallCount++;
        if (statusCallCount === 1) {
            // 첫 조회 시점엔 아직 취소 진행 중.
            return { success: true, status: 200, runStatus: 'cancelling', runId: 'run-1' };
        }
        // 이후 서버가 취소 완료로 전환.
        return {
            success: true, status: 200, runStatus: 'cancelled', runId: 'run-1',
            message: '백업이 취소되었습니다.',
            summary: { Duration: 0, BytesCopied: 0, BytesPerSecond: 0, ScannedFiles: 0, TotalFiles: 0,
                Copied: 0, Skipped: 0, Renamed: 0, Overwritten: 0, Quarantined: 0, Failed: 0, Unclassified: 0 }
        };
    };
    vm.runInContext(`beginTrackingRun('run-1'); runStartPending = false; runRequestSent = true; ws = null;`, context);
    getElement('startBtn').disabled = true;

    await vm.runInContext('cancelBackup()', context);

    // 단발 조회에 그치지 않고 terminal에 수렴해 UI 잠김이 풀려야 한다.
    assert.ok(statusCallCount >= 2, `expected >=2 status polls, got ${statusCallCount}`);
    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(getElement('startBtn').disabled, false);
    assert.equal(getElement('cancelBtn').disabled, true);
});

const zeroSummary = { Duration: 0, BytesCopied: 0, BytesPerSecond: 0, ScannedFiles: 0, TotalFiles: 0,
    Copied: 0, Skipped: 0, Renamed: 0, Overwritten: 0, Quarantined: 0, Failed: 0, Unclassified: 0 };

test('cancel with a failed non-open WebSocket object still converges via polling', async () => {
    const { context, getElement } = createContext();
    let statusCallCount = 0;
    context.cancelBackupRunOnServer = async () => ({ success: true, status: 200, runId: 'run-1' });
    // connectWebSocket 실패 시 onerror가 reject하지만 ws=null 정리는 이후 onclose에서
    // 하므로, CLOSED(readyState 3) socket 객체가 남는 실제 lifecycle을 모델링한다.
    vm.runInContext('connectWebSocket = async () => { ws = { readyState: 3, close() {} }; throw new Error("failed"); };', context);
    context.getBackupRunStatusFromServer = async () => {
        statusCallCount++;
        if (statusCallCount === 1) return { success: true, status: 200, runStatus: 'cancelling', runId: 'run-1' };
        return { success: true, status: 200, runStatus: 'cancelled', runId: 'run-1', message: 'x', summary: zeroSummary };
    };
    vm.runInContext(`beginTrackingRun('run-1'); runStartPending = false; runRequestSent = true; ws = null;`, context);
    getElement('startBtn').disabled = true;

    await vm.runInContext('cancelBackup()', context);

    // if(ws)로 오판하면 폴링을 건너뛴다. OPEN 여부로 판정해 수렴해야 한다.
    assert.ok(statusCallCount >= 2, `expected polling, got ${statusCallCount}`);
    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(getElement('startBtn').disabled, false);
});

test('WebSocket close during an active-socket cancel starts status convergence', async () => {
    const { context, getElement } = createContext();
    let statusCallCount = 0;
    context.cancelBackupRunOnServer = async () => ({ success: true, status: 200, runId: 'run-1' });
    context.getBackupRunStatusFromServer = async () => {
        statusCallCount++;
        return { success: true, status: 200, runStatus: 'cancelled', runId: 'run-1', message: 'x', summary: zeroSummary };
    };
    // 취소 시점엔 WebSocket이 열려 있어 즉시 폴링하지 않는다.
    vm.runInContext(`beginTrackingRun('run-1'); runStartPending = false; runRequestSent = true; ws = { readyState: 1, close() {} };`, context);

    await vm.runInContext('cancelBackup()', context);
    assert.equal(statusCallCount, 0, 'live socket이면 폴링하지 않아야 한다');

    // 이후 terminal 전에 연결이 끊긴다(onclose가 ws=null로 정리).
    vm.runInContext('ws = null;', context);
    vm.runInContext(`handleWebSocketClose({ code: 1006 }, true, true)`, context);
    // 백그라운드 수렴 루프가 terminal을 처리해 isRunning이 내려갈 때까지 microtask flush.
    for (let i = 0; i < 50 && vm.runInContext('isRunning', context); i++) {
        await new Promise((resolve) => setImmediate(resolve));
    }

    assert.ok(statusCallCount >= 1, 'close 후 상태 수렴 폴링이 시작되어야 한다');
    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(getElement('startBtn').disabled, false);
});

test('durable observer keeps polling past the old bound until terminal', async () => {
    const { context, getElement } = createContext();
    let statusCallCount = 0;
    context.cancelBackupRunOnServer = async () => ({ success: true, status: 200, runId: 'run-1' });
    vm.runInContext('connectWebSocket = async () => { throw new Error("no ws"); };', context);
    // 정상 백업/취소가 오래 걸려 예전 6회 bound를 넘어도 관찰자가 포기하지 않아야 한다.
    // 10회까지 진행 중, 11회째 terminal.
    context.getBackupRunStatusFromServer = async () => {
        statusCallCount++;
        if (statusCallCount <= 10) {
            return { success: true, status: 200, runStatus: 'cancelling', runId: 'run-1' };
        }
        return { success: true, status: 200, runStatus: 'cancelled', runId: 'run-1', message: 'x', summary: zeroSummary };
    };
    vm.runInContext(`beginTrackingRun('run-1'); runStartPending = false; runRequestSent = true; ws = null;`, context);
    getElement('startBtn').disabled = true;

    await vm.runInContext('cancelBackup()', context);

    // 예전 구현이면 6회에서 멈춰 잠겼다. durable observer는 terminal까지 유지한다.
    assert.ok(statusCallCount >= 11, `expected >=11 polls, got ${statusCallCount}`);
    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(getElement('startBtn').disabled, false);
});

test('durable observer recovers after a transient outage instead of giving up', async () => {
    const { context, getElement } = createContext();
    let statusCallCount = 0;
    context.cancelBackupRunOnServer = async () => ({ success: true, status: 200, runId: 'run-1' });
    vm.runInContext('connectWebSocket = async () => { throw new Error("no ws"); };', context);
    // 일시적 장애: 7회 조회 실패(예전 5회 give-up을 넘김) → 이후 복구되어 terminal.
    // 영구 포기하지 않고 저빈도로 계속 재시도해 결국 완료를 관찰해야 한다.
    context.getBackupRunStatusFromServer = async () => {
        statusCallCount++;
        if (statusCallCount <= 7) return { success: false, error: 'network' };
        if (statusCallCount === 8) return { success: true, status: 200, runStatus: 'cancelling', runId: 'run-1' };
        return { success: true, status: 200, runStatus: 'cancelled', runId: 'run-1', message: 'x', summary: zeroSummary };
    };
    vm.runInContext(`beginTrackingRun('run-1'); runStartPending = false; runRequestSent = true; ws = null;`, context);
    getElement('startBtn').disabled = true;

    await vm.runInContext('cancelBackup()', context);

    // 예전 구현이면 5회에서 멈춰 잠겼다. 이제는 복구 후 terminal까지 관찰한다.
    assert.ok(statusCallCount >= 9, `expected recovery past the old 5-failure bound, got ${statusCallCount}`);
    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(getElement('startBtn').disabled, false);
});

test('observer reconnect failure does not spawn a competing observer (single lease)', async () => {
    const { context, getElement } = createContext();
    let statusCallCount = 0;
    context.cancelBackupRunOnServer = async () => ({ success: true, status: 200, runId: 'run-1' });
    // 실제 lifecycle: 재연결 실패 시 onclose가 발생한다. 이를 handleWebSocketClose 호출로
    // 모델링한다. lease 가드가 없으면 여기서 새 관찰자가 스폰되어 token/backoff가 리셋되고
    // 관찰자가 폭주한다. 가드가 있으면 기존 관찰자가 유일 소유자로 유지된다.
    vm.runInContext(`connectWebSocket = async () => {
        ws = null;
        handleWebSocketClose({ code: 1006 }, true, true);
        throw new Error('reconnect failed');
    };`, context);
    context.getBackupRunStatusFromServer = async () => {
        statusCallCount++;
        if (statusCallCount <= 2) return { success: true, status: 200, runStatus: 'cancelling', runId: 'run-1' };
        return { success: true, status: 200, runStatus: 'cancelled', runId: 'run-1', message: 'x', summary: zeroSummary };
    };
    vm.runInContext(`beginTrackingRun('run-1'); runStartPending = false; runRequestSent = true; ws = null;`, context);
    getElement('startBtn').disabled = true;

    await vm.runInContext('cancelBackup()', context);

    // 관찰자는 정확히 한 번만 시작되어야 한다(token 0→1). 폭주하면 token이 커진다.
    assert.equal(vm.runInContext('statusConvergenceToken', context), 1, 'single observer lease여야 한다');
    assert.equal(vm.runInContext('runningObserverToken', context), null, 'terminal 후 lease가 해제되어야 한다');
    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(getElement('startBtn').disabled, false);
});

test('cancel response loss does not lock out re-cancel when the run is still running', async () => {
    const { context, getElement } = createContext();
    // status 없는 네트워크 실패 + WebSocket은 살아 있음. POST가 도달하지 못해 서버가
    // 계속 running일 수 있으므로 취소를 확정하면 안 되고 재취소가 가능해야 한다(P1-a).
    let cancelAttempts = 0;
    context.cancelBackupRunOnServer = async () => {
        cancelAttempts++;
        if (cancelAttempts === 1) return { success: false, error: 'network' };
        return { success: true, status: 200, runId: 'run-1', runStatus: 'cancelling' };
    };
    vm.runInContext(`beginTrackingRun('run-1'); runStartPending = false; runRequestSent = true; ws = { readyState: 1, close() {} };`, context);

    await vm.runInContext('cancelBackup()', context);

    // 낙관적 확정 금지: 의도는 확정되지 않고 취소 버튼은 다시 열려 있어야 한다.
    assert.equal(vm.runInContext('runCancelRequested', context), false, '취소를 확정하면 안 된다');
    assert.equal(vm.runInContext('runCancelPending', context), false);
    assert.equal(getElement('cancelBtn').disabled, false, '재취소가 가능해야 한다');
    assert.equal(vm.runInContext('isRunning', context), true);

    // 재취소는 이번엔 성공한다 → 취소 확정.
    await vm.runInContext('cancelBackup()', context);
    assert.equal(vm.runInContext('runCancelRequested', context), true, '재취소가 취소를 확정해야 한다');
});

test('cancel convergence hands off observation to a newly adopted active run', async () => {
    const { context, getElement } = createContext();
    context.cancelBackupRunOnServer = async () => ({ success: true, status: 200, runId: 'run-1' });
    vm.runInContext('connectWebSocket = async () => { throw new Error("no ws"); };', context);
    // 폴링 1~2회: run-2가 활성(scoped mismatch → unscoped 채택). 3회부터: run-2 완료.
    // 채택 후 소켓이 없으면 run-2의 terminal을 받을 경로가 없어 시작 버튼이 잠기던 P1.
    let statusCallCount = 0;
    context.getBackupRunStatusFromServer = async () => {
        statusCallCount++;
        if (statusCallCount <= 2) {
            return { success: true, status: 200, runStatus: 'running', runId: 'run-2' };
        }
        return { success: true, status: 200, runStatus: 'complete', runId: 'run-2', message: 'x', summary: zeroSummary };
    };
    vm.runInContext(`beginTrackingRun('run-1'); runStartPending = false; runRequestSent = true; ws = null;`, context);
    getElement('startBtn').disabled = true;

    await vm.runInContext('cancelBackup()', context);
    // run-2 수렴은 fire-and-forget이므로 terminal까지 microtask flush.
    for (let i = 0; i < 50 && vm.runInContext('isRunning', context); i++) {
        await new Promise((resolve) => setImmediate(resolve));
    }

    // run-2가 완료로 수렴해 실행 상태가 정리되고 시작 버튼이 열려야 한다.
    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(vm.runInContext('terminalRunId', context), 'run-2');
    assert.equal(getElement('startBtn').disabled, false);
});

test('WebSocket close while a cancel request is pending does not reopen the cancel button', async () => {
    const { context, getElement } = createContext();
    let resolveCancel;
    const cancelPromise = new Promise((resolve) => { resolveCancel = resolve; });
    context.cancelBackupRunOnServer = () => cancelPromise;
    vm.runInContext('connectWebSocket = async () => { throw new Error("no ws"); };', context);
    let statusCallCount = 0;
    context.getBackupRunStatusFromServer = async () => {
        statusCallCount++;
        return { success: true, status: 200, runStatus: 'cancelled', runId: 'run-1', message: 'x', summary: zeroSummary };
    };
    vm.runInContext(`beginTrackingRun('run-1'); runStartPending = false; runRequestSent = true; ws = { readyState: 1, close() {} };`, context);

    // 취소 요청 시작(응답이 오기 전까지 runCancelPending 유지).
    const cancelDone = vm.runInContext('cancelBackup()', context);
    await new Promise((resolve) => setImmediate(resolve));
    assert.equal(vm.runInContext('runCancelPending', context), true);

    // 응답 전에 WebSocket close. pending 중 close는 수렴을 시작하면 안 된다.
    vm.runInContext('ws = null;', context);
    vm.runInContext(`handleWebSocketClose({ code: 1006 }, true, true)`, context);
    await new Promise((resolve) => setImmediate(resolve));

    assert.equal(statusCallCount, 0, 'pending 중 close는 폴링을 시작하지 않아야 한다');
    assert.equal(vm.runInContext('runCancelPending', context), true, '취소 pending이 유지되어야 한다');
    assert.equal(getElement('cancelBtn').disabled, true, '취소 버튼이 다시 열려서는 안 된다');

    // 응답 도착 → 요청 자신이 수렴을 담당해 terminal까지 처리.
    resolveCancel({ success: true, status: 200, runId: 'run-1' });
    await cancelDone;
    for (let i = 0; i < 50 && vm.runInContext('isRunning', context); i++) {
        await new Promise((resolve) => setImmediate(resolve));
    }
    assert.ok(statusCallCount >= 1, '요청 settle 후 수렴 폴링이 시작되어야 한다');
    assert.equal(vm.runInContext('isRunning', context), false);
});

test('cancel response loss keeps cancel intent and observes to terminal', async () => {
    const { context, getElement } = createContext();
    // status 없는 네트워크 실패: POST가 서버에 도달해 취소가 시작됐을 수 있다.
    context.cancelBackupRunOnServer = async () => ({ success: false, error: 'network' });
    vm.runInContext('connectWebSocket = async () => { throw new Error("no ws"); };', context);
    let statusCallCount = 0;
    context.getBackupRunStatusFromServer = async () => {
        statusCallCount++;
        if (statusCallCount === 1) return { success: true, status: 200, runStatus: 'cancelling', runId: 'run-1' };
        return { success: true, status: 200, runStatus: 'cancelled', runId: 'run-1', message: 'x', summary: zeroSummary };
    };
    vm.runInContext(`beginTrackingRun('run-1'); runStartPending = false; runRequestSent = true; ws = null;`, context);
    getElement('startBtn').disabled = true;

    await vm.runInContext('cancelBackup()', context);

    // 확정 실패로 보고 의도를 버리는 대신, cancelling으로 확정하고 terminal까지 관찰.
    assert.ok(statusCallCount >= 2, `expected observation polling, got ${statusCallCount}`);
    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(getElement('startBtn').disabled, false);
});

test('restoreRunningUI preserves an active cancel intent on a running snapshot', () => {
    const { context, getElement } = createContext();
    // 취소가 확정된 상태에서 서버가 아직 running을 보고해도 취소 의도를 보존해야 한다.
    vm.runInContext(`beginTrackingRun('run-1'); runCancelRequested = true; runCancelPending = false;`, context);
    getElement('cancelBtn').disabled = false;

    vm.runInContext(`restoreRunningUI('running')`, context);

    assert.equal(vm.runInContext('runCancelRequested', context), true, '취소 의도가 유지되어야 한다');
    assert.equal(vm.runInContext('runCancelPending', context), false);
    assert.equal(getElement('cancelBtn').disabled, true, '취소 버튼이 다시 열려서는 안 된다');
    assert.equal(getElement('progressText').textContent, '취소 요청 중...');
});

test('observer receiving a running snapshot mid-cancel does not reopen the cancel button', async () => {
    const { context, getElement } = createContext();
    context.cancelBackupRunOnServer = async () => ({ success: true, status: 200, runId: 'run-1' });
    vm.runInContext('connectWebSocket = async () => { throw new Error("no ws"); };', context);
    let statusCallCount = 0;
    // 취소 확정 후에도 서버는 아직 running(취소 처리 중 파일 복사 진행) → cancelled.
    context.getBackupRunStatusFromServer = async () => {
        statusCallCount++;
        if (statusCallCount <= 2) return { success: true, status: 200, runStatus: 'running', runId: 'run-1' };
        return { success: true, status: 200, runStatus: 'cancelled', runId: 'run-1', message: 'x', summary: zeroSummary };
    };
    vm.runInContext(`beginTrackingRun('run-1'); runStartPending = false; runRequestSent = true; ws = null;`, context);
    getElement('startBtn').disabled = true;

    await vm.runInContext('cancelBackup()', context);

    // running snapshot이 restoreRunningUI를 거쳐도 취소 의도가 유지되고, 결국 terminal.
    assert.ok(statusCallCount >= 3, `expected observation polling, got ${statusCallCount}`);
    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(getElement('startBtn').disabled, false);
});

test('initial WebSocket failure adopts the active run over HTTP and converges to terminal', async () => {
    const { context, getElement } = createContext();
    // 페이지 로드 시 WebSocket 연결 실패 → HTTP로 active run 채택 → 관찰자가 terminal까지.
    vm.runInContext('connectWebSocket = async () => { throw new Error("no ws"); };', context);
    let statusCallCount = 0;
    context.getBackupRunStatusFromServer = async () => {
        statusCallCount++;
        if (statusCallCount <= 2) return { success: true, status: 200, runStatus: 'running', runId: 'run-A' };
        return { success: true, status: 200, runStatus: 'complete', runId: 'run-A', message: 'x', summary: zeroSummary };
    };
    getElement('startBtn').disabled = false;

    await vm.runInContext('initializeRunTracking()', context);
    for (let i = 0; i < 50 && vm.runInContext('isRunning', context); i++) {
        await new Promise((resolve) => setImmediate(resolve));
    }

    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(vm.runInContext('terminalRunId', context), 'run-A');
    assert.equal(getElement('startBtn').disabled, false);
});

test('WebSocket close during a normal run converges to terminal', async () => {
    const { context, getElement } = createContext();
    vm.runInContext('connectWebSocket = async () => { throw new Error("no ws"); };', context);
    let statusCallCount = 0;
    context.getBackupRunStatusFromServer = async () => {
        statusCallCount++;
        if (statusCallCount <= 2) return { success: true, status: 200, runStatus: 'running', runId: 'run-1' };
        return { success: true, status: 200, runStatus: 'complete', runId: 'run-1', message: 'x', summary: zeroSummary };
    };
    vm.runInContext(`beginTrackingRun('run-1'); runStartPending = false; runRequestSent = true; ws = null;`, context);
    getElement('startBtn').disabled = true;

    // 취소가 아닌 정상 실행 중 연결 종료 → 관찰자가 terminal까지 폴링해야 한다.
    vm.runInContext(`handleWebSocketClose({ code: 1006 }, true, false)`, context);
    for (let i = 0; i < 50 && vm.runInContext('isRunning', context); i++) {
        await new Promise((resolve) => setImmediate(resolve));
    }

    assert.ok(statusCallCount >= 1, '연결 종료 후 상태 폴링이 시작되어야 한다');
    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(getElement('startBtn').disabled, false);
});

test('cancel 409 adopts a successor run without WebSocket and converges to terminal', async () => {
    const { context, getElement } = createContext();
    context.cancelBackupRunOnServer = async () => ({ success: false, status: 409, error: 'conflict' });
    vm.runInContext('connectWebSocket = async () => { throw new Error("no ws"); };', context);
    let statusCallCount = 0;
    context.getBackupRunStatusFromServer = async () => {
        statusCallCount++;
        if (statusCallCount <= 3) return { success: true, status: 200, runStatus: 'running', runId: 'run-2' };
        return { success: true, status: 200, runStatus: 'complete', runId: 'run-2', message: 'x', summary: zeroSummary };
    };
    vm.runInContext(`beginTrackingRun('run-1'); runStartPending = false; runRequestSent = true; ws = null;`, context);
    getElement('startBtn').disabled = true;

    await vm.runInContext('cancelBackup()', context);
    for (let i = 0; i < 50 && vm.runInContext('isRunning', context); i++) {
        await new Promise((resolve) => setImmediate(resolve));
    }

    // successor run-2를 채택하고, 소켓이 없어도 관찰자가 terminal까지 유지해야 한다.
    assert.equal(vm.runInContext('terminalRunId', context), 'run-2');
    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(getElement('startBtn').disabled, false);
});

test('uncertain start with an open WebSocket converges when the server is idle', async () => {
    const { context, getElement } = createContext();
    // WebSocket은 열려 있지만 시작 POST가 서버에 도달하지 못한 경우: 서버는 idle이라
    // WebSocket 이벤트가 오지 않는다. HTTP 수렴으로 idle을 확정해 UI를 풀어야 한다(P1).
    vm.runInContext('connectWebSocket = async () => { ws = { readyState: WebSocket.OPEN, close() {} }; };', context);
    context.startBackupRunOnServer = async () => ({ success: false, error: 'network' }); // status 없음
    let statusCallCount = 0;
    context.getBackupRunStatusFromServer = async () => {
        statusCallCount++;
        if (statusCallCount === 1) return { success: false, error: 'network' }; // 최초 조회 실패 → synthetic
        return { success: true, status: 200, runStatus: 'idle', runId: null }; // POST 미도달 → 서버 idle
    };
    getElement('source').value = '/s';
    getElement('dest').value = '/d';

    await vm.runInContext('startBackup()', context);
    for (let i = 0; i < 50 && vm.runInContext('isRunning', context); i++) {
        await new Promise((resolve) => setImmediate(resolve));
    }

    assert.ok(statusCallCount >= 2, `expected HTTP convergence, got ${statusCallCount}`);
    assert.equal(vm.runInContext('isRunning', context), false);
    assert.equal(getElement('startBtn').disabled, false);
});

test('cross-tab terminal marker does not suppress a 409 recovery result', async () => {
    const map = new Map();
    const localStorage = {
        getItem: (k) => (map.has(k) ? map.get(k) : null),
        setItem: (k, v) => { map.set(k, String(v)); }
    };
    localStorage.setItem('shutterpipe.lastObservedTerminal', 'server-1:run-A');
    const { context, getElement } = createContext(null, localStorage);
    context.getBackupRunStatusFromServer = async () => ({
        success: true, status: 200, runStatus: 'complete', runId: 'run-A', serverId: 'server-1', revision: 5,
        summary: zeroSummary
    });
    // 409가 requested run을 정리한 직후 상태: 추적 run 없음(currentRunId=null), 시작 버튼 잠김.
    vm.runInContext(`lastServerId = 'server-1'; currentRunId = null; isRunning = false; runStartPending = false;`, context);
    getElement('startBtn').disabled = true;

    await vm.runInContext('synchronizeRunStatus(null, { observe: true })', context);

    // 다른 탭 marker와 무관하게 이 탭도 terminal을 처리하고 idle UI를 복원해야 한다.
    assert.equal(getElement('startBtn').disabled, false);
    assert.equal(vm.runInContext('currentRunId', context), null);
    assert.equal(getElement('progressText').textContent, '완료!');
});
