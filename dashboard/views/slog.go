package views

import (
	"iter"
	"log/slog"
)

func iterSlogAttrs(record slog.Record) iter.Seq[slog.Attr] {
	return func(yield func(attr slog.Attr) bool) {
		record.Attrs(func(attr slog.Attr) bool {
			if !yield(attr) {
				return false
			}
			return true
		})
	}
}
