import { linter, type Diagnostic } from '@codemirror/lint';
import type { EditorView } from '@codemirror/view';
import { apiPost } from '$lib/api/client';

export type PlcLintLanguage = 'starlark' | 'python' | 'st';

type ValidateDiagnostic = {
	severity: 'error' | 'warning' | 'info';
	message: string;
	line: number;
	col: number;
};

type ValidateResponse = {
	diagnostics: ValidateDiagnostic[];
};

export type PlcLintOptions = {
	language: () => PlcLintLanguage;
	variableNames: () => string[];
	delay?: number;
};

const VAR_ACCESS_RE = /\b(get_num|get_bool|get_var|set_num|set_bool|set_var)\s*\(\s*"([^"\\]*)"/g;

function lineColToPos(view: EditorView, line: number, col: number): number {
	const doc = view.state.doc;
	const safeLine = Math.max(1, Math.min(line, doc.lines));
	const lineObj = doc.line(safeLine);
	const safeCol = Math.max(1, Math.min(col || 1, lineObj.length + 1));
	return lineObj.from + (safeCol - 1);
}

function unknownVariableDiagnostics(
	view: EditorView,
	known: Set<string>
): Diagnostic[] {
	const diags: Diagnostic[] = [];
	const text = view.state.doc.toString();
	let match: RegExpExecArray | null;
	VAR_ACCESS_RE.lastIndex = 0;
	while ((match = VAR_ACCESS_RE.exec(text)) !== null) {
		const name = match[2];
		if (name === '' || known.has(name)) continue;
		const nameStart = match.index + match[0].lastIndexOf(name);
		diags.push({
			from: nameStart,
			to: nameStart + name.length,
			severity: 'warning',
			message: `Unknown variable "${name}"`
		});
	}
	return diags;
}

async function parseErrorDiagnostics(
	view: EditorView,
	language: PlcLintLanguage
): Promise<Diagnostic[]> {
	const source = view.state.doc.toString();
	if (source.trim() === '') return [];
	const result = await apiPost<ValidateResponse>('/plcs/plc/programs/validate', {
		source,
		language
	});
	if (result.error || !result.data) return [];
	return result.data.diagnostics.map((d) => {
		const from = lineColToPos(view, d.line, d.col);
		const lineEnd = view.state.doc.line(
			Math.max(1, Math.min(d.line || 1, view.state.doc.lines))
		).to;
		return {
			from,
			to: Math.max(from + 1, Math.min(from + 1, lineEnd)),
			severity: d.severity,
			message: d.message
		} as Diagnostic;
	});
}

export function plcLinter(opts: PlcLintOptions) {
	return linter(
		async (view) => {
			const lang = opts.language();
			const known = new Set(opts.variableNames());
			const [parseDiags, unknownDiags] = await Promise.all([
				parseErrorDiagnostics(view, lang),
				Promise.resolve(unknownVariableDiagnostics(view, known))
			]);
			return [...parseDiags, ...unknownDiags];
		},
		{ delay: opts.delay ?? 500 }
	);
}
