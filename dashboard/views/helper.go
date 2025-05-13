package views

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

func formatDurationSince(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	if d < 0 {
		return "in the future"
	}
	if d < time.Minute {
		return fmt.Sprintf("%d seconds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	}
	return fmt.Sprintf("%d days ago", int(d.Hours()/24))
}

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
