// Backup History Management

let currentHistory = [];
let currentFilter = 'all'; // 'all', 'dry-run', 'real'

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
        filtered = currentHistory.filter(entry => entry.config.dry_run === true);
    } else if (currentFilter === 'real') {
        filtered = currentHistory.filter(entry => entry.config.dry_run === false);
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

    container.innerHTML = entries.map(entry => {
        const summary = entry.summary;
        const config = entry.config;
        const status = entry.status;

        // Format date and time
        const date = new Date(summary.StartTime);
        const dateStr = date.toLocaleDateString('ko-KR');
        const timeStr = date.toLocaleTimeString('ko-KR', { hour: '2-digit', minute: '2-digit', second: '2-digit' });

        // Format paths (show last 2 components) and escape for XSS prevention
        const sourceEscaped = escapeHtml(config.source);
        const destEscaped = escapeHtml(config.dest);
        const sourcePath = formatShortPath(config.source);
        const destPath = formatShortPath(config.dest);

        // Status icon and dry run badge
        const statusIcon = status === 'success' ? '✓' : '✗';
        const statusClass = status === 'success' ? 'status-success' : 'status-failed';
        const statusText = status === 'success' ? '성공' : '실패';
        const dryRunBadge = config.dry_run ? '<span class="dry-run-badge">시뮬레이션</span>' : '';

        // Convert duration from nanoseconds to seconds
        const durationSeconds = Math.round(summary.Duration / 1000000000);

        return `
            <div class="history-card">
                <div class="history-header">
                    <span class="history-date">${dateStr} ${timeStr}</span>
                    <div class="history-badges">
                        <span class="history-status ${statusClass}">[${statusIcon} ${statusText}]</span>
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
                        <span class="stat-value">${summary.Copied}</span>
                    </div>
                    <div class="stat-group">
                        <span class="stat-label">스킵:</span>
                        <span class="stat-value">${summary.Skipped}</span>
                    </div>
                    <div class="stat-group">
                        <span class="stat-label">실패:</span>
                        <span class="stat-value">${summary.Failed}</span>
                    </div>
                </div>
                <div class="history-stats">
                    <div class="stat-group">
                        <span class="stat-label">분류불가:</span>
                        <span class="stat-value">${summary.Unclassified}</span>
                    </div>
                    <div class="stat-group">
                        <span class="stat-label">소요시간:</span>
                        <span class="stat-value">${formatDuration(durationSeconds)}</span>
                    </div>
                </div>
                <div class="history-throughput">
                    ${formatBytes(summary.BytesCopied)} (약 ${formatSpeed(summary.BytesPerSecond)})
                </div>
            </div>
        `;
    }).join('');
}

// Format path to show last 2 components
function formatShortPath(path) {
    const parts = path.split('/').filter(p => p);
    if (parts.length <= 2) return path;
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
