package views

import "strings"

type ButtonVariant string
type ButtonSize string

const (
	ButtonVariantDefault   ButtonVariant = ""
	ButtonVariantOutline   ButtonVariant = "outline"
	ButtonVariantSecondary ButtonVariant = "secondary"

	ButtonSizeSm   ButtonSize = "sm"
	ButtonSizeIcon ButtonSize = "icon"
)

type ButtonProps struct {
	Variant  ButtonVariant
	Size     ButtonSize
	Class    string
	Disabled bool
}

func buttonClasses(props ButtonProps) string {
	var classes []string

	// Base classes
	classes = append(classes, "cursor-pointer inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50")

	// Variant classes
	switch props.Variant {
	case ButtonVariantOutline:
		classes = append(classes, "border border-neutral-200 bg-white hover:bg-neutral-200 text-black")
	case ButtonVariantSecondary:
		classes = append(classes, "bg-neutral-200 text-black hover:bg-neutral-200/80")
	default: // DefaultVariant
		classes = append(classes, "bg-black text-white hover:bg-black/90")
	}

	// Size classes
	switch props.Size {
	case ButtonSizeSm:
		classes = append(classes, "h-8 rounded-md px-3")
	case ButtonSizeIcon:
		classes = append(classes, "h-10 w-10")
	default: // DefaultSize
		classes = append(classes, "h-10 px-4 py-2")
	}

	// Additional custom classes
	if props.Class != "" {
		classes = append(classes, props.Class)
	}

	return strings.Join(classes, " ")
}
