// UserData API - Settings, Bookmarks, PathHistory management

async function parseApiErrorResponse(response) {
    const contentType = response.headers.get('content-type') || '';

    if (contentType.includes('application/json')) {
        try {
            const errorData = await response.json();
            const message = errorData?.message || errorData?.error || `서버 오류 (${response.status})`;
            return { field: errorData?.field, error: message };
        } catch (parseError) {
            return { error: `서버 오류 (${response.status})` };
        }
    }

    const errorText = await response.text();
    return { error: errorText || `서버 오류 (${response.status})` };
}

// =============================================================================
// Run API
// =============================================================================

async function startBackupRunOnServer(config) {
    try {
        const response = await fetch('/api/run', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(config)
        });

        if (!response.ok) {
            const parsedError = await parseApiErrorResponse(response);
            return {
                success: false,
                status: response.status,
                field: parsedError.field,
                error: parsedError.error
            };
        }

        return { success: true, status: response.status };
    } catch (error) {
        console.error('백업 시작 요청 실패:', error);
        return { success: false, error: error.message };
    }
}

// =============================================================================
// Settings API
// =============================================================================

async function loadSettingsFromServer() {
    try {
        const response = await fetch('/api/settings');
        if (!response.ok) {
            throw new Error(`Failed to load settings: ${response.status}`);
        }
        return await response.json();
    } catch (error) {
        console.error('설정 로드 실패:', error);
        if (typeof showNotification === 'function') {
            showNotification('설정 로드 실패', 'error');
        }
        return null;
    }
}

async function saveSettingsToServer(settings) {
    try {
        const response = await fetch('/api/settings', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(settings)
        });
        if (!response.ok) {
            const parsedError = await parseApiErrorResponse(response);
            if (typeof showNotification === 'function') {
                showNotification(`설정 저장 실패: ${parsedError.error}`, 'error');
            }
            return { success: false, field: parsedError.field, error: parsedError.error };
        }
        return { success: true };
    } catch (error) {
        console.error('설정 저장 실패:', error);
        if (typeof showNotification === 'function') {
            showNotification(`설정 저장 실패: ${error.message}`, 'error');
        }
        return { success: false, error: error.message };
    }
}

// =============================================================================
// Bookmarks API
// =============================================================================

async function loadBookmarksFromServer() {
    try {
        const response = await fetch('/api/bookmarks');
        if (!response.ok) {
            throw new Error(`Failed to load bookmarks: ${response.status}`);
        }
        return await response.json();
    } catch (error) {
        console.error('북마크 로드 실패:', error);
        if (typeof showNotification === 'function') {
            showNotification('북마크 로드 실패', 'error');
        }
        return null;
    }
}

async function saveBookmarksToServer(bookmarks) {
    try {
        const response = await fetch('/api/bookmarks', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(bookmarks)
        });
        if (!response.ok) {
            const parsedError = await parseApiErrorResponse(response);
            if (typeof showNotification === 'function') {
                showNotification(`북마크 저장 실패: ${parsedError.error}`, 'error');
            }
            return { success: false, field: parsedError.field, error: parsedError.error };
        }
        return { success: true };
    } catch (error) {
        console.error('북마크 저장 실패:', error);
        if (typeof showNotification === 'function') {
            showNotification(`북마크 저장 실패: ${error.message}`, 'error');
        }
        return { success: false, error: error.message };
    }
}

// =============================================================================
// PathHistory API
// =============================================================================

async function loadPathHistoryFromServer() {
    try {
        const response = await fetch('/api/path-history');
        if (!response.ok) {
            throw new Error(`Failed to load path history: ${response.status}`);
        }
        return await response.json();
    } catch (error) {
        console.error('경로 히스토리 로드 실패:', error);
        if (typeof showNotification === 'function') {
            showNotification('경로 히스토리 로드 실패', 'error');
        }
        return null;
    }
}

async function savePathHistoryToServer(history) {
    try {
        const response = await fetch('/api/path-history', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(history)
        });
        if (!response.ok) {
            const parsedError = await parseApiErrorResponse(response);
            if (typeof showNotification === 'function') {
                showNotification(`경로 히스토리 저장 실패: ${parsedError.error}`, 'error');
            }
            return { success: false, field: parsedError.field, error: parsedError.error };
        }
        return { success: true };
    } catch (error) {
        console.error('경로 히스토리 저장 실패:', error);
        if (typeof showNotification === 'function') {
            showNotification(`경로 히스토리 저장 실패: ${error.message}`, 'error');
        }
        return { success: false, error: error.message };
    }
}

// =============================================================================
// BackupHistory API
// =============================================================================

async function loadBackupHistoryFromServer(limit = 20) {
    try {
        const response = await fetch(`/api/backup-history?limit=${limit}`);
        if (!response.ok) {
            throw new Error(`Failed to load backup history: ${response.status}`);
        }
        return await response.json();
    } catch (error) {
        console.error('백업 이력 로드 실패:', error);
        if (typeof showNotification === 'function') {
            showNotification('백업 이력 로드 실패', 'error');
        }
        return null;
    }
}

// =============================================================================
// Migration from localStorage
// =============================================================================

async function migrateFromLocalStorage() {
    const migrated = localStorage.getItem('shutterpipe-migrated');
    if (migrated === 'true') {
        console.log('이미 마이그레이션됨, 건너뜀');
        return;
    }

    console.log('localStorage에서 서버로 데이터 마이그레이션 시작...');

    let migrationSuccess = true;

    // 설정 마이그레이션
    const savedConfig = localStorage.getItem('shutterpipe-config');
    if (savedConfig) {
        try {
            const config = JSON.parse(savedConfig);
            const result = await saveSettingsToServer(config);
            if (!result.success) {
                migrationSuccess = false;
            } else {
                console.log('✓ 설정 마이그레이션 완료');
            }
        } catch (error) {
            console.error('설정 마이그레이션 실패:', error);
            migrationSuccess = false;
        }
    }

    // 북마크 마이그레이션
    const savedBookmarks = localStorage.getItem('shutterpipe-bookmarks');
    if (savedBookmarks) {
        try {
            const bookmarks = JSON.parse(savedBookmarks);
            const result = await saveBookmarksToServer(bookmarks);
            if (!result.success) {
                migrationSuccess = false;
            } else {
                console.log('✓ 북마크 마이그레이션 완료');
            }
        } catch (error) {
            console.error('북마크 마이그레이션 실패:', error);
            migrationSuccess = false;
        }
    }

    // 경로 히스토리 마이그레이션
    const savedHistory = localStorage.getItem('shutterpipe-path-history');
    if (savedHistory) {
        try {
            const history = JSON.parse(savedHistory);
            const result = await savePathHistoryToServer(history);
            if (!result.success) {
                migrationSuccess = false;
            } else {
                console.log('✓ 경로 히스토리 마이그레이션 완료');
            }
        } catch (error) {
            console.error('경로 히스토리 마이그레이션 실패:', error);
            migrationSuccess = false;
        }
    }

    if (migrationSuccess) {
        localStorage.setItem('shutterpipe-migrated', 'true');
        console.log('마이그레이션 완료!');
        if (typeof showNotification === 'function') {
            showNotification('데이터가 서버로 마이그레이션되었습니다', 'success');
        }
    } else {
        console.warn('일부 마이그레이션 실패. 다음 로드 시 재시도합니다.');
    }
}

// 페이지 로드 시 자동 마이그레이션 실행
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', migrateFromLocalStorage);
} else {
    migrateFromLocalStorage();
}
