package views

import (
	"context"
	"io"
	"strings"

	"github.com/a-h/templ"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// highlightContent applies syntax highlighting to the content
func highlightContent(content string, contentType string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		// Split content type before ;
		contentType = strings.Split(contentType, ";")[0]

		lexer := lexers.MatchMimeType(contentType)
		if lexer == nil {
			lexer = lexers.Fallback
		}

		formatter, style := chromaFormatterAndStyle()

		iterator, err := lexer.Tokenise(nil, content)
		if err != nil {
			return err
		}

		err = formatter.Format(w, style, iterator)
		return err
	})
}

func chromaFormatterAndStyle() (*html.Formatter, *chroma.Style) {
	formatter := html.New(
		html.Standalone(false),
		html.WithClasses(true),
		html.TabWidth(4),
	)

	style := styles.Get("monokai")
	if style == nil {
		style = styles.Fallback
	}

	return formatter, style
}

func chromaStyles() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, _ = io.WriteString(w, "<style>")
		formatter, style := chromaFormatterAndStyle()
		err := formatter.WriteCSS(w, style)

		_, _ = io.WriteString(w, ".chroma { white-space: pre-wrap; }\n")
		_, _ = io.WriteString(w, "</style>")
		return err
	})
}

type HandlerOptions struct {
	PathPrefix string
}

// Context key for HandlerOptions
type handlerOptionsKey struct{}

// SetHandlerOptions adds HandlerOptions to the context
func WithHandlerOptions(ctx context.Context, opts HandlerOptions) context.Context {
	return context.WithValue(ctx, handlerOptionsKey{}, opts)
}

// GetHandlerOptions retrieves HandlerOptions from the context
func GetHandlerOptions(ctx context.Context) (HandlerOptions, bool) {
	opts, ok := ctx.Value(handlerOptionsKey{}).(HandlerOptions)
	return opts, ok
}

// MustGetHandlerOptions retrieves HandlerOptions from the context or panics if not found
func MustGetHandlerOptions(ctx context.Context) HandlerOptions {
	opts, ok := GetHandlerOptions(ctx)
	if !ok {
		panic("HandlerOptions not found in context")
	}
	return opts
}
