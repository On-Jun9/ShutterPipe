// Backup Module
// 백업 실행 및 WebSocket 통신

const observedTerminalRuns = new Set();
const terminalObservationStorageKey = 'shutterpipe.lastObservedTerminal';

function terminalObservationKey(serverId, runId) {
    return `${serverId || 'unknown'}:${runId || ''}`;
}

function hasObservedTerminalRun(serverId, runId) {
    const key = terminalObservationKey(serverId, runId);
    if (observedTerminalRuns.has(key)) return true;
    try {
        return window.localStorage?.getItem(terminalObservationStorageKey) === key;
    } catch (_error) {
        return false;
    }
}

function rememberTerminalRun(serverId, runId) {
    if (!runId) return;
    const key = terminalObservationKey(serverId, runId);
    observedTerminalRuns.add(key);
    try {
        window.localStorage?.setItem(terminalObservationStorageKey, key);
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

function beginTrackingRun(runId, status = 'running') {
    currentRunId = runId;
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
        beginTrackingRun(runId);
        runStartPending = false;
        runRequestSent = true;
    }
    if (Number.isFinite(update.revision)) {
        lastServerRevision = Math.max(lastServerRevision, update.revision);
    }
    return true;
}

function restoreRunningUI(status) {
    runStartPending = false;
    runRequestSent = true;
    runCancelPending = false;
    runCancelRequested = status === 'cancelling';
    document.getElementById('startBtn').disabled = true;
    setCancelButtonState(status === 'running');

    const progressSection = document.getElementById('progressSection');
    if (progressSection) progressSection.style.display = 'block';
    const progressText = document.getElementById('progressText');
    if (progressText) {
        progressText.textContent = status === 'cancelling'
            ? '취소 요청 중...'
            : '실행 중인 백업에 다시 연결되었습니다.';
    }
}

async function synchronizeRunStatus(expectedRunId = null) {
    const requestedRevision = runStateRevision;
    const result = await getBackupRunStatusFromServer();
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
        if (hasObservedTerminalRun(result.serverId, result.runId)) {
            terminalRunId = result.runId;
            if (Number.isFinite(result.revision)) {
                lastServerRevision = Math.max(lastServerRevision, result.revision);
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
            summary: result.summary,
            error: result.error,
            revision: result.revision,
            message: result.runStatus === 'cancelled' ? '백업이 취소되었습니다.' : undefined
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
        beginTrackingRun(result.runId, result.runStatus);
        restoreRunningUI(result.runStatus);
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
        if (typeof enableBackupButton === 'function') {
            enableBackupButton();
        } else {
            document.getElementById('startBtn').disabled = false;
        }
    }
    return { resolved: true, idle: result.runStatus === 'idle', result };
}

// 백업 시작
async function startBackup() {
    addLogEntry('백업 시작 버튼 클릭됨', 'info');

    // 중복 클릭 방지 (시작 중 또는 실행 중)
    if (isRunning || runStartPending) {
        addLogEntry('이미 백업이 실행 중입니다.', 'warning');
        alert('이미 백업이 실행 중입니다.');
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
    beginTrackingRun(requestedRunId);
    runRequestSent = false;
    runCancelPending = false;
    runCancelRequested = false;
    hasShownCloseAlert = false;  // 중복 알림 방지 플래그 초기화
    document.getElementById('startBtn').disabled = true;
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
            dry_run: document.getElementById('dryRun').checked,
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
        const runResult = await startBackupRunOnServer(config);

        const statusText = runResult.status || 'NETWORK_ERROR';
        addLogEntry(`서버 응답 수신: Status ${statusText}`, runResult.success ? 'success' : 'error');

        if (!runResult.success) {
            const message = runResult.error || '알 수 없는 오류';
            // HTTP 응답 자체가 없으면 서버가 요청을 수락한 뒤 응답만 유실됐을 수 있다.
            if (!runResult.status) {
                if (terminalRunId === requestedRunId || currentRunId !== requestedRunId) return;
                const reconciliation = await synchronizeRunStatus(requestedRunId);
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
                    alert('서버 응답을 확인하지 못했습니다. 백업이 실행 중일 수 있습니다.');
                    return;
                }
            }
            throw new Error('백업 시작 실패: ' + message);
        }

        const serverObservation = observeServerInstance(runResult.serverId || null);
        if (serverObservation === 'stale') return;
        if (serverObservation === 'changed') {
            beginTrackingRun(requestedRunId);
            runRequestSent = true;
        }


        if (runResult.runId && runResult.runId !== requestedRunId) {
            throw new Error('서버 실행 ID가 요청과 일치하지 않습니다.');
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
        if (typeof enableBackupButton === 'function') {
            enableBackupButton();
        } else {
            document.getElementById('startBtn').disabled = false;
        }

        // WebSocket 정리
        if (ws) {
            ws.close();
            ws = null;
        }
    }
}

// 백업 취소
async function cancelBackup() {
    addLogEntry('백업 취소 버튼 클릭됨', 'warning');

    if (!isRunning || runStartPending) {
        addLogEntry('실행 중인 백업이 없습니다.', 'warning');
        return;
    }

    if (runCancelPending || runCancelRequested) {
        addLogEntry('이미 취소 요청이 진행 중입니다.', 'warning');
        return;
    }

    runCancelPending = true;
    setCancelButtonState(true, true);
    const cancelRunId = currentRunId;

    try {
        const cancelResult = await cancelBackupRunOnServer(cancelRunId);
        const statusText = cancelResult.status || 'NETWORK_ERROR';
        addLogEntry(`취소 요청 응답 수신: Status ${statusText}`, cancelResult.success ? 'warning' : 'error');

        if (!cancelResult.success) {
            if (cancelResult.status === 409) {
                if (currentRunId !== cancelRunId || (cancelRunId && terminalRunId === cancelRunId)) return;
                runCancelPending = false;
                setCancelButtonState(true);
                const reconciliation = await synchronizeRunStatus(cancelRunId);
                if (currentRunId === cancelRunId && (reconciliation?.mismatch || reconciliation?.idle)) {
                    finishTrackedRun(cancelRunId, false);
                    setCancelButtonState(false);
                    if (typeof enableBackupButton === 'function') {
                        enableBackupButton();
                    } else {
                        document.getElementById('startBtn').disabled = false;
                    }
                    await synchronizeRunStatus();
                }
                addLogEntry('취소 대상 실행이 일치하지 않아 서버 상태를 다시 확인했습니다.', 'warning');
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

        if (!ws) {
            addLogEntry('취소 요청은 전달되었지만 연결이 끊겨 완료 상태를 확인할 수 없습니다.', 'warning');
        } else {
            addLogEntry('백업 취소 요청을 서버에 전달했습니다.', 'warning');
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
    addLogEntry(`WebSocket 연결 종료 (Code: ${event.code})`, 'warning');
    if (!backupMayStillBeRunning) {
        return;
    }

    if (cancelInProgress) {
        setCancelButtonState(false);
        const progressText = document.getElementById('progressText');
        if (progressText) {
            progressText.textContent = '취소 요청됨 - 완료 상태 확인 불가';
        }
        addLogEntry('취소 요청 처리 중 연결이 종료되어 완료 상태를 확인할 수 없습니다.', 'warning');
    } else {
        // 실행 여부는 서버 상태가 확정할 때까지 유지하고 HTTP 취소는 계속 허용한다.
        setCancelButtonState(true);
        addLogEntry('서버와의 연결이 끊겼습니다. 백업 상태를 확인할 수 없습니다.', 'error');
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

    if (update.type === 'complete' || update.type === 'cancelled' || update.type === 'error') {
        rememberTerminalRun(update.server_id || lastServerId, update.run_id);
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
        progressText.textContent = `복사 중: ${update.filename} (${update.current}/${update.total})`;

        addFileToList(update.filename, update.action);

        // 에러나 특수 동작 로그
        if (update.action === 'failed') {
            addLogEntry(`실패: ${update.filename} - ${update.error || 'Unknown error'}`, 'error');
        } else if (update.action === 'quarantined') {
            addLogEntry(`격리됨: ${update.filename}`, 'warning');
        }

    } else if (update.type === 'complete') {
        // 상태 복구
        finishTrackedRun(update.run_id);
        setCancelButtonState(false);

        // 버튼 복구
        if (typeof enableBackupButton === 'function') {
            enableBackupButton();
        } else {
            document.getElementById('startBtn').disabled = false;
        }

        progressBar.classList.remove('pulse');
        progressBar.style.width = '100%';
        progressPercent.textContent = '100%';
        progressText.textContent = '완료!';

        addLogEntry('백업 작업이 완료되었습니다.', 'success');
        if (update.summary) {
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
        if (typeof enableBackupButton === 'function') {
            enableBackupButton();
        } else {
            document.getElementById('startBtn').disabled = false;
        }

        // UI 초기화
        resetBackupUI('오류 발생');

        const errorMessage = update.error || '알 수 없는 오류';
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
        if (typeof enableBackupButton === 'function') {
            enableBackupButton();
        } else {
            document.getElementById('startBtn').disabled = false;
        }

        progressBar.classList.remove('pulse');
        progressText.textContent = update.message || '취소됨';
        addLogEntry(update.message || '백업 작업이 취소되었습니다.', 'warning');

        if (update.summary) {
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
    await synchronizeRunStatus();
}

window.addEventListener('DOMContentLoaded', initializeRunTracking);

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
        'failed': '[실패]'
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
