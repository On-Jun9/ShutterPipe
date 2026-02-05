// Backup Module
// 백업 실행 및 WebSocket 통신

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
    runStartPending = true;
    isRunning = true;
    runRequestSent = false;
    hasShownCloseAlert = false;  // 중복 알림 방지 플래그 초기화
    document.getElementById('startBtn').disabled = true;

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
        const response = await fetch('/api/run', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(config)
        });

        addLogEntry(`서버 응답 수신: Status ${response.status}`, response.ok ? 'success' : 'error');

        // !response.ok를 throw로 변경하여 catch에 몰아주기
        if (!response.ok) {
            const error = await response.text();
            throw new Error('백업 시작 실패: ' + error);
        }

        // API 요청 성공 → 시작 완료, 실행 중
        runStartPending = false;
        // isRunning은 true 유지

        // 진행 상황 초기화
        document.getElementById('progressBar').style.width = '0%';
        document.getElementById('progressBar').classList.remove('pulse');
        document.getElementById('progressPercent').textContent = '0%';
        document.getElementById('progressText').textContent = '준비 중...';
        document.getElementById('fileList').innerHTML = '<p style="font-size: 14px; color: var(--color-text-tertiary); text-align: center;">파일 처리 목록이 여기에 표시됩니다...</p>';

        document.getElementById('progressSection').style.display = 'block';
        document.getElementById('summarySection').style.display = 'none';

    } catch (error) {
        addLogEntry(`실행 중 예외 발생: ${error.message}`, 'error');
        alert('오류: ' + error.message);

        // 상태 복구 (모든 플래그 리셋)
        runStartPending = false;
        isRunning = false;
        runRequestSent = false;

        // UI 초기화
        resetBackupUI('오류 발생');

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

// WebSocket 연결
function connectWebSocket() {
    return new Promise((resolve, reject) => {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/api/ws`;

        addLogEntry(`WebSocket URL: ${wsUrl}`, 'info');
        ws = new WebSocket(wsUrl);

        ws.onopen = () => {
            addLogEntry('WebSocket 연결 열림', 'success');
            resolve();
        };

        ws.onmessage = (event) => {
            const update = JSON.parse(event.data);
            handleProgressUpdate(update);
        };

        ws.onerror = (error) => {
            console.error('WebSocket error:', error);
            addLogEntry('WebSocket 연결 오류 발생', 'error');

            // ws.onerror는 에러 로깅만 담당
            // 상태 복구는 ws.onclose에서만 처리 (error -> close 순서 방지)
            reject(new Error('WebSocket connection failed'));
        };

        ws.onclose = (event) => {
            console.log('WebSocket closed');
            addLogEntry(`WebSocket 연결 종료 (Code: ${event.code})`, 'warning');

            // /api/run 전송 이후에는 서버에서 작업이 계속될 수 있으므로 경고
            const backupMayStillBeRunning = runRequestSent || (!runStartPending && isRunning);
            if (backupMayStillBeRunning) {
                // 중복 알림 방지
                if (!hasShownCloseAlert) {
                    hasShownCloseAlert = true;
                    addLogEntry('서버와의 연결이 끊겼습니다. 백업 상태를 확인할 수 없습니다.', 'error');
                    alert('서버와의 연결이 끊어졌습니다.\n\n백업이 계속 진행 중일 수 있으므로,\n페이지를 새로고침하여 상태를 확인하세요.');
                }

                // 상태는 유지 (재클릭 방지)
                // 사용자가 페이지를 새로고침하여 상태를 확인해야 함
            }
        };
    });
}

// 진행 상황 업데이트 처리
function handleProgressUpdate(update) {
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
        runStartPending = false;
        isRunning = false;
        runRequestSent = false;

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
        showSummary(update.summary);

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
        runStartPending = false;
        isRunning = false;
        runRequestSent = false;

        // 버튼 복구
        if (typeof enableBackupButton === 'function') {
            enableBackupButton();
        } else {
            document.getElementById('startBtn').disabled = false;
        }

        // UI 초기화
        resetBackupUI('오류 발생');

        addLogEntry('오류 발생: ' + update.error, 'error');
        alert('오류: ' + update.error);

        if (ws) {
            ws.close();
            ws = null;
        }
    }
}

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
}
