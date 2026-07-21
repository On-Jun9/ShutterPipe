// Backup History Management

let currentHistory = [];
let currentFilter = 'all'; // 'all', 'dry-run', 'real', 'verify'

// Load backup history list from server
async function loadHistoryList() {
    const history = await loadBackupHistoryFromServer(50);
    if (!history) {
        console.error('백업 이력 로드 실패');
        return;
    }

    currentHistory = history.entries || [];
    applyFilter();
}

// Apply current filter
function applyFilter() {
    let filtered = currentHistory;

    if (currentFilter === 'dry-run') {
        filtered = currentHistory.filter(entry =>
            !isVerifyHistoryEntry(entry) && entry.config?.dry_run === true
        );
    } else if (currentFilter === 'real') {
        filtered = currentHistory.filter(entry =>
            !isVerifyHistoryEntry(entry) && entry.config?.dry_run === false
        );
    } else if (currentFilter === 'verify') {
        filtered = currentHistory.filter(isVerifyHistoryEntry);
    }

    renderHistoryList(filtered);
    updateFilterButtons();
}

// Set filter
function setHistoryFilter(filter) {
    currentFilter = filter;
    applyFilter();
}

// Update filter button states
function updateFilterButtons() {
    const buttons = document.querySelectorAll('.history-filter-btn');
    buttons.forEach(btn => {
        if (btn.dataset.filter === currentFilter) {
            btn.classList.add('active');
        } else {
            btn.classList.remove('active');
        }
    });
}

// Render history list in sidebar
function renderHistoryList(entries) {
    const container = document.getElementById('history-list');
    if (!container) return;

    if (entries.length === 0) {
        container.innerHTML = '<div class="no-history">백업 이력이 없습니다</div>';
        return;
    }

    container.innerHTML = entries.map(entry =>
        isVerifyHistoryEntry(entry)
            ? renderVerifyHistoryCard(entry)
            : renderBackupHistoryCard(entry)
    ).join('');
}

// Entries written before run kinds were introduced are backup entries.
function isVerifyHistoryEntry(entry) {
    return entry?.kind === 'verify';
}

function getHistoryStatus(status) {
    if (status === 'success') {
        return { icon: '✓', className: 'status-success', text: '성공' };
    }
    if (status === 'canceled') {
        return { icon: '■', className: 'status-canceled', text: '취소됨' };
    }
    return { icon: '✗', className: 'status-failed', text: '실패' };
}

function getHistoryTimestamp(value) {
    const date = value ? new Date(value) : null;
    if (!date || Number.isNaN(date.getTime())) {
        return { date: '-', time: '', text: '-' };
    }

    const dateText = date.toLocaleDateString('ko-KR');
    const timeText = date.toLocaleTimeString('ko-KR', {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit'
    });
    return { date: dateText, time: timeText, text: `${dateText} ${timeText}` };
}

function getHistoryNumber(value) {
    const number = Number(value);
    return Number.isFinite(number) && number >= 0 ? number : 0;
}

function renderBackupHistoryCard(entry) {
    const summary = entry.summary || {};
    const config = entry.config || {};
    const status = getHistoryStatus(entry.status);
    const startedAt = getHistoryTimestamp(summary.StartTime || entry.created_at);

    // Format paths (show last 2 components) and escape for XSS prevention
    const sourceEscaped = escapeHtml(config.source);
    const destEscaped = escapeHtml(config.dest);
    const sourcePath = formatShortPath(config.source);
    const destPath = formatShortPath(config.dest);
    const dryRunBadge = config.dry_run ? '<span class="dry-run-badge">시뮬레이션</span>' : '';

    // Convert duration from nanoseconds to seconds
    const durationSeconds = Math.round(getHistoryNumber(summary.Duration) / 1000000000);

    return `
        <div class="history-card">
            <div class="history-header">
                <span class="history-date">${startedAt.date} ${startedAt.time}</span>
                <div class="history-badges">
                    <span class="history-status ${status.className}">[${status.icon} ${status.text}]</span>
                    ${dryRunBadge}
                </div>
            </div>
            <div class="history-paths">
                <div class="history-path" title="${sourceEscaped}">${escapeHtml(sourcePath)}</div>
                <div class="history-arrow">→</div>
                <div class="history-path" title="${destEscaped}">${escapeHtml(destPath)}</div>
            </div>
            <div class="history-stats">
                <div class="stat-group">
                    <span class="stat-label">복사:</span>
                    <span class="stat-value">${getHistoryNumber(summary.Copied)}</span>
                </div>
                <div class="stat-group">
                    <span class="stat-label">스킵:</span>
                    <span class="stat-value">${getHistoryNumber(summary.Skipped)}</span>
                </div>
                <div class="stat-group">
                    <span class="stat-label">실패:</span>
                    <span class="stat-value">${getHistoryNumber(summary.Failed)}</span>
                </div>
            </div>
            <div class="history-stats">
                <div class="stat-group">
                    <span class="stat-label">분류불가:</span>
                    <span class="stat-value">${getHistoryNumber(summary.Unclassified)}</span>
                </div>
                <div class="stat-group">
                    <span class="stat-label">소요시간:</span>
                    <span class="stat-value">${formatDuration(durationSeconds)}</span>
                </div>
            </div>
            <div class="history-throughput">
                ${formatBytes(getHistoryNumber(summary.BytesCopied))}
                (약 ${formatSpeed(getHistoryNumber(summary.BytesPerSecond))})
            </div>
        </div>
    `;
}

function renderVerifyHistoryCard(entry) {
    const summary = entry.verify_summary || {};
    const status = getHistoryStatus(entry.status);
    const startedAt = getHistoryTimestamp(summary.start_time || entry.created_at);
    const endedAt = getHistoryTimestamp(summary.end_time);
    const source = typeof summary.source === 'string' ? summary.source : '';
    const dest = typeof summary.dest === 'string' ? summary.dest : '';
    const mode = summary.mode === 'hash' ? '해시 검증' : (summary.mode === 'quick' ? '빠른 검증' : '검증');
    const durationSeconds = Math.round(getHistoryNumber(summary.duration) / 1000000000);

    return `
        <div class="history-card">
            <div class="history-header">
                <span class="history-date">${startedAt.date} ${startedAt.time}</span>
                <div class="history-badges">
                    <span class="history-status ${status.className}">[${status.icon} ${status.text}]</span>
                    <span class="dry-run-badge">${escapeHtml(mode)}</span>
                </div>
            </div>
            <div class="history-paths">
                <div class="history-path" title="${escapeHtml(source)}">${escapeHtml(formatShortPath(source))}</div>
                <div class="history-arrow">→</div>
                <div class="history-path" title="${escapeHtml(dest)}">${escapeHtml(formatShortPath(dest))}</div>
            </div>
            <div class="history-stats">
                <div class="stat-group">
                    <span class="stat-label">정상:</span>
                    <span class="stat-value">${getHistoryNumber(summary.normal)}</span>
                </div>
                <div class="stat-group">
                    <span class="stat-label">누락:</span>
                    <span class="stat-value">${getHistoryNumber(summary.missing)}</span>
                </div>
                <div class="stat-group">
                    <span class="stat-label">불일치:</span>
                    <span class="stat-value">${getHistoryNumber(summary.mismatch)}</span>
                </div>
                <div class="stat-group">
                    <span class="stat-label">검증불가:</span>
                    <span class="stat-value">${getHistoryNumber(summary.unverifiable)}</span>
                </div>
            </div>
            <div class="history-stats">
                <div class="stat-group">
                    <span class="stat-label">원본:</span>
                    <span class="stat-value">${getHistoryNumber(summary.source_files)}개 / ${formatBytes(getHistoryNumber(summary.source_bytes))}</span>
                </div>
                <div class="stat-group">
                    <span class="stat-label">소요시간:</span>
                    <span class="stat-value">${formatDuration(durationSeconds)}</span>
                </div>
            </div>
            <div class="history-throughput">
                완료: ${endedAt.text}
            </div>
        </div>
    `;
}

// Format path to show last 2 components
function formatShortPath(path) {
    const safePath = typeof path === 'string' ? path : '';
    const parts = safePath.split('/').filter(p => p);
    if (parts.length <= 2) return safePath;
    return '.../' + parts.slice(-2).join('/');
}

// Toggle history sidebar
function toggleHistoryPanel() {
    const sidebar = document.getElementById('history-sidebar');
    const backdrop = document.getElementById('history-backdrop');

    if (!sidebar || !backdrop) return;

    const isOpen = sidebar.classList.contains('open');

    if (isOpen) {
        sidebar.classList.remove('open');
        backdrop.classList.remove('show');
    } else {
        sidebar.classList.add('open');
        backdrop.classList.add('show');
        loadHistoryList();
    }
}

// Initialize history panel
function initHistoryPanel() {
    const toggleBtn = document.getElementById('history-toggle-btn');
    const backdrop = document.getElementById('history-backdrop');
    const closeBtn = document.getElementById('history-close-btn');

    if (toggleBtn) {
        toggleBtn.addEventListener('click', toggleHistoryPanel);
    }

    if (backdrop) {
        backdrop.addEventListener('click', toggleHistoryPanel);
    }

    if (closeBtn) {
        closeBtn.addEventListener('click', toggleHistoryPanel);
    }
}

// Initialize on page load
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initHistoryPanel);
} else {
    initHistoryPanel();
}
