import type { CompletionContext, CompletionResult } from '@codemirror/autocomplete';

const varFunctions = [
  'get_var', 'get_num', 'get_bool', 'get_str', 'set_var',
  'NO', 'NC', 'OTE', 'OTL', 'OTU',
  'TON', 'TOF', 'CTU', 'CTD', 'RES',
];

const fnPattern = new RegExp(
  `(?:${varFunctions.join('|')})\\(\\s*["']([^"']*)$`
);

export function createVarCompletion(varNames: string[]) {
  return (context: CompletionContext): CompletionResult | null => {
    const line = context.state.doc.lineAt(context.pos);
    const textBefore = line.text.slice(0, context.pos - line.from);
    const match = textBefore.match(fnPattern);
    if (!match) return null;

    const partial = match[1];
    const from = context.pos - partial.length;

    return {
      from,
      options: varNames.map(name => ({ label: name, type: 'variable' })),
      filter: true,
    };
  };
}
