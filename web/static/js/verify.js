// Verify Module
// 검증 전용: 비교 모드 옵션, 해시 매니페스트, 검증 요약 렌더링, 문제 파일 재백업 큐

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
        reportRunMessage(`명령을 복사하지 못했습니다: ${error.message}`, 'error');
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
                <div class="summary-value summary-value-detail">${escapeHtml(summary.manifest.filename)}</div>
                <div class="verify-help">${Number(summary.manifest.entries || 0)}건 · 형식 오류 ${parseErrors}줄</div>
            </div>
        `
        : '';
    const duration = formatDuration(Math.round(Number(summary.duration || 0) / 1000000000));
    const canRequeue = !!summary.requeue_allowed && Number(summary.requeue_eligible || 0) > 0;
    // 재백업이 차단된 사유별 안내: 해시 목록 형식 오류 외에도 도착 스캔 불완전 등으로
    // 서버가 requeue를 비활성화하면 "문제 0건" 같은 오해 소지 라벨 대신 사유를 안내한다.
    const requeueLabel = incomplete
        ? '불완전한 해시 목록에서는 재복사 예약을 할 수 없습니다.'
        : !canRequeue && Number(summary.problem_count || 0) > 0
            ? '재복사 예약을 할 수 없습니다. 경고와 문제 목록을 확인하세요.'
            : `문제 ${Number(summary.requeue_eligible || 0)}건, 다음 백업 때 다시 복사되게 하기`;

    summaryContent.innerHTML = `
        <div class="summary-section verify-problems">
            <div class="summary-grid">
                <div class="summary-item" data-type="info">
                    <div class="summary-label">출발</div>
                    <div class="summary-value summary-value-detail">${escapeHtml(summary.source)}</div>
                    <div class="verify-help">${Number(summary.source_files || 0)}개 · ${formatBytes(Number(summary.source_bytes || 0))}</div>
                </div>
                <div class="summary-item" data-type="info">
                    <div class="summary-label">도착</div>
                    <div class="summary-value summary-value-detail">${escapeHtml(summary.dest)}</div>
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
    setElementHidden(summarySection, false);
    addCompletionActions('verify');
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
    reportRunMessage(message, 'error');
}

