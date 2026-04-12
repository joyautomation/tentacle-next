//go:build plc || all

// Package st implements an IEC 61131-3 Structured Text to Starlark transpiler.
package st

// TokenType identifies a token kind.
type TokenType int

const (
	// Literals and identifiers
	TokenEOF TokenType = iota
	TokenIdent
	TokenNumber
	TokenString
	TokenTimeLiteral // T#5s, T#100ms

	// Keywords
	TokenProgram
	TokenEndProgram
	TokenVar
	TokenVarInput
	TokenVarOutput
	TokenEndVar
	TokenIf
	TokenThen
	TokenElsif
	TokenElse
	TokenEndIf
	TokenFor
	TokenTo
	TokenBy
	TokenDo
	TokenEndFor
	TokenWhile
	TokenEndWhile
	TokenRepeat
	TokenUntil
	TokenEndRepeat
	TokenCase
	TokenOf
	TokenEndCase
	TokenReturn
	TokenTrue
	TokenFalse
	TokenAnd
	TokenOr
	TokenNot
	TokenXor
	TokenMod

	// Types
	TokenInt
	TokenReal
	TokenBool
	TokenStringType
	TokenDint
	TokenLreal

	// Operators
	TokenAssign    // :=
	TokenEqual     // =
	TokenNotEqual  // <>
	TokenLess      // <
	TokenLessEq    // <=
	TokenGreater   // >
	TokenGreaterEq // >=
	TokenPlus      // +
	TokenMinus     // -
	TokenStar      // *
	TokenSlash     // /
	TokenLParen    // (
	TokenRParen    // )
	TokenSemicolon // ;
	TokenColon     // :
	TokenComma     // ,
	TokenDot       // .
	TokenHash      // #
)

// Token represents a single lexical token.
type Token struct {
	Type    TokenType
	Literal string
	Line    int
	Col     int
}

// keywords maps IEC 61131-3 keywords to token types.
var keywords = map[string]TokenType{
	"PROGRAM":     TokenProgram,
	"END_PROGRAM": TokenEndProgram,
	"VAR":         TokenVar,
	"VAR_INPUT":   TokenVarInput,
	"VAR_OUTPUT":  TokenVarOutput,
	"END_VAR":     TokenEndVar,
	"IF":          TokenIf,
	"THEN":        TokenThen,
	"ELSIF":       TokenElsif,
	"ELSE":        TokenElse,
	"END_IF":      TokenEndIf,
	"FOR":         TokenFor,
	"TO":          TokenTo,
	"BY":          TokenBy,
	"DO":          TokenDo,
	"END_FOR":     TokenEndFor,
	"WHILE":       TokenWhile,
	"END_WHILE":   TokenEndWhile,
	"REPEAT":      TokenRepeat,
	"UNTIL":       TokenUntil,
	"END_REPEAT":  TokenEndRepeat,
	"CASE":        TokenCase,
	"OF":          TokenOf,
	"END_CASE":    TokenEndCase,
	"RETURN":      TokenReturn,
	"TRUE":        TokenTrue,
	"FALSE":       TokenFalse,
	"AND":         TokenAnd,
	"OR":          TokenOr,
	"NOT":         TokenNot,
	"XOR":         TokenXor,
	"MOD":         TokenMod,
	"INT":         TokenInt,
	"REAL":        TokenReal,
	"BOOL":        TokenBool,
	"STRING":      TokenStringType,
	"DINT":        TokenDint,
	"LREAL":       TokenLreal,
}
