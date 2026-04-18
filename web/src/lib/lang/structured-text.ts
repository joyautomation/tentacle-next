import { StreamLanguage, type StreamParser } from '@codemirror/language';
import { tags } from '@lezer/highlight';

const keywords = new Set([
  'PROGRAM', 'END_PROGRAM', 'FUNCTION', 'END_FUNCTION', 'FUNCTION_BLOCK', 'END_FUNCTION_BLOCK',
  'VAR', 'VAR_INPUT', 'VAR_OUTPUT', 'VAR_IN_OUT', 'VAR_GLOBAL', 'VAR_TEMP', 'VAR_EXTERNAL',
  'END_VAR', 'CONSTANT', 'RETAIN', 'AT',
  'IF', 'THEN', 'ELSIF', 'ELSE', 'END_IF',
  'CASE', 'OF', 'END_CASE',
  'FOR', 'TO', 'BY', 'DO', 'END_FOR',
  'WHILE', 'END_WHILE',
  'REPEAT', 'UNTIL', 'END_REPEAT',
  'RETURN', 'EXIT',
  'AND', 'OR', 'XOR', 'NOT', 'MOD',
  'TRUE', 'FALSE',
  'TYPE', 'END_TYPE', 'STRUCT', 'END_STRUCT', 'ARRAY',
]);

const types = new Set([
  'BOOL', 'BYTE', 'WORD', 'DWORD', 'LWORD',
  'SINT', 'INT', 'DINT', 'LINT',
  'USINT', 'UINT', 'UDINT', 'ULINT',
  'REAL', 'LREAL',
  'STRING', 'WSTRING', 'CHAR', 'WCHAR',
  'TIME', 'DATE', 'TIME_OF_DAY', 'TOD', 'DATE_AND_TIME', 'DT',
]);

interface StState {
  inBlockComment: boolean;
}

const stParser: StreamParser<StState> = {
  name: 'structured-text',

  startState(): StState {
    return { inBlockComment: false };
  },

  token(stream, state): string | null {
    if (state.inBlockComment) {
      const end = stream.match(/.*?\*\)/, false);
      if (end) {
        stream.match(/.*?\*\)/);
        state.inBlockComment = false;
      } else {
        stream.skipToEnd();
      }
      return 'blockComment';
    }

    if (stream.match('(*')) {
      state.inBlockComment = true;
      return 'blockComment';
    }

    if (stream.match('//')) {
      stream.skipToEnd();
      return 'lineComment';
    }

    if (stream.match(/^'[^']*'/)) {
      return 'string';
    }

    if (stream.match(/^"[^"]*"/)) {
      return 'string';
    }

    if (stream.match(/^16#[0-9A-Fa-f_]+/) ||
        stream.match(/^8#[0-7_]+/) ||
        stream.match(/^2#[01_]+/)) {
      return 'number';
    }

    if (stream.match(/^[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?/)) {
      return 'number';
    }

    if (stream.match(/^T#[0-9dhms_]+/i) ||
        stream.match(/^D#[0-9\-]+/i) ||
        stream.match(/^TOD#[0-9:]+/i) ||
        stream.match(/^DT#[0-9\-:]+/i)) {
      return 'number';
    }

    if (stream.match(/^:=/)) return 'operator';
    if (stream.match(/^<>|<=|>=|=>|\*\*/)) return 'operator';

    if (stream.match(/^[a-zA-Z_][a-zA-Z0-9_]*/)) {
      const word = stream.current().toUpperCase();
      if (keywords.has(word)) {
        if (word === 'TRUE' || word === 'FALSE') return 'bool';
        return 'keyword';
      }
      if (types.has(word)) return 'typeName';
      return 'variableName';
    }

    stream.next();
    return null;
  },

  tokenTable: {
    keyword: tags.keyword,
    typeName: tags.typeName,
    variableName: tags.variableName,
    string: tags.string,
    number: tags.number,
    bool: tags.bool,
    operator: tags.operator,
    lineComment: tags.lineComment,
    blockComment: tags.blockComment,
  },
};

export function structuredText() {
  return StreamLanguage.define(stParser);
}
