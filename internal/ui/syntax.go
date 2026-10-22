package ui

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
)

// Register once at package initialization, before asynchronous rendering. Chroma's
// native terminal16 formatter maps these named ANSI colors to palette slots;
// neither ordinary text nor a code block supplies a background or base color.
var terminalSyntax = styles.Register(chroma.MustNewStyle("t3oxide-terminal", chroma.StyleEntries{
	chroma.Text:              "",
	chroma.Background:        "",
	chroma.Error:             "bold #ansidarkred",
	chroma.Comment:           "italic",
	chroma.CommentPreproc:    "#ansipurple",
	chroma.Keyword:           "bold #ansidarkblue",
	chroma.Operator:          "#ansipurple",
	chroma.NameBuiltin:       "#ansiteal",
	chroma.NameFunction:      "#ansiteal",
	chroma.NameClass:         "bold #ansiteal",
	chroma.NameTag:           "#ansidarkblue",
	chroma.NameAttribute:     "#ansiteal",
	chroma.LiteralString:     "#ansidarkgreen",
	chroma.LiteralNumber:     "#ansipurple",
	chroma.GenericDeleted:    "#ansidarkred",
	chroma.GenericInserted:   "#ansidarkgreen",
	chroma.GenericEmph:       "italic",
	chroma.GenericStrong:     "bold",
	chroma.GenericSubheading: "bold #ansidarkblue",
}))
