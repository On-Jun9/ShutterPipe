// Backup Module
// 백업 실행 및 WebSocket 통신

// 백업 시작
async function startBackup() {
    addLogEntry('백업 시작 버튼 클릭됨', 'info');

    if (isRunning) {
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

    // 히스토리에 추가
    if (typeof addToPathHistory === 'function') {
        addToPathHistory('source', source);
        addToPathHistory('dest', dest);
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

    try {
        // Step 1: Connect WebSocket FIRST
        addLogEntry('WebSocket 연결 시도 중...', 'info');
        await connectWebSocket();
        addLogEntry('WebSocket 연결 성공. 서버에 실행 요청 전송 중...', 'info');

        // Step 2: Send Run Request
        const response = await fetch('/api/run', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(config)
        });

        addLogEntry(`서버 응답 수신: Status ${response.status}`, response.ok ? 'success' : 'error');

        if (!response.ok) {
            const error = await response.text();
            addLogEntry(`서버 요청 실패: ${error}`, 'error');
            alert('백업 시작 실패: ' + error);
            // Close WS if failed
            if (ws) {
                ws.close();
                ws = null;
            }
            return;
        }

        isRunning = true;
        document.getElementById('startBtn').disabled = true;

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

        // Reset UI state on error
        isRunning = false;
        document.getElementById('startBtn').disabled = false;

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

            // Reset UI if error occurs during connection
            if (isRunning) {
                isRunning = false;
                document.getElementById('startBtn').disabled = false;
            }
            reject(new Error('WebSocket connection failed'));
        };

        ws.onclose = (event) => {
            console.log('WebSocket closed');
            addLogEntry(`WebSocket 연결 종료 (Code: ${event.code})`, 'warning');

            // Reset UI if closed unexpectedly while running
            if (isRunning) {
                isRunning = false;
                document.getElementById('startBtn').disabled = false;
                addLogEntry('서버와의 연결이 끊겨 작업이 중단되었습니다.', 'error');
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
        isRunning = false;
        document.getElementById('startBtn').disabled = false;
        progressBar.classList.remove('pulse');
        progressBar.style.width = '100%';
        progressPercent.textContent = '100%';
        progressText.textContent = '완료!';

        addLogEntry('백업 작업이 완료되었습니다.', 'success');
        showSummary(update.summary);

        if (ws) {
            ws.close();
            ws = null;
        }
    } else if (update.type === 'error') {
        isRunning = false;
        document.getElementById('startBtn').disabled = false;
        progressBar.classList.remove('pulse');
        alert('오류: ' + update.error);
        addLogEntry('오류 발생: ' + update.error, 'error');

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

// 용량을 적절한 단위로 포맷
function formatBytes(bytes) {
    if (bytes === 0) return '0 B';
    const gb = bytes / (1024 * 1024 * 1024);
    const mb = bytes / (1024 * 1024);

    if (gb >= 1) {
        return gb.toFixed(2) + ' GB';
    } else if (mb >= 1) {
        return mb.toFixed(2) + ' MB';
    } else {
        return (bytes / 1024).toFixed(2) + ' KB';
    }
}

// 속도를 적절한 단위로 포맷
function formatSpeed(bytesPerSecond) {
    if (bytesPerSecond === 0) return '0 B/s';

    const gbps = bytesPerSecond / (1024 * 1024 * 1024);
    const mbps = bytesPerSecond / (1024 * 1024);
    const kbps = bytesPerSecond / 1024;

    if (gbps >= 1) {
        return gbps.toFixed(2) + ' GB/s';
    } else if (mbps >= 1) {
        return mbps.toFixed(2) + ' MB/s';
    } else if (kbps >= 1) {
        return kbps.toFixed(2) + ' KB/s';
    } else {
        return bytesPerSecond.toFixed(2) + ' B/s';
    }
}

// 시간을 적절한 단위로 포맷
function formatDuration(seconds) {
    if (seconds === 0) return '0초';

    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    const secs = seconds % 60;

    const parts = [];
    if (hours > 0) parts.push(`${hours}시간`);
    if (minutes > 0) parts.push(`${minutes}분`);
    if (secs > 0 || parts.length === 0) parts.push(`${secs}초`);

    return parts.join(' ');
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
