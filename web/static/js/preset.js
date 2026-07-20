// Preset Management Module
// 설정 프리셋 관리 기능

let currentPresets = [];
let selectedPreset = null;

// 사이드바 토글
function togglePresetSidebar() {
    const sidebar = document.getElementById('presetSidebar');
    const backdrop = document.getElementById('presetBackdrop');

    sidebar.classList.toggle('active');
    backdrop.classList.toggle('active');
}

// 페이지 로드 시 프리셋 목록 불러오기
async function loadPresetList() {
    try {
        const response = await fetch('/api/presets');
        if (!response.ok) {
            throw new Error('Failed to load presets');
        }

        currentPresets = await response.json();
        if (!currentPresets) {
            currentPresets = [];
        }
        renderPresetList();
    } catch (error) {
        console.error('프리셋 목록 로드 실패:', error);
        currentPresets = [];
        renderPresetList();
    }
}

// 프리셋 목록 렌더링
function renderPresetList() {
    const presetList = document.getElementById('presetList');
    if (!presetList) return;

    if (!currentPresets || currentPresets.length === 0) {
        presetList.innerHTML = '<p class="preset-empty-message">저장된 프리셋이 없습니다</p>';
        return;
    }

    presetList.innerHTML = currentPresets.map(preset => {
        const tags = [];

        // 분류 방식
        if (preset.organize_strategy === 'date') {
            tags.push('날짜별');
        } else if (preset.organize_strategy === 'event') {
            tags.push('이벤트별');
        }

        // 중복 검사 방식
        if (preset.dedup_method === 'hash') {
            tags.push('해시 검사');
        } else {
            tags.push('이름+크기');
        }

        // 드라이런
        if (preset.dry_run) {
            tags.push('드라이런');
        }

        // 이전 기록 무시
        if (preset.ignore_state) {
            tags.push('이전 기록 무시');
        }

        const escapedName = escapeHtml(preset.name);
        const escapedDesc = escapeHtml(preset.description);
        // Use data attributes and event delegation instead of inline onclick for better security
        return `
            <div class="preset-card" data-preset-name="${escapedName}">
                <div class="preset-card-header">
                    <div class="preset-card-title">${escapedName}</div>
                    <div class="preset-card-actions">
                        <button class="preset-card-btn load" data-action="load" data-preset="${escapedName}" title="불러오기">
                            ↓
                        </button>
                        <button class="preset-card-btn delete" data-action="delete" data-preset="${escapedName}" title="삭제">
                            ×
                        </button>
                    </div>
                </div>
                ${preset.description ? `<div class="preset-card-description">${escapedDesc}</div>` : ''}
                <div class="preset-card-meta">
                    ${tags.map(tag => `<span class="preset-card-tag">${escapeHtml(tag)}</span>`).join('')}
                </div>
            </div>
        `;
    }).join('');
}

// 프리셋 불러오기 (이름으로)
async function loadPresetByName(presetName) {
    try {
        const response = await fetch(`/api/presets/load?name=${encodeURIComponent(presetName)}`);
        if (!response.ok) {
            throw new Error('Failed to load preset');
        }

        const config = await response.json();

        // UI에 설정 적용
        // 경로는 프리셋에 값이 있을 때만 적용 (기존 경로 유지)
        if (config.source) {
            document.getElementById('source').value = config.source;
        }
        if (config.dest) {
            document.getElementById('dest').value = config.dest;
        }
        document.getElementById('organizeStrategy').value = config.organize_strategy || 'date';
        document.getElementById('eventName').value = config.event_name || '';
        document.getElementById('conflictPolicy').value = config.conflict_policy || 'skip';
        document.getElementById('dedupMethod').value = config.dedup_method || 'name-size';
        document.getElementById('dryRun').checked = config.dry_run || false;
        document.getElementById('hashVerify').checked = config.hash_verify || false;
        document.getElementById('ignoreState').checked = config.ignore_state || false;

        // 확장자 태그 업데이트
        if (config.include_extensions && typeof includeExtensions !== 'undefined') {
            includeExtensions = config.include_extensions;
            if (typeof renderExtensionTags === 'function') {
                renderExtensionTags();
            }
        }

        // 이벤트명 입력 필드 표시/숨김
        if (typeof toggleEventNameInput === 'function') {
            toggleEventNameInput();
        }

        // 북마크 버튼 상태 업데이트
        if (typeof updateBookmarkButtons === 'function') {
            updateBookmarkButtons();
        }

        // 사이드바 닫기
        togglePresetSidebar();

        // 알림
        showNotification(`프리셋 "${presetName}"을 불러왔습니다`, 'success');
    } catch (error) {
        console.error('프리셋 로드 실패:', error);
        showNotification('프리셋을 불러오는데 실패했습니다', 'error');
    }
}

// 프리셋 저장 다이얼로그 표시
function showSavePresetDialog() {
    const dialog = document.getElementById('savePresetDialog');
    if (dialog) {
        dialog.style.display = 'block';
        document.getElementById('presetName').value = '';
        document.getElementById('presetDescription').value = '';
        document.getElementById('presetName').focus();
    }
}

// 프리셋 저장 다이얼로그 숨김
function hideSavePresetDialog() {
    const dialog = document.getElementById('savePresetDialog');
    if (dialog) {
        dialog.style.display = 'none';
    }
}

// 프리셋 저장
async function savePreset() {
    const name = document.getElementById('presetName').value.trim();
    const description = document.getElementById('presetDescription').value.trim();

    if (!name) {
        showNotification('프리셋 이름을 입력해주세요', 'warning');
        return;
    }

    // 현재 설정 수집
    const jobsInput = document.getElementById('jobs');
    const jobsValue = jobsInput?.value;
    const jobsParsed = parseInt(jobsValue);

    const config = {
        source: document.getElementById('source').value,
        dest: document.getElementById('dest').value,
        include_extensions: typeof includeExtensions !== 'undefined' ? includeExtensions : [],
        jobs: isNaN(jobsParsed) ? 0 : jobsParsed,
        dedup_method: document.getElementById('dedupMethod').value,
        conflict_policy: document.getElementById('conflictPolicy').value,
        organize_strategy: document.getElementById('organizeStrategy').value,
        event_name: document.getElementById('eventName').value,
        unclassified_dir: 'unclassified',
        quarantine_dir: 'quarantine',
        dry_run: document.getElementById('dryRun').checked,
        hash_verify: document.getElementById('hashVerify').checked,
        ignore_state: document.getElementById('ignoreState').checked
    };

    try {
        const response = await fetch('/api/presets', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({
                name: name,
                description: description,
                config: config
            })
        });

        if (!response.ok) {
            throw new Error('Failed to save preset');
        }

        showNotification(`프리셋 "${name}"을 저장했습니다`, 'success');
        hideSavePresetDialog();
        loadPresetList(); // 목록 새로고침
    } catch (error) {
        console.error('프리셋 저장 실패:', error);
        showNotification('프리셋 저장에 실패했습니다', 'error');
    }
}

// 프리셋 삭제 (이름으로)
async function deletePresetByName(presetName) {
    if (!confirm(`"${presetName}" 프리셋을 정말 삭제하시겠습니까?`)) {
        return;
    }

    try {
        const response = await fetch(`/api/presets/delete?name=${encodeURIComponent(presetName)}`, {
            method: 'DELETE'
        });

        if (!response.ok) {
            throw new Error('Failed to delete preset');
        }

        showNotification(`프리셋 "${presetName}"을 삭제했습니다`, 'success');
        loadPresetList(); // 목록 새로고침
    } catch (error) {
        console.error('프리셋 삭제 실패:', error);
        showNotification('프리셋 삭제에 실패했습니다', 'error');
    }
}

// 알림 표시 (간단한 토스트)
function showNotification(message, type = 'info') {
    // 기존 알림 제거
    const existing = document.querySelector('.preset-notification');
    if (existing) {
        existing.remove();
    }

    // 새 알림 생성
    const notification = document.createElement('div');
    notification.className = `preset-notification preset-notification-${type}`;
    notification.textContent = message;

    // 스타일
    Object.assign(notification.style, {
        position: 'fixed',
        bottom: '24px',
        right: '24px',
        padding: '16px 24px',
        borderRadius: '12px',
        background: type === 'success' ? '#10b981' : type === 'error' ? '#ef4444' : type === 'warning' ? '#f59e0b' : '#6366f1',
        color: 'white',
        fontSize: '15px',
        fontWeight: '600',
        boxShadow: '0 10px 25px rgba(0, 0, 0, 0.2)',
        zIndex: '9999',
        animation: 'slideInRight 0.3s ease',
        maxWidth: '400px'
    });

    document.body.appendChild(notification);

    // 3초 후 제거
    setTimeout(() => {
        notification.style.animation = 'slideOutRight 0.3s ease';
        setTimeout(() => notification.remove(), 300);
    }, 3000);
}

// 애니메이션 CSS 추가
const style = document.createElement('style');
style.textContent = `
    @keyframes slideInRight {
        from {
            transform: translateX(400px);
            opacity: 0;
        }
        to {
            transform: translateX(0);
            opacity: 1;
        }
    }
    @keyframes slideOutRight {
        from {
            transform: translateX(0);
            opacity: 1;
        }
        to {
            transform: translateX(400px);
            opacity: 0;
        }
    }
`;
document.head.appendChild(style);

// 페이지 로드 시 프리셋 목록 로드
window.addEventListener('DOMContentLoaded', () => {
    loadPresetList();

    // Event delegation for preset card buttons
    const presetList = document.getElementById('presetList');
    if (presetList) {
        presetList.addEventListener('click', (e) => {
            const btn = e.target.closest('.preset-card-btn');
            if (!btn) return;

            const action = btn.dataset.action;
            const presetName = btn.dataset.preset;

            if (action === 'load') {
                loadPresetByName(presetName);
            } else if (action === 'delete') {
                deletePresetByName(presetName);
            }
        });
    }
});
