// Run Tracking Module
// 실행 추적과 복구: run id 발급, terminal 기록, 서버 인스턴스 관찰, 상태 수렴 폴링

function isSocketOpen() {
    return !!ws && ws.readyState === WebSocket.OPEN;
}

function terminalObservationKey(serverId, runId) {
    return `${serverId || 'unknown'}:${runId || ''}`;
}

function hasObservedTerminalInPage(serverId, runId) {
    const key = terminalObservationKey(serverId, runId);
    return observedTerminalRuns.has(key);
}

function hasPersistedTerminalRun(serverId, runId) {
    const key = terminalObservationKey(serverId, runId);
    try {
        return window.sessionStorage?.getItem(terminalObservationStorageKey) === key;
    } catch (_error) {
        return false;
    }
}

function rememberTerminalRun(serverId, runId) {
    if (!runId) return;
    const key = terminalObservationKey(serverId, runId);
    observedTerminalRuns.add(key);
    try {
        window.sessionStorage?.setItem(terminalObservationStorageKey, key);
    } catch (_error) {
        // In-memory tracking still prevents repeat handling in this page.
    }
}

// UI 초기화 함수
function resetBackupUI(message = '오류 발생') {
    const progressBar = document.getElementById('progressBar');
    const progressPercent = document.getElementById('progressPercent');
    const progressText = document.getElementById('progressText');

    progressBar.classList.remove('pulse');
    progressBar.style.width = '0%';
    progressPercent.textContent = '0%';
    progressText.textContent = message;

    document.getElementById('fileList').innerHTML =
        '<p class="file-list-placeholder">파일 처리 목록이 여기에 표시됩니다...</p>';
    setElementHidden(document.getElementById('summarySection'), true);
}

function setCancelButtonState(enabled, pending = false) {
    const cancelBtn = document.getElementById('cancelBtn');
    if (!cancelBtn) return;

    cancelBtn.disabled = !enabled || pending;
    cancelBtn.textContent = pending ? '취소 요청 중...' : '취소';
}

function createRunId() {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
        return crypto.randomUUID();
    }
    return `run-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function beginTrackingRun(runId, status = 'running', kind = null) {
    // 다른 실행을 채택할 때 이전 실행의 파일 목록/요약이 새 실행과 섞이지 않게 비운다.
    if (runId && runId !== resultsRunId) {
        if (typeof resetRunResultsView === 'function') resetRunResultsView();
        resultsRunId = runId;
    }
    currentRunId = runId;
    currentRunKind = kind || currentRunKind || 'backup';
    terminalRunId = null;
    runStatus = status;
    isRunning = status !== 'idle';
    runStateRevision++;
}

function observeServerInstance(serverId) {
    if (!serverId) return 'unknown';
    if (serverId === lastServerId) return 'current';
    if (retiredServerIds.has(serverId)) return 'stale';
    if (lastServerId && lastServerId !== serverId) {
        retiredServerIds.add(lastServerId);
        lastServerRevision = 0;
        currentRunId = null;
        currentRunKind = null;
        terminalRunId = null;
        runStatus = 'idle';
        isRunning = false;
        runStartPending = false;
        runRequestSent = false;
        runCancelPending = false;
        runCancelRequested = false;
        runStateRevision++;
        lastServerId = serverId;
        return 'changed';
    }
    lastServerId = serverId;
    return 'current';
}

function finishTrackedRun(runId, markTerminal = true) {
    if (runId && currentRunId && runId !== currentRunId) {
        return false;
    }

    if (markTerminal) {
        terminalRunId = runId || currentRunId;
    }
    currentRunId = null;
    currentRunKind = null;
    runStatus = 'idle';
    isRunning = false;
    runStartPending = false;
    runRequestSent = false;
    runCancelPending = false;
    runCancelRequested = false;
    runStateRevision++;
    return true;
}

function acceptProgressUpdate(update) {
    const runId = update.run_id || null;
    if (observeServerInstance(update.server_id || null) === 'stale') {
        return false;
    }
    if (Number.isFinite(update.revision) && update.revision < lastServerRevision) {
        return false;
    }
    if (!runId) {
        return true; // 이전 서버와의 하위 호환
    }
    if (terminalRunId === runId) {
        return false;
    }
    if (currentRunId && currentRunId !== runId) {
        return false;
    }
    if (!currentRunId) {
        beginTrackingRun(runId, 'running', update.kind || null);
        runStartPending = false;
        runRequestSent = true;
    } else if (update.kind) {
        currentRunKind = update.kind;
    }
    if (Number.isFinite(update.revision)) {
        lastServerRevision = Math.max(lastServerRevision, update.revision);
    }
    return true;
}

function restoreRunningUI(status) {
    runStartPending = false;
    runRequestSent = true;
    // 취소가 진행/확정 중이면 서버가 아직 running을 보고해도 취소 의도를 보존한다.
    // durable observer가 취소 처리 중 받은 running snapshot으로 취소 상태를 되돌리거나
    // (P1-c), 취소 pending을 해제해 버튼을 다시 열지 않도록 한다.
    const cancelActive = runCancelPending || runCancelRequested;
    if (!cancelActive) {
        runCancelPending = false;
        runCancelRequested = status === 'cancelling';
    }
    const showCancelling = cancelActive || status === 'cancelling';
    setRunVisualState(showCancelling ? 'cancelling' : 'running');
    setRunStartButtonsDisabled(true);
    const dryRun = document.getElementById('dryRun');
    if (dryRun) dryRun.disabled = currentRunKind === 'verify';
    // 새로고침 복구 시 모드 탭을 실제 실행 종류에 맞춘다 (검증 실행 중인데 백업 탭이
    // 활성으로 보이는 시각적 불일치 방지). 잠긴 라디오라도 프로그램적 설정은 반영된다.
    const modeRadio = document.getElementById(currentRunKind === 'verify' ? 'mode-verify' : 'mode-backup');
    if (modeRadio) modeRadio.checked = true;
    setCancelButtonState(showCancelling ? false : status === 'running');

    const progressSection = document.getElementById('progressSection');
    if (progressSection) setElementHidden(progressSection, false);
    const progressText = document.getElementById('progressText');
    if (progressText) {
        progressText.textContent = showCancelling
            ? '취소 요청 중...'
            : `실행 중인 ${currentRunKind === 'verify' ? '검증' : '백업'}에 다시 연결되었습니다.`;
    }
}

// 백업/검증 모드 전환 잠금 (실행 중 true, idle false). DOM 조작은 이 한 곳에만 둔다.
function setRunModeTabsLocked(locked) {
    ['mode-backup', 'mode-verify'].forEach((id) => {
        const radio = document.getElementById(id);
        if (radio) {
            radio.disabled = locked;
            if (radio.setAttribute) radio.setAttribute('aria-disabled', String(locked));
        }
    });
    const modeHeader = document.getElementById('run-mode-tabs');
    if (modeHeader && modeHeader.classList) {
        if (locked) modeHeader.classList.add('locked');
        else modeHeader.classList.remove('locked');
    }
}

function setRunStartButtonsDisabled(disabled) {
    ['startBtn', 'verifyBtn'].forEach((id) => {
        const button = document.getElementById(id);
        if (button) button.disabled = disabled;
    });
    setRunModeTabsLocked(disabled);
}

function restoreIdleRunControls() {
    const dryRun = document.getElementById('dryRun');
    if (dryRun) dryRun.disabled = false;
    if (typeof enableBackupButton === 'function') {
        enableBackupButton();
    } else {
        setRunStartButtonsDisabled(false);
    }
    // enableBackupButton은 버튼만 다시 켜고 탭 잠금은 모르므로, idle 복귀 시 여기서 해제한다.
    setRunModeTabsLocked(false);
    setRunVisualState('idle');
}

function finishStaleTrackedRun(runId) {
    // 추적하던 run이 사라졌다(다른 run 활성/idle/epoch 변경). 우리 run을 정리하고
    // 버튼을 복구한 뒤, 새 active run이 있으면 채택하도록 unscoped sync를 시도한다.
    // finishTrackedRun이 false면 이미 다른 successor run이 채택된 것이므로, 그 run의
    // UI를 깨지 않도록 버튼 복구를 건너뛴다.
    if (finishTrackedRun(runId, false)) {
        setCancelButtonState(false);
        restoreIdleRunControls();
    }
    // 새 active run을 채택하면 관찰자를 보장한다(관찰자 mismatch 경로에서 호출된 경우엔
    // lease를 이미 보유해 no-op이고, handoff는 별도로 소유권을 넘긴다).
    return synchronizeRunStatus(null, { observe: true });
}

// observeRunUntilTerminal은 지정한 run을 terminal까지 소유하는 run별 단일 관찰자다.
// runningObserverToken으로 lease를 표시해, 관찰자가 스스로 시도하는 WebSocket
// 재연결이 실패해 onclose가 나더라도 중복 관찰자가 스폰되지 않는다(단일 소유권).
// 관찰의 기본 경로는 WebSocket이므로 매 회차 재연결을 시도하고, 성공하면 이후
// terminal은 live 이벤트가 전달하도록 소유권을 놓고 종료한다. WebSocket이 없으면
// capped backoff로 status를 폴링하되, 상한(마지막 지연=8s)에 도달하면 그 간격으로
// 저빈도 무기한 유지한다 — 정상 백업은 오래 걸릴 수 있고 일시적 장애도 지나갈 수
// 있으므로 고정 횟수에서 영구 포기하지 않는다. 종료 조건:
//   - terminal 도달(handler가 UI 정리)
//   - mismatch/idle/epoch 변경 → 정리 후 새 active run이 있으면 그 run으로 소유권 이관
//   - WebSocket 재연결(이후 terminal은 WebSocket이 전달)
//   - 새 관찰자가 lease를 인수(token 교체)
async function observeRunUntilTerminal(runId, options) {
    const myToken = ++statusConvergenceToken;
    runningObserverToken = myToken;
    // assumeActive=false는 "불확실 시작"이다: 실행이 실제로 존재하는지 아직 모르므로
    // WebSocket이 열려 있어도(서버가 idle이면 이벤트가 안 온다) active/terminal/idle/
    // mismatch가 확정될 때까지 HTTP로 계속 수렴한다. active를 확인하면 confirmed가 되어
    // 이후 WebSocket이 살아 있으면 live 이벤트에 소유권을 넘긴다.
    let confirmed = !options || options.assumeActive !== false;
    let degraded = false;
    try {
        for (let attempt = 0; ; attempt++) {
            if (attempt > 0) {
                const delayIndex = Math.min(attempt - 1, statusConvergenceDelaysMs.length - 1);
                await new Promise((resolve) => setTimeout(resolve, statusConvergenceDelaysMs[delayIndex]));
            }
            if (statusConvergenceToken !== myToken) return; // 새 관찰자가 lease 인수
            if (!isRunning || currentRunId !== runId || terminalRunId === runId) return;

            // 관찰의 기본 경로는 WebSocket이다. 끊겼으면 재연결을 시도한다. 이 재연결
            // 실패로 onclose가 발생해도 runningObserverToken이 이 관찰자를 가리켜
            // handleWebSocketClose가 새 관찰자를 스폰하지 않는다.
            if (!isSocketOpen()) {
                try {
                    await connectWebSocket();
                } catch (_error) {
                    // 재연결 실패 시에는 이번 회차를 폴링으로 확인한다.
                }
                if (statusConvergenceToken !== myToken) return;
            }

            // WebSocket 재연결 여부와 무관하게 catch-up 조회를 1회 한다: 연결이 끊긴
            // 동안 terminal에 도달했다면 재연결된 WebSocket은 그 이벤트를 재생하지 않는다.
            const reconciliation = await synchronizeRunStatus(runId);
            if (statusConvergenceToken !== myToken) return;
            if (reconciliation?.terminal) return; // terminal 핸들러가 UI를 이미 정리함
            if (reconciliation?.mismatch || reconciliation?.idle || currentRunId !== runId || terminalRunId === runId) {
                const adopted = await finishStaleTrackedRun(runId);
                // unscoped sync가 소켓 없이 새 active run을 채택했다면 그 run으로 관찰
                // 소유권을 넘겨 terminal까지 유지한다(handoff가 token을 올려 lease 이관).
                if (adopted?.active && !isSocketOpen() && currentRunId && currentRunId !== runId) {
                    observeRunUntilTerminal(currentRunId);
                }
                return;
            }

            // active(running/cancelling)를 실제로 관측하면 실행 존재가 확정된다.
            if (reconciliation?.active) confirmed = true;

            // 조회 실패(서버 도달 불가)여도 영구 종료하지 않는다. capped backoff 상한에서
            // 저빈도로 계속 재시도해 서버 복구/실행 terminal을 관찰한다. 상태 전이 시에만
            // 안내를 남긴다(스팸 방지).
            const queryFailed = reconciliation && reconciliation.resolved === false && !reconciliation.stale;
            if (queryFailed && !degraded) {
                degraded = true;
                addLogEntry('연결이 불안정합니다. 백업 상태를 계속 확인합니다.', 'warning');
            } else if (!queryFailed && degraded) {
                degraded = false;
                addLogEntry('상태 조회가 다시 정상화되었습니다.', 'info');
            }

            // 실행이 확정된 상태에서 WebSocket이 살아 있으면 이후 terminal은 live 이벤트가
            // 전달하므로 폴링을 멈추고 lease를 놓는다. 아직 불확실(confirmed=false)하면
            // WebSocket이 열려 있어도 확정될 때까지 계속 폴링한다.
            if (isSocketOpen() && confirmed) return;
        }
    } finally {
        // 이 관찰자가 여전히 소유자일 때만 lease를 해제한다. successor로 소유권을 넘긴
        // 경우(handoff가 token 증가) lease는 successor가 계속 보유한다.
        if (statusConvergenceToken === myToken) runningObserverToken = null;
    }
}

// ensureObserver는 active 채택/소켓 상실/불확실 시작 등 모든 진입 경로가 공유하는
// 단일 관찰자 보장 지점이다. 이미 관찰자가 있으면(lease 보유) 아무것도 하지 않아
// 중복 관찰자를 만들지 않는다. confirmed-active(assumeActive=true, 기본)는 WebSocket이
// 살아 있으면 live 이벤트가 terminal을 전달하므로 관찰자를 띄우지 않는다. 불확실
// 시작(assumeActive=false)은 WebSocket 상태와 무관하게 HTTP로 확정까지 수렴한다.
function ensureObserver(runId, options) {
    if (!runId) return;
    if (runningObserverToken !== null) return; // 이미 관찰자가 소유 중
    const assumeActive = !options || options.assumeActive !== false;
    if (assumeActive && isSocketOpen()) return; // live WebSocket이 terminal 전달
    observeRunUntilTerminal(runId, { assumeActive });
}

// 실행은 확정됐지만(예: 409 보수 경로에서 다른 탭의 실행에 붙었을 때) run id를 몰라
// 관찰자를 띄울 수 없는 상태에서 WebSocket마저 끊기면, ensureObserver(null)은 no-op이라
// 복구 경로가 사라져 UI가 running에 영구히 잠긴다. run id 없이 서버 상태로 수렴해 실제
// 실행을 채택(→ synchronizeRunStatus가 currentRunId를 세우고 ensureObserver로 정식 관찰자
// 이관)하거나 idle/terminal로 복구한다. 관찰자 lease(runningObserverToken)를 잡지 않으므로
// 채택 시 ensureObserver가 관찰자를 설치하는 것을 막지 않는다. 중복 실행은 플래그로 방지.
let recoveringUnidentifiedRun = false;
async function recoverUnidentifiedRun() {
    if (recoveringUnidentifiedRun) return;
    recoveringUnidentifiedRun = true;
    try {
        for (let attempt = 0; ; attempt++) {
            // 채택돼 run id가 생겼거나(관찰자 인수), 실행이 끝났거나, 다른 관찰자가 소유
            // 중이면 복구 종료.
            if (!isRunning || currentRunId || runningObserverToken !== null) return;
            const reconciliation = await synchronizeRunStatus(null, { observe: true });
            if (!isRunning || currentRunId || runningObserverToken !== null) return;
            if (reconciliation?.terminal || reconciliation?.idle) return;
            // 조회 실패/stale이면 저빈도로 계속 수렴한다(영구 포기하지 않음).
            const delayIndex = Math.min(attempt, statusConvergenceDelaysMs.length - 1);
            await new Promise((resolve) => setTimeout(resolve, statusConvergenceDelaysMs[delayIndex]));
        }
    } finally {
        recoveringUnidentifiedRun = false;
    }
}

async function synchronizeRunStatus(expectedRunId = null, options = {}) {
    const requestedRevision = runStateRevision;
    const result = await getBackupRunStatusFromServer(expectedRunId);
    if (!result.success) {
        addLogEntry(`백업 상태 조회 실패: ${result.error || result.status}`, 'warning');
        return { resolved: false, result };
    }

    // 조회 도중 WebSocket 이벤트나 사용자 요청이 상태를 바꿨다면 조회 결과는 오래된 값이다.
    if (requestedRevision !== runStateRevision) {
        return { resolved: false, stale: true, result };
    }
    if (observeServerInstance(result.serverId || null) === 'stale') {
        return { resolved: false, stale: true, result };
    }
    if (Number.isFinite(result.revision) && result.revision < lastServerRevision) {
        return { resolved: false, stale: true, result };
    }

    if (result.runStatus === 'complete' || result.runStatus === 'cancelled' || result.runStatus === 'error') {
        if (expectedRunId && result.runId !== expectedRunId) {
            return { resolved: true, mismatch: true, result };
        }
        if (!result.runId || terminalRunId === result.runId) {
            return { resolved: true, terminal: true, result };
        }
        const restoreVerifyResult = result.runKind === 'verify' && !!result.verifySummary;
        if (hasObservedTerminalInPage(result.serverId, result.runId) ||
            (hasPersistedTerminalRun(result.serverId, result.runId) && !restoreVerifyResult)) {
            terminalRunId = result.runId;
            if (Number.isFinite(result.revision)) {
                lastServerRevision = Math.max(lastServerRevision, result.revision);
            }
            // 이 탭이 새로고침 전에 이미 처리한 terminal이라 이벤트를 재생하지 않지만,
            // 그 실행을 다시 추적 중이거나(currentRunId===runId) 추적 run 없이 실행
            // UI만 남아 있는 경우(예: 409에서 requested run을 정리해 currentRunId=null
            // 인데 시작 버튼이 잠긴 상태)에도 idle UI를 복원해야 계속 잠기지 않는다.
            if (currentRunId === result.runId || (!currentRunId && !runStartPending)) {
                finishTrackedRun(result.runId);
                setCancelButtonState(false);
                restoreIdleRunControls();
            }
            return { resolved: true, terminal: true, observed: true, result };
        }
        if (currentRunId && currentRunId !== result.runId) {
            return { resolved: true, mismatch: true, result };
        }
        handleProgressUpdate({
            type: result.runStatus,
            run_id: result.runId,
            server_id: result.serverId,
            kind: result.runKind,
            summary: result.summary,
            verify_summary: result.verifySummary,
            error: result.error,
            revision: result.revision,
            message: result.runStatus === 'cancelled'
                ? `${result.runKind === 'verify' ? '검증' : '백업'}이 취소되었습니다.`
                : undefined
        });
        return { resolved: true, terminal: true, result };
    }

    if (result.runStatus === 'running' || result.runStatus === 'cancelling') {
        if (!result.runId || terminalRunId === result.runId) return;
        if (expectedRunId && result.runId !== expectedRunId) {
            return { resolved: true, mismatch: true, result };
        }
        if (Number.isFinite(result.revision)) {
            lastServerRevision = Math.max(lastServerRevision, result.revision);
        }
        beginTrackingRun(result.runId, result.runStatus, result.runKind);
        restoreRunningUI(result.runStatus);
        // HTTP로 active run을 채택하는 호출자(observe:true)는 terminal을 받을 경로를
        // 보장한다. WebSocket이 없으면 관찰자가 폴링하고, 있으면 live 이벤트에 맡긴다.
        // 단발 reconciliation(observe 미지정)이나 관찰자 자신의 재진입(lease 보유)은
        // 관찰자를 새로 만들지 않는다.
        if (options.observe) ensureObserver(result.runId);
        return { resolved: true, active: true, result };
    }

    if (result.runStatus === 'idle' && isRunning) {
        if (!expectedRunId && runStartPending) {
            return { resolved: false, stale: true, result };
        }
        // 시작 응답 유실을 확인하는 호출자는 원래 시작 오류 처리 경로에서 UI를 정리한다.
        if (expectedRunId && currentRunId === expectedRunId) {
            return { resolved: true, idle: true, result };
        }
        finishTrackedRun(currentRunId, false);
        setCancelButtonState(false);
        restoreIdleRunControls();
    }
    return { resolved: true, idle: result.runStatus === 'idle', result };
}

// 백업과 검증은 동일한 실행 상태 머신을 공유한다.
