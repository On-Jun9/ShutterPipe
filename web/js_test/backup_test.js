const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

const projectRoot = path.resolve(__dirname, '..', '..');
const coreSource = fs.readFileSync(path.join(projectRoot, 'web/static/js/core.js'), 'utf8');
const backupSource = fs.readFileSync(path.join(projectRoot, 'web/static/js/backup.js'), 'utf8');
const userDataApiSource = fs.readFileSync(path.join(projectRoot, 'web/static/js/userdata-api.js'), 'utf8');

function createContext(localStorage = null) {
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
        window: {
            addEventListener() {},
            location: { host: 'localhost:8080', protocol: 'http:' },
            localStorage
        }
    });

    vm.runInContext(coreSource, context);
    vm.runInContext(backupSource, context);
    return { context, getElement };
}

test('cancel request without WebSocket keeps run state uncertain', async () => {
    const { context, getElement } = createContext();
    vm.runInContext('isRunning = true; runStartPending = false; runRequestSent = true; ws = null;', context);
    getElement('startBtn').disabled = true;

    await vm.runInContext('cancelBackup()', context);

    assert.equal(vm.runInContext('isRunning', context), true);
    assert.equal(vm.runInContext('runCancelRequested', context), true);
    assert.equal(getElement('startBtn').disabled, true);
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
    let alertMessage = '';
    const { context, getElement } = createContext();
    context.alert = (message) => { alertMessage = message; };
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
    assert.match(alertMessage, /백업 시작 실패/);
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
    vm.runInContext(`beginTrackingRun('run-1'); runStartPending = false; runRequestSent = true; lastServerId = 'server-1';`, context);

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
    assert.equal(getElement('summarySection').style.display, 'block');
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
    let alertMessage = '';
    const { context, getElement } = createContext();
    context.alert = (message) => { alertMessage = message; };
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
    assert.equal(getElement('progressText').textContent, '오류 발생');
    assert.equal(alertMessage, '오류: disk unavailable');
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

    let firstAlerts = 0;
    const first = createContext(storage);
    first.context.alert = () => { firstAlerts++; };
    first.context.getBackupRunStatusFromServer = async () => terminalStatus;
    await vm.runInContext('synchronizeRunStatus()', first.context);

    let secondAlerts = 0;
    const second = createContext(storage);
    second.context.alert = () => { secondAlerts++; };
    second.context.getBackupRunStatusFromServer = async () => terminalStatus;
    const result = await vm.runInContext('synchronizeRunStatus()', second.context);

    assert.equal(firstAlerts, 1);
    assert.equal(secondAlerts, 0);
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
        { status: 200, body: { status: 'started', run_id: 'run-start', server_id: 'server-1' } },
        { status: 200, body: { status: 'cancelling', run_id: 'run-start', server_id: 'server-1', revision: 7 } },
        { status: 200, body: { status: 'complete', run_id: 'run-start', server_id: 'server-1', summary: { Copied: 1 }, revision: 8 } }
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
    const cancel = await vm.runInContext(`cancelBackupRunOnServer('run-start')`, apiContext);
    const status = await vm.runInContext('getBackupRunStatusFromServer()', apiContext);

    assert.equal(start.runId, 'run-start');
	assert.equal(start.serverId, 'server-1');
    assert.deepEqual(JSON.parse(requests[0].options.body), { source: '/a', run_id: 'run-start' });
    assert.equal(cancel.runId, 'run-start');
	assert.equal(cancel.runStatus, 'cancelling');
	assert.equal(cancel.revision, 7);
    assert.deepEqual(JSON.parse(requests[1].options.body), { run_id: 'run-start' });
    assert.equal(status.runStatus, 'complete');
    assert.equal(status.runId, 'run-start');
    assert.deepEqual(status.summary, { Copied: 1 });
    assert.equal(status.revision, 8);
	assert.equal(status.serverId, 'server-1');
    assert.equal(requests[2].url, '/api/run/status');
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
        vm.runInContext(`cancelBackupRunOnServer('cancel-timeout')`, apiContext),
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
