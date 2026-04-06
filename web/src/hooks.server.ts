import type { Handle } from '@sveltejs/kit';

export const handle: Handle = async ({ event, resolve }) => {
	const theme = event.cookies.get('theme') ?? 'themeSystem';
	const validThemes = ['themeSystem', 'themeLight', 'themeDark'];
	const safeTheme = validThemes.includes(theme) ? theme : 'themeSystem';

	return resolve(event, {
		transformPageChunk: ({ html }) => html.replace('%theme%', safeTheme)
	});
};
