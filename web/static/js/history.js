// Backup History Management

let currentHistory = [];
let currentFilter = 'all';
let previousHistoryFocus = null;

// 서버 저장소가 최근 100건만 보관하므로 100건 조회 = 전체 이력
const HISTORY_PAGE_SIZE = 10;
let historyPage = 1;

async function loadHistoryList() {
    const history = await loadBackupHistoryFromServer(100);
    if (!history) {
        console.error('백업 이력 로드 실패');
        return;
    }
    currentHistory = Array.isArray(history.entries) ? history.entries : [];
    applyFilter();
}

function getHistoryEntryTime(entry) {
    const summary = isVerifyHistoryEntry(entry) ? entry?.verify_summary : entry?.summary;
    const value = summary?.start_time || summary?.StartTime || entry?.created_at;
    const time = new Date(value).getTime();
    return Number.isFinite(time) ? time : 0;
}

function applyFilter() {
    let filtered = currentHistory;
    if (currentFilter === 'dry-run') {
        filtered = currentHistory.filter((entry) => !isVerifyHistoryEntry(entry) && entry.config?.dry_run === true);
    } else if (currentFilter === 'real') {
        filtered = currentHistory.filter((entry) => !isVerifyHistoryEntry(entry) && entry.config?.dry_run === false);
    } else if (currentFilter === 'verify') {
        filtered = currentHistory.filter(isVerifyHistoryEntry);
    }
    const sorted = [...filtered].sort((a, b) => getHistoryEntryTime(b) - getHistoryEntryTime(a));
    const totalPages = Math.max(1, Math.ceil(sorted.length / HISTORY_PAGE_SIZE));
    if (historyPage > totalPages) historyPage = totalPages;
    if (historyPage < 1) historyPage = 1;
    const start = (historyPage - 1) * HISTORY_PAGE_SIZE;
    renderHistoryList(sorted.slice(start, start + HISTORY_PAGE_SIZE));
    renderHistoryPagination(sorted.length, totalPages);
    updateFilterButtons();
}

function setHistoryFilter(filter) {
    currentFilter = filter;
    historyPage = 1;
    applyFilter();
}

function setHistoryPage(page) {
    historyPage = page;
    applyFilter();
    window.scrollTo?.({ top: 0 });
}

function renderHistoryPagination(totalCount, totalPages) {
    const container = document.getElementById('history-pagination');
    if (!container) return;
    container.hidden = totalCount <= HISTORY_PAGE_SIZE;
    if (container.hidden) return;
    container.replaceChildren();

    const prev = createHistoryElement('button', 'btn-small', '이전');
    prev.type = 'button';
    prev.disabled = historyPage <= 1;
    prev.addEventListener('click', () => setHistoryPage(historyPage - 1));

    const label = createHistoryElement('span', 'history-page-label', `${historyPage} / ${totalPages}`);

    const next = createHistoryElement('button', 'btn-small', '다음');
    next.type = 'button';
    next.disabled = historyPage >= totalPages;
    next.addEventListener('click', () => setHistoryPage(historyPage + 1));

    container.append(prev, label, next);
}

function updateFilterButtons() {
    document.querySelectorAll('.history-filter-btn').forEach((button) => {
        const isActive = button.dataset.filter === currentFilter;
        button.classList.toggle('active', isActive);
        button.setAttribute('aria-pressed', String(isActive));
    });
}

function createHistoryElement(tag, className, text) {
    const element = document.createElement(tag);
    if (className) element.className = className;
    if (text !== undefined) element.textContent = text;
    return element;
}

function getHistoryNumber(value) {
    const number = Number(value);
    return Number.isFinite(number) && number >= 0 ? number : 0;
}

function getHistoryStatus(status) {
    if (status === 'success') return { icon: '✓', className: 'status-success', text: '성공' };
    if (status === 'canceled') return { icon: '■', className: 'status-canceled', text: '취소됨' };
    return { icon: '✗', className: 'status-failed', text: '실패' };
}

function getHistoryTimestamp(value) {
    const date = value ? new Date(value) : null;
    if (!date || Number.isNaN(date.getTime())) return { date: '-', time: '', text: '-' };
    const dateText = date.toLocaleDateString('ko-KR');
    const timeText = date.toLocaleTimeString('ko-KR', {
        hour: '2-digit', minute: '2-digit', second: '2-digit'
    });
    return { date: dateText, time: timeText, text: `${dateText} ${timeText}` };
}

function formatHistoryBytes(value) {
    const bytes = getHistoryNumber(value);
    return typeof formatBytes === 'function' ? formatBytes(bytes) : `${bytes} B`;
}

function formatHistoryDuration(value) {
    const seconds = Math.round(getHistoryNumber(value) / 1000000000);
    return typeof formatDuration === 'function' ? formatDuration(seconds) : `${seconds}초`;
}

function formatHistorySpeed(value) {
    const speed = getHistoryNumber(value);
    return typeof formatSpeed === 'function' ? formatSpeed(speed) : `${speed} B/s`;
}

function formatShortPath(path) {
    const safePath = typeof path === 'string' ? path : '';
    const parts = safePath.split('/').filter(Boolean);
    return parts.length <= 2 ? safePath : `.../${parts.slice(-2).join('/')}`;
}

function createBadge(className, text) {
    return createHistoryElement('span', className, text);
}

function createStat(label, value) {
    const group = createHistoryElement('div', 'stat-group');
    group.append(
        createHistoryElement('span', 'stat-label', `${label}:`),
        createHistoryElement('span', 'stat-value', value)
    );
    return group;
}

function createHistoryHeader(kind, status, timestamp, extraBadge = '') {
    const header = createHistoryElement('div', 'history-header');
    header.appendChild(createHistoryElement('span', 'history-date', timestamp.text));
    const badges = createHistoryElement('div', 'history-badges');
    badges.append(
        createBadge('history-kind', kind),
        createBadge(`history-status ${status.className}`, `${status.icon} ${status.text}`)
    );
    if (extraBadge) badges.appendChild(createBadge('dry-run-badge', extraBadge));
    header.appendChild(badges);
    return header;
}

function createHistoryPaths(source, dest) {
    const paths = createHistoryElement('div', 'history-paths');
    const sourcePath = createHistoryElement('div', 'history-path', formatShortPath(source));
    const destPath = createHistoryElement('div', 'history-path', formatShortPath(dest));
    sourcePath.title = typeof source === 'string' ? source : '';
    destPath.title = typeof dest === 'string' ? dest : '';
    paths.append(sourcePath, createHistoryElement('div', 'history-arrow', '→'), destPath);
    return paths;
}

function addDetailItem(container, label, value) {
    const row = createHistoryElement('div', 'history-detail-row');
    row.append(
        createHistoryElement('dt', 'history-detail-label', label),
        createHistoryElement('dd', 'history-detail-value', value)
    );
    container.appendChild(row);
}

function createHistoryDetails(config, statistics, problems = [], warnings = []) {
    const details = createHistoryElement('details', 'history-details');
    details.appendChild(createHistoryElement('summary', '', '설정과 전체 결과 보기'));

    const configTitle = createHistoryElement('h3', 'history-detail-title', '실행 설정');
    const configList = createHistoryElement('dl', 'history-detail-list');
    const configItems = [
        ['원본', config?.source || '-'], ['목적지', config?.dest || '-'],
        ['날짜 범위', `${config?.date_filter_start || '전체'} ~ ${config?.date_filter_end || '전체'}`],
        ['정리 방식', config?.organize_strategy === 'event' ? '이벤트별' : '날짜별'],
        ['충돌 처리', config?.conflict_policy || 'skip'], ['중복 검사', config?.dedup_method || 'name-size'],
        ['병렬 워커', getHistoryNumber(config?.jobs) || '자동'],
        ['포함 확장자', Array.isArray(config?.include_extensions) ? config.include_extensions.join(', ') || '-' : '-'],
        ['분류 불가 폴더', config?.unclassified_dir || 'unclassified'],
        ['격리 폴더', config?.quarantine_dir || 'quarantine'],
        ['시뮬레이션', config?.dry_run ? '사용' : '사용 안 함'],
        ['해시 검증', config?.hash_verify ? '사용' : '사용 안 함'],
        ['이전 기록 무시', config?.ignore_state ? '사용' : '사용 안 함']
    ];
    if (config?.event_name) configItems.splice(3, 0, ['이벤트명', config.event_name]);
    configItems.forEach(([label, value]) => addDetailItem(configList, label, String(value)));
    details.append(configTitle, configList);

    const statsTitle = createHistoryElement('h3', 'history-detail-title', '전체 통계');
    const statsList = createHistoryElement('dl', 'history-detail-list');
    statistics.forEach(([label, value]) => addDetailItem(statsList, label, String(value)));
    details.append(statsTitle, statsList);

    if (problems.length > 0) {
        const problemTitle = createHistoryElement('h3', 'history-detail-title', '문제 항목');
        const problemList = createHistoryElement('ul', 'history-problem-list');
        problems.forEach((problem) => {
            const label = [
                problem?.verdict,
                problem?.name,
                problem?.source_path,
                Number.isFinite(Number(problem?.size)) ? formatHistoryBytes(problem.size) : '',
                problem?.reason
            ]
                .filter(Boolean)
                .join(' · ');
            problemList.appendChild(createHistoryElement('li', '', label));
        });
        details.append(problemTitle, problemList);
    }
    if (warnings.length > 0) {
        const warningTitle = createHistoryElement('h3', 'history-detail-title', '경고');
        const warningList = createHistoryElement('ul', 'history-warning-list');
        warnings.forEach((warning) => warningList.appendChild(createHistoryElement('li', '', String(warning))));
        details.append(warningTitle, warningList);
    }
    return details;
}

function renderBackupHistoryCard(entry) {
    const summary = entry?.summary || {};
    const config = entry?.config || {};
    const status = getHistoryStatus(entry?.status);
    const timestamp = getHistoryTimestamp(summary.StartTime || entry?.created_at);
    const card = createHistoryElement('article', 'history-card');
    card.appendChild(createHistoryHeader('백업', status, timestamp, config.dry_run ? '시뮬레이션' : '실제 백업'));
    card.appendChild(createHistoryPaths(config.source, config.dest));

    const primaryStats = createHistoryElement('div', 'history-stats');
    primaryStats.append(
        createStat('복사', getHistoryNumber(summary.Copied)),
        createStat('스킵', getHistoryNumber(summary.Skipped)),
        createStat('실패', getHistoryNumber(summary.Failed))
    );
    card.appendChild(primaryStats);
    card.appendChild(createHistoryElement(
        'div', 'history-throughput',
        `${formatHistoryBytes(summary.BytesCopied)} · ${formatHistorySpeed(summary.BytesPerSecond)} · ${formatHistoryDuration(summary.Duration)}`
    ));
    card.appendChild(createHistoryDetails(config, [
        ['스캔', getHistoryNumber(summary.ScannedFiles)], ['대상', getHistoryNumber(summary.TotalFiles)],
        ['복사', getHistoryNumber(summary.Copied)], ['스킵', getHistoryNumber(summary.Skipped)],
        ['이름 변경', getHistoryNumber(summary.Renamed)], ['덮어씀', getHistoryNumber(summary.Overwritten)],
        ['격리', getHistoryNumber(summary.Quarantined)], ['실패', getHistoryNumber(summary.Failed)],
        ['분류 불가', getHistoryNumber(summary.Unclassified)], ['복사량', formatHistoryBytes(summary.BytesCopied)],
        ['평균 속도', formatHistorySpeed(summary.BytesPerSecond)], ['시작 시각', getHistoryTimestamp(summary.StartTime).text],
        ['완료 시각', getHistoryTimestamp(summary.EndTime).text], ['소요 시간', formatHistoryDuration(summary.Duration)]
    ], [], Array.isArray(summary.Warnings) ? summary.Warnings : []));
    return card;
}

function renderVerifyHistoryCard(entry) {
    const summary = entry?.verify_summary || {};
    const config = entry?.config || {};
    const status = getHistoryStatus(entry?.status);
    const timestamp = getHistoryTimestamp(summary.start_time || entry?.created_at);
    const mode = summary.mode === 'hash' ? '해시 검증' : summary.mode === 'quick' ? '빠른 검증' : '검증';
    const card = createHistoryElement('article', 'history-card');
    card.appendChild(createHistoryHeader('검증', status, timestamp, mode));
    card.appendChild(createHistoryPaths(summary.source || config.source, summary.dest || config.dest));

    const primaryStats = createHistoryElement('div', 'history-stats');
    primaryStats.append(
        createStat('정상', getHistoryNumber(summary.normal)),
        createStat('누락', getHistoryNumber(summary.missing)),
        createStat('불일치', getHistoryNumber(summary.mismatch))
    );
    card.appendChild(primaryStats);
    card.appendChild(createHistoryElement(
        'div', 'history-throughput',
        `${getHistoryNumber(summary.problem_count)}개 문제 · ${formatHistoryDuration(summary.duration)}`
    ));
    card.appendChild(createHistoryDetails({
        ...config,
        source: summary.source || config.source,
        dest: summary.dest || config.dest
    }, [
        ['대상 파일', getHistoryNumber(summary.source_files)], ['대상 용량', formatHistoryBytes(summary.source_bytes)],
        ['정상', getHistoryNumber(summary.normal)], ['누락', getHistoryNumber(summary.missing)],
        ['불일치', getHistoryNumber(summary.mismatch)], ['검증 불가', getHistoryNumber(summary.unverifiable)],
        ['문제 수', getHistoryNumber(summary.problem_count)], ['재백업 가능', summary.requeue_allowed ? '가능' : '불가'],
        ['재백업 대상', getHistoryNumber(summary.requeue_eligible)], ['완료 시각', getHistoryTimestamp(summary.end_time).text],
        ['소요 시간', formatHistoryDuration(summary.duration)],
        ['매니페스트', summary.manifest ? `${summary.manifest.filename || '-'} · ${getHistoryNumber(summary.manifest.entries)}개` : '사용 안 함'],
        ['불완전한 매니페스트', summary.incomplete_manifest ? '예' : '아니요']
    ], Array.isArray(summary.problems) ? summary.problems : [], [
        ...(Array.isArray(summary.warnings) ? summary.warnings : []),
        ...(summary.problems_truncated ? ['문제 목록이 일부만 저장되었습니다'] : [])
    ]));
    return card;
}

function renderHistoryList(entries) {
    const container = document.getElementById('history-list');
    if (!container) return;
    container.replaceChildren();
    if (entries.length === 0) {
        container.appendChild(createHistoryElement('div', 'no-history', '백업 이력이 없습니다'));
        return;
    }
    entries.forEach((entry) => {
        container.appendChild(isVerifyHistoryEntry(entry) ? renderVerifyHistoryCard(entry) : renderBackupHistoryCard(entry));
    });
}

function isVerifyHistoryEntry(entry) {
    return entry?.kind === 'verify';
}

function getHistoryToggleButtons() {
    return document.querySelectorAll('#history-toggle-btn, [aria-controls="history-sidebar"]');
}

function isHistoryPanelOpen() {
    return document.getElementById('history-sidebar')?.classList.contains('open') || false;
}

function syncHistoryPanelState(isOpen) {
    const sidebar = document.getElementById('history-sidebar');
    if (sidebar) {
        sidebar.classList.toggle('open', isOpen);
        sidebar.setAttribute('aria-hidden', String(!isOpen));
        sidebar.inert = !isOpen;
    }
    getHistoryToggleButtons().forEach((button) => button.setAttribute('aria-expanded', String(isOpen)));
    document.body.classList.toggle('history-panel-open', isOpen);

    // 세 탭이 같은 워크스페이스 제목을 공유한다
    const title = document.getElementById('workspaceTitle');
    const description = document.getElementById('workspaceDescription');
    if (isOpen) {
        if (title) title.textContent = '백업 이력';
        if (description) description.textContent = '과거 실행 기록을 최신순으로 확인합니다.';
    } else if (typeof updateRunActionLabels === 'function') {
        updateRunActionLabels();
    }
}

function openHistoryPanel() {
    if (isHistoryPanelOpen()) return;
    previousHistoryFocus = document.activeElement;
    syncHistoryPanelState(true);
    loadHistoryList();
    document.querySelector('.history-filter-btn.active')?.focus?.();
}

function closeHistoryPanel() {
    if (!isHistoryPanelOpen()) return;
    syncHistoryPanelState(false);
    previousHistoryFocus?.focus?.();
    previousHistoryFocus = null;
}

function toggleHistoryPanel() {
    if (isHistoryPanelOpen()) closeHistoryPanel();
    else openHistoryPanel();
}

function initHistoryPanel() {
    const filterContainer = document.querySelector('.history-filters');
    syncHistoryPanelState(isHistoryPanelOpen());

    // 탭 동작: 이력 버튼은 열기 전용, 새 백업/검증 탭을 누르면 이력 뷰가 닫힌다
    getHistoryToggleButtons().forEach((button) => button.addEventListener('click', openHistoryPanel));
    document.querySelectorAll('#run-mode-tabs .tab-label').forEach((label) => {
        label.addEventListener('click', closeHistoryPanel);
    });
    if (filterContainer) {
        filterContainer.addEventListener('click', (event) => {
            const button = event.target.closest('.history-filter-btn');
            if (button?.dataset.filter) setHistoryFilter(button.dataset.filter);
        });
    }
    document.addEventListener('keydown', (event) => {
        if (event.key === 'Escape' && isHistoryPanelOpen()) closeHistoryPanel();
    });
}

if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initHistoryPanel);
} else {
    initHistoryPanel();
}
