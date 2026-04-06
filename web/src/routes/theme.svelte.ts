export type Theme = 'themeSystem' | 'themeLight' | 'themeDark';

const isTheme = (value: string): value is Theme =>
	value === 'themeSystem' || value === 'themeLight' || value === 'themeDark';

function readThemeCookie(): Theme | null {
	if (typeof document === 'undefined') return null;
	const match = document.cookie?.match(/(?:^|; )theme=([^;]+)/);
	if (!match) return null;
	try {
		const value = decodeURIComponent(match[1]);
		return isTheme(value) ? value : null;
	} catch {
		return null;
	}
}

function createThemeState() {
	let current = $state<Theme>('themeSystem');

	function initialize(serverTheme?: Theme | null) {
		if (serverTheme && isTheme(serverTheme)) {
			current = serverTheme;
		} else {
			const cookieTheme = readThemeCookie();
			current = cookieTheme ?? 'themeSystem';
		}
	}

	function set(newTheme: Theme) {
		current = newTheme;
		if (typeof document !== 'undefined') {
			document.cookie = `theme=${newTheme}; path=/; max-age=31536000`;
			document.body.className = newTheme;
		}
	}

	return {
		get value() {
			return current;
		},
		initialize,
		set
	};
}

export const themeState = createThemeState();

export function getEffectiveTheme(): 'themeLight' | 'themeDark' {
	if (themeState.value === 'themeSystem') {
		const prefersDark = globalThis.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false;
		return prefersDark ? 'themeDark' : 'themeLight';
	}
	return themeState.value;
}
