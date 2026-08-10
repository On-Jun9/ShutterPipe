// Run UI Module
// 실행 관련 화면 갱신: 버튼 라벨, 실행 구성 요약, 진행 판독, 상태 표시, 인라인 메시지

function firstExistingElement(ids) {
    for (const id of ids) {
        const element = document.getElementById(id);
        if (element) return element;
    }
    return null;
}

function setElementState(element, state) {
    if (!element) return;
    if (element.dataset) element.dataset.runState = state;
    if (element.classList) {
        ['idle', 'running', 'cancelling', 'complete', 'error'].forEach((name) => {
            element.classList.remove(`is-${name}`);
        });
        element.classList.add(`is-${state}`);
    }
}

function setElementHidden(element, hidden) {
    if (!element) return;
    element.hidden = hidden;
    if (hidden && element.setAttribute) element.setAttribute('hidden', '');
    if (!hidden && element.removeAttribute) element.removeAttribute('hidden');
}

function updateRunActionLabels() {
    const isVerifyMode = document.getElementById('mode-verify')?.checked;
    const dryRun = !!document.getElementById('dryRun')?.checked;
    const startBtn = document.getElementById('startBtn');
    const verifyBtn = document.getElementById('verifyBtn');
    const workspaceTitle = document.getElementById('workspaceTitle');
    const workspaceDescription = document.getElementById('workspaceDescription');

    // 이력 뷰가 열려 있으면 제목은 이력이 소유한다 (history.js가 복원 시점에 다시 호출)
    const historyViewOpen = document.body?.classList?.contains?.('history-panel-open');
    if (!historyViewOpen) {
        if (workspaceTitle) workspaceTitle.textContent = isVerifyMode ? '백업 검증' : '새 백업';
        if (workspaceDescription) {
            workspaceDescription.textContent = isVerifyMode
                ? '원본과 목적지의 파일을 다시 대조해 백업 상태를 진단합니다.'
                : '촬영일시 기준으로 사진과 영상을 안전하게 백업하고 분류합니다.';
        }
    }

    if (startBtn) {
        startBtn.textContent = dryRun && !isVerifyMode ? '시뮬레이션 시작' : '백업 시작';
    }
    if (verifyBtn) verifyBtn.textContent = '검증 시작';
}

function formatRoutePath(raw, fallback) {
    const value = String(raw || '').trim();
    if (!value) return fallback;
    const parts = value.replace(/\/+$/, '').split('/').filter(Boolean);
    if (parts.length <= 2) return value;
    return `…/${parts[parts.length - 2]}/${parts[parts.length - 1]}`;
}

function updateRunConfigurationSummary() {
    const summary = firstExistingElement([
        'runConfigurationSummary', 'runConfigSummary', 'runSummary', 'runBarSummary', 'run-summary'
    ]);
    if (!summary) return;

    const sourceRaw = document.getElementById('source')?.value?.trim();
    const destRaw = document.getElementById('dest')?.value?.trim();
    const source = sourceRaw || '원본 경로 미입력';
    const dest = destRaw || '목적지 경로 미입력';
    const start = document.getElementById('dateFilterStart')?.value;
    const end = document.getElementById('dateFilterEnd')?.value;
    const dateRange = start && end ? `${start} ~ ${end}` : start ? `${start} 이후` : end ? `${end} 이전` : '날짜 전체';
    const extensionCount = Array.isArray(includeExtensions) ? includeExtensions.length : 0;
    const dryRun = document.getElementById('dryRun')?.checked;

    summary.textContent = `${source} → ${dest} · ${dateRange} · 확장자 ${extensionCount}개${dryRun ? ' · 시뮬레이션' : ''}`;

    // 실행 패널의 구조화된 요약 행 갱신
    const setText = (id, text) => {
        const el = document.getElementById(id);
        if (el) el.textContent = text;
    };
    setText('runRouteFrom', formatRoutePath(sourceRaw, '원본 미입력'));
    setText('runRouteTo', formatRoutePath(destRaw, '목적지 미입력'));
    setText('runSummaryDate', dateRange === '날짜 전체' ? '전체' : dateRange);
    setText('runSummaryExt', `${extensionCount}개`);

    const strategy = document.getElementById('organizeStrategy')?.value;
    const eventName = document.getElementById('eventName')?.value?.trim();
    setText('runSummaryOrganize', strategy === 'event'
        ? (eventName ? `이벤트별 · ${eventName}` : '이벤트별')
        : '날짜별');
    setText('runSummaryRunKind', dryRun ? '시뮬레이션' : '실제 백업');

    const conflictLabels = { skip: '건너뛰기', rename: '이름 변경', overwrite: '덮어쓰기', quarantine: '격리' };
    const dedupLabels = { 'name-size': '이름+크기', hash: '해시' };
    const conflict = conflictLabels[document.getElementById('conflictPolicy')?.value] || '건너뛰기';
    const dedup = dedupLabels[document.getElementById('dedupMethod')?.value] || '이름+크기';
    setText('runSummaryPolicy', `${conflict} · ${dedup}`);

    setText('runSummaryVerifyMode', document.getElementById('verifyModeHash')?.checked
        ? '정밀 비교 (SHA-256)'
        : '빠른 비교 (이름+크기)');

    const isVerifyMode = document.getElementById('mode-verify')?.checked;
    setElementHidden(document.getElementById('runDryRunNote'), !(dryRun && !isVerifyMode));
}

function updateProgressReadout(update = {}) {
    const readout = firstExistingElement(['runReadout', 'progressReadout', 'run-readout']);
    if (!readout) return;

    const total = Number(update.total ?? update.source_total ?? update.scanned_total ?? 0);
    const done = Number(update.current ?? update.done ?? update.completed ?? 0);
    const percent = Number.isFinite(Number(update.percent))
        ? Math.max(0, Math.min(100, Math.round(Number(update.percent))))
        : total > 0 ? Math.max(0, Math.min(100, Math.round((done / total) * 100))) : 0;

    readout.textContent = `SRC ${Number.isFinite(total) && total > 0 ? total.toLocaleString() : '—'} · DONE ${Number.isFinite(done) && done > 0 ? done.toLocaleString() : '0'} · ${percent}%`;
    if (readout.setAttribute) readout.setAttribute('aria-label', `대상 ${total || 0}개 중 ${done || 0}개 완료, ${percent}%`);
    const progressBar = document.getElementById('progressBar');
    if (progressBar?.setAttribute) {
        progressBar.setAttribute('aria-valuenow', String(percent));
        progressBar.setAttribute('aria-valuetext', `${percent}% 완료`);
    }
}

// 진행 영역 머리말은 실행 상태를 따라간다. 정적 문자열로 두면 종료 후에도 RUNNING이 남는다.
const RUN_STATE_KICKERS = {
    idle: 'READY',
    running: 'RUNNING',
    cancelling: 'CANCELLING',
    complete: 'DONE',
    error: 'ERROR'
};

function setRunVisualState(state) {
    const form = firstExistingElement(['backupForm', 'runForm', 'backupWorkflow', 'runWorkflow']);
    const isActive = state === 'running' || state === 'cancelling';
    if (form) {
        setElementState(form, state);
        if ('inert' in form) form.inert = isActive;
        if (form.setAttribute) {
            form.setAttribute('aria-busy', String(isActive));
            form.setAttribute('aria-disabled', String(isActive));
        }
    }

    const progressSection = document.getElementById('progressSection');
    const summarySection = document.getElementById('summarySection');
    const actionArea = document.getElementById('runActionArea');
    const startBtn = document.getElementById('startBtn');
    const verifyBtn = document.getElementById('verifyBtn');
    const cancelBtn = document.getElementById('cancelBtn');
    const lamp = firstExistingElement(['runAccessLamp', 'accessLamp', 'access-lamp']);
    setElementState(progressSection, state);
    setElementState(summarySection, state);
    setElementState(actionArea, state);
    setElementState(lamp, state);
    setElementHidden(startBtn, isActive);
    setElementHidden(verifyBtn, isActive);
    setElementHidden(cancelBtn, !isActive);
    if (lamp?.setAttribute) lamp.setAttribute('aria-label', `실행 상태: ${state}`);
    const kicker = document.getElementById('progressKicker');
    if (kicker) kicker.textContent = RUN_STATE_KICKERS[state] || RUN_STATE_KICKERS.running;
    updateRunActionLabels();
}

function reportRunMessage(message, type = 'error', fieldId = null) {
    const inlineError = document.getElementById('runInlineError');
    // node:test의 최소 DOM은 요청한 모든 ID에 빈 객체를 만들므로, 실제 ID를 가진
    // 새 인라인 오류 영역일 때만 우선 사용하고 그 외에는 기존 progressText로 폴백한다.
    const target = inlineError?.id === 'runInlineError' ? inlineError : null;
    const progressText = document.getElementById('progressText');
    const messageTarget = target || progressText;
    if (messageTarget) {
        messageTarget.textContent = message;
        if (messageTarget.dataset) messageTarget.dataset.messageType = type;
        if (messageTarget.setAttribute) messageTarget.setAttribute('aria-live', 'assertive');
        setElementHidden(messageTarget, false);
    }
    if (target) setElementHidden(document.getElementById('progressSection'), false);
    if (fieldId) {
        const field = document.getElementById(fieldId);
        if (field) {
            if (field.setAttribute) field.setAttribute('aria-invalid', 'true');
            if (typeof field.focus === 'function') field.focus();
        }
    }
}

function clearRunMessage() {
    const inlineError = document.getElementById('runInlineError');
    if (inlineError?.id === 'runInlineError') {
        inlineError.textContent = '';
        setElementHidden(inlineError, true);
    }
}

function clearRunInputErrors() {
    ['source', 'dest'].forEach((id) => document.getElementById(id)?.removeAttribute?.('aria-invalid'));
}

function addCompletionActions(kind) {
    const actions = firstExistingElement(['runNextActions', 'summaryActions', 'completionActions', 'summary-actions']);
    const container = actions || document.getElementById('summaryContent');
    if (!container || !document.createElement || !container.appendChild) return;
    const existing = container.querySelector?.('[data-run-completion-actions]');
    if (existing) existing.remove?.();

    const actionGroup = document.createElement('div');
    actionGroup.dataset.runCompletionActions = 'true';
    actionGroup.className = 'summary-actions';
    const historyButton = document.createElement('button');
    historyButton.type = 'button';
    historyButton.className = 'btn-small';
    historyButton.textContent = '이력 보기';
    historyButton.onclick = () => {
        if (typeof toggleHistoryPanel === 'function') {
            toggleHistoryPanel();
        } else {
            document.getElementById('history-toggle-btn')?.click?.();
        }
    };
    actionGroup.appendChild(historyButton);

    if (kind !== 'verify') {
        const verifyButton = document.createElement('button');
        verifyButton.type = 'button';
        verifyButton.className = 'btn-small';
        verifyButton.textContent = '검증으로 이동';
        verifyButton.onclick = () => {
            const verifyMode = document.getElementById('mode-verify');
            if (!verifyMode) return;
            verifyMode.checked = true;
            if (typeof Event === 'function' && typeof verifyMode.dispatchEvent === 'function') {
                verifyMode.dispatchEvent(new Event('change', { bubbles: true }));
            }
            updateRunActionLabels();
        };
        actionGroup.appendChild(verifyButton);
    }
    container.appendChild(actionGroup);
    setElementHidden(actions, false);
}

// isSocketOpen은 ws가 실제로 열려 있는지 판정한다. connectWebSocket은 연결 전에
// ws에 socket을 대입하고 실패 시 onerror에서 reject하지만 ws=null 정리는 이후
// onclose에서 하므로, 단순 `if (ws)`는 CONNECTING/CLOSING/CLOSED socket을 연결됨으로
// 오판한다. terminal 이벤트를 받을 수 있는 상태는 OPEN뿐이다.
