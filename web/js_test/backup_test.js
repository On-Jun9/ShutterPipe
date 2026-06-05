const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

const projectRoot = path.resolve(__dirname, '..', '..');
const coreSource = fs.readFileSync(path.join(projectRoot, 'web/static/js/core.js'), 'utf8');
const backupSource = fs.readFileSync(path.join(projectRoot, 'web/static/js/backup.js'), 'utf8');

function createContext() {
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
        console,
        document: { getElementById: getElement },
        formatBytes: () => '',
        formatDuration: () => '',
        formatSpeed: () => '',
        window: { location: { host: 'localhost:8080', protocol: 'http:' } }
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
