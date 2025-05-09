package views

import "strings"

type BadgeVariant string

const (
	BadgeVariantSecondary BadgeVariant = "secondary"
	BadgeVariantSuccess   BadgeVariant = "success"
	BadgeVariantWarning   BadgeVariant = "warning"
	BadgeVariantError     BadgeVariant = "error"
	BadgeVariantOutline   BadgeVariant = "outline"
)

type BadgeProps struct {
	Variant BadgeVariant
	Class   string
}

func badgeClasses(props BadgeProps) string {
	var classes []string

	// Base classes
	classes = append(classes, "inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors font-mono")

	// Variant classes
	switch props.Variant {
	case BadgeVariantSecondary:
		classes = append(classes, "border-transparent bg-neutral-200 text-black")
	case BadgeVariantSuccess:
		classes = append(classes, "border-transparent bg-green-600 text-white")
	case BadgeVariantWarning:
		classes = append(classes, "border-transparent bg-orange-400 text-white")
	case BadgeVariantError:
		classes = append(classes, "border-transparent bg-red-500 text-white")
	case BadgeVariantOutline:
		classes = append(classes, "border-neutral-300 text-foreground")
	default: // DefaultBadgeVariant
		classes = append(classes, "border-transparent bg-black text-white")
	}

	// Additional custom classes
	if props.Class != "" {
		classes = append(classes, props.Class)
	}

	return strings.Join(classes, " ")
}
