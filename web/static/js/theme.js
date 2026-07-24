// Theme Module
// 라이트/다크/시스템 테마 선택과 저장. 깜빡임 방지를 위해 head에서 로드된다.

const THEME_STORAGE_KEY = 'shutterpipe.theme';
const THEME_ORDER = ['system', 'light', 'dark'];
const THEME_LABELS = { system: '시스템', light: '라이트', dark: '다크' };
let currentTheme = 'system';

function applyTheme(theme) {
    const root = document.documentElement;
    // 테마 전환 순간에는 색 트랜지션을 꺼서 light-dark() 재계산과 트랜지션이
    // 서로 물려 중간색으로 고착되는 것을 막는다.
    root.classList.add('theme-switching');
    if (theme === 'light' || theme === 'dark') {
        root.dataset.theme = theme;
    } else {
        delete root.dataset.theme;
    }
    if (typeof requestAnimationFrame === 'function') {
        requestAnimationFrame(() => requestAnimationFrame(() => root.classList.remove('theme-switching')));
    } else {
        root.classList.remove('theme-switching');
    }
}

function updateThemeToggle(theme) {
    const button = document.getElementById('themeToggle');
    if (!button) return;
    button.dataset.mode = theme;
    button.title = `테마: ${THEME_LABELS[theme]}`;
    button.setAttribute('aria-label', `테마: ${THEME_LABELS[theme]}, 클릭하면 전환`);
}

function setTheme(theme) {
    currentTheme = THEME_ORDER.includes(theme) ? theme : 'system';
    try {
        localStorage.setItem(THEME_STORAGE_KEY, currentTheme);
    } catch (_error) { /* 프라이빗 모드 등에서 저장 실패는 무시 */ }
    applyTheme(currentTheme);
    updateThemeToggle(currentTheme);
}

function cycleTheme() {
    const next = THEME_ORDER[(THEME_ORDER.indexOf(currentTheme) + 1) % THEME_ORDER.length];
    setTheme(next);
}

(function initTheme() {
    try {
        currentTheme = localStorage.getItem(THEME_STORAGE_KEY) || 'system';
    } catch (_error) { /* 저장소 접근 실패 시 시스템 테마 */ }
    if (!THEME_ORDER.includes(currentTheme)) currentTheme = 'system';
    applyTheme(currentTheme);
    document.addEventListener('DOMContentLoaded', () => updateThemeToggle(currentTheme));
})();
