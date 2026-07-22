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

async function startBackup(kind = 'backup') {
    const isVerify = kind === 'verify';
    const operationLabel = isVerify ? '검증' : '백업';
    addLogEntry(`${operationLabel} 시작 버튼 클릭됨`, 'info');

    // 중복 클릭 방지 (시작 중 또는 실행 중)
    if (isRunning || runStartPending) {
        addLogEntry('이미 다른 작업이 실행 중입니다.', 'warning');
        reportRunMessage('이미 백업 또는 검증이 실행 중입니다.', 'warning');
        return;
    }

    const source = document.getElementById('source').value;
    const dest = document.getElementById('dest').value;

    if (!source || !dest) {
        addLogEntry('경로 미입력: 원본 또는 목적지 경로가 비어있습니다.', 'warning');
        reportRunMessage('원본 경로와 목적지를 입력해주세요.', 'error', source ? 'dest' : 'source');
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
    clearRunInputErrors();
    clearRunMessage();
    setRunVisualState('running');
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
                    reportRunMessage('서버 응답 유실 - 실행 상태 확인 필요', 'warning');
                    addLogEntry('시작 응답과 상태 조회가 모두 실패해 실행 상태를 보수적으로 유지합니다.', 'warning');
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
                setElementHidden(document.getElementById('progressSection'), false);
                setRunVisualState('running');
                reportRunMessage('다른 작업 실행 중 - 상태 확인 필요', 'warning');
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
        document.getElementById('fileList').innerHTML = '<p class="file-list-placeholder">파일 처리 목록이 여기에 표시됩니다...</p>';

        setElementHidden(document.getElementById('progressSection'), false);
        setElementHidden(document.getElementById('summarySection'), true);
        setElementHidden(document.getElementById('runNextActions'), true);
        updateProgressReadout();

    } catch (error) {
        if (terminalRunId === requestedRunId || currentRunId !== requestedRunId) {
            addLogEntry(`종료된 실행의 늦은 시작 응답 무시: ${error.message}`, 'warning');
            return;
        }
        addLogEntry(`실행 중 예외 발생: ${error.message}`, 'error');

        // 상태 복구 (모든 플래그 리셋)
        runStartPending = false;
        finishTrackedRun(requestedRunId);
        runRequestSent = false;
        runCancelPending = false;
        runCancelRequested = false;

        // UI 초기화
        resetBackupUI('오류 발생');
        setRunVisualState('error');
        reportRunMessage(`오류: ${error.message}`, 'error');
        setCancelButtonState(false);

        // 버튼 복구 (경로 검증 상태 반영)
        restoreIdleRunControls();
        setRunVisualState('error');

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
    setRunVisualState('cancelling');
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
        setRunVisualState('cancelling');
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
        setRunVisualState('running');
        reportRunMessage(`취소 요청에 실패했습니다: ${error.message}`, 'error');
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
        reportRunMessage('서버와의 연결이 끊겼습니다. 실행 상태를 계속 확인합니다.', 'warning');
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
        if (progressText) progressText.textContent = update.message || '상태를 확인하는 중...';
        if (progressBar) {
            progressBar.style.width = '100%';
            progressBar.classList.add('pulse');
            progressBar.removeAttribute?.('aria-valuenow');
            progressBar.setAttribute?.('aria-valuetext', '진행 상태 확인 중');
        }
        if (progressPercent) progressPercent.textContent = '';
        addLogEntry(update.message, 'info');

    } else if (update.type === 'analysis_progress') {
        const percent = Math.round((update.current / update.total) * 100);
        if (progressBar) {
            progressBar.classList.remove('pulse');
            progressBar.style.width = percent + '%';
        }
        if (progressPercent) progressPercent.textContent = percent + '%';
        if (progressText) progressText.textContent = `${update.message} (${update.current}/${update.total})`;
        updateProgressReadout(update);
        // 500개마다 로그 출력
        if (update.current % 500 === 0) {
             addLogEntry(`${update.message} (${update.current}/${update.total})`, 'info');
        }

    } else if (update.type === 'progress') {
        const percent = Math.round((update.current / update.total) * 100);
        if (progressBar) {
            progressBar.classList.remove('pulse');
            progressBar.style.width = percent + '%';
        }
        if (progressPercent) progressPercent.textContent = percent + '%';
        updateProgressReadout(update);
        if (operationKind === 'verify') {
            if (progressText) progressText.textContent = `검증 중: ${update.filename} (${update.current}/${update.total})`;
            if (update.verify_verdict && update.verify_verdict !== 'ok') {
                addFileToList(update.filename, update.verify_verdict);
            }
        } else {
            if (progressText) progressText.textContent = `복사 중: ${update.filename} (${update.current}/${update.total})`;
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
        setRunVisualState('complete');

        if (progressBar) {
            progressBar.classList.remove('pulse');
            progressBar.style.width = '100%';
        }
        if (progressPercent) progressPercent.textContent = '100%';
        if (progressText) progressText.textContent = '완료!';
        updateProgressReadout({ current: 1, total: 1 });

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
        setRunVisualState('error');

        const errorMessage = update.error || '알 수 없는 오류';
        if (operationKind === 'verify' && update.verify_summary) {
            if (progressBar) {
                progressBar.classList.remove('pulse');
                progressBar.style.width = '100%';
            }
            if (progressPercent) progressPercent.textContent = '100%';
            if (progressText) progressText.textContent = '오류로 종료됨';
            showVerifySummary(update.verify_summary, update.run_id, update.server_id || lastServerId);
        } else if (update.summary) {
            // 부분 실패: 성공/실패 집계를 유지해 사용자가 무엇이 처리됐는지 볼 수 있게 한다.
            if (progressBar) {
                progressBar.classList.remove('pulse');
                progressBar.style.width = '100%';
            }
            if (progressPercent) progressPercent.textContent = '100%';
            if (progressText) progressText.textContent = '오류로 종료됨';
            showSummary(update.summary);
        } else {
            resetBackupUI('오류 발생');
        }
        addLogEntry('오류 발생: ' + errorMessage, 'error');
        reportRunMessage(`오류: ${errorMessage}`, 'error');

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
        setRunVisualState('complete');

        if (progressBar) progressBar.classList.remove('pulse');
        if (progressText) progressText.textContent = update.message || '취소됨';
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

function initializeRunUI() {
    updateRunConfigurationSummary();
    updateRunActionLabels();
    updateProgressReadout();

    ['source', 'dest', 'dateFilterStart', 'dateFilterEnd', 'dryRun', 'mode-backup', 'mode-verify',
        'organizeStrategy', 'eventName', 'conflictPolicy', 'dedupMethod', 'verifyModeQuick', 'verifyModeHash'].forEach((id) => {
        const field = document.getElementById(id);
        if (!field?.addEventListener) return;
        field.addEventListener('input', updateRunConfigurationSummary);
        field.addEventListener('change', () => {
            updateRunConfigurationSummary();
            updateRunActionLabels();
        });
    });

    const extensionContainer = document.getElementById('extensionTagsContainer');
    if (extensionContainer && typeof MutationObserver === 'function') {
        new MutationObserver(updateRunConfigurationSummary).observe(extensionContainer, { childList: true });
    }
}

window.addEventListener('DOMContentLoaded', initializeRunUI);

