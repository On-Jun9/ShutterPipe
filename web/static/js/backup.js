// Backup Module
// 백업 실행 및 WebSocket 통신

const observedTerminalRuns = new Set();
const terminalObservationStorageKey = 'shutterpipe.lastObservedTerminal';

// 상태 수렴 루프 식별자. 새 루프가 시작되면 증가시켜 이전 루프가 스스로 종료하게
// 하여 중복 실행을 방지한다.
let statusConvergenceToken = 0;
// 현재 실행 중인 관찰자(observeRunUntilTerminal)의 token. null이면 관찰자가 없다.
// 관찰자가 스스로 시도하는 WebSocket 재연결이 실패해 onclose가 발생해도, 이 값으로
// 이미 관찰자가 있음을 알 수 있어 handleWebSocketClose가 중복 관찰자를 스폰하지
// 않는다(run별 단일 소유권 lease). 관찰자는 종료 시 자신이 여전히 소유자일 때만 해제한다.
let runningObserverToken = null;
// capped backoff 지연(ms). 상한(마지막 값)에 도달하면 그 간격으로 저빈도 유지한다.
const statusConvergenceDelaysMs = [500, 1000, 2000, 4000, 8000];

// isSocketOpen은 ws가 실제로 열려 있는지 판정한다. connectWebSocket은 연결 전에
// ws에 socket을 대입하고 실패 시 onerror에서 reject하지만 ws=null 정리는 이후
// onclose에서 하므로, 단순 `if (ws)`는 CONNECTING/CLOSING/CLOSED socket을 연결됨으로
// 오판한다. terminal 이벤트를 받을 수 있는 상태는 OPEN뿐이다.
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
        '<p style="font-size: 14px; color: var(--color-text-tertiary); text-align: center;">파일 처리 목록이 여기에 표시됩니다...</p>';
    document.getElementById('summarySection').style.display = 'none';
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
    setRunStartButtonsDisabled(true);
    const dryRun = document.getElementById('dryRun');
    if (dryRun) dryRun.disabled = currentRunKind === 'verify';
    // 새로고침 복구 시 모드 탭을 실제 실행 종류에 맞춘다 (검증 실행 중인데 백업 탭이
    // 활성으로 보이는 시각적 불일치 방지). 잠긴 라디오라도 프로그램적 설정은 반영된다.
    const modeRadio = document.getElementById(currentRunKind === 'verify' ? 'mode-verify' : 'mode-backup');
    if (modeRadio) modeRadio.checked = true;
    setCancelButtonState(showCancelling ? false : status === 'running');

    const progressSection = document.getElementById('progressSection');
    if (progressSection) progressSection.style.display = 'block';
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
        if (radio) radio.disabled = locked;
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
async function startBackup(kind = 'backup') {
    const isVerify = kind === 'verify';
    const operationLabel = isVerify ? '검증' : '백업';
    addLogEntry(`${operationLabel} 시작 버튼 클릭됨`, 'info');

    // 중복 클릭 방지 (시작 중 또는 실행 중)
    if (isRunning || runStartPending) {
        addLogEntry('이미 다른 작업이 실행 중입니다.', 'warning');
        alert('이미 백업 또는 검증이 실행 중입니다.');
        return;
    }

    const source = document.getElementById('source').value;
    const dest = document.getElementById('dest').value;

    if (!source || !dest) {
        addLogEntry('경로 미입력: 원본 또는 목적지 경로가 비어있습니다.', 'warning');
        alert('원본 경로와 목적지를 입력해주세요.');
        return;
    }

    // 상태 설정 (경로 검증 직후, 비동기 작업 전)
    // 이제부터 모든 비동기 작업이 플래그 보호를 받음
    const requestedRunId = createRunId();
    runStartPending = true;
    beginTrackingRun(requestedRunId, 'running', kind);
    runRequestSent = false;
    runCancelPending = false;
    runCancelRequested = false;
    hasShownCloseAlert = false;  // 중복 알림 방지 플래그 초기화
    setRunStartButtonsDisabled(true);
    const dryRunInput = document.getElementById('dryRun');
    if (dryRunInput) dryRunInput.disabled = isVerify;
    setCancelButtonState(false);

    try {
        // 히스토리에 추가 (플래그 설정 후이므로 중복 클릭 방지)
        if (typeof addToPathHistory === 'function') {
            await addToPathHistory('source', source);
            await addToPathHistory('dest', dest);
        }

        addLogEntry(`설정 확인: Source=${source}, Dest=${dest}`, 'info');

        // 날짜 필터 (날짜만 전송, 타임존 없음)
        const dateFilterStart = document.getElementById('dateFilterStart').value;
        const dateFilterEnd = document.getElementById('dateFilterEnd').value;

        const config = {
            run_id: requestedRunId,
            source: source,
            dest: dest,
            organize_strategy: document.getElementById('organizeStrategy').value,
            event_name: document.getElementById('eventName').value,
            conflict_policy: document.getElementById('conflictPolicy').value,
            dedup_method: document.getElementById('dedupMethod').value,
            dry_run: isVerify ? false : document.getElementById('dryRun').checked,
            hash_verify: document.getElementById('hashVerify').checked,
            ignore_state: document.getElementById('ignoreState').checked,

            // 날짜 필터 (YYYY-MM-DD 형식, 타임존 무시하고 날짜만 비교)
            date_filter_start: dateFilterStart || null,
            date_filter_end: dateFilterEnd || null,

            // 고급 설정
            include_extensions: includeExtensions,
            jobs: parseInt(document.getElementById('jobs').value) || 0,
            unclassified_dir: document.getElementById('unclassifiedDir').value || 'unclassified',
            quarantine_dir: document.getElementById('quarantineDir').value || 'quarantine',
            state_file: document.getElementById('stateFile').value,
            log_file: document.getElementById('logFile').value,
            log_json: document.getElementById('logJson').checked
        };
        let hashManifestFile = null;
        if (isVerify) {
            config.verify_mode = getVerifyMode();
            if (config.verify_mode === 'hash' && document.getElementById('useHashManifest').checked) {
                hashManifestFile = document.getElementById('hashManifest').files?.[0] || null;
                if (!hashManifestFile) {
                    throw new Error('도착 폴더 해시 목록 파일을 선택해주세요.');
                }
            }
        }
        // Step 1: Connect WebSocket FIRST
        addLogEntry('WebSocket 연결 시도 중...', 'info');
        await connectWebSocket();
        addLogEntry('WebSocket 연결 성공. 서버에 실행 요청 전송 중...', 'info');

        // Step 1.5: Verify WebSocket is still connected before API request
        if (!ws || ws.readyState !== WebSocket.OPEN) {
            throw new Error('WebSocket 연결이 끊어졌습니다. 다시 시도해주세요.');
        }

        // Step 2: Send Run Request
        runRequestSent = true;
        const runResult = isVerify
            ? await startVerifyRunOnServer(config, hashManifestFile)
            : await startBackupRunOnServer(config);

        const statusText = runResult.status || 'NETWORK_ERROR';
        addLogEntry(`서버 응답 수신: Status ${statusText}`, runResult.success ? 'success' : 'error');

        if (!runResult.success) {
            const message = runResult.error || '알 수 없는 오류';
            // HTTP 응답 자체가 없으면 서버가 요청을 수락한 뒤 응답만 유실됐을 수 있다.
            if (!runResult.status) {
                if (terminalRunId === requestedRunId || currentRunId !== requestedRunId) return;
                const reconciliation = await synchronizeRunStatus(requestedRunId, { observe: true });
                if (terminalRunId === requestedRunId) return;
                if (!reconciliation?.mismatch && (reconciliation?.active || reconciliation?.terminal)) return;
                if (currentRunId && currentRunId !== requestedRunId) return;
                if (!reconciliation?.resolved) {
                    runStartPending = false;
                    runRequestSent = true;
                    runStatus = 'running';
                    setCancelButtonState(true);
                    document.getElementById('progressText').textContent = '서버 응답 유실 - 실행 상태 확인 필요';
                    addLogEntry('시작 응답과 상태 조회가 모두 실패해 실행 상태를 보수적으로 유지합니다.', 'warning');
                    alert(`서버 응답을 확인하지 못했습니다. ${operationLabel}이 실행 중일 수 있습니다.`);
                    // 불확실 시작: POST가 서버에 도달하지 못했다면 서버는 idle이라 열린
                    // WebSocket에서도 이벤트가 오지 않는다. WebSocket 상태와 무관하게
                    // active/idle/terminal/mismatch가 확정될 때까지 HTTP로 수렴한다(P1).
                    ensureObserver(requestedRunId, { assumeActive: false });
                    return;
                }
            }

            // 409: 다른 작업이 이미 활성 상태다. 이 시작 시도를 버리고 실제 실행에 동기화한다.
            if (runResult.status === 409) {
                runStartPending = false;
                finishTrackedRun(requestedRunId, false);
                const reconciliation = await synchronizeRunStatus(null, { observe: true });
                // 조회 도중 다른 탭의 진행 이벤트가 실제 실행을 채택했을 수 있다(currentRunId).
                if (reconciliation?.active || currentRunId) {
                    addLogEntry('다른 작업이 이미 실행 중이어서 해당 실행에 연결했습니다.', 'warning');
                    return; // WebSocket 유지: 진행 이벤트를 계속 수신한다.
                }
                if (reconciliation?.terminal) {
                    return;
                }
                // 409를 준 실행이 조회 시점엔 이미 끝나 서버가 idle이면, 불확실한
                // 실행 상태로 고정하지 말고 idle UI로 복구한다.
                if (reconciliation?.idle) {
                    setCancelButtonState(false);
                    restoreIdleRunControls();
                    if (ws) {
                        ws.close();
                        ws = null;
                    }
                    return;
                }
                // 조회 도중 WebSocket terminal 이벤트나 새 실행이 이미 최신 상태를
                // 적용해 조회 결과가 stale이면, 오래된 결과로 불확실 상태를 덮어쓰지
                // 않고 이미 반영된 상태를 그대로 유지한다. 이를 처리하지 않으면 완료된
                // 실행이 currentRunId·WebSocket 없이 running으로 부활한다.
                if (reconciliation?.stale) {
                    return;
                }
                // 상태 조회는 실패했지만 409는 실행 중임을 확정한다. idle로 되돌려
                // 다시 시작 버튼을 열지 말고, WebSocket을 유지한 채 보수적으로 실행 중
                // 상태를 유지한다. 이후 진행 이벤트가 도착하면 실행 UI가 복구된다.
                runRequestSent = true;
                runStatus = 'running';
                isRunning = true;
                runStateRevision++;
                setCancelButtonState(false); // 실행 ID를 몰라 이 탭에서는 취소할 수 없다.
                setRunStartButtonsDisabled(true);
                document.getElementById('progressSection').style.display = 'block';
                document.getElementById('progressText').textContent = '다른 작업 실행 중 - 상태 확인 필요';
                addLogEntry('다른 작업이 실행 중이지만 상태를 확인하지 못했습니다. 진행 상황 수신을 기다립니다.', 'warning');
                return; // WebSocket 유지
            }
            throw new Error(`${operationLabel} 시작 실패: ${message}`);
        }

        const serverObservation = observeServerInstance(runResult.serverId || null);
        if (serverObservation === 'stale') return;
        if (serverObservation === 'changed') {
            beginTrackingRun(requestedRunId, 'running', kind);
            runRequestSent = true;
        }


        if (runResult.runId && runResult.runId !== requestedRunId) {
            throw new Error('서버 실행 ID가 요청과 일치하지 않습니다.');
        }
        if (runResult.runKind && runResult.runKind !== kind) {
            throw new Error('서버 실행 종류가 요청과 일치하지 않습니다.');
        }

        // complete/cancelled/error 이벤트가 HTTP 응답보다 먼저 도착한 경우 terminal UI를 유지한다.
        if (terminalRunId === requestedRunId || currentRunId !== requestedRunId) {
            return;
        }

        // API 요청 성공 → 시작 완료, 실행 중
        runStartPending = false;
        // isRunning은 true 유지
        setCancelButtonState(true);

        // 진행 상황 초기화
        document.getElementById('progressBar').style.width = '0%';
        document.getElementById('progressBar').classList.remove('pulse');
        document.getElementById('progressPercent').textContent = '0%';
        document.getElementById('progressText').textContent = '준비 중...';
        document.getElementById('fileList').innerHTML = '<p style="font-size: 14px; color: var(--color-text-tertiary); text-align: center;">파일 처리 목록이 여기에 표시됩니다...</p>';

        document.getElementById('progressSection').style.display = 'block';
        document.getElementById('summarySection').style.display = 'none';

    } catch (error) {
        if (terminalRunId === requestedRunId || currentRunId !== requestedRunId) {
            addLogEntry(`종료된 실행의 늦은 시작 응답 무시: ${error.message}`, 'warning');
            return;
        }
        addLogEntry(`실행 중 예외 발생: ${error.message}`, 'error');
        alert('오류: ' + error.message);

        // 상태 복구 (모든 플래그 리셋)
        runStartPending = false;
        finishTrackedRun(requestedRunId);
        runRequestSent = false;
        runCancelPending = false;
        runCancelRequested = false;

        // UI 초기화
        resetBackupUI('오류 발생');
        setCancelButtonState(false);

        // 버튼 복구 (경로 검증 상태 반영)
        restoreIdleRunControls();

        // WebSocket 정리
        if (ws) {
            ws.close();
            ws = null;
        }
    }
}

async function startVerify() {
    return startBackup('verify');
}

// 현재 백업 또는 검증 취소
async function cancelBackup() {
    const operationLabel = currentRunKind === 'verify' ? '검증' : '백업';
    addLogEntry(`${operationLabel} 취소 버튼 클릭됨`, 'warning');

    if (!isRunning || runStartPending) {
        addLogEntry('실행 중인 작업이 없습니다.', 'warning');
        return;
    }

    if (runCancelPending || runCancelRequested) {
        addLogEntry('이미 취소 요청이 진행 중입니다.', 'warning');
        return;
    }

    runCancelPending = true;
    // 취소 의도를 새 세대로 표시한다. 취소 직전에 시작돼 아직 도착하지 않은 status
    // 조회는 requestedRevision이 어긋나 stale로 폐기되므로, 늦은 running 응답이
    // restoreRunningUI를 거쳐 취소 의도를 덮어쓰지 못한다(P1-c).
    runStateRevision++;
    setCancelButtonState(true, true);
    const cancelRunId = currentRunId;

    try {
        const cancelResult = await cancelBackupRunOnServer(cancelRunId, lastServerId);
        const statusText = cancelResult.status || 'NETWORK_ERROR';
        addLogEntry(`취소 요청 응답 수신: Status ${statusText}`, cancelResult.success ? 'warning' : 'error');

        if (!cancelResult.success) {
            if (cancelResult.status === 409) {
                if (currentRunId !== cancelRunId || (cancelRunId && terminalRunId === cancelRunId)) return;
                runCancelPending = false;
                setCancelButtonState(true);
                const reconciliation = await synchronizeRunStatus(cancelRunId, { observe: true });
                // 취소 대상 실행이 사라졌거나(mismatch/idle), 서버 재시작으로 epoch가
                // 바뀌어 currentRunId가 초기화된 경우, 새 활성 실행에 붙지 않았다면
                // 시작 버튼을 복구한다. currentRunId === cancelRunId만 보면 epoch
                // 초기화 시 복구 조건이 실행되지 않아 버튼이 잠긴다.
                const runGone = reconciliation?.mismatch || reconciliation?.idle || currentRunId !== cancelRunId;
                if (runGone && !reconciliation?.active) {
                    finishTrackedRun(cancelRunId, false);
                    setCancelButtonState(false);
                    restoreIdleRunControls();
                    await synchronizeRunStatus(null, { observe: true });
                }
                addLogEntry('취소 대상 실행이 일치하지 않아 서버 상태를 다시 확인했습니다.', 'warning');
                return;
            }

            // status가 없는 실패는 네트워크 오류/timeout이다. 두 경우가 구분되지 않는다:
            //   (1) 서버가 취소를 수락했지만 응답만 유실 → 서버는 cancelling/cancelled
            //   (2) POST가 서버에 도달하지 못함 → 서버는 계속 running
            // 취소를 낙관적으로 확정(runCancelRequested=true)하면 (2)에서 서버가 계속
            // running을 반환해도 재취소가 영구 차단된다. 대신 의도를 확정하지 않고 서버
            // 실제 상태로 수렴시킨다: 관찰자의 조회가 cancelling/cancelled면
            // restoreRunningUI가 취소를 확정하고, 계속 running이면 취소 버튼을 열어
            // 재취소를 허용한다(P1-a). 오래된 진행 이벤트는 revision 증가로 무효화한다.
            if (!cancelResult.status) {
                if (currentRunId !== cancelRunId || (cancelRunId && terminalRunId === cancelRunId)) return;
                addLogEntry('취소 응답을 받지 못했습니다. 서버 상태를 확인합니다.', 'warning');
                runCancelPending = false;
                runStateRevision++;
                setCancelButtonState(true); // 서버가 계속 running이면 재취소가 가능해야 한다
                if (!isSocketOpen()) {
                    await observeRunUntilTerminal(cancelRunId);
                }
                return;
            }

            throw new Error(cancelResult.error || '알 수 없는 오류');
        }

        if (cancelResult.runId && cancelResult.runId !== cancelRunId) return;
        if (currentRunId !== cancelRunId || (cancelRunId && terminalRunId === cancelRunId)) return;

        const serverObservation = observeServerInstance(cancelResult.serverId || null);
        if (serverObservation === 'stale') return;
        if (Number.isFinite(cancelResult.revision)) {
            lastServerRevision = Math.max(lastServerRevision, cancelResult.revision);
        }

        runCancelPending = false;
        runCancelRequested = true;
        runStatus = cancelResult.runStatus || 'cancelling';
        runStateRevision++;
        setCancelButtonState(false);
        document.getElementById('progressText').textContent = '취소 요청 중...';

        if (!isSocketOpen()) {
            // WebSocket이 열려 있지 않으면 terminal 이벤트를 받지 못해 UI가 잠긴다.
            // durable observer가 재연결을 시도하고, 안 되면 terminal까지 폴링한다.
            addLogEntry('취소 요청은 전달되었지만 연결이 끊겨 상태를 확인합니다.', 'warning');
            await observeRunUntilTerminal(cancelRunId);
        } else {
            addLogEntry(`${operationLabel} 취소 요청을 서버에 전달했습니다.`, 'warning');
        }
    } catch (error) {
        if (currentRunId !== cancelRunId || (cancelRunId && terminalRunId === cancelRunId)) return;
        runCancelPending = false;
        runCancelRequested = false;
        setCancelButtonState(isRunning && !runStartPending);
        addLogEntry(`취소 요청 실패: ${error.message}`, 'error');
        alert('오류: ' + error.message);
    }
}

// WebSocket 연결
function connectWebSocket() {
    if (ws && ws.readyState === WebSocket.OPEN) {
        return Promise.resolve();
    }
    if (wsConnectPromise) {
        return wsConnectPromise;
    }
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/api/ws`;
    const socket = new WebSocket(wsUrl);
    ws = socket;
    const connectPromise = new Promise((resolve, reject) => {
        addLogEntry(`WebSocket URL: ${wsUrl}`, 'info');

        socket.onopen = () => {
            if (ws !== socket) {
                socket.close();
                return;
            }
            addLogEntry('WebSocket 연결 열림', 'success');
            if (wsConnectPromise === connectPromise) wsConnectPromise = null;
            resolve();
        };

        socket.onmessage = (event) => {
            if (ws !== socket) return;
            let update;
            try {
                update = JSON.parse(event.data);
            } catch (e) {
                console.error('WebSocket message parse error:', e);
                addLogEntry('서버 메시지 파싱 오류 발생', 'error');
                return;
            }
            handleProgressUpdate(update);
        };

        socket.onerror = (error) => {
            if (ws !== socket) return;
            console.error('WebSocket error:', error);
            addLogEntry('WebSocket 연결 오류 발생', 'error');

            // ws.onerror는 에러 로깅만 담당
            // 상태 복구는 ws.onclose에서만 처리 (error -> close 순서 방지)
            if (wsConnectPromise === connectPromise) wsConnectPromise = null;
            reject(new Error('WebSocket connection failed'));
        };

        socket.onclose = (event) => {
            if (ws !== socket) return;
            const backupMayStillBeRunning = runRequestSent || (!runStartPending && isRunning);
            const cancelInProgress = runCancelPending || runCancelRequested;
            if (wsConnectPromise === connectPromise) wsConnectPromise = null;
            ws = null;
            handleWebSocketClose(event, backupMayStillBeRunning, cancelInProgress);
        };
    });
    wsConnectPromise = connectPromise;
    return wsConnectPromise;
}

function handleWebSocketClose(event, backupMayStillBeRunning, cancelInProgress) {
    console.log('WebSocket closed');
    // 이미 관찰자가 복구를 소유 중이면(대개 관찰자 자신의 재연결 시도가 실패해 발생한
    // onclose) 중복 관찰자·로그·alert를 만들지 않고 관찰자에 맡긴다. 관찰자가 스폰한
    // WebSocket close가 다시 관찰자를 스폰해 backoff/lease를 리셋하던 재귀를 끊는다.
    if (backupMayStillBeRunning && runningObserverToken !== null) {
        return;
    }
    addLogEntry(`WebSocket 연결 종료 (Code: ${event.code})`, 'warning');
    if (!backupMayStillBeRunning) {
        return;
    }

    if (cancelInProgress) {
        setCancelButtonState(false);
        const progressText = document.getElementById('progressText');
        if (progressText) {
            progressText.textContent = '취소 요청됨 - 완료 상태 확인 중...';
        }
        addLogEntry('취소 요청 처리 중 연결이 종료되어 상태를 폴링합니다.', 'warning');
        // 취소 진행 중 연결이 끊기면 terminal 이벤트를 받을 경로가 없다. 관찰자를
        // 시작해 완료까지 폴링한다(위 lease 가드로 이미 관찰자가 있으면 여기 도달 안 함).
        // 단, 취소 HTTP 요청이 아직 pending이면 여기서 폴링을 시작하지 않는다. 요청이
        // settle되기 전 서버는 아직 running을 응답하고, 그 snapshot이 restoreRunningUI를
        // 거쳐 runCancelPending을 해제하고 취소 버튼을 다시 열어버린다. 요청 자신이
        // settle 후 !isSocketOpen() 경로에서 수렴을 시작하므로 pending 동안은 맡긴다.
        if (currentRunId && !runCancelPending) {
            observeRunUntilTerminal(currentRunId);
        }
    } else {
        // 실행 여부는 서버 상태가 확정할 때까지 유지하고 HTTP 취소는 계속 허용한다.
        setCancelButtonState(true);
        addLogEntry('서버와의 연결이 끊겼습니다. 백업 상태를 확인할 수 없습니다.', 'error');
        // 정상 실행 중 연결이 끊기면 terminal 이벤트를 받을 경로가 없다. 관찰자를 시작해
        // 재연결 시도 + terminal 폴링으로 UI가 running에 잠기지 않게 한다.
        ensureObserver(currentRunId);
    }

    if (!hasShownCloseAlert) {
        hasShownCloseAlert = true;
        alert('서버와의 연결이 끊어졌습니다.\n\n백업이 계속 진행 중일 수 있으므로,\n페이지를 새로고침하여 상태를 확인하세요.');
    }
}

// 진행 상황 업데이트 처리
function handleProgressUpdate(update) {
    if (!acceptProgressUpdate(update)) {
        addLogEntry(`다른 실행의 지연된 이벤트 무시: ${update.run_id}`, 'warning');
        return;
    }
    const operationKind = update.kind || currentRunKind || 'backup';
    const operationLabel = operationKind === 'verify' ? '검증' : '백업';

    if (update.type === 'complete' || update.type === 'cancelled' || update.type === 'error') {
        rememberTerminalRun(update.server_id || lastServerId, update.run_id);
    }

    // 다른 탭에서 시작된 실행을 이 탭이 진행 이벤트로 처음 감지한 경우, 실행 UI를
    // 복구해 이 탭에서도 취소할 수 있게 한다. 시작 버튼은 경로 검증 등으로 이미
    // 비활성일 수 있으므로, 실행 중이며 취소 요청이 없는데 취소 버튼이 비활성인
    // 상태(=아직 실행 UI를 세우지 않음)를 최초 감지 신호로 사용한다.
    if (update.type === 'status' || update.type === 'analysis_progress' || update.type === 'progress') {
        // runCancelPending도 제외한다: 취소 HTTP 요청이 진행 중일 때 restoreRunningUI가
        // runCancelPending을 초기화하고 취소 버튼을 다시 활성화하면 안 된다.
        if (isRunning && !runStartPending && !runCancelRequested && !runCancelPending) {
            const cancelBtn = document.getElementById('cancelBtn');
            if (cancelBtn && cancelBtn.disabled) {
                restoreRunningUI('running');
            }
        }
    }

    const progressBar = document.getElementById('progressBar');
    const progressPercent = document.getElementById('progressPercent');
    const progressText = document.getElementById('progressText');

    if (update.type === 'status') {
        progressText.textContent = update.message;
        progressBar.style.width = '100%';
        progressBar.classList.add('pulse');
        progressPercent.textContent = '';
        addLogEntry(update.message, 'info');

    } else if (update.type === 'analysis_progress') {
        const percent = Math.round((update.current / update.total) * 100);
        progressBar.classList.remove('pulse');
        progressBar.style.width = percent + '%';
        progressPercent.textContent = percent + '%';
        progressText.textContent = `${update.message} (${update.current}/${update.total})`;
        // 500개마다 로그 출력
        if (update.current % 500 === 0) {
             addLogEntry(`${update.message} (${update.current}/${update.total})`, 'info');
        }

    } else if (update.type === 'progress') {
        const percent = Math.round((update.current / update.total) * 100);
        progressBar.classList.remove('pulse');
        progressBar.style.width = percent + '%';
        progressPercent.textContent = percent + '%';
        if (operationKind === 'verify') {
            progressText.textContent = `검증 중: ${update.filename} (${update.current}/${update.total})`;
            if (update.verify_verdict && update.verify_verdict !== 'ok') {
                addFileToList(update.filename, update.verify_verdict);
            }
        } else {
            progressText.textContent = `복사 중: ${update.filename} (${update.current}/${update.total})`;
            addFileToList(update.filename, update.action);

            // 에러나 특수 동작 로그
            if (update.action === 'failed') {
                addLogEntry(`실패: ${update.filename} - ${update.error || 'Unknown error'}`, 'error');
            } else if (update.action === 'quarantined') {
                addLogEntry(`격리됨: ${update.filename}`, 'warning');
            }
        }

    } else if (update.type === 'complete') {
        // 상태 복구
        finishTrackedRun(update.run_id);
        setCancelButtonState(false);

        // 버튼 복구
        restoreIdleRunControls();

        progressBar.classList.remove('pulse');
        progressBar.style.width = '100%';
        progressPercent.textContent = '100%';
        progressText.textContent = '완료!';

        addLogEntry(`${operationLabel} 작업이 완료되었습니다.`, 'success');
        if (operationKind === 'verify' && update.verify_summary) {
            showVerifySummary(update.verify_summary, update.run_id, update.server_id || lastServerId);
        } else if (update.summary) {
            showSummary(update.summary);
        }

        // Reload backup history after completion
        if (typeof loadHistoryList === 'function') {
            loadHistoryList();
        }

        if (ws) {
            ws.close();
            ws = null;
        }
    } else if (update.type === 'error') {
        // 상태 복구
        finishTrackedRun(update.run_id);
        setCancelButtonState(false);

        // 버튼 복구
        restoreIdleRunControls();

        const errorMessage = update.error || '알 수 없는 오류';
        if (operationKind === 'verify' && update.verify_summary) {
            progressBar.classList.remove('pulse');
            progressBar.style.width = '100%';
            progressPercent.textContent = '100%';
            progressText.textContent = '오류로 종료됨';
            showVerifySummary(update.verify_summary, update.run_id, update.server_id || lastServerId);
        } else if (update.summary) {
            // 부분 실패: 성공/실패 집계를 유지해 사용자가 무엇이 처리됐는지 볼 수 있게 한다.
            progressBar.classList.remove('pulse');
            progressBar.style.width = '100%';
            progressPercent.textContent = '100%';
            progressText.textContent = '오류로 종료됨';
            showSummary(update.summary);
        } else {
            resetBackupUI('오류 발생');
        }
        addLogEntry('오류 발생: ' + errorMessage, 'error');
        alert('오류: ' + errorMessage);

        if (ws) {
            ws.close();
            ws = null;
        }
    } else if (update.type === 'cancelled') {
        // 상태 복구
        finishTrackedRun(update.run_id);
        setCancelButtonState(false);

        // 버튼 복구
        restoreIdleRunControls();

        progressBar.classList.remove('pulse');
        progressText.textContent = update.message || '취소됨';
        addLogEntry(update.message || `${operationLabel} 작업이 취소되었습니다.`, 'warning');

        if (operationKind === 'verify' && update.verify_summary) {
            showVerifySummary(update.verify_summary, update.run_id, update.server_id || lastServerId);
        } else if (update.summary) {
            showSummary(update.summary);
        }

        if (typeof loadHistoryList === 'function') {
            loadHistoryList();
        }

        if (ws) {
            ws.close();
            ws = null;
        }
    }
}

async function initializeRunTracking() {
    const initializationRevision = runStateRevision;
    try {
        await connectWebSocket();
    } catch (error) {
        addLogEntry(`초기 WebSocket 연결 실패: ${error.message}`, 'warning');
    }
    if (initializationRevision !== runStateRevision) return;
    // 페이지 로드 시 active run을 채택하면 관찰자를 보장한다: WebSocket 연결에 실패했다면
    // HTTP 폴링으로 terminal까지 수렴해야 UI가 running에 잠기지 않는다(P1).
    await synchronizeRunStatus(null, { observe: true });
}

window.addEventListener('DOMContentLoaded', initializeRunTracking);

function shellQuote(value) {
    return `'${String(value || '').replace(/'/g, `'\\''`)}'`;
}

function pathContains(parent, candidate) {
    const normalizedParent = String(parent || '').replace(/\/+$/, '') || '/';
    const normalizedCandidate = String(candidate || '').replace(/\/+$/, '') || '/';
    return normalizedParent === '/' ||
        normalizedCandidate === normalizedParent ||
        normalizedCandidate.startsWith(normalizedParent + '/');
}

function updateHashManifestCommand() {
    const command = document.getElementById('hashManifestCommand');
    const warning = document.getElementById('hashManifestCommandWarning');
    if (!command) return;

    const dest = document.getElementById('dest')?.value?.trim() || '<도착 폴더>';
    let output = '/tmp/shutterpipe-hashes.txt';
    let outputExpression = shellQuote(output);
    let outputWarning = '출력 파일은 반드시 검사 대상 폴더 밖에 두세요.';

    if (pathContains(dest, output)) {
        output = '$HOME/shutterpipe-hashes.txt';
        outputExpression = '"$HOME/shutterpipe-hashes.txt"';
    }
    if (dest === '/') {
        outputExpression = shellQuote('/Volumes/OTHER/shutterpipe-hashes.txt');
        outputWarning = '루트 폴더 전체를 대상으로 하면 같은 파일시스템 안에 안전한 출력 위치가 없습니다. 검사 대상 밖의 다른 볼륨을 지정하세요.';
    }

    command.textContent = `find ${shellQuote(dest)} -type f -exec sha256sum {} + > ${outputExpression}`;
    if (warning) warning.textContent = outputWarning;
}

function getVerifyMode() {
    return document.getElementById('verifyModeHash')?.checked ? 'hash' : 'quick';
}

function updateVerifyOptions() {
    const mode = getVerifyMode();
    const hashOptions = document.getElementById('hashVerifyOptions');
    const useManifest = document.getElementById('useHashManifest');
    const manifestPanel = document.getElementById('hashManifestPanel');
    const manifestInput = document.getElementById('hashManifest');
    const hashMode = mode === 'hash';
    const manifestEnabled = hashMode && !!useManifest?.checked;

    if (hashOptions) hashOptions.hidden = !hashMode;
    if (useManifest) useManifest.disabled = !hashMode;
    if (manifestPanel) manifestPanel.hidden = !manifestEnabled;
    if (manifestInput) manifestInput.disabled = !manifestEnabled;
    updateHashManifestCommand();
}

async function copyHashManifestCommand() {
    const command = document.getElementById('hashManifestCommand')?.textContent || '';
    try {
        await navigator.clipboard.writeText(command);
        if (typeof showNotification === 'function') {
            showNotification('해시 목록 생성 명령을 복사했습니다.', 'success');
        }
    } catch (error) {
        alert(`명령을 복사하지 못했습니다: ${error.message}`);
    }
}

function initializeVerifyUI() {
    updateVerifyOptions();
    const dest = document.getElementById('dest');
    if (dest?.addEventListener) {
        dest.addEventListener('input', updateHashManifestCommand);
    }
}

window.addEventListener('DOMContentLoaded', initializeVerifyUI);

// 파일 목록에 추가
function addFileToList(filename, action) {
    const fileList = document.getElementById('fileList');

    if (fileList.children.length === 1 && fileList.children[0].tagName === 'P') {
        fileList.innerHTML = '';
    }

    const actionLabels = {
        'copied': '[복사]',
        'skipped': '[건너뜀]',
        'renamed': '[이름변경]',
        'overwritten': '[덮어쓰기]',
        'quarantined': '[격리]',
        'failed': '[실패]',
        'missing': '[누락]',
        'mismatch': '[불일치]',
        'unverifiable': '[검증 불가]'
    };

    const label = actionLabels[action] || '[처리]';
    const entry = document.createElement('div');
    entry.className = 'file-list-item';
    entry.textContent = `${label} ${filename}`;

    fileList.appendChild(entry);
    fileList.scrollTop = fileList.scrollHeight;
}

// 요약 표시
function showSummary(summary) {
    const summarySection = document.getElementById('summarySection');
    const summaryContent = document.getElementById('summaryContent');
    const summaryTitle = document.getElementById('summaryTitle');
    if (summaryTitle) summaryTitle.textContent = '완료 요약';

    const durationSeconds = Math.round(summary.Duration / 1000000000);
    const duration = formatDuration(durationSeconds);
    const totalSize = formatBytes(summary.BytesCopied);
    const speed = formatSpeed(summary.BytesPerSecond);

    summaryContent.innerHTML = `
        <div class="summary-section">
            <h3 class="summary-section-title">파일 처리</h3>
            <div class="summary-grid">
                <div class="summary-item" data-type="info">
                    <div class="summary-label">스캔됨</div>
                    <div class="summary-value">${summary.ScannedFiles}</div>
                </div>
                <div class="summary-item" data-type="info">
                    <div class="summary-label">처리 대상</div>
                    <div class="summary-value">${summary.TotalFiles}</div>
                </div>
                <div class="summary-item" data-type="success">
                    <div class="summary-label">복사됨</div>
                    <div class="summary-value">${summary.Copied}</div>
                </div>
                <div class="summary-item" data-type="neutral">
                    <div class="summary-label">건너뜀</div>
                    <div class="summary-value">${summary.Skipped}</div>
                </div>
                <div class="summary-item" data-type="info">
                    <div class="summary-label">이름 변경</div>
                    <div class="summary-value">${summary.Renamed}</div>
                </div>
                <div class="summary-item" data-type="info">
                    <div class="summary-label">덮어쓰기</div>
                    <div class="summary-value">${summary.Overwritten}</div>
                </div>
                <div class="summary-item" data-type="warning">
                    <div class="summary-label">격리됨</div>
                    <div class="summary-value">${summary.Quarantined}</div>
                </div>
                <div class="summary-item" data-type="error">
                    <div class="summary-label">실패</div>
                    <div class="summary-value">${summary.Failed}</div>
                </div>
                <div class="summary-item" data-type="warning">
                    <div class="summary-label">분류 불가</div>
                    <div class="summary-value">${summary.Unclassified}</div>
                </div>
            </div>
        </div>

        <div class="summary-section">
            <h3 class="summary-section-title">성능</h3>
            <div class="summary-grid">
                <div class="summary-item" data-type="info">
                    <div class="summary-label">소요 시간</div>
                    <div class="summary-value">${duration}</div>
                </div>
                <div class="summary-item" data-type="info">
                    <div class="summary-label">복사량</div>
                    <div class="summary-value">${totalSize}</div>
                </div>
                <div class="summary-item" data-type="info">
                    <div class="summary-label">속도</div>
                    <div class="summary-value">${speed}</div>
                </div>
            </div>
        </div>
    `;

    summarySection.style.display = 'block';

	if (Array.isArray(summary.Warnings)) {
		summary.Warnings.forEach((warning) => {
			addLogEntry(`주의: ${String(warning)}`, 'warning');
		});
	}
}

function verifyVerdictLabel(verdict) {
    return {
        missing: '누락',
        mismatch: '불일치',
        unverifiable: '검증 불가'
    }[verdict] || String(verdict || '문제');
}

function showVerifySummary(summary, runId, serverId) {
    const summarySection = document.getElementById('summarySection');
    const summaryContent = document.getElementById('summaryContent');
    const summaryTitle = document.getElementById('summaryTitle');
    const parseErrors = Number(summary.manifest?.parse_errors || 0);
    const incomplete = !!summary.incomplete_manifest;
    const modeLabel = summary.mode === 'hash' ? '정밀' : '빠른';
    const title = incomplete
        ? `⚠ 불완전한 목록으로 검증됨 (형식 오류 ${parseErrors}줄)`
        : `✅ 검증 완료 — ${modeLabel} 모드${summary.manifest ? ' (해시 목록 대조)' : ''}`;
    if (summaryTitle) summaryTitle.textContent = title;

    const problemItems = Array.isArray(summary.problems)
        ? summary.problems.map((problem) => `
            <div class="verify-problem">
                <div class="verify-problem-header">
                    <span>${escapeHtml(verifyVerdictLabel(problem.verdict))}</span>
                    <span title="${escapeHtml(problem.source_path)}">${escapeHtml(problem.name || problem.source_path)}</span>
                </div>
                <div class="verify-problem-reason">${escapeHtml(problem.reason)}</div>
            </div>
        `).join('')
        : '';
    const manifestInfo = summary.manifest
        ? `
            <div class="summary-item" data-type="${incomplete ? 'warning' : 'info'}">
                <div class="summary-label">해시 목록</div>
                <div class="summary-value" style="font-size: 16px;">${escapeHtml(summary.manifest.filename)}</div>
                <div class="verify-help">${Number(summary.manifest.entries || 0)}건 · 형식 오류 ${parseErrors}줄</div>
            </div>
        `
        : '';
    const duration = formatDuration(Math.round(Number(summary.duration || 0) / 1000000000));
    const canRequeue = !!summary.requeue_allowed && Number(summary.requeue_eligible || 0) > 0;
    const requeueLabel = incomplete
        ? '불완전한 해시 목록에서는 재복사 예약을 할 수 없습니다.'
        : `문제 ${Number(summary.requeue_eligible || 0)}건, 다음 백업 때 다시 복사되게 하기`;

    summaryContent.innerHTML = `
        <div class="summary-section verify-problems">
            <div class="summary-grid">
                <div class="summary-item" data-type="info">
                    <div class="summary-label">출발</div>
                    <div class="summary-value" style="font-size: 16px;">${escapeHtml(summary.source)}</div>
                    <div class="verify-help">${Number(summary.source_files || 0)}개 · ${formatBytes(Number(summary.source_bytes || 0))}</div>
                </div>
                <div class="summary-item" data-type="info">
                    <div class="summary-label">도착</div>
                    <div class="summary-value" style="font-size: 16px;">${escapeHtml(summary.dest)}</div>
                    <div class="verify-help">소요 시간 ${escapeHtml(duration)}</div>
                </div>
                ${manifestInfo}
                <div class="summary-item" data-type="success">
                    <div class="summary-label">정상</div>
                    <div class="summary-value">${Number(summary.normal || 0)}</div>
                </div>
                <div class="summary-item" data-type="error">
                    <div class="summary-label">누락</div>
                    <div class="summary-value">${Number(summary.missing || 0)}</div>
                </div>
                <div class="summary-item" data-type="warning">
                    <div class="summary-label">불일치</div>
                    <div class="summary-value">${Number(summary.mismatch || 0)}</div>
                </div>
                <div class="summary-item" data-type="neutral">
                    <div class="summary-label">검증 불가</div>
                    <div class="summary-value">${Number(summary.unverifiable || 0)}</div>
                </div>
            </div>
            ${summary.manifest ? '<p class="verify-warning">결과는 목록을 생성한 시점의 도착 폴더 상태 기준입니다.</p>' : ''}
        </div>
        <div class="summary-section verify-problems">
            <h3 class="summary-section-title">문제 파일 (${Number(summary.problem_count || 0)}건)</h3>
            <div class="verify-problem-list">
                ${problemItems || '<p class="verify-help">문제 파일이 없습니다.</p>'}
            </div>
            ${summary.problems_truncated ? '<p class="verify-warning">화면 표시 상한을 초과한 문제는 로그에서 확인하세요.</p>' : ''}
        </div>
        ${Number(summary.problem_count || 0) > 0 ? `
            <div class="verify-requeue">
                <button id="verifyRequeueBtn" class="btn-primary" ${canRequeue ? '' : 'disabled'}>
                    ${escapeHtml(requeueLabel)}
                </button>
            </div>
        ` : ''}
    `;

    const requeueButton = document.getElementById('verifyRequeueBtn');
    if (requeueButton && canRequeue) {
        requeueButton.dataset.runId = runId || '';
        requeueButton.dataset.serverId = serverId || '';
        requeueButton.onclick = () => requeueVerificationProblems(requeueButton);
    }
    summarySection.style.display = 'block';
    addLogEntry(`검증 결과: 정상 ${Number(summary.normal || 0)}, 문제 ${Number(summary.problem_count || 0)}`, incomplete ? 'warning' : 'success');
    if (Array.isArray(summary.warnings)) {
        summary.warnings.forEach((warning) => addLogEntry(`주의: ${String(warning)}`, 'warning'));
    }
}

async function requeueVerificationProblems(button) {
    if (!button || button.disabled) return;
    button.disabled = true;
    const originalText = button.textContent;
    button.textContent = '재복사 예약 중...';

    const result = await requeueVerifyOnServer(button.dataset.runId, button.dataset.serverId);
    if (result.success) {
        button.textContent = `재복사 예약됨 ${Number(result.applied || 0)}건 (건너뜀 ${Number(result.skipped || 0)}건)`;
        addLogEntry(button.textContent, 'success');
        return;
    }

    button.disabled = false;
    button.textContent = originalText;
    const message = result.status === 409
        ? result.error || '실행 중에는 처리할 수 없습니다. 다시 검증해 주세요.'
        : result.error || '재복사 예약에 실패했습니다.';
    addLogEntry(`재복사 예약 실패: ${message}`, 'error');
    alert(message);
}
