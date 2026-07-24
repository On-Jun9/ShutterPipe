// Run Results Module
// 실행 결과 렌더링: 파일 처리 목록, 백업 완료 요약

// 새 실행 채택 시 이전 실행의 결과 표시를 비운다 (beginTrackingRun에서 호출)
function resetRunResultsView() {
    const fileList = document.getElementById('fileList');
    if (fileList) {
        fileList.innerHTML = '<p class="file-list-placeholder">파일 처리 목록이 여기에 표시됩니다...</p>';
    }
    setElementHidden(document.getElementById('summarySection'), true);
    setElementHidden(document.getElementById('runNextActions'), true);
}

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

    setElementHidden(summarySection, false);
    addCompletionActions('backup');

	if (Array.isArray(summary.Warnings)) {
		summary.Warnings.forEach((warning) => {
			addLogEntry(`주의: ${String(warning)}`, 'warning');
		});
	}
}

