// Preset Management Module

let currentPresets = [];
let presetUsesPopover = false;

function getPresetElement(id) {
    return document.getElementById(id);
}

function getPresetInputValue(id, fallback = '') {
    const input = getPresetElement(id);
    return input ? input.value : fallback;
}

function getPresetCheckboxValue(id) {
    const input = getPresetElement(id);
    return Boolean(input?.checked);
}

function getPresetToggleButtons() {
    return document.querySelectorAll('[aria-controls="presetSidebar"]');
}

function isPresetSidebarOpen(sidebar) {
    if (!sidebar) return false;
    try {
        if (sidebar.matches(':popover-open')) return true;
    } catch (_) {
        // Popover selectors are not available in older browsers.
    }
    return sidebar.classList.contains('active');
}

function syncPresetSidebarState(isOpen) {
    const sidebar = getPresetElement('presetSidebar');
    const backdrop = getPresetElement('presetBackdrop');

    if (sidebar) {
        sidebar.classList.toggle('active', isOpen);
        sidebar.setAttribute('aria-hidden', String(!isOpen));
    }
    if (backdrop) backdrop.classList.toggle('active', isOpen && !presetUsesPopover);
    getPresetToggleButtons().forEach((button) => {
        button.setAttribute('aria-expanded', String(isOpen));
    });
}

function openPresetSidebar() {
    const sidebar = getPresetElement('presetSidebar');
    if (!sidebar) return;

    if (typeof sidebar.showPopover === 'function') {
        try {
            if (!sidebar.matches(':popover-open')) sidebar.showPopover();
            presetUsesPopover = true;
            syncPresetSidebarState(true);
            return;
        } catch (_) {
            // Keep the class-based panel working when Popover API setup is invalid.
            presetUsesPopover = false;
        }
    }
    syncPresetSidebarState(true);
}

function closePresetSidebar() {
    const sidebar = getPresetElement('presetSidebar');
    if (sidebar && typeof sidebar.hidePopover === 'function') {
        try {
            if (sidebar.matches(':popover-open')) sidebar.hidePopover();
        } catch (_) {
            // The fallback class state below remains usable.
        }
    }
    syncPresetSidebarState(false);
}

// Kept as the HTML event contract for the preset trigger and close button.
function togglePresetSidebar() {
    const sidebar = getPresetElement('presetSidebar');
    if (!sidebar) return;
    if (isPresetSidebarOpen(sidebar)) {
        closePresetSidebar();
    } else {
        openPresetSidebar();
    }
}

async function loadPresetList() {
    try {
        const response = await fetch('/api/presets');
        if (!response.ok) throw new Error('Failed to load presets');

        const presets = await response.json();
        currentPresets = Array.isArray(presets) ? presets : [];
    } catch (error) {
        console.error('프리셋 목록 로드 실패:', error);
        currentPresets = [];
    }
    renderPresetList();
}

function getPresetTags(preset) {
    const tags = [];
    if (preset.organize_strategy === 'date') tags.push('날짜별 정리');
    if (preset.organize_strategy === 'event') tags.push('이벤트별 정리');
    tags.push(preset.dedup_method === 'hash' ? '해시 중복 검사' : '이름+크기 검사');
    if (preset.dry_run) tags.push('시뮬레이션');
    if (preset.hash_verify) tags.push('해시 검증');
    if (preset.ignore_state) tags.push('이전 기록 무시');
    return tags;
}

function createPresetButton(action, presetName, label, title) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = `preset-card-btn ${action}`;
    button.dataset.action = action;
    button.dataset.preset = presetName;
    button.title = title;
    button.setAttribute('aria-label', title);
    button.textContent = label;
    return button;
}

function createPresetCard(preset) {
    const card = document.createElement('article');
    const presetName = typeof preset?.name === 'string' ? preset.name : '';
    card.className = 'preset-card';
    card.dataset.presetName = presetName;

    const header = document.createElement('div');
    header.className = 'preset-card-header';
    const title = document.createElement('div');
    title.className = 'preset-card-title';
    title.textContent = presetName || '이름 없는 프리셋';
    header.appendChild(title);

    const actions = document.createElement('div');
    actions.className = 'preset-card-actions';
    actions.append(
        createPresetButton('load', presetName, '↓', '프리셋 불러오기'),
        createPresetButton('delete', presetName, '×', '프리셋 삭제')
    );
    header.appendChild(actions);
    card.appendChild(header);

    if (preset?.description) {
        const description = document.createElement('div');
        description.className = 'preset-card-description';
        description.textContent = preset.description;
        card.appendChild(description);
    }

    const tags = getPresetTags(preset || {});
    if (tags.length > 0) {
        const meta = document.createElement('div');
        meta.className = 'preset-card-meta';
        tags.forEach((tag) => {
            const tagElement = document.createElement('span');
            tagElement.className = 'preset-card-tag';
            tagElement.textContent = tag;
            meta.appendChild(tagElement);
        });
        card.appendChild(meta);
    }
    return card;
}

function renderPresetList() {
    const presetList = getPresetElement('presetList');
    if (!presetList) return;

    presetList.replaceChildren();
    if (currentPresets.length === 0) {
        const empty = document.createElement('p');
        empty.className = 'preset-empty-message';
        empty.textContent = '저장된 프리셋이 없습니다';
        presetList.appendChild(empty);
        return;
    }
    currentPresets.forEach((preset) => presetList.appendChild(createPresetCard(preset)));
}

function setPresetFieldValue(id, value, fallback = '') {
    const input = getPresetElement(id);
    if (input) input.value = value ?? fallback;
}

function setPresetCheckboxValue(id, value) {
    const input = getPresetElement(id);
    if (input) input.checked = Boolean(value);
}

async function loadPresetByName(presetName) {
    try {
        const response = await fetch(`/api/presets/load?name=${encodeURIComponent(presetName)}`);
        if (!response.ok) throw new Error('Failed to load preset');

        const config = await response.json();
        if (config.source) setPresetFieldValue('source', config.source);
        if (config.dest) setPresetFieldValue('dest', config.dest);
        setPresetFieldValue('organizeStrategy', config.organize_strategy, 'date');
        setPresetFieldValue('eventName', config.event_name);
        setPresetFieldValue('conflictPolicy', config.conflict_policy, 'skip');
        setPresetFieldValue('dedupMethod', config.dedup_method, 'name-size');
        setPresetFieldValue('dateFilterStart', config.date_filter_start);
        setPresetFieldValue('dateFilterEnd', config.date_filter_end);
        setPresetFieldValue('jobs', config.jobs ?? 0);
        setPresetFieldValue('unclassifiedDir', config.unclassified_dir, 'unclassified');
        setPresetFieldValue('quarantineDir', config.quarantine_dir, 'quarantine');
        setPresetFieldValue('stateFile', config.state_file);
        setPresetFieldValue('logFile', config.log_file);
        setPresetCheckboxValue('dryRun', config.dry_run);
        setPresetCheckboxValue('hashVerify', config.hash_verify);
        setPresetCheckboxValue('ignoreState', config.ignore_state);
        setPresetCheckboxValue('logJson', config.log_json);

        if (Array.isArray(config.include_extensions) && typeof includeExtensions !== 'undefined') {
            includeExtensions = config.include_extensions;
            if (typeof renderExtensionTags === 'function') renderExtensionTags();
        }
        if (typeof toggleEventNameInput === 'function') toggleEventNameInput();
        if (typeof updateDateFilterButtons === 'function') updateDateFilterButtons();
        if (typeof updateBookmarkButtons === 'function') updateBookmarkButtons();
        // 프로그램적 값 대입은 change 이벤트를 내지 않으므로 실행 요약/버튼 라벨을 직접 갱신
        if (typeof updateRunConfigurationSummary === 'function') updateRunConfigurationSummary();
        if (typeof updateRunActionLabels === 'function') updateRunActionLabels();

        closePresetSidebar();
        showNotification(`프리셋 "${presetName}"을 불러왔습니다`, 'success');
    } catch (error) {
        console.error('프리셋 로드 실패:', error);
        showNotification('프리셋을 불러오는데 실패했습니다', 'error');
    }
}

function showSavePresetDialog() {
    const dialog = getPresetElement('savePresetDialog');
    if (!dialog) return;

    setPresetFieldValue('presetName');
    setPresetFieldValue('presetDescription');
    if (typeof dialog.showModal === 'function') {
        try {
            if (!dialog.open) dialog.showModal();
        } catch (_) {
            dialog.style.display = 'block';
        }
    } else {
        dialog.style.display = 'block';
        dialog.setAttribute('aria-hidden', 'false');
    }
    getPresetElement('presetName')?.focus();
}

function hideSavePresetDialog() {
    const dialog = getPresetElement('savePresetDialog');
    if (!dialog) return;

    if (typeof dialog.close === 'function' && dialog.open) {
        dialog.close();
    } else {
        dialog.style.display = 'none';
        dialog.setAttribute('aria-hidden', 'true');
    }
}

function buildPresetConfig() {
    const jobsValue = Number.parseInt(getPresetInputValue('jobs', '0'), 10);
    return {
        source: getPresetInputValue('source'),
        dest: getPresetInputValue('dest'),
        include_extensions: typeof includeExtensions !== 'undefined' ? includeExtensions : [],
        jobs: Number.isNaN(jobsValue) ? 0 : jobsValue,
        dedup_method: getPresetInputValue('dedupMethod', 'name-size'),
        conflict_policy: getPresetInputValue('conflictPolicy', 'skip'),
        organize_strategy: getPresetInputValue('organizeStrategy', 'date'),
        event_name: getPresetInputValue('eventName'),
        date_filter_start: getPresetInputValue('dateFilterStart'),
        date_filter_end: getPresetInputValue('dateFilterEnd'),
        unclassified_dir: getPresetInputValue('unclassifiedDir', 'unclassified') || 'unclassified',
        quarantine_dir: getPresetInputValue('quarantineDir', 'quarantine') || 'quarantine',
        state_file: getPresetInputValue('stateFile'),
        log_file: getPresetInputValue('logFile'),
        log_json: getPresetCheckboxValue('logJson'),
        dry_run: getPresetCheckboxValue('dryRun'),
        hash_verify: getPresetCheckboxValue('hashVerify'),
        ignore_state: getPresetCheckboxValue('ignoreState')
    };
}

async function savePreset() {
    const name = getPresetInputValue('presetName').trim();
    const description = getPresetInputValue('presetDescription').trim();
    if (!name) {
        showNotification('프리셋 이름을 입력해주세요', 'warning');
        getPresetElement('presetName')?.focus();
        return;
    }

    try {
        const response = await fetch('/api/presets', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name, description, config: buildPresetConfig() })
        });
        if (!response.ok) throw new Error('Failed to save preset');

        showNotification(`프리셋 "${name}"을 저장했습니다`, 'success');
        hideSavePresetDialog();
        loadPresetList();
    } catch (error) {
        console.error('프리셋 저장 실패:', error);
        showNotification('프리셋 저장에 실패했습니다', 'error');
    }
}

async function deletePresetByName(presetName) {
    if (!confirm(`"${presetName}" 프리셋을 정말 삭제하시겠습니까?`)) return;
    try {
        const response = await fetch(`/api/presets/delete?name=${encodeURIComponent(presetName)}`, {
            method: 'DELETE'
        });
        if (!response.ok) throw new Error('Failed to delete preset');

        showNotification(`프리셋 "${presetName}"을 삭제했습니다`, 'success');
        loadPresetList();
    } catch (error) {
        console.error('프리셋 삭제 실패:', error);
        showNotification('프리셋 삭제에 실패했습니다', 'error');
    }
}

function showNotification(message, type = 'info') {
    const existing = document.querySelector('.preset-notification');
    if (existing) existing.remove();

    const notification = document.createElement('div');
    notification.className = `preset-notification preset-notification-${type}`;
    notification.textContent = message;
    notification.setAttribute('role', 'status');
    document.body.appendChild(notification);

    setTimeout(() => notification.remove(), 3000);
}

function initPresetPanel() {
    const sidebar = getPresetElement('presetSidebar');
    const presetList = getPresetElement('presetList');
    const dialog = getPresetElement('savePresetDialog');
    const backdrop = getPresetElement('presetBackdrop');

    loadPresetList();
    syncPresetSidebarState(isPresetSidebarOpen(sidebar));

    if (sidebar) {
        sidebar.addEventListener('toggle', () => {
            presetUsesPopover = typeof sidebar.showPopover === 'function';
            syncPresetSidebarState(isPresetSidebarOpen(sidebar));
        });
    }
    if (dialog) {
        dialog.addEventListener('close', () => {
            dialog.style.display = '';
        });
    }
    if (presetList) {
        presetList.addEventListener('click', (event) => {
            const button = event.target.closest('.preset-card-btn');
            if (!button) return;
            if (button.dataset.action === 'load') loadPresetByName(button.dataset.preset);
            if (button.dataset.action === 'delete') deletePresetByName(button.dataset.preset);
        });
    }
    if (backdrop) backdrop.addEventListener('click', closePresetSidebar);

    document.addEventListener('click', (event) => {
        if (!sidebar || !isPresetSidebarOpen(sidebar)) return;
        if (sidebar.contains(event.target)) return;
        if ([...getPresetToggleButtons()].some((button) => button.contains(event.target))) return;
        closePresetSidebar();
    });
    document.addEventListener('keydown', (event) => {
        if (event.key === 'Escape' && isPresetSidebarOpen(sidebar)) closePresetSidebar();
    });
}

if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initPresetPanel);
} else {
    initPresetPanel();
}
