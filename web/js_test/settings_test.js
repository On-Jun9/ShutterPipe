const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

const projectRoot = path.resolve(__dirname, '..', '..');
const settingsSource = fs.readFileSync(path.join(projectRoot, 'web/static/js/settings.js'), 'utf8');

// settings.js는 전역 DOM과 서버 API 함수에 의존한다. 저장 순서만 검증하므로
// 폼 요소와 서버 전송 함수만 최소로 흉내 낸다.
function createContext(saveSettingsToServer) {
    const elements = new Map();
    const getElement = (id) => {
        if (!elements.has(id)) {
            elements.set(id, {
                checked: false,
                classList: { add() {}, remove() {} },
                closest: () => null,
                title: '',
                value: ''
            });
        }
        return elements.get(id);
    };

    const context = vm.createContext({
        console,
        document: { getElementById: getElement, querySelectorAll: () => [] },
        includeExtensions: ['jpg'],
        isRunning: false,
        saveSettingsToServer,
        setTimeout,
        window: { addEventListener() {} }
    });

    vm.runInContext(settingsSource, context);
    return { context, getElement };
}

test('연속 저장 요청은 직렬로 전송되고 마지막 전송이 최신 폼 값을 담는다', async () => {
    // 병렬 전송은 먼저 출발한 느린 요청이 나중에 도착해 최신 설정을 덮는다.
    const sent = [];
    let release;
    const firstSendBlocked = new Promise((resolve) => { release = resolve; });
    const context = createContext(async (config) => {
        sent.push(config.source);
        if (sent.length === 1) await firstSendBlocked;
        return { success: true };
    });

    const calls = vm.runInContext(`
        (() => {
            const src = document.getElementById('source');
            const pending = [];
            for (let i = 0; i < 5; i++) {
                src.value = '/src-' + i;
                pending.push(saveSettings());
            }
            return Promise.all(pending);
        })()
    `, context.context);

    release();
    await calls;

    // 첫 전송이 끝날 때까지 나머지 4건은 한 건으로 합쳐진다.
    assert.equal(sent.length, 2);
    assert.equal(sent[0], '/src-0');
    assert.equal(sent[1], '/src-4');
});

test('저장이 끝난 뒤의 요청은 다시 전송된다', async () => {
    const sent = [];
    const context = createContext(async (config) => {
        sent.push(config.source);
        return { success: true };
    });

    await vm.runInContext(`
        (async () => {
            const src = document.getElementById('source');
            src.value = '/first';
            await saveSettings();
            src.value = '/second';
            await saveSettings();
        })()
    `, context.context);

    assert.deepEqual(sent, ['/first', '/second']);
});
